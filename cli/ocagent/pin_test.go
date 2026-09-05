package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// TestPinStreamsFileToAddRouteAndPrintsReceipt: the happy path on T-92's
// one-call ADD door — the bytes arrive byte-exact under the agent token at
// /api/tasks/{task_id}/artifacts/upload, ?name= (required) and the optional
// ?description=/?mime= ride the query with the basename as ?filename=, and
// stdout is EXACTLY the artifact id then the server's receipt JSON.
func TestPinStreamsFileToAddRouteAndPrintsReceipt(t *testing.T) {
	blob := bytes.Repeat([]byte("%PDF-1.7 report"), 500)
	receipt := `{"task_id":"t-92","artifact_id":"ta-abc123","artifact_count":3}`
	srv, gotReq, gotBody := uploadServer(t, 200, receipt)
	path := writeTempFile(t, "rollback plan.pdf", blob)

	var out, errOut bytes.Buffer
	rc := cmdPin(srv.Client(), Config{BaseConfigured: true, Base: srv.URL, Token: "tok-k", ID: "kyle"},
		path, "t-92", "", "Rollback plan", "what to do when the migration wedges", "application/pdf", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (stderr: %s)", rc, errOut.String())
	}
	if got := gotReq.Header.Get("Authorization"); got != "Bearer tok-k" {
		t.Fatalf("Authorization = %q, want the agent Bearer token", got)
	}
	if gotReq.URL.Path != "/api/tasks/t-92/artifacts/upload" {
		t.Fatalf("path = %q, want the one-call add route", gotReq.URL.Path)
	}
	q := gotReq.URL.Query()
	if q.Get("name") != "Rollback plan" ||
		q.Get("description") != "what to do when the migration wedges" ||
		q.Get("filename") != "rollback plan.pdf" ||
		q.Get("mime") != "application/pdf" {
		t.Fatalf("query = %v, want name/description/filename/mime", q)
	}
	if !bytes.Equal(*gotBody, blob) {
		t.Fatalf("body did not round-trip (%d vs %d bytes)", len(*gotBody), len(blob))
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || lines[0] != "ta-abc123" || lines[1] != receipt {
		t.Fatalf("stdout = %q, want artifact id line then receipt JSON", out.String())
	}
}

// TestPinReplaceUsesReplaceRouteAndOmitsUnsetText: --replace switches to the
// raw-body REPLACE twin (the artifact id in the path, kept in the receipt), and
// an unset --name/--description are OMITTED from the query rather than sent
// blank — blank is what CLEARS a description on that route.
func TestPinReplaceUsesReplaceRouteAndOmitsUnsetText(t *testing.T) {
	srv, gotReq, _ := uploadServer(t, 200,
		`{"task_id":"t-92","artifact_id":"ta-keep","artifact_count":3,"version_count":2}`)
	path := writeTempFile(t, "shot.png", []byte("\x89PNG\r\n\x1a\nxx"))

	var out, errOut bytes.Buffer
	if rc := cmdPin(srv.Client(), Config{BaseConfigured: true, Base: srv.URL, Token: "t"},
		path, "t-92", "ta-keep", "", "", "", &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d (stderr: %s)", rc, errOut.String())
	}
	if gotReq.URL.Path != "/api/tasks/t-92/artifact/ta-keep/replace/upload" {
		t.Fatalf("path = %q, want the one-call replace route", gotReq.URL.Path)
	}
	q := gotReq.URL.Query()
	if _, has := q["name"]; has {
		t.Fatalf("an unset --name must be omitted (carried forward), got %v", q)
	}
	if _, has := q["description"]; has {
		t.Fatalf("an unset --description must be omitted (carried forward), got %v", q)
	}
	if _, has := q["mime"]; has {
		t.Fatalf("mime must be omitted when unset (the server sniffs), got %v", q)
	}
	if q.Get("filename") != "shot.png" {
		t.Fatalf("filename = %q", q.Get("filename"))
	}
	if first := strings.SplitN(out.String(), "\n", 2)[0]; first != "ta-keep" {
		t.Fatalf("stdout id line = %q, want the id the replace KEEPS", first)
	}
}

// TestPinErrorExitCodes: the documented exit-code contract — 3 auth (no token /
// 401 / 403 executor guard), 4 refused (400 cap, 404 unknown task, 409 frozen
// deliverables), 5 unexpected status or an unparseable 200, 1 filesystem faults.
func TestPinErrorExitCodes(t *testing.T) {
	path := writeTempFile(t, "f.bin", []byte("bytes"))

	t.Run("no token is 3", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdPin(http.DefaultClient, Config{BaseConfigured: true, Base: "http://irrelevant"},
			path, "t-1", "", "n", "", "", &out, &errOut); rc != 3 {
			t.Fatalf("rc = %d, want 3", rc)
		}
	})
	t.Run("no OC_BASE is 3", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdPin(http.DefaultClient, Config{Base: defaultBase, Token: "t"},
			path, "t-1", "", "n", "", "", &out, &errOut); rc != 3 {
			t.Fatalf("rc = %d, want 3", rc)
		}
	})
	for name, tc := range map[string]struct {
		status int
		body   string
		want   int
	}{
		"401 is 3":              {401, `{"error":{"code":"unauthorized","message":"x"}}`, 3},
		"403 executor is 3":     {403, `{"error":{"code":"forbidden","message":"not the executor"}}`, 3},
		"400 cap is 4":          {400, `{"error":{"code":"validation_error","message":"attachment exceeds the 100 MB size limit"}}`, 4},
		"404 unknown task is 4": {404, `{"error":{"code":"not_found","message":"task 't-nope' not found"}}`, 4},
		"409 frozen is 4":       {409, `{"error":{"code":"conflict","message":"task is done: deliverables are frozen"}}`, 4},
		"500 is 5":              {500, `{"error":{"code":"internal_error","message":"x"}}`, 5},
		"unparseable 200 is 5":  {200, `not json`, 5},
		"200 with empty id":     {200, `{"task_id":"t-1","artifact_id":"","artifact_count":0}`, 5},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _, _ := uploadServer(t, tc.status, tc.body)
			var out, errOut bytes.Buffer
			if rc := cmdPin(srv.Client(), Config{BaseConfigured: true, Base: srv.URL, Token: "t"},
				path, "t-1", "", "n", "", "", &out, &errOut); rc != tc.want {
				t.Fatalf("rc = %d, want %d (stderr: %s)", rc, tc.want, errOut.String())
			}
			if out.Len() != 0 {
				t.Fatalf("stdout must stay empty on failure, got %q", out.String())
			}
		})
	}
	t.Run("missing file is 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdPin(http.DefaultClient, Config{BaseConfigured: true, Base: "http://irrelevant", Token: "t"},
			path+".absent", "t-1", "", "n", "", "", &out, &errOut); rc != 1 {
			t.Fatalf("rc = %d, want 1", rc)
		}
	})
	t.Run("directory is 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdPin(http.DefaultClient, Config{BaseConfigured: true, Base: "http://irrelevant", Token: "t"},
			t.TempDir(), "t-1", "", "n", "", "", &out, &errOut); rc != 1 {
			t.Fatalf("rc = %d, want 1", rc)
		}
	})
}

