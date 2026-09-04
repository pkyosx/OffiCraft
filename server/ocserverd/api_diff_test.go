package main

// api_diff_test.go — a comparison is a URL (T-59): the pair route, the mint
// route, and the credential-less path between them.

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

// getDiff fires the pair route with whatever credential (or none) is given and
// returns the decoded answer.
func getDiff(t *testing.T, base, token, query string) (int, DiffPairDTO, string) {
	t.Helper()
	status, body := doRaw(t, "GET", base+"/api/diff?"+query, token, "", nil)
	var pair DiffPairDTO
	if status == 200 {
		if err := json.Unmarshal([]byte(body), &pair); err != nil {
			t.Fatalf("200 but unparseable pair: %v (%s)", err, body)
		}
	}
	return status, pair, body
}

func diffQueryOf(before, after string) string {
	return url.Values{"before": {before}, "after": {after}}.Encode()
}

func sideText(t *testing.T, side DiffSideDTO) string {
	t.Helper()
	if side.Text == nil {
		t.Fatalf("side %q carries no text (gone=%v)", side.Address, side.Gone)
	}
	return *side.Text
}

// The whole pair in one answer, which is the shape the signature depends on:
// one request, one thing to sign.
func TestDiffPairResolvesBothStoredBlobs(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	before, _ := uploadBlob(t, srv.URL, tok, "?mime=text/plain", []byte("old text"))["id"].(string)
	after, _ := uploadBlob(t, srv.URL, tok, "?mime=text/plain", []byte("new text"))["id"].(string)

	status, pair, body := getDiff(t, srv.URL, tok, diffQueryOf(before, after))
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	if got := sideText(t, pair.Before); got != "old text" {
		t.Errorf("before text = %q, want %q", got, "old text")
	}
	if got := sideText(t, pair.After); got != "new text" {
		t.Errorf("after text = %q, want %q", got, "new text")
	}
	if pair.Before.Gone || pair.After.Gone {
		t.Error("neither side is gone, but the pair says one is")
	}
	if pair.Before.Address != before || pair.After.Address != after {
		t.Errorf("the addresses must come back verbatim and in order, got %q/%q",
			pair.Before.Address, pair.After.Address)
	}
}

// An address that resolves to nothing is NOT an error — the other side still
// draws, which is what the reader needs when a revision has been pruned.
func TestDiffPairReportsAGoneSideAndStillDrawsTheOther(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	after, _ := uploadBlob(t, srv.URL, tok, "?mime=text/plain", []byte("still here"))["id"].(string)

	for name, gone := range map[string]string{
		"a blob that was never stored": "att-ffffffffffff",
		"a pruned revision":            "doc:lessons/mira/999999/text",
		"a kind this station lacks":    "doc:not_a_kind/x/current/text",
		"a document with no seed":      "doc:lessons/mira/seed/text",
		// 19 digits is SAYABLE — the address grammar allows up to 19 — and
		// overflows int64, so the parse that api_diff.go's fail-closed branch
		// catches is reached in earnest and answers this same honest "gone".
		"a revision id no int64 holds": "doc:lessons/mira/9999999999999999999/text",
	} {
		status, pair, body := getDiff(t, srv.URL, tok, diffQueryOf(gone, after))
		if status != 200 {
			t.Errorf("%s: status = %d, want 200 — a missing side is an answer, not an error (%s)",
				name, status, body)
			continue
		}
		if !pair.Before.Gone {
			t.Errorf("%s: before side must report gone", name)
		}
		if pair.Before.GoneReason == nil || *pair.Before.GoneReason == "" {
			t.Errorf("%s: a gone side must say why", name)
		}
		if pair.Before.Text != nil {
			t.Errorf("%s: a gone side must carry no text", name)
		}
		if sideText(t, pair.After) != "still here" {
			t.Errorf("%s: the other side stopped drawing", name)
		}
	}
}

