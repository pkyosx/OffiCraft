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

// ---------------------------------------------------------------------------
// diff: ocagent diff <before> <after> [--label-before X] [--label-after Y] [--external]
// ---------------------------------------------------------------------------
//
// A COMPARISON IS A URL (T-59, owner 2026-09-03: 「可以指定兩個文件位置，就可以跳
// 出我們這個 diff 的畫面」). Name two things that already have an address, get a
// link back; paste the link to whoever needs to see the difference. It is not
// an attachment any more — nothing is stored, so there is no id to hang on a
// message and nothing to keep in step with the two sides.
//
// TWO FLAVOURS. By default this prints the INTERNAL link: no signature, opened
// by anyone who can already sign in to this station. --external asks the server
// to mint the signed one, which needs NO login at all — it has no expiry and no
// single link can be withdrawn, and the ONE thing that ends it is removing the
// signing key it was minted under, which kills every link that key signed at
// once (T-62). Mint it only for a reader who has no account.
//
// THE INTERNAL LINK COSTS NO REQUEST. It is a pure function of the two
// addresses, so this subcommand normally talks to nobody: it can answer while
// the station is unreachable, and it cannot fail halfway.
//
// THIS SUBCOMMAND UPLOADS NOTHING (owner 2026-09-03: 「我希望的是使用 diff 時，
// 都是直接給連結 id, 這個不負責上傳檔案」). A side is EITHER a stored blob id
// (`att-…`, what `ocagent upload` prints) or a document address
// (`doc:<kind>/<key>/<at>/<field>`) — nothing else. Getting bytes into the
// store stays `ocagent upload`'s one job: the common case is that the thing to
// compare is ALREADY in the system (a task artifact, an attachment someone
// sent, a document), and a `diff` that insisted on paths would force a
// pointless re-upload of it.
//
// stdout: ONE line, the URL. Exit codes are upload's: 0 ok, 1 transport,
// 2 usage, 3 auth, 4 rejected by the server, 5 anything else.

// ── the address spelling ────────────────────────────────────────────────────
//
// 🔴 THIS IS A COPY, AND THE SERVER IS THE AUTHORITY.
// server/ocserverd/diffaddr.go defines the spelling; this copy exists so a
// mistyped side costs one local sentence in the member's own vocabulary instead
// of a round trip whose refusal does not say WHICH of the two arguments to look
// at — and, since --external is the only flavour that talks to the server at
// all, so that the unsigned path is judged too rather than not at all.
//
// The two modules cannot import each other, so the copy is confronted against
// the authority through bin/tests/fixtures/diff-side-addresses.tsv, which both
// mirror tests read (diff_mirror_test.go here, diffaddr_mirror_test.go there).
// Change the spelling in one place and that fixture reddens the other by name.
const docSidePrefix = "doc:"

const (
	docAtCurrent = "current"
	docAtSeed    = "seed"
)

var (
	docAddrSegment = regexp.MustCompile(`^[A-Za-z0-9._:@+-]+$`)
	docAtRevision  = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	blobSideID     = regexp.MustCompile(`^att-[0-9a-f]{12}$`)
)

// ── the page URL ────────────────────────────────────────────────────────────
//
// 🔴 ALSO A COPY of server/ocserverd/api_diff.go's diffPagePath / diffParam*.
// The server mints the EXTERNAL link, so it owns this spelling; this copy is
// what lets the internal link be built without asking. diff_mirror_test.go
// confronts these five literals against that file's source.
const (
	diffPagePath        = "/diff"
	diffParamBefore     = "before"
	diffParamAfter      = "after"
	diffParamLabelBefor = "label_before"
	diffParamLabelAfter = "label_after"
)

