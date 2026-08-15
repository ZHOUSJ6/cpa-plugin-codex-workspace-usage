package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeHostClient struct {
	auths       []hostAuthFileEntry
	auth        hostAuthGetResponse
	responses   []hostHTTPResponse
	httpErr     error
	requests    []hostHTTPRequest
	callbackIDs []string
}

func (f *fakeHostClient) ListAuths() ([]hostAuthFileEntry, error) {
	return append([]hostAuthFileEntry(nil), f.auths...), nil
}

func (f *fakeHostClient) GetAuth(string) (hostAuthGetResponse, error) {
	return f.auth, nil
}

func (f *fakeHostClient) DoHTTP(callbackID string, request hostHTTPRequest) (hostHTTPResponse, error) {
	f.callbackIDs = append(f.callbackIDs, callbackID)
	f.requests = append(f.requests, request)
	if f.httpErr != nil {
		return hostHTTPResponse{}, f.httpErr
	}
	if len(f.responses) == 0 {
		return hostHTTPResponse{StatusCode: http.StatusInternalServerError}, nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestValidateDateRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		start     string
		end       string
		maxDays   int
		wantError bool
	}{
		{name: "one day", start: "2026-08-10", end: "2026-08-10", maxDays: 1},
		{name: "inclusive range", start: "2026-08-10", end: "2026-08-11", maxDays: 2},
		{name: "missing", start: "", end: "2026-08-11", maxDays: 2, wantError: true},
		{name: "bad format", start: "2026-8-10", end: "2026-08-11", maxDays: 2, wantError: true},
		{name: "reversed", start: "2026-08-12", end: "2026-08-11", maxDays: 2, wantError: true},
		{name: "too large", start: "2026-08-10", end: "2026-08-12", maxDays: 2, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := validateDateRange(test.start, test.end, test.maxDays)
			if (err != nil) != test.wantError {
				t.Fatalf("validateDateRange() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestQueryUsageUsesCodexCredentialAndSanitizesResponse(t *testing.T) {
	t.Parallel()
	host := &fakeHostClient{
		auths: []hostAuthFileEntry{{
			ID:        "codex-id",
			AuthIndex: "auth-index-1",
			Name:      "codex.json",
			Provider:  "codex",
			Email:     "admin@example.com",
		}},
		auth: hostAuthGetResponse{JSON: json.RawMessage(`{
			"type":"codex",
			"access_token":"secret-token",
			"account_id":"workspace-account"
		}`)},
		responses: []hostHTTPResponse{{
			StatusCode: http.StatusOK,
			Body: []byte(`{
				"data":[
					{"date":"2026-08-11","totals":{"credits":2.5,"turns":4,"users":1},"models":[{"secret":"ignored"}]},
					{"date":"2026-08-10","totals":{"credits":1.5,"turns":3,"text_total_tokens":100}}
				],
				"group_by":"day",
				"internal_workspace_id":"ignored"
			}`),
		}},
	}
	service := usageService{host: host, cfg: defaultPluginConfig(), sleep: func(time.Duration) {}}
	result, errQuery := service.queryUsage(url.Values{
		"auth_index": []string{"auth-index-1"},
		"start_date": []string{"2026-08-10"},
		"end_date":   []string{"2026-08-11"},
	}, "callback-7")
	if errQuery != nil {
		t.Fatalf("queryUsage() error = %v", errQuery)
	}
	if len(result.Data) != 2 || result.Data[0].Date != "2026-08-10" {
		t.Fatalf("result.Data = %#v, want sorted two-day result", result.Data)
	}
	if result.Summary.Credits == nil || *result.Summary.Credits != 4 || result.Summary.Turns == nil || *result.Summary.Turns != 7 {
		t.Fatalf("result.Summary = %#v, want summed metrics", result.Summary)
	}
	if result.Account.Email != "admin@example.com" || result.Account.AuthIndex != "auth-index-1" {
		t.Fatalf("result.Account = %#v", result.Account)
	}
	if len(host.requests) != 1 || host.callbackIDs[0] != "callback-7" {
		t.Fatalf("host calls = %d callback IDs = %#v", len(host.requests), host.callbackIDs)
	}
	request := host.requests[0]
	if request.Headers.Get("Authorization") != "Bearer secret-token" || request.Headers.Get("Chatgpt-Account-Id") != "workspace-account" {
		t.Fatalf("request headers did not contain selected credential")
	}
	if !strings.Contains(request.URL, "start_date=2026-08-10") || !strings.Contains(request.URL, "workspace_user=true") {
		t.Fatalf("request URL = %q", request.URL)
	}
	rawResult, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	for _, forbidden := range []string{"secret-token", "workspace-account", "internal_workspace_id", "models"} {
		if strings.Contains(string(rawResult), forbidden) {
			t.Fatalf("result leaked %q: %s", forbidden, rawResult)
		}
	}
}

func TestQueryUsageRetriesTransientStatus(t *testing.T) {
	t.Parallel()
	host := &fakeHostClient{
		auths: []hostAuthFileEntry{{AuthIndex: "a", Provider: "codex"}},
		auth:  hostAuthGetResponse{JSON: json.RawMessage(`{"type":"codex","access_token":"token","account_id":"account"}`)},
		responses: []hostHTTPResponse{
			{StatusCode: http.StatusTooManyRequests, Headers: http.Header{"Retry-After": []string{"1"}}},
			{StatusCode: http.StatusOK, Body: []byte(`{"data":[],"group_by":"day"}`)},
		},
	}
	var delays []time.Duration
	service := usageService{host: host, cfg: pluginConfig{MaxRangeDays: 90, MaxRetries: 1}, sleep: func(delay time.Duration) {
		delays = append(delays, delay)
	}}
	_, errQuery := service.queryUsage(url.Values{
		"auth_index": []string{"a"},
		"start_date": []string{"2026-08-10"},
		"end_date":   []string{"2026-08-10"},
	}, "")
	if errQuery != nil {
		t.Fatalf("queryUsage() error = %v", errQuery)
	}
	if len(host.requests) != 2 || len(delays) != 1 || delays[0] != time.Second {
		t.Fatalf("requests=%d delays=%v", len(host.requests), delays)
	}
}

func TestUpstreamErrorDoesNotExposeBody(t *testing.T) {
	t.Parallel()
	host := &fakeHostClient{
		auths: []hostAuthFileEntry{{AuthIndex: "a", Provider: "codex"}},
		auth:  hostAuthGetResponse{JSON: json.RawMessage(`{"type":"codex","access_token":"token","account_id":"account"}`)},
		responses: []hostHTTPResponse{{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(`{"workspace_id":"sensitive","message":"secret upstream detail"}`),
		}},
	}
	service := usageService{host: host, cfg: pluginConfig{MaxRangeDays: 90}, sleep: func(time.Duration) {}}
	response := service.handleUsage(url.Values{
		"auth_index": []string{"a"},
		"start_date": []string{"2026-08-10"},
		"end_date":   []string{"2026-08-10"},
	}, "")
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusBadGateway)
	}
	if strings.Contains(string(response.Body), "sensitive") || strings.Contains(string(response.Body), "secret upstream detail") {
		t.Fatalf("response leaked upstream body: %s", response.Body)
	}
}

func TestAccountsOnlyReturnsCodexMetadata(t *testing.T) {
	t.Parallel()
	host := &fakeHostClient{auths: []hostAuthFileEntry{
		{AuthIndex: "gemini", Provider: "gemini", Email: "ignored@example.com"},
		{AuthIndex: "codex", Provider: "codex", Email: "codex@example.com", Path: "/secret/path"},
	}}
	response := (usageService{host: host, cfg: defaultPluginConfig()}).handleAccounts()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if strings.Contains(string(response.Body), "ignored@example.com") || strings.Contains(string(response.Body), "/secret/path") {
		t.Fatalf("unsafe account response: %s", response.Body)
	}
	if !strings.Contains(string(response.Body), "codex@example.com") {
		t.Fatalf("missing Codex account: %s", response.Body)
	}
}