// The field name is part of the address, so naming one the revision does not
// hold is the same honest "gone" — never someone else's field.
func TestDiffPairReportsAFieldTheDocumentDoesNotHold(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	status, pair, body := getDiff(t, srv.URL, tok,
		diffQueryOf("doc:global_context/global/current/definition_md",
			"doc:global_context/global/current/text"))
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	if !pair.Before.Gone {
		t.Error("a field the document does not hold must read as gone")
	}
	if pair.After.Gone {
		t.Errorf("the live global context must resolve: %v", pair.After)
	}
}

// Only an UNSAYABLE address is refused up front, and the refusal has to say
// which of the two sides it is about.
func TestDiffPairRefusesAnUnsayableAddress(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	for name, q := range map[string]string{
		"a traversing blob id": diffQueryOf("att-/../../api/version", "att-0123456789ab"),
		"a padded address":     diffQueryOf(" att-0123456789ab ", "att-0123456789ab"),
		"a file path":          diffQueryOf("./before.md", "att-0123456789ab"),
		"a bad at":             diffQueryOf("doc:lessons/mira/latest/text", "att-0123456789ab"),
	} {
		status, _, body := getDiff(t, srv.URL, tok, q)
		if status != 422 {
			t.Errorf("%s: status = %d, want 422 (%s)", name, status, body)
		}
		if !strings.Contains(body, "before side") {
			t.Errorf("%s: the refusal must name WHICH side: %s", name, body)
		}
	}
	// A missing parameter is the wire-frozen 422 from the generated binder.
	if status, _, _ := getDiff(t, srv.URL, tok, "before=att-0123456789ab"); status != 422 {
		t.Errorf("a missing `after` must be 422, got %d", status)
	}
}

// The load-bearing one: an agent mints the external link and a caller carrying
// NO credentials at all reads the comparison through it.
func TestDiffShareLinkReadsThePairWithoutCredentials(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	before, _ := uploadBlob(t, srv.URL, tok, "?mime=text/plain", []byte("old text"))["id"].(string)
	after, _ := uploadBlob(t, srv.URL, tok, "?mime=text/plain", []byte("new text"))["id"].(string)

	minted := mintDiffLink(t, srv.URL, tok, url.Values{
		"before": {before}, "after": {after}, "label_before": {"v1"},
	}.Encode())

	// Server-relative by contract — only the client knows the public origin.
	if !strings.HasPrefix(minted, "/diff?") {
		t.Fatalf("the mint must answer a server-relative /diff path, got %q", minted)
	}
	page, err := url.Parse(minted)
	if err != nil {
		t.Fatalf("minted an unparseable url: %v (%q)", err, minted)
	}
	q := page.Query()
	if q.Get("before") != before || q.Get("after") != after || q.Get("label_before") != "v1" {
		t.Fatalf("the minted link lost a parameter: %q", minted)
	}
	if q.Get("sig") == "" {
		t.Fatalf("the external link carries no signature: %q", minted)
	}
	// An unlabelled column stays absent rather than blank, so the reader writes
	// its own localized heading.
	if _, present := q["label_after"]; present {
		t.Errorf("an unlabelled column must not appear on the link: %q", minted)
	}

	status, pair, body := getDiff(t, srv.URL, "", q.Encode())
	if status != 200 {
		t.Fatalf("a credential-less holder of the link must read the pair: %d %s", status, body)
	}
	if sideText(t, pair.Before) != "old text" || sideText(t, pair.After) != "new text" {
		t.Fatalf("the signed read served the wrong pair: %+v", pair)
	}
	if pair.Before.Label == nil || *pair.Before.Label != "v1" {
		t.Errorf("the label must head its own column, got %v", pair.Before.Label)
	}
}

