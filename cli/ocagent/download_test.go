package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dispositionReply builds a 200 blob reply carrying the headers the attachment
// route sends.
func dispositionReply(body, disposition, contentType string) *cannedHTTP {
	header := http.Header{}
	if disposition != "" {
		header.Set("Content-Disposition", disposition)
	}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &cannedHTTP{replies: []cannedReply{{status: 200, body: body, header: header}}}
}

func TestNewStreamingClient(t *testing.T) {
	t.Skip("newStreamingClient returns a configured *http.Client literal; every assertion " +
		"available on it restates a field of that literal, so no behavioural test is written")
}

func TestCmdDownload(t *testing.T) {
	configured := Config{Base: "https://station.example.com", BaseConfigured: true, Token: "tok-1"}

	t.Run("a served blob lands under the name the server gave it", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		client := dispositionReply("the bytes", `attachment; filename="report.txt"`, "text/plain")
		rc := cmdDownload(client, configured, "att-0123456789ab", dir, &out, &errOut)

		dest := filepath.Join(dir, "report.txt")
		if rc != 0 || out.String() != dest+"\n" {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), dest+"\n")
		}
		if errOut.String() != "[ocagent] download: report.txt (9 bytes, text/plain)\n" {
			t.Fatalf("stderr %q, want the landed-file line", errOut.String())
		}
		landed, err := os.ReadFile(dest)
		if err != nil || string(landed) != "the bytes" {
			t.Fatalf("file %q err %v, want %q", string(landed), err, "the bytes")
		}
		want := sentRequest{
			method: "GET",
			url:    "https://station.example.com/api/chat/attachment/att-0123456789ab",
			ua:     "ocagent/0.1",
			accept: "*/*",
			auth:   "Bearer tok-1",
		}
		if len(client.sent) != 1 || client.sent[0] != want {
			t.Fatalf("sent %+v, want %+v", client.sent, want)
		}
	})

	t.Run("an image served with no disposition lands under its attachment id", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		rc := cmdDownload(dispositionReply("PNGDATA", "", "image/png"), configured,
			"att-0123456789ab", dir, &out, &errOut)
		dest := filepath.Join(dir, "att-0123456789ab")
		if rc != 0 || out.String() != dest+"\n" {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), dest+"\n")
		}
		if _, err := os.Stat(dest); err != nil {
			t.Fatalf("stat %s: %v", dest, err)
		}
	})

	t.Run("a traversing filename can never leave the target directory", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		rc := cmdDownload(dispositionReply("x", `attachment; filename="../../etc/passwd"`, "text/plain"),
			configured, "att-0123456789ab", dir, &out, &errOut)
		dest := filepath.Join(dir, "passwd")
		if rc != 0 || out.String() != dest+"\n" {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), dest+"\n")
		}
		if _, err := os.Stat(dest); err != nil {
			t.Fatalf("stat %s: %v", dest, err)
		}
	})

	t.Run("no --out lands the blob under the agent workdir's tmp/attachments", func(t *testing.T) {
		t.Chdir(t.TempDir())
		var out, errOut bytes.Buffer
		rc := cmdDownload(dispositionReply("x", `attachment; filename="report.txt"`, "text/plain"),
			configured, "att-0123456789ab", "", &out, &errOut)
		landed := strings.TrimSuffix(out.String(), "\n")
		if rc != 0 || !filepath.IsAbs(landed) ||
			!strings.HasSuffix(landed, filepath.Join("tmp", "attachments", "report.txt")) {
			t.Fatalf("got (%d, %q), want (0, an absolute tmp/attachments path)", rc, landed)
		}
		if _, err := os.Stat(landed); err != nil {
			t.Fatalf("stat %s: %v", landed, err)
		}
	})

	t.Run("an attachment id with path characters is escaped into the URL", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		client := dispositionReply("x", "", "application/octet-stream")
		cmdDownload(client, configured, "att/../secret", dir, &out, &errOut)
		want := "https://station.example.com/api/chat/attachment/att%2F..%2Fsecret"
		if client.sent[0].url != want {
			t.Fatalf("URL %q, want %q", client.sent[0].url, want)
		}
	})

	t.Run("no token refuses before any request and writes no file", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		client := dispositionReply("x", "", "")
		noToken := Config{Base: "https://station.example.com", BaseConfigured: true}
		rc := cmdDownload(client, noToken, "att-0123456789ab", dir, &out, &errOut)
		want := "[ocagent] download: no OC_TOKEN configured — cannot make an authed fetch.\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", the no-token refusal)",
				rc, out.String(), errOut.String())
		}
		assertEmptyDir(t, dir)
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("an unset OC_BASE refuses before any request and writes no file", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		client := dispositionReply("x", "", "")
		unset := Config{Base: defaultBase, Token: "tok-1"}
		rc := cmdDownload(client, unset, "att-0123456789ab", dir, &out, &errOut)
		want := "[ocagent] download: no OC_BASE configured — nothing here knows which station " +
			"to talk to, and the built-in default is this machine's loopback address.\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", the OC_BASE refusal)",
				rc, out.String(), errOut.String())
		}
		assertEmptyDir(t, dir)
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("a transport failure is exit 1 and writes no file", func(t *testing.T) {
		dir := t.TempDir()
		var out, errOut bytes.Buffer
		rc := cmdDownload(failingHTTP("connection refused"), configured, "att-0123456789ab", dir,
			&out, &errOut)
		want := "[ocagent] download: request failed (network): connection refused\n"
		if rc != 1 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (1, \"\", the network reason)",
				rc, out.String(), errOut.String())
		}
		assertEmptyDir(t, dir)
	})

	statusCases := []struct {
		name    string
		status  int
		body    string
		wantRC  int
		wantErr string
	}{
		{"401 is an auth failure", 401, `{"detail":"bad token"}`, 3,
			"[ocagent] download: auth rejected (HTTP 401) for \"att-0123456789ab\": {\"detail\":\"bad token\"}\n"},
		{"403 is an auth failure", 403, `{"detail":"not yours"}`, 3,
			"[ocagent] download: auth rejected (HTTP 403) for \"att-0123456789ab\": {\"detail\":\"not yours\"}\n"},
		{"404 is its own exit code so a script can branch on it", 404, `{"detail":"no such blob"}`, 4,
			"[ocagent] download: attachment \"att-0123456789ab\" not found (HTTP 404): {\"detail\":\"no such blob\"}\n"},
		{"500 is anything else", 500, "upstream exploded", 5,
			"[ocagent] download: unexpected HTTP 500 for \"att-0123456789ab\": upstream exploded\n"},
	}
	for _, tc := range statusCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var out, errOut bytes.Buffer
			rc := cmdDownload(canned(tc.status, tc.body), configured, "att-0123456789ab", dir,
				&out, &errOut)
			if rc != tc.wantRC || errOut.String() != tc.wantErr || out.String() != "" {
				t.Fatalf("got (%d, %q, %q), want (%d, \"\", %q)",
					rc, out.String(), errOut.String(), tc.wantRC, tc.wantErr)
			}
			assertEmptyDir(t, dir)
		})
	}

	t.Run("an undirectory-able --out is exit 1 and writes no file", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "blocked")
		if err := os.WriteFile(blocker, []byte("i am a file"), 0o644); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		rc := cmdDownload(dispositionReply("x", `attachment; filename="report.txt"`, "text/plain"),
			configured, "att-0123456789ab", filepath.Join(blocker, "sub"), &out, &errOut)
		if rc != 1 || out.String() != "" ||
			!strings.HasPrefix(errOut.String(), "[ocagent] download: cannot create ") {
			t.Fatalf("got (%d, %q, %q), want (1, \"\", a cannot-create line)",
				rc, out.String(), errOut.String())
		}
	})
}