// sideRefusal judges ONE argument and returns the sentence to print when it is
// not a side at all ("" = it is one).
//
// Every match is against the value AS GIVEN, never a trimmed copy: a padded
// address is one the server cannot resolve either, so accepting it here would
// only move the confusion later.
func sideRefusal(arg, which string) string {
	if !strings.HasPrefix(arg, docSidePrefix) {
		if blobSideID.MatchString(arg) {
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
		// "." and ".." contain no excluded character but traverse anyway.
		if part == "." || part == ".." || !docAddrSegment.MatchString(part) {
			return fmt.Sprintf("%q has a %s that is not a usable address segment: %q", arg, what, part)
		}
	}
	if at := parts[2]; at != docAtCurrent && at != docAtSeed && !docAtRevision.MatchString(at) {
		return fmt.Sprintf("%q has an <at> of %q — it must be %s, %s, "+
			"or a version id from list_document_history.", arg, at, docAtCurrent, docAtSeed)
	}
	return ""
}

// looksLikeAPath is the test behind the ONE error message that has to teach the
// new flow. It is deliberately generous: every argument that reaches it has
// already failed to be a blob id and a document address, so the only question
// left is which sentence helps more — and a member who typed something with a
// slash, a dot-extension or a name that is really on disk was reaching for the
// old file-path contract.
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

// diffQuery builds the page query. Empty labels are LEFT OUT rather than sent
// blank — mirrors the server's diffPageQuery, which the external flavour uses.
func diffQuery(before, after, labelBefore, labelAfter string) string {
	q := url.Values{diffParamBefore: {before}, diffParamAfter: {after}}
	if labelBefore != "" {
		q.Set(diffParamLabelBefor, labelBefore)
	}
	if labelAfter != "" {
		q.Set(diffParamLabelAfter, labelAfter)
	}
	return q.Encode()
}

// cmdDiff implements `ocagent diff`. Prints ONE line: the URL.
func cmdDiff(
	client httpClient, cfg Config,
	before, after, beforeLabel, afterLabel string, external bool,
	out, errOut io.Writer,
) int {
	// BOTH sides are judged before anything else, so a bad second argument
	// costs a message rather than a link naming one good side and one bad one.
	for _, side := range []struct{ arg, which string }{{before, "before"}, {after, "after"}} {
		if msg := sideRefusal(side.arg, side.which); msg != "" {
			fmt.Fprintf(errOut, "[ocagent] diff: %s\n", msg)
			return 2
		}
	}
	// This guard used to read `cfg.Base == ""`, which loadConfig makes
	// UNREACHABLE: an unset OC_BASE is replaced by defaultBase before any
	// subcommand sees it, so Base is never empty and the refusal below never
	// ran. What actually happened with OC_BASE unset was a link printed against
	// this machine's loopback address and an exit code of 0 — a comparison URL
	// that says it worked and leads nowhere the recipient can open. The
	// condition now asks the question the message always claimed to ask.
	//
	// It refuses on BOTH flavours deliberately. The plain link makes no request,
	// so nothing fails to warn the caller; it is precisely the flavour whose
	// wrongness is invisible until someone else clicks it.
	if requireBase(cfg, "diff", errOut) {
		return 3
	}
	if !external {
		fmt.Fprintln(out, cfg.Base+diffPagePath+"?"+
			diffQuery(before, after, strings.TrimSpace(beforeLabel), strings.TrimSpace(afterLabel)))
		return 0
	}
	return mintExternalDiffLink(client, cfg, before, after,
		strings.TrimSpace(beforeLabel), strings.TrimSpace(afterLabel), out, errOut)
}

// mintExternalDiffLink asks the server for the SIGNED link. Only the server can
// mint it — the signature is an HMAC under a key this process never holds — so
// this is the one flavour that costs a request.
func mintExternalDiffLink(
	client httpClient, cfg Config, before, after, labelBefore, labelAfter string,
	out, errOut io.Writer,
) int {
	if cfg.Token == "" {
		fmt.Fprint(errOut, "[ocagent] diff: no OC_TOKEN configured — minting an external link is an authed call.\n")
		return 3
	}
	query := url.Values{diffParamBefore: {before}, diffParamAfter: {after}}
	if labelBefore != "" {
		query.Set(diffParamLabelBefor, labelBefore)
	}
	if labelAfter != "" {
		query.Set(diffParamLabelAfter, labelAfter)
	}
	// The route is built into a variable rather than spelled at the call, which
	// is download.go's shape too: it keeps this a plain bodyless GET for
	// bin/uplink-guard.py rather than a callsite that looks like a send.
	reqURL := cfg.Base + "/api/diff/share-link?" + query.Encode()
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] diff: bad request: %v\n", err)
		return 1
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] diff: request failed (network): %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	detail := strings.TrimSpace(string(raw))

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] diff: auth rejected (HTTP %d): %s\n", resp.StatusCode, detail)
		return 3
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		fmt.Fprintf(errOut, "[ocagent] diff: server rejected the pair (HTTP %d): %s\n", resp.StatusCode, detail)
		return 4
	default:
		fmt.Fprintf(errOut, "[ocagent] diff: unexpected HTTP %d: %s\n", resp.StatusCode, detail)
		return 5
	}

	var minted struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &minted); err != nil || minted.URL == "" {
		fmt.Fprintf(errOut, "[ocagent] diff: 200 but unparseable link body: %s\n", detail)
		return 5
	}
	// The server mints a SERVER-RELATIVE path and the caller absolutizes it —
	// the same posture get_chat_attachment_share_link has, and the reason is
	// that the server does not know which origin this reader reaches it on.
	fmt.Fprintln(out, cfg.Base+minted.URL)
	return 0
}

// diffUsage prints the complete reference for the subcommand.
//
// This text is the SINGLE AUTHORITY on the parameters and on how a side is
// spelled. The global context (seeds/system_interaction.md) deliberately
// carries only when and why to reach for a comparison, plus the judgement calls
// a member cannot read off a syntax line, and points here for the rest — one
// fact, one place, and the place that ships with the binary rather than the one
// that can drift a release behind it.
func diffUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent diff <before> <after> [--label-before <text>] [--label-after <text>] [--external]

Prints a URL: the before/after compare screen for those two things. Paste it to
whoever needs to see the difference. Nothing is stored and nothing is uploaded.

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

--external asks the server to mint a SIGNED link instead: it opens with no login
at all and has no expiry, and no single link can be withdrawn — the only thing
that ends one is removing the signing key it was minted under (Settings ›
Signing keys), which kills every link that key signed at once. Mint it only for
a reader who has no account on this station. Without it you get the plain link, which any
signed-in reader opens and which costs no request at all.

An address that no longer resolves is not an error here: the screen says that
side is gone and still draws the other one.

stdout: one line, the URL.
`)
}
