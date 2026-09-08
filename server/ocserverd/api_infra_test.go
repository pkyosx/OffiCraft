// Skeleton generated from server/ocserverd/api_infra.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMarkStationShutdown(t *testing.T) {
	t.Skip("TODO: markStationShutdown records the process-level cause before the server cancels request contexts or the upgrade re-execs.")
}

func TestClearStationShutdown(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestCancelStationContext(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestDetachReasonForLog(t *testing.T) {
	t.Skip("TODO: detachReasonForLog keeps the operator vocabulary exactly as it was: an exit that concluded nothing is still reported as peer-closed, which is what a return with no recorded cause means.")
}

func TestSseContextDetachReason(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

// sseUnflushableWriter is a response writer with no Flush method — the one
// shape GET /api/events refuses before it opens a stream.
type sseUnflushableWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *sseUnflushableWriter) Header() http.Header         { return w.header }
func (w *sseUnflushableWriter) WriteHeader(status int)      { w.status = status }
func (w *sseUnflushableWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

func TestHandleEventsApiEventsGet(t *testing.T) {
	t.Run("a connection whose response writer cannot stream answers 500", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		w := &sseUnflushableWriter{header: http.Header{}}
		api.HandleEventsApiEventsGet(w, httptest.NewRequest("GET", "/api/events", nil))

		if w.status != 500 {
			t.Fatalf("want 500, got %d (%s)", w.status, w.body.String())
		}
		var got any
		if err := json.Unmarshal(w.body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", w.body.String())
		}
		apiWantValue(t, "body", got, map[string]any{
			"error": map[string]any{
				"code":    "internal_error",
				"message": "streaming unsupported",
			},
		})
		if ct := w.header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("want application/json, got %q", ct)
		}
		dashboard.wantFrames()
	})

	t.Run("an owner connection opens the stream with the SSE headers and is fanned every delta published while it is held", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		stream := apiEventsStream(t, h, owner, "")

		if stream.code() != 200 {
			t.Fatalf("want 200, got %d (%q)", stream.code(), stream.text())
		}
		for header, want := range map[string]string{
			"Content-Type":      "text/event-stream; charset=utf-8",
			"Cache-Control":     "no-cache",
			"X-Accel-Buffering": "no",
		} {
			if got := stream.header.Get(header); got != want {
				t.Fatalf("%s = %q, want %q", header, got, want)
			}
		}
		if got := stream.header.Get(sseStationSHAHeader); got != api.processSHA {
			t.Fatalf("%s = %q, want the running build %q", sseStationSHAHeader, got, api.processSHA)
		}
		if got := stream.text(); got != ": connected\n\n" {
			t.Fatalf("the stream must open with the connected greeting, got %q", got)
		}

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, `{"account":"acct/x"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		stream.waitFor("\ndata: ")
		stream.wantWireFrames(map[string]any{
			"seq":   1,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "acct/x",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})

		if row := apiTestSession(t, h, owner, "kip"); row["presence"] != "offline" {
			t.Fatalf("the owner connection must project nobody online, kip presence = %v", row["presence"])
		}
	})

	t.Run("an agent connection projects its member online for the life of the stream and banks the live cost when it drops", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`)
		if status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		stream := apiEventsStream(t, h, agent, "")

		apiWantValue(t, "session-online", any(apiTestSession(t, h, owner, "kip")), map[string]any{
			"id":          "kip",
			"name":        "Kip",
			"presence":    "online",
			"cost":        2.5,
			"banked_cost": nil,
			"account":     "",
			"context_pct": nil,
			"effort":      "",
			"machine":     "",
			"model":       "",
			"role":        "",
			"runtime":     "",
			"tokens":      nil,
		})

		stream.stop()

		apiWantValue(t, "session-offline", any(apiTestSession(t, h, owner, "kip")), map[string]any{
			"id":          "kip",
			"name":        "Kip",
			"presence":    "offline",
			"cost":        nil,
			"banked_cost": 2.5,
			"account":     "",
			"context_pct": nil,
			"effort":      "",
			"machine":     "",
			"model":       "",
			"role":        "",
			"runtime":     "",
			"tokens":      nil,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"owner_id":      "owner",
					"status":        "active",
					"desired_state": "",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		})
	})

	t.Run("a request without a credential is refused 401 before any stream opens", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/events", "", "")

		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		if row := apiTestSession(t, h, owner, "kip"); row["presence"] != "offline" {
			t.Fatalf("a refused connection must project nobody online, kip presence = %v", row["presence"])
		}
		dashboard.wantFrames()
	})

	t.Run("a member whose stop is in effect is refused 409 and never re-projects online", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, ""); status != 200 {
			t.Fatalf("deactivate: want 200, got %d (%v)", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, ""); status != 200 {
			t.Fatalf("report stopped: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/events", agent, "")

		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"member 'kip' has a stop in effect (desired_state=offline) — SSE refused "+
				"(a stopped member must not re-project online; activate it to reconnect)")
		if row := apiTestSession(t, h, owner, "kip"); row["presence"] != "stopped" {
			t.Fatalf("a refused reconnect must stay stopped, kip presence = %v", row["presence"])
		}
		dashboard.wantFrames()
	})

	t.Run("a request body is ignored, so even a malformed one still opens the stream", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		stream := apiEventsStream(t, h, owner, "{{{")

		if stream.code() != 200 {
			t.Fatalf("want 200, got %d (%q)", stream.code(), stream.text())
		}
		if got := stream.text(); got != ": connected\n\n" {
			t.Fatalf("want the connected greeting, got %q", got)
		}
		stream.wantWireFrames()
	})

	t.Run("a second connection for the same member takes the slot over and the first one ends while the member stays online", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		first := apiEventsStream(t, h, agent, "")
		second := apiEventsStream(t, h, agent, "")

		<-first.done
		if first.code() != 200 {
			t.Fatalf("the taken-over stream want 200, got %d (%q)", first.code(), first.text())
		}
		if second.code() != 200 {
			t.Fatalf("the takeover stream want 200, got %d (%q)", second.code(), second.text())
		}
		if row := apiTestSession(t, h, owner, "kip"); row["presence"] != "online" {
			t.Fatalf("the member must stay online across a takeover, kip presence = %v", row["presence"])
		}
	})
}

