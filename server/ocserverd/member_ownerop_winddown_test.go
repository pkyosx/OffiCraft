package main

import "testing"

func TestMemberHasStateToFlush(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		desired string
		refocus float64
		stopped float64
		online  bool
		want    bool
	}{
		{"a live staff session with no collected stop has state to flush", KindStaff, DesiredStateOnline, 0, 0, true, true},
		{"an offline staff session has no state to flush", KindStaff, DesiredStateOnline, 0, 0, false, false},
		{"an offline desired staff member has no state to flush", KindStaff, DesiredStateOffline, 0, 0, true, false},
		{"a staff member whose epoch was already collected has no state to flush", KindStaff, DesiredStateOnline, 10, 20, true, false},
		{"a stopped latch without a refocus epoch still has state to flush", KindStaff, DesiredStateOnline, 0, 20, true, true},
		{"a live outsource row has no staff state to flush", KindOutsource, DesiredStateOnline, 0, 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _, _ := newAPITestServer(t)
			m := Member{
				ID: apiTestPlainAgentID, Kind: tc.kind, DesiredState: tc.desired,
				RefocusSince: tc.refocus, StoppedSince: tc.stopped,
			}
			var listener *apiTestListener
			if tc.online {
				listener = apiTestListen(t, api, m.ID)
			}
			if got := api.memberHasStateToFlush(m); got != tc.want {
				t.Fatalf("memberHasStateToFlush() = %t, want %t", got, tc.want)
			}
			if listener != nil {
				listener.wantFrames()
			}
		})
	}
}

func TestWinddownKindFor(t *testing.T) {
	cases := []struct {
		name        string
		op          string
		wantKind    string
		wantClocked bool
	}{
		{"context-high wind-down is final and clocked", refocusOpContextHigh, "final", true},
		{"context-notice wind-down is soft and unclocked", refocusOpContextNotice, "soft", false},
		{"manual refocus wind-down is soft and unclocked", refocusOpRefocus, "soft", false},
		{"restart-self wind-down is soft and unclocked", refocusOpRestartSelf, "soft", false},
		{"token-expiry wind-down is soft and unclocked", refocusOpTokenExpiry, "soft", false},
		{"accelerated-stop wind-down is final and clocked", refocusOpAcceleratedStop, "final", true},
		{"relocate wind-down is soft and unclocked", memberOpRelocate, "soft", false},
		{"model wind-down is soft and unclocked", memberOpModel, "soft", false},
		{"an unknown wind-down is soft and unclocked", "unknown", "soft", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, clocked := winddownKindFor(tc.op)
			if kind != tc.wantKind || clocked != tc.wantClocked {
				t.Fatalf("winddownKindFor(%q) = (%q, %t), want (%q, %t)",
					tc.op, kind, clocked, tc.wantKind, tc.wantClocked)
			}
		})
	}
}

func TestArmRefocusEpoch(t *testing.T) {
	cases := []struct {
		name string
		m    Member
		op   string
		want bool
		row  Member
	}{
		{
			"opens a fresh soft epoch and clears old anchors",
			Member{ID: "m-soft", Kind: KindStaff, DesiredState: DesiredStateOnline,
				StoppingSince: 11, StoppedSince: 12, RefocusSince: 13, RefocusOp: "old"},
			memberOpRelocate,
			true,
			Member{ID: "m-soft", Kind: KindStaff, DesiredState: DesiredStateOnline,
				RefocusSince: 1234.5, RefocusOp: memberOpRelocate},
		},
		{
			"rearms an equal-stage epoch and clears old anchors",
			Member{ID: "m-same", Kind: KindStaff, DesiredState: DesiredStateOnline,
				StoppingSince: 11, StoppedSince: 12, RefocusSince: 13, RefocusOp: memberOpRelocate},
			memberOpModel,
			true,
			Member{ID: "m-same", Kind: KindStaff, DesiredState: DesiredStateOnline,
				RefocusSince: 1234.5, RefocusOp: memberOpModel},
		},
		{
			"refuses a lower-stage epoch and preserves the whole row",
			Member{ID: "m-final", Kind: KindStaff, DesiredState: DesiredStateOnline,
				StoppingSince: 11, StoppedSince: 12, RefocusSince: 13, RefocusOp: refocusOpContextHigh},
			memberOpRelocate,
			false,
			Member{ID: "m-final", Kind: KindStaff, DesiredState: DesiredStateOnline,
				StoppingSince: 11, StoppedSince: 12, RefocusSince: 13, RefocusOp: refocusOpContextHigh},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.m
			if got := armRefocusEpoch(&m, tc.op, 1234.5); got != tc.want {
				t.Fatalf("armRefocusEpoch() = %t, want %t", got, tc.want)
			}
			apiTestWantEqual(t, "member after armRefocusEpoch", m, tc.row)
		})
	}
}

