package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	workspaceUsageEndpoint = "https://chatgpt.com/backend-api/wham/analytics/daily-workspace-usage-counts"
	maxUpstreamBodyBytes   = 4 << 20
)

type usageService struct {
	host  hostClient
	cfg   pluginConfig
	sleep func(time.Duration)
}

type apiError struct {
	status         int
	code           string
	message        string
	upstreamStatus int
}

func (e *apiError) Error() string {
	return e.message
}

type apiErrorBody struct {
	Error          string `json:"error"`
	Message        string `json:"message"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
}

type accountSummary struct {
	AuthIndex   string `json:"auth_index"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Label       string `json:"label,omitempty"`
	Email       string `json:"email,omitempty"`
	Status      string `json:"status,omitempty"`
	Disabled    bool   `json:"disabled"`
	Unavailable bool   `json:"unavailable"`
}

type accountsResponse struct {
	Accounts []accountSummary `json:"accounts"`
}

type codexCredential struct {
	Type        string `json:"type"`
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id"`
}

type workspaceUsageResponse struct {
	Data    []dailyUsage `json:"data"`
	GroupBy string       `json:"group_by,omitempty"`
}

type dailyUsage struct {
	Date   string       `json:"date"`
	Totals *usageTotals `json:"totals,omitempty"`
}

type usageTotals struct {
	Credits                 *float64 `json:"credits,omitempty"`
	CachedTextInputTokens   *int64   `json:"cached_text_input_tokens,omitempty"`
	UncachedTextInputTokens *int64   `json:"uncached_text_input_tokens,omitempty"`
	TextOutputTokens        *int64   `json:"text_output_tokens,omitempty"`
	TextTotalTokens         *int64   `json:"text_total_tokens,omitempty"`
	Threads                 *int64   `json:"threads,omitempty"`
	Turns                   *int64   `json:"turns,omitempty"`
	Users                   *int64   `json:"users,omitempty"`
}

type usageResult struct {
	Account   accountSummary `json:"account"`
	StartDate string         `json:"start_date"`
	EndDate   string         `json:"end_date"`
	GroupBy   string         `json:"group_by"`
	Data      []dailyUsage   `json:"data"`
	Summary   usageTotals    `json:"summary"`
}

func (s usageService) handleAccounts() managementResponse {
	accounts, errList := s.codexAccounts()
	if errList != nil {
		return responseForAPIError(newAPIError(http.StatusBadGateway, "auth_list_failed", "failed to list CPA credentials", 0))
	}
	return jsonManagementResponse(http.StatusOK, accountsResponse{Accounts: accounts})
}

func (s usageService) handleUsage(query url.Values, callbackID string) managementResponse {
	result, errQuery := s.queryUsage(query, callbackID)
	if errQuery != nil {
		return responseForAPIError(errQuery)
	}
	return jsonManagementResponse(http.StatusOK, result)
}

func (s usageService) queryUsage(query url.Values, callbackID string) (usageResult, error) {
	if s.host == nil {
		return usageResult{}, newAPIError(http.StatusBadGateway, "host_unavailable", "CPA host callbacks are unavailable", 0)
	}
	authIndex := strings.TrimSpace(query.Get("auth_index"))
	if authIndex == "" {
		return usageResult{}, newAPIError(http.StatusBadRequest, "auth_index_required", "auth_index is required", 0)
	}
	startDate, endDate, errDates := validateDateRange(query.Get("start_date"), query.Get("end_date"), s.cfg.MaxRangeDays)
	if errDates != nil {
		return usageResult{}, errDates
	}

	account, credential, errCredential := s.loadCredential(authIndex)
	if errCredential != nil {
		return usageResult{}, errCredential
	}

	requestURL := workspaceUsageURL(startDate, endDate)
	request := hostHTTPRequest{
		Method: http.MethodGet,
		URL:    requestURL,
		Headers: http.Header{
			"Accept":             []string{"application/json"},
			"Authorization":      []string{"Bearer " + credential.AccessToken},
			"Chatgpt-Account-Id": []string{credential.AccountID},
			"Openai-Beta":        []string{"codex-1"},
		},
	}
	upstream, errRequest := s.doWithRetry(callbackID, request)
	if errRequest != nil {
		return usageResult{}, errRequest
	}
	parsed, errParse := parseUsageResponse(upstream, startDate, endDate)
	if errParse != nil {
		return usageResult{}, errParse
	}

	return usageResult{
		Account:   account,
		StartDate: startDate,
		EndDate:   endDate,
		GroupBy:   parsed.GroupBy,
		Data:      parsed.Data,
		Summary:   summarizeUsage(parsed.Data),
	}, nil
}

func (s usageService) codexAccounts() ([]accountSummary, error) {
	if s.host == nil {
		return nil, fmt.Errorf("host callbacks are unavailable")
	}
	entries, errList := s.host.ListAuths()
	if errList != nil {
		return nil, errList
	}
	accounts := make([]accountSummary, 0)
	for _, entry := range entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Provider), "codex") && !strings.EqualFold(strings.TrimSpace(entry.Type), "codex") {
			continue
		}
		accounts = append(accounts, safeAccount(entry))
	}
	sort.Slice(accounts, func(i, j int) bool {
		return strings.ToLower(accounts[i].Name+accounts[i].Email+accounts[i].AuthIndex) < strings.ToLower(accounts[j].Name+accounts[j].Email+accounts[j].AuthIndex)
	})
	return accounts, nil
}

