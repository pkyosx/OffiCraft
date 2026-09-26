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

// POST /api/chat/attachments is excluded from the MCP surface: this is how a
// file gets stored without its bytes riding the agent's LLM context as base64
// in post_chat. The server ignores the request Content-Type header, so none is
// set.

func uploadUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent upload <path> [--mime <type>]

Streams one local file into the station's attachment store and prints the id
it was stored under. Put that id in the `+"`attachments`"+` of post_chat (or a reply
card, or a task artifact) instead of pasting the file's contents.

--mime <type>  declare the media type. Without it the server decides: PNG,
               JPEG, GIF and WebP are recognised by their bytes, a name
               ending in .json is application/json, and everything else is
               application/octet-stream.

The stored filename is the path's basename. The flag may come before or after
<path>.

stdout on success, two lines:
  line 1  the attachment id (att-…)
  line 2  the server's JSON for it: {"id": …, "mime": …, "filename": …}
Diagnostics, including a one-line summary of what was stored, go to stderr.

Exit codes:
  0  stored
  1  the file could not be opened or is a directory, or the request never got
     an answer (network)
  2  usage: --help itself, an unknown flag, a flag missing its value, missing
     <path>, or more than one path
  3  no OC_TOKEN or no OC_BASE in the environment, or the server answered
     401/403
  4  the server refused the file (HTTP 400): it is empty, over the size limit,
     or its filename is too long. The limits are the server's; its message
     says which one was hit.
  5  any other HTTP status, or a 200 whose body is not an attachment ref
`)
}

type uploadedRef struct {
	ID       string `json:"id"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
	bodyJSON string
}

func cmdUpload(client httpClient, cfg Config, path, mimeType string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		fmt.Fprint(errOut, "[ocagent] upload: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}
	// OC_BASE CLASSIFICATION: GUARDED — refuse, exit 3.
	if warnMissingBase(cfg, "upload", errOut) {
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

	filename := filepath.Base(path)
	query := url.Values{}
	if name := strings.TrimSpace(filename); name != "" && name != "." && name != string(filepath.Separator) {
		query.Set("filename", name)
	}
	if declared := strings.TrimSpace(mimeType); declared != "" {
		query.Set("mime", declared)
	}
	reqURL := cfg.Base + "/api/chat/attachments?" + query.Encode()

	req, err := http.NewRequest(http.MethodPost, reqURL, f)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] upload: bad request for %q: %v\n", filename, err)
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

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	detail := strings.TrimSpace(string(raw))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] upload: auth rejected (HTTP %d) for %q: %s\n",
			resp.StatusCode, filename, detail)
		return 3
	case http.StatusBadRequest:
		fmt.Fprintf(errOut, "[ocagent] upload: server rejected %q (HTTP 400): %s\n",
			filename, detail)
		return 4
	default:
		fmt.Fprintf(errOut, "[ocagent] upload: unexpected HTTP %d for %q: %s\n",
			resp.StatusCode, filename, detail)
		return 5
	}

	var ref uploadedRef
	if err := json.Unmarshal(raw, &ref); err != nil || ref.ID == "" {
		fmt.Fprintf(errOut, "[ocagent] upload: 200 but unparseable ref body: %s\n", detail)
		return 5
	}
	ref.bodyJSON = detail

	fmt.Fprintf(errOut, "[ocagent] upload: %s (%d bytes, %s) → %s\n",
		ref.Filename, info.Size(), ref.Mime, ref.ID)
	fmt.Fprintln(out, ref.ID)
	fmt.Fprintln(out, ref.bodyJSON)
	return 0
}