func TestWinddownStageRankOf(t *testing.T) {
	cases := []struct {
		name string
		op   string
		want int
	}{
		{"context-high is the accelerated stage", refocusOpContextHigh, 2},
		{"accelerated-stop is the accelerated stage", refocusOpAcceleratedStop, 2},
		{"context-notice is the stop stage", refocusOpContextNotice, 1},
		{"refocus is the stop stage", refocusOpRefocus, 1},
		{"restart-self is the stop stage", refocusOpRestartSelf, 1},
		{"token-expiry is the stop stage", refocusOpTokenExpiry, 1},
		{"relocate is the stop stage", memberOpRelocate, 1},
		{"model is the stop stage", memberOpModel, 1},
		{"an unknown operation is the stop stage", "unknown", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := winddownStageRankOf(tc.op); got != tc.want {
				t.Fatalf("winddownStageRankOf(%q) = %d, want %d", tc.op, got, tc.want)
			}
		})
	}
}

func TestWinddownStageOf(t *testing.T) {
	cases := []struct {
		name string
		m    Member
		want int
	}{
		{"a member with no epoch is at stage zero", Member{ID: "m-none"}, 0},
		{"a soft epoch is at stage one", Member{ID: "m-soft", RefocusSince: 10, RefocusOp: memberOpRelocate}, 1},
		{"an accelerated epoch is at stage two", Member{ID: "m-accelerated", RefocusSince: 10, RefocusOp: refocusOpContextHigh}, 2},
		{"a live forced epoch is at stage three", Member{ID: "m-forced", StoppingSince: 20, ForcedStopAt: 30}, 3},
		{"an old force record without stopping is not an open stage", Member{ID: "m-old-force", ForcedStopAt: 30}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := winddownStageOf(tc.m); got != tc.want {
				t.Fatalf("winddownStageOf() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMemberOwnerOpHandoverArmable(t *testing.T) {
	cases := []struct {
		name      string
		kind      string
		desired   string
		online    bool
		refocus   float64
		refocusOp string
		stopped   float64
		op        string
		want      bool
	}{
		{"a live staff row is armable for relocate", KindStaff, DesiredStateOnline, true, 0, "", 0, memberOpRelocate, true},
		{"an offline session is not armable", KindStaff, DesiredStateOnline, false, 0, "", 0, memberOpRelocate, false},
		{"an already collected epoch is not armable", KindStaff, DesiredStateOnline, true, 10, "", 20, memberOpRelocate, false},
		{"an offline desired row is not armable", KindStaff, DesiredStateOffline, true, 0, "", 0, memberOpRelocate, false},
		{"a lower-stage operation is not armable", KindStaff, DesiredStateOnline, true, 10, refocusOpContextHigh, 0, memberOpRelocate, false},
		{"a non-staff row is not armable", KindOutsource, DesiredStateOnline, true, 0, "", 0, memberOpRelocate, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, d, _ := newAPITestServer(t)
			m := apiTestMemberRow(t, d, apiTestPlainAgentID)
			m.Kind, m.DesiredState = tc.kind, tc.desired
			m.RefocusSince, m.RefocusOp, m.StoppedSince = tc.refocus, tc.refocusOp, tc.stopped
			var listener *apiTestListener
			if tc.online {
				listener = apiTestListen(t, api, m.ID)
			}
			before := m
			if got := api.memberOwnerOpHandoverArmable(m, tc.op); got != tc.want {
				t.Fatalf("memberOwnerOpHandoverArmable() = %t, want %t", got, tc.want)
			}
			apiTestWantEqual(t, "memberOwnerOpHandoverArmable input", m, before)
			if listener != nil {
				listener.wantFrames()
			}
		})
	}
}

func TestArmMemberOwnerOpHandover(t *testing.T) {
	cases := []struct {
		name      string
		desired   string
		online    bool
		refocus   float64
		refocusOp string
		op        string
		want      bool
	}{
		{"a live staff row gets a fresh epoch", DesiredStateOnline, true, 0, "", memberOpRelocate, true},
		{"an offline session is not armed", DesiredStateOnline, false, 0, "", memberOpRelocate, false},
		{"an offline desired row is not armed", DesiredStateOffline, true, 0, "", memberOpRelocate, false},
		{"a lower-stage operation is not armed", DesiredStateOnline, true, 10, refocusOpContextHigh, memberOpRelocate, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, d, _ := newAPITestServer(t)
			storedBefore := apiTestMemberRow(t, d, apiTestPlainAgentID)
			m := storedBefore
			m.DesiredState, m.RefocusSince, m.RefocusOp = tc.desired, tc.refocus, tc.refocusOp
			var listener *apiTestListener
			if tc.online {
				listener = apiTestListen(t, api, m.ID)
			}
			before := m
			lower := nowSecs()
			got := api.armMemberOwnerOpHandover(&m, tc.op)
			upper := nowSecs()
			if got != tc.want {
				t.Fatalf("armMemberOwnerOpHandover() = %t, want %t", got, tc.want)
			}
			if tc.want {
				if m.RefocusSince <= before.RefocusSince || m.RefocusSince < lower || m.RefocusSince > upper {
					t.Fatalf("fresh RefocusSince = %v, want (%v, %v] within [%v, %v]", m.RefocusSince, before.RefocusSince, upper, lower, upper)
				}
				want := before
				want.RefocusSince = m.RefocusSince
				want.RefocusOp = tc.op
				want.StoppingSince = 0
				want.StoppedSince = 0
				apiTestWantEqual(t, "member after armMemberOwnerOpHandover", m, want)
			} else {
				apiTestWantEqual(t, "member after refused armMemberOwnerOpHandover", m, before)
			}
			apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, m.ID), storedBefore)
			if listener != nil {
				listener.wantFrames()
			}
		})
	}
}

