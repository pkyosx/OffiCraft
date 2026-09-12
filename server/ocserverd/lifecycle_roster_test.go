// Skeleton generated from server/ocserverd/lifecycle_roster.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestLifecyclePolicyFor(t *testing.T) {
	cases := []struct {
		name   string
		member Member
		want   bool
	}{
		{name: "active staff is retained even when offline is desired", member: Member{
			Kind: KindStaff, RosterStatus: RosterStatusActive, DesiredState: DesiredStateOffline,
		}, want: true},
		{name: "removed staff is excluded", member: Member{
			Kind: KindStaff, RosterStatus: RosterStatusRemoved, DesiredState: DesiredStateOnline,
		}, want: false},
		{name: "a warden is excluded from agent lifecycle", member: Member{
			Kind: KindWarden, RosterStatus: RosterStatusActive, DesiredState: DesiredStateOffline,
		}, want: false},
		{name: "an offline warden with uninstall intent is retained for consumption", member: Member{
			Kind: KindWarden, RosterStatus: RosterStatusActive, DesiredState: DesiredStateUninstall,
		}, want: true},
		{name: "active outsource work is retained", member: Member{
			Kind: KindOutsource, RosterStatus: RosterStatusActive,
			ActivatedTS: 100, DesiredState: DesiredStateOnline,
		}, want: true},
		{name: "held-down outsource work is excluded", member: Member{
			Kind: KindOutsource, RosterStatus: RosterStatusActive,
			ActivatedTS: 100, DesiredState: DesiredStateOffline,
		}, want: false},
		{name: "assigned outsource work has no session to retain", member: Member{
			Kind: KindOutsource, RosterStatus: RosterStatusActive,
			DesiredState: DesiredStateOnline,
		}, want: false},
		{name: "released outsource work is excluded", member: Member{
			Kind: KindOutsource, RosterStatus: RosterStatusRemoved,
			ActivatedTS: 100, DesiredState: DesiredStateOnline,
		}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lifecyclePolicyFor(tc.member).ShouldExist(); got != tc.want {
				t.Fatalf("ShouldExist() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLifecycleTickDriverFor(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want string
	}{
		{name: "staff is driven by reconcile", kind: KindStaff, want: "runReconcileTick"},
		{name: "warden is driven by reconcile", kind: KindWarden, want: "runReconcileTick"},
		{name: "outsource is driven by the scheduler", kind: KindOutsource, want: "runOutsourceTick"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(lifecycleTickDriverFor(Member{Kind: tc.kind})); got != tc.want {
				t.Fatalf("driver = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLifecycleRosterPasses(t *testing.T) {
	passes := (&apiServer{}).lifecycleRosterPasses()
	if len(passes) != 4 {
		t.Fatalf("pass count = %d, want 4", len(passes))
	}
	wantNames := []string{
		"context_high_recycle",
		"token_expiry_winddown",
		"stale_stopping_clear",
		"uninstall_intent_consume",
	}
	rows := []Member{{Kind: KindStaff}, {Kind: KindOutsource}, {Kind: KindWarden}}
	wantApplies := [][]bool{
		{true, true, true},
		{true, true, true},
		{true, true, true},
		{false, false, true},
	}
	for i, pass := range passes {
		if pass.Name != wantNames[i] {
			t.Fatalf("pass %d name = %q, want %q", i, pass.Name, wantNames[i])
		}
		for j, row := range rows {
			if got := pass.AppliesTo(row); got != wantApplies[i][j] {
				t.Fatalf("pass %q applies to row kind %q = %v, want %v",
					pass.Name, row.Kind, got, wantApplies[i][j])
			}
		}
	}
}

func TestRunLifecycleRosterPasses(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	warden := Member{
		ID:           "warden-formality",
		Name:         "Formality warden",
		Kind:         KindWarden,
		Runtime:      RuntimeClaude,
		Effort:       "medium",
		DesiredState: DesiredStateUninstall,
		RosterStatus: RosterStatusActive,
	}
	if err := d.PutMember(warden); err != nil {
		t.Fatalf("PutMember: %v", err)
	}

	roster := []Member{warden}
	api.runLifecycleRosterPasses(roster, 1700000100)
	if roster[0].DesiredState != DesiredStateOffline {
		t.Fatalf("roster desired state = %q, want %q", roster[0].DesiredState, DesiredStateOffline)
	}
	stored, err := d.GetMember(warden.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if stored == nil || stored.DesiredState != DesiredStateOffline {
		t.Fatalf("persisted desired state = %+v, want offline", stored)
	}
}

func TestRunWorkerLifecyclePasses(t *testing.T) {
	t.Run("an online active worker receives the shared stale-stop pass and folds its anchors back", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		worker := OutsourceWorker{
			ID: "ow-lifecycle-pass", Codename: "Lifecycle worker", Runtime: RuntimeClaude,
			Effort: "medium", TaskID: "T-lifecycle-pass", Status: WorkerStatusActive,
			ActivatedTS: 100, DesiredState: DesiredStateOnline, DesiredMachineID: ServerSelfHost,
			RefocusSince: 50, RefocusOp: refocusOpContextNotice,
			StoppingSince: 100, StoppedSince: 40, CreatedTS: 1,
		}
		if err := d.PutOutsourceWorker(worker); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		listener, err := api.hub.Connect(worker.ID, ServerSelfHost)
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(listener) })

		workers := []OutsourceWorker{worker}
		api.runWorkerLifecyclePasses(workers, 701)
		if workers[0].StoppingSince != 0 {
			t.Fatalf("folded stopping_since = %v, want 0", workers[0].StoppingSince)
		}
		if workers[0].RefocusSince != 50 || workers[0].RefocusOp != refocusOpContextNotice || workers[0].StoppedSince != 40 {
			t.Fatalf("folded wind-down anchors = (%v, %q, %v), want (50, %q, 40)",
				workers[0].RefocusSince, workers[0].RefocusOp, workers[0].StoppedSince, refocusOpContextNotice)
		}

		stored, err := d.GetOutsourceWorker(worker.ID)
		if err != nil {
			t.Fatalf("GetOutsourceWorker: %v", err)
		}
		if stored == nil || stored.StoppingSince != 0 {
			t.Fatalf("persisted stopping_since = %+v, want 0", stored)
		}
	})

	t.Run("an assigned worker is not offered to lifecycle passes", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		worker := OutsourceWorker{
			ID: "ow-assigned-lifecycle", Codename: "Assigned worker", Runtime: RuntimeClaude,
			Effort: "medium", TaskID: "T-assigned-lifecycle", Status: WorkerStatusAssigned,
			DesiredState: DesiredStateOnline, DesiredMachineID: ServerSelfHost,
			StoppingSince: 100, CreatedTS: 1,
		}
		if err := d.PutOutsourceWorker(worker); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		workers := []OutsourceWorker{worker}
		api.runWorkerLifecyclePasses(workers, 701)
		if workers[0].StoppingSince != 100 {
			t.Fatalf("assigned worker stopping_since = %v, want 100", workers[0].StoppingSince)
		}
		stored, err := d.GetOutsourceWorker(worker.ID)
		if err != nil {
			t.Fatalf("GetOutsourceWorker: %v", err)
		}
		if stored == nil || stored.StoppingSince != 100 {
			t.Fatalf("persisted assigned worker = %+v, want stopping_since 100", stored)
		}
	})
}
