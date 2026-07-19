package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// download: ocagent download <attachment-id> [--out <dir>]
// ---------------------------------------------------------------------------
//
// The RECEIVE side of chat attachments. MCP `post_chat` lets an agent SEND a
// file, and `list_chat` surfaces the light attachment refs ({id, filename,
// mime}) of what it received — but the blob bytes themselves live behind
// GET /api/chat/attachment/<id>, which is excluded from the MCP surface (a
// binary fetch, not a tool). This subcommand is the official path for an agent
// to land a received attachment as a LOCAL FILE it can then read/unzip —
// including large ones (a zip up to the server's 100 MB non-image cap), which
// must never ride a tool-result as base64.
//
// The response body is STREAMED straight to disk (io.Copy) — never buffered in
// memory — using the agent's own token via the ordinary config seam (OC_TOKEN /
// OC_BASE), the same clean-identity contract as every other subcommand.
//
// Filename: the server names the blob via Content-Disposition (RFC 5987 —
// `filename*=UTF-8''…` carries the true name, `filename="…"` an ASCII
// fallback); an IMAGE is served with no disposition at all, so the id is used.
// Whatever the server sends is reduced to its BASENAME before use — a
// hostile/odd `filename="../../x"` can never traverse out of the target dir.
//
// Exit codes (documented so hooks/scripts can branch):
//   0 success (the landed ABSOLUTE path is the only stdout output)
//   1 transport / filesystem failure (refused, DNS, timeout, write fault)
//   2 usage (bad flags / missing <attachment-id>) — set by realMain's FlagSet
//   3 auth (no token configured, or the server said 401/403)
//   4 not found (404 — unknown id, or a blob outside this owner's scope)
//   5 any other unexpected HTTP status

// downloadDefaultSubdir is the default landing directory, relative to the agent
// workdir (its cwd — the spawn shim starts the agent there): tmp/attachments/.
const downloadDefaultSubdir = "tmp/attachments"

const (
	downloadDialTimeout   = 10 * time.Second
	downloadHeaderTimeout = 30 * time.Second
)

// newStreamingClient builds the HTTP client both blob directions share
// (download's fetch, upload's send). Total Timeout is deliberately 0 — a
// multi-megabyte body on a slow link must not be cut mid-stream by a
// wall-clock deadline (the same reasoning as the SSE stream client); connect +
// response-header phases keep their own bounded timeouts so a dead server
// still fails fast.
func newStreamingClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: downloadDialTimeout}).DialContext,
			TLSHandshakeTimeout:   downloadDialTimeout,
			ResponseHeaderTimeout: downloadHeaderTimeout,
		},
	}
}

// cmdDownload implements `ocagent download`. On success the landed file's
// ABSOLUTE path is the only thing printed to `out` (stdout) so a caller can
// capture it; every diagnostic goes to `errOut` (stderr). outDir=="" resolves
// to <cwd>/tmp/attachments (the agent workdir convention).
func cmdDownload(client httpClient, cfg Config, attachmentID, outDir string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		// Fail fast + honestly: without a token the server would 401 anyway, but
		// the local message ("mis-wired launch") beats a bare server status.
		fmt.Fprint(errOut, "[ocagent] download: no OC_TOKEN configured — cannot make an authed fetch.\n")
		return 3
	}

	reqURL := cfg.Base + "/api/chat/attachment/" + url.PathEscape(attachmentID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] download: bad request for %q: %v\n", attachmentID, err)
		return 1
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] download: request failed (network): %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain a short body snippet for the diagnostic (the server sends a JSON
		// detail) — bounded so a huge error page can't balloon memory.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		detail := strings.TrimSpace(string(snippet))
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			fmt.Fprintf(errOut, "[ocagent] download: auth rejected (HTTP %d) for %q: %s\n",
				resp.StatusCode, attachmentID, detail)
			return 3
		case http.StatusNotFound:
			fmt.Fprintf(errOut, "[ocagent] download: attachment %q not found (HTTP 404): %s\n",
				attachmentID, detail)
			return 4
		default:
			fmt.Fprintf(errOut, "[ocagent] download: unexpected HTTP %d for %q: %s\n",
				resp.StatusCode, attachmentID, detail)
			return 5
		}
	}

	dir := outDir
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "[ocagent] download: cannot resolve the agent workdir: %v\n", err)
			return 1
		}
		dir = filepath.Join(cwd, filepath.FromSlash(downloadDefaultSubdir))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(errOut, "[ocagent] download: cannot create %s: %v\n", dir, err)
		return 1
	}

	name := sanitizeFilename(
		filenameFromDisposition(resp.Header.Get("Content-Disposition")),
		sanitizeFilename(attachmentID, "attachment"),
	)
	dest := filepath.Join(dir, name)

	f, err := os.Create(dest)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] download: cannot create %s: %v\n", dest, err)
		return 1
	}
	written, err := io.Copy(f, resp.Body) // STREAM to disk — never buffer the blob
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dest) // never leave a truncated half-file behind
		fmt.Fprintf(errOut, "[ocagent] download: write to %s failed: %v — partial file removed\n", dest, err)
		return 1
	}

	abs, absErr := filepath.Abs(dest)
	if absErr != nil { // filepath.Abs only fails when Getwd does; dest is then already absolute-ish
		abs = dest
	}
	fmt.Fprintf(errOut, "[ocagent] download: %s (%d bytes, %s)\n",
		name, written, resp.Header.Get("Content-Type"))
	fmt.Fprintln(out, abs)
	return 0
}

// filenameFromDisposition extracts the served filename from a Content-Disposition
// header, PREFERRING the RFC 5987 `filename*=UTF-8”<pct-encoded>` parameter (the
// true, possibly non-ASCII name the server always sends alongside the ASCII
// fallback) over the plain `filename="…"`. Returns "" when the header is absent
// (an image serves with no disposition) or carries no usable name — the caller
// then falls back to the attachment id.
func filenameFromDisposition(disp string) string {
	if disp == "" {
		return ""
	}
	const star = "filename*=UTF-8''"
	if i := strings.Index(disp, star); i >= 0 {
		v := disp[i+len(star):]
		if j := strings.IndexByte(v, ';'); j >= 0 {
			v = v[:j]
		}
		if dec, err := url.PathUnescape(strings.TrimSpace(v)); err == nil && dec != "" {
			return dec
		}
	}
	const plain = `filename="`
	if i := strings.Index(disp, plain); i >= 0 {
		v := disp[i+len(plain):]
		if j := strings.IndexByte(v, '"'); j >= 0 {
			return v[:j]
		}
	}
	return ""
}

// sanitizeFilename reduces a server-supplied (i.e. UNTRUSTED — another member
// chose it) filename to a safe single path component: basename only (both
// slash flavours), never empty / "." / ".." — those degrade to `fallback`.
// This is the path-traversal guard: whatever the header says, the file lands
// INSIDE the target directory.
func sanitizeFilename(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		if i := strings.LastIndexByte(name, '\\'); i >= 0 { // windows-style separators too
			name = name[i+1:]
		}
		name = filepath.Base(name)
	}
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return fallback
	}
	return name
}
