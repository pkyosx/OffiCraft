package main

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// sentRequest flattens one outbound request to the facts the callers promise.
type sentRequest struct {
	method string
	url    string
	ua     string
	accept string
	ctype  string
	auth   string
	length int64
	body   string
}

// cannedReply is one answer a fake client hands back, or the transport failure
// it reports instead.
type cannedReply struct {
	status int
	body   string
	header http.Header
	err    error
}

// cannedHTTP answers each Do with the next canned reply (200/empty once the
// list runs out) and records every request it was asked to send.
type cannedHTTP struct {
	replies []cannedReply
	sent    []sentRequest
}

func (c *cannedHTTP) Do(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	c.sent = append(c.sent, sentRequest{
		method: req.Method,
		url:    req.URL.String(),
		ua:     req.Header.Get("User-Agent"),
		accept: req.Header.Get("Accept"),
		ctype:  req.Header.Get("Content-Type"),
		auth:   req.Header.Get("Authorization"),
		length: req.ContentLength,
		body:   body,
	})
	reply := cannedReply{status: 200}
	if len(c.replies) > 0 {
		reply = c.replies[0]
		c.replies = c.replies[1:]
	}
	if reply.err != nil {
		return nil, reply.err
	}
	header := reply.header
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: reply.status,
		Body:       io.NopCloser(strings.NewReader(reply.body)),
		Header:     header,
	}, nil
}

// canned builds a fake client that answers exactly once.
func canned(status int, body string) *cannedHTTP {
	return &cannedHTTP{replies: []cannedReply{{status: status, body: body}}}
}

// failingHTTP builds a fake client whose transport refuses.
func failingHTTP(reason string) *cannedHTTP {
	return &cannedHTTP{replies: []cannedReply{{err: errors.New(reason)}}}
}

func TestHttpRequest(t *testing.T) {
	t.Run("an authed GET carries the agent identity headers and no content type", func(t *testing.T) {
		client := canned(200, "pong")
		status, text := httpRequest(client, http.MethodGet, "http://s/api/members", "tok-1", nil)
		if status != 200 || text != "pong" {
			t.Fatalf("got (%d, %q), want (200, \"pong\")", status, text)
		}
		want := []sentRequest{{
			method: "GET",
			url:    "http://s/api/members",
			ua:     "ocagent/0.1",
			accept: "application/json",
			ctype:  "",
			auth:   "Bearer tok-1",
			body:   "",
		}}
		if !reflect.DeepEqual(client.sent, want) {
			t.Fatalf("sent %+v, want %+v", client.sent, want)
		}
	})

	t.Run("a JSON body is marshalled and declares its content type", func(t *testing.T) {
		client := canned(200, `{"ok":true}`)
		status, text := httpRequest(client, http.MethodPost, "http://s/api/chat",
			"tok-2", map[string]any{"text": "hi"})
		if status != 200 || text != `{"ok":true}` {
			t.Fatalf("got (%d, %q), want (200, `{\"ok\":true}`)", status, text)
		}
		want := []sentRequest{{
			method: "POST",
			url:    "http://s/api/chat",
			ua:     "ocagent/0.1",
			accept: "application/json",
			ctype:  "application/json",
			auth:   "Bearer tok-2",
			length: 13,
			body:   `{"text":"hi"}`,
		}}
		if !reflect.DeepEqual(client.sent, want) {
			t.Fatalf("sent %+v, want %+v", client.sent, want)
		}
	})

	t.Run("an empty token sends no Authorization header", func(t *testing.T) {
		client := canned(200, "{}")
		httpRequest(client, http.MethodGet, "http://s/api/version", "", nil)
		if client.sent[0].auth != "" {
			t.Fatalf("Authorization %q, want none", client.sent[0].auth)
		}
	})

	t.Run("an HTTP error status is data: the code and the body come back", func(t *testing.T) {
		client := canned(401, `{"detail":"bad token"}`)
		status, text := httpRequest(client, http.MethodGet, "http://s/api/members", "tok-3", nil)
		if status != 401 || text != `{"detail":"bad token"}` {
			t.Fatalf("got (%d, %q), want (401, `{\"detail\":\"bad token\"}`)", status, text)
		}
	})

	t.Run("a transport failure surfaces as status 0 plus the reason", func(t *testing.T) {
		client := failingHTTP("dial tcp 127.0.0.1:7755: connect: connection refused")
		status, text := httpRequest(client, http.MethodGet, "http://s/api/members", "tok-4", nil)
		if status != 0 || text != "dial tcp 127.0.0.1:7755: connect: connection refused" {
			t.Fatalf("got (%d, %q), want (0, the dial reason)", status, text)
		}
	})

	t.Run("an unmarshalable body fails before any request is sent", func(t *testing.T) {
		client := canned(200, "{}")
		status, text := httpRequest(client, http.MethodPost, "http://s/api/chat", "tok-5", make(chan int))
		if status != 0 || text != "json: unsupported type: chan int" {
			t.Fatalf("got (%d, %q), want (0, \"json: unsupported type: chan int\")", status, text)
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing on the wire", client.sent)
		}
	})

	t.Run("an unbuildable request fails before any request is sent", func(t *testing.T) {
		client := canned(200, "{}")
		status, text := httpRequest(client, "BAD METHOD", "http://s/api/members", "tok-6", nil)
		if status != 0 || text != `net/http: invalid method "BAD METHOD"` {
			t.Fatalf("got (%d, %q), want (0, the invalid-method reason)", status, text)
		}
		if len(client.sent) != 0 {
			t.Fatalf("sent %+v, want nothing on the wire", client.sent)
		}
	})
}