// TestPinDispatchUsage: realMain's flag surface — a missing <path>, a missing
// --task and a missing --name (without --replace) are each usage (2) and print
// the subcommand's own usage block; the flags parse on either side of the
// positional; and `pin` is advertised in `ocagent --help`.
func TestPinDispatchUsage(t *testing.T) {
	for name, argv := range map[string][]string{
		"missing path":              {"pin", "--task", "t-1", "--name", "n"},
		"missing --task":            {"pin", "/tmp/whatever", "--name", "n"},
		"missing --name on an add":  {"pin", "/tmp/whatever", "--task", "t-1"},
		"blank --name on an add":    {"pin", "/tmp/whatever", "--task", "t-1", "--name", "   "},
		"extra positional argument": {"pin", "/tmp/a", "/tmp/b", "--task", "t-1", "--name", "n"},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if rc := realMain(argv, func(string) string { return "" },
				strings.NewReader(""), &out); rc != 2 {
				t.Fatalf("rc = %d, want 2 (out: %s)", rc, out.String())
			}
			if !strings.Contains(out.String(), "usage: ocagent pin") {
				t.Fatalf("usage text missing: %q", out.String())
			}
		})
	}

	srv, gotReq, _ := uploadServer(t, 200,
		`{"task_id":"t-9","artifact_id":"ta-d1","artifact_count":1}`)
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
	var out bytes.Buffer
	if rc := realMain([]string{"pin", path, "--task", "t-9", "--name", "notes", "--mime", "text/plain"},
		env, strings.NewReader(""), &out); rc != 0 {
		t.Fatalf("rc = %d, want 0 (out: %s)", rc, out.String())
	}
	q := gotReq.URL.Query()
	if q.Get("mime") != "text/plain" || q.Get("name") != "notes" {
		t.Fatalf("flags after the positional must parse, got %v", q)
	}

	// --help discoverability: the subcommand list and the subcommand's own
	// --help are the two places a reader looks for it.
	var help bytes.Buffer
	if rc := realMain([]string{"--help"}, func(string) string { return "" },
		strings.NewReader(""), &help); rc != 0 {
		t.Fatalf("--help rc = %d, want 0", rc)
	}
	if !strings.Contains(help.String(), "  pin ") {
		t.Fatalf("`pin` missing from ocagent --help: %q", help.String())
	}
	var subHelp bytes.Buffer
	if rc := realMain([]string{"pin", "--help"}, func(string) string { return "" },
		strings.NewReader(""), &subHelp); rc != 2 {
		t.Fatalf("`pin --help` rc = %d, want 2 (flag.ErrHelp)", rc)
	}
	if !strings.Contains(subHelp.String(), "usage: ocagent pin") ||
		!strings.Contains(subHelp.String(), "--replace") {
		t.Fatalf("`pin --help` text missing: %q", subHelp.String())
	}
}
