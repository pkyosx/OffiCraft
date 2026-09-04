package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two stored ids of the exact shape the server accepts, so a test that means
// "a real blob id" cannot accidentally pass on a shape the server would refuse.
const (
	beforeID = "att-0123456789ab"
	afterID  = "att-ba9876543210"
)

// runDiff drives the subcommand with a client that FAILS if it is used. The
// plain flavour must not talk to the server at all, so a request here is the
// assertion failing, not a fixture missing.
func runDiff(t *testing.T, cfg Config, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	client := http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("diff made a request it should not have: %s", r.URL)
		return nil, nil
	})}
	before, after := args[0], args[1]
	rc := cmdDiff(&client, cfg, before, after, "", "", false, &out, &errOut)
	return rc, out.String(), errOut.String()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The whole contract in one assertion: two addresses in, ONE url out, and not a
// single byte of network traffic. The plain link is a pure function of its two
// sides, which is what lets a member produce one while the station is
// unreachable.
func TestDiffPrintsTheInternalURLWithoutTalkingToTheServer(t *testing.T) {
	rc, out, errOut := runDiff(t, Config{Base: "https://oc.example"}, beforeID, afterID)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut)
	}
	want := "https://oc.example/diff?after=" + afterID + "&before=" + beforeID + "\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// The sides ride the query PERCENT-ENCODED, so a document address — which
// carries "/" and ":" — survives as one value rather than becoming extra path
// segments the reader would resolve somewhere else entirely.
func TestDiffEncodesADocumentSideIntoOneQueryValue(t *testing.T) {
	doc := "doc:lessons/mira/current/text"
	rc, out, errOut := runDiff(t, Config{Base: "https://oc.example"}, doc, afterID)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut)
	}
	parsed, err := url.Parse(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("printed a url that does not parse: %v (%q)", err, out)
	}
	if parsed.Path != "/diff" {
		t.Errorf("path = %q, want /diff — the address must not leak into the path", parsed.Path)
	}
	if got := parsed.Query().Get("before"); got != doc {
		t.Errorf("before = %q, want %q", got, doc)
	}
}

func TestDiffPutsTheLabelsOnTheURLOnlyWhenGiven(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := cmdDiff(nil, Config{Base: "https://oc.example"}, beforeID, afterID,
		"v1", "", false, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut.String())
	}
	q, _ := url.Parse(strings.TrimSpace(out.String()))
	if got := q.Query().Get("label_before"); got != "v1" {
		t.Errorf("label_before = %q, want v1", got)
	}
	// An unlabelled side carries NO parameter at all — that is what makes the
	// compare screen write its own localized heading rather than a blank one.
	if _, present := q.Query()["label_after"]; present {
		t.Errorf("an unlabelled side must not appear on the url: %s", out.String())
	}
}

// The one error message that has to teach the new flow: `diff` never uploads,
// so a path is refused with the two commands that DO work.
func TestDiffRefusesAFilePathAndSaysToUploadItFirst(t *testing.T) {
	rc, out, errOut := runDiff(t, Config{Base: "https://oc.example"}, "./before.md", afterID)
	if rc != 2 {
		t.Fatalf("rc = %d, want 2", rc)
	}
	if out != "" {
		t.Errorf("stdout must stay empty on a refusal, got %q", out)
	}
	for _, want := range []string{"does not upload files", "ocagent upload ./before.md", "att-"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("message does not mention %q:\n%s", want, errOut)
		}
	}
}

// A bare filename with no slash and no extension is still a path when a file of
// that name is really there — the member typed what they were looking at.
func TestDiffRefusesAnExistingFileEvenWithoutAPathLikeName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	rc, _, errOut := runDiff(t, Config{Base: "https://oc.example"}, "notes", afterID)
	if rc != 2 || !strings.Contains(errOut, "does not upload files") {
		t.Fatalf("rc = %d, want 2 with the upload-first message:\n%s", rc, errOut)
	}
}

func TestDiffRefusesAnArgumentThatIsNeitherAnIDNorADocumentAddress(t *testing.T) {
	for _, arg := range []string{"att-", "att-0123456789", "att-0123456789AB", "nonsense"} {
		rc, _, errOut := runDiff(t, Config{Base: "https://oc.example"}, beforeID, arg)
		if rc != 2 {
			t.Errorf("%q: rc = %d, want 2", arg, rc)
		}
		if !strings.Contains(errOut, "after side") {
			t.Errorf("%q: the message must name WHICH side is wrong:\n%s", arg, errOut)
		}
	}
}