func TestStampRestartIntent(t *testing.T) {
	input := Member{
		ID: "m-stamp", DesiredState: DesiredStateOffline, RestartAfterStop: false,
		StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate,
	}
	stampRestartIntent(&input)
	want := Member{
		ID: "m-stamp", DesiredState: DesiredStateOffline, RestartAfterStop: true,
		StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate,
	}
	apiTestWantEqual(t, "member after stampRestartIntent", input, want)
}

func TestClearRestartIntent(t *testing.T) {
	cases := []struct {
		name string
		m    Member
		want Member
	}{
		{
			"clearing a queued restart preserves the wind-down row",
			Member{ID: "m-clear", DesiredState: DesiredStateOffline, RestartAfterStop: true,
				StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate},
			Member{ID: "m-clear", DesiredState: DesiredStateOffline,
				StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate},
		},
		{
			"clearing an absent restart stays clear",
			Member{ID: "m-clear", DesiredState: DesiredStateOffline,
				StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate},
			Member{ID: "m-clear", DesiredState: DesiredStateOffline,
				StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.m
			clearRestartIntent(&m)
			apiTestWantEqual(t, "member after clearRestartIntent", m, tc.want)
		})
	}
}

func TestMemberRestartQueuedReceipt(t *testing.T) {
	cases := []struct {
		name string
		op   string
		want string
	}{
		{"a refocus receipt names the saved operation", refocusOpRefocus, "held_down: the refocus was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down"},
		{"a relocate receipt names the saved operation", memberOpRelocate, "held_down: the relocate was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down"},
		{"a model receipt names the saved operation", memberOpModel, "held_down: the runtime/model was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down"},
		{"an empty operation is still represented literally", "", "held_down: the  was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memberRestartQueuedReceipt(tc.op); got != tc.want {
				t.Fatalf("memberRestartQueuedReceipt(%q) = %q, want %q", tc.op, got, tc.want)
			}
		})
	}
}

