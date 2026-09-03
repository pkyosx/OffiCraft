package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// capturedUpload is one POST /api/chat/attachments the CLI made, in order.
type capturedUpload struct {
	mime     string
	filename string
	body     string
}

// diffServer mints a fresh id per upload and records what arrived, so a test
// can assert BOTH the order of the three posts and that the pair names the ids
// the server actually handed back — the transposition this subcommand exists to
// prevent is invisible to any check that only looks at the last request.
func diffServer(t *testing.T) (*httptest.Server, *[]capturedUpload) {
	t.Helper()
	var seen []capturedUpload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := r.URL.Query()
		seen = append(seen, capturedUpload{
			mime:     q.Get("mime"),
			filename: q.Get("filename"),
			body:     string(body),
		})
		id := "att-" + string(rune('a'+len(seen)-1))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"` + id + `","mime":"` + q.Get("mime") +
			`","filename":"` + q.Get("filename") + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestDiffUploadsBothFilesThenAPairNamingTheirIDs(t *testing.T) {
	srv, seen := diffServer(t)
	beforePath := writeTempFile(t, "old.txt", []byte("alpha\nbravo\n"))
	afterPath := writeTempFile(t, "new.txt", []byte("alpha\nBRAVO\n"))

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	if rc := cmdDiff(srv.Client(), cfg, beforePath, afterPath, "", "", &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut.String())
	}

	if len(*seen) != 3 {
		t.Fatalf("want three uploads (two documents then the pair), got %d", len(*seen))
	}
	// The two documents go up as themselves — no declared type, so the server
	// keeps its own sniffing, exactly as `upload` with no --mime.
	if (*seen)[0].body != "alpha\nbravo\n" || (*seen)[0].mime != "" {
		t.Errorf("first upload = %+v, want the before file's bytes untyped", (*seen)[0])
	}
	if (*seen)[1].body != "alpha\nBRAVO\n" || (*seen)[1].mime != "" {
		t.Errorf("second upload = %+v, want the after file's bytes untyped", (*seen)[1])
	}

	// The pair is typed, and it is a POINTER PAIR: the documents' bytes must not
	// appear in it a second time.
	pair := (*seen)[2]
	if pair.mime != diffAttachmentMime {
		t.Errorf("pair mime = %q, want %q", pair.mime, diffAttachmentMime)
	}
	type side struct {
		AttachmentID string `json:"attachment_id"`
		Label        string `json:"label"`
	}
	var got struct {
		Before side `json:"before"`
		After  side `json:"after"`
	}
	if err := json.Unmarshal([]byte(pair.body), &got); err != nil {
		t.Fatalf("pair body is not JSON: %v (%s)", err, pair.body)
	}
	if got.Before.AttachmentID != "att-a" || got.After.AttachmentID != "att-b" {
		t.Errorf("pair names %q/%q, want the ids the server minted (att-a/att-b) in that order",
			got.Before.AttachmentID, got.After.AttachmentID)
	}
	// Unlabelled columns are the state the owner could not read; the file's own
	// name is the default rather than nothing.
	if got.Before.Label != "old.txt" || got.After.Label != "new.txt" {
		t.Errorf("labels = %q/%q, want the two basenames", got.Before.Label, got.After.Label)
	}

	// stdout mirrors `upload`: the id, then the server's own ref JSON.
	lines := bytes.Split(bytes.TrimRight(out.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 2 || string(lines[0]) != "att-c" {
		t.Errorf("stdout = %q, want the pair's id then its ref JSON", out.String())
	}
}

func TestDiffLabelsOverrideTheFileNames(t *testing.T) {
	srv, seen := diffServer(t)
	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	rc := cmdDiff(srv.Client(), cfg,
		writeTempFile(t, "a.txt", []byte("x")),
		writeTempFile(t, "b.txt", []byte("y")),
		"9/2 21:12", "目前存檔內容", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d (%s)", rc, errOut.String())
	}
	type side struct {
		Label string `json:"label"`
	}
	var got struct {
		Before side `json:"before"`
		After  side `json:"after"`
	}
	if err := json.Unmarshal([]byte((*seen)[2].body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Before.Label != "9/2 21:12" || got.After.Label != "目前存檔內容" {
		t.Errorf("labels = %q/%q, want the given headings", got.Before.Label, got.After.Label)
	}
}

// A pair whose sides never uploaded would be a compare attachment that can
// never draw. The run has to stop at the failure, not carry on and mint one.
func TestDiffStopsBeforeMintingAPairWhenASideFails(t *testing.T) {
	var seen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if seen == 2 {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"attachment is empty"}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"att-a","mime":"text/plain","filename":"a.txt"}`))
	}))
	t.Cleanup(srv.Close)

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	rc := cmdDiff(srv.Client(), cfg,
		writeTempFile(t, "a.txt", []byte("x")),
		writeTempFile(t, "b.txt", []byte("y")),
		"", "", &out, &errOut)

	if rc != 4 {
		t.Errorf("rc = %d, want 4 (the server rejected a side)", rc)
	}
	if seen != 2 {
		t.Errorf("%d requests made, want 2 — the pair must not be posted after a side failed", seen)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing: there is no attachment to name", out.String())
	}
}

func TestDiffRefusesWithoutAToken(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := cmdDiff(http.DefaultClient, Config{Base: "http://unused"},
		"a.txt", "b.txt", "", "", &out, &errOut)
	if rc != 3 {
		t.Errorf("rc = %d, want 3 (no token)", rc)
	}
}

// T-59 second round: a side may name ONE FIELD OF A DOCUMENT instead of a file
// (`doc:<kind>/<key>/<at>/<field>`). Nothing is uploaded for such a side — the
// document already has an address — so the whole point is that the number of
// uploads goes DOWN, and that the pair carries the address verbatim.
func TestDiffNamesADocumentSideWithoutUploadingAnything(t *testing.T) {
	srv, seen := diffServer(t)
	afterPath := writeTempFile(t, "new.txt", []byte("alpha\nBRAVO\n"))

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	rc := cmdDiff(srv.Client(), cfg, "doc:lessons/mira/12/text", afterPath, "", "", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut.String())
	}

	// TWO posts, not three: the document side is a reference, and re-uploading
	// a copy of it is exactly what this shape exists to avoid.
	if len(*seen) != 2 {
		t.Fatalf("want two uploads (the file, then the pair), got %d", len(*seen))
	}
	pair := (*seen)[1]
	if pair.mime != diffAttachmentMime {
		t.Errorf("pair mime = %q, want %q", pair.mime, diffAttachmentMime)
	}
	var got struct {
		Before struct {
			Doc struct {
				Kind  string `json:"kind"`
				Key   string `json:"key"`
				At    string `json:"at"`
				Field string `json:"field"`
			} `json:"doc"`
			AttachmentID string `json:"attachment_id"`
			Label        string `json:"label"`
		} `json:"before"`
		After struct {
			AttachmentID string `json:"attachment_id"`
			Label        string `json:"label"`
		} `json:"after"`
	}
	if err := json.Unmarshal([]byte(pair.body), &got); err != nil {
		t.Fatalf("pair body is not JSON: %v (%s)", err, pair.body)
	}
	if got.Before.Doc.Kind != "lessons" || got.Before.Doc.Key != "mira" ||
		got.Before.Doc.At != "12" || got.Before.Doc.Field != "text" {
		t.Errorf("document side = %+v, want the four segments verbatim", got.Before.Doc)
	}
	// Exactly one shape per side: a document side that also carried a blob id
	// would be refused by the server, and one that carried a label would
	// override the reader's own localized heading (「版本 #12」/「目前存檔內容」)
	// with something written here in one language.
	if got.Before.AttachmentID != "" || got.Before.Label != "" {
		t.Errorf("document side = %+v, want no attachment_id and no label", got.Before)
	}
	// The file side is unaffected: still uploaded, still labelled by its name.
	if got.After.AttachmentID != "att-a" || got.After.Label != "new.txt" {
		t.Errorf("file side = %+v, want the minted id and the basename", got.After)
	}
}

func TestDiffLabelsADocumentSideOnlyWhenAskedTo(t *testing.T) {
	srv, seen := diffServer(t)

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	rc := cmdDiff(srv.Client(), cfg,
		"doc:global_context/global/seed/text", "doc:global_context/global/current/text",
		"出廠", "", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut.String())
	}
	// Both sides are references: ONE post, the pair itself.
	if len(*seen) != 1 {
		t.Fatalf("want one upload (the pair alone), got %d", len(*seen))
	}
	var got struct {
		Before struct {
			Label string `json:"label"`
		} `json:"before"`
		After struct {
			Label string `json:"label"`
		} `json:"after"`
	}
	if err := json.Unmarshal([]byte((*seen)[0].body), &got); err != nil {
		t.Fatalf("pair body is not JSON: %v", err)
	}
	if got.Before.Label != "出廠" {
		t.Errorf("before label = %q, want the one given", got.Before.Label)
	}
	if got.After.Label != "" {
		t.Errorf("after label = %q, want none so the reader writes its own", got.After.Label)
	}
}

// A malformed address must be caught BEFORE anything is uploaded: there is no
// blob GC yet, so a first side that went up while the second was rejected is a
// file nothing will ever point at and nothing will ever collect.
func TestDiffRejectsAMalformedDocumentAddressBeforeUploadingAnything(t *testing.T) {
	for _, tc := range []struct{ name, arg string }{
		{"too few segments", "doc:lessons/mira/current"},
		{"too many segments", "doc:lessons/mira/current/text/extra"},
		{"an empty segment", "doc:lessons//current/text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := diffServer(t)
			beforePath := writeTempFile(t, "old.txt", []byte("alpha\n"))

			var out, errOut bytes.Buffer
			cfg := Config{Base: srv.URL, Token: "tok"}
			if rc := cmdDiff(srv.Client(), cfg, beforePath, tc.arg, "", "", &out, &errOut); rc != 2 {
				t.Fatalf("rc = %d, want 2 (usage) — %s", rc, errOut.String())
			}
			if len(*seen) != 0 {
				t.Fatalf("want nothing uploaded, got %d post(s): %+v", len(*seen), *seen)
			}
		})
	}
}

// The prefix is only ambiguous if a REAL PATH spells the same four segments —
// which needs directories, since no single filename may contain "/". Rare, and
// therefore exactly the case where picking a meaning silently would be worst:
// the reader would get a comparison against a document they never named, or a
// file they never named, with no way to tell which from the output.
func TestDiffRefusesWhenADocumentAddressIsAlsoARealFile(t *testing.T) {
	srv, seen := diffServer(t)
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("doc:lessons/mira/current", 0o755); err != nil {
		t.Fatalf("cannot build the colliding path: %v", err)
	}
	if err := os.WriteFile("doc:lessons/mira/current/text", []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("cannot build the colliding path: %v", err)
	}

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	if rc := cmdDiff(srv.Client(), cfg, "doc:lessons/mira/current/text",
		"doc:global_context/global/current/text", "", "", &out, &errOut); rc != 2 {
		t.Fatalf("rc = %d, want 2 (usage) — %s", rc, errOut.String())
	}
	if len(*seen) != 0 {
		t.Fatalf("want nothing uploaded, got %d post(s)", len(*seen))
	}
	if !bytes.Contains(errOut.Bytes(), []byte("both a document address and a real file")) {
		t.Errorf("stderr = %q, want it to name the ambiguity", errOut.String())
	}
}
