package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMarkStationShutdown(t *testing.T) {
	t.Run("a stream opened while the marker stands is greeted and then ends itself, while an unmarked station holds the same stream open", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		held := apiEventsStream(t, h, owner, "")
		select {
		case <-held.done:
			t.Fatalf("premise: an unmarked station must hold the stream open, got %q", held.text())
		case <-time.After(400 * time.Millisecond):
		}

		api.markStationShutdown()

		closing := apiEventsStream(t, h, owner, "")
		select {
		case <-closing.done:
		case <-time.After(10 * time.Second):
			t.Fatalf("a stream must end itself while the station is closing, got %q", closing.text())
		}
		if closing.code() != 200 {
			t.Fatalf("want 200, got %d", closing.code())
		}
		if got := closing.text(); got != ": connected\n\n" {
			t.Fatalf("want only the greeting, got %q", got)
		}
		dashboard.wantFrames()
	})

	t.Run("the marker is what the SSE context detach reason reads, and marking again leaves the same one answer", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		if got := api.sseContextDetachReason(); got != "peer-closed" {
			t.Fatalf("premise: a running station reads %q", got)
		}

		api.markStationShutdown()
		api.markStationShutdown()

		if got := api.sseContextDetachReason(); got != "station-shutdown" {
			t.Fatalf("want station-shutdown, got %q", got)
		}
	})
}

func TestClearStationShutdown(t *testing.T) {
	t.Run("clearing the marker puts the reason back and lets a stream be held open again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.markStationShutdown()

		api.clearStationShutdown()

		if got := api.sseContextDetachReason(); got != "peer-closed" {
			t.Fatalf("want peer-closed, got %q", got)
		}
		stream := apiEventsStream(t, h, owner, "")
		select {
		case <-stream.done:
			t.Fatalf("a cleared station must hold the stream open, got %q", stream.text())
		case <-time.After(400 * time.Millisecond):
		}
		if got := stream.text(); got != ": connected\n\n" {
			t.Fatalf("want only the greeting, got %q", got)
		}
	})

	t.Run("clearing a station that was never marked leaves it exactly where it was", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		api.clearStationShutdown()

		if got := api.sseContextDetachReason(); got != "peer-closed" {
			t.Fatalf("want peer-closed, got %q", got)
		}
	})
}

func TestCancelStationContext(t *testing.T) {
	t.Run("the wired cancel is called, so the station context ends as cancelled", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		api.stationCancel = cancel

		api.cancelStationContext()

		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("the station context must be done")
		}
		if err := ctx.Err(); err != context.Canceled {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("a station with no cancel wired is a no-op rather than a panic", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		api.cancelStationContext()

		if got := api.sseContextDetachReason(); got != "peer-closed" {
			t.Fatalf("want peer-closed, got %q", got)
		}
		stream := apiEventsStream(t, h, owner, "")
		select {
		case <-stream.done:
			t.Fatalf("nothing may have been cancelled, got %q", stream.text())
		case <-time.After(400 * time.Millisecond):
		}
		dashboard.wantFrames()
	})
}

func TestDetachReasonForLog(t *testing.T) {
	t.Run("an exit that concluded nothing is still reported as peer-closed", func(t *testing.T) {
		if got := detachReasonForLog(sseDetachReasonUnset); got != "peer-closed" {
			t.Fatalf("want peer-closed, got %q", got)
		}
	})

	t.Run("every concluded cause is printed exactly as it was recorded", func(t *testing.T) {
		for _, reason := range []string{
			sseDetachReasonTakeover,
			sseDetachReasonPeerClosed,
			sseDetachReasonWriteFailed,
			sseDetachReasonStationShutdown,
			"something nobody records",
		} {
			if got := detachReasonForLog(reason); got != reason {
				t.Fatalf("detachReasonForLog(%q) = %q", reason, got)
			}
		}
	})
}

