package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Owner ruling 2026-09-03: diff never uploads — do not make it accept file paths.

// COPY of server/ocserverd/diffaddr.go, which owns the side spelling (agreed
// wording: bin/tests/fixtures/diff-side-addresses.tsv). Nothing in Go is checked
// against that table, so drift between this copy and the server's is silent.
const docSidePrefix = "doc:"

const (
	docAtCurrent = "current"
	docAtSeed    = "seed"
)

var (
	docAddrSegment      = regexp.MustCompile(`^[A-Za-z0-9._:@+-]+$`)
	docAtVersionID      = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	attachmentIDPattern = regexp.MustCompile(`^att-[0-9a-f]{12}$`)
)

// COPY of server/ocserverd/api_diff.go's diffPagePath / diffParam*. The server
// owns these literals; nothing checks this Go copy against it.
const (
	diffPagePath         = "/diff"
	diffParamBefore      = "before"
	diffParamAfter       = "after"
	diffParamLabelBefore = "label_before"
	diffParamLabelAfter  = "label_after"
)

const diffShareLinkPath = "/api/diff/share-link"

// A padded address is refused rather than trimmed: the server cannot resolve
// it either, and the plain link would carry it unchecked.
func sideRefusal(arg, which string) string {
	if !strings.HasPrefix(arg, docSidePrefix) {
		if attachmentIDPattern.MatchString(arg) {
			return ""
		}
		if looksLikeAPath(arg) {
			return fmt.Sprintf("the %s side %q is a file path, and diff does not upload files.\n"+
				"  Put the file in the store first, then pass the id it prints:\n"+
				"      ocagent upload %s          → prints an id like att-0123456789ab\n"+
				"      ocagent diff <before id> <after id>\n"+
				"  If what you want to compare is already in the system (a task artifact, an\n"+
				"  attachment someone sent you), you already have its id — no upload needed.\n"+
				"  A side is a stored attachment id (att-…) or doc:<kind>/<key>/<at>/<field>.",
				which, arg, arg)
		}
		return fmt.Sprintf("the %s side %q is neither a stored attachment id "+
			"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address "+
			"(doc:<kind>/<key>/<at>/<field>).", which, arg)
	}
	parts := strings.Split(strings.TrimPrefix(arg, docSidePrefix), "/")
	if len(parts) != 4 {
		return fmt.Sprintf("%q is not a document address — it is "+
			"doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a version id.", arg)
	}
	for i, what := range []string{"kind", "key", "", "field"} {
		if what == "" {
			continue
		}
		part := parts[i]
		if part == "" {
			return fmt.Sprintf("%q leaves its %s empty.", arg, what)
		}
		if part == "." || part == ".." || !docAddrSegment.MatchString(part) {
			return fmt.Sprintf("%q has a %s that is not a usable address segment: %q", arg, what, part)
		}
	}
	if at := parts[2]; at != docAtCurrent && at != docAtSeed && !docAtVersionID.MatchString(at) {
		return fmt.Sprintf("%q has an <at> of %q — it must be %s, %s, "+
			"or a version id from list_document_history.", arg, at, docAtCurrent, docAtSeed)
	}
	return ""
}

