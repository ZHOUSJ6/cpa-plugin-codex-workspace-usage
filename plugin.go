package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	pluginID      = "codex-workspace-usage"
	accountsPath  = "/v0/management/codex-workspace-usage/accounts"
	usagePath     = "/v0/management/codex-workspace-usage"
	dashboardPath = "/v0/resource/plugins/codex-workspace-usage/dashboard"
)

// pluginVersion is a variable so tagged release builds can inject the exact
// version with -ldflags without changing source between platforms.
var pluginVersion = "0.3.0"

var currentConfig atomic.Value

func init() {
	currentConfig.Store(defaultPluginConfig())
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type pluginConfig struct {
	MaxRangeDays int `yaml:"max_range_days"`
	MaxRetries   int `yaml:"max_retries"`
}

type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      pluginMetadata           `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

type registrationCapabilities struct {
	ManagementAPI bool `json:"management_api"`
}

type managementRegistration struct {
	Routes    []managementRoute `json:"routes"`
	Resources []resourceRoute   `json:"resources,omitempty"`
}

type managementRoute struct {
	Method string `json:"Method"`
	Path   string `json:"Path"`
}

type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementRequest struct {
	Method         string
	Path           string
	Headers        http.Header
	Query          url.Values
	Body           []byte
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type hostHTTPRequest struct {
	HostCallbackID string      `json:"host_callback_id,omitempty"`
	Method         string      `json:"method,omitempty"`
	URL            string      `json:"url,omitempty"`
	Headers        http.Header `json:"headers,omitempty"`
	Body           []byte      `json:"body,omitempty"`
}

type hostClient interface {
	ListAuths() ([]hostAuthFileEntry, error)
	GetAuth(authIndex string) (hostAuthGetResponse, error)
	DoHTTP(callbackID string, request hostHTTPRequest) (hostHTTPResponse, error)
}

func handleMethod(method string, request []byte, host hostClient) ([]byte, error) {
	switch method {
	case methodPluginRegister, methodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case methodManagementRegister:
		return okEnvelope(managementRegistration{
			Routes: []managementRoute{
				{Method: http.MethodGet, Path: usagePath},
				{Method: http.MethodGet, Path: accountsPath},
			},
			Resources: []resourceRoute{{
				Path:        "/dashboard",
				Menu:        "Codex 用量",
				Description: "查看 Codex Workspace 每日令牌、会话和 Credits 用量。",
			}},
		})
	case methodManagementHandle:
		return handleManagement(request, host)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func configure(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return fmt.Errorf("decode plugin lifecycle request: %w", errUnmarshal)
		}
	}

	cfg := defaultPluginConfig()
	if len(req.ConfigYAML) > 0 {
		if errDecode := decodePluginConfig(req.ConfigYAML, &cfg); errDecode != nil {
			return errDecode
		}
	}
	if errValidate := cfg.validate(); errValidate != nil {
		return errValidate
	}
	currentConfig.Store(cfg)
	return nil
}

func decodePluginConfig(raw []byte, cfg *pluginConfig) error {
	if cfg == nil {
		return fmt.Errorf("plugin config target is nil")
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// Plugin-owned fields are top-level scalars. Nested store metadata is
		// intentionally ignored instead of being interpreted as plugin config.
		if strings.TrimLeft(line, " \t") != line {
			continue
		}
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = line[:comment]
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "max_range_days", "max_retries":
			parsed, errParse := strconv.Atoi(value)
			if errParse != nil {
				return fmt.Errorf("decode plugin config line %d: %s must be an integer", lineNumber, key)
			}
			if key == "max_range_days" {
				cfg.MaxRangeDays = parsed
			} else {
				cfg.MaxRetries = parsed
			}
		}
	}
	if errScan := scanner.Err(); errScan != nil {
		return fmt.Errorf("decode plugin config: %w", errScan)
	}
	return nil
}

func defaultPluginConfig() pluginConfig {
	return pluginConfig{MaxRangeDays: 90, MaxRetries: 2}
}

func (cfg pluginConfig) validate() error {
	if cfg.MaxRangeDays < 1 || cfg.MaxRangeDays > 366 {
		return fmt.Errorf("max_range_days must be between 1 and 366")
	}
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 5 {
		return fmt.Errorf("max_retries must be between 0 and 5")
	}
	return nil
}

func loadedConfig() pluginConfig {
	if raw := currentConfig.Load(); raw != nil {
		if cfg, ok := raw.(pluginConfig); ok {
			return cfg
		}
	}
	return defaultPluginConfig()
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: schemaVersion,
		Metadata: pluginMetadata{
			Name:             "Codex Workspace Usage",
			Version:          pluginVersion,
			Author:           "ZHOUSJ6",
			GitHubRepository: "https://github.com/ZHOUSJ6/cpa-plugin-codex-workspace-usage",
			ConfigFields: []configField{
				{
					Name:        "max_range_days",
					Type:        "integer",
					Description: "Maximum inclusive date range accepted by one query (1-366).",
				},
				{
					Name:        "max_retries",
					Type:        "integer",
					Description: "Retry count for rate limits, transient upstream errors, and transport failures (0-5).",
				},
			},
		},
		Capabilities: registrationCapabilities{ManagementAPI: true},
	}
}

func handleManagement(raw []byte, host hostClient) ([]byte, error) {
	var req managementRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return nil, fmt.Errorf("decode management request: %w", errUnmarshal)
		}
	}
	if !strings.EqualFold(req.Method, http.MethodGet) {
		return okEnvelope(jsonManagementError(http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported", 0))
	}

	service := usageService{host: host, cfg: loadedConfig()}
	switch strings.TrimRight(req.Path, "/") {
	case dashboardPath:
		return okEnvelope(dashboardResponse())
	case accountsPath:
		return okEnvelope(service.handleAccounts())
	case usagePath:
		return okEnvelope(service.handleUsage(req.Query, req.HostCallbackID))
	default:
		return okEnvelope(jsonManagementError(http.StatusNotFound, "route_not_found", "plugin route not found", 0))
	}
}

func jsonManagementResponse(status int, payload any) managementResponse {
	body, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		body = []byte(`{"error":"response_encode_failed","message":"failed to encode plugin response"}`)
		status = http.StatusInternalServerError
	}
	return managementResponse{
		StatusCode: status,
		Headers: http.Header{
			"Content-Type":  []string{"application/json; charset=utf-8"},
			"Cache-Control": []string{"no-store"},
		},
		Body: body,
	}
}

func jsonManagementError(status int, code, message string, upstreamStatus int) managementResponse {
	payload := apiErrorBody{Error: code, Message: message}
	if upstreamStatus > 0 {
		payload.UpstreamStatus = upstreamStatus
	}
	return jsonManagementResponse(status, payload)
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}
