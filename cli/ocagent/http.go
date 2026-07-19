package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// http (the single network seam — mirrors agent/oc_agent.py http_request /
// get_json / post_json, and ocwarden/main.go httpPoster)
// ---------------------------------------------------------------------------
//
// SHARED-CODE NOTE: this is the agent-side twin of ocwarden's httpPoster. The
// warden only ever POSTs telemetry, so it exposes a POST-only `Poster`; the
// agent CLI additionally needs authed GETs (roster reads GET /api/members), so
// this seam mirrors the fuller Python surface (http_request → get_json/post_json)
// rather than warden's narrower Poster. Same Bearer/UA/Accept header contract.

const userAgent = "ocagent/0.1" // matches agent/oc_agent.py _UA exactly

const httpTimeout = 10 * time.Second // matches http_request default timeout

// httpClient is the injectable network seam. Tests point Config.Base at an
// httptest.Server, so a plain default client exercises the whole chain with no
// real network.
type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

// defaultHTTPClient is the real client used outside tests.
func defaultHTTPClient() *http.Client { return &http.Client{Timeout: httpTimeout} }

// httpRequest issues ONE HTTP request and returns (status, bodyText), mirroring
// agent/oc_agent.py http_request. A Bearer token + explicit User-Agent ride
// every request. A transport failure (DNS/refused/timeout) surfaces as
// (0, reason) so callers branch on a falsy status; an HTTP error status (e.g.
// 401) returns its code + body (an auth failure is data, not an exception).
func httpRequest(client httpClient, method, url, token string, jsonBody any) (int, string) {
	var body io.Reader
	if jsonBody != nil {
		raw, err := json.Marshal(jsonBody)
		if err != nil {
			return 0, err.Error()
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error() // connection refused / DNS / timeout — a falsy status
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// getJSON GETs path and parses the body as JSON → (status, obj-or-nil). Mirrors
// get_json's authed kwarg: authed=true rides the agent's own Bearer token (the
// default for gated reads like /api/members); authed=false sends NO token, for a
// PUBLIC route (bootstrap's GET /api/version). A non-JSON / empty body parses to
// nil.
func getJSON(client httpClient, cfg Config, path string, authed bool) (int, any) {
	token := ""
	if authed {
		token = cfg.Token
	}
	status, text := httpRequest(client, http.MethodGet, cfg.Base+path, token, nil)
	return status, safeJSON(text)
}

// postJSON POSTs payload (JSON) to path → (status, obj-or-nil). Mirrors
// post_json (always authed with the agent's token).
func postJSON(client httpClient, cfg Config, path string, payload any) (int, any) {
	status, text := httpRequest(client, http.MethodPost, cfg.Base+path, cfg.Token, payload)
	return status, safeJSON(text)
}

// safeJSON parses text as JSON, returning nil on any failure (mirrors
// _safe_json). Never panics.
func safeJSON(text string) any {
	var obj any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil
	}
	return obj
}