func TestSseContextDetachReason(t *testing.T) {
	t.Run("a context that ended on a running station reads as the peer closing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		if got := api.sseContextDetachReason(); got != "peer-closed" {
			t.Fatalf("want peer-closed, got %q", got)
		}
	})

	t.Run("the same ended context reads as the station closing once the shutdown marker stands, and reverts when it is cleared", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.markStationShutdown()

		if got := api.sseContextDetachReason(); got != "station-shutdown" {
			t.Fatalf("want station-shutdown, got %q", got)
		}

		api.clearStationShutdown()

		if got := api.sseContextDetachReason(); got != "peer-closed" {
			t.Fatalf("want peer-closed, got %q", got)
		}
	})
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
	t.Run("an id the roster does not know and an ordinary active member are both admitted", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		if got := api.sseStopGateRefusal("ghost"); got != "" {
			t.Fatalf("an unknown sub must be admitted, got %q", got)
		}
		if got := api.sseStopGateRefusal("kip"); got != "" {
			t.Fatalf("an active member must be admitted, got %q", got)
		}
	})

	t.Run("a member working its close-out is still admitted, and refused the moment it reports stopped", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, ""); status != 200 {
			t.Fatalf("deactivate: want 200, got %d (%v)", status, data)
		}

		if got := api.sseStopGateRefusal("kip"); got != "" {
			t.Fatalf("a close-out in flight must be admitted, got %q", got)
		}

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, ""); status != 200 {
			t.Fatalf("report stopped: want 200, got %d (%v)", status, data)
		}

		if got := api.sseStopGateRefusal("kip"); got != "member 'kip' has a stop in effect (desired_state=offline) — "+
			"SSE refused (a stopped member must not re-project online; activate it to reconnect)" {
			t.Fatalf("a finished close-out must be refused, got %q", got)
		}
	})

	t.Run("a member the owner force-stopped is refused without waiting for it to report", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, ""); status != 200 {
			t.Fatalf("force-stop: want 200, got %d (%v)", status, data)
		}

		if got := api.sseStopGateRefusal("kip"); got != "member 'kip' has a stop in effect (desired_state=offline) — "+
			"SSE refused (a stopped member must not re-project online; activate it to reconnect)" {
			t.Fatalf("want the stop refusal, got %q", got)
		}
	})

	t.Run("activating the member again lifts the gate", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, ""); status != 200 {
			t.Fatalf("force-stop: want 200, got %d (%v)", status, data)
		}

		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, ""); status != 200 {
			t.Fatalf("activate: want 200, got %d (%v)", status, data)
		}

		if got := api.sseStopGateRefusal("kip"); got != "" {
			t.Fatalf("an activated member must be admitted, got %q", got)
		}
	})

	t.Run("a dismissed member is refused with the roster wording instead", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: want 200, got %d (%v)", status, data)
		}

		if got := api.sseStopGateRefusal("kip"); got != "member 'kip' is removed from the roster — "+
			"SSE refused (a dismissed member must not re-project online)" {
			t.Fatalf("want the roster refusal, got %q", got)
		}
	})

	t.Run("an outsource worker keeps the pre-fold admission even once its row is released", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)

		if got := api.sseStopGateRefusal("ow-abc123"); got != "" {
			t.Fatalf("a released worker must be admitted, got %q", got)
		}
	})
}

func TestOnFirstConnect(t *testing.T) {
	t.Run("the waking anchor the member reported is spent and the session is anchored in both stores", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/self/waking", agent, ""); status != 200 {
			t.Fatalf("report waking: want 200, got %d (%v)", status, data)
		}
		if m := infraTestMember(t, d, "kip"); m.WakingSince <= 0 {
			t.Fatalf("premise: the waking anchor must stand, got %v", m.WakingSince)
		}
		dashboard := apiTestListen(t, api, "")

		api.onFirstConnect("kip")

		m := infraTestMember(t, d, "kip")
		if m.WakingSince != 0 {
			t.Fatalf("the waking anchor must be spent, got %v", m.WakingSince)
		}
		if m.SessionBootTS <= 0 {
			t.Fatalf("the session must be anchored durably, got %v", m.SessionBootTS)
		}
		if got := api.gauge.Get("kip")["boot_ts"]; got != m.SessionBootTS {
			t.Fatalf("the gauge anchor %v must agree with the durable %v", got, m.SessionBootTS)
		}
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

	t.Run("a member with no waking anchor is anchored without a roster write of any kind", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := infraTestMember(t, d, "kip")
		dashboard := apiTestListen(t, api, "")

		api.onFirstConnect("kip")

		after := infraTestMember(t, d, "kip")
		if after.SessionBootTS <= 0 {
			t.Fatalf("the session must be anchored durably, got %v", after.SessionBootTS)
		}
		before.SessionBootTS = after.SessionBootTS
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("nothing else may move:\n got %+v\nwant %+v", after, before)
		}
		dashboard.wantFrames()
	})

	t.Run("a reconnect mid-session leaves the anchor exactly where the first connect put it", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.onFirstConnect("kip")
		anchored := infraTestMember(t, d, "kip").SessionBootTS

		api.onFirstConnect("kip")

		if got := infraTestMember(t, d, "kip").SessionBootTS; got != anchored {
			t.Fatalf("the anchor moved from %v to %v", anchored, got)
		}
		if got := api.gauge.Get("kip")["boot_ts"]; got != anchored {
			t.Fatalf("the gauge anchor moved to %v, want %v", got, anchored)
		}
	})

	t.Run("a worker's connect fans the presence delta the owner's worker list converges on", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		dashboard := apiTestListen(t, api, "")

		api.onFirstConnect("ow-abc123")

		if got := infraTestMember(t, d, "ow-abc123").SessionBootTS; got <= 0 {
			t.Fatalf("the worker's session must be anchored, got %v", got)
		}
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "server"))
	})
}

