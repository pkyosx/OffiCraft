package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	fakeValue1 = "fake-value-1"
	fakeValue2 = "fake-value-22"
)

var secretCfg = Config{Base: "https://station.example.com", BaseConfigured: true, Token: "tok-1"}

const secretListBody = `{"secrets":[` +
	`{"id":"sec-000000000001","name":"GitHub","note":"read-only token\nrotate monthly","value":"` + fakeValue1 + `"},` +
	`{"id":"sec-000000000002","name":"npm","note":"","value":"` + fakeValue2 + `"}]}`

func revealBody(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, `{"id":"`+pairs[i]+`","name":"n","note":"","value":"`+pairs[i+1]+`"}`)
	}
	return `{"secrets":[` + strings.Join(parts, ",") + `]}`
}

func assertNoValue(t *testing.T, where string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, fakeValue1) || strings.Contains(s, fakeValue2) {
			t.Fatalf("%s leaked a secret value: %q", where, s)
		}
	}
}

func TestCmdSecret(t *testing.T) {
	t.Run("help names every subcommand and the never-prints rule", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdSecret([]string{"--help"}, secretCfg, nil, &out, &errOut); rc != 0 {
			t.Fatalf("--help returned %d, want 0", rc)
		}
		for _, want := range []string{"list", "show <ID>", "run --env VAR=<ID>", "write <ID> --file <path>", "NO COMMAND EVER PRINTS A VALUE"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("secret --help lacks %q:\n%s", want, out.String())
			}
		}
	})

	t.Run("each subcommand's --help prints its own usage", func(t *testing.T) {
		for _, sub := range []string{"list", "show", "run", "write"} {
			var out, errOut bytes.Buffer
			rc := cmdSecret([]string{sub, "--help"}, secretCfg, nil, &out, &errOut)
			if rc != 2 || !strings.HasPrefix(out.String(), "usage: ocagent secret "+sub) {
				t.Errorf("%s --help = (%d, %q), want (2, its usage)", sub, rc, out.String())
			}
		}
	})

	t.Run("an unknown subcommand is a usage error", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdSecret([]string{"dump"}, secretCfg, nil, &out, &errOut); rc != 2 {
			t.Fatalf("rc = %d, want 2", rc)
		}
	})

	t.Run("the top-level usage lists secret", func(t *testing.T) {
		var out bytes.Buffer
		usage(&out)
		if !strings.Contains(out.String(), "  secret ") {
			t.Fatalf("top-level usage does not list secret:\n%s", out.String())
		}
	})
}

func TestCmdSecretList(t *testing.T) {
	t.Run("prints id, name and indented note, oldest first, never a value", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, secretListBody)
		if rc := cmdSecretList(client, secretCfg, &out, &errOut); rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut.String())
		}
		want := "sec-000000000001  GitHub\n    read-only token\n    rotate monthly\n\nsec-000000000002  npm\n"
		if out.String() != want {
			t.Fatalf("stdout = %q, want %q", out.String(), want)
		}
		if len(client.sent) != 1 || client.sent[0].method != "GET" ||
			client.sent[0].url != "https://station.example.com/api/self/secrets" || client.sent[0].auth != "Bearer tok-1" {
			t.Fatalf("sent %+v, want one authed GET /api/self/secrets", client.sent)
		}
	})

	t.Run("a 403 exits 3 quoting only the envelope message", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdSecretList(canned(403, `{"error":{"code":"forbidden","message":"principal not permitted"},"x":"y"}`), secretCfg, &out, &errOut)
		want := "[ocagent] secret list: auth rejected (HTTP 403): principal not permitted\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", %q)", rc, out.String(), errOut.String(), want)
		}
	})

	t.Run("no token refuses before anything is sent", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, secretListBody)
		rc := cmdSecretList(client, Config{Base: secretCfg.Base, BaseConfigured: true}, &out, &errOut)
		if rc != 3 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (3, nothing sent)", rc, client.sent)
		}
	})

	t.Run("an unset OC_BASE refuses before anything is sent", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, secretListBody)
		rc := cmdSecretList(client, Config{Base: defaultBase, Token: "tok-1"}, &out, &errOut)
		if rc != 3 || len(client.sent) != 0 || !strings.Contains(errOut.String(), "no OC_BASE configured") {
			t.Fatalf("got (%d, %q, sent %v), want (3, the OC_BASE refusal, nothing sent)", rc, errOut.String(), client.sent)
		}
	})
}