func TestConsumeRestartAfterStop(t *testing.T) {
	t.Run("a converged offline member is restarted and every changed field is stored", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, apiTestPlainAgentID)
		m := before
		m.DesiredState = DesiredStateOffline
		m.RestartAfterStop = true
		m.StoppingSince, m.StoppedSince = 20, 30
		m.RefocusSince, m.RefocusOp = 10, memberOpRelocate
		m.WakingSince = 40
		if err := d.PutMember(m); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		if err := api.persistMemberWindDownAnchors(m); err != nil {
			t.Fatalf("persistMemberWindDownAnchors: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		if got := api.consumeRestartAfterStop(&m, 1234.5); !got {
			t.Fatal("consumeRestartAfterStop() = false, want true")
		}
		wantOK := false
		want := before
		want.DesiredState = DesiredStateOnline
		want.RestartAfterStop = false
		want.StoppingSince, want.StoppedSince = 0, 0
		want.RefocusSince, want.RefocusOp = 0, ""
		want.WakingSince = 0
		want.LastOp = "start"
		want.LastOpOK = &wantOK
		want.LastOpLog = ""
		want.LastOpReason = "held_down: the stop the owner asked for has landed — starting this member again, which is what the 重啟 he pressed during the wind-down asked for"
		want.LastOpAt = 1234.5
		apiTestWantEqual(t, "member in memory", m, want)
		apiTestWantEqual(t, "member in database", apiTestMemberRow(t, d, m.ID), want)
		frame := apiTestMemberFrame(1, "patch", m.ID,
			apiTestMemberPayload(m.ID, "Kip", "active", "online"), "server")
		dashboard.wantFrames(frame, apiTestMemberFrame(2, "patch", m.ID,
			apiTestMemberPayload(m.ID, "Kip", "active", "online"), "server"))
	})

	runGuard := func(name string, online bool, mutate func(*Member)) {
		t.Run(name, func(t *testing.T) {
			api, _, d, _ := newAPITestServer(t)
			storedBefore := apiTestMemberRow(t, d, apiTestPlainAgentID)
			m := storedBefore
			mutate(&m)
			expected := m
			var listener *apiTestListener
			if online {
				listener = apiTestListen(t, api, m.ID)
			}
			dashboard := apiTestListen(t, api, "")
			if got := api.consumeRestartAfterStop(&m, 1234.5); got {
				t.Fatal("consumeRestartAfterStop() = true, want false")
			}
			apiTestWantEqual(t, "member in memory", m, expected)
			apiTestWantEqual(t, "member in database", apiTestMemberRow(t, d, m.ID), storedBefore)
			dashboard.wantFrames()
			if listener != nil {
				listener.wantFrames()
			}
		})
	}
	runGuard("a member without a queued restart stays down", false, func(m *Member) {
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 20
	})
	runGuard("a removed member does not consume a queued restart", false, func(m *Member) {
		m.DesiredState = DesiredStateOffline
		m.RestartAfterStop = true
		m.RosterStatus = RosterStatusRemoved
	})
	runGuard("an online desired member does not consume a queued restart", false, func(m *Member) {
		m.RestartAfterStop = true
		m.DesiredState = DesiredStateOnline
	})
	runGuard("a live session does not consume a queued restart", true, func(m *Member) {
		m.DesiredState = DesiredStateOffline
		m.RestartAfterStop = true
		m.StoppingSince = 20
	})
}