func TestAnchorSessionBoot(t *testing.T) {
	t.Run("a session nobody has anchored yet is stamped in both stores at once", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		api.anchorSessionBoot("kip")

		anchored := infraTestMember(t, d, "kip").SessionBootTS
		if anchored <= 0 {
			t.Fatalf("the durable anchor must be stamped, got %v", anchored)
		}
		if got := api.gauge.Get("kip")["boot_ts"]; got != anchored {
			t.Fatalf("the gauge anchor %v must be the durable one %v", got, anchored)
		}
	})

	t.Run("a station re-exec that emptied the gauge restores the anchor from the roster rather than minting a new one", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.anchorSessionBoot("kip")
		anchored := infraTestMember(t, d, "kip").SessionBootTS
		api.gauge.Delete("kip")

		api.anchorSessionBoot("kip")

		if got := api.gauge.Get("kip")["boot_ts"]; got != anchored {
			t.Fatalf("the restored gauge anchor is %v, want the durable %v", got, anchored)
		}
		if got := infraTestMember(t, d, "kip").SessionBootTS; got != anchored {
			t.Fatalf("the durable anchor moved to %v, want %v", got, anchored)
		}
	})

	t.Run("a gauge anchor with no durable twin is adopted, never overwritten with now", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.gauge.Set("kip", map[string]any{"boot_ts": 1700000000.0, "context_pct": 12.0})

		api.anchorSessionBoot("kip")

		if got := api.gauge.Get("kip")["boot_ts"]; got != 1700000000.0 {
			t.Fatalf("the gauge anchor must be left where it was, got %v", got)
		}
		if got := infraTestMember(t, d, "kip").SessionBootTS; got != 1700000000.0 {
			t.Fatalf("the durable anchor must adopt the gauge's, got %v", got)
		}
		if got := api.gauge.Get("kip")["context_pct"]; got != 12.0 {
			t.Fatalf("the rest of the gauge entry must survive, context_pct = %v", got)
		}
	})

	t.Run("an id the roster does not know is anchored in the gauge alone, and a second call leaves it there", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		api.anchorSessionBoot("ghost")

		anchored, ok := api.gauge.Get("ghost")["boot_ts"].(float64)
		if !ok || anchored <= 0 {
			t.Fatalf("the gauge anchor must be stamped, got %v", api.gauge.Get("ghost"))
		}
		row, err := d.GetMember("ghost")
		if err != nil || row != nil {
			t.Fatalf("no roster row may be created, got %v (%v)", row, err)
		}

		api.anchorSessionBoot("ghost")

		if got := api.gauge.Get("ghost")["boot_ts"]; got != anchored {
			t.Fatalf("the anchor moved from %v to %v", anchored, got)
		}
	})

	t.Run("an anchor that is already in agreement is left alone, and nothing is fanned to anybody", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.anchorSessionBoot("kip")
		anchored := infraTestMember(t, d, "kip").SessionBootTS
		dashboard := apiTestListen(t, api, "")

		api.anchorSessionBoot("kip")

		if got := api.gauge.Get("kip")["boot_ts"]; got != anchored {
			t.Fatalf("the gauge anchor moved to %v, want %v", got, anchored)
		}
		dashboard.wantFrames()
	})
}

func TestStampLandedMachine(t *testing.T) {
	t.Run("a connection from the machine the member is pinned to records the landing and fans the roster delta", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if got := infraTestMember(t, d, "mira").LastMachineID; got != "" {
			t.Fatalf("premise: nothing may be recorded yet, got %q", got)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampLandedMachine("mira", ServerSelfHost)

		if got := infraTestMember(t, d, "mira").LastMachineID; got != ServerSelfHost {
			t.Fatalf("want the landing recorded, got %q", got)
		}
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::mira",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "mira",
					"name":          "Mira",
					"owner_id":      "owner",
					"status":        "active",
					"desired_state": "offline",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		})
	})

	t.Run("a second connection from the same machine costs neither a write nor a delta", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.stampLandedMachine("mira", ServerSelfHost)
		before := infraTestMember(t, d, "mira")
		dashboard := apiTestListen(t, api, "")

		api.stampLandedMachine("mira", ServerSelfHost)

		if !reflect.DeepEqual(infraTestMember(t, d, "mira"), before) {
			t.Fatalf("the row must be untouched:\n got %+v\nwant %+v", infraTestMember(t, d, "mira"), before)
		}
		dashboard.wantFrames()
	})

	t.Run("a claim from anywhere but the pinned machine leaves the known landing alone", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.stampLandedMachine("mira", ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.stampLandedMachine("mira", "m-elsewhere")

		if got := infraTestMember(t, d, "mira").LastMachineID; got != ServerSelfHost {
			t.Fatalf("a wanderer must not rewrite the landing, got %q", got)
		}
		dashboard.wantFrames()
	})

	t.Run("a claimless connection and an id the roster does not know both record nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		api.stampLandedMachine("mira", "")
		api.stampLandedMachine("ghost", ServerSelfHost)

		if got := infraTestMember(t, d, "mira").LastMachineID; got != "" {
			t.Fatalf("a blank claim must record nothing, got %q", got)
		}
		row, err := d.GetMember("ghost")
		if err != nil || row != nil {
			t.Fatalf("no roster row may be created, got %v (%v)", row, err)
		}
		dashboard.wantFrames()
	})

	t.Run("a member with no pin at all is left alone, because nothing can confirm the claim", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if got := infraTestMember(t, d, "kip").DesiredMachineID; got != "" {
			t.Fatalf("premise: kip must carry no pin, got %q", got)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampLandedMachine("kip", ServerSelfHost)

		if got := infraTestMember(t, d, "kip").LastMachineID; got != "" {
			t.Fatalf("an unverifiable connection must record nothing, got %q", got)
		}
		dashboard.wantFrames()
	})
}