func TestCmdSecretShow(t *testing.T) {
	t.Run("prints one secret's id, name and note, never a value", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdSecretShow(canned(200, secretListBody), secretCfg, "sec-000000000002", &out, &errOut); rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if want := "id:    sec-000000000002\nname:  npm\nnote:  (none)\n"; out.String() != want {
			t.Fatalf("stdout = %q, want %q", out.String(), want)
		}
	})

	t.Run("an unknown id exits 4", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdSecretShow(canned(200, secretListBody), secretCfg, "sec-ffffffffffff", &out, &errOut)
		if rc != 4 || out.String() != "" || !strings.Contains(errOut.String(), "sec-ffffffffffff") {
			t.Fatalf("got (%d, %q, %q), want (4, \"\", naming the id)", rc, out.String(), errOut.String())
		}
	})
}

func TestParseEnvBindings(t *testing.T) {
	t.Run("one id feeding two variables is revealed once", func(t *testing.T) {
		_, ids, err := parseEnvBindings([]string{"A=sec-1", "B=sec-1", "C=sec-2"})
		if err != nil || !reflect.DeepEqual(ids, []string{"sec-1", "sec-2"}) {
			t.Fatalf("got (%v, %v), want ([sec-1 sec-2], nil)", ids, err)
		}
	})

	for _, bad := range [][]string{nil, {"A"}, {"A="}, {"1A=sec-1"}, {"A-B=sec-1"}, {"A=sec-1", "A=sec-2"}} {
		if _, _, err := parseEnvBindings(bad); err == nil {
			t.Errorf("parseEnvBindings(%q) accepted a malformed binding", bad)
		}
	}

	t.Run("more than 16 distinct ids is refused", func(t *testing.T) {
		var envs []string
		for i := 0; i < 17; i++ {
			envs = append(envs, "V"+strings.Repeat("X", i)+"=sec-"+strings.Repeat("a", i+1))
		}
		if _, _, err := parseEnvBindings(envs); err == nil {
			t.Fatal("17 distinct ids were accepted")
		}
	})
}

func TestCmdSecretRun(t *testing.T) {
	run := func(t *testing.T, reply *cannedHTTP, envs []string, script string) (int, string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		rc := cmdSecretRun(reply, secretCfg, envs, []string{"sh", "-c", script}, []string{"PATH=" + os.Getenv("PATH")}, nil, &out, &errOut)
		return rc, out.String(), errOut.String()
	}

	t.Run("one reveal for every id; the child sees the values, the output shows ***", func(t *testing.T) {
		client := canned(200, revealBody("sec-1", fakeValue1, "sec-2", fakeValue2))
		rc, out, errOut := run(t, client, []string{"A=sec-1", "B=sec-2", "C=sec-1"},
			`printf '%s|%s|%s\n' "$A" "$B" "$C"; printf 'err:%s\n' "$B" >&2; printf 'fake-val'`)
		if rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut)
		}
		if out != "***|***|***\nfake-val" || errOut != "err:***\n" {
			t.Fatalf("got (%q, %q), want both streams masked", out, errOut)
		}
		if len(client.sent) != 1 || client.sent[0].body != `{"ids":["sec-1","sec-2"]}` {
			t.Fatalf("sent %+v, want one reveal of both ids", client.sent)
		}
	})

	t.Run("the child's exit code is propagated", func(t *testing.T) {
		rc, _, _ := run(t, canned(200, revealBody("sec-1", fakeValue1)), []string{"A=sec-1"}, "exit 7")
		if rc != 7 {
			t.Fatalf("rc = %d, want 7", rc)
		}
	})

	t.Run("a child killed by a signal exits 128+signal", func(t *testing.T) {
		rc, _, _ := run(t, canned(200, revealBody("sec-1", fakeValue1)), []string{"A=sec-1"}, "kill -TERM $$")
		if rc != 128+15 {
			t.Fatalf("rc = %d, want 143", rc)
		}
	})

	t.Run("an unknown id exits 4 and the command never runs", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		rc, out, errOut := run(t, canned(404, `{"error":{"code":"not_found","message":"unknown secret ids: sec-9"}}`),
			[]string{"A=sec-9"}, "touch "+marker)
		if rc != 4 || out != "" || !strings.Contains(errOut, "unknown secret ids: sec-9") {
			t.Fatalf("got (%d, %q, %q), want (4, \"\", the server's message)", rc, out, errOut)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("the command ran although the reveal was refused")
		}
	})

	t.Run("a 200 missing an id refuses without quoting the body", func(t *testing.T) {
		rc, out, errOut := run(t, canned(200, revealBody("sec-1", fakeValue1)), []string{"A=sec-1", "B=sec-2"}, "true")
		if rc != 5 {
			t.Fatalf("rc = %d, want 5", rc)
		}
		assertNoValue(t, "secret run", out, errOut)
	})

	t.Run("no command is a usage error and reveals nothing", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("sec-1", fakeValue1))
		rc := cmdSecretRun(client, secretCfg, []string{"A=sec-1"}, nil, nil, nil, &out, &errOut)
		if rc != 2 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (2, nothing sent)", rc, client.sent)
		}
	})
}