func (s usageService) loadCredential(authIndex string) (accountSummary, codexCredential, error) {
	entries, errList := s.host.ListAuths()
	if errList != nil {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusBadGateway, "auth_list_failed", "failed to list CPA credentials", 0)
	}
	var selected *hostAuthFileEntry
	for i := range entries {
		if entries[i].AuthIndex == authIndex {
			selected = &entries[i]
			break
		}
	}
	if selected == nil {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusNotFound, "auth_not_found", "credential was not found", 0)
	}
	if !strings.EqualFold(strings.TrimSpace(selected.Provider), "codex") && !strings.EqualFold(strings.TrimSpace(selected.Type), "codex") {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusBadRequest, "auth_not_codex", "credential is not a Codex OAuth credential", 0)
	}
	if selected.Disabled || selected.Unavailable {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusConflict, "auth_unavailable", "credential is disabled or unavailable", 0)
	}

	stored, errGet := s.host.GetAuth(authIndex)
	if errGet != nil {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusBadGateway, "auth_read_failed", "failed to read the Codex credential", 0)
	}
	var credential codexCredential
	if errUnmarshal := json.Unmarshal(stored.JSON, &credential); errUnmarshal != nil {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusBadGateway, "auth_invalid", "Codex credential JSON is invalid", 0)
	}
	credential.Type = strings.TrimSpace(credential.Type)
	credential.AccessToken = strings.TrimSpace(credential.AccessToken)
	credential.AccountID = strings.TrimSpace(credential.AccountID)
	if !strings.EqualFold(credential.Type, "codex") || credential.AccessToken == "" || credential.AccountID == "" {
		return accountSummary{}, codexCredential{}, newAPIError(http.StatusBadGateway, "auth_incomplete", "Codex credential is missing access_token or account_id", 0)
	}
	return safeAccount(*selected), credential, nil
}

func safeAccount(entry hostAuthFileEntry) accountSummary {
	return accountSummary{
		AuthIndex:   entry.AuthIndex,
		ID:          entry.ID,
		Name:        entry.Name,
		Label:       entry.Label,
		Email:       entry.Email,
		Status:      entry.Status,
		Disabled:    entry.Disabled,
		Unavailable: entry.Unavailable,
	}
}

func validateDateRange(rawStart, rawEnd string, maxRangeDays int) (string, string, error) {
	start := strings.TrimSpace(rawStart)
	end := strings.TrimSpace(rawEnd)
	if start == "" || end == "" {
		return "", "", newAPIError(http.StatusBadRequest, "date_range_required", "start_date and end_date are required", 0)
	}
	startTime, errStart := time.Parse("2006-01-02", start)
	endTime, errEnd := time.Parse("2006-01-02", end)
	if errStart != nil || errEnd != nil || startTime.Format("2006-01-02") != start || endTime.Format("2006-01-02") != end {
		return "", "", newAPIError(http.StatusBadRequest, "invalid_date", "dates must use YYYY-MM-DD", 0)
	}
	if endTime.Before(startTime) {
		return "", "", newAPIError(http.StatusBadRequest, "invalid_date_range", "end_date must not be before start_date", 0)
	}
	if maxRangeDays < 1 {
		maxRangeDays = defaultPluginConfig().MaxRangeDays
	}
	days := int(endTime.Sub(startTime)/(24*time.Hour)) + 1
	if days > maxRangeDays {
		return "", "", newAPIError(http.StatusBadRequest, "date_range_too_large", fmt.Sprintf("date range must not exceed %d days", maxRangeDays), 0)
	}
	return start, end, nil
}

func workspaceUsageURL(startDate, endDate string) string {
	query := url.Values{
		"start_date":     []string{startDate},
		"end_date":       []string{endDate},
		"group_by":       []string{"day"},
		"workspace_user": []string{"true"},
	}
	return workspaceUsageEndpoint + "?" + query.Encode()
}