func TestGetJSON(t *testing.T) {
	cfg := Config{Base: "http://s", BaseConfigured: true, Token: "tok-1"}

	t.Run("authed reads ride the agent's own bearer token", func(t *testing.T) {
		client := canned(200, `{"members":[{"id":"a-1"}]}`)
		status, obj := getJSON(client, cfg, "/api/members", true)
		want := map[string]any{"members": []any{map[string]any{"id": "a-1"}}}
		if status != 200 || !reflect.DeepEqual(obj, want) {
			t.Fatalf("got (%d, %#v), want (200, %#v)", status, obj, want)
		}
		if client.sent[0].url != "http://s/api/members" || client.sent[0].auth != "Bearer tok-1" {
			t.Fatalf("sent %+v, want the authed members URL", client.sent[0])
		}
	})

	t.Run("a public read sends no token at all", func(t *testing.T) {
		client := canned(200, `{"version":"1.2.3"}`)
		status, obj := getJSON(client, cfg, "/api/version", false)
		want := map[string]any{"version": "1.2.3"}
		if status != 200 || !reflect.DeepEqual(obj, want) {
			t.Fatalf("got (%d, %#v), want (200, %#v)", status, obj, want)
		}
		if client.sent[0].auth != "" {
			t.Fatalf("Authorization %q, want none on a public route", client.sent[0].auth)
		}
	})

	t.Run("a non-JSON body parses to nil beside its real status", func(t *testing.T) {
		client := canned(502, "<html>bad gateway</html>")
		status, obj := getJSON(client, cfg, "/api/members", true)
		if status != 502 || obj != nil {
			t.Fatalf("got (%d, %#v), want (502, nil)", status, obj)
		}
	})

	t.Run("a transport failure is status 0 and nil", func(t *testing.T) {
		client := failingHTTP("connection refused")
		status, obj := getJSON(client, cfg, "/api/members", true)
		if status != 0 || obj != nil {
			t.Fatalf("got (%d, %#v), want (0, nil)", status, obj)
		}
	})
}

func TestPostJSON(t *testing.T) {
	cfg := Config{Base: "http://s", BaseConfigured: true, Token: "tok-1"}

	t.Run("the payload rides the body and the token rides the header", func(t *testing.T) {
		client := canned(200, `{"id":"m-9"}`)
		status, obj := postJSON(client, cfg, "/api/chat", map[string]any{"text": "hi"})
		want := map[string]any{"id": "m-9"}
		if status != 200 || !reflect.DeepEqual(obj, want) {
			t.Fatalf("got (%d, %#v), want (200, %#v)", status, obj, want)
		}
		sent := []sentRequest{{
			method: "POST",
			url:    "http://s/api/chat",
			ua:     "ocagent/0.1",
			accept: "application/json",
			ctype:  "application/json",
			auth:   "Bearer tok-1",
			length: 13,
			body:   `{"text":"hi"}`,
		}}
		if !reflect.DeepEqual(client.sent, sent) {
			t.Fatalf("sent %+v, want %+v", client.sent, sent)
		}
	})

	t.Run("a rejection keeps its status and its parsed detail", func(t *testing.T) {
		client := canned(422, `{"detail":"context_pct is not declared"}`)
		status, obj := postJSON(client, cfg, "/api/agent/context", map[string]any{"context_pct": 1})
		want := map[string]any{"detail": "context_pct is not declared"}
		if status != 422 || !reflect.DeepEqual(obj, want) {
			t.Fatalf("got (%d, %#v), want (422, %#v)", status, obj, want)
		}
	})

	t.Run("an empty 200 body parses to nil", func(t *testing.T) {
		client := canned(200, "")
		status, obj := postJSON(client, cfg, "/api/chat", map[string]any{})
		if status != 200 || obj != nil {
			t.Fatalf("got (%d, %#v), want (200, nil)", status, obj)
		}
	})
}

func TestSafeJSON(t *testing.T) {
	cases := []struct {
		name string
		text string
		want any
	}{
		{"an object decodes to a map with float64 numbers", `{"a":1,"b":"x"}`,
			map[string]any{"a": float64(1), "b": "x"}},
		{"an array decodes to a slice", `[1,"x"]`, []any{float64(1), "x"}},
		{"a bare string decodes to a string", `"hi"`, "hi"},
		{"a bare number decodes to float64", `2.5`, 2.5},
		{"a bare bool decodes to bool", `true`, true},
		{"JSON null is indistinguishable from a parse failure", `null`, nil},
		{"an empty body is nil", ``, nil},
		{"a non-JSON body is nil", `<html>nope</html>`, nil},
		{"trailing garbage after a valid value is nil", `{"a":1} oops`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeJSON(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("safeJSON(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