func TestQueueWorkerRestartAfterStop(t *testing.T) {
	queuedOK := false
	cases := []struct {
		name string
		w    OutsourceWorker
		op   string
		want bool
		row  OutsourceWorker
	}{
		{
			"an offline worker with a stop anchor queues restart",
			OutsourceWorker{ID: "ow-queue", Codename: "Contractor", Status: WorkerStatusActive,
				DesiredState: DesiredStateOffline, StoppingSince: 20},
			memberOpRelocate,
			true,
			OutsourceWorker{ID: "ow-queue", Codename: "Contractor", Status: WorkerStatusActive,
				DesiredState: DesiredStateOffline, RestartAfterStop: true, StoppingSince: 20,
				LastOp: "start", LastOpOK: &queuedOK,
				LastOpReason: "held_down: the relocate was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down",
				LastOpAt:     1234.5},
		},
		{
			"an offline worker without a stop anchor is not queued",
			OutsourceWorker{ID: "ow-queue", Codename: "Contractor", Status: WorkerStatusActive,
				DesiredState: DesiredStateOffline},
			memberOpRelocate,
			false,
			OutsourceWorker{ID: "ow-queue", Codename: "Contractor", Status: WorkerStatusActive,
				DesiredState: DesiredStateOffline},
		},
		{
			"a worker with online desired state is not queued",
			OutsourceWorker{ID: "ow-queue", Codename: "Contractor", Status: WorkerStatusActive,
				DesiredState: DesiredStateOnline, StoppingSince: 20},
			memberOpRelocate,
			false,
			OutsourceWorker{ID: "ow-queue", Codename: "Contractor", Status: WorkerStatusActive,
				DesiredState: DesiredStateOnline, StoppingSince: 20},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
			dashboard := apiTestListen(t, api, "")
			w := tc.w
			if got := api.queueWorkerRestartAfterStop(&w, tc.op, 1234.5); got != tc.want {
				t.Fatalf("queueWorkerRestartAfterStop() = %t, want %t", got, tc.want)
			}
			apiTestWantEqual(t, "worker after queueWorkerRestartAfterStop", w, tc.row)
			dashboard.wantFrames()
		})
	}
}

func TestPersistWorkerRestartIntent(t *testing.T) {
	api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
	before := w
	w.RestartAfterStop = true
	w.LastOp = "start"
	ok := false
	w.LastOpOK = &ok
	w.LastOpLog = ""
	w.LastOpReason = "held_down: the relocate was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down"
	w.LastOpAt = 1234.5
	dashboard := apiTestListen(t, api, "")
	if err := api.persistWorkerRestartIntent(w); err != nil {
		t.Fatalf("persistWorkerRestartIntent: %v", err)
	}
	want := before
	want.RestartAfterStop = true
	want.LastOp = "start"
	want.LastOpOK = &ok
	want.LastOpLog = ""
	want.LastOpReason = "held_down: the relocate was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down"
	want.LastOpAt = 1234.5
	apiTestWantEqual(t, "worker in memory", w, want)
	stored, err := d.GetOutsourceWorker(w.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetOutsourceWorker: %v (%v)", err, stored)
	}
	apiTestWantEqual(t, "worker in database", *stored, want)
	apiTestWantWorker(t, h, owner, w.ID, apiTestWorkerRow(t, map[string]any{
		"status":         "active",
		"last_op":        "start",
		"last_op_ok":     false,
		"last_op_log":    "",
		"last_op_reason": "held_down: the relocate was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down",
		"last_op_at":     1234.5,
	}))
	dashboard.wantFrames()
}