func TestClearSessionBootTS(t *testing.T) {
	t.Run("the session-scoped gauge state and both durable session columns go, while the report itself stays", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent,
			`{"context_pct":45,"compaction_count":3}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		if err := d.SetMemberHandoverNoticedTS("kip", 1700000007); err != nil {
			t.Fatalf("SetMemberHandoverNoticedTS: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.clearSessionBootTS("kip")

		infraWantGaugeKeys(t, api, "kip", "rate_limits", "ts")
		m := infraTestMember(t, d, "kip")
		if m.SessionBootTS != 0 {
			t.Fatalf("the durable anchor must be zeroed, got %v", m.SessionBootTS)
		}
		if m.HandoverNoticedTS != 0 {
			t.Fatalf("the notice claim must be zeroed, got %v", m.HandoverNoticedTS)
		}
		dashboard.wantFrames()
	})

	t.Run("clearing again is a clean no-op, and so is clearing an id with no gauge entry and no roster row", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		api.clearSessionBootTS("kip")
		before := infraTestMember(t, d, "kip")

		api.clearSessionBootTS("kip")
		api.clearSessionBootTS("ghost")

		if !reflect.DeepEqual(infraTestMember(t, d, "kip"), before) {
			t.Fatalf("the row must be untouched:\n got %+v\nwant %+v", infraTestMember(t, d, "kip"), before)
		}
		infraWantGaugeKeys(t, api, "kip", "rate_limits", "ts")
		if entry := api.gauge.Get("ghost"); entry != nil {
			t.Fatalf("no gauge entry may be created, got %v", entry)
		}
		row, err := d.GetMember("ghost")
		if err != nil || row != nil {
			t.Fatalf("no roster row may be created, got %v (%v)", row, err)
		}
	})

	t.Run("the next connect after a boundary mints a fresh anchor rather than inheriting the old one", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.onFirstConnect("kip")
		first := infraTestMember(t, d, "kip").SessionBootTS

		api.clearSessionBootTS("kip")
		api.onFirstConnect("kip")

		second := infraTestMember(t, d, "kip").SessionBootTS
		if second <= first {
			t.Fatalf("the new session must anchor later than %v, got %v", first, second)
		}
		if got := api.gauge.Get("kip")["boot_ts"]; got != second {
			t.Fatalf("the gauge anchor %v must be the durable one %v", got, second)
		}
	})
}

func TestOnLastDisconnect(t *testing.T) {
	t.Run("a member's live figure is folded into its durable banked figure and the live field is popped", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		api.onLastDisconnect("kip")

		apiWantValue(t, "session", any(apiTestSession(t, h, owner, "kip")), map[string]any{
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

	t.Run("a worker's figure banks on the same edge and its presence delta is what the owner's list is given", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		worker := apiTestAgentToken(t, api, "ow-abc123", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", worker, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		api.onLastDisconnect("ow-abc123")

		if got := infraTestMember(t, d, "ow-abc123").BankedCost; got != 2.5 {
			t.Fatalf("want the worker's 2.5 banked, got %v", got)
		}
		if _, present := api.telemetry.Get("ow-abc123")["cost"]; present {
			t.Fatalf("the live figure must be popped, entry = %v", api.telemetry.Get("ow-abc123"))
		}
		dashboard.wantFrames(apiTestWorkerDelta(3, "active", "server"))
	})

	t.Run("an edge that fires again banks nothing a second time", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		api.onLastDisconnect("kip")

		api.onLastDisconnect("kip")

		if row := apiTestSession(t, h, owner, "kip"); row["banked_cost"] != 2.5 {
			t.Fatalf("want 2.5 banked exactly once, got %v", row["banked_cost"])
		}
	})
}

func TestPublishOutsourcePresenceEdge(t *testing.T) {
	t.Run("a live worker's edge fans the canonical worker delta to the owner cockpit alone", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		api.publishOutsourcePresenceEdge("ow-abc123")

		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "server"))
		bystander.wantFrames()
	})

	t.Run("a released worker, a staff id and an id nobody carries are all silent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		api.publishOutsourcePresenceEdge("ow-abc123")
		api.publishOutsourcePresenceEdge("kip")
		api.publishOutsourcePresenceEdge("ghost")

		dashboard.wantFrames()
	})
}

func TestBankLiveCost(t *testing.T) {
	t.Run("a staff member's live figure moves into the durable column and the fold fans the member delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		subject := apiTestListen(t, api, "kip")

		api.bankLiveCost("kip")

		apiWantValue(t, "session", any(apiTestSession(t, h, owner, "kip")), map[string]any{
			"id":          "kip",
			"name":        "Kip",
			"presence":    "online",
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
			"trigger": "kip",
		}
		dashboard.wantFrames(memberFrame)
		subject.wantFrames(memberFrame)
	})

	t.Run("a second fold on the same actor adds nothing, because the live figure is popped", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		api.bankLiveCost("kip")
		dashboard := apiTestListen(t, api, "")

		api.bankLiveCost("kip")

		if row := apiTestSession(t, h, owner, "kip"); row["banked_cost"] != 2.5 {
			t.Fatalf("want 2.5 banked exactly once, got %v", row["banked_cost"])
		}
		dashboard.wantFrames()
	})

	t.Run("two sessions' figures accumulate in the one durable column", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		api.bankLiveCost("kip")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":1.25}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		api.bankLiveCost("kip")

		if row := apiTestSession(t, h, owner, "kip"); row["banked_cost"] != 3.75 {
			t.Fatalf("want 3.75 banked, got %v", row["banked_cost"])
		}
	})

	t.Run("a worker banks into the same column without a member delta naming its id", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		worker := apiTestAgentToken(t, api, "ow-abc123", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", worker, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		api.bankLiveCost("ow-abc123")

		if got := infraTestMember(t, d, "ow-abc123").BankedCost; got != 2.5 {
			t.Fatalf("want 2.5 banked, got %v", got)
		}
		if _, present := api.telemetry.Get("ow-abc123")["cost"]; present {
			t.Fatalf("the live figure must be popped, entry = %v", api.telemetry.Get("ow-abc123"))
		}
		dashboard.wantFrames()
	})

	t.Run("an id that resolves to neither kind keeps its live figure rather than destroying it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.telemetry.Set("ghost", map[string]any{"cost": 2.5})
		dashboard := apiTestListen(t, api, "")

		api.bankLiveCost("ghost")

		if got := api.telemetry.Get("ghost")["cost"]; got != 2.5 {
			t.Fatalf("the live figure must survive, got %v", got)
		}
		dashboard.wantFrames()
	})

	t.Run("an actor with nothing live banks nothing and writes nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := infraTestMember(t, d, "kip")
		dashboard := apiTestListen(t, api, "")

		api.bankLiveCost("kip")

		if !reflect.DeepEqual(infraTestMember(t, d, "kip"), before) {
			t.Fatalf("the row must be untouched:\n got %+v\nwant %+v", infraTestMember(t, d, "kip"), before)
		}
		dashboard.wantFrames()
	})
}

func TestDropLiveCost(t *testing.T) {
	t.Run("the live figure is removed and reported back, leaving the rest of the entry standing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		got := api.dropLiveCost("kip")

		if got == nil || *got != 2.5 {
			t.Fatalf("want the 2.5 it removed, got %v", got)
		}
		if _, present := api.telemetry.Get("kip")["cost"]; present {
			t.Fatalf("the live figure must be gone, entry = %v", api.telemetry.Get("kip"))
		}
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
			"runtime":     "claude",
			"tokens":      nil,
		})
	})

	t.Run("an actor whose entry carries no live figure reports nothing removed", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		if got := api.dropLiveCost("kip"); got != nil {
			t.Fatalf("want nil, got %v", *got)
		}
		if account := api.telemetry.Get("kip")["account"]; account != "acct/x" {
			t.Fatalf("the rest of the entry must survive, account = %v", account)
		}
	})

	t.Run("an actor with no entry at all reports nothing removed", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		if got := api.dropLiveCost("ghost"); got != nil {
			t.Fatalf("want nil, got %v", *got)
		}
		if entry := api.telemetry.Get("ghost"); entry != nil {
			t.Fatalf("no entry may be created, got %v", entry)
		}
	})

	t.Run("dropping twice reports the figure once", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		first := api.dropLiveCost("kip")
		second := api.dropLiveCost("kip")

		if first == nil || *first != 2.5 {
			t.Fatalf("first drop: want 2.5, got %v", first)
		}
		if second != nil {
			t.Fatalf("second drop: want nil, got %v", *second)
		}
	})
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
	t.Run("only the increase over the last credited report is added to the account card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		for _, report := range []string{
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`,
			`{"cost":4,"account":"acct/x","runtime":"claude"}`,
		} {
			if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, report); status != 200 {
				t.Fatalf("telemetry %s: want 200, got %d (%v)", report, status, data)
			}
		}

		apiWantValue(t, "account", any(apiTestAccount(t, h, owner, "acct/x")), map[string]any{
			"account":      "acct/x",
			"display_name": "acct/x",
			"cost":         4.0,
			"machine":      "",
			"five_hour":    nil,
			"seven_day":    nil,
		})
	})

	t.Run("a report lower than the last is a session counting from zero, so its whole figure is new spend", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		for _, report := range []string{
			`{"cost":4,"account":"acct/x","runtime":"claude"}`,
			`{"cost":1,"account":"acct/x","runtime":"claude"}`,
		} {
			if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, report); status != 200 {
				t.Fatalf("telemetry %s: want 200, got %d (%v)", report, status, data)
			}
		}

		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 5.0 {
			t.Fatalf("want 5 on the card, got %v", row["cost"])
		}
	})

	t.Run("re-reporting the same cumulative figure credits nothing twice", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		for i := 0; i < 3; i++ {
			if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
				`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
				t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
			}
		}

		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("want 2.5 on the card, got %v", row["cost"])
		}
	})

	t.Run("the credit follows the account the report named, and a reporter that names none makes no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		nameless := apiTestAgentToken(t, api, "mira", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", nameless, `{"cost":9}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("the named account must keep its 2.5, got %v", row["cost"])
		}
		if row := apiTestAccount(t, h, owner, ""); row != nil {
			t.Fatalf("an unnamed account must not become a card, got %v", row)
		}
	})

	t.Run("an entry the ingest never gave a numeric cost credits nothing at all", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		api.accrueAccountSpend(map[string]any{"account": "acct/x"})
		api.accrueAccountSpend(map[string]any{"cost": 2.5})

		if row := apiTestAccount(t, h, owner, "acct/x"); row != nil {
			t.Fatalf("no card may be created, got %v", row)
		}
	})
}

