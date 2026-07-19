package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// attachmentServer serves one blob at /api/chat/attachment/<id> with the given
// headers, capturing the request's Authorization header. Any other id is a 404
// with the server's JSON detail shape.
func attachmentServer(t *testing.T, id string, body []byte, headers map[string]string) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/chat/attachment/"+id {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"detail":"attachment not found"}`))
			return
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotAuth
}

// TestDownloadStreamsBlobToOutDirWithDispositionName: the happy path — a zip
// with a Content-Disposition (ASCII fallback + RFC 5987 filename*) lands under
// --out under its TRUE (filename*) name, byte-exact, authed with the agent
// token, and stdout carries ONLY the absolute path.
func TestDownloadStreamsBlobToOutDirWithDispositionName(t *testing.T) {
	blob := bytes.Repeat([]byte("PK\x03\x04zipzip"), 1000)
	srv, gotAuth := attachmentServer(t, "att-abc123", blob, map[string]string{
		"Content-Type":        "application/zip",
		"Content-Disposition": `attachment; filename="bundle.zip"; filename*=UTF-8''bundle.zip`,
	})
	dir := t.TempDir()
	cfg := Config{Base: srv.URL, Token: "tok-k", ID: "kyle"}

	var out, errOut bytes.Buffer
	rc := cmdDownload(srv.Client(), cfg, "att-abc123", dir, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	if *gotAuth != "Bearer tok-k" {
		t.Fatalf("Authorization = %q, want the agent Bearer token", *gotAuth)
	}
	want := filepath.Join(dir, "bundle.zip")
	// stdout is EXACTLY the landed absolute path + newline (script-capturable).
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("stdout = %q, want the absolute path %q", got, want)
	}
	landed, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("landed file unreadable: %v", err)
	}
	if !bytes.Equal(landed, blob) {
		t.Fatalf("landed bytes differ: got %d bytes, want %d", len(landed), len(blob))
	}
}