func looksLikeAPath(arg string) bool {
	if strings.ContainsAny(arg, `/\`) || strings.HasPrefix(arg, "~") {
		return true
	}
	if filepath.Ext(arg) != "" {
		return true
	}
	_, err := os.Stat(arg)
	return err == nil
}

// Mirrors server/ocserverd's diffPageQuery, which builds the external link.
func diffQuery(before, after, labelBefore, labelAfter string) string {
	q := url.Values{diffParamBefore: {before}, diffParamAfter: {after}}
	if labelBefore != "" {
		q.Set(diffParamLabelBefore, labelBefore)
	}
	if labelAfter != "" {
		q.Set(diffParamLabelAfter, labelAfter)
	}
	return q.Encode()
}

func cmdDiff(
	client httpClient, cfg Config,
	before, after, labelBefore, labelAfter string, external bool,
	out, errOut io.Writer,
) int {
	for _, side := range []struct{ arg, which string }{{before, "before"}, {after, "after"}} {
		if msg := sideRefusal(side.arg, side.which); msg != "" {
			fmt.Fprintf(errOut, "[ocagent] diff: %s\n", msg)
			return 2
		}
	}
	// OC_BASE CLASSIFICATION: EXEMPT for the plain link, GUARDED (refuse, exit 3)
	// for --external. The plain link is a host-less path: a station-local
	// member's OC_BASE is loopback, so a link built on it opens only for that
	// member — do not prefix it with cfg.Base.
	if !external {
		fmt.Fprintln(out, diffPagePath+"?"+
			diffQuery(before, after, strings.TrimSpace(labelBefore), strings.TrimSpace(labelAfter)))
		return 0
	}
	if warnMissingBase(cfg, "diff", errOut) {
		return 3
	}
	return mintExternalDiffLink(client, cfg, before, after,
		strings.TrimSpace(labelBefore), strings.TrimSpace(labelAfter), out, errOut)
}

func mintExternalDiffLink(
	client httpClient, cfg Config, before, after, labelBefore, labelAfter string,
	out, errOut io.Writer,
) int {
	if cfg.Token == "" {
		fmt.Fprint(errOut, "[ocagent] diff: no OC_TOKEN configured — minting an external link is an authed call.\n")
		return 3
	}
	payload := map[string]any{diffParamBefore: before, diffParamAfter: after}
	if labelBefore != "" {
		payload[diffParamLabelBefore] = labelBefore
	}
	if labelAfter != "" {
		payload[diffParamLabelAfter] = labelAfter
	}
	status, detail := httpRequest(client, http.MethodPost, cfg.Base+diffShareLinkPath, cfg.Token, payload)
	detail = strings.TrimSpace(detail)
	if status == 0 {
		fmt.Fprintf(errOut, "[ocagent] diff: request failed (network): %s\n", detail)
		return 1
	}

	switch {
	case status == http.StatusOK:
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] diff: auth rejected (HTTP %d): %s\n", status, detail)
		return 3
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		fmt.Fprintf(errOut, "[ocagent] diff: server rejected the pair (HTTP %d): %s\n", status, detail)
		return 4
	default:
		fmt.Fprintf(errOut, "[ocagent] diff: unexpected HTTP %d: %s\n", status, detail)
		return 5
	}

	var minted struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(detail), &minted); err != nil || minted.URL == "" {
		fmt.Fprintf(errOut, "[ocagent] diff: 200 but unparseable link body: %s\n", detail)
		return 5
	}
	// The server returns a server-relative path (it cannot know which origin this
	// reader reaches it on); the caller absolutizes it.
	fmt.Fprintln(out, cfg.Base+minted.URL)
	return 0
}

// This usage text is the single authority on parameters and side spelling:
// seeds/system_interaction.md deliberately defers here instead of repeating it.
func diffUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent diff <before> <after> [--label-before <text>] [--label-after <text>] [--external]

Prints a link to the before/after compare screen for those two things. Paste it
to whoever needs to see the difference. Nothing is uploaded.

The plain link is a host-less path (/diff?...): paste it as printed, it opens on
whatever station address the reader is using. Do not prefix it with OC_BASE or
any host. It needs no OC_BASE at all.

Each side must ALREADY have an address, in one of two forms:

  att-0123456789ab               a stored attachment id — what `+"`ocagent upload`"+`
                                 prints, and what a task artifact or an
                                 attachment someone sent you already is.

  doc:<kind>/<key>/<at>/<field>  one field of a system document.
                                   <kind>/<key>  the same two `+"`list_document_history`"+` takes
                                   <at>          current (the live content) | seed (the
                                                 shipped default) | a version id from
                                                 `+"`list_document_history`"+`
                                   <field>       the field name inside that version, also
                                                 from `+"`list_document_history`"+` (most
                                                 documents carry exactly one)

To compare a LOCAL FILE, put it in the store first and pass the id it prints:

  ocagent upload ./before.md          → att-…
  ocagent diff <before id> <after id>

--label-before / --label-after set that column's heading. A side with no label
gets the compare screen's own heading; do NOT label a doc: side, which the
screen already names in the reader's own language.

--external asks the server to mint a SHARE link instead: it opens with no login
at all and stops working 7 days later. Each call mints a separate link; the
owner can revoke any one of them on the Share tab (or with revoke_share_link)
without touching the others. The URL is printed once and cannot be fetched
again. For another expiry, or a link that never expires, call the
get_diff_share_link tool with expires_ts instead. Mint it only for a reader who
has no account on this station. It is a full URL built on OC_BASE, so it
refuses when OC_BASE is unset or malformed. Without it you get the plain link,
which any signed-in reader opens and which costs no request at all.

An address that no longer resolves is not an error here: the screen says that
side is gone and still draws the other one.

stdout: one line — the /diff?... path, or with --external the share URL.
`)
}
