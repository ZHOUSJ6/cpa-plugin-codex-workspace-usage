package main

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed web/index.html
var dashboardHTML []byte

func dashboardResponse() managementResponse {
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":            []string{"text/html; charset=utf-8"},
			"Cache-Control":           []string{"no-store"},
			"Content-Security-Policy": []string{"default-src 'none'; base-uri 'none'; connect-src 'self'; form-action 'none'; frame-ancestors *; img-src 'self' data:; script-src 'unsafe-inline'; style-src 'unsafe-inline'"},
			"Referrer-Policy":         []string{"no-referrer"},
			"X-Content-Type-Options":  []string{"nosniff"},
			"Permissions-Policy":      []string{"camera=(), microphone=(), geolocation=(), payment=(), usb=()"},
		},
		Body: append([]byte(nil), dashboardHTML...),
	}
}

func dashboardRequestAllowed(headers http.Header) bool {
	return strings.EqualFold(strings.TrimSpace(headers.Get("Sec-Fetch-Dest")), "iframe")
}

func dashboardAccessDeniedResponse() managementResponse {
	return managementResponse{
		StatusCode: http.StatusForbidden,
		Headers: http.Header{
			"Content-Type":            []string{"text/plain; charset=utf-8"},
			"Cache-Control":           []string{"no-store"},
			"Content-Security-Policy": []string{"default-src 'none'; frame-ancestors 'none'"},
			"Referrer-Policy":         []string{"no-referrer"},
			"X-Content-Type-Options":  []string{"nosniff"},
		},
		Body: []byte("403 Forbidden\nOpen this plugin from the CLIProxyAPI Management Center.\n"),
	}
}