func TestDiffRejectsAMalformedDocumentAddress(t *testing.T) {
	for name, arg := range map[string]string{
		"too few segments":  "doc:lessons/mira/current",
		"too many":          "doc:lessons/mira/current/text/extra",
		"an empty segment":  "doc:lessons//current/text",
		"a traversing key":  "doc:lessons/../current/text",
		"an at that is not": "doc:lessons/mira/latest/text",
		"a zero revision":   "doc:lessons/mira/0/text",
		"a padded at":       "doc:lessons/mira/ current /text",
	} {
		rc, out, errOut := runDiff(t, Config{Base: "https://oc.example"}, arg, afterID)
		if rc != 2 {
			t.Errorf("%s (%q): rc = %d, want 2", name, arg, rc)
		}
		if out != "" {
			t.Errorf("%s: printed a url for an address it refused: %q", name, out)
		}
		if !strings.Contains(errOut, "diff:") {
			t.Errorf("%s: no diagnostic:\n%s", name, errOut)
		}
	}
}

// `doc:` is judged as an ADDRESS even when a file of that name is sitting
// there: the prefix is the member saying which of the two vocabularies they
// meant, and a filesystem probe must not overrule it.
func TestDiffNamesADocumentEvenWhenAFileOfThatNameExists(t *testing.T) {
	dir := t.TempDir()
	addr := "doc:lessons/mira/current/text"
	if err := os.MkdirAll(filepath.Join(dir, "doc:lessons/mira/current"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, addr), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	rc, out, errOut := runDiff(t, Config{Base: "https://oc.example"}, addr, afterID)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut)
	}
	if !strings.Contains(out, url.QueryEscape(addr)) {
		t.Errorf("the document address did not reach the url: %q", out)
	}
}

// --external is the ONLY flavour that costs a request, and the link it prints
// is the server's server-relative path with this reader's own origin in front —
// the same posture get_chat_attachment_share_link has.
func TestDiffExternalMintsTheSignedLinkAndAbsolutizesIt(t *testing.T) {
	var asked *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"url":"/diff?after=` + afterID + `&before=` + beforeID + `&sig=SIG"}`))
	}))
	t.Cleanup(srv.Close)

	var out, errOut bytes.Buffer
	rc := cmdDiff(srv.Client(), Config{Base: srv.URL, Token: "tok"},
		beforeID, afterID, "", "", true, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut.String())
	}
	if asked == nil || asked.Path != "/api/diff/share-link" {
		t.Fatalf("asked %v, want the mint route", asked)
	}
	if got := asked.Query().Get("before"); got != beforeID {
		t.Errorf("mint request before = %q, want %q", got, beforeID)
	}
	want := srv.URL + "/diff?after=" + afterID + "&before=" + beforeID + "&sig=SIG\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}

func TestDiffExternalRefusesWithoutAToken(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := cmdDiff(nil, Config{Base: "https://oc.example"}, beforeID, afterID, "", "", true, &out, &errOut)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3", rc)
	}
	if !strings.Contains(errOut.String(), "OC_TOKEN") {
		t.Errorf("message must name the missing credential:\n%s", errOut.String())
	}
}

func TestDiffExternalReportsTheServersRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":{"message":"the before side: unsayable"}}`))
	}))
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	rc := cmdDiff(srv.Client(), Config{Base: srv.URL, Token: "tok"},
		beforeID, afterID, "", "", true, &out, &errOut)
	if rc != 4 {
		t.Fatalf("rc = %d, want 4", rc)
	}
	if !strings.Contains(errOut.String(), "unsayable") {
		t.Errorf("the server's own words must reach the member:\n%s", errOut.String())
	}
}

// --help is the SINGLE AUTHORITY on the parameters (seeds/system_interaction.md
// deliberately carries none of them and points here), so anything a member
// cannot find here has nowhere else to be found.
func TestDiffUsageIsCompleteEnoughToBeTheOnlyAuthority(t *testing.T) {
	var b bytes.Buffer
	diffUsage(&b)
	for _, want := range []string{
		"att-0123456789ab", "doc:<kind>/<key>/<at>/<field>",
		"current", "seed", "list_document_history",
		"--label-before", "--label-after", "--external",
		"ocagent upload", "no login",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("usage never mentions %q — the seed points here for it", want)
		}
	}
}
