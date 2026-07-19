package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadServer accepts POST /api/chat/attachments, captures the request's
// Authorization header, query params, and streamed body, and answers the
// canned status/body.
func uploadServer(t *testing.T, status int, respBody string) (*httptest.Server, *http.Request, *[]byte) {
	t.Helper()
	var gotReq http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = *r.Clone(r.Context())
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotReq, &gotBody
}

func writeTempFile(t *testing.T, name string, blob []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestUploadStreamsFileAndPrintsIDThenRefJSON: the happy path — the file's
// bytes arrive byte-exact under the agent token, the basename rides
// ?filename=, --mime rides ?mime=, and stdout is EXACTLY the id line then the
// server's ref JSON (script-capturable).
func TestUploadStreamsFileAndPrintsIDThenRefJSON(t *testing.T) {
	blob := bytes.Repeat([]byte("PK\x03\x04zipzip"), 1000)
	refJSON := `{"id":"att-up1","mime":"application/zip","filename":"bundle.zip"}`
	srv, gotReq, gotBody := uploadServer(t, 200, refJSON)
	path := writeTempFile(t, "bundle.zip", blob)

	var out, errOut bytes.Buffer
	rc := cmdUpload(srv.Client(), Config{Base: srv.URL, Token: "tok-k", ID: "kyle"},
		path, "application/zip", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	if got := gotReq.Header.Get("Authorization"); got != "Bearer tok-k" {
		t.Fatalf("Authorization = %q, want the agent Bearer token", got)
	}
	if gotReq.URL.Path != "/api/chat/attachments" {
		t.Fatalf("path = %q", gotReq.URL.Path)
	}
	q := gotReq.URL.Query()
	if q.Get("filename") != "bundle.zip" || q.Get("mime") != "application/zip" {
		t.Fatalf("query = %v, want basename + declared mime", q)
	}
	if !bytes.Equal(*gotBody, blob) {
		t.Fatalf("body did not round-trip (%d vs %d bytes)", len(*gotBody), len(blob))
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || lines[0] != "att-up1" || lines[1] != refJSON {
		t.Fatalf("stdout = %q, want id line then ref JSON", out.String())
	}
}

// TestUploadOmitsMimeQueryWhenUnset: no --mime → no ?mime= (the server
// sniffs); the filename still rides.
func TestUploadOmitsMimeQueryWhenUnset(t *testing.T) {
	srv, gotReq, _ := uploadServer(t, 200, `{"id":"att-up2","mime":"image/png","filename":"shot.png"}`)
	path := writeTempFile(t, "shot.png", []byte("\x89PNG\r\n\x1a\nxx"))

	var out, errOut bytes.Buffer
	if rc := cmdUpload(srv.Client(), Config{Base: srv.URL, Token: "t"},
		path, "", &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d (stderr: %s)", rc, errOut.String())
	}
	q := gotReq.URL.Query()
	if _, has := q["mime"]; has {
		t.Fatalf("mime must be omitted when unset, got %v", q)
	}
	if q.Get("filename") != "shot.png" {
		t.Fatalf("filename = %q", q.Get("filename"))
	}
	var ref struct{ ID string }
	firstLine := strings.SplitN(out.String(), "\n", 2)[0]
	if json.Unmarshal([]byte(`{"ID":"`+firstLine+`"}`), &ref) != nil || ref.ID != "att-up2" {
		t.Fatalf("stdout id line = %q", firstLine)
	}
}

// TestUploadErrorExitCodes: the documented exit-code contract — 3 auth
// (no token / 401), 4 rejected (400 cap), 5 unexpected status or an
// unparseable 200, 1 filesystem faults (missing file, directory).
func TestUploadErrorExitCodes(t *testing.T) {
	path := writeTempFile(t, "f.bin", []byte("bytes"))

	t.Run("no token is 3", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdUpload(http.DefaultClient, Config{Base: "http://irrelevant"},
			path, "", &out, &errOut); rc != 3 {
			t.Fatalf("rc = %d, want 3", rc)
		}
	})
	for name, tc := range map[string]struct {
		status int
		body   string
		want   int
	}{
		"401 is 3":             {401, `{"error":{"code":"unauthorized","message":"x"}}`, 3},
		"400 cap is 4":         {400, `{"error":{"code":"bad_request","message":"attachment exceeds the 100 MB size limit"}}`, 4},
		"500 is 5":             {500, `{"error":{"code":"internal_error","message":"x"}}`, 5},
		"unparseable 200 is 5": {200, `not json`, 5},
		"200 with empty id":    {200, `{"id":""}`, 5},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _, _ := uploadServer(t, tc.status, tc.body)
			var out, errOut bytes.Buffer
			if rc := cmdUpload(srv.Client(), Config{Base: srv.URL, Token: "t"},
				path, "", &out, &errOut); rc != tc.want {
				t.Fatalf("rc = %d, want %d (stderr: %s)", rc, tc.want, errOut.String())
			}
			if out.Len() != 0 {
				t.Fatalf("stdout must stay empty on failure, got %q", out.String())
			}
		})
	}
	t.Run("missing file is 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdUpload(http.DefaultClient, Config{Base: "http://irrelevant", Token: "t"},
			filepath.Join(t.TempDir(), "absent.bin"), "", &out, &errOut); rc != 1 {
			t.Fatalf("rc = %d, want 1", rc)
		}
	})
	t.Run("directory is 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdUpload(http.DefaultClient, Config{Base: "http://irrelevant", Token: "t"},
			t.TempDir(), "", &out, &errOut); rc != 1 {
			t.Fatalf("rc = %d, want 1", rc)
		}
	})
}

// TestUploadDispatchUsage: realMain's flag surface — a missing <path> is
// usage (2), and --mime parses on either side of the positional.
func TestUploadDispatchUsage(t *testing.T) {
	var out bytes.Buffer
	if rc := realMain([]string{"upload"}, func(string) string { return "" },
		strings.NewReader(""), &out); rc != 2 {
		t.Fatalf("rc = %d, want 2 for missing <path>", rc)
	}
	if !strings.Contains(out.String(), "usage: ocagent upload") {
		t.Fatalf("usage text missing: %q", out.String())
	}

	srv, gotReq, _ := uploadServer(t, 200, `{"id":"att-up3","mime":"text/plain","filename":"a.txt"}`)
	path := writeTempFile(t, "a.txt", []byte("hi"))
	env := func(k string) string {
		switch k {
		case "OC_BASE":
			return srv.URL
		case "OC_TOKEN":
			return "tok"
		}
		return ""
	}
	var out2 bytes.Buffer
	if rc := realMain([]string{"upload", path, "--mime", "text/plain"}, env,
		strings.NewReader(""), &out2); rc != 0 {
		t.Fatalf("rc = %d, want 0 (out: %s)", rc, out2.String())
	}
	if gotReq.URL.Query().Get("mime") != "text/plain" {
		t.Fatalf("--mime after the positional must parse, got %v", gotReq.URL.Query())
	}
}