// The deny side. Each case is a way the credential could over-grant if the
// signature covered less than the whole of what one answer depends on.
func TestDiffSignatureGrantsExactlyThatOneComparison(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	before, _ := uploadBlob(t, srv.URL, tok, "", []byte("old"))["id"].(string)
	after, _ := uploadBlob(t, srv.URL, tok, "", []byte("new"))["id"].(string)
	other, _ := uploadBlob(t, srv.URL, tok, "", []byte("someone else's"))["id"].(string)

	minted, _ := url.Parse(mintDiffLink(t, srv.URL, tok, url.Values{
		"before": {before}, "after": {after}, "label_before": {"v1"},
	}.Encode()))
	sig := minted.Query().Get("sig")

	tampered := sig[:len(sig)-1] + "X"
	if tampered == sig {
		tampered = sig[:len(sig)-1] + "Y"
	}
	// The attachment share sig, replayed here: a different key by construction
	// (sharesig.go's two version labels), so it must not verify.
	attSig := shareSigFor(secret, before)

	for name, q := range map[string]url.Values{
		"a tampered sig":          {"before": {before}, "after": {after}, "label_before": {"v1"}, "sig": {tampered}},
		"a swapped after side":    {"before": {before}, "after": {other}, "label_before": {"v1"}, "sig": {sig}},
		"the sides transposed":    {"before": {after}, "after": {before}, "label_before": {"v1"}, "sig": {sig}},
		"a relabelled column":     {"before": {before}, "after": {after}, "label_before": {"v2"}, "sig": {sig}},
		"the label dropped":       {"before": {before}, "after": {after}, "sig": {sig}},
		"an empty sig":            {"before": {before}, "after": {after}, "label_before": {"v1"}, "sig": {""}},
		"no credential at all":    {"before": {before}, "after": {after}, "label_before": {"v1"}},
		"an attachment's own sig": {"before": {before}, "after": {after}, "label_before": {"v1"}, "sig": {attSig}},
	} {
		if status, _, body := getDiff(t, srv.URL, "", q.Encode()); status != 401 {
			t.Errorf("%s must be 401, got %d %s", name, status, body)
		}
	}
	// A diff sig must not generalise into a credential for anything else.
	if status, body := doRaw(t, "GET", srv.URL+"/api/chat?sig="+sig, "", "", nil); status != 401 {
		t.Errorf("a diff sig on an unrelated row must stay 401, got %d %s", status, body)
	}
	// A present-but-invalid bearer credential never falls through to the sig.
	if status, body := doRaw(t, "GET",
		srv.URL+"/api/diff?"+minted.RawQuery, "not-a-token", "", nil); status != 401 {
		t.Errorf("a bad token must stay 401 even beside a good sig, got %d %s", status, body)
	}
}

// The internal flavour needs no sig at all: any authenticated caller reads it,
// which is the same reach they already had one side at a time.
func TestDiffPairReadsForAnAuthenticatedCallerWithNoSignature(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	before, _ := uploadBlob(t, srv.URL,
		mustMint(t, "mira", secret, now), "", []byte("old"))["id"].(string)

	for name, sub := range map[string]string{
		"a plain agent / outsource worker": "ow-nobody",
		"the warden (the row's floor)":     ServerSelfHost,
	} {
		status, _, body := getDiff(t, srv.URL, mustMint(t, sub, secret, now),
			diffQueryOf(before, before))
		if status != 200 {
			t.Errorf("%s must read the pair at this row's floor: %d %s", name, status, body)
		}
	}
}

func mustMint(t *testing.T, sub string, secret []byte, now int64) string {
	t.Helper()
	tok, err := mintJWT(sub, "agent", 300, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// mintDiffLink calls the mint route and returns the server-relative url.
func mintDiffLink(t *testing.T, base, token, query string) string {
	t.Helper()
	status, body := doRaw(t, "GET", base+"/api/diff/share-link?"+query, token, "", nil)
	if status != 200 {
		t.Fatalf("mint failed: %d %s", status, body)
	}
	var minted DiffShareLinkDTO
	if err := json.Unmarshal([]byte(body), &minted); err != nil {
		t.Fatalf("mint answered unparseable JSON: %v (%s)", err, body)
	}
	return minted.Url
}