func TestSseStopGateRefusal(t *testing.T) {
	t.Skip("TODO: sseStopGateRefusal is the zombie SSE gate predicate (defence line B of the zombie-agent work; line A is the warden's process-tree sweep).")
}

func TestOnFirstConnect(t *testing.T) {
	t.Skip("TODO: onFirstConnect handles the SSE first-connect edge for an agent connection: clear the caller's waking anchor (the wake completed) and stamp the session boot_ts on its gauge entry.")
}

func TestAnchorSessionBoot(t *testing.T) {
	t.Skip("TODO: anchorSessionBoot is the T-4235 session-anchor resolution, run on the SSE first-connect edge.")
}

func TestStampLandedMachine(t *testing.T) {
	t.Skip("TODO: stampLandedMachine records the machine a session actually connected from (T-98f4) — the durable anchor rule 3 of the outsource placement decision reads (「沒被搬過 + 不是第一次 → 留在上一輪實際跑的那台」), and, for every kind, the last-observed machine the cockpit compares the owner's pin against.")
}

func TestClearSessionBootTS(t *testing.T) {
	t.Skip("TODO: clearSessionBootTS drops session-scoped gauge state from a member's / worker's gauge entry at a real session BOUNDARY — a START dispatch that begins a new session, or a STOP/kill that ends one.")
}

func TestOnLastDisconnect(t *testing.T) {
	t.Skip("TODO: onLastDisconnect handles the SSE last-disconnect edge for an agent connection: fold the live telemetry cost into the actor's durable banked_cost, then POP the live field (exactly-once-per-edge banking).")
}

func TestPublishOutsourcePresenceEdge(t *testing.T) {
	t.Skip("TODO: publishOutsourcePresenceEdge makes the worker-list projection converge after a real SSE online edge.")
}

func TestBankLiveCost(t *testing.T) {
	t.Skip("TODO: bankLiveCost is the ONE cost-banking fold for BOTH actor kinds (T-ba6b — owner constitution: 外包＝系統代管的正職員工, so the worker reuses the member mechanism instead of a parallel copy): pop the actor's live telemetry cost and add it to the durable member.banked_cost of whichever kind the id resolves to (the outsource_worker table was folded into member in 00025, so both kinds are the same column and the same sole writer).")
}

func TestDropLiveCost(t *testing.T) {
	t.Skip("TODO: dropLiveCost removes the live telemetry cost from an actor's entry and reports what it removed (nil when there was nothing there).")
}

