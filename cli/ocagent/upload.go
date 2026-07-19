package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// upload: ocagent upload <path> [--mime <type>]
// ---------------------------------------------------------------------------
//
// The SEND side of chat attachments — download's mirror twin. MCP `post_chat`
// can only carry file bytes as base64 inside a tool call, which drags the
// whole payload (inflated 4/3×) through the sending agent's LLM context and
// makes large files impossible. This subcommand is the official path around
// that: it STREAMS the file's bytes to POST /api/chat/attachments (a route
// excluded from the MCP surface — a binary ingest, not a tool) and prints the
// minted attachment id, so the agent then sends the message with a light
// `{id}` ref in post_chat's attachments — no byte ever rides its context.
//
// The request body is streamed straight from disk (an *os.File body; the
// http client sets Content-Length from the flag-checked stat) — never
// buffered in memory — using the agent's own token via the ordinary config
// seam (OC_TOKEN / OC_BASE), the same clean-identity contract as every other
// subcommand.
//
// Naming/typing: the file's BASENAME rides ?filename= (the server stores it
// and serves it back via Content-Disposition on download); --mime rides
// ?mime= when given, else the server sniffs (image magic bytes, fallback
// application/octet-stream). The request Content-Type header is ignored by
// the server (see the route's spec) so none is set here.
//
// Stdout on success (script-capturable, mirrors download's path-only stdout):
//   line 1: the minted attachment id
//   line 2: the server's light-ref JSON {id, mime, filename}
// Every diagnostic goes to stderr.
//
// Exit codes (documented so hooks/scripts can branch):
//   0 success
//   1 transport / filesystem failure (unreadable file, refused, DNS, timeout)
//   2 usage (bad flags / missing <path>) — set by realMain's FlagSet
//   3 auth (no token configured, or the server said 401/403)
//   4 rejected (400 — over the size cap, empty file)
//   5 any other unexpected HTTP status

// cmdUpload implements `ocagent upload`. On success stdout carries the minted
// attachment id then the server's light-ref JSON; diagnostics go to `errOut`.
func cmdUpload(client httpClient, cfg Config, path, mimeType string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		// Fail fast + honestly: without a token the server would 401 anyway, but
		// the local message ("mis-wired launch") beats a bare server status.
		fmt.Fprint(errOut, "[ocagent] upload: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] upload: cannot open %s: %v\n", path, err)
		return 1
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] upload: cannot stat %s: %v\n", path, err)
		return 1
	}
	if info.IsDir() {
		fmt.Fprintf(errOut, "[ocagent] upload: %s is a directory, not a file\n", path)
		return 1
	}

	query := url.Values{}
	if name := filepath.Base(path); name != "" && name != "." && name != string(filepath.Separator) {
		query.Set("filename", name)
	}
	if strings.TrimSpace(mimeType) != "" {
		query.Set("mime", strings.TrimSpace(mimeType))
	}
	reqURL := cfg.Base + "/api/chat/attachments?" + query.Encode()

	// An *os.File body streams: the transport reads from disk chunk by chunk
	// and stamps Content-Length from the explicit ContentLength below.
	req, err := http.NewRequest(http.MethodPost, reqURL, f)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] upload: bad request for %q: %v\n", path, err)
		return 1
	}
	req.ContentLength = info.Size()
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] upload: request failed (network): %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	// The response is a small JSON ref either way — bounded read for hygiene.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	detail := strings.TrimSpace(string(raw))

	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through to the success path below
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] upload: auth rejected (HTTP %d) for %q: %s\n",
			resp.StatusCode, path, detail)
		return 3
	case resp.StatusCode == http.StatusBadRequest:
		fmt.Fprintf(errOut, "[ocagent] upload: server rejected %q (HTTP 400): %s\n",
			path, detail)
		return 4
	default:
		fmt.Fprintf(errOut, "[ocagent] upload: unexpected HTTP %d for %q: %s\n",
			resp.StatusCode, path, detail)
		return 5
	}

	var ref struct {
		ID       string `json:"id"`
		Mime     string `json:"mime"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil || ref.ID == "" {
		fmt.Fprintf(errOut, "[ocagent] upload: 200 but unparseable ref body: %s\n", detail)
		return 5
	}
	fmt.Fprintf(errOut, "[ocagent] upload: %s (%d bytes, %s) → %s\n",
		ref.Filename, info.Size(), ref.Mime, ref.ID)
	fmt.Fprintln(out, ref.ID)
	fmt.Fprintln(out, detail)
	return 0
}
