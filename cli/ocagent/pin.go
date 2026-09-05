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
// pin: ocagent pin <path> --task <task-id> --name <name>
//                        [--description <text>] [--mime <type>]
//                        [--replace <artifact-id>]
// ---------------------------------------------------------------------------
//
// The CLI handle on T-92's ONE-CALL artifact doors — the two routes that take
// raw bytes and hand back a pinned deliverable:
//
//	POST /api/tasks/{task_id}/artifacts/upload                        (add)
//	POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload   (replace)
//
// WHY A SUBCOMMAND AT ALL, given `upload` + the add_task_artifact tool already
// get a local file onto a task card. They do it in TWO steps with a gap in the
// middle: store the blob, then bind it. A caller that takes the first step and
// not the second leaves a blob nothing references and nothing goes looking for
// — the collector revisits blobs a delete put on its candidate list, it is not
// a sweep. These routes store and pin inside ONE transaction, so either both
// land or neither does; without this subcommand nothing shipped in this repo
// could reach them (they carry x-mcp include:false because a binary body cannot
// ride inside a JSON tool call, and the frontend does not call them).
//
// ONE SUBCOMMAND FOR BOTH ROUTES, not two: they are the same request — raw
// octet-stream body, the same four query parameters — differing only in the
// path and in whether `name` is required. `--replace <artifact-id>` names the
// artifact whose CONTENT is being swapped; without it a new artifact is pinned.
//
// Contract notes, read off spec/openapi.json rather than guessed:
//   - the body IS the bytes (application/octet-stream) — NOT multipart, NOT
//     base64. Streamed straight from disk like `upload`, never buffered.
//   - ?name= is REQUIRED on the ADD route (48 runes, blank refused, refused
//     rather than truncated) and OPTIONAL on the replace route, where an
//     omitted one is carried forward. This subcommand refuses a missing --name
//     locally for an add, so a mis-typed invocation costs no upload.
//   - ?description= is optional on both (256 runes), carried forward on replace.
//   - ?filename= and ?mime= describe the BLOB exactly as on the chat-attachment
//     upload: the basename rides filename automatically, --mime rides mime only
//     when given, else the server sniffs (image magic bytes, then
//     application/octet-stream).
//   - `kind` is NOT a parameter: an image mime pins image, anything else file.
//     The replace route refuses a kind change (and refuses a link artifact).
//   - the request Content-Type header is deliberately ignored by the server, so
//     none is set here — the same reason `upload` sets none.
//
// Stdout on success (script-capturable, mirrors upload's two lines):
//
//	line 1: the artifact id (the NEW one on an add; the unchanged one on a replace)
//	line 2: the server's receipt JSON verbatim
//
// Every diagnostic goes to stderr.
//
// Exit codes (documented so hooks/scripts can branch — upload's, extended by
// the two refusals only a task-scoped write can make):
//
//	0 success
//	1 transport / filesystem failure (unreadable file, refused, DNS, timeout)
//	2 usage (bad flags / missing <path> / missing --task / missing --name) — realMain's FlagSet
//	3 auth (no token configured, or the server said 401/403 — 403 is the executor guard)
//	4 refused (400 bad request / over the size cap, 404 unknown task or artifact,
//	  409 the task is terminal and its deliverables are frozen)
//	5 any other unexpected HTTP status

// artifactReceipt is the bounded receipt both one-call routes answer with. The
// add route returns TaskArtifactReceiptDTO (three fields); the replace route
// returns TaskArtifactReplaceReceiptDTO, which is the same three plus
// version_count. One struct reads both — VersionCount stays 0 on an add.
type artifactReceipt struct {
	TaskID        string `json:"task_id"`
	ArtifactID    string `json:"artifact_id"`
	ArtifactCount int    `json:"artifact_count"`
	VersionCount  int    `json:"version_count"`
	// The response body verbatim — stdout line 2 is the SERVER's JSON, not a
	// re-serialisation of the fields above, so a field this build does not know
	// about still reaches whoever is reading the output.
	raw string
}