func TestMaskWriter(t *testing.T) {
	writeChunks := func(values []string, chunks ...string) string {
		var dst bytes.Buffer
		m := newMaskWriter(&dst, values)
		for _, c := range chunks {
			if _, err := m.Write([]byte(c)); err != nil {
				t.Fatal(err)
			}
		}
		m.Flush()
		return dst.String()
	}

	t.Run("a value split at every offset is still masked", func(t *testing.T) {
		text := "before " + fakeValue1 + " after"
		for cut := 0; cut <= len(text); cut++ {
			if got := writeChunks([]string{fakeValue1}, text[:cut], text[cut:]); got != "before *** after" {
				t.Fatalf("cut at %d: %q", cut, got)
			}
		}
	})

	t.Run("a value written one byte at a time is masked", func(t *testing.T) {
		text := fakeValue1 + fakeValue2 + "x" + fakeValue1
		var chunks []string
		for i := range text {
			chunks = append(chunks, text[i:i+1])
		}
		if got := writeChunks([]string{fakeValue1, fakeValue2}, chunks...); got != "******x***" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a value containing another is masked whole", func(t *testing.T) {
		if got := writeChunks([]string{"value-1", fakeValue1}, "a "+fakeValue1+" b value-1"); got != "a *** b ***" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a short value that is the head of a longer one waits for the next write", func(t *testing.T) {
		if got := writeChunks([]string{"fake-v1", "fake-v12"}, "x fake-v1", "2 y fake-v1"); got != "x *** y ***" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a held prefix that never completes is released at end of stream", func(t *testing.T) {
		if got := writeChunks([]string{fakeValue1}, "tail fake-val"); got != "tail fake-val" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("output with no value passes through unchanged", func(t *testing.T) {
		if got := writeChunks([]string{fakeValue1}, "plain ", "text\n"); got != "plain text\n" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestCmdSecretWrite(t *testing.T) {
	t.Run("writes the value 0600 and prints only the path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "deploy.key")
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("sec-1", fakeValue1))
		if rc := cmdSecretWrite(client, secretCfg, "sec-1", path, false, &out, &errOut); rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut.String())
		}
		if out.String() != "已寫入 "+path+"\n" {
			t.Fatalf("stdout = %q", out.String())
		}
		assertNoValue(t, "secret write", out.String(), errOut.String())
		got, err := os.ReadFile(path)
		if err != nil || string(got) != fakeValue1 {
			t.Fatalf("file holds %q (%v)", got, err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600", info.Mode().Perm())
		}
		entries, _ := os.ReadDir(filepath.Dir(path))
		if len(entries) != 1 {
			t.Fatalf("directory holds %d entries, want only the file (no temp left)", len(entries))
		}
	})

	t.Run("an existing file is refused before anything is fetched", func(t *testing.T) {
		path := fileWith(t, "deploy.key", "old")
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("sec-1", fakeValue1))
		rc := cmdSecretWrite(client, secretCfg, "sec-1", path, false, &out, &errOut)
		if rc != 6 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (6, nothing sent)", rc, client.sent)
		}
		if got, _ := os.ReadFile(path); string(got) != "old" {
			t.Fatalf("existing file changed to %q", got)
		}
	})

	t.Run("--force replaces an existing file and leaves it 0600", func(t *testing.T) {
		path := fileWith(t, "deploy.key", "old")
		var out, errOut bytes.Buffer
		if rc := cmdSecretWrite(canned(200, revealBody("sec-1", fakeValue1)), secretCfg, "sec-1", path, true, &out, &errOut); rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut.String())
		}
		got, _ := os.ReadFile(path)
		info, _ := os.Stat(path)
		if string(got) != fakeValue1 || info.Mode().Perm() != 0o600 {
			t.Fatalf("file = (%q, %o), want (the value, 600)", got, info.Mode().Perm())
		}
	})

	t.Run("a missing parent directory fails and is not created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "absent")
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("sec-1", fakeValue1))
		rc := cmdSecretWrite(client, secretCfg, "sec-1", filepath.Join(dir, "deploy.key"), false, &out, &errOut)
		if rc != 1 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (1, nothing sent)", rc, client.sent)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Fatal("the missing directory was created")
		}
	})

	t.Run("an unknown id exits 4 and writes nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "deploy.key")
		var out, errOut bytes.Buffer
		rc := cmdSecretWrite(canned(404, `{"error":{"code":"not_found","message":"unknown secret ids: sec-9"}}`), secretCfg, "sec-9", path, false, &out, &errOut)
		if rc != 4 || out.String() != "" {
			t.Fatalf("got (%d, %q), want (4, \"\")", rc, out.String())
		}
		if _, err := os.Stat(path); err == nil {
			t.Fatal("a file was written for an unknown id")
		}
	})
}
