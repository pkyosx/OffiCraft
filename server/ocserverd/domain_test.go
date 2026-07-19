package main

// domain_test.go — case-for-case port of the retired Python tests/domain/
// {test_member_domain,test_chat_read_domain}.py plus the fold semantics the
// Python service exercises through service.boot (fold_role_def /
// fold_lessons / fold_user_context) and the alias fold.

import (
	"maps"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func presenceMember(mutate func(*Member)) Member {
	m := Member{ID: "mira", Kind: KindAssistant}
	if mutate != nil {
		mutate(&m)
	}
	return m
}

// ── PresenceState ────────────────────────────────────────────────────────────

func TestPresenceState(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Member)
		now    float64
		online bool
		want   string
	}{
		{"online when connected", nil, 1000.0, true, MemberPresenceOnline},
		{"waking within ttl", func(m *Member) {
			m.DesiredState = DesiredStateOnline
			m.WakingSince = 1000.0
		}, 1000.0 + WakingTTLSecs - 1, false, MemberPresenceWaking},
		{"offline when waking stale", func(m *Member) {
			m.DesiredState = DesiredStateOnline
			m.WakingSince = 1000.0
		}, 1000.0 + WakingTTLSecs + 1, false, MemberPresenceOffline},
		{"offline when no intent", func(m *Member) {
			m.DesiredState = DesiredStateOffline
			m.WakingSince = 1000.0
		}, 1000.0, false, MemberPresenceOffline},
		{"stopping when online and stopping_since set", func(m *Member) {
			m.StoppingSince = 1000.0
		}, 1000.0, true, MemberPresenceStopping},
		{"stopped when offline and stopping_since set", func(m *Member) {
			m.StoppingSince = 1000.0
		}, 1000.0, false, MemberPresenceStopped},
		{"stopping_since takes precedence over online", func(m *Member) {
			m.StoppingSince = 1000.0
		}, 1_000_000.0, true, MemberPresenceStopping},
		{"cleared stopping_since returns to online", func(m *Member) {
			m.StoppingSince = 0.0
		}, 1000.0, true, MemberPresenceOnline},
		{"cleared stopping_since returns to waking", func(m *Member) {
			m.DesiredState = DesiredStateOnline
			m.WakingSince = 1000.0
			m.StoppingSince = 0.0
		}, 1000.0 + WakingTTLSecs - 1, false, MemberPresenceWaking},
		// SSE is the GROUND TRUTH for stopped: a warden kill receipt (last_op
		// stop/ok/at>=stopping_since) must NOT judge stopped while the SSE is
		// up — a receipt can lie (the kill missed; the process still answers).
		{"kill receipt never shortcuts a live SSE", func(m *Member) {
			ok := true
			m.StoppingSince = 1000.0
			m.LastOp = "stop"
			m.LastOpOK = &ok
			m.LastOpAt = 1000.0
		}, 1000.0, true, MemberPresenceStopping},
		{"no receipt keeps stopped on dropped SSE", func(m *Member) {
			m.StoppingSince = 1000.0
		}, 1000.0, false, MemberPresenceStopped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PresenceState(presenceMember(tc.mutate), tc.now, tc.online)
			if got != tc.want {
				t.Fatalf("PresenceState = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── deriveLiveness (the shared kernel PresenceState delegates to) ────────────

func TestDeriveLiveness(t *testing.T) {
	cases := []struct {
		name string
		in   livenessInput
		want string
	}{
		{"stop dominates while online → stopping",
			livenessInput{Online: true, StopIntent: true}, MemberPresenceStopping},
		{"stop while offline → stopped",
			livenessInput{Online: false, StopIntent: true}, MemberPresenceStopped},
		{"stop dominates even over a pending wake",
			livenessInput{Online: false, StopIntent: true, WakePending: true}, MemberPresenceStopped},
		{"online with no stop → online",
			livenessInput{Online: true}, MemberPresenceOnline},
		{"online outranks a pending wake",
			livenessInput{Online: true, WakePending: true}, MemberPresenceOnline},
		{"offline with fresh wake → waking",
			livenessInput{Online: false, WakePending: true}, MemberPresenceWaking},
		{"offline, nothing pending → offline",
			livenessInput{}, MemberPresenceOffline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveLiveness(tc.in); got != tc.want {
				t.Fatalf("deriveLiveness(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ── WakingTimedOut ───────────────────────────────────────────────────────────

func TestWakingTimedOut(t *testing.T) {
	waking := presenceMember(func(m *Member) {
		m.DesiredState = DesiredStateOnline
		m.WakingSince = 1000.0
	})
	if !WakingTimedOut(waking, 1000.0+WakingTTLSecs+1, false) {
		t.Fatal("stale waking signal must time out")
	}
	if WakingTimedOut(waking, 1000.0+WakingTTLSecs-1, false) {
		t.Fatal("within the TTL is not a timeout")
	}
	notDesired := presenceMember(func(m *Member) {
		m.DesiredState = DesiredStateOffline
		m.WakingSince = 1000.0
	})
	if WakingTimedOut(notDesired, 1000.0+WakingTTLSecs+1, false) {
		t.Fatal("no online intent means no failed wake")
	}
	// An online session is not a failed wake, however stale the signal.
	if WakingTimedOut(waking, 1000.0+WakingTTLSecs+1, true) {
		t.Fatal("an online member never times out of waking")
	}
}

// ── StoppingTimedOut ─────────────────────────────────────────────────────────

func TestStoppingTimedOut(t *testing.T) {
	stopping := presenceMember(func(m *Member) { m.StoppingSince = 1000.0 })
	if !StoppingTimedOut(stopping, 1000.0+StoppingTimeoutSecs+1, true) {
		t.Fatal("lapsed grace must read timed out")
	}
	if StoppingTimedOut(stopping, 1000.0+StoppingTimeoutSecs-1, true) {
		t.Fatal("within the grace window is not a timeout")
	}
	if StoppingTimedOut(stopping, 1000.0+StoppingTimeoutSecs+1, false) {
		t.Fatal("no live session to force-kill means no stuck collect")
	}
	notStopping := presenceMember(nil)
	if StoppingTimedOut(notStopping, 1_000_000.0, true) {
		t.Fatal("not stopping at all")
	}
	// Orthogonality: the timeout is a reconciliation trigger, NOT a presence
	// input — past the grace the member still projects "stopping".
	now := 1000.0 + StoppingTimeoutSecs + 1
	if got := PresenceState(stopping, now, true); got != MemberPresenceStopping {
		t.Fatalf("timed-out member must still project stopping, got %q", got)
	}
}

// ── CanonicalHost ────────────────────────────────────────────────────────────

func TestCanonicalHostFoldsOnlyLegacyAlias(t *testing.T) {
	if got := CanonicalHost(legacyServerSelfHost); got != ServerSelfHost {
		t.Fatalf("legacy alias must fold to %q, got %q", ServerSelfHost, got)
	}
	for _, host := range []string{ServerSelfHost, "m-seth-box", ""} {
		if got := CanonicalHost(host); got != host {
			t.Fatalf("CanonicalHost(%q) = %q, want unchanged", host, got)
		}
	}
}

// ── CanonicalKind ────────────────────────────────────────────────────────────

func TestCanonicalKindMapsBlankToAssistant(t *testing.T) {
	// The Python bare hire writes kind="" — the Go closed set folds it to the
	// default colleague kind.
	got, err := CanonicalKind("")
	if err != nil || got != KindAssistant {
		t.Fatalf("blank kind must fold to assistant, got (%q, %v)", got, err)
	}
	for _, kind := range []string{KindAssistant, KindWarden, KindOutsource} {
		got, err := CanonicalKind(kind)
		if err != nil || got != kind {
			t.Fatalf("closed-set kind %q must pass through, got (%q, %v)", kind, got, err)
		}
	}
	for _, kind := range []string{"robot", "Assistant", "WARDEN", "Outsource"} {
		if _, err := CanonicalKind(kind); err == nil {
			t.Fatalf("kind %q outside the closed set must be refused", kind)
		}
	}
}

// ── MemberNo ─────────────────────────────────────────────────────────────────

func TestMemberNoFormatAndDeterministic(t *testing.T) {
	a, b := MemberNo("mira"), MemberNo("mira")
	if a != b {
		t.Fatalf("MemberNo must be deterministic: %q != %q", a, b)
	}
	if !regexp.MustCompile(`^MB-[A-Z]{3}\d{3}$`).MatchString(a) {
		t.Fatalf("bad Member-ID format: %q", a)
	}
	if MemberNo("mira") == MemberNo("m-abc123") {
		t.Fatal("MemberNo must be id-sensitive")
	}
}

func TestMemberNoMatchesPythonDerivation(t *testing.T) {
	// Fixtures computed with the Python domain.member.member_no — the display
	// label must stay byte-identical across the port (same id → same badge on
	// both stacks).
	want := map[string]string{
		"mira":          "MB-IRZ635",
		"m-abc123":      "MB-OTI651",
		"m-server-self": "MB-OGT003",
	}
	for id, exp := range want {
		if got := MemberNo(id); got != exp {
			t.Fatalf("MemberNo(%q) = %q, want Python-parity %q", id, got, exp)
		}
	}
}

// ── member name pool / PickMemberName ────────────────────────────────────────

func TestMemberNamePoolIsMiraStyleAndBigEnough(t *testing.T) {
	if len(MemberNamePool) < 20 {
		t.Fatalf("pool too small: %d", len(MemberNamePool))
	}
	seen := map[string]bool{}
	nameRe := regexp.MustCompile(`^[A-Z][a-z]{1,9}$`)
	for _, n := range MemberNamePool {
		fold := strings.ToLower(n)
		if seen[fold] {
			t.Fatalf("duplicate pool name %q", n)
		}
		seen[fold] = true
		if !nameRe.MatchString(n) {
			t.Fatalf("pool name %q is not a Mira-style short given name", n)
		}
	}
	if seen["mira"] {
		t.Fatal("the pool must never contain the seed name Mira")
	}
}

func TestPickMemberNameAvoidsTakenCaseInsensitively(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	taken := []string{"nova", "  Kai  ", "Mira"} // trimmed + folded before comparing
	for range 50 {
		picked := PickMemberName(taken, rng)
		if !slices.Contains(MemberNamePool, picked) {
			t.Fatalf("picked %q is not from the pool", picked)
		}
		fold := strings.ToLower(picked)
		if fold == "nova" || fold == "kai" {
			t.Fatalf("picked a taken name %q", picked)
		}
	}
}

func TestPickMemberNameExhaustedPoolFallsBackToSuffix(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	taken := slices.Clone(MemberNamePool)
	picked := PickMemberName(taken, rng)
	if !regexp.MustCompile(`^[A-Z][a-z]{1,9}-\d+$`).MatchString(picked) {
		t.Fatalf("exhausted pool must fall back to a numeric suffix, got %q", picked)
	}
	for _, tk := range taken {
		if strings.EqualFold(tk, picked) {
			t.Fatalf("fallback %q collides with a taken name", picked)
		}
	}
}

// ── entity invariants ────────────────────────────────────────────────────────

func TestValidateMember(t *testing.T) {
	for _, kind := range []string{KindAssistant, KindWarden, KindOutsource} {
		if err := ValidateMember(Member{ID: "mira", Kind: kind}); err != nil {
			t.Fatalf("valid %s member refused: %v", kind, err)
		}
	}
	if err := ValidateMember(Member{Kind: KindAssistant}); err == nil {
		t.Fatal("blank id must be refused")
	}
	if err := ValidateMember(Member{ID: "m-1", Kind: ""}); err == nil {
		t.Fatal("a stored member must carry a closed-set kind (blank is ingest-only)")
	}
	if err := ValidateMember(Member{ID: "m-1", Kind: "robot"}); err == nil {
		t.Fatal("off-set kind must be refused")
	}
}

func TestValidateChatEntities(t *testing.T) {
	if err := ValidateChatMessage(ChatMessage{ID: "c-1"}); err != nil {
		t.Fatalf("valid message refused: %v", err)
	}
	if err := ValidateChatMessage(ChatMessage{}); err == nil {
		t.Fatal("blank message id must be refused")
	}
	if err := ValidateChatAttachment(ChatAttachment{ID: "att-1"}); err != nil {
		t.Fatalf("valid attachment refused: %v", err)
	}
	if err := ValidateChatAttachment(ChatAttachment{}); err == nil {
		t.Fatal("blank attachment id must be refused")
	}
}

func TestValidateChatRead(t *testing.T) {
	ok := ChatRead{ReaderID: "mira", PeerID: "owner", LastReadTS: 42.0}
	if err := ValidateChatRead(ok); err != nil {
		t.Fatalf("valid receipt refused: %v", err)
	}
	blankReader := ChatRead{PeerID: "owner"}
	if err := ValidateChatRead(blankReader); err == nil || !strings.Contains(err.Error(), "reader_id") {
		t.Fatalf("blank reader_id must be refused naming the field, got %v", err)
	}
	blankPeer := ChatRead{ReaderID: "mira"}
	if err := ValidateChatRead(blankPeer); err == nil || !strings.Contains(err.Error(), "peer_id") {
		t.Fatalf("blank peer_id must be refused naming the field, got %v", err)
	}
}

func TestValidateOverlayEntities(t *testing.T) {
	if err := ValidateRoleDef(RoleDef{RoleKey: "assistant"}); err != nil {
		t.Fatalf("valid role def refused: %v", err)
	}
	if err := ValidateRoleDef(RoleDef{}); err == nil {
		t.Fatal("blank role_key must be refused")
	}
	if err := ValidateLessons(Lessons{RoleKey: "assistant", TaskType: "default"}); err != nil {
		t.Fatalf("valid lessons refused: %v", err)
	}
	if err := ValidateLessons(Lessons{TaskType: "default"}); err == nil {
		t.Fatal("blank role_key must be refused")
	}
	if err := ValidateLessons(Lessons{RoleKey: "assistant"}); err == nil {
		t.Fatal("blank task_type must be refused")
	}
	if err := ValidateAccountAlias(AccountAlias{Account: "acct"}); err != nil {
		t.Fatalf("valid account alias refused: %v", err)
	}
	if err := ValidateAccountAlias(AccountAlias{}); err == nil {
		t.Fatal("blank account must be refused")
	}
	if err := ValidateMachineAlias(MachineAlias{MachineID: "m-1"}); err != nil {
		t.Fatalf("valid machine alias refused: %v", err)
	}
	if err := ValidateMachineAlias(MachineAlias{}); err == nil {
		t.Fatal("blank machine_id must be refused")
	}
}

// ── AttachmentRefIDs ─────────────────────────────────────────────────────────

func TestAttachmentRefIDs(t *testing.T) {
	meta := map[string]any{"attachments": []any{
		map[string]any{"id": "att-1", "mime": "image/png"},
		map[string]any{"id": ""},      // blank id skipped
		map[string]any{"mime": "x/y"}, // no id skipped
		"not-a-ref",                   // non-conforming entry skipped
		map[string]any{"id": "att-2"},
	}}
	got := AttachmentRefIDs(meta)
	if !slices.Equal(got, []string{"att-1", "att-2"}) {
		t.Fatalf("refs = %v, want [att-1 att-2]", got)
	}
	// Free-form / non-conforming meta yields no refs, never panics.
	if got := AttachmentRefIDs(map[string]any{}); got != nil {
		t.Fatalf("no attachments key must yield nil, got %v", got)
	}
	if got := AttachmentRefIDs(map[string]any{"attachments": "oops"}); got != nil {
		t.Fatalf("non-list attachments must yield nil, got %v", got)
	}
}

// ── UnreadCounts ─────────────────────────────────────────────────────────────

func msg(sender, recipient string, ts float64) ChatMessage {
	return ChatMessage{ID: "c-" + sender, Sender: sender, Recipient: recipient, TS: ts}
}

func receipt(reader, peer string, ts float64) ChatRead {
	return ChatRead{ReaderID: reader, PeerID: peer, LastReadTS: ts}
}

func TestUnreadCounts(t *testing.T) {
	cases := []struct {
		name     string
		messages []ChatMessage
		receipts []ChatRead
		want     map[string]int
	}{
		{"no receipt counts every addressed message",
			[]ChatMessage{msg("mira", "owner", 10.0)}, nil,
			map[string]int{"mira": 1}},
		{"watermark covering the message clears it",
			[]ChatMessage{msg("mira", "owner", 10.0)},
			[]ChatRead{receipt("owner", "mira", 10.0)},
			map[string]int{}},
		{"messages newer than the watermark are counted",
			[]ChatMessage{
				msg("mira", "owner", 10.0),
				msg("mira", "owner", 20.0),
				msg("mira", "owner", 30.0),
			},
			[]ChatRead{receipt("owner", "mira", 10.0)},
			map[string]int{"mira": 2}},
		{"counts are per peer",
			[]ChatMessage{
				msg("mira", "owner", 10.0),
				msg("joey", "owner", 20.0),
				msg("joey", "owner", 30.0),
			}, nil,
			map[string]int{"mira": 1, "joey": 2}},
		{"agent-to-agent messages never count",
			[]ChatMessage{msg("mira", "joey", 99.0), msg("joey", "mira", 100.0)}, nil,
			map[string]int{}},
		{"readers own sent messages never count",
			[]ChatMessage{msg("owner", "mira", 50.0)}, nil,
			map[string]int{}},
		{"another readers receipt does not clear",
			[]ChatMessage{msg("mira", "owner", 10.0)},
			[]ChatRead{receipt("mira", "owner", 999.0)},
			map[string]int{"mira": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UnreadCounts(tc.messages, tc.receipts, "owner")
			if !maps.Equal(got, tc.want) {
				t.Fatalf("UnreadCounts = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── FoldRoleDef ──────────────────────────────────────────────────────────────

func TestFoldRoleDef(t *testing.T) {
	liveOverlay := &RoleDef{RoleKey: "assistant", Name: "Edited", DefinitionMD: "custom md"}
	tombstoned := &RoleDef{RoleKey: "assistant", Tombstoned: true}

	// A live overlay wins whole (self-contained: full name + md); an edited
	// seed role stays a seed role (resettable, not deletable).
	got := FoldRoleDef("assistant", liveOverlay, "Assistant", "seed md", true)
	want := &FoldedRoleDef{Key: "assistant", Name: "Edited", DefinitionMD: "custom md", IsSeed: true}
	if got == nil || *got != *want {
		t.Fatalf("live overlay fold = %+v, want %+v", got, want)
	}

	// A tombstoned overlay (reset) falls back to the seed as the default.
	got = FoldRoleDef("assistant", tombstoned, "Assistant", "seed md", true)
	want = &FoldedRoleDef{Key: "assistant", Name: "Assistant", DefinitionMD: "seed md", IsDefault: true, IsSeed: true}
	if got == nil || *got != *want {
		t.Fatalf("tombstoned fold = %+v, want %+v", got, want)
	}

	// No overlay at all also reads the seed default.
	got = FoldRoleDef("assistant", nil, "Assistant", "seed md", true)
	if got == nil || *got != *want {
		t.Fatalf("no-overlay fold = %+v, want %+v", got, want)
	}

	// A custom role: overlay only, no file seed — deletable, never default.
	custom := &RoleDef{RoleKey: "r-abc", Name: "Scout", DefinitionMD: CustomRoleTemplateMD}
	got = FoldRoleDef("r-abc", custom, "", "", false)
	want = &FoldedRoleDef{Key: "r-abc", Name: "Scout", DefinitionMD: CustomRoleTemplateMD}
	if got == nil || *got != *want {
		t.Fatalf("custom fold = %+v, want %+v", got, want)
	}

	// Neither a seed nor a live overlay → nil (unknown role, caller 404s);
	// a tombstoned custom overlay reads the same as absent.
	if got := FoldRoleDef("nope", nil, "", "", false); got != nil {
		t.Fatalf("unknown role must fold to nil, got %+v", got)
	}
	deadCustom := &RoleDef{RoleKey: "r-abc", Tombstoned: true}
	if got := FoldRoleDef("r-abc", deadCustom, "", "", false); got != nil {
		t.Fatalf("tombstoned custom overlay must fold to nil, got %+v", got)
	}
}

// ── FoldLessons ──────────────────────────────────────────────────────────────

func TestFoldLessons(t *testing.T) {
	// No overlay / a tombstoned overlay → the shared seed as the default
	// (every role falls back to the SAME seed until its overlay diverges it).
	if text, isDefault := FoldLessons(nil, "seed lessons"); text != "seed lessons" || !isDefault {
		t.Fatalf("no overlay must fold to the seed default, got (%q, %v)", text, isDefault)
	}
	dead := &Lessons{RoleKey: "assistant", TaskType: "default", Text: "old", Tombstoned: true}
	if text, isDefault := FoldLessons(dead, "seed lessons"); text != "seed lessons" || !isDefault {
		t.Fatalf("tombstoned overlay must fold to the seed default, got (%q, %v)", text, isDefault)
	}
	live := &Lessons{RoleKey: "assistant", TaskType: "default", Text: "learned"}
	if text, isDefault := FoldLessons(live, "seed lessons"); text != "learned" || isDefault {
		t.Fatalf("live overlay must win, got (%q, %v)", text, isDefault)
	}
}

// ── FoldUserContext ──────────────────────────────────────────────────────────

func TestFoldUserContext(t *testing.T) {
	// The additive block's seed is EMPTY: no row (or a tombstoned one) folds
	// to ""/default so the boot-context assembly skips the block entirely.
	if text, isDefault := FoldUserContext(nil); text != "" || !isDefault {
		t.Fatalf("no row must fold to the empty default, got (%q, %v)", text, isDefault)
	}
	dead := &UserContext{Text: "old", Tombstoned: true}
	if text, isDefault := FoldUserContext(dead); text != "" || !isDefault {
		t.Fatalf("tombstoned row must fold to the empty default, got (%q, %v)", text, isDefault)
	}
	live := &UserContext{Text: "my house rules"}
	if text, isDefault := FoldUserContext(live); text != "my house rules" || isDefault {
		t.Fatalf("live row must win, got (%q, %v)", text, isDefault)
	}
}

// ── DisplayName ──────────────────────────────────────────────────────────────

func TestDisplayName(t *testing.T) {
	names := map[string]string{"m-abc123": "Seth 的 MBP"}
	if got := DisplayName("m-abc123", names); got != "Seth 的 MBP" {
		t.Fatalf("overlay label must win, got %q", got)
	}
	// No overlay → the id is its own display name (purely additive fold).
	if got := DisplayName("m-unnamed", names); got != "m-unnamed" {
		t.Fatalf("absent overlay must fold to the id, got %q", got)
	}
	// An empty label reads as no overlay (matches the Python `get(id) or id`).
	if got := DisplayName("m-blank", map[string]string{"m-blank": ""}); got != "m-blank" {
		t.Fatalf("empty label must fold to the id, got %q", got)
	}
	if got := DisplayName("m-x", nil); got != "m-x" {
		t.Fatalf("nil map must fold to the id, got %q", got)
	}
}

func TestDeriveTaskStatus(t *testing.T) {
	step := func(s string) TaskStep { return TaskStep{Status: s} }
	cases := []struct {
		name  string
		steps []TaskStep
		want  string
	}{
		{"zero steps → not_started", nil, TaskStatusNotStarted},
		{"all superseded → not_started",
			[]TaskStep{step(StepStatusSuperseded), step(StepStatusSuperseded)}, TaskStatusNotStarted},
		{"all pending → not_started",
			[]TaskStep{step(StepStatusPending), step(StepStatusPending)}, TaskStatusNotStarted},
		{"one in_progress → in_progress",
			[]TaskStep{step(StepStatusInProgress), step(StepStatusPending)}, TaskStatusInProgress},
		{"one done, rest pending → in_progress",
			[]TaskStep{step(StepStatusDone), step(StepStatusPending)}, TaskStatusInProgress},
		{"all done → done",
			[]TaskStep{step(StepStatusDone), step(StepStatusDone)}, TaskStatusDone},
		{"done + superseded (superseded excluded) → done",
			[]TaskStep{step(StepStatusDone), step(StepStatusSuperseded)}, TaskStatusDone},
		{"waiting_external present → waiting_external",
			[]TaskStep{step(StepStatusInProgress), step(StepStatusWaitingExternal)}, TaskStatusWaitingExternal},
		{"waiting_owner beats waiting_external",
			[]TaskStep{step(StepStatusWaitingExternal), step(StepStatusWaitingOwner)}, TaskStatusWaitingOwner},
		{"waiting_owner beats done",
			[]TaskStep{step(StepStatusDone), step(StepStatusWaitingOwner)}, TaskStatusWaitingOwner},
		{"waiting_external beats done",
			[]TaskStep{step(StepStatusDone), step(StepStatusWaitingExternal)}, TaskStatusWaitingExternal},
		{"never returns a lock or explicit terminal — reassigning/terminated absent from output",
			[]TaskStep{step(StepStatusInProgress)}, TaskStatusInProgress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveTaskStatus(tc.steps); got != tc.want {
				t.Fatalf("DeriveTaskStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidTaskLock(t *testing.T) {
	for _, ok := range []string{TaskLockNone, TaskLockReassigning, "", "reassigning"} {
		if !ValidTaskLock(ok) {
			t.Fatalf("ValidTaskLock(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"pending_outsource_approval", "waiting_capacity", "bogus", "Reassigning"} {
		if ValidTaskLock(bad) {
			t.Fatalf("ValidTaskLock(%q) = true, want false", bad)
		}
	}
}

func TestRecomputeTaskStatus(t *testing.T) {
	step := func(s string) TaskStep { return TaskStep{Status: s} }
	stepReason := func(s, reason string) TaskStep {
		return TaskStep{Status: s, WaitingReason: reason}
	}

	// (a) The guard: explicit terminals are the derivation's blind spots —
	// status AND waiting_reason are left untouched.
	t.Run("terminal untouched", func(t *testing.T) {
		for _, frozen := range []string{
			TaskStatusTerminated, TaskStatusDuplicated,
		} {
			task := &Task{Status: frozen, WaitingReason: "keep"}
			RecomputeTaskStatus(task, []TaskStep{step(StepStatusDone), step(StepStatusDone)})
			if task.Status != frozen {
				t.Fatalf("%s must be left untouched, got %q", frozen, task.Status)
			}
			if task.WaitingReason != "keep" {
				t.Fatalf("%s must keep its waiting_reason, got %q", frozen, task.WaitingReason)
			}
		}
	})

	// (b) Derives the status from the steps and mirrors the FIRST waiting_external
	// step's reason as the display waiting_reason.
	t.Run("derives status and mirrors first waiting_external reason", func(t *testing.T) {
		task := &Task{Status: TaskStatusInProgress, WaitingReason: ""}
		RecomputeTaskStatus(task, []TaskStep{
			step(StepStatusInProgress),
			stepReason(StepStatusWaitingExternal, "vendor A"),
			stepReason(StepStatusWaitingExternal, "vendor B"),
		})
		if task.Status != TaskStatusWaitingExternal {
			t.Fatalf("status must derive to waiting_external, got %q", task.Status)
		}
		if task.WaitingReason != "vendor A" {
			t.Fatalf("waiting_reason must mirror the first waiting_external step, got %q",
				task.WaitingReason)
		}
	})

	// (c) Clears the display waiting_reason when no step is waiting_external.
	t.Run("clears waiting_reason when no step is waiting_external", func(t *testing.T) {
		task := &Task{Status: TaskStatusWaitingExternal, WaitingReason: "stale"}
		RecomputeTaskStatus(task, []TaskStep{step(StepStatusInProgress), step(StepStatusPending)})
		if task.Status != TaskStatusInProgress {
			t.Fatalf("status must derive to in_progress, got %q", task.Status)
		}
		if task.WaitingReason != "" {
			t.Fatalf("waiting_reason must clear, got %q", task.WaitingReason)
		}
	})
}