// cmdPin implements `ocagent pin`: it opens one path and streams its bytes into
// the task-scoped one-call artifact route — the add route, or the replace twin
// when replaceID is non-empty. On success stdout carries the artifact id then
// the server's receipt JSON; diagnostics go to `errOut`.
func cmdPin(client httpClient, cfg Config, path, taskID, replaceID, name, description, mimeType string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		// Fail fast + honestly: without a token the server would 401 anyway, but
		// the local message ("mis-wired launch") beats a bare server status.
		fmt.Fprint(errOut, "[ocagent] pin: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}
	// OC_BASE CLASSIFICATION: GUARDED — refuse, exit 3. Same mis-wire class as
	// upload/download/diff: without OC_BASE the request would be built against
	// this machine's loopback address and the pin would quietly go nowhere.
	if requireBase(cfg, "pin", errOut) {
		return 3
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] pin: cannot open %s: %v\n", path, err)
		return 1
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] pin: cannot stat %s: %v\n", path, err)
		return 1
	}
	if info.IsDir() {
		fmt.Fprintf(errOut, "[ocagent] pin: %s is a directory, not a file\n", path)
		return 1
	}

	filename := filepath.Base(path)
	query := url.Values{}
	if n := strings.TrimSpace(name); n != "" {
		query.Set("name", n)
	}
	// An omitted --description is OMITTED from the query, never sent blank: on
	// the replace route a sent value REPLACES the stored one (a blank clears
	// it), so sending "" for "I said nothing" would silently wipe the prose.
	if d := strings.TrimSpace(description); d != "" {
		query.Set("description", d)
	}
	if base := strings.TrimSpace(filename); base != "" && base != "." && base != string(filepath.Separator) {
		query.Set("filename", base)
	}
	if declared := strings.TrimSpace(mimeType); declared != "" {
		query.Set("mime", declared)
	}
	// url.Values.Encode escapes the media type and the display name, which
	// matters twice over: a `+` reaches the server as a SPACE when a query is
	// pasted together by hand, and a name is arbitrary prose (spaces, #, CJK).
	reqURL := cfg.Base + pinPath(taskID, replaceID) + "?" + query.Encode()

	req, err := http.NewRequest(http.MethodPost, reqURL, f)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] pin: bad request for %q: %v\n", filename, err)
		return 1
	}
	req.ContentLength = info.Size()
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] pin: request failed (network): %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	detail := strings.TrimSpace(string(raw))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] pin: auth rejected (HTTP %d) for %q: %s\n",
			resp.StatusCode, filename, detail)
		return 3
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		fmt.Fprintf(errOut, "[ocagent] pin: server refused %q (HTTP %d): %s\n",
			filename, resp.StatusCode, detail)
		return 4
	default:
		fmt.Fprintf(errOut, "[ocagent] pin: unexpected HTTP %d for %q: %s\n",
			resp.StatusCode, filename, detail)
		return 5
	}

	var receipt artifactReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil || receipt.ArtifactID == "" {
		fmt.Fprintf(errOut, "[ocagent] pin: 200 but unparseable receipt body: %s\n", detail)
		return 5
	}
	receipt.raw = detail

	verb := "pinned"
	if replaceID != "" {
		verb = "replaced"
	}
	fmt.Fprintf(errOut, "[ocagent] pin: %s %s (%d bytes) on %s → %s (%d artifact(s))\n",
		verb, filename, info.Size(), receipt.TaskID, receipt.ArtifactID, receipt.ArtifactCount)
	fmt.Fprintln(out, receipt.ArtifactID)
	fmt.Fprintln(out, receipt.raw)
	return 0
}

// pinPath picks between T-92's two one-call routes and escapes the ids into the
// path. url.PathEscape, not raw concatenation: an id is server-minted and
// path-safe today, but a mistyped one must reach the server as a 404 rather
// than as a different route.
func pinPath(taskID, replaceID string) string {
	if replaceID != "" {
		return "/api/tasks/" + url.PathEscape(taskID) +
			"/artifact/" + url.PathEscape(replaceID) + "/replace/upload"
	}
	return "/api/tasks/" + url.PathEscape(taskID) + "/artifacts/upload"
}

// pinUsage is the ONE authority on this subcommand's spelling — the same role
// diffUsage plays for `diff`, and for the same reason: the flag set alone
// cannot say that --name is required for an add and carried forward on a
// replace.
func pinUsage(out io.Writer) {
	fmt.Fprintln(out, "usage: ocagent pin <path> --task <task-id> --name <name>")
	fmt.Fprintln(out, "                  [--description <text>] [--mime <type>] [--replace <artifact-id>]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Pin a local file or image onto a task as a deliverable in ONE call: the")
	fmt.Fprintln(out, "  bytes are stored and the artifact registered in the same transaction, so")
	fmt.Fprintln(out, "  there is no gap where an uploaded blob sits unreferenced.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  --task <task-id>       the task to pin onto (required)")
	fmt.Fprintln(out, "  --name <name>          display name, at most 48 runes (required when pinning a")
	fmt.Fprintln(out, "                         NEW artifact; on --replace an omitted one is kept)")
	fmt.Fprintln(out, "  --description <text>   optional prose, at most 256 runes (kept on --replace")
	fmt.Fprintln(out, "                         when omitted)")
	fmt.Fprintln(out, "  --mime <type>          declared media type (default: server-side sniff)")
	fmt.Fprintln(out, "  --replace <artifact-id>  swap THIS artifact's content, keeping its id; the")
	fmt.Fprintln(out, "                         kind cannot change and a link artifact is refused")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  On success stdout is the artifact id then the server's receipt JSON.")
	fmt.Fprintln(out, "  Only while the task is open: a closed task's deliverables are frozen (409).")
}
