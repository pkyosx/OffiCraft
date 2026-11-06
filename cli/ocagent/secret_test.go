package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

	t.Run("help explains cards, retention and the argv rule", func(t *testing.T) {
		var out, errOut bytes.Buffer
		cmdSecret([]string{"--help"}, secretCfg, nil, &out, &errOut)
		for _, want := range []string{"rc-", "retention", "Never put one on a command line"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("secret --help lacks %q:\n%s", want, out.String())
			}
		}
	})

	t.Run("show takes stored secrets only and says what to do with a card id", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdSecret([]string{"show", "rc-00000000000a"}, secretCfg, nil, &out, &errOut)
		want := "[ocagent] secret show: \"rc-00000000000a\" is a reply card, and show only reads stored secrets; " +
			"read the card with get_reply_card and retrieve its value with `ocagent secret run` or `write`\n"
		if rc != 2 || errOut.String() != want {
			t.Fatalf("got (%d, %q), want (2, %q)", rc, errOut.String(), want)
		}
	})

	t.Run("run and write refuse an id of neither shape", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if rc := cmdSecret([]string{"write", "rc-ABC", "--file", "x"}, secretCfg, nil, &out, &errOut); rc != 2 {
			t.Fatalf("write rc = %d, want 2 (%q)", rc, errOut.String())
		}
		if _, _, err := parseEnvBindings([]string{"A=rc-ABC"}); err == nil {
			t.Fatal("run accepted a malformed card id")
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

	t.Run("an id not shaped like a secret id is a usage error before anything is sent", func(t *testing.T) {
		for _, args := range [][]string{
			{"show", "sec-1"},
			{"write", "SEC-00000000000A", "--file", filepath.Join(t.TempDir(), "out")},
			{"run", "--env", "A=prod-token", "--", "true"},
		} {
			var out, errOut bytes.Buffer
			if rc := cmdSecret(args, secretCfg, nil, &out, &errOut); rc != 2 || !strings.Contains(errOut.String(), "is not a secret id") {
				t.Errorf("%q = (%d, %q), want (2, the id-shape refusal)", args, rc, errOut.String())
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
		_, ids, err := parseEnvBindings([]string{"A=sec-00000000000a", "B=sec-00000000000a", "C=sec-00000000000b"})
		if err != nil || !reflect.DeepEqual(ids, []string{"sec-00000000000a", "sec-00000000000b"}) {
			t.Fatalf("got (%v, %v), want ([sec-00000000000a sec-00000000000b], nil)", ids, err)
		}
	})

	for _, bad := range [][]string{nil, {"A"}, {"A="}, {"1A=sec-00000000000a"}, {"A-B=sec-00000000000a"}, {"A=sec-00000000000a", "A=sec-00000000000b"}} {
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
		rc := runSecretCommand(reply, secretCfg, envs, "", []string{"sh", "-c", script}, []string{"PATH=" + os.Getenv("PATH")}, nil, &out, &errOut)
		return rc, out.String(), errOut.String()
	}

	t.Run("one reveal for every id; the child sees the values, the output shows ***", func(t *testing.T) {
		client := canned(200, revealBody("sec-00000000000a", fakeValue1, "sec-00000000000b", fakeValue2))
		rc, out, errOut := run(t, client, []string{"A=sec-00000000000a", "B=sec-00000000000b", "C=sec-00000000000a"},
			`printf '%s|%s|%s\n' "$A" "$B" "$C"; printf 'err:%s\n' "$B" >&2; printf 'fake-val'`)
		if rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut)
		}
		if out != "***|***|***\nfake-val" || errOut != "err:***\n" {
			t.Fatalf("got (%q, %q), want both streams masked", out, errOut)
		}
		if len(client.sent) != 1 || client.sent[0].body != `{"ids":["sec-00000000000a","sec-00000000000b"]}` {
			t.Fatalf("sent %+v, want one reveal of both ids", client.sent)
		}
	})

	t.Run("the child's exit code is propagated", func(t *testing.T) {
		rc, _, _ := run(t, canned(200, revealBody("sec-00000000000a", fakeValue1)), []string{"A=sec-00000000000a"}, "exit 7")
		if rc != 7 {
			t.Fatalf("rc = %d, want 7", rc)
		}
	})

	t.Run("a child killed by a signal exits 128+signal", func(t *testing.T) {
		rc, _, _ := run(t, canned(200, revealBody("sec-00000000000a", fakeValue1)), []string{"A=sec-00000000000a"}, "kill -TERM $$")
		if rc != 128+15 {
			t.Fatalf("rc = %d, want 143", rc)
		}
	})

	t.Run("an unknown id exits 4 and the command never runs", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		rc, out, errOut := run(t, canned(404, `{"error":{"code":"not_found","message":"unknown secret ids: sec-00000000000f"}}`),
			[]string{"A=sec-00000000000f"}, "touch "+marker)
		if rc != 4 || out != "" || !strings.Contains(errOut, "unknown secret ids: sec-00000000000f") {
			t.Fatalf("got (%d, %q, %q), want (4, \"\", the server's message)", rc, out, errOut)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("the command ran although the reveal was refused")
		}
	})

	t.Run("a 200 missing an id refuses without quoting the body", func(t *testing.T) {
		rc, out, errOut := run(t, canned(200, revealBody("sec-00000000000a", fakeValue1)), []string{"A=sec-00000000000a", "B=sec-00000000000b"}, "true")
		if rc != 5 {
			t.Fatalf("rc = %d, want 5", rc)
		}
		assertNoValue(t, "secret run", out, errOut)
	})

	t.Run("a background process holding the output does not keep run waiting", func(t *testing.T) {
		defer func(d time.Duration) { secretRunWaitDelay = d }(secretRunWaitDelay)
		secretRunWaitDelay = 100 * time.Millisecond
		start := time.Now()
		rc, out, _ := run(t, canned(200, revealBody("sec-00000000000a", fakeValue1)), []string{"A=sec-00000000000a"},
			`sleep 5 & printf '%s' "$A"`)
		if rc != 0 || out != "***" {
			t.Fatalf("got (%d, %q), want (0, \"***\")", rc, out)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("run waited %v for the background process", elapsed)
		}
	})

	t.Run("a card id works like a secret id", func(t *testing.T) {
		client := canned(200, revealBody("rc-00000000000a", fakeValue1))
		rc, out, errOut := run(t, client, []string{"A=rc-00000000000a"}, `printf '%s\n' "$A"`)
		if rc != 0 || out != "***\n" {
			t.Fatalf("got (%d, %q, %q), want (0, masked value)", rc, out, errOut)
		}
		if len(client.sent) != 1 || !strings.Contains(string(client.sent[0].body), `"ids":["rc-00000000000a"]`) {
			t.Fatalf("reveal sent %+v", client.sent)
		}
	})

	t.Run("a command that cannot be found fails before any reveal, so no value is spent", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("rc-00000000000a", fakeValue1))
		rc := runSecretCommand(client, secretCfg, []string{"A=rc-00000000000a"}, "",
			[]string{"oc-no-such-command-anywhere"}, []string{"PATH=" + os.Getenv("PATH")}, nil, &out, &errOut)
		if rc != 1 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (1, nothing sent)", rc, client.sent)
		}
		if !strings.Contains(errOut.String(), "[ocagent] secret run: cannot start oc-no-such-command-anywhere") {
			t.Fatalf("stderr = %q", errOut.String())
		}
	})

	t.Run("a 409 exits 7 with the station's message and runs nothing", func(t *testing.T) {
		client := canned(409, `{"error":{"code":"conflict","message":"no retrievable value on secret card(s): rc-00000000000a"}}`)
		rc, out, errOut := run(t, client, []string{"A=rc-00000000000a"}, `echo ran`)
		if rc != 7 || out != "" {
			t.Fatalf("got (%d, %q), want (7, nothing run)", rc, out)
		}
		if want := "[ocagent] secret run: refused (HTTP 409): no retrievable value on secret card(s): rc-00000000000a\n"; errOut != want {
			t.Fatalf("stderr = %q, want %q", errOut, want)
		}
	})

	t.Run("no command is a usage error and reveals nothing", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("sec-00000000000a", fakeValue1))
		rc := runSecretCommand(client, secretCfg, []string{"A=sec-00000000000a"}, "", nil, nil, nil, &out, &errOut)
		if rc != 2 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (2, nothing sent)", rc, client.sent)
		}
	})
}