func TestStartAccountSpendSession(t *testing.T) {
	t.Run("the next report after a session start is credited whole rather than as an increase", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		api.startAccountSpendSession("kip")

		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 5.0 {
			t.Fatalf("want 5 on the card, got %v", row["cost"])
		}
	})

	t.Run("without the boundary the same repeated figure is credited once", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		for i := 0; i < 2; i++ {
			if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
				`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
				t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
			}
		}

		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 2.5 {
			t.Fatalf("want 2.5 on the card, got %v", row["cost"])
		}
	})

	t.Run("the boundary a waking report announces is the one the member's own route reaches", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}

		if status, data := apiJSON(t, h, "POST", "/api/self/waking", agent, ""); status != 200 {
			t.Fatalf("report waking: want 200, got %d (%v)", status, data)
		}

		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"cost":2.5,"account":"acct/x","runtime":"claude"}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		if row := apiTestAccount(t, h, owner, "acct/x"); row["cost"] != 5.0 {
			t.Fatalf("want 5 on the card, got %v", row["cost"])
		}
	})

	t.Run("an actor with no baseline to forget keeps its entry and its live figure exactly as they were", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2.5}`); status != 200 {
			t.Fatalf("telemetry: want 200, got %d (%v)", status, data)
		}
		before := api.telemetry.Get("kip")

		api.startAccountSpendSession("kip")
		api.startAccountSpendSession("ghost")

		if !reflect.DeepEqual(api.telemetry.Get("kip"), before) {
			t.Fatalf("the entry must be untouched:\n got %v\nwant %v", api.telemetry.Get("kip"), before)
		}
		if entry := api.telemetry.Get("ghost"); entry != nil {
			t.Fatalf("no entry may be created, got %v", entry)
		}
		if row := apiTestSession(t, h, owner, "kip"); row["cost"] != 2.5 {
			t.Fatalf("the live figure must survive, got %v", row["cost"])
		}
	})
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
	t.Run("zero is not put on the wire at all", func(t *testing.T) {
		if got := nonZeroCost(0); got != nil {
			t.Fatalf("want nil, got %v", *got)
		}
	})

	t.Run("any other figure comes back as the value itself", func(t *testing.T) {
		for _, want := range []float64{2.5, 0.0001, -3} {
			got := nonZeroCost(want)
			if got == nil {
				t.Fatalf("nonZeroCost(%v): want the figure, got nil", want)
			}
			if *got != want {
				t.Fatalf("nonZeroCost(%v) = %v", want, *got)
			}
		}
	})
}

