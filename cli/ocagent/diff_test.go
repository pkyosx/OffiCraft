package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantPathSideRefusal = "the before side \"./before.md\" is a file path, and diff does not upload files.\n" +
	"  Put the file in the store first, then pass the id it prints:\n" +
	"      ocagent upload ./before.md          → prints an id like att-0123456789ab\n" +
	"      ocagent diff <before id> <after id>\n" +
	"  If what you want to compare is already in the system (a task artifact, an\n" +
	"  attachment someone sent you), you already have its id — no upload needed.\n" +
	"  A side is a stored attachment id (att-…) or doc:<kind>/<key>/<at>/<field>."

func TestSideRefusal(t *testing.T) {
	cases := []struct {
		name  string
		arg   string
		which string
		want  string
	}{
		{"a stored attachment id is a side", "att-0123456789ab", "before", ""},
		{"a current document field is a side", "doc:task/T-1/current/body", "before", ""},
		{"a seed document field is a side", "doc:task/T-1/seed/body", "after", ""},
		{"a version id is a side", "doc:task/T-1/42/body", "after", ""},
		{"a punctuation-rich but legal segment is a side", "doc:task/T-1.a:b@c+d/current/body", "before", ""},

		{"an uppercase attachment id is not one", "att-0123456789AB", "before",
			"the before side \"att-0123456789AB\" is neither a stored attachment id " +
				"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address " +
				"(doc:<kind>/<key>/<at>/<field>)."},
		{"a short attachment id is not one", "att-0123456789a", "after",
			"the after side \"att-0123456789a\" is neither a stored attachment id " +
				"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address " +
				"(doc:<kind>/<key>/<at>/<field>)."},
		{"a padded attachment id is judged as given, never trimmed", " att-0123456789ab", "before",
			"the before side \" att-0123456789ab\" is neither a stored attachment id " +
				"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address " +
				"(doc:<kind>/<key>/<at>/<field>)."},
		{"an empty argument is not a side", "", "after",
			"the after side \"\" is neither a stored attachment id " +
				"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address " +
				"(doc:<kind>/<key>/<at>/<field>)."},

		{"a file path gets the sentence that teaches the upload flow", "./before.md", "before",
			wantPathSideRefusal},

		{"three segments is not a document address", "doc:task/T-1/current",
			"before", "\"doc:task/T-1/current\" is not a document address — it is " +
				"doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a version id."},
		{"five segments is not a document address", "doc:task/T-1/current/body/extra",
			"after", "\"doc:task/T-1/current/body/extra\" is not a document address — it is " +
				"doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a version id."},
		{"an empty kind is named", "doc:/T-1/current/body", "before",
			"\"doc:/T-1/current/body\" leaves its kind empty."},
		{"an empty key is named", "doc:task//current/body", "before",
			"\"doc:task//current/body\" leaves its key empty."},
		{"an empty field is named", "doc:task/T-1/current/", "after",
			"\"doc:task/T-1/current/\" leaves its field empty."},
		{"a traversal key is refused as a segment", "doc:task/../current/body", "before",
			"\"doc:task/../current/body\" has a key that is not a usable address segment: \"..\""},
		{"a dot key is refused as a segment", "doc:task/./current/body", "before",
			"\"doc:task/./current/body\" has a key that is not a usable address segment: \".\""},
		{"a spaced field is refused as a segment", "doc:task/T-1/current/body text", "after",
			"\"doc:task/T-1/current/body text\" has a field that is not a usable address segment: \"body text\""},
		{"an unknown <at> is named with what it must be", "doc:task/T-1/latest/body", "before",
			"\"doc:task/T-1/latest/body\" has an <at> of \"latest\" — it must be current, seed, " +
				"or a version id from list_document_history."},
		{"an empty <at> is named", "doc:task/T-1//body", "after",
			"\"doc:task/T-1//body\" has an <at> of \"\" — it must be current, seed, " +
				"or a version id from list_document_history."},
		{"a zero-leading version id is not a version id", "doc:task/T-1/042/body", "before",
			"\"doc:task/T-1/042/body\" has an <at> of \"042\" — it must be current, seed, " +
				"or a version id from list_document_history."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sideRefusal(tc.arg, tc.which); got != tc.want {
				t.Fatalf("sideRefusal(%q, %q) =\n%q\nwant\n%q", tc.arg, tc.which, got, tc.want)
			}
		})
	}
}

func TestLooksLikeAPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "leftovers"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cases := []struct {
		name string
		arg  string
		want bool
	}{
		{"a forward slash makes it a path", "tmp/before.md", true},
		{"a backslash makes it a path", `tmp\before.md`, true},
		{"a leading tilde makes it a path", "~/before.md", true},
		{"a dot-extension makes it a path", "before.md", true},
		{"a bare name that really is on disk makes it a path", "leftovers", true},
		{"a bare name that is on nothing is not a path", "notthere", false},
		{"an empty argument is not a path", "", false},
		{"an attachment id is not a path", "att-0123456789ab", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeAPath(tc.arg); got != tc.want {
				t.Fatalf("looksLikeAPath(%q) = %v, want %v", tc.arg, got, tc.want)
			}
		})
	}
}

func TestDiffQuery(t *testing.T) {
	cases := []struct {
		name                                   string
		before, after, labelBefore, labelAfter string
		want                                   string
	}{
		{"no labels sends only the two sides", "att-0123456789ab", "att-ba9876543210", "", "",
			"after=att-ba9876543210&before=att-0123456789ab"},
		{"both labels ride along", "att-0123456789ab", "att-ba9876543210", "Old", "New",
			"after=att-ba9876543210&before=att-0123456789ab&label_after=New&label_before=Old"},
		{"an empty label is left out rather than sent blank", "att-0123456789ab", "att-ba9876543210",
			"Old", "", "after=att-ba9876543210&before=att-0123456789ab&label_before=Old"},
		{"a document address is percent-encoded", "doc:task/T-1/seed/body", "doc:task/T-1/current/body",
			"", "", "after=doc%3Atask%2FT-1%2Fcurrent%2Fbody&before=doc%3Atask%2FT-1%2Fseed%2Fbody"},
		{"a label with a space and an ampersand is escaped", "att-0123456789ab", "att-ba9876543210",
			"a b&c", "", "after=att-ba9876543210&before=att-0123456789ab&label_before=a+b%26c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diffQuery(tc.before, tc.after, tc.labelBefore, tc.labelAfter)
			if got != tc.want {
				t.Fatalf("diffQuery = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCmdDiff(t *testing.T) {
	configured := Config{Base: "https://station.example.com", BaseConfigured: true, Token: "tok-1"}

	t.Run("the plain link is printed without asking the station anything", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, "{}")
		rc := cmdDiff(client, configured, "att-0123456789ab", "att-ba9876543210", "Old", "New", false,
			&out, &errOut)
		want := "https://station.example.com/diff?after=att-ba9876543210&before=att-0123456789ab" +
			"&label_after=New&label_before=Old\n"
		if rc != 0 || out.String() != want || errOut.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (0, %q, \"\")", rc, out.String(), errOut.String(), want)
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want no request at all", client.sent)
		}
	})

	t.Run("labels are trimmed and a whitespace-only label is left out", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdDiff(canned(200, "{}"), configured,
			"doc:task/T-1/seed/body", "doc:task/T-1/current/body", "  Seed  ", "   ", false, &out, &errOut)
		want := "https://station.example.com/diff?after=doc%3Atask%2FT-1%2Fcurrent%2Fbody" +
			"&before=doc%3Atask%2FT-1%2Fseed%2Fbody&label_before=Seed\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
	})

	t.Run("a bad before side refuses before anything is printed", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, "{}")
		rc := cmdDiff(client, configured, "./before.md", "att-ba9876543210", "", "", false, &out, &errOut)
		want := "[ocagent] diff: " + wantPathSideRefusal + "\n"
		if rc != 2 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (2, \"\", the path refusal)", rc, out.String(), errOut.String())
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want no request", client.sent)
		}
	})

	t.Run("a bad after side is judged too, and named as the after side", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdDiff(canned(200, "{}"), configured, "att-0123456789ab", "nonsense", "", "", true,
			&out, &errOut)
		want := "[ocagent] diff: the after side \"nonsense\" is neither a stored attachment id " +
			"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address " +
			"(doc:<kind>/<key>/<at>/<field>).\n"
		if rc != 2 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (2, \"\", the after refusal)", rc, out.String(), errOut.String())
		}
	})

	t.Run("two bad sides cost one message, the before one", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := cmdDiff(canned(200, "{}"), configured, "nonsense-a", "nonsense-b", "", "", false, &out, &errOut)
		want := "[ocagent] diff: the before side \"nonsense-a\" is neither a stored attachment id " +
			"(att- plus 12 hex digits, what `ocagent upload` prints) nor a document address " +
			"(doc:<kind>/<key>/<at>/<field>).\n"
		if rc != 2 || errOut.String() != want {
			t.Fatalf("got (%d, %q), want (2, only the before refusal)", rc, errOut.String())
		}
	})

	t.Run("an unset OC_BASE refuses the plain flavour, which would otherwise print a loopback link", func(t *testing.T) {
		var out, errOut bytes.Buffer
		unset := Config{Base: defaultBase, Token: "tok-1"}
		rc := cmdDiff(canned(200, "{}"), unset, "att-0123456789ab", "att-ba9876543210", "", "", false,
			&out, &errOut)
		want := "[ocagent] diff: no OC_BASE configured — nothing here knows which station " +
			"to talk to, and the built-in default is this machine's loopback address.\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", the OC_BASE refusal)", rc, out.String(), errOut.String())
		}
	})

	t.Run("an unset OC_BASE refuses the external flavour too, sending nothing", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, `{"url":"/diff/s/abc"}`)
		unset := Config{Base: defaultBase, Token: "tok-1"}
		rc := cmdDiff(client, unset, "att-0123456789ab", "att-ba9876543210", "", "", true, &out, &errOut)
		if rc != 3 || out.String() != "" || len(client.sent) != 0 {
			t.Fatalf("got (%d, %q, sent %+v), want (3, \"\", nothing sent)", rc, out.String(), client.sent)
		}
	})

	t.Run("a bad side is refused before the OC_BASE guard runs", func(t *testing.T) {
		var out, errOut bytes.Buffer
		unset := Config{Base: defaultBase, Token: "tok-1"}
		rc := cmdDiff(canned(200, "{}"), unset, "nonsense", "att-ba9876543210", "", "", false, &out, &errOut)
		if rc != 2 || !strings.Contains(errOut.String(), "the before side \"nonsense\"") {
			t.Fatalf("got (%d, %q), want (2, the side refusal, not the OC_BASE one)", rc, errOut.String())
		}
	})

	t.Run("--external hands the pair to the server and prints the signed link", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, `{"url":"/diff/s/abc123"}`)
		rc := cmdDiff(client, configured, "att-0123456789ab", "att-ba9876543210", " Old ", "", true,
			&out, &errOut)
		if rc != 0 || out.String() != "https://station.example.com/diff/s/abc123\n" {
			t.Fatalf("got (%d, %q), want (0, the absolutised signed link)", rc, out.String())
		}
		wantURL := "https://station.example.com/api/diff/share-link?" +
			"after=att-ba9876543210&before=att-0123456789ab&label_before=Old"
		if len(client.sent) != 1 || client.sent[0].url != wantURL {
			t.Fatalf("sent %+v, want one GET to %q", client.sent, wantURL)
		}
	})
}