func TestRunSecretCommandStdinJSON(t *testing.T) {
	const raw = `dummy "q" \b
line2-aaaaaaaa`
	const encoded = `"dummy \"q\" \\b\nline2-aaaaaaaa"`
	reveal := func(id string) *cannedHTTP {
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{{"id": id, "value": raw}}})
		return canned(200, string(body))
	}
	run := func(client httpClient, envs []string, id string, script string) (int, string, string) {
		var out, errOut bytes.Buffer
		rc := runSecretCommand(client, secretCfg, envs, id, []string{"sh", "-c", script},
			[]string{"PATH=" + os.Getenv("PATH")}, nil, &out, &errOut)
		return rc, out.String(), errOut.String()
	}

	t.Run("the command reads the value on stdin as a JSON string", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "in.json")
		client := reveal("rc-00000000000a")
		rc, out, errOut := run(client, nil, "rc-00000000000a", "cat > "+file)
		if rc != 0 || out != "" || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
		if len(client.sent) != 1 || client.sent[0].body != `{"ids":["rc-00000000000a"]}` {
			t.Fatalf("reveal requests = %+v", client.sent)
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != encoded {
			t.Fatalf("stdin = %q, want %q", got, encoded)
		}
	})

	t.Run("a command echoing its stdin shows the mask", func(t *testing.T) {
		rc, out, errOut := run(reveal("rc-00000000000a"), nil, "rc-00000000000a", "cat")
		if rc != 0 || out != "***" || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
	})

	t.Run("a value needing HTML or non-ASCII escapes reaches stdin unescaped", func(t *testing.T) {
		const value = "a<b&c>-dummy\n密碼😀"
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{{"id": "rc-00000000000a", "value": value}}})
		file := filepath.Join(t.TempDir(), "in.json")
		rc, out, errOut := run(canned(200, string(body)), nil, "rc-00000000000a", "cat > "+file)
		if rc != 0 || out != "" || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if want := `"a<b&c>-dummy\n密碼😀"`; string(got) != want {
			t.Fatalf("stdin = %q, want %q", got, want)
		}
	})

	t.Run("a command re-encoding the value with its own JSON library shows the mask", func(t *testing.T) {
		const value = "a<b&c>-dummy\n密碼😀"
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{{"id": "rc-00000000000a", "value": value}}})
		for _, printed := range []struct{ name, text string }{
			{"as read from stdin", `"a<b&c>-dummy\n密碼😀"`},
			{"as read from stdin, unquoted", `a<b&c>-dummy\n密碼😀`},
			{"HTML-escaped like Go", `"a\u003cb\u0026c\u003e-dummy\n密碼😀"`},
			{"HTML-escaped like Go, unquoted", `a\u003cb\u0026c\u003e-dummy\n密碼😀`},
			{"ASCII-only like Python", `"a<b&c>-dummy\n\u5bc6\u78bc\ud83d\ude00"`},
			{"ASCII-only like Python, unquoted", `a<b&c>-dummy\n\u5bc6\u78bc\ud83d\ude00`},
		} {
			t.Run(printed.name, func(t *testing.T) {
				rc, out, errOut := run(canned(200, string(body)), nil, "rc-00000000000a", "printf '%s' '"+printed.text+"'")
				if rc != 0 || out != "***" || errOut != "" {
					t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
				}
			})
		}
	})

	t.Run("a command stripping a key before re-encoding it shows the mask", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{{"id": "rc-00000000000a", "value": "-----BEGIN dummy-----\naaaaaaaa\n"}}})
		rc, out, errOut := run(canned(200, string(body)), nil, "rc-00000000000a", `printf '%s' '"-----BEGIN dummy-----\naaaaaaaa"'`)
		if rc != 0 || out != "***" || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
	})

	t.Run("a value with surrounding whitespace reaches stdin as is, and its re-encoded form shows the mask", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{{"id": "rc-00000000000a", "value": " dummy-aaaaaaaa\n"}}})
		file := filepath.Join(t.TempDir(), "in.json")
		rc, out, errOut := run(canned(200, string(body)), nil, "rc-00000000000a", "cat > "+file)
		if rc != 0 || out != "" || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if want := `" dummy-aaaaaaaa\n"`; string(got) != want {
			t.Fatalf("stdin = %q, want %q", got, want)
		}
		rc, out, errOut = run(canned(200, string(body)), nil, "rc-00000000000a", `printf '%s' '" dummy-aaaaaaaa\n"'`)
		if rc != 0 || out != "***" || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
	})

	t.Run("a whitespace-only value leaves empty strings in the output alone", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{{"id": "rc-00000000000a", "value": "  "}}})
		rc, out, errOut := run(canned(200, string(body)), nil, "rc-00000000000a", `printf '%s' '{"a":"","b":"ok"}'`)
		if rc != 0 || out != `{"a":"","b":"ok"}` || errOut != "" {
			t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
		}
	})

	t.Run("a malformed id fetches nothing", func(t *testing.T) {
		client := reveal("rc-00000000000a")
		rc, out, errOut := run(client, nil, "not-an-id", "true")
		want := "[ocagent] secret run: --stdin-json \"not-an-id\" is not a secret id (sec- or rc- followed by 12 lowercase hex digits)\n"
		if rc != 2 || out != "" || errOut != want || len(client.sent) != 0 {
			t.Fatalf("rc=%d out=%q err=%q calls=%d", rc, out, errOut, len(client.sent))
		}
	})

	t.Run("neither flag is a usage error", func(t *testing.T) {
		client := reveal("rc-00000000000a")
		rc, _, errOut := run(client, nil, "", "true")
		want := "[ocagent] secret run: at least one --env VAR=<ID> or --stdin-json <ID> is required\n"
		if rc != 2 || errOut != want || len(client.sent) != 0 {
			t.Fatalf("rc=%d err=%q calls=%d", rc, errOut, len(client.sent))
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

	t.Run("a line of a multi-line value printed alone is masked, whatever its length", func(t *testing.T) {
		value := []string{"-----BEGIN KEY-----\r\naaaaaaaaaaaa\n\n   \nab=\n12\n密語\n"}
		for in, want := range map[string]string{
			"head: -----BEGIN KEY-----\n": "head: ***\n",
			"body aaaaaaaaaaaa end":       "body *** end",
			"tail ab= end":                "tail *** end",
			"code 12 end":                 "code *** end",
			"pass 密語 end":                 "pass *** end",
		} {
			if got := writeChunks(value, in); got != want {
				t.Fatalf("output for %q = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("lines of a multi-line value with no letter or digit leave the output alone", func(t *testing.T) {
		text := "a b\n\nc   d {\"ok\":true},\n-----\n"
		value := []string{"{\n  \"k\": \"aaaaaaaaaaaa\",\n\n   \n-----\n},\nbbbbbbbbbbbb\n}"}
		if got := writeChunks(value, text); got != text {
			t.Fatalf("got %q", got)
		}
		if got := writeChunks(value, `x "k": "aaaaaaaaaaaa", y`); got != "x *** y" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a two-line value masks each line, split across writes too", func(t *testing.T) {
		value := []string{"aaaaaaaaaaaa\nbbbbbbbbbbbb"}
		if got := writeChunks(value, "x aaaaaaaaaaaa y"); got != "x *** y" {
			t.Fatalf("got %q", got)
		}
		// The second line, because the first is also the head of the whole value.
		text := "x bbbbbbbbbbbb y"
		for i := 1; i < len(text); i++ {
			if got := writeChunks(value, text[:i], text[i:]); got != "x *** y" {
				t.Fatalf("split at %d: got %q", i, got)
			}
		}
	})

	t.Run("a single-line value is masked only whole", func(t *testing.T) {
		if got := writeChunks([]string{"aaaa bbbb"}, "aaaa and bbbb and aaaa bbbb"); got != "aaaa and bbbb and ***" {
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

	t.Run("values that overlap end to start are masked as one span", func(t *testing.T) {
		for _, tc := range []struct {
			values     []string
			text, want string
		}{
			{[]string{"12345678", "01"}, "x012345678y", "x***y"},
			{[]string{"abcdefgh", "zzab"}, "zzabcdefgh", "***"},
		} {
			if got := writeChunks(tc.values, tc.text); got != tc.want {
				t.Errorf("%v in one write: got %q, want %q", tc.values, got, tc.want)
			}
			if got := writeChunks(tc.values, strings.Split(tc.text, "")...); got != tc.want {
				t.Errorf("%v byte by byte: got %q, want %q", tc.values, got, tc.want)
			}
		}
	})

	t.Run("the trimmed and CRLF forms of a value are masked", func(t *testing.T) {
		for _, tc := range []struct {
			value      string
			text, want string
		}{
			{fakeValue1 + "\n", "x " + fakeValue1 + " y", "x *** y"},
			{"line1\nline2", "a line1\r\nline2 b", "a *** b"},
			{"line1\nline2", "a line1\nline2 b", "a *** b"},
		} {
			if got := writeChunks([]string{tc.value}, tc.text); got != tc.want {
				t.Errorf("value %q: got %q, want %q", tc.value, got, tc.want)
			}
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
		client := canned(200, revealBody("sec-00000000000a", fakeValue1))
		if rc := cmdSecretWrite(client, secretCfg, "sec-00000000000a", path, false, &out, &errOut); rc != 0 {
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

	t.Run("a multi-line value is written byte for byte", func(t *testing.T) {
		multi := "-----BEGIN FAKE KEY-----\nfake-line-1\r\nfake-line-2\n\n-----END FAKE KEY-----\n"
		body, _ := json.Marshal(map[string]any{"secrets": []map[string]string{
			{"id": "sec-00000000000a", "name": "n", "note": "", "value": multi},
		}})
		path := filepath.Join(t.TempDir(), "deploy.key")
		var out, errOut bytes.Buffer
		if rc := cmdSecretWrite(canned(200, string(body)), secretCfg, "sec-00000000000a", path, false, &out, &errOut); rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut.String())
		}
		assertNoValue(t, "secret write", out.String(), errOut.String())
		if got, _ := os.ReadFile(path); string(got) != multi {
			t.Fatalf("file holds %q, want %q", got, multi)
		}
	})

	t.Run("a card id writes like a secret id, and a 409 exits 7 and writes nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "deploy.key")
		var out, errOut bytes.Buffer
		if rc := cmdSecretWrite(canned(200, revealBody("rc-00000000000a", fakeValue1)), secretCfg, "rc-00000000000a", path, false, &out, &errOut); rc != 0 {
			t.Fatalf("rc = %d (stderr %q), want 0", rc, errOut.String())
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != fakeValue1 {
			t.Fatalf("file holds %q (%v)", got, err)
		}
		refused := filepath.Join(t.TempDir(), "deploy.key")
		out.Reset()
		errOut.Reset()
		rc := cmdSecretWrite(canned(409, `{"error":{"code":"conflict","message":"no retrievable value on secret card(s): rc-00000000000a"}}`),
			secretCfg, "rc-00000000000a", refused, false, &out, &errOut)
		if want := "[ocagent] secret write: refused (HTTP 409): no retrievable value on secret card(s): rc-00000000000a\n"; rc != 7 || errOut.String() != want {
			t.Fatalf("got (%d, %q), want (7, %q)", rc, errOut.String(), want)
		}
		if _, err := os.Stat(refused); !os.IsNotExist(err) {
			t.Fatalf("a refused write left a file: %v", err)
		}
	})

	t.Run("a bad path on a card id is refused before anything is fetched", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("rc-00000000000a", fakeValue1))
		rc := cmdSecretWrite(client, secretCfg, "rc-00000000000a", filepath.Join(t.TempDir(), "missing", "deploy.key"), false, &out, &errOut)
		if rc != 1 || len(client.sent) != 0 {
			t.Fatalf("got (%d, sent %v), want (1, nothing sent)", rc, client.sent)
		}
	})

	t.Run("a directory that cannot take the file is refused before anything is fetched", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
		path := filepath.Join(dir, "deploy.key")
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("rc-00000000000a", fakeValue1))
		rc := cmdSecretWrite(client, secretCfg, "rc-00000000000a", path, false, &out, &errOut)
		const wantPrefix = "[ocagent] secret write: cannot create a temporary file next to "
		if rc != 1 || len(client.sent) != 0 || !strings.HasPrefix(errOut.String(), wantPrefix+path+": ") {
			t.Fatalf("got (%d, sent %v, err %q), want (1, nothing sent, %q...)", rc, client.sent, errOut.String(), wantPrefix)
		}
	})

	t.Run("an existing file is refused before anything is fetched", func(t *testing.T) {
		path := fileWith(t, "deploy.key", "old")
		var out, errOut bytes.Buffer
		client := canned(200, revealBody("sec-00000000000a", fakeValue1))
		rc := cmdSecretWrite(client, secretCfg, "sec-00000000000a", path, false, &out, &errOut)
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
		if rc := cmdSecretWrite(canned(200, revealBody("sec-00000000000a", fakeValue1)), secretCfg, "sec-00000000000a", path, true, &out, &errOut); rc != 0 {
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
		client := canned(200, revealBody("sec-00000000000a", fakeValue1))
		rc := cmdSecretWrite(client, secretCfg, "sec-00000000000a", filepath.Join(dir, "deploy.key"), false, &out, &errOut)
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
		rc := cmdSecretWrite(canned(404, `{"error":{"code":"not_found","message":"unknown secret ids: sec-00000000000f"}}`), secretCfg, "sec-00000000000f", path, false, &out, &errOut)
		if rc != 4 || out.String() != "" {
			t.Fatalf("got (%d, %q), want (4, \"\")", rc, out.String())
		}
		if _, err := os.Stat(path); err == nil {
			t.Fatal("a file was written for an unknown id")
		}
	})
}