// assertEmptyDir fails unless dir holds nothing.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%s holds %v, want nothing", dir, names)
	}
}

func TestFilenameFromDisposition(t *testing.T) {
	cases := []struct {
		name string
		disp string
		want string
	}{
		{"an absent header names nothing", "", ""},
		{"a plain ASCII filename is read", `attachment; filename="report.txt"`, "report.txt"},
		{"the RFC 5987 parameter carries a non-ASCII name",
			`attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt`, "报告.txt"},
		{"the RFC 5987 parameter wins over the ASCII fallback",
			`attachment; filename="report.txt"; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt`, "报告.txt"},
		{"a parameter after the RFC 5987 value is not part of the name",
			`attachment; filename*=UTF-8''a%20b.txt; foo=1`, "a b.txt"},
		{"an undecodable RFC 5987 value falls back to the ASCII one",
			`attachment; filename="report.txt"; filename*=UTF-8''%ZZ`, "report.txt"},
		{"an empty RFC 5987 value falls back to the ASCII one",
			`attachment; filename="report.txt"; filename*=UTF-8''`, "report.txt"},
		{"an unterminated ASCII filename names nothing", `attachment; filename="report.txt`, ""},
		{"a header with no filename at all names nothing", "attachment", ""},
		{"inline with no filename names nothing", "inline", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := filenameFromDisposition(tc.disp); got != tc.want {
				t.Fatalf("filenameFromDisposition(%q) = %q, want %q", tc.disp, got, tc.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"an ordinary name survives", "report.txt", "report.txt"},
		{"surrounding whitespace is trimmed", "  report.txt  ", "report.txt"},
		{"a posix traversal is reduced to its basename", "../../etc/passwd", "passwd"},
		{"a windows traversal is reduced to its basename", `..\..\windows\evil.exe`, "evil.exe"},
		{"an absolute path is reduced to its basename", "/etc/passwd", "passwd"},
		{"a trailing separator is dropped", "/etc/", "etc"},
		{"a trailing backslash leaves nothing usable", `dir\`, "att-0123456789ab"},
		{"an empty name degrades to the fallback", "", "att-0123456789ab"},
		{"a whitespace-only name degrades to the fallback", "   ", "att-0123456789ab"},
		{"a bare dot degrades to the fallback", ".", "att-0123456789ab"},
		{"a bare dot-dot degrades to the fallback", "..", "att-0123456789ab"},
		{"a bare separator degrades to the fallback", "/", "att-0123456789ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeFilename(tc.in, "att-0123456789ab"); got != tc.want {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