func TestMintExternalDiffLink(t *testing.T) {
	configured := Config{Base: "https://station.example.com", BaseConfigured: true, Token: "tok-1"}

	t.Run("a 200 link body is absolutised against this reader's own base", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, `{"url":"/diff/s/abc123"}`)
		rc := mintExternalDiffLink(client, configured, "att-0123456789ab", "att-ba9876543210",
			"Old", "New", &out, &errOut)
		if rc != 0 || out.String() != "https://station.example.com/diff/s/abc123\n" || errOut.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (0, the absolutised link, \"\")",
				rc, out.String(), errOut.String())
		}
		want := sentRequest{
			method: "GET",
			url: "https://station.example.com/api/diff/share-link?after=att-ba9876543210" +
				"&before=att-0123456789ab&label_after=New&label_before=Old",
			ua:     "ocagent/0.1",
			accept: "application/json",
			auth:   "Bearer tok-1",
		}
		if len(client.sent) != 1 || client.sent[0] != want {
			t.Fatalf("sent %+v, want %+v", client.sent, want)
		}
	})

	t.Run("no token refuses before any request is built", func(t *testing.T) {
		var out, errOut bytes.Buffer
		client := canned(200, `{"url":"/diff/s/abc"}`)
		noToken := Config{Base: "https://station.example.com", BaseConfigured: true}
		rc := mintExternalDiffLink(client, noToken, "att-0123456789ab", "att-ba9876543210", "", "",
			&out, &errOut)
		want := "[ocagent] diff: no OC_TOKEN configured — minting an external link is an authed call.\n"
		if rc != 3 || errOut.String() != want || out.String() != "" {
			t.Fatalf("got (%d, %q, %q), want (3, \"\", the no-token refusal)",
				rc, out.String(), errOut.String())
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing", client.sent)
		}
	})

	t.Run("a transport failure is exit 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		rc := mintExternalDiffLink(failingHTTP("connection refused"), configured,
			"att-0123456789ab", "att-ba9876543210", "", "", &out, &errOut)
		want := "[ocagent] diff: request failed (network): connection refused\n"
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
			"[ocagent] diff: auth rejected (HTTP 401): {\"detail\":\"bad token\"}\n"},
		{"403 is an auth failure", 403, `{"detail":"forbidden"}`, 3,
			"[ocagent] diff: auth rejected (HTTP 403): {\"detail\":\"forbidden\"}\n"},
		{"400 is the server rejecting the pair", 400, `{"detail":"unknown side"}`, 4,
			"[ocagent] diff: server rejected the pair (HTTP 400): {\"detail\":\"unknown side\"}\n"},
		{"422 is the server rejecting the pair", 422, `{"detail":"bad address"}`, 4,
			"[ocagent] diff: server rejected the pair (HTTP 422): {\"detail\":\"bad address\"}\n"},
		{"500 is anything else", 500, "upstream exploded", 5,
			"[ocagent] diff: unexpected HTTP 500: upstream exploded\n"},
		{"200 with a body that is not a link is anything else", 200, "not json", 5,
			"[ocagent] diff: 200 but unparseable link body: not json\n"},
		{"200 with an empty url is anything else", 200, `{"url":""}`, 5,
			"[ocagent] diff: 200 but unparseable link body: {\"url\":\"\"}\n"},
	}
	for _, tc := range statusCases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			rc := mintExternalDiffLink(canned(tc.status, tc.body), configured,
				"att-0123456789ab", "att-ba9876543210", "", "", &out, &errOut)
			if rc != tc.wantRC || errOut.String() != tc.wantErr || out.String() != "" {
				t.Fatalf("got (%d, %q, %q), want (%d, \"\", %q)",
					rc, out.String(), errOut.String(), tc.wantRC, tc.wantErr)
			}
		})
	}
}