func (s usageService) doWithRetry(callbackID string, request hostHTTPRequest) (hostHTTPResponse, error) {
	retries := s.cfg.MaxRetries
	if retries < 0 {
		retries = 0
	}
	sleep := s.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for attempt := 0; ; attempt++ {
		response, errDo := s.host.DoHTTP(callbackID, request)
		if errDo == nil && !shouldRetryStatus(response.StatusCode) {
			if response.StatusCode != http.StatusOK {
				return hostHTTPResponse{}, upstreamStatusError(response.StatusCode)
			}
			return response, nil
		}
		if attempt >= retries {
			if errDo != nil {
				return hostHTTPResponse{}, newAPIError(http.StatusBadGateway, "upstream_request_failed", "failed to query Codex Analytics", 0)
			}
			return hostHTTPResponse{}, upstreamStatusError(response.StatusCode)
		}
		sleep(retryDelay(attempt, response.Headers))
	}
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func retryDelay(attempt int, headers http.Header) time.Duration {
	if headers != nil {
		if seconds, errParse := strconv.Atoi(strings.TrimSpace(headers.Get("Retry-After"))); errParse == nil && seconds > 0 {
			if seconds > 2 {
				seconds = 2
			}
			return time.Duration(seconds) * time.Second
		}
	}
	delay := 200 * time.Millisecond * time.Duration(1<<min(attempt, 3))
	if delay > 2*time.Second {
		return 2 * time.Second
	}
	return delay
}

func upstreamStatusError(status int) error {
	switch status {
	case http.StatusBadRequest:
		return newAPIError(http.StatusBadGateway, "upstream_bad_request", "Codex Analytics rejected the query parameters", status)
	case http.StatusUnauthorized:
		return newAPIError(http.StatusBadGateway, "upstream_unauthorized", "Codex credential is expired or unauthorized", status)
	case http.StatusForbidden:
		return newAPIError(http.StatusBadGateway, "upstream_forbidden", "credential cannot access workspace Codex Analytics", status)
	case http.StatusTooManyRequests:
		return newAPIError(http.StatusTooManyRequests, "upstream_rate_limited", "Codex Analytics rate limit was reached", status)
	default:
		if status >= 500 {
			return newAPIError(http.StatusBadGateway, "upstream_unavailable", "Codex Analytics is temporarily unavailable", status)
		}
		return newAPIError(http.StatusBadGateway, "upstream_error", "Codex Analytics returned an unexpected status", status)
	}
}

func parseUsageResponse(response hostHTTPResponse, startDate, endDate string) (workspaceUsageResponse, error) {
	if len(response.Body) > maxUpstreamBodyBytes {
		return workspaceUsageResponse{}, newAPIError(http.StatusBadGateway, "upstream_response_too_large", "Codex Analytics response is too large", response.StatusCode)
	}
	var parsed workspaceUsageResponse
	if errUnmarshal := json.Unmarshal(response.Body, &parsed); errUnmarshal != nil {
		return workspaceUsageResponse{}, newAPIError(http.StatusBadGateway, "upstream_invalid_response", "Codex Analytics returned invalid JSON", response.StatusCode)
	}
	if parsed.Data == nil {
		parsed.Data = []dailyUsage{}
	}
	if parsed.GroupBy == "" {
		parsed.GroupBy = "day"
	}
	if parsed.GroupBy != "day" {
		return workspaceUsageResponse{}, newAPIError(http.StatusBadGateway, "upstream_invalid_response", "Codex Analytics returned an unexpected group_by value", response.StatusCode)
	}

	startTime, _ := time.Parse("2006-01-02", startDate)
	endTime, _ := time.Parse("2006-01-02", endDate)
	seen := make(map[string]struct{}, len(parsed.Data))
	for _, day := range parsed.Data {
		date, errParse := time.Parse("2006-01-02", day.Date)
		if errParse != nil || day.Date != date.Format("2006-01-02") || date.Before(startTime) || date.After(endTime) {
			return workspaceUsageResponse{}, newAPIError(http.StatusBadGateway, "upstream_invalid_response", "Codex Analytics returned an invalid or out-of-range date", response.StatusCode)
		}
		if _, exists := seen[day.Date]; exists {
			return workspaceUsageResponse{}, newAPIError(http.StatusBadGateway, "upstream_invalid_response", "Codex Analytics returned a duplicate date", response.StatusCode)
		}
		seen[day.Date] = struct{}{}
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Date < parsed.Data[j].Date })
	return parsed, nil
}

func summarizeUsage(days []dailyUsage) usageTotals {
	var summary usageTotals
	for _, day := range days {
		if day.Totals == nil {
			continue
		}
		addFloat(&summary.Credits, day.Totals.Credits)
		addInt(&summary.CachedTextInputTokens, day.Totals.CachedTextInputTokens)
		addInt(&summary.UncachedTextInputTokens, day.Totals.UncachedTextInputTokens)
		addInt(&summary.TextOutputTokens, day.Totals.TextOutputTokens)
		addInt(&summary.TextTotalTokens, day.Totals.TextTotalTokens)
		addInt(&summary.Threads, day.Totals.Threads)
		addInt(&summary.Turns, day.Totals.Turns)
		addInt(&summary.Users, day.Totals.Users)
	}
	return summary
}

func addFloat(dst **float64, src *float64) {
	if src == nil {
		return
	}
	if *dst == nil {
		value := *src
		*dst = &value
		return
	}
	**dst += *src
}

func addInt(dst **int64, src *int64) {
	if src == nil {
		return
	}
	if *dst == nil {
		value := *src
		*dst = &value
		return
	}
	**dst += *src
}

func newAPIError(status int, code, message string, upstreamStatus int) *apiError {
	return &apiError{status: status, code: code, message: message, upstreamStatus: upstreamStatus}
}

func responseForAPIError(err error) managementResponse {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return jsonManagementError(apiErr.status, apiErr.code, apiErr.message, apiErr.upstreamStatus)
	}
	return jsonManagementError(http.StatusInternalServerError, "internal_error", "plugin request failed", 0)
}