func TestPublishMonitoringSignal(t *testing.T) {
	t.Run("the invalidation is fanned to the owner cockpit alone, naming the actor and the trigger", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		subject := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.publishMonitoringSignal("kip", "owner")

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "kip",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		subject.wantFrames()
		bystander.wantFrames()
	})

	t.Run("an account tag is fanned under the same signal, and two calls are two frames", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		api.publishMonitoringSignal("acct/x", triggerServer)
		api.publishMonitoringSignal("acct/x", "kip")

		dashboard.wantFrames(map[string]any{
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
			"trigger": "server",
		}, map[string]any{
			"seq":   2,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "acct/x",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		})
	})
}

func TestRpcError(t *testing.T) {
	t.Run("the error envelope is written as a 200 JSON body carrying the id, the code and the message", func(t *testing.T) {
		w := httptest.NewRecorder()

		rpcError(w, 7, rpcInvalidRequest, "invalid request: method must be a string")

		if w.Code != 200 {
			t.Fatalf("want 200, got %d", w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("want application/json, got %q", got)
		}
		if got := w.Body.String(); got != `{"error":{"code":-32600,"message":"invalid request: method must be a string"},"id":7,"jsonrpc":"2.0"}` {
			t.Fatalf("body = %s", got)
		}
	})

	t.Run("an id the caller never sent is written as null rather than dropped", func(t *testing.T) {
		w := httptest.NewRecorder()

		rpcError(w, nil, rpcParseError, "parse error: body is not valid JSON")

		if got := w.Body.String(); got != `{"error":{"code":-32700,"message":"parse error: body is not valid JSON"},"id":null,"jsonrpc":"2.0"}` {
			t.Fatalf("body = %s", got)
		}
	})
}

func TestRpcResult(t *testing.T) {
	t.Run("the result envelope is written as a 200 JSON body carrying the id and the result", func(t *testing.T) {
		w := httptest.NewRecorder()

		rpcResult(w, "a", map[string]any{"protocolVersion": mcpProtocolVersion})

		if w.Code != 200 {
			t.Fatalf("want 200, got %d", w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("want application/json, got %q", got)
		}
		if got := w.Body.String(); got != `{"id":"a","jsonrpc":"2.0","result":{"protocolVersion":"2025-06-18"}}` {
			t.Fatalf("body = %s", got)
		}
	})

	t.Run("an empty result is written as an empty object, not as null", func(t *testing.T) {
		w := httptest.NewRecorder()

		rpcResult(w, 2, map[string]any{})

		if got := w.Body.String(); got != `{"id":2,"jsonrpc":"2.0","result":{}}` {
			t.Fatalf("body = %s", got)
		}
	})
}

func TestMcpCatalogTools(t *testing.T) {
	t.Run("the frozen catalog is served whole, every descriptor carrying exactly a name, a description and an input schema", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		tools, err := api.mcpCatalogTools()

		if err != nil {
			t.Fatalf("mcpCatalogTools: %v", err)
		}
		if len(tools) != 127 {
			t.Fatalf("want the frozen catalog's 127 descriptors, got %d", len(tools))
		}
		names := []string{}
		seen := map[string]bool{}
		for i, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("descriptor %d is not an object: %#v", i, raw)
			}
			apiWantValue(t, fmt.Sprintf("tool[%d]", i), any(tool), map[string]any{
				"name":        apiAnyString,
				"description": apiAnyString,
				"inputSchema": tool["inputSchema"],
			})
			schema, ok := tool["inputSchema"].(map[string]any)
			if !ok || schema["type"] != "object" {
				t.Fatalf("descriptor %d has no object input schema: %#v", i, tool["inputSchema"])
			}
			name, _ := tool["name"].(string)
			if seen[name] {
				t.Fatalf("%q is listed twice", name)
			}
			seen[name] = true
			names = append(names, name)
		}
		if names[0] != "get_version" || names[len(names)-1] != "replace_task_artifact" {
			t.Fatalf("the catalog order moved: first %q, last %q", names[0], names[len(names)-1])
		}
		apiWantValue(t, "get_version", tools[0], map[string]any{
			"name": "get_version",
			"description": "Read the build identity this station is RUNNING: version, git sha, git time and the MCP catalog hash, " +
				"plus the cached update status and `update_checked_ok_at`, the time that update check last SUCCEEDED " +
				"(absent = it never has, so `update_available: false` is not evidence of being up to date). " +
				"Settle whether something is deployed by git sha ancestry, never by the version string.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		})
	})

	t.Run("the descriptors are the same list tools/list serves", func(t *testing.T) {
		api, h, owner := apiTestMCPServer(t)

		tools, err := api.mcpCatalogTools()
		if err != nil {
			t.Fatalf("mcpCatalogTools: %v", err)
		}
		status, data := apiMCP(t, h, owner, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		result, _ := data["result"].(map[string]any)
		apiWantValue(t, "tools/list", result["tools"], tools)
	})
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
	t.Run("a session below its notice point stays quiet and never runs the notice source", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":10}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		calls := 0

		frame, ok := api.handoverNoticeTick("kip", "claude", func() string {
			calls++
			return "close out and hand over"
		})

		if ok || frame != nil {
			t.Fatalf("want a quiet tick, got %q", frame)
		}
		if calls != 0 {
			t.Fatalf("the notice source ran %d times", calls)
		}
	})

	t.Run("past the notice point the tick reports the directed context-high frame once, and every later tick is quiet without composing anything", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		calls := 0
		notice := func() string {
			calls++
			return "close out and hand over"
		}

		frame, ok := api.handoverNoticeTick("kip", "claude", notice)

		if !ok {
			t.Fatalf("want a frame, got quiet")
		}
		if got := string(frame); got != `data: {"topic":"context-high","data":{"topic":"context-high","to":"kip",`+
			`"level":"warn","pct":45.0,"reason":"close out and hand over"}}`+"\n\n" {
			t.Fatalf("frame = %q", got)
		}
		anchor := infraTestMember(t, d, "kip").SessionBootTS
		if got := infraTestMember(t, d, "kip").HandoverNoticedTS; got != anchor {
			t.Fatalf("the claim must name this session's anchor %v, got %v", anchor, got)
		}
		if calls != 1 {
			t.Fatalf("the notice source ran %d times, want 1", calls)
		}

		again, ok := api.handoverNoticeTick("kip", "claude", notice)

		if ok || again != nil {
			t.Fatalf("the session's one notice is spent, got %q", again)
		}
		if calls != 1 {
			t.Fatalf("the notice source ran %d times after the claim, want 1", calls)
		}
	})

	t.Run("a new session after a real boundary is entitled to its own notice", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		notice := func() string { return "close out and hand over" }
		if _, ok := api.handoverNoticeTick("kip", "claude", notice); !ok {
			t.Fatalf("premise: the first session must be told")
		}

		api.clearSessionBootTS("kip")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}

		if _, ok := api.handoverNoticeTick("kip", "claude", notice); !ok {
			t.Fatalf("a fresh session must be told too")
		}
	})

	t.Run("a member already winding down is told nothing, and its notice is not spent on the silence", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, ""); status != 200 {
			t.Fatalf("force-stop: want 200, got %d (%v)", status, data)
		}
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		calls := 0
		notice := func() string {
			calls++
			return "close out and hand over"
		}

		frame, ok := api.handoverNoticeTick("kip", "claude", notice)

		if ok || frame != nil {
			t.Fatalf("want silence, got %q", frame)
		}
		if calls != 0 {
			t.Fatalf("the notice source ran %d times", calls)
		}

		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, ""); status != 200 {
			t.Fatalf("activate: want 200, got %d (%v)", status, data)
		}
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}

		if _, ok := api.handoverNoticeTick("kip", "claude", notice); !ok {
			t.Fatalf("a cleared wind-down must leave the notice unspent")
		}
	})

	t.Run("a session with no anchor at all is quiet, because a notice off an unrecognisable anchor could never be claimed", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}
		calls := 0

		frame, ok := api.handoverNoticeTick("kip", "claude", func() string {
			calls++
			return "close out and hand over"
		})

		if ok || frame != nil {
			t.Fatalf("want silence, got %q", frame)
		}
		if calls != 0 {
			t.Fatalf("the notice source ran %d times", calls)
		}
	})

	t.Run("a notice source with nothing to say leaves the session's one notice unspent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		api.onFirstConnect("kip")
		if status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":45}`); status != 200 {
			t.Fatalf("context ingest: want 200, got %d (%v)", status, data)
		}

		frame, ok := api.handoverNoticeTick("kip", "claude", func() string { return "" })

		if ok || frame != nil {
			t.Fatalf("want silence, got %q", frame)
		}
		if _, ok := api.handoverNoticeTick("kip", "claude", func() string { return "close out and hand over" }); !ok {
			t.Fatalf("the notice must still be available")
		}
	})
}

// infraTestMember reads one roster row back, for the durable half of an edge
// whose other half is in memory.
func infraTestMember(t *testing.T, d *DAL, id string) Member {
	t.Helper()
	m, err := d.GetMember(id)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", id, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%q): no row", id)
	}
	return *m
}

// infraWantGaugeKeys asserts an actor's in-memory gauge entry carries exactly
// these keys — the surface a session boundary adds to and takes from.
func infraWantGaugeKeys(t *testing.T, api *apiServer, id string, want ...string) {
	t.Helper()
	got := []string{}
	for key := range api.gauge.Get(id) {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gauge %q keys: want %v, got %v", id, want, got)
	}
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