func TestDiffUsage(t *testing.T) {
	var out bytes.Buffer
	diffUsage(&out)
	text := out.String()

	t.Run("it opens with the invocation line naming every flag the subcommand takes", func(t *testing.T) {
		want := "usage: ocagent diff <before> <after> [--label-before <text>] [--label-after <text>] [--external]\n"
		if !strings.HasPrefix(text, want) {
			t.Fatalf("usage starts %q, want it to start %q", text[:min(len(text), len(want))], want)
		}
	})

	t.Run("the example attachment id it teaches is one sideRefusal actually accepts", func(t *testing.T) {
		if !strings.Contains(text, "att-0123456789ab") {
			t.Fatal("the usage no longer shows an example attachment id")
		}
		if msg := sideRefusal("att-0123456789ab", "before"); msg != "" {
			t.Fatalf("the id the usage teaches is refused: %s", msg)
		}
	})

	t.Run("every <at> spelling it teaches is one sideRefusal actually accepts", func(t *testing.T) {
		if !strings.Contains(text, "doc:<kind>/<key>/<at>/<field>") {
			t.Fatal("the usage no longer shows the document address form")
		}
		for _, at := range []string{"current", "seed"} {
			if !strings.Contains(text, at) {
				t.Fatalf("the usage no longer names the <at> value %q", at)
			}
			if msg := sideRefusal("doc:task/T-1/"+at+"/body", "before"); msg != "" {
				t.Fatalf("the <at> the usage teaches is refused: %s", msg)
			}
		}
	})
}
