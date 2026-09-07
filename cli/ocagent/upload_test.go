package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// fileWith writes one file into a fresh temp dir and returns its path.
func fileWith(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCmdUpload(t *testing.T) {
	configured := Config{Base: "https://station.example.com", BaseConfigured: true, Token: "tok-1"}
	const mintedRef = `{"id":"att-0123456789ab","mime":"text/plain","filename":"report.txt"}`

	t.Run("the file's bytes are streamed and the minted ref is printed", func(t *testing.T) {
		path := fileWith(t, "report.txt", "the bytes")
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		rc := cmdUpload(client, configured, path, "text/plain", &out, &errOut)

		if rc != 0 {
			t.Fatalf("cmdUpload returned %d, want 0", rc)
		}
		if out.String() != "att-0123456789ab\n"+mintedRef+"\n" {
			t.Fatalf("stdout %q, want the id then the server's own JSON", out.String())
		}
		wantErr := "[ocagent] upload: report.txt (9 bytes, text/plain) → att-0123456789ab\n"
		if errOut.String() != wantErr {
			t.Fatalf("stderr %q, want %q", errOut.String(), wantErr)
		}
		want := sentRequest{
			method: "POST",
			url:    "https://station.example.com/api/chat/attachments?filename=report.txt&mime=text%2Fplain",
			ua:     "ocagent/0.1",
			accept: "application/json",
			auth:   "Bearer tok-1",
			length: 9,
			body:   "the bytes",
		}
		if len(client.sent) != 1 || client.sent[0] != want {
			t.Fatalf("sent %+v, want %+v", client.sent, want)
		}
	})

	t.Run("no --mime leaves the sniffing to the server", func(t *testing.T) {
		path := fileWith(t, "report.txt", "x")
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		cmdUpload(client, configured, path, "  ", &out, &errOut)
		want := "https://station.example.com/api/chat/attachments?filename=report.txt"
		if client.sent[0].url != want {
			t.Fatalf("URL %q, want %q", client.sent[0].url, want)
		}
	})

	t.Run("a plus in the media type is escaped rather than reaching the server as a space", func(t *testing.T) {
		path := fileWith(t, "book.epub", "x")
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		cmdUpload(client, configured, path, "application/epub+zip", &out, &errOut)
		want := "https://station.example.com/api/chat/attachments?" +
			"filename=book.epub&mime=application%2Fepub%2Bzip"
		if client.sent[0].url != want {
			t.Fatalf("URL %q, want %q", client.sent[0].url, want)
		}
	})

	t.Run("stdout carries the server's body verbatim, not a re-serialisation", func(t *testing.T) {
		path := fileWith(t, "report.txt", "x")
		var out, errOut bytes.Buffer
		body := `{"id":"att-0123456789ab","mime":"text/plain","filename":"report.txt","future":"kept"}`
		rc := cmdUpload(canned(200, body), configured, path, "", &out, &errOut)
		if rc != 0 || out.String() != "att-0123456789ab\n"+body+"\n" {
			t.Fatalf("got (%d, %q), want (0, the id then the verbatim body)", rc, out.String())
		}
	})

	t.Run("no token refuses before the file is even opened", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		noToken := Config{Base: "https://station.example.com", BaseConfigured: true}
		rc := cmdUpload(client, noToken, filepath.Join(t.TempDir(), "absent.txt"), "", &out, &errOut)
		want := "[ocagent] upload: no OC_TOKEN configured — cannot make an authed upload.\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", the no-token refusal)",
				rc, out.String(), errOut.String())
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("an unset OC_BASE refuses before anything is sent", func(t *testing.T) {
		path := fileWith(t, "report.txt", "x")
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		unset := Config{Base: defaultBase, Token: "tok-1"}
		rc := cmdUpload(client, unset, path, "", &out, &errOut)
		want := "[ocagent] upload: no OC_BASE configured — nothing here knows which station " +
			"to talk to, and the built-in default is this machine's loopback address.\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", the OC_BASE refusal)",
				rc, out.String(), errOut.String())
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("a missing file is exit 1 and nothing is sent", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "absent.txt")
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		rc := cmdUpload(client, configured, missing, "", &out, &errOut)
		want := "[ocagent] upload: cannot open " + missing + ": open " + missing +
			": no such file or directory\n"
		if rc != 1 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (1, \"\", %q)", rc, out.String(), errOut.String(), want)
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("a directory is exit 1 and nothing is sent", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		client := canned(200, mintedRef)
		rc := cmdUpload(client, configured, dir, "", &out, &errOut)
		want := "[ocagent] upload: " + dir + " is a directory, not a file\n"
		if rc != 1 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (1, \"\", %q)", rc, out.String(), errOut.String(), want)
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("a transport failure is exit 1", func(t *testing.T) {
		path := fileWith(t, "report.txt", "x")
		var out, errOut bytes.Buffer
		rc := cmdUpload(failingHTTP("connection refused"), configured, path, "", &out, &errOut)
		want := "[ocagent] upload: request failed (network): connection refused\n"
		if rc != 1 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (1, \"\", the network reason)",
				rc, out.String(), errOut.String())
		}
	})

	statusCases := []struct {
		name    string
		status  int
		body    string
		wantRC  int
		wantErr string
	}{
		{"401 is an auth failure", 401, `{"detail":"bad token"}`, 3,
			"[ocagent] upload: auth rejected (HTTP 401) for \"report.txt\": {\"detail\":\"bad token\"}\n"},
		{"403 is an auth failure", 403, `{"detail":"forbidden"}`, 3,
			"[ocagent] upload: auth rejected (HTTP 403) for \"report.txt\": {\"detail\":\"forbidden\"}\n"},
		{"400 is the server rejecting the file", 400, `{"detail":"over the size cap"}`, 4,
			"[ocagent] upload: server rejected \"report.txt\" (HTTP 400): {\"detail\":\"over the size cap\"}\n"},
		{"500 is anything else", 500, "upstream exploded", 5,
			"[ocagent] upload: unexpected HTTP 500 for \"report.txt\": upstream exploded\n"},
		{"200 with a body that is not a ref is anything else", 200, "not json", 5,
			"[ocagent] upload: 200 but unparseable ref body: not json\n"},
		{"200 with an empty id is anything else", 200, `{"id":"","mime":"text/plain"}`, 5,
			"[ocagent] upload: 200 but unparseable ref body: {\"id\":\"\",\"mime\":\"text/plain\"}\n"},
	}
	for _, tc := range statusCases {
		t.Run(tc.name, func(t *testing.T) {
			path := fileWith(t, "report.txt", "x")
			var out, errOut bytes.Buffer
			rc := cmdUpload(canned(tc.status, tc.body), configured, path, "", &out, &errOut)
			if rc != tc.wantRC || errOut.String() != tc.wantErr || out.String() != "" {
				t.Fatalf("got (%d, %q, %q), want (%d, \"\", %q)",
					rc, out.String(), errOut.String(), tc.wantRC, tc.wantErr)
			}
		})
	}
}
