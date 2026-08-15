package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestConfigureAndRegister(t *testing.T) {
	raw, errMarshal := json.Marshal(lifecycleRequest{ConfigYAML: []byte("max_range_days: 31\nmax_retries: 1\n")})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	response, errHandle := handleMethod("plugin.register", raw, &fakeHostClient{})
	if errHandle != nil {
		t.Fatalf("handleMethod() error = %v", errHandle)
	}
	if !loadedConfigEqual(pluginConfig{MaxRangeDays: 31, MaxRetries: 1}) {
		t.Fatalf("loaded config = %#v", loadedConfig())
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(response, &env); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if !env.OK {
		t.Fatalf("registration envelope = %#v", env)
	}
	var registration registration
	if errUnmarshal := json.Unmarshal(env.Result, &registration); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if registration.Metadata.GitHubRepository == "" || !registration.Capabilities.ManagementAPI {
		t.Fatalf("registration is not host-valid: %#v", registration)
	}
}

func TestManagementRegistrationDeclaresRoutesAndDashboardMenu(t *testing.T) {
	response, errHandle := handleMethod("management.register", nil, &fakeHostClient{})
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(response, &env); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	var registration managementRegistration
	if errUnmarshal := json.Unmarshal(env.Result, &registration); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if len(registration.Routes) != 2 {
		t.Fatalf("routes = %#v", registration.Routes)
	}
	for _, route := range registration.Routes {
		if route.Method != http.MethodGet {
			t.Fatalf("route method = %q", route.Method)
		}
	}
	if len(registration.Resources) != 1 {
		t.Fatalf("resources = %#v, want one dashboard menu", registration.Resources)
	}
	resource := registration.Resources[0]
	if resource.Path != "/dashboard" || resource.Menu != "Codex 用量" || resource.Description == "" {
		t.Fatalf("dashboard resource = %#v", resource)
	}
}

func TestDashboardResourceReturnsHardenedHTML(t *testing.T) {
	request, errMarshal := json.Marshal(managementRequest{Method: http.MethodGet, Path: dashboardPath})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	response, errHandle := handleMethod(methodManagementHandle, request, &fakeHostClient{})
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(response, &env); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	var dashboard managementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &dashboard); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if dashboard.StatusCode != http.StatusOK || dashboard.Headers.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("dashboard response = %#v", dashboard)
	}
	if !strings.Contains(dashboard.Headers.Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("dashboard CSP = %q", dashboard.Headers.Get("Content-Security-Policy"))
	}
	body := string(dashboard.Body)
	for _, expected := range []string{"Codex 用量观测台", usagePath, "Authorization"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("dashboard HTML missing %q", expected)
		}
	}
}

func loadedConfigEqual(want pluginConfig) bool {
	got := loadedConfig()
	return got.MaxRangeDays == want.MaxRangeDays && got.MaxRetries == want.MaxRetries
}