func TestClearWorkerRestartIntent(t *testing.T) {
	cases := []struct {
		name string
		w    OutsourceWorker
		want OutsourceWorker
	}{
		{
			"clearing a queued worker restart preserves its wind-down row",
			OutsourceWorker{ID: "ow-clear", DesiredState: DesiredStateOffline, RestartAfterStop: true,
				StoppingSince: 10, StoppedSince: 20},
			OutsourceWorker{ID: "ow-clear", DesiredState: DesiredStateOffline,
				StoppingSince: 10, StoppedSince: 20},
		},
		{
			"clearing an absent worker restart stays clear",
			OutsourceWorker{ID: "ow-clear", DesiredState: DesiredStateOffline,
				StoppingSince: 10, StoppedSince: 20},
			OutsourceWorker{ID: "ow-clear", DesiredState: DesiredStateOffline,
				StoppingSince: 10, StoppedSince: 20},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.w
			clearWorkerRestartIntent(&w)
			apiTestWantEqual(t, "worker after clearWorkerRestartIntent", w, tc.want)
		})
	}
}

func TestConsumeWorkerRestartAfterStop(t *testing.T) {
	t.Run("a converged offline worker restarts and publishes only the owner delta", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		before := w
		w.DesiredState = DesiredStateOffline
		w.RestartAfterStop = true
		w.StoppingSince, w.StoppedSince = 20, 30
		w.RefocusSince, w.RefocusOp = 10, memberOpRelocate
		w.WakingSince = 40
		if err := api.persistWorkerWindDownAnchors(w); err != nil {
			t.Fatalf("persistWorkerWindDownAnchors: %v", err)
		}
		if err := d.PutOutsourceWorker(w); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "other-member")
		if got := api.consumeWorkerRestartAfterStop(&w, 1234.5); !got {
			t.Fatal("consumeWorkerRestartAfterStop() = false, want true")
		}
		wantOK := false
		want := before
		want.DesiredState = DesiredStateOnline
		want.RestartAfterStop = false
		want.StoppingSince, want.StoppedSince = 0, 0
		want.RefocusSince, want.RefocusOp = 0, ""
		want.WakingSince = 0
		want.LastOp = "start"
		want.LastOpOK = &wantOK
		want.LastOpLog = ""
		want.LastOpReason = "held_down: the stop the owner asked for has landed — starting this worker again, which is what the 重啟 he pressed during the wind-down asked for"
		want.LastOpAt = 1234.5
		apiTestWantEqual(t, "worker in memory", w, want)
		stored, err := d.GetOutsourceWorker(w.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", err, stored)
		}
		apiTestWantEqual(t, "worker in database", *stored, want)
		apiTestWantWorker(t, h, owner, w.ID, apiTestWorkerRow(t, map[string]any{
			"status":         "active",
			"last_op":        "start",
			"last_op_ok":     false,
			"last_op_log":    "",
			"last_op_reason": "held_down: the stop the owner asked for has landed — starting this worker again, which is what the 重啟 he pressed during the wind-down asked for",
			"last_op_at":     1234.5,
			"desired_state":  "online",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "server"))
		bystander.wantFrames()
	})

	runGuard := func(name string, online bool, mutate func(*OutsourceWorker)) {
		t.Run(name, func(t *testing.T) {
			api, _, d, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
			storedBefore, err := d.GetOutsourceWorker(w.ID)
			if err != nil || storedBefore == nil {
				t.Fatalf("GetOutsourceWorker: %v (%v)", err, storedBefore)
			}
			mutate(&w)
			expected := w
			var listener *apiTestListener
			if online {
				listener = apiTestListen(t, api, w.ID)
			}
			dashboard := apiTestListen(t, api, "")
			if got := api.consumeWorkerRestartAfterStop(&w, 1234.5); got {
				t.Fatal("consumeWorkerRestartAfterStop() = true, want false")
			}
			apiTestWantEqual(t, "worker in memory", w, expected)
			stored, readErr := d.GetOutsourceWorker(w.ID)
			if readErr != nil || stored == nil {
				t.Fatalf("GetOutsourceWorker: %v (%v)", readErr, stored)
			}
			apiTestWantEqual(t, "worker in database", *stored, *storedBefore)
			dashboard.wantFrames()
			if listener != nil {
				listener.wantFrames()
			}
		})
	}
	runGuard("a worker without a queued restart stays down", false, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = 20
	})
	runGuard("a released worker does not consume a queued restart", false, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.RestartAfterStop = true
		w.Status = WorkerStatusReleased
	})
	runGuard("an online desired worker does not consume a queued restart", false, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOnline
		w.RestartAfterStop = true
		w.StoppingSince = 20
	})
	runGuard("a live session does not consume a queued restart", true, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.RestartAfterStop = true
		w.StoppingSince = 20
	})
}