func TestHandleResetCostApiMembersMemberIdCostResetPost(t *testing.T) {
	t.Run("the live figure alone is cleared, answered as the receipt of what was destroyed, and fanned to the member's watchers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		subject := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/cost/reset", owner, "")

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":           "kip",
			"cleared_cost":        2.5,
			"cleared_banked_cost": nil,
		})
		apiWantValue(t, "session", any(apiTestSession(t, h, owner, "kip")), map[string]any{
			"id":          "kip",
			"name":        "Kip",
			"presence":    "online",
			"cost":        nil,
			"banked_cost": nil,
			"account":     "",
			"context_pct": nil,
			"effort":      "",
			"machine":     "",
			"model":       "",
			"role":        "",
			"runtime":     "",
			"tokens":      nil,
		})
		memberFrame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"owner_id":      "owner",
					"status":        "active",
					"desired_state": "",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(memberFrame, map[string]any{
			"seq":   3,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "kip",
				"epoch":   3,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		subject.wantFrames(memberFrame)
		bystander.wantFrames()
	})

	t.Run("the durable figure a finished session banked is cleared and returned as the receipt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		apiEventsStream(t, h, agent, "").stop()
		if row := apiTestSession(t, h, owner, "kip"); row["banked_cost"] != 2.5 {
			t.Fatalf("premise: the session must have banked 2.5, got %v", row)
		}

		status, data := apiJSON(t, h, "POST", "/api/members/kip/cost/reset", owner, "")

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":           "kip",
			"cleared_cost":        nil,
			"cleared_banked_cost": 2.5,
		})
		apiWantValue(t, "session", any(apiTestSession(t, h, owner, "kip")), map[string]any{
			"id":          "kip",
			"name":        "Kip",
			"presence":    "offline",
			"cost":        nil,
			"banked_cost": nil,
			"account":     "",
			"context_pct": nil,
			"effort":      "",
			"machine":     "",
			"model":       "",
			"role":        "",
			"runtime":     "",
			"tokens":      nil,
		})
	})

	t.Run("clearing one actor leaves the account's own accumulator exactly where it was", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		status, data := apiJSON(t, h, "POST", "/api/members/kip/cost/reset", owner, "")

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":           "kip",
			"cleared_cost":        2.5,
			"cleared_banked_cost": nil,
		})
		apiWantValue(t, "account", any(apiTestAccount(t, h, owner, "acct/x")), map[string]any{
			"account":      "acct/x",
			"display_name": "acct/x",
			"cost":         2.5,
			"machine":      "",
			"five_hour":    nil,
			"seven_day":    nil,
		})
	})

	t.Run("an id that is neither a member nor a worker answers 404 and destroys nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ghost/cost/reset", owner, "")

		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
		if row := apiTestSession(t, h, owner, "kip"); row["cost"] != 2.5 {
			t.Fatalf("no other actor may be touched, kip cost = %v", row["cost"])
		}
		dashboard.wantFrames()
	})

	t.Run("an admin_agent identity is refused 403 because this row is owner-only, and the figure survives", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		admin := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/cost/reset", admin, "")

		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		if row := apiTestSession(t, h, owner, "kip"); row["cost"] != 2.5 {
			t.Fatalf("a refused reset must destroy nothing, kip cost = %v", row["cost"])
		}
		dashboard.wantFrames()
	})

	t.Run("a request without a credential is refused 401 and the figure survives", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/cost/reset", "", "")

		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		if row := apiTestSession(t, h, owner, "kip"); row["cost"] != 2.5 {
			t.Fatalf("a refused reset must destroy nothing, kip cost = %v", row["cost"])
		}
		dashboard.wantFrames()
	})

	t.Run("the request body is ignored, so a malformed one still clears the figure", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		status, data := apiJSON(t, h, "POST", "/api/members/kip/cost/reset", owner, "{{{")

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":           "kip",
			"cleared_cost":        2.5,
			"cleared_banked_cost": nil,
		})
		if row := apiTestSession(t, h, owner, "kip"); row["cost"] != nil {
			t.Fatalf("the figure must be gone, kip cost = %v", row["cost"])
		}
	})
}

func TestAccrueAccountSpend(t *testing.T) {
	t.Skip("TODO: accrueAccountSpend credits the NEW spend in one telemetry report to the account it was reported under (T-53, owner ruling rc-5c5d7c7c6dcd 「分開：帳號卡自己一份數字，清它不動成員」).")
}

