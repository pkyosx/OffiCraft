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

// GET /api/chat/attachment/<id> is excluded from the MCP surface: this is the
// agent's only way to land an attachment as a file, since large ones must never
// ride a tool-result as base64.

// Relative to the agent workdir, which is the cwd because the spawn shim starts
// the agent there.
const downloadDefaultSubdir = "tmp/attachments"

const (
	clientDialTimeout   = 10 * time.Second
	clientHeaderTimeout = 30 * time.Second
)

func downloadUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent download <attachment-id> [--out <dir>]

Fetches one stored attachment (an att-… id from a chat message, a reply card
or a task artifact) and writes it to a local file, streamed straight to disk.

--out <dir>  directory to write into, created if missing. Default:
             tmp/attachments/ under the current directory.

The file is named after the attachment's stored filename, reduced to its last
path component; an image, or an attachment with no usable name, is named after
its id. A file already at that path is overwritten. The flag may come before
or after the id.

stdout on success: one line, the absolute path of the written file.
Diagnostics, including a one-line summary of what was written, go to stderr.

Exit codes:
  0  written
  1  the request never got an answer (network), or the directory or file could
     not be created or written (a partial file is removed)
  2  usage: --help itself, an unknown flag, a flag missing its value, missing
     <attachment-id>, or more than one argument
  3  no OC_TOKEN or no OC_BASE in the environment, or the server answered
     401/403
  4  HTTP 404: no attachment with that id on the station OC_BASE points at
     (which may be the wrong station)
  5  any other HTTP status
`)
}

// Shared by download, upload and diff (main.go). Timeout stays 0 on purpose: a
// large body on a slow link must not be cut mid-stream by a wall-clock limit.
func newNoDeadlineClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: clientDialTimeout}).DialContext,
			TLSHandshakeTimeout:   clientDialTimeout,
			ResponseHeaderTimeout: clientHeaderTimeout,
		},
	}
}

func cmdDownload(client httpClient, cfg Config, attachmentID, outDir string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		fmt.Fprint(errOut, "[ocagent] download: no OC_TOKEN configured — cannot make an authed fetch.\n")
		return 3
	}
	// OC_BASE CLASSIFICATION: GUARDED — refuse, exit 3.
	if warnMissingBase(cfg, "download", errOut) {
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
	written, err := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dest)
		fmt.Fprintf(errOut, "[ocagent] download: write to %s failed: %v — partial file removed\n", dest, err)
		return 1
	}

	abs, absErr := filepath.Abs(dest)
	if absErr != nil {
		abs = dest
	}
	fmt.Fprintf(errOut, "[ocagent] download: %s (%d bytes, %s)\n",
		name, written, resp.Header.Get("Content-Type"))
	fmt.Fprintln(out, abs)
	return 0
}

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

func sanitizeFilename(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		if i := strings.LastIndexByte(name, '\\'); i >= 0 {
			name = name[i+1:]
		}
		name = filepath.Base(name)
	}
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return fallback
	}
	return name
}