func TestCollectWindDownRow(t *testing.T) {
	row := Member{ID: "m-collect", StoppingSince: 10, StoppedSince: 0,
		RefocusSince: 20, RefocusOp: memberOpRelocate}
	anchors := windDownAnchorRowOfMember(&row)
	first, firstPrior := collectWindDownRow(anchors, 1234.5)
	if !first || firstPrior != 0 {
		t.Fatalf("first collect = (%t, %v), want (true, 0)", first, firstPrior)
	}
	apiTestWantEqual(t, "row after first collect", row, Member{ID: "m-collect",
		StoppingSince: 10, StoppedSince: 1234.5, RefocusSince: 20, RefocusOp: memberOpRelocate})
	second, secondPrior := collectWindDownRow(anchors, 1300)
	if second || secondPrior != 1234.5 {
		t.Fatalf("second collect = (%t, %v), want (false, 1234.5)", second, secondPrior)
	}
	apiTestWantEqual(t, "row after second collect", row, Member{ID: "m-collect",
		StoppingSince: 10, StoppedSince: 1234.5, RefocusSince: 20, RefocusOp: memberOpRelocate})

	existing := Member{ID: "m-existing", StoppingSince: 10, StoppedSince: 20,
		RefocusSince: 30, RefocusOp: memberOpModel}
	existingAnchors := windDownAnchorRowOfMember(&existing)
	latched, prior := collectWindDownRow(existingAnchors, 1234.5)
	if latched || prior != 20 {
		t.Fatalf("existing collect = (%t, %v), want (false, 20)", latched, prior)
	}
	apiTestWantEqual(t, "existing row", existing, Member{ID: "m-existing",
		StoppingSince: 10, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpModel})
}

func TestOpenWindDownRow(t *testing.T) {
	row := Member{ID: "m-open", StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate}
	anchors := windDownAnchorRowOfMember(&row)
	openWindDownRow(anchors, 1234.5)
	apiTestWantEqual(t, "row after opening", row, Member{ID: "m-open",
		StoppingSince: 1234.5, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate})
	openWindDownRow(anchors, 1300)
	apiTestWantEqual(t, "row after reopening", row, Member{ID: "m-open",
		StoppingSince: 1234.5, StoppedSince: 20, RefocusSince: 30, RefocusOp: memberOpRelocate})

	existing := Member{ID: "m-existing", StoppingSince: 20}
	existingAnchors := windDownAnchorRowOfMember(&existing)
	openWindDownRow(existingAnchors, 1234.5)
	apiTestWantEqual(t, "existing row", existing, Member{ID: "m-existing", StoppingSince: 20})
}

func TestClearWindDownRow(t *testing.T) {
	row := Member{ID: "m-clear-row", StoppingSince: 10, StoppedSince: 20,
		RefocusSince: 30, RefocusOp: memberOpRelocate, WakingSince: 40, ForcedStopAt: 50}
	clearWindDownRow(windDownAnchorRowOfMember(&row))
	apiTestWantEqual(t, "row after clear", row, Member{ID: "m-clear-row",
		WakingSince: 40, ForcedStopAt: 50})
}