func TestStartAccountSpendSession(t *testing.T) {
	t.Skip("TODO: startAccountSpendSession forgets the accrual baseline because a NEW SESSION is starting: the next cost this actor reports is counted from zero, so its whole figure is new spend rather than an increase over the previous session's.")
}

func TestHandleResetAccountCostApiAccountsCostResetPost(t *testing.T) {
	t.Run("the account's own accumulator is cleared, receipted, and every actor's figure is left alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, `{"account":"acct/x"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"account":      "acct/x",
			"cleared_cost": 2.5,
		})
		apiWantValue(t, "account", any(apiTestAccount(t, h, owner, "acct/x")), map[string]any{
			"account":      "acct/x",
			"display_name": "acct/x",
			"cost":         nil,
			"machine":      "",
			"five_hour":    nil,
			"seven_day":    nil,
		})
		if row := apiTestSession(t, h, owner, "kip"); row["cost"] != 2.5 || row["banked_cost"] != nil {
			t.Fatalf("clearing the account must not move the actor's figures, kip = %v", row)
		}
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "acct/x",
				"epoch":   3,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("pressing it again is 200 with nothing to clear rather than an error", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, `{"account":"acct/x"}`); status != 200 {
			t.Fatalf("first press: want 200, got %d (%v)", status, data)
		}

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, `{"account":"acct/x"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"account":      "acct/x",
			"cleared_cost": nil,
		})
	})

	t.Run("an account tag nothing ever reported is 200 with nothing cleared and no row is created", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, `{"account":"acct/ghost"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"account":      "acct/ghost",
			"cleared_cost": nil,
		})
		if row := apiTestAccount(t, h, owner, "acct/ghost"); row != nil {
			t.Fatalf("an unreported account must not become a card, got %v", row)
		}
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("the reported account must be untouched, got %v", row)
		}
	})

	t.Run("a blank account is refused 422 and nothing is cleared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, `{"account":"   "}`)

		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "account cannot be blank")
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("a refused reset must clear nothing, got %v", row)
		}
		dashboard.wantFrames()
	})

	t.Run("a body that is not JSON is refused 422 by the wire layer and nothing is cleared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", owner, "{{{")

		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("a refused reset must clear nothing, got %v", row)
		}
		dashboard.wantFrames()
	})

	t.Run("an admin_agent identity is refused 403 because this row is owner-only, and the figure survives", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		admin := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", admin, `{"account":"acct/x"}`)

		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("a refused reset must clear nothing, got %v", row)
		}
		dashboard.wantFrames()
	})

	t.Run("a request without a credential is refused 401 and the figure survives", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/accounts/cost/reset", "", `{"account":"acct/x"}`)

		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("a refused reset must clear nothing, got %v", row)
		}
		dashboard.wantFrames()
	})
}

func TestNonZeroCost(t *testing.T) {
	t.Skip("TODO: nonZeroCost mirrors foldActorRuntime's rule for the banked figure: 0 is not put on the wire.")
}

func TestPublishMonitoringSignal(t *testing.T) {
	t.Skip("TODO: publishMonitoringSignal fans the same owner-only cockpit invalidation the telemetry ingest fans, so a reset converges the 估計$ cell without waiting for the next sample.")
}

