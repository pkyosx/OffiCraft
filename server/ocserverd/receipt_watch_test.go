package main

import (
	"reflect"
	"testing"
)

func TestArmReceiptWatch(t *testing.T) {
	t.Run("a landed operation records its receipt deadline and a later dispatch replaces it", func(t *testing.T) {
		api := &apiServer{receiptPending: map[string]pendingReceipt{}}

		api.armReceiptWatch("member-1", "start", "machine-a", 100)
		got := api.receiptPending["member-1"]
		if want := (pendingReceipt{RPC: "start", Warden: "machine-a", Deadline: 190}); got != want {
			t.Fatalf("first watch = %+v, want %+v", got, want)
		}

		api.armReceiptWatch("member-1", "stop", "machine-b", 200)
		got = api.receiptPending["member-1"]
		if want := (pendingReceipt{RPC: "stop", Warden: "machine-b", Deadline: 290}); got != want {
			t.Fatalf("replaced watch = %+v, want %+v", got, want)
		}
	})

	t.Run("a blank target or operation does not create or change a watch", func(t *testing.T) {
		want := pendingReceipt{RPC: "start", Warden: "machine-a", Deadline: 190}
		api := &apiServer{receiptPending: map[string]pendingReceipt{"member-1": want}}

		api.armReceiptWatch("", "start", "machine-b", 100)
		api.armReceiptWatch("member-1", "", "machine-b", 100)

		if got := api.receiptPending["member-1"]; got != want {
			t.Fatalf("watch after invalid arms = %+v, want %+v", got, want)
		}
		if _, ok := api.receiptPending[""]; ok {
			t.Fatal("blank target must not be added to receipt watches")
		}
	})
}

func TestMemberIDRawOf(t *testing.T) {
	for _, tc := range []struct {
		name          string
		commandResult map[string]any
		want          string
	}{
		{name: "a string member id is returned", commandResult: map[string]any{"member_id": "mira"}, want: "mira"},
		{name: "a missing member id is empty", commandResult: map[string]any{}, want: ""},
		{name: "a non-string member id is empty", commandResult: map[string]any{"member_id": 42}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := memberIDRawOf(tc.commandResult); got != tc.want {
				t.Fatalf("memberIDRawOf(%v) = %q, want %q", tc.commandResult, got, tc.want)
			}
		})
	}
}

func TestNoteReceiptArrived(t *testing.T) {
	t.Run("the expected machine receipt disarms the target", func(t *testing.T) {
		api := &apiServer{receiptPending: map[string]pendingReceipt{
			"member-1": {RPC: "start", Warden: "machine-a", Deadline: 190},
		}}

		api.noteReceiptArrived("member-1", "machine-a")

		if _, ok := api.receiptPending["member-1"]; ok {
			t.Fatal("the matching receipt must disarm the watch")
		}
	})

	t.Run("a receipt from a different known machine leaves the watch armed", func(t *testing.T) {
		want := map[string]pendingReceipt{
			"member-1": {RPC: "stop", Warden: "machine-a", Deadline: 290},
		}
		api := &apiServer{receiptPending: map[string]pendingReceipt{
			"member-1": {RPC: "stop", Warden: "machine-a", Deadline: 290},
		}}

		api.noteReceiptArrived("member-1", "machine-b")

		if got := api.receiptPending; !reflect.DeepEqual(got, want) {
			t.Fatalf("watches after a mismatched receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("an unknown reporter or unresolved machine does not block disarming", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			watch    pendingReceipt
			reporter string
		}{
			{name: "unknown reporter", watch: pendingReceipt{RPC: "start", Warden: "machine-a", Deadline: 190}, reporter: ""},
			{name: "unresolved machine", watch: pendingReceipt{RPC: "stop", Warden: "", Deadline: 290}, reporter: "machine-a"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				api := &apiServer{receiptPending: map[string]pendingReceipt{"member-1": tc.watch}}

				api.noteReceiptArrived("member-1", tc.reporter)

				if _, ok := api.receiptPending["member-1"]; ok {
					t.Fatal("the receipt must disarm a watch when identity comparison is unavailable")
				}
			})
		}
	})

	t.Run("a blank or unknown target does not affect existing watches", func(t *testing.T) {
		want := map[string]pendingReceipt{
			"member-1": {RPC: "start", Warden: "machine-a", Deadline: 190},
		}
		api := &apiServer{receiptPending: map[string]pendingReceipt{
			"member-1": {RPC: "start", Warden: "machine-a", Deadline: 190},
		}}

		api.noteReceiptArrived("", "machine-a")
		api.noteReceiptArrived("ghost", "machine-a")

		if got := api.receiptPending; !reflect.DeepEqual(got, want) {
			t.Fatalf("watches after irrelevant receipts = %+v, want %+v", got, want)
		}
	})
}

