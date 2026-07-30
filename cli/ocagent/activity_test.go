package main

// activity_test.go — `ocagent report-activity` (T-a1d7). The three hard rules
// from activity.go's header are contracts, not preferences, so each gets a
// test: nothing on stdout, always exit 0, and one POST per invocation with no
// throttle or backoff standing between a turn boundary and the server.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func activityCfg(base string) Config {
	return Config{Base: base, Token: "tok-activity", ID: "m-1"}
}

func TestReportActivity_PostsTheBoundary(t *testing.T) {
	srv, posts := contextServer(t)
	var errw strings.Builder
	code := cmdReportActivity(srv.Client(), activityCfg(srv.URL),
		[]string{"--state", "active", "--turn-id", "turn-9"},
		time.Unix(1_700_000_000, 0), strings.NewReader(""), &errw)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(*posts) != 1 {
		t.Fatalf("want exactly 1 POST, got %d", len(*posts))
	}
	p := (*posts)[0]
	if p.path != "/api/self/activity" {
		t.Fatalf("path = %q", p.path)
	}
	if p.auth != "Bearer tok-activity" {
		t.Fatalf("the agent's OWN token must ride the request, got %q", p.auth)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(p.body), &body); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, p.body)
	}
	if body["state"] != "active" || body["turn_id"] != "turn-9" {
		t.Fatalf("body lost the boundary: %s", p.body)
	}
	if body["runtime"] != "claude" {
		t.Fatalf("runtime tag = %v, want claude", body["runtime"])
	}
	// ⚠️ NO agent_id / member_id anywhere: identity is the token's sub (§14 —
	// a caller never states who it is).
	for _, forbidden := range []string{"agent_id", "member_id"} {
		if _, present := body[forbidden]; present {
			t.Fatalf("%s must never be sent — identity comes from the token", forbidden)
		}
	}
}

// A UserPromptSubmit hook's stdout is injected into the model's context. This
// command therefore has NO stdout writer at all — the closest thing to a
// structural guarantee we can assert is that the one writer it does get (the
// error sink) is where every message lands.
func TestReportActivity_WritesNothingToStdout(t *testing.T) {
	srv, _ := contextServer(t)
	var errw strings.Builder
	// Success path: silent everywhere.
	cmdReportActivity(srv.Client(), activityCfg(srv.URL), []string{"--state", "idle"},
		time.Unix(1_700_000_000, 0), strings.NewReader(""), &errw)
	if errw.String() != "" {
		t.Fatalf("a successful report must say nothing at all, got %q", errw.String())
	}
}

// Failures must be LOUD in the log and INVISIBLE to the turn. A silent failure
// makes a column that quietly stopped updating look like an agent that quietly
// stopped working.
func TestReportActivity_FailureIsLoggedButNeverFailsTheTurn(t *testing.T) {
	srv, _ := refusingServer(t) // every POST answers 422
	var errw strings.Builder
	code := cmdReportActivity(srv.Client(), activityCfg(srv.URL), []string{"--state", "idle"},
		time.Unix(1_700_000_000, 0), strings.NewReader(""), &errw)
	if code != 0 {
		t.Fatalf("a hook must never fail the turn: exit %d", code)
	}
	if !strings.Contains(errw.String(), "FAILED status=422") {
		t.Fatalf("the failure must be visible in the log, got %q", errw.String())
	}
}

func TestReportActivity_RefusesAnyStateOutsideTheClosedSet(t *testing.T) {
	srv, posts := contextServer(t)
	for _, bad := range []string{"", "working", "ACTIVE"} {
		var errw strings.Builder
		if code := cmdReportActivity(srv.Client(), activityCfg(srv.URL),
			[]string{"--state", bad}, time.Unix(1_700_000_000, 0),
			strings.NewReader(""), &errw); code != 0 {
			t.Fatalf("--state %q must still exit 0, got %d", bad, code)
		}
		if errw.String() == "" {
			t.Fatalf("--state %q must be explained on stderr", bad)
		}
	}
	if len(*posts) != 0 {
		t.Fatalf("a bad state must not reach the server, got %d POSTs", len(*posts))
	}
}

// The hook payload carries the session id on stdin. Reading it is entirely
// best-effort: it scopes `seq`, and a report without it is still a real report.
func TestReportActivity_SessionIdFromHookPayload(t *testing.T) {
	t.Run("read from the hook's stdin JSON", func(t *testing.T) {
		srv, posts := contextServer(t)
		var errw strings.Builder
		cmdReportActivity(srv.Client(), activityCfg(srv.URL), []string{"--state", "active"},
			time.Unix(1_700_000_000, 0),
			strings.NewReader(`{"session_id":"sess-abc","prompt":"hi"}`), &errw)
		var body map[string]any
		_ = json.Unmarshal([]byte((*posts)[0].body), &body)
		if body["session_id"] != "sess-abc" {
			t.Fatalf("session_id = %v, want sess-abc", body["session_id"])
		}
	})
	t.Run("garbage stdin degrades to no session, never to a failure", func(t *testing.T) {
		srv, posts := contextServer(t)
		var errw strings.Builder
		code := cmdReportActivity(srv.Client(), activityCfg(srv.URL), []string{"--state", "active"},
			time.Unix(1_700_000_000, 0), strings.NewReader("not json at all"), &errw)
		if code != 0 || len(*posts) != 1 {
			t.Fatalf("the report must still go out: code=%d posts=%d", code, len(*posts))
		}
		var body map[string]any
		_ = json.Unmarshal([]byte((*posts)[0].body), &body)
		if _, present := body["session_id"]; present {
			t.Fatal("an unreadable payload must not produce a fabricated session id")
		}
	})
}

// 🔴 The deliberate DIFFERENCE from context-report: there is no throttle and no
// failure backoff. A turn boundary is a state TRANSITION — suppressing one does
// not delay information, it deletes it (a skipped UserPromptSubmit leaves the
// session reading idle for the whole turn, because Claude will not speak again
// until that turn ends).
func TestReportActivity_ConsecutiveBoundariesAreNeverSuppressed(t *testing.T) {
	srv, posts := contextServer(t)
	now := time.Unix(1_700_000_000, 0)
	for i, state := range []string{"active", "idle", "active", "idle"} {
		var errw strings.Builder
		cmdReportActivity(srv.Client(), activityCfg(srv.URL), []string{"--state", state},
			now.Add(time.Duration(i)*time.Millisecond), strings.NewReader(""), &errw)
	}
	if len(*posts) != 4 {
		t.Fatalf("every boundary must reach the server, got %d/4 POSTs", len(*posts))
	}
	// And the sequence numbers must be strictly increasing, or the server's
	// out-of-order guard would drop the later ones.
	var last float64
	for i, p := range *posts {
		var body map[string]any
		_ = json.Unmarshal([]byte(p.body), &body)
		seq, ok := body["seq"].(float64)
		if !ok {
			t.Fatalf("POST %d carries no seq: %s", i, p.body)
		}
		if i > 0 && seq <= last {
			t.Fatalf("seq must strictly increase (%v then %v)", last, seq)
		}
		last = seq
	}
}