func TestRpcError(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestRpcResult(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestMcpCatalogTools(t *testing.T) {
	t.Skip("TODO: mcpCatalogTools loads the FROZEN tool catalog (spec/mcp-catalog.json — the committed wire SSOT the Python tools/list serves byte-equal descriptors of).")
}

func TestHandleMcpApiMcpPost(t *testing.T) {
	t.Run("initialize echoes the protocol version the client asked for and names the station", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner,
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "officraft", "version": appVersion},
			},
		})
	})

	t.Run("initialize without a requested version answers the station's own protocol version", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, `{"jsonrpc":"2.0","id":"a","method":"initialize"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      "a",
			"result": map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "officraft", "version": appVersion},
			},
		})
	})

	t.Run("ping answers an empty result under the same envelope", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"result":  map[string]any{},
		})
	})

	t.Run("a notification — no id, or the notifications/* namespace — is acknowledged 202 with no response object", func(t *testing.T) {
		api, h, owner := apiTestMCPServer(t)
		dashboard := apiTestListen(t, api, "")

		for _, body := range []string{
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","method":"ping"}`,
			`{"jsonrpc":"2.0","id":3,"method":"notifications/cancelled"}`,
		} {
			rec := apiRequest(t, h, "POST", "/api/mcp", owner, body)
			if rec.Code != 202 {
				t.Fatalf("%s: want 202, got %d (%s)", body, rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); got != "null" {
				t.Fatalf("%s: want the bare null acknowledgement, got %q", body, got)
			}
		}
		dashboard.wantFrames()
	})

	t.Run("a body that is not JSON answers the parse error with a null id", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, "{{{")

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      nil,
			"error": map[string]any{
				"code":    -32700,
				"message": "parse error: body is not valid JSON",
			},
		})
	})

	t.Run("a JSON body that is not an object answers invalid request", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      nil,
			"error": map[string]any{
				"code":    -32600,
				"message": "invalid request: expected a JSON object",
			},
		})
	})

	t.Run("a method that is not a string answers invalid request with the id echoed back", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, `{"jsonrpc":"2.0","id":7,"method":5}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      7,
			"error": map[string]any{
				"code":    -32600,
				"message": "invalid request: method must be a string",
			},
		})
	})

	t.Run("a method outside the transport's own vocabulary answers method not found", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, `{"jsonrpc":"2.0","id":8,"method":"tools/invoke"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      8,
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found: 'tools/invoke'",
			},
		})
	})

	t.Run("tools/list serves the frozen catalog and leaves the two transport rows out of it", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		if data["jsonrpc"] != "2.0" || data["id"] != float64(9) {
			t.Fatalf("want the JSON-RPC envelope for id 9, got %v", data)
		}
		result, _ := data["result"].(map[string]any)
		if len(result) != 1 {
			t.Fatalf("tools/list result must carry tools and nothing else, got %v", result)
		}
		tools, _ := result["tools"].([]any)
		if len(tools) != 127 {
			t.Fatalf("want the frozen catalog's 127 descriptors, got %d", len(tools))
		}
		listed := map[string]any{}
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			name, _ := tool["name"].(string)
			if name == "" {
				t.Fatalf("every descriptor must be named, got %v", tool)
			}
			listed[name] = tool
		}
		for _, absent := range []string{"get_events", "post_mcp"} {
			if _, found := listed[absent]; found {
				t.Fatalf("%q is the transport, not a tool — it must not be listed", absent)
			}
		}
		apiWantValue(t, "get_member", listed["get_member"], map[string]any{
			"name":        "get_member",
			"description": apiAnyString,
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"member_id": map[string]any{"type": "string"}},
				"required":   []any{"member_id"},
			},
		})
	})

	t.Run("tools/call params of the wrong shape answer invalid params without reaching a route", func(t *testing.T) {
		api, h, owner := apiTestMCPServer(t)
		dashboard := apiTestListen(t, api, "")

		for _, probe := range []struct {
			body    string
			message string
		}{
			{`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":[]}`,
				"invalid params: expected an object"},
			{`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":1}}`,
				"invalid params: name must be a string"},
			{`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"get_member","arguments":[]}}`,
				"invalid params: 'arguments' must be an object"},
			{`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"get_events","arguments":{}}}`,
				"unknown tool: 'get_events'"},
		} {
			status, data := apiMCP(t, h, owner, probe.body)
			if status != 200 {
				t.Fatalf("%s: want 200, got %d (%v)", probe.body, status, data)
			}
			apiWantBody(t, data, map[string]any{
				"jsonrpc": "2.0",
				"id":      10,
				"error": map[string]any{
					"code":    -32602,
					"message": probe.message,
				},
			})
		}
		dashboard.wantFrames()
	})

	t.Run("a path argument that is missing, or one that could be cleaned into another route, is refused as a tool-level result", func(t *testing.T) {
		api, h, owner := apiTestMCPServer(t)
		dashboard := apiTestListen(t, api, "")

		for _, probe := range []struct {
			arguments string
			message   string
		}{
			{`{}`, "field required: member_id"},
			{`{"member_id":"   "}`, "field required: member_id"},
			{`{"member_id":"../members"}`, "invalid path: member_id"},
		} {
			status, data := apiMCP(t, h, owner,
				`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"get_member","arguments":`+probe.arguments+`}}`)
			if status != 200 {
				t.Fatalf("%s: want 200, got %d (%v)", probe.arguments, status, data)
			}
			refusal := `{"error":{"code":"validation_error","message":"` + probe.message + `"}}`
			apiWantBody(t, data, map[string]any{
				"jsonrpc": "2.0",
				"id":      11,
				"result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": refusal}},
					"isError": true,
					"structuredContent": map[string]any{
						"error": map[string]any{"code": "validation_error", "message": probe.message},
					},
				},
			})
		}
		dashboard.wantFrames()
	})

	t.Run("a tools/call re-enters the route, writes through it and wraps the receipt as a CallToolResult", func(t *testing.T) {
		api, h, owner := apiTestMCPServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiMCP(t, h, owner,
			`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"update_member","arguments":{"member_id":"kip","name":"Kip Renamed"}}}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      12,
			"result": map[string]any{
				"content":           []any{map[string]any{"type": "text", "text": `{"id":"kip"}`}},
				"isError":           false,
				"structuredContent": map[string]any{"id": "kip"},
			},
		})
		_, member := apiJSON(t, h, "GET", "/api/members/kip", owner, "")
		apiWantValue(t, "member.name", member["name"], "Kip Renamed")
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip Renamed",
					"owner_id":      "owner",
					"status":        "active",
					"desired_state": "",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a GET tool's leftover arguments become the query string the route binds", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip"}`); status != 200 {
			t.Fatalf("create task: want 200, got %d (%v)", status, data)
		}

		status, data := apiMCP(t, h, owner,
			`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"list_tasks","arguments":{"status":"done"}}}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      13,
			"result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "[]"}},
				"isError": false,
			},
		})

		status, data = apiMCP(t, h, owner,
			`{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"list_tasks","arguments":{"status":"nope"}}}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		message := "status must be one of not_started, in_progress, waiting_owner, " +
			"waiting_external, reassigning, done, terminated, duplicated"
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      14,
			"result": map[string]any{
				"content": []any{map[string]any{"type": "text",
					"text": `{"error":{"code":"validation_error","message":"` + message + `"}}`}},
				"isError": true,
				"structuredContent": map[string]any{
					"error": map[string]any{"code": "validation_error", "message": message},
				},
			},
		})
	})

	t.Run("a route's own 4xx comes back as a successful JSON-RPC result carrying isError", func(t *testing.T) {
		_, h, owner := apiTestMCPServer(t)

		status, data := apiMCP(t, h, owner,
			`{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"get_member","arguments":{"member_id":"ghost"}}}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      15,
			"result": map[string]any{
				"content": []any{map[string]any{"type": "text",
					"text": `{"error":{"code":"not_found","message":"member 'ghost' not found"}}`}},
				"isError": true,
				"structuredContent": map[string]any{
					"error": map[string]any{"code": "not_found", "message": "member 'ghost' not found"},
				},
			},
		})
	})

	t.Run("the caller's own rank travels with the loopback, so a plain agent's admin-gated tool is refused and nothing is written", func(t *testing.T) {
		api, h, owner := apiTestMCPServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiMCP(t, h, agent,
			`{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"dismiss_member","arguments":{"member_id":"mira"}}}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"jsonrpc": "2.0",
			"id":      16,
			"result": map[string]any{
				"content": []any{map[string]any{"type": "text",
					"text": `{"error":{"code":"forbidden","message":"principal not permitted"}}`}},
				"isError": true,
				"structuredContent": map[string]any{
					"error": map[string]any{"code": "forbidden", "message": "principal not permitted"},
				},
			},
		})
		roster, member := apiJSON(t, h, "GET", "/api/members/mira", owner, "")
		if roster != 200 {
			t.Fatalf("mira must still be on the roster, got %d (%v)", roster, member)
		}
		apiWantValue(t, "member.roster_status", member["roster_status"], "active")
		dashboard.wantFrames()
	})

	t.Run("a request without a credential is refused 401 by the transport's own gate, not as a JSON-RPC error", func(t *testing.T) {
		_, h, _ := apiTestMCPServer(t)

		status, data := apiMCP(t, h, "", `{"jsonrpc":"2.0","id":17,"method":"ping"}`)

		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandoverNoticeTick(t *testing.T) {
	t.Skip("TODO: handoverNoticeTick is ONE quiet tick of the context-high band: it reports the frame to write, or ok=false to stay quiet.")
}

// apiEventStream is one live GET /api/events connection driven through the
// whole handler stack in memory. The route only returns when its request
// context ends, so every stream is opened under a cancel + hard deadline and
// the cleanup WAITS for the handler to return: a test that forgets to stop one
// still terminates, and nothing leaks past the test that opened it.
type apiEventStream struct {
	t      *testing.T
	mu     sync.Mutex
	header http.Header
	status int
	body   strings.Builder
	wrote  chan struct{}
	done   chan struct{}
	cancel context.CancelFunc
}

func (c *apiEventStream) Header() http.Header { return c.header }

func (c *apiEventStream) WriteHeader(status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == 0 {
		c.status = status
	}
}

func (c *apiEventStream) Write(p []byte) (int, error) {
	c.mu.Lock()
	if c.status == 0 {
		c.status = http.StatusOK
	}
	c.body.Write(p)
	c.mu.Unlock()
	select {
	case c.wrote <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (c *apiEventStream) Flush() {}

func (c *apiEventStream) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.body.String()
}

func (c *apiEventStream) code() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// apiEventsStream opens GET /api/events with credential and body, and answers
// once the handler has written its greeting.
func apiEventsStream(t *testing.T, h http.Handler, credential, body string) *apiEventStream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	req := httptest.NewRequest("GET", "/api/events", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	c := &apiEventStream{
		t: t, header: http.Header{},
		wrote:  make(chan struct{}, 64),
		done:   make(chan struct{}),
		cancel: cancel,
	}
	go func() {
		defer close(c.done)
		h.ServeHTTP(c, req)
	}()
	t.Cleanup(func() { cancel(); <-c.done })
	c.waitFor(": connected\n\n")
	return c
}

// waitFor blocks until the stream text contains want, and fails rather than
// hanging if the stream ends or the wait runs long.
func (c *apiEventStream) waitFor(want string) {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if strings.Contains(c.text(), want) {
			return
		}
		select {
		case <-c.wrote:
		case <-c.done:
			if strings.Contains(c.text(), want) {
				return
			}
			c.t.Fatalf("the stream ended without %q: %q", want, c.text())
		case <-deadline:
			c.t.Fatalf("timed out waiting for %q on the stream: %q", want, c.text())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// stop ends the connection the way a client dropping does, and waits for the
// handler — including its last-disconnect edge — to finish.
func (c *apiEventStream) stop() {
	c.cancel()
	<-c.done
}

// wantWireFrames asserts the delta frames written onto THIS socket, decoded the
// way apiTestListener.wantFrames decodes the buffered ones.
func (c *apiEventStream) wantWireFrames(want ...map[string]any) {
	c.t.Helper()
	got := []any{}
	for _, chunk := range strings.Split(c.text(), "\n\n") {
		if !strings.Contains(chunk, "\ndata: ") {
			continue
		}
		got = append(got, apiDecodeSSEFrame(c.t, []byte(chunk+"\n\n")))
	}
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(c.t, "wire", any(got), any(expected))
}

// apiTestMonitoringRow reads one actor's cockpit row back from GET
// /api/monitoring — the surface the cost figures and the SSE presence
// projection are actually read on.
func apiTestMonitoringRow(t *testing.T, h http.Handler, owner, collection, key, id string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
	if status != 200 {
		t.Fatalf("GET /api/monitoring: want 200, got %d (%v)", status, data)
	}
	rows, _ := data[collection].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row[key] == id {
			return row
		}
	}
	return nil
}

func apiTestSession(t *testing.T, h http.Handler, owner, id string) map[string]any {
	t.Helper()
	return apiTestMonitoringRow(t, h, owner, "sessions", "id", id)
}

func apiTestAccount(t *testing.T, h http.Handler, owner, account string) map[string]any {
	t.Helper()
	return apiTestMonitoringRow(t, h, owner, "accounts", "account", account)
}

// apiTestMCPServer is newAPITestServer with the tools/call loopback wired the
// way serve time wires it (server.go). Without this every tools/call answers
// "loopback handler not wired" instead of re-entering the route.
func apiTestMCPServer(t *testing.T) (*apiServer, http.Handler, string) {
	t.Helper()
	api, h, _, owner := newAPITestServer(t)
	api.loopback = h
	return api, h, owner
}

// apiMCP posts one JSON-RPC envelope at POST /api/mcp and answers the decoded
// response with its status.
func apiMCP(t *testing.T, h http.Handler, credential, body string) (int, map[string]any) {
	t.Helper()
	return apiJSON(t, h, "POST", "/api/mcp", credential, body)
}
