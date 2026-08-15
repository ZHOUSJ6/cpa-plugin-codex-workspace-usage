package main

import (
	_ "embed"
	"net/http"
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