func TestTakeLapsedReceipts(t *testing.T) {
	t.Run("past and exactly-on-deadline watches are removed while a future watch remains", func(t *testing.T) {
		api := &apiServer{receiptPending: map[string]pendingReceipt{
			"past":   {RPC: "start", Warden: "machine-a", Deadline: 99},
			"at-now": {RPC: "stop", Warden: "machine-b", Deadline: 100},
			"future": {RPC: "start", Warden: "machine-c", Deadline: 101},
		}}

		got := api.takeLapsedReceipts(100)
		want := map[string]pendingReceipt{
			"past":   {RPC: "start", Warden: "machine-a", Deadline: 99},
			"at-now": {RPC: "stop", Warden: "machine-b", Deadline: 100},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("lapsed watches = %+v, want %+v", got, want)
		}

		wantRemaining := map[string]pendingReceipt{
			"future": {RPC: "start", Warden: "machine-c", Deadline: 101},
		}
		if got := api.receiptPending; !reflect.DeepEqual(got, wantRemaining) {
			t.Fatalf("remaining watches = %+v, want %+v", got, wantRemaining)
		}
	})
}

func TestReceiptMissingReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		watch pendingReceipt
		want  string
	}{
		{
			name:  "names the operation and known machine",
			watch: pendingReceipt{RPC: "stop", Warden: "machine-a"},
			want:  "receipt_missing: the stop was handed to machine \"machine-a\" but no receipt came back within 90s — the op may or may not have run; this row's last state is UNKNOWN, not failed. Suspect the machine's link to the server (the receipt POST) before suspecting the op itself",
		},
		{
			name:  "uses target machine when dispatch did not resolve one",
			watch: pendingReceipt{RPC: "start"},
			want:  "receipt_missing: the start was handed to the target machine but no receipt came back within 90s — the op may or may not have run; this row's last state is UNKNOWN, not failed. Suspect the machine's link to the server (the receipt POST) before suspecting the op itself",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := receiptMissingReason(tc.watch); got != tc.want {
				t.Fatalf("receiptMissingReason(%+v) = %q, want %q", tc.watch, got, tc.want)
			}
		})
	}
}

func TestSweepLapsedReceipts(t *testing.T) {
	t.Run("expired targets are stamped and removed while future targets stay pending", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "member-expired", Name: "Expired", Kind: KindStaff})
		reconcileTestPut(t, d, Member{ID: "member-future", Name: "Future", Kind: KindStaff})
		dashboard := apiTestListen(t, api, "")
		api.receiptPending = map[string]pendingReceipt{
			"ghost":          {RPC: "start", Warden: "machine-a", Deadline: 100},
			"member-expired": {RPC: "stop", Warden: "machine-a", Deadline: 100},
			"member-future":  {RPC: "start", Warden: "machine-a", Deadline: 101},
		}

		api.sweepLapsedReceipts(100)

		got := reconcileTestRow(t, d, "member-expired")
		if got.LastOp != "stop" {
			t.Fatalf("expired member last_op = %q, want %q", got.LastOp, "stop")
		}
		if got.LastOpOK == nil || *got.LastOpOK {
			t.Fatalf("expired member last_op_ok = %v, want false", got.LastOpOK)
		}
		if got.LastOpLog != "" {
			t.Fatalf("expired member last_op_log = %q, want empty", got.LastOpLog)
		}
		if got.LastOpReason != "receipt_missing: the stop was handed to machine \"machine-a\" but no receipt came back within 90s — the op may or may not have run; this row's last state is UNKNOWN, not failed. Suspect the machine's link to the server (the receipt POST) before suspecting the op itself" {
			t.Fatalf("expired member last_op_reason = %q", got.LastOpReason)
		}
		if got.LastOpAt != 100 {
			t.Fatalf("expired member last_op_at = %v, want 100", got.LastOpAt)
		}

		future := reconcileTestRow(t, d, "member-future")
		if future.LastOp != "" || future.LastOpOK != nil || future.LastOpLog != "" || future.LastOpReason != "" || future.LastOpAt != 0 {
			t.Fatalf("future member receipt = %+v, want no receipt", future)
		}
		if got := api.receiptPending; !reflect.DeepEqual(got, map[string]pendingReceipt{
			"member-future": {RPC: "start", Warden: "machine-a", Deadline: 101},
		}) {
			t.Fatalf("pending watches after sweep = %+v", got)
		}
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "member-expired",
			apiTestMemberPayload("member-expired", "Expired", "active", ""), "server"))
	})
}

