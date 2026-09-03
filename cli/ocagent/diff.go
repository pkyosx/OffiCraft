package main

import (
	"bytes"
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
// diff: ocagent diff <before> <after> [--label-before X] [--label-after Y]
// ---------------------------------------------------------------------------
//
// The agent-facing half of the compare attachment (T-59, owner 2026-09-03:
// 「可以指定兩個文件位置，就可以跳出我們這個 diff 的畫面」). Point it at two
// files, get back one attachment id to hang on a message, a reply card or a
// task artifact; the owner clicks it and lands in the compare screen.
//
// WHY A SUBCOMMAND RATHER THAN "the agent writes the JSON itself". The pointer
// pair is three uploads that have to happen in order, and the two ids it names
// only exist after the first two land. An agent doing that by hand types the
// ids into a JSON literal from the output of two earlier commands — a step that
// is easy, boring and silently wrong when it goes wrong (the pair is accepted,
// and one side simply never resolves). The friction is also the thing that
// decides whether a feature is used at all, and this one is worth nothing if
// agents find it annoying.
//
// The BYTES ARE NEVER COPIED into the pair: it stores the two blob ids, so the
// two documents stay individually openable and nothing is stored twice.
//
// Stdout mirrors `upload` so the two are scriptable the same way:
//   line 1: the compare attachment's id
//   line 2: its light-ref JSON {id, mime, filename}
// Exit codes are upload's, unchanged: 0 ok, 1 transport/filesystem,
// 2 usage, 3 auth, 4 rejected by the server, 5 anything else.

const diffAttachmentMime = "application/vnd.officraft.diff"

// uploadedRef is the light ref the attachments route mints for a stored blob.
type uploadedRef struct {
	ID       string `json:"id"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
	// The response body verbatim — stdout line 2 is the SERVER's JSON, not a
	// re-serialisation of the three fields above, so a field this build does not
	// know about still reaches whoever is reading the output.
	raw string
}

// postAttachment streams one body into POST /api/chat/attachments and returns
// the minted ref. Extracted from cmdUpload so `diff` cannot grow a second,
// slightly different copy of the auth, query-building and exit-code contract —
// `verb` is only there so a diagnostic names the subcommand the reader ran.
//
// `size` is passed to Content-Length; -1 leaves it unset for an in-memory body.
func postAttachment(
	client httpClient, cfg Config, verb string,
	body io.Reader, size int64, filename, mimeType string,
	errOut io.Writer,
) (uploadedRef, int) {
	query := url.Values{}
	if name := strings.TrimSpace(filename); name != "" && name != "." && name != string(filepath.Separator) {
		query.Set("filename", name)
	}
	if declared := strings.TrimSpace(mimeType); declared != "" {
		query.Set("mime", declared)
	}
	// url.Values.Encode escapes the media type, which matters: a `+` reaches the
	// server as a SPACE when a query is pasted together by hand.
	reqURL := cfg.Base + "/api/chat/attachments?" + query.Encode()

	req, err := http.NewRequest(http.MethodPost, reqURL, body)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: bad request for %q: %v\n", verb, filename, err)
		return uploadedRef{}, 1
	}
	if size >= 0 {
		req.ContentLength = size
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: request failed (network): %v\n", verb, err)
		return uploadedRef{}, 1
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	detail := strings.TrimSpace(string(raw))

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] %s: auth rejected (HTTP %d) for %q: %s\n",
			verb, resp.StatusCode, filename, detail)
		return uploadedRef{}, 3
	case resp.StatusCode == http.StatusBadRequest:
		fmt.Fprintf(errOut, "[ocagent] %s: server rejected %q (HTTP 400): %s\n",
			verb, filename, detail)
		return uploadedRef{}, 4
	default:
		fmt.Fprintf(errOut, "[ocagent] %s: unexpected HTTP %d for %q: %s\n",
			verb, resp.StatusCode, filename, detail)
		return uploadedRef{}, 5
	}

	var ref uploadedRef
	if err := json.Unmarshal(raw, &ref); err != nil || ref.ID == "" {
		fmt.Fprintf(errOut, "[ocagent] %s: 200 but unparseable ref body: %s\n", verb, detail)
		return uploadedRef{}, 5
	}
	ref.raw = detail
	return ref, 0
}

// uploadOneFile streams a path through postAttachment, reporting the
// filesystem faults as upload's exit code 1.
func uploadOneFile(
	client httpClient, cfg Config, verb, path, mimeType string, errOut io.Writer,
) (uploadedRef, int64, int) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: cannot open %s: %v\n", verb, path, err)
		return uploadedRef{}, 0, 1
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: cannot stat %s: %v\n", verb, path, err)
		return uploadedRef{}, 0, 1
	}
	if info.IsDir() {
		fmt.Fprintf(errOut, "[ocagent] %s: %s is a directory, not a file\n", verb, path)
		return uploadedRef{}, 0, 1
	}
	ref, code := postAttachment(client, cfg, verb, f, info.Size(),
		filepath.Base(path), mimeType, errOut)
	return ref, info.Size(), code
}

// docSidePrefix marks an argument as a DOCUMENT ADDRESS rather than a file
// path: `doc:<kind>/<key>/<at>/<field>` (T-59 second round). Four segments,
// split on "/" — the one character a kind, key, `at` or field may never contain
// (the server's address charset excludes it precisely so a reader can splice
// these into a URL path), which is what makes the split unambiguous.
//
// A prefix rather than a heuristic because the alternative is guessing, and the
// guess would be silent when it is wrong. If a FILE of that literal name
// exists, `diff` refuses instead of picking one meaning — see docSide.
const docSidePrefix = "doc:"

// docSide turns one argument into a side of the pair. It returns the side's
// JSON object, or nil when the argument is an ordinary path the caller should
// upload.
//
// The label is deliberately LEFT EMPTY for a document side, where a file side
// defaults to its filename: the reader already has a better heading than
// anything this process could write — 「目前存檔內容」/「初始版本」/「版本 #id」
// in the reader's own language — and a label written here would override it in
// English for everyone.
func docSide(arg, givenLabel string, errOut io.Writer) (map[string]any, int, bool) {
	if !strings.HasPrefix(arg, docSidePrefix) {
		return nil, 0, false
	}
	if _, err := os.Stat(arg); err == nil {
		fmt.Fprintf(errOut, "[ocagent] diff: %q is both a document address and a real file — "+
			"pass ./%s to mean the file.\n", arg, arg)
		return nil, 2, true
	}
	parts := strings.Split(strings.TrimPrefix(arg, docSidePrefix), "/")
	if len(parts) != 4 {
		fmt.Fprintf(errOut, "[ocagent] diff: %q is not a document address — "+
			"it is doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a version id.\n", arg)
		return nil, 2, true
	}
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			fmt.Fprintf(errOut, "[ocagent] diff: %q leaves part %d of the document address empty.\n",
				arg, i+1)
			return nil, 2, true
		}
	}
	side := map[string]any{"doc": map[string]string{
		"kind": parts[0], "key": parts[1], "at": parts[2], "field": parts[3],
	}}
	if trimmed := strings.TrimSpace(givenLabel); trimmed != "" {
		side["label"] = trimmed
	}
	return side, 0, true
}

// cmdDiff implements `ocagent diff`. Up to three uploads: each side that is a
// FILE is uploaded, then the pair that names both.
func cmdDiff(
	client httpClient, cfg Config,
	beforePath, afterPath, beforeLabel, afterLabel string,
	out, errOut io.Writer,
) int {
	if cfg.Token == "" {
		fmt.Fprint(errOut, "[ocagent] diff: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}

	// The label is what the compare screen writes above each column, so a FILE
	// side defaults to the file's own name rather than to nothing: two
	// unlabelled columns are the state the owner already complained about being
	// unable to read.
	label := func(given, path string) string {
		if trimmed := strings.TrimSpace(given); trimmed != "" {
			return trimmed
		}
		return filepath.Base(path)
	}
	// Both sides are resolved BEFORE anything is uploaded: a malformed document
	// address on the second side must not leave the first side's bytes sitting
	// in the store with nothing pointing at them (there is no GC yet).
	beforeDoc, code, isDoc := docSide(beforePath, beforeLabel, errOut)
	if isDoc && code != 0 {
		return code
	}
	afterDoc, code, isAfterDoc := docSide(afterPath, afterLabel, errOut)
	if isAfterDoc && code != 0 {
		return code
	}

	uploadSide := func(path, given string) (map[string]any, int) {
		ref, size, code := uploadOneFile(client, cfg, "diff", path, "", errOut)
		if code != 0 {
			return nil, code
		}
		fmt.Fprintf(errOut, "[ocagent] diff: %s (%d bytes) → %s\n",
			filepath.Base(path), size, ref.ID)
		return map[string]any{"attachment_id": ref.ID, "label": label(given, path)}, 0
	}

	beforeSide := beforeDoc
	if beforeSide == nil {
		if beforeSide, code = uploadSide(beforePath, beforeLabel); code != 0 {
			return code
		}
	}
	afterSide := afterDoc
	if afterSide == nil {
		if afterSide, code = uploadSide(afterPath, afterLabel); code != 0 {
			return code
		}
	}

	pair, err := json.Marshal(map[string]any{"before": beforeSide, "after": afterSide})
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] diff: cannot build the pair: %v\n", err)
		return 1
	}

	sideName := func(arg string, isDoc bool) string {
		if isDoc {
			return strings.TrimPrefix(arg, docSidePrefix)
		}
		return filepath.Base(arg)
	}
	name := sideName(beforePath, beforeDoc != nil) + " → " + sideName(afterPath, afterDoc != nil)
	ref, code := postAttachment(client, cfg, "diff",
		bytes.NewReader(pair), int64(len(pair)), name, diffAttachmentMime, errOut)
	if code != 0 {
		return code
	}
	fmt.Fprintf(errOut, "[ocagent] diff: %s → %s\n", name, ref.ID)
	fmt.Fprintln(out, ref.ID)
	fmt.Fprintln(out, ref.raw)
	return 0
}
