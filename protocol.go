package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// CLIProxyAPI's native plugin ABI is intentionally language-independent. Keep
// these wire types local so this plugin does not pull the entire CPA server
// dependency graph into a small management extension.
const (
	abiVersion    uint32 = 1
	schemaVersion uint32 = 1

	methodPluginRegister     = "plugin.register"
	methodPluginReconfigure  = "plugin.reconfigure"
	methodManagementRegister = "management.register"
	methodManagementHandle   = "management.handle"
	methodHostHTTPDo         = "host.http.do"
	methodHostAuthList       = "host.auth.list"
	methodHostAuthGet        = "host.auth.get"
)

type pluginMetadata struct {
	Name             string
	Version          string
	Author           string
	GitHubRepository string
	Logo             string
	ConfigFields     []configField
}

type configField struct {
	Name        string
	Type        string
	EnumValues  []string
	Description string
}

type hostAuthFileEntry struct {
	ID             string    `json:"id,omitempty"`
	AuthIndex      string    `json:"auth_index,omitempty"`
	Name           string    `json:"name"`
	Type           string    `json:"type,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	Label          string    `json:"label,omitempty"`
	Status         string    `json:"status,omitempty"`
	Disabled       bool      `json:"disabled,omitempty"`
	Unavailable    bool      `json:"unavailable,omitempty"`
	RuntimeOnly    bool      `json:"runtime_only,omitempty"`
	Source         string    `json:"source,omitempty"`
	Path           string    `json:"path,omitempty"`
	Size           int64     `json:"size,omitempty"`
	ModTime        time.Time `json:"mod_time,omitempty"`
	Email          string    `json:"email,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Priority       int       `json:"priority,omitempty"`
	Note           string    `json:"note,omitempty"`
	Websockets     bool      `json:"websockets,omitempty"`
	Success        int64     `json:"success,omitempty"`
	Failed         int64     `json:"failed,omitempty"`
	RecentRequests []any     `json:"recent_requests,omitempty"`
}

type hostAuthGetRequest struct {
	AuthIndex string `json:"auth_index"`
}

type hostAuthGetResponse struct {
	AuthIndex string          `json:"auth_index"`
	Name      string          `json:"name,omitempty"`
	Path      string          `json:"path,omitempty"`
	JSON      json.RawMessage `json:"json"`
}

type hostHTTPResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