// TestDownloadPrefersRFC5987UTF8Name: a non-ASCII true name rides filename*
// (percent-encoded); the file must land under the DECODED UTF-8 name, not the
// stripped ASCII fallback.
func TestDownloadPrefersRFC5987UTF8Name(t *testing.T) {
	srv, _ := attachmentServer(t, "att-zh", []byte("zipbytes"), map[string]string{
		"Content-Disposition": `attachment; filename=".zip"; filename*=UTF-8''%E8%A8%AD%E8%A8%88.zip`,
	})
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	rc := cmdDownload(srv.Client(), Config{Base: srv.URL, Token: "t"}, "att-zh", dir, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	if want := filepath.Join(dir, "設計.zip"); strings.TrimSpace(out.String()) != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

// TestDownloadImageNoDispositionFallsBackToID: an image serves with NO
// Content-Disposition at all — the attachment id names the file.
func TestDownloadImageNoDispositionFallsBackToID(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfakepixels")
	srv, _ := attachmentServer(t, "att-img42", png, map[string]string{
		"Content-Type": "image/png",
	})
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	rc := cmdDownload(srv.Client(), Config{Base: srv.URL, Token: "t"}, "att-img42", dir, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	if want := filepath.Join(dir, "att-img42"); strings.TrimSpace(out.String()) != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
	landed, _ := os.ReadFile(filepath.Join(dir, "att-img42"))
	if !bytes.Equal(landed, png) {
		t.Fatalf("landed bytes differ from the served image")
	}
}

// TestDownloadPathTraversalFilenameIsBasenamed: a hostile filename ("../../evil")
// must land INSIDE the target dir under its basename — never above it.
func TestDownloadPathTraversalFilenameIsBasenamed(t *testing.T) {
	srv, _ := attachmentServer(t, "att-evil", []byte("x"), map[string]string{
		"Content-Disposition": `attachment; filename="../../evil.txt"; filename*=UTF-8''..%2F..%2Fevil.txt`,
	})
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b") // nested so ../../ would escape into root
	var out, errOut bytes.Buffer
	rc := cmdDownload(srv.Client(), Config{Base: srv.URL, Token: "t"}, "att-evil", dir, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	if want := filepath.Join(dir, "evil.txt"); strings.TrimSpace(out.String()) != want {
		t.Fatalf("stdout = %q, want the traversal-stripped %q", out.String(), want)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("traversal escaped: a file landed OUTSIDE the target dir")
	}
}

// TestDownloadDefaultDirIsWorkdirTmpAttachments: with no --out the blob lands
// under <cwd>/tmp/attachments (the agent-workdir convention).
func TestDownloadDefaultDirIsWorkdirTmpAttachments(t *testing.T) {
	srv, _ := attachmentServer(t, "att-dflt", []byte("hello"), map[string]string{
		"Content-Disposition": `attachment; filename="notes.txt"; filename*=UTF-8''notes.txt`,
	})
	wd := t.TempDir()
	t.Chdir(wd)
	var out, errOut bytes.Buffer
	rc := cmdDownload(srv.Client(), Config{Base: srv.URL, Token: "t"}, "att-dflt", "", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	got := strings.TrimSpace(out.String())
	// Compare via EvalSymlinks — macOS TempDir rides /private symlinks.
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(wd, "tmp", "attachments"))
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("stdout path %q does not exist: %v", got, err)
	}
	if gotResolved != filepath.Join(wantDir, "notes.txt") {
		t.Fatalf("landed at %q, want under the default %q", gotResolved, wantDir)
	}
}

// TestDownloadErrorExitCodes: 404 → 4, 401/403 → 3, other HTTP → 5, network → 1,
// missing token → 3 — each with a stderr diagnostic and NOTHING on stdout.
func TestDownloadErrorExitCodes(t *testing.T) {
	statusSrv := func(code int) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"detail":"nope"}`))
		}))
		t.Cleanup(s.Close)
		return s
	}
	cases := []struct {
		name   string
		status int
		wantRC int
	}{
		{"not found 404", 404, 4},
		{"auth 401", 401, 3},
		{"auth 403", 403, 3},
		{"server error 500", 500, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := statusSrv(c.status)
			var out, errOut bytes.Buffer
			rc := cmdDownload(srv.Client(), Config{Base: srv.URL, Token: "t"}, "att-x", t.TempDir(), &out, &errOut)
			if rc != c.wantRC {
				t.Fatalf("rc = %d, want %d", rc, c.wantRC)
			}
			if out.Len() != 0 {
				t.Fatalf("stdout must stay empty on failure; got %q", out.String())
			}
			if errOut.Len() == 0 {
				t.Fatalf("stderr must carry a diagnostic")
			}
		})
	}

	t.Run("network failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // connection refused
		var out, errOut bytes.Buffer
		rc := cmdDownload(newStreamingClient(), Config{Base: srv.URL, Token: "t"}, "att-x", t.TempDir(), &out, &errOut)
		if rc != 1 {
			t.Fatalf("rc = %d, want 1", rc)
		}
		if !strings.Contains(errOut.String(), "network") {
			t.Fatalf("stderr should say the request failed at transport: %q", errOut.String())
		}
	})

	t.Run("no token fails fast", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdDownload(newStreamingClient(), Config{Base: "http://127.0.0.1:1"}, "att-x", t.TempDir(), &out, &errOut)
		if rc != 3 {
			t.Fatalf("rc = %d, want 3", rc)
		}
		if !strings.Contains(errOut.String(), "OC_TOKEN") {
			t.Fatalf("stderr should name the missing OC_TOKEN: %q", errOut.String())
		}
	})
}

// TestDownloadDispatchUsage: realMain wiring — a missing id is usage (2), and
// the flag works on EITHER side of the positional (stdlib flag stops at the
// first positional, so main.go re-parses the tail).
func TestDownloadDispatchUsage(t *testing.T) {
	var out bytes.Buffer
	if rc := realMain([]string{"download"}, testEnv(nil), strings.NewReader(""), &out); rc != 2 {
		t.Fatalf("no-arg download rc = %d, want 2", rc)
	}
	if !strings.Contains(out.String(), "attachment-id") {
		t.Fatalf("usage text should name the missing <attachment-id>: %q", out.String())
	}

	// id + trailing --out parses (proven by it reaching the network stage and
	// failing there with a NON-usage code against an unroutable base).
	srv, _ := attachmentServer(t, "att-ok", []byte("y"), map[string]string{})
	dir := t.TempDir()
	env := testEnv(map[string]string{"OC_BASE": srv.URL, "OC_TOKEN": "t"})
	var out2 bytes.Buffer
	if rc := realMain([]string{"download", "att-ok", "--out", dir}, env, strings.NewReader(""), &out2); rc != 0 {
		t.Fatalf("download <id> --out <dir> rc = %d, want 0 (out: %s)", rc, out2.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "att-ok")); err != nil {
		t.Fatalf("blob did not land under --out: %v", err)
	}
}

// TestFilenameFromDisposition: the header→name table, including the absent /
// malformed degenerate rows.
func TestFilenameFromDisposition(t *testing.T) {
	cases := []struct{ disp, want string }{
		{"", ""},
		{`attachment; filename="a.zip"; filename*=UTF-8''a.zip`, "a.zip"},
		{`inline; filename="fallback.pdf"; filename*=UTF-8''%E5%A0%B1%E5%91%8A.pdf`, "報告.pdf"},
		{`attachment; filename="only-plain.bin"`, "only-plain.bin"},
		{`inline; filename*=UTF-8''trailing.txt; foo=bar`, "trailing.txt"},
		{`attachment; filename*=UTF-8''%ZZbad`, ""}, // bad pct-encoding, no plain fallback
		{`attachment`, ""},
	}
	for _, c := range cases {
		if got := filenameFromDisposition(c.disp); got != c.want {
			t.Errorf("filenameFromDisposition(%q) = %q, want %q", c.disp, got, c.want)
		}
	}
}

// TestSanitizeFilename: the traversal/degenerate table — always a single safe
// path component or the fallback.
func TestSanitizeFilename(t *testing.T) {
	cases := []struct{ name, want string }{
		{"plain.txt", "plain.txt"},
		{"../../etc/passwd", "passwd"},
		{"/abs/path.bin", "path.bin"},
		{`..\..\win.dll`, "win.dll"},
		{"..", "FB"},
		{".", "FB"},
		{"", "FB"},
		{"   ", "FB"},
		{"nested/dir/name.zip", "name.zip"},
	}
	for _, c := range cases {
		if got := sanitizeFilename(c.name, "FB"); got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