func TestStampReceiptMissing(t *testing.T) {
	t.Run("an active roster member stores the missing receipt and publishes its row change", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "member-target", Name: "Target", Kind: KindStaff})
		dashboard := apiTestListen(t, api, "")

		api.stampReceiptMissing("member-target", pendingReceipt{RPC: "stop", Warden: "machine-a"}, 1234)

		got := reconcileTestRow(t, d, "member-target")
		if got.LastOp != "stop" {
			t.Fatalf("member last_op = %q, want %q", got.LastOp, "stop")
		}
		if got.LastOpOK == nil || *got.LastOpOK {
			t.Fatalf("member last_op_ok = %v, want false", got.LastOpOK)
		}
		if got.LastOpLog != "" {
			t.Fatalf("member last_op_log = %q, want empty", got.LastOpLog)
		}
		if got.LastOpReason != "receipt_missing: the stop was handed to machine \"machine-a\" but no receipt came back within 90s — the op may or may not have run; this row's last state is UNKNOWN, not failed. Suspect the machine's link to the server (the receipt POST) before suspecting the op itself" {
			t.Fatalf("member last_op_reason = %q", got.LastOpReason)
		}
		if got.LastOpAt != 1234 {
			t.Fatalf("member last_op_at = %v, want 1234", got.LastOpAt)
		}
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "member-target",
			apiTestMemberPayload("member-target", "Target", "active", ""), "server"))
	})

	t.Run("an active outsource worker stores the missing receipt and publishes its owner row", func(t *testing.T) {
		api, h, d, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		api.stampReceiptMissing("ow-abc123", pendingReceipt{RPC: "start", Warden: "m-server-self"}, 1234)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op":        "start",
			"last_op_ok":     false,
			"last_op_log":    "",
			"last_op_reason": "receipt_missing: the start was handed to machine \"m-server-self\" but no receipt came back within 90s — the op may or may not have run; this row's last state is UNKNOWN, not failed. Suspect the machine's link to the server (the receipt POST) before suspecting the op itself",
			"last_op_at":     1234,
		}))
		if row, err := d.GetOutsourceWorker("ow-abc123"); err != nil || row == nil || row.LastOp != "start" {
			t.Fatalf("worker durable receipt = %+v, %v", row, err)
		}
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "server"))
	})

	t.Run("a removed member or unknown target receives no stamp or SSE output", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "member-removed", Name: "Removed", Kind: KindStaff, RosterStatus: RosterStatusRemoved})
		dashboard := apiTestListen(t, api, "")

		api.stampReceiptMissing("member-removed", pendingReceipt{RPC: "stop", Warden: "machine-a"}, 1234)
		api.stampReceiptMissing("ghost", pendingReceipt{RPC: "start", Warden: "machine-a"}, 1234)

		got := reconcileTestRow(t, d, "member-removed")
		if got.LastOp != "" || got.LastOpOK != nil || got.LastOpLog != "" || got.LastOpReason != "" || got.LastOpAt != 0 {
			t.Fatalf("removed member receipt = %+v, want no receipt", got)
		}
		dashboard.wantFrames()
	})
}
