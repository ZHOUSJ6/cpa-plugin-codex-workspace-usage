# Codex Workspace Usage

[![Build](https://github.com/ZHOUSJ6/cpa-plugin-codex-workspace-usage/actions/workflows/build.yml/badge.svg)](https://github.com/ZHOUSJ6/cpa-plugin-codex-workspace-usage/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/ZHOUSJ6/cpa-plugin-codex-workspace-usage)](https://github.com/ZHOUSJ6/cpa-plugin-codex-workspace-usage/releases)

`codex-workspace-usage` is a CLIProxyAPI v7 native plugin that queries the daily Codex workspace usage endpoint used by the ChatGPT Codex Analytics page. It adds a dedicated **Codex 用量** menu to the official Management Center and also exposes authenticated JSON APIs for automation.

The plugin exposes authenticated CPA Management API routes. It reads an existing Codex OAuth credential through the CPA host callbacks for each request; access tokens and account IDs are never stored in plugin configuration or returned to API callers.

> The upstream `chatgpt.com/backend-api` endpoint is private and observational. OpenAI may change its path, authentication, parameters, or response fields without notice. Prefer the official Codex Analytics API for long-lived automation when it is available to your workspace.

## Install

After the plugin is accepted into the official [CLIProxyAPI Plugin Store](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store), open the Plugin Store in the CLIProxyAPI Management Center, search for **Codex Workspace Usage**, and install the matching package. CLIProxyAPI can load an installed plugin online without stopping the service.

Until then, download the archive matching your operating system and CPU architecture from [GitHub Releases](https://github.com/ZHOUSJ6/cpa-plugin-codex-workspace-usage/releases). Verify it against `checksums.txt`, extract the library at the ZIP root, and place it in either location:

```text
plugins/<GOOS>/<GOARCH>/codex-workspace-usage.<ext>
plugins/codex-workspace-usage.<ext>
```

The release archives use the official store naming convention:

```text
codex-workspace-usage_<version>_<goos>_<goarch>.zip
```

## Build

Requirements:

- Go 1.26+
- CGO toolchain for the target platform
- CLIProxyAPI v7 SDK

```bash
make test
make build
make package
```

The current-platform dynamic library is written to `dist/`:

```text
dist/codex-workspace-usage.dylib  # macOS
dist/codex-workspace-usage.so     # Linux
dist/codex-workspace-usage.dll    # Windows
```

Tagged releases build Linux, macOS, Windows, and FreeBSD packages using native GitHub-hosted runners where available. The workflow publishes amd64 and arm64 packages for Linux, macOS, and Windows, plus FreeBSD amd64.

## CLIProxyAPI configuration

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    codex-workspace-usage:
      enabled: true
      priority: 1
      max_range_days: 90
      max_retries: 2
```

Configuration fields:

- `max_range_days`: maximum inclusive date range accepted by one query, from 1 to 366.
- `max_retries`: retries for HTTP 429, transient 5xx responses, and transport failures, from 0 to 5.

## Management API

All routes are under `/v0/management`, so normal CLIProxyAPI Management API authentication is required. Responses include `Cache-Control: no-store`.

### List usable Codex credentials

```http
GET /v0/management/codex-workspace-usage/accounts
```

The response contains safe credential metadata such as `auth_index`, label, email, and status. It does not expose filesystem paths, OAuth tokens, refresh tokens, or ChatGPT account IDs.

### Query daily workspace usage

```http
GET /v0/management/codex-workspace-usage?auth_index=<AUTH_INDEX>&start_date=2026-08-10&end_date=2026-08-11
```

Example response:

```json
{
  "account": {
    "auth_index": "...",
    "email": "admin@example.com",
    "disabled": false,
    "unavailable": false
  },
  "start_date": "2026-08-10",
  "end_date": "2026-08-11",
  "group_by": "day",
  "data": [
    {
      "date": "2026-08-10",
      "totals": {
        "credits": 12.5,
        "text_total_tokens": 1000000,
        "turns": 20
      }
    }
  ],
  "summary": {
    "credits": 12.5,
    "text_total_tokens": 1000000,
    "turns": 20
  }
}
```

Only documented allow-listed usage fields are returned. Optional upstream `clients`, `models`, workspace identifiers, and unknown fields are discarded. Missing dates are not synthesized, and missing metrics remain absent rather than being converted to zero.

`users` and `threads` are summed in `summary` for display convenience, but they retain the upstream daily-aggregation semantics: summed users are not unique users across the entire date range, and the precise Threads definition is not public.

## Management Center dashboard

When the plugin is enabled, the official CLIProxyAPI Management Center discovers its resource metadata and adds a dedicated **Codex 用量** menu. The dashboard is served from:

```text
/v0/resource/plugins/codex-workspace-usage/dashboard
```

The page provides:

- Automatic reuse of the current Management Center session when opened from the plugin menu.
- Automatic light, white, dark, and system-theme synchronization with the Management Center.
- Codex account selection and 7/30/90-day range shortcuts.
- Total Tokens, Turns, Credits, and returned-day summary cards.
- Daily Token trend, input/output composition, peak-day signal, and daily detail table.
- Responsive iframe and standalone layouts with no external frontend dependencies.

CLIProxyAPI resource pages are intentionally browser-navigable and are not Management API-authenticated. The dashboard therefore contains only static HTML, CSS, and JavaScript. When embedded by the official Management Center, it reads the same-origin `cli-proxy-auth` session already maintained by the panel and uses that session only for same-server `/v0/management/codex-workspace-usage...` requests. This follows the established plugin integration used by other Management Center plugins.

The plugin does not write, copy, or independently persist the management key. If the panel has no saved key, or if the dashboard is opened directly instead of through the Management Center, a compact temporary authentication fallback is shown. The fallback key remains only in the current page's JavaScript memory.

## Security notes

- Install the plugin only in a trusted CLIProxyAPI process; native plugins execute in-process.
- OAuth material is read only when a query is made and is not included in plugin responses or error messages.
- The dashboard resource contains no account or usage data by itself. All data requests still pass through Management API authentication.
- Upstream error bodies are never forwarded because they may contain workspace, user, model, or internal identifiers.
- Outbound requests use CPA's `host.http.do` bridge, so the configured proxy and transport policy are retained. CLIProxyAPI masks sensitive authorization header values in request logs.
- The plugin does not refresh credentials itself. If the selected Codex credential has expired, let CLIProxyAPI refresh it or re-authenticate the account, then retry.

## License

This project is licensed under the [MIT License](LICENSE).
