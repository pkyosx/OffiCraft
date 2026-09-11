package main

// domain_test.go — the behaviour of server/ocserverd/domain.go: what each
// closed-set guard admits, what each fold answers in every one of its states,
// and the exact refusal text a caller is handed when a write is turned away.

import (
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalKind(t *testing.T) {
	t.Run("every member of the closed set passes through unchanged, and blank folds to staff", func(t *testing.T) {
		for in, want := range map[string]string{
			"":          KindStaff,
			KindStaff:   KindStaff,
			KindWarden:  KindWarden,
			"outsource": KindOutsource,
		} {
			got, err := CanonicalKind(in)
			if err != nil {
				t.Fatalf("CanonicalKind(%q): %v", in, err)
			}
			if got != want {
				t.Fatalf("CanonicalKind(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("the pre-rename value is refused with a message naming the rename, not a bare closed-set list", func(t *testing.T) {
		got, err := CanonicalKind("assistant")
		if got != "" {
			t.Fatalf("CanonicalKind(assistant) = %q, want the empty string beside the error", got)
		}
		want := `member kind "assistant" was renamed to "staff" (T-48); the closed set is {"staff", "warden", "outsource"}`
		if err == nil || err.Error() != want {
			t.Fatalf("CanonicalKind(assistant) error:\n got %v\nwant %s", err, want)
		}
	})

	t.Run("any other value outside the closed set is refused with the plain membership message", func(t *testing.T) {
		got, err := CanonicalKind("robot")
		if got != "" {
			t.Fatalf("CanonicalKind(robot) = %q, want the empty string beside the error", got)
		}
		want := `member kind "robot" not in {"staff", "warden", "outsource"}`
		if err == nil || err.Error() != want {
			t.Fatalf("CanonicalKind(robot) error:\n got %v\nwant %s", err, want)
		}
	})
}

func TestCanonicalHost(t *testing.T) {
	t.Run("the retired legacy alias folds onto the server-self machine id", func(t *testing.T) {
		if got := CanonicalHost("mbp5"); got != "m-server-self" {
			t.Fatalf("CanonicalHost(mbp5) = %q, want %q", got, "m-server-self")
		}
	})

	t.Run("every other host — the canonical id, a registered machine, the empty string — passes through byte for byte", func(t *testing.T) {
		for _, host := range []string{"m-server-self", "m-abc123", "mbp", "MBP5", "mbp55", ""} {
			if got := CanonicalHost(host); got != host {
				t.Fatalf("CanonicalHost(%q) = %q, want it unchanged", host, got)
			}
		}
	})
}

func TestDeriveLiveness(t *testing.T) {
	t.Run("stop intent dominates: online reads stopping and offline reads stopped, whatever the wake anchor says", func(t *testing.T) {
		for _, wake := range []bool{false, true} {
			if got := deriveLiveness(livenessInput{Online: true, StopIntent: true, WakePending: wake}); got != "stopping" {
				t.Fatalf("deriveLiveness(online, stop, wake=%v) = %q, want %q", wake, got, "stopping")
			}
			if got := deriveLiveness(livenessInput{Online: false, StopIntent: true, WakePending: wake}); got != "stopped" {
				t.Fatalf("deriveLiveness(offline, stop, wake=%v) = %q, want %q", wake, got, "stopped")
			}
		}
	})

	t.Run("without stop intent the live SSE fact wins, then a fresh wake, then offline", func(t *testing.T) {
		if got := deriveLiveness(livenessInput{Online: true, WakePending: true}); got != "online" {
			t.Fatalf("deriveLiveness(online, wake) = %q, want %q", got, "online")
		}
		if got := deriveLiveness(livenessInput{Online: false, WakePending: true}); got != "waking" {
			t.Fatalf("deriveLiveness(offline, wake) = %q, want %q", got, "waking")
		}
		if got := deriveLiveness(livenessInput{}); got != "offline" {
			t.Fatalf("deriveLiveness(zero) = %q, want %q", got, "offline")
		}
	})
}

func TestPresenceState(t *testing.T) {
	const now = 10_000.0

	t.Run("a set stopping_since is the stop intent of both kinds and outranks a fresh wake anchor", func(t *testing.T) {
		m := Member{DesiredState: DesiredStateOnline, WakingSince: now - 1, StoppingSince: now - 1}
		if got := PresenceState(m, now, true); got != "stopping" {
			t.Fatalf("PresenceState(stopping anchor, online) = %q, want %q", got, "stopping")
		}
		if got := PresenceState(m, now, false); got != "stopped" {
			t.Fatalf("PresenceState(stopping anchor, offline) = %q, want %q", got, "stopped")
		}
	})

	t.Run("an offline desired_state with NO stopping anchor reads offline, not stopped — the out-of-box seed row", func(t *testing.T) {
		seeded := Member{DesiredState: DesiredStateOffline}
		if got := PresenceState(seeded, now, false); got != "offline" {
			t.Fatalf("PresenceState(offline intent, no anchor) = %q, want %q", got, "offline")
		}
		anchored := Member{DesiredState: DesiredStateOffline, StoppingSince: now - 1}
		if got := PresenceState(anchored, now, false); got != "stopped" {
			t.Fatalf("PresenceState(offline intent, anchored) = %q, want %q", got, "stopped")
		}
	})

	t.Run("a fresh waking_since under an online intent reads waking, and one older than the TTL falls back to offline", func(t *testing.T) {
		fresh := Member{DesiredState: DesiredStateOnline, WakingSince: now - WakingTTLSecs}
		if got := PresenceState(fresh, now, false); got != "waking" {
			t.Fatalf("PresenceState(wake exactly at the TTL) = %q, want %q", got, "waking")
		}
		stale := Member{DesiredState: DesiredStateOnline, WakingSince: now - WakingTTLSecs - 0.001}
		if got := PresenceState(stale, now, false); got != "offline" {
			t.Fatalf("PresenceState(wake past the TTL) = %q, want %q", got, "offline")
		}
	})

	t.Run("a wake cancelled mid-flight — anchor standing, intent already offline — reads offline", func(t *testing.T) {
		m := Member{DesiredState: DesiredStateOffline, WakingSince: now - 1}
		if got := PresenceState(m, now, false); got != "offline" {
			t.Fatalf("PresenceState(cancelled wake) = %q, want %q", got, "offline")
		}
	})

	t.Run("the live SSE fact alone makes an anchorless row online", func(t *testing.T) {
		if got := PresenceState(Member{}, now, true); got != "online" {
			t.Fatalf("PresenceState(bare row, online) = %q, want %q", got, "online")
		}
		if got := PresenceState(Member{}, now, false); got != "offline" {
			t.Fatalf("PresenceState(bare row, offline) = %q, want %q", got, "offline")
		}
	})
}

func TestWakingTimedOut(t *testing.T) {
	const now = 10_000.0
	m := Member{DesiredState: DesiredStateOnline, WakingSince: now - WakingTTLSecs - 1}

	t.Run("an offline member whose wake anchor is older than the TTL has timed out", func(t *testing.T) {
		if !WakingTimedOut(m, now, false) {
			t.Fatal("WakingTimedOut(stale wake, offline) = false, want true")
		}
	})

	t.Run("an online session, an anchor still inside the TTL, a missing anchor or an offline intent all report not timed out", func(t *testing.T) {
		if WakingTimedOut(m, now, true) {
			t.Fatal("WakingTimedOut(stale wake, ONLINE) = true, want false")
		}
		atTTL := Member{DesiredState: DesiredStateOnline, WakingSince: now - WakingTTLSecs}
		if WakingTimedOut(atTTL, now, false) {
			t.Fatal("WakingTimedOut(anchor exactly at the TTL) = true, want false")
		}
		noAnchor := Member{DesiredState: DesiredStateOnline}
		if WakingTimedOut(noAnchor, now, false) {
			t.Fatal("WakingTimedOut(no anchor) = true, want false")
		}
		cancelled := Member{DesiredState: DesiredStateOffline, WakingSince: now - WakingTTLSecs - 1}
		if WakingTimedOut(cancelled, now, false) {
			t.Fatal("WakingTimedOut(offline intent) = true, want false")
		}
	})
}

func TestStoppingTimedOut(t *testing.T) {
	const now = 10_000.0
	m := Member{StoppingSince: now - StoppingTimeoutSecs - 1}

	t.Run("a still-online member whose shutdown grace lapsed has timed out", func(t *testing.T) {
		if !StoppingTimedOut(m, now, true) {
			t.Fatal("StoppingTimedOut(stale stop, online) = false, want true")
		}
	})

	t.Run("a session already gone, an anchor still inside the grace, or no anchor at all report not timed out", func(t *testing.T) {
		if StoppingTimedOut(m, now, false) {
			t.Fatal("StoppingTimedOut(stale stop, OFFLINE) = true, want false")
		}
		atGrace := Member{StoppingSince: now - StoppingTimeoutSecs}
		if StoppingTimedOut(atGrace, now, true) {
			t.Fatal("StoppingTimedOut(anchor exactly at the grace) = true, want false")
		}
		if StoppingTimedOut(Member{}, now, true) {
			t.Fatal("StoppingTimedOut(no anchor) = true, want false")
		}
	})
}

func TestPickMemberName(t *testing.T) {
	t.Run("with nothing taken it answers a name from the pool", func(t *testing.T) {
		got := PickMemberName(nil, mathrand.New(mathrand.NewPCG(1, 2)))
		if !containsString(MemberNamePool, got) {
			t.Fatalf("PickMemberName(nil) = %q, want one of the %d pool names", got, len(MemberNamePool))
		}
	})

	t.Run("a taken name is excluded whatever its case or surrounding whitespace", func(t *testing.T) {
		taken := make([]string, 0, len(MemberNamePool))
		for i, n := range MemberNamePool {
			if i == 0 {
				continue
			}
			taken = append(taken, "  "+strings.ToUpper(n)+"\t")
		}
		for i := 0; i < 50; i++ {
			got := PickMemberName(taken, mathrand.New(mathrand.NewPCG(uint64(i), 7)))
			if got != MemberNamePool[0] {
				t.Fatalf("PickMemberName(all but %q taken) = %q, want %q",
					MemberNamePool[0], got, MemberNamePool[0])
			}
		}
	})

	t.Run("with the whole pool taken it falls back to a fresh numeric-suffix candidate", func(t *testing.T) {
		taken := append([]string(nil), MemberNamePool...)
		got := PickMemberName(taken, mathrand.New(mathrand.NewPCG(3, 4)))
		if containsString(MemberNamePool, got) {
			t.Fatalf("PickMemberName(whole pool taken) = %q, want a name outside the pool", got)
		}
		base, suffix, ok := strings.Cut(got, "-")
		if !ok || !containsString(MemberNamePool, base) {
			t.Fatalf("PickMemberName(whole pool taken) = %q, want <PoolName>-<n>", got)
		}
		var n int
		if _, err := fmt.Sscanf(suffix, "%d", &n); err != nil || n < 2 || n > 999 {
			t.Fatalf("PickMemberName(whole pool taken) = %q: suffix %q must be 2..999", got, suffix)
		}
	})

	t.Run("a nil rng still answers a fresh pool name", func(t *testing.T) {
		got := PickMemberName(nil, nil)
		if !containsString(MemberNamePool, got) {
			t.Fatalf("PickMemberName(nil, nil) = %q, want one of the pool names", got)
		}
	})
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestValidateMember(t *testing.T) {
	t.Run("a row with an id, a closed-set kind and a blank runtime is valid", func(t *testing.T) {
		for _, kind := range []string{KindStaff, KindWarden, KindOutsource} {
			if err := ValidateMember(Member{ID: "m-1", Kind: kind}); err != nil {
				t.Fatalf("ValidateMember(kind=%q): %v", kind, err)
			}
		}
		if err := ValidateMember(Member{ID: "m-1", Kind: KindStaff, Runtime: RuntimeCodex}); err != nil {
			t.Fatalf("ValidateMember(runtime=codex): %v", err)
		}
	})

	t.Run("a blank id is refused before the kind is even looked at", func(t *testing.T) {
		err := ValidateMember(Member{Kind: "bogus"})
		if err == nil || err.Error() != "member requires a non-empty id" {
			t.Fatalf("ValidateMember(no id): got %v", err)
		}
	})

	t.Run("a kind off the closed set is refused, blank included — blank is an ingest-seam value, never a stored one", func(t *testing.T) {
		want := `member m-1: kind "" not in {"staff", "warden", "outsource"}`
		if err := ValidateMember(Member{ID: "m-1"}); err == nil || err.Error() != want {
			t.Fatalf("ValidateMember(blank kind):\n got %v\nwant %s", err, want)
		}
		want = `member m-1: kind "assistant" not in {"staff", "warden", "outsource"}`
		if err := ValidateMember(Member{ID: "m-1", Kind: "assistant"}); err == nil || err.Error() != want {
			t.Fatalf("ValidateMember(legacy kind):\n got %v\nwant %s", err, want)
		}
	})

	t.Run("a runtime off the closed set is refused", func(t *testing.T) {
		want := `member m-1: runtime "gemini" not in {"claude", "codex"}`
		err := ValidateMember(Member{ID: "m-1", Kind: KindStaff, Runtime: "gemini"})
		if err == nil || err.Error() != want {
			t.Fatalf("ValidateMember(bad runtime):\n got %v\nwant %s", err, want)
		}
	})
}

func TestValidateChatMessage(t *testing.T) {
	if err := ValidateChatMessage(ChatMessage{ID: "c-1"}); err != nil {
		t.Fatalf("ValidateChatMessage(with an id): %v", err)
	}
	err := ValidateChatMessage(ChatMessage{Sender: "ann", Recipient: "bob", Body: "hi"})
	if err == nil || err.Error() != "chat message requires a non-empty id" {
		t.Fatalf("ValidateChatMessage(no id): got %v", err)
	}
}

func TestValidateChatAttachment(t *testing.T) {
	if err := ValidateChatAttachment(ChatAttachment{ID: "att-1"}); err != nil {
		t.Fatalf("ValidateChatAttachment(with an id): %v", err)
	}
	err := ValidateChatAttachment(ChatAttachment{Mime: "image/png", Data: []byte("x")})
	if err == nil || err.Error() != "chat attachment requires a non-empty id" {
		t.Fatalf("ValidateChatAttachment(no id): got %v", err)
	}
}

func TestValidateChatRead(t *testing.T) {
	t.Run("both participants present is valid, a zero watermark included", func(t *testing.T) {
		if err := ValidateChatRead(ChatRead{ReaderID: "ann", PeerID: "bob"}); err != nil {
			t.Fatalf("ValidateChatRead(both sides): %v", err)
		}
	})

	t.Run("a missing reader is refused first, then a missing peer", func(t *testing.T) {
		err := ValidateChatRead(ChatRead{PeerID: "bob", LastReadTS: 5})
		if err == nil || err.Error() != "chat read receipt requires a non-empty reader_id" {
			t.Fatalf("ValidateChatRead(no reader): got %v", err)
		}
		err = ValidateChatRead(ChatRead{ReaderID: "ann", LastReadTS: 5})
		if err == nil || err.Error() != "chat read receipt requires a non-empty peer_id" {
			t.Fatalf("ValidateChatRead(no peer): got %v", err)
		}
		err = ValidateChatRead(ChatRead{})
		if err == nil || err.Error() != "chat read receipt requires a non-empty reader_id" {
			t.Fatalf("ValidateChatRead(neither side): got %v", err)
		}
	})
}

func TestValidateRoleDef(t *testing.T) {
	if err := ValidateRoleDef(RoleDef{RoleKey: "engineer"}); err != nil {
		t.Fatalf("ValidateRoleDef(with a key): %v", err)
	}
	err := ValidateRoleDef(RoleDef{Name: "Engineer", DefinitionMD: "# hi"})
	if err == nil || err.Error() != "role def requires a non-empty role_key" {
		t.Fatalf("ValidateRoleDef(no key): got %v", err)
	}
}

func TestValidateAccountAlias(t *testing.T) {
	if err := ValidateAccountAlias(AccountAlias{Account: "acct-1"}); err != nil {
		t.Fatalf("ValidateAccountAlias(with an account): %v", err)
	}
	err := ValidateAccountAlias(AccountAlias{DisplayName: "Studio account"})
	if err == nil || err.Error() != "account alias requires a non-empty account" {
		t.Fatalf("ValidateAccountAlias(no account): got %v", err)
	}
}

func TestValidateMachineAlias(t *testing.T) {
	if err := ValidateMachineAlias(MachineAlias{MachineID: "m-server-self"}); err != nil {
		t.Fatalf("ValidateMachineAlias(with a machine id): %v", err)
	}
	err := ValidateMachineAlias(MachineAlias{DisplayName: "The studio Mac"})
	if err == nil || err.Error() != "machine alias requires a non-empty machine_id" {
		t.Fatalf("ValidateMachineAlias(no machine id): got %v", err)
	}
}

func TestValidateWebhookEndpointID(t *testing.T) {
	t.Run("letters, digits, underscore and hyphen are admitted up to the 64-character cap", func(t *testing.T) {
		for _, id := range []string{"a", "Deploy_Bot-9", strings.Repeat("x", 64)} {
			if err := ValidateWebhookEndpointID(id); err != nil {
				t.Fatalf("ValidateWebhookEndpointID(%q): %v", id, err)
			}
		}
	})

	t.Run("a blank id is refused", func(t *testing.T) {
		err := ValidateWebhookEndpointID("")
		if err == nil || err.Error() != "endpoint id cannot be blank" {
			t.Fatalf("ValidateWebhookEndpointID(blank): got %v", err)
		}
	})

	t.Run("one character past the cap is refused, and the message names the cap", func(t *testing.T) {
		err := ValidateWebhookEndpointID(strings.Repeat("x", 65))
		want := "endpoint id must be at most 64 characters"
		if err == nil || err.Error() != want {
			t.Fatalf("ValidateWebhookEndpointID(65 chars):\n got %v\nwant %s", err, want)
		}
	})

	t.Run("whitespace, punctuation and non-ASCII are all outside the closed character set", func(t *testing.T) {
		want := "endpoint id may contain only letters, digits, '_' and '-' (no spaces or special characters)"
		for _, id := range []string{"has space", "dot.id", "slash/id", "端點", "id\n"} {
			err := ValidateWebhookEndpointID(id)
			if err == nil || err.Error() != want {
				t.Fatalf("ValidateWebhookEndpointID(%q):\n got %v\nwant %s", id, err, want)
			}
		}
	})
}

func TestValidWebhookPlatform(t *testing.T) {
	for _, p := range []string{WebhookPlatformGeneric, WebhookPlatformSlack, WebhookPlatformGithub} {
		if !ValidWebhookPlatform(p) {
			t.Fatalf("ValidWebhookPlatform(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "Generic", "gitlab", "discord", "generic "} {
		if ValidWebhookPlatform(p) {
			t.Fatalf("ValidWebhookPlatform(%q) = true, want false", p)
		}
	}
}

func TestValidScheduledMessageCadence(t *testing.T) {
	for _, c := range []string{"daily", "weekly", "monthly", "custom"} {
		if !ValidScheduledMessageCadence(c) {
			t.Fatalf("ValidScheduledMessageCadence(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "hourly", "Daily", "yearly", "cron"} {
		if ValidScheduledMessageCadence(c) {
			t.Fatalf("ValidScheduledMessageCadence(%q) = true, want false", c)
		}
	}
}

func TestScheduledMessageCadenceList(t *testing.T) {
	got := scheduledMessageCadenceList()
	want := "['daily' 'weekly' 'monthly' 'custom']"
	if got != want {
		t.Fatalf("scheduledMessageCadenceList() = %q, want %q", got, want)
	}
	for _, c := range strings.Fields(strings.Trim(got, "[]")) {
		if !ValidScheduledMessageCadence(strings.Trim(c, "'")) {
			t.Fatalf("the refusal message lists %s, which the guard refuses", c)
		}
	}
}

func TestScheduledMessageCadenceReads(t *testing.T) {
	t.Run("each cadence reads exactly the fields its slot arithmetic consults", func(t *testing.T) {
		reads := map[string][]string{
			"daily":   {"hour", "minute"},
			"weekly":  {"day_of_week", "hour", "minute"},
			"monthly": {"day_of_month", "hour", "minute"},
			"custom":  {"custom_months", "custom_days", "custom_hours", "custom_minutes"},
		}
		every := []string{"hour", "minute", "day_of_week", "day_of_month",
			"custom_months", "custom_days", "custom_hours", "custom_minutes"}
		for cadence, want := range reads {
			for _, field := range every {
				got := scheduledMessageCadenceReads(cadence, field)
				if got != containsString(want, field) {
					t.Fatalf("scheduledMessageCadenceReads(%q, %q) = %v, want %v",
						cadence, field, got, !got)
				}
			}
		}
	})

	t.Run("a cadence outside the closed set reads nothing at all", func(t *testing.T) {
		for _, field := range []string{"hour", "minute", "day_of_week", "custom_days"} {
			if scheduledMessageCadenceReads("hourly", field) {
				t.Fatalf("scheduledMessageCadenceReads(hourly, %q) = true, want false", field)
			}
			if scheduledMessageCadenceReads("", field) {
				t.Fatalf("scheduledMessageCadenceReads(\"\", %q) = true, want false", field)
			}
		}
	})

	t.Run("an unknown field name is read by no cadence", func(t *testing.T) {
		for _, cadence := range []string{"daily", "weekly", "monthly", "custom"} {
			if scheduledMessageCadenceReads(cadence, "second") {
				t.Fatalf("scheduledMessageCadenceReads(%q, second) = true, want false", cadence)
			}
		}
	})
}

func TestAllCustomMonths(t *testing.T) {
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	got := allCustomMonths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allCustomMonths() = %v, want %v", got, want)
	}
	got[0] = 99
	if second := allCustomMonths(); !reflect.DeepEqual(second, want) {
		t.Fatalf("allCustomMonths() after a caller mutated its result = %v, want %v", second, want)
	}
}

func TestValidateScheduledMessageCustomSets(t *testing.T) {
	full := func() ([]int, []int, []int, []int) {
		return allCustomMonths(), []int{1, 15}, []int{9}, []int{0, 30}
	}

	t.Run("four non-empty in-range sets whose month and day can coincide are accepted", func(t *testing.T) {
		if err := ValidateScheduledMessageCustomSets(full()); err != nil {
			t.Fatalf("ValidateScheduledMessageCustomSets(ordinary sets): %v", err)
		}
		if err := ValidateScheduledMessageCustomSets([]int{2}, []int{29}, []int{0}, []int{0}); err != nil {
			t.Fatalf("ValidateScheduledMessageCustomSets(the deliberate leap-year schedule): %v", err)
		}
	})

	t.Run("an empty set is refused per field, and only custom_months carries the omit-it hint", func(t *testing.T) {
		base := "cannot be empty when cadence is 'custom'; list every value that should fire " +
			"(an empty set would be read as either 'always' or 'never', and those must not be one keystroke apart)"
		for _, tc := range []struct {
			name  string
			m     []int
			d     []int
			h     []int
			mi    []int
			field string
			hint  string
		}{
			{"months", nil, []int{1}, []int{0}, []int{0}, "custom_months",
				" (to mean every month, OMIT the field entirely rather than sending [])"},
			{"days", []int{1}, nil, []int{0}, []int{0}, "custom_days", ""},
			{"hours", []int{1}, []int{1}, nil, []int{0}, "custom_hours", ""},
			{"minutes", []int{1}, []int{1}, []int{0}, nil, "custom_minutes", ""},
		} {
			err := ValidateScheduledMessageCustomSets(tc.m, tc.d, tc.h, tc.mi)
			want := tc.field + " " + base + tc.hint
			if err == nil || err.Error() != want {
				t.Fatalf("empty %s:\n got %v\nwant %s", tc.name, err, want)
			}
			empty := []int{}
			switch tc.field {
			case "custom_months":
				err = ValidateScheduledMessageCustomSets(empty, tc.d, tc.h, tc.mi)
			case "custom_days":
				err = ValidateScheduledMessageCustomSets(tc.m, empty, tc.h, tc.mi)
			case "custom_hours":
				err = ValidateScheduledMessageCustomSets(tc.m, tc.d, empty, tc.mi)
			default:
				err = ValidateScheduledMessageCustomSets(tc.m, tc.d, tc.h, empty)
			}
			if err == nil || err.Error() != want {
				t.Fatalf("explicitly [] %s:\n got %v\nwant %s", tc.name, err, want)
			}
		}
	})

	t.Run("an out-of-range value is refused per field with its own bounds", func(t *testing.T) {
		for _, tc := range []struct {
			m, d, h, mi []int
			want        string
		}{
			{[]int{0}, []int{1}, []int{0}, []int{0}, "custom_months values must be between 1 and 12; got 0"},
			{[]int{13}, []int{1}, []int{0}, []int{0}, "custom_months values must be between 1 and 12; got 13"},
			{[]int{1}, []int{32}, []int{0}, []int{0}, "custom_days values must be between 1 and 31; got 32"},
			{[]int{1}, []int{1}, []int{24}, []int{0}, "custom_hours values must be between 0 and 23; got 24"},
			{[]int{1}, []int{1}, []int{0}, []int{60}, "custom_minutes values must be between 0 and 59; got 60"},
			{[]int{1}, []int{1}, []int{0}, []int{-1}, "custom_minutes values must be between 0 and 59; got -1"},
		} {
			err := ValidateScheduledMessageCustomSets(tc.m, tc.d, tc.h, tc.mi)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ValidateScheduledMessageCustomSets(%v,%v,%v,%v):\n got %v\nwant %s",
					tc.m, tc.d, tc.h, tc.mi, err, tc.want)
			}
		}
	})

	t.Run("four legal sets whose month and day can never coincide are still refused", func(t *testing.T) {
		err := ValidateScheduledMessageCustomSets([]int{2}, []int{31}, []int{9}, []int{0})
		want := "custom_months [2] and custom_days [31] never occur together, so this schedule " +
			"could never fire: the longest of the chosen months has 29 days, and the earliest day " +
			"chosen is the 31. Pick a day one of these months actually has, or add a month that has " +
			"this day. (February counts as 29 days, so February with the 29th is allowed and fires " +
			"in leap years only.)"
		if err == nil || err.Error() != want {
			t.Fatalf("months {2} x days {31}:\n got %v\nwant %s", err, want)
		}
	})
}

func TestMaxDaysInMonth(t *testing.T) {
	want := map[int]int{
		1: 31, 2: 29, 3: 31, 4: 30, 5: 31, 6: 30,
		7: 31, 8: 31, 9: 30, 10: 31, 11: 30, 12: 31,
	}
	for m := 1; m <= 12; m++ {
		if got := maxDaysInMonth(m); got != want[m] {
			t.Fatalf("maxDaysInMonth(%d) = %d, want %d", m, got, want[m])
		}
	}
	for _, m := range []int{0, 13, -1, 99} {
		if got := maxDaysInMonth(m); got != 31 {
			t.Fatalf("maxDaysInMonth(%d) = %d, want the default 31", m, got)
		}
	}
}

func TestScheduledMessageMonthDayFeasible(t *testing.T) {
	t.Run("a pair some calendar can satisfy is accepted, the leap day included", func(t *testing.T) {
		for _, tc := range []struct{ months, days []int }{
			{[]int{2}, []int{29}},
			{[]int{2}, []int{28, 31}},
			{[]int{2, 1}, []int{31}},
			{[]int{4}, []int{30}},
			{allCustomMonths(), []int{31}},
		} {
			if err := scheduledMessageMonthDayFeasible(tc.months, tc.days); err != nil {
				t.Fatalf("scheduledMessageMonthDayFeasible(%v, %v): %v", tc.months, tc.days, err)
			}
		}
	})

	t.Run("a pair no calendar can satisfy is refused with both sets and both numbers in the message", func(t *testing.T) {
		err := scheduledMessageMonthDayFeasible([]int{4, 6}, []int{31})
		want := "custom_months [4 6] and custom_days [31] never occur together, so this schedule " +
			"could never fire: the longest of the chosen months has 30 days, and the earliest day " +
			"chosen is the 31. Pick a day one of these months actually has, or add a month that has " +
			"this day. (February counts as 29 days, so February with the 29th is allowed and fires " +
			"in leap years only.)"
		if err == nil || err.Error() != want {
			t.Fatalf("months {4,6} x days {31}:\n got %v\nwant %s", err, want)
		}
		err = scheduledMessageMonthDayFeasible([]int{2}, []int{30, 31})
		want = "custom_months [2] and custom_days [30 31] never occur together, so this schedule " +
			"could never fire: the longest of the chosen months has 29 days, and the earliest day " +
			"chosen is the 30. Pick a day one of these months actually has, or add a month that has " +
			"this day. (February counts as 29 days, so February with the 29th is allowed and fires " +
			"in leap years only.)"
		if err == nil || err.Error() != want {
			t.Fatalf("months {2} x days {30,31}:\n got %v\nwant %s", err, want)
		}
	})
}

func TestValidateScheduledMessageWallClockPresence(t *testing.T) {
	t.Run("a calendar cadence needs both an hour and a minute stated", func(t *testing.T) {
		for _, cadence := range []string{"daily", "weekly", "monthly"} {
			if err := ValidateScheduledMessageWallClockPresence(cadence, true, true); err != nil {
				t.Fatalf("ValidateScheduledMessageWallClockPresence(%q, both sent): %v", cadence, err)
			}
			err := ValidateScheduledMessageWallClockPresence(cadence, false, true)
			want := "hour is required when cadence is '" + cadence + "'; only 'custom' reads the " +
				"custom_hours set instead, and an omitted hour must never be taken to mean midnight"
			if err == nil || err.Error() != want {
				t.Fatalf("%q without an hour:\n got %v\nwant %s", cadence, err, want)
			}
			err = ValidateScheduledMessageWallClockPresence(cadence, true, false)
			want = "minute is required when cadence is '" + cadence + "'; only 'custom' reads the " +
				"custom_minutes set instead, and an omitted minute must never be taken to mean 0"
			if err == nil || err.Error() != want {
				t.Fatalf("%q without a minute:\n got %v\nwant %s", cadence, err, want)
			}
			err = ValidateScheduledMessageWallClockPresence(cadence, false, false)
			want = "hour is required when cadence is '" + cadence + "'; only 'custom' reads the " +
				"custom_hours set instead, and an omitted hour must never be taken to mean midnight"
			if err == nil || err.Error() != want {
				t.Fatalf("%q with neither:\n got %v\nwant %s", cadence, err, want)
			}
		}
	})

	t.Run("custom reads the sets instead, so it needs neither", func(t *testing.T) {
		if err := ValidateScheduledMessageWallClockPresence("custom", false, false); err != nil {
			t.Fatalf("ValidateScheduledMessageWallClockPresence(custom, neither sent): %v", err)
		}
	})

	t.Run("a cadence outside the closed set is left to the cadence guard and answers nil here", func(t *testing.T) {
		for _, cadence := range []string{"", "hourly", "Daily"} {
			if err := ValidateScheduledMessageWallClockPresence(cadence, false, false); err != nil {
				t.Fatalf("ValidateScheduledMessageWallClockPresence(%q): %v", cadence, err)
			}
		}
	})
}

func TestValidScheduledMessageStatus(t *testing.T) {
	for _, s := range []string{"enabled", "disabled"} {
		if !ValidScheduledMessageStatus(s) {
			t.Fatalf("ValidScheduledMessageStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Enabled", "paused", "deleted"} {
		if ValidScheduledMessageStatus(s) {
			t.Fatalf("ValidScheduledMessageStatus(%q) = true, want false", s)
		}
	}
}

func TestValidateScheduledMessageBody(t *testing.T) {
	if err := ValidateScheduledMessageBody(" 早安 "); err != nil {
		t.Fatalf("ValidateScheduledMessageBody(real text): %v", err)
	}
	for _, body := range []string{"", " ", "\t\n  \r\n"} {
		err := ValidateScheduledMessageBody(body)
		if err == nil || err.Error() != "body cannot be blank" {
			t.Fatalf("ValidateScheduledMessageBody(%q): got %v", body, err)
		}
	}
}

func TestValidateScheduledMessageSlotFields(t *testing.T) {
	t.Run("every in-range combination is accepted, boundaries included", func(t *testing.T) {
		for _, tc := range [][4]int{{0, 0, 0, 1}, {23, 59, 6, 31}, {9, 30, 3, 15}} {
			if err := ValidateScheduledMessageSlotFields(tc[0], tc[1], tc[2], tc[3]); err != nil {
				t.Fatalf("ValidateScheduledMessageSlotFields%v: %v", tc, err)
			}
		}
	})

	t.Run("each field out of range is refused with its own bounds, in declaration order", func(t *testing.T) {
		for _, tc := range struct {
			cases []struct {
				h, mi, dw, dm int
				want          string
			}
		}{[]struct {
			h, mi, dw, dm int
			want          string
		}{
			{-1, 0, 0, 1, "hour must be between 0 and 23; got -1"},
			{24, 0, 0, 1, "hour must be between 0 and 23; got 24"},
			{0, 60, 0, 1, "minute must be between 0 and 59; got 60"},
			{0, -1, 0, 1, "minute must be between 0 and 59; got -1"},
			{0, 0, 7, 1, "day_of_week must be between 0 (Sunday) and 6 (Saturday); got 7"},
			{0, 0, -1, 1, "day_of_week must be between 0 (Sunday) and 6 (Saturday); got -1"},
			{0, 0, 0, 0, "day_of_month must be between 1 and 31; got 0"},
			{0, 0, 0, 32, "day_of_month must be between 1 and 31; got 32"},
			{24, 60, 7, 32, "hour must be between 0 and 23; got 24"},
		}}.cases {
			err := ValidateScheduledMessageSlotFields(tc.h, tc.mi, tc.dw, tc.dm)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ValidateScheduledMessageSlotFields(%d,%d,%d,%d):\n got %v\nwant %s",
					tc.h, tc.mi, tc.dw, tc.dm, err, tc.want)
			}
		}
	})
}

func TestValidateScheduledMessageTimezone(t *testing.T) {
	t.Run("a stated IANA name loads, UTC included", func(t *testing.T) {
		for _, name := range []string{"Asia/Taipei", "UTC", "America/New_York"} {
			if err := ValidateScheduledMessageTimezone(name); err != nil {
				t.Fatalf("ValidateScheduledMessageTimezone(%q): %v", name, err)
			}
		}
	})

	t.Run("a blank name is refused rather than resolving to UTC by accident", func(t *testing.T) {
		want := "timezone cannot be blank; it must be a stated IANA timezone name such as " +
			"'Asia/Taipei' or 'UTC' — an empty name would resolve to UTC by accident rather than by choice"
		for _, name := range []string{"", "   ", "\t"} {
			err := ValidateScheduledMessageTimezone(name)
			if err == nil || err.Error() != want {
				t.Fatalf("ValidateScheduledMessageTimezone(%q):\n got %v\nwant %s", name, err, want)
			}
		}
	})

	t.Run("Local is refused in any casing even though it loads fine", func(t *testing.T) {
		want := "timezone 'Local' means the zone the SERVER happens to be in, which would move " +
			"every schedule when the server moves; state the schedule's own IANA timezone name " +
			"(e.g. 'Asia/Taipei', or 'UTC' if that is genuinely what is meant)"
		for _, name := range []string{"Local", "local", "LOCAL"} {
			err := ValidateScheduledMessageTimezone(name)
			if err == nil || err.Error() != want {
				t.Fatalf("ValidateScheduledMessageTimezone(%q):\n got %v\nwant %s", name, err, want)
			}
		}
	})

	t.Run("a name no zone database knows is refused and quoted back", func(t *testing.T) {
		err := ValidateScheduledMessageTimezone("Mars/Olympus")
		want := "timezone 'Mars/Olympus' is not a known IANA timezone name"
		if err == nil || err.Error() != want {
			t.Fatalf("ValidateScheduledMessageTimezone(Mars/Olympus):\n got %v\nwant %s", err, want)
		}
	})
}

func TestAttachmentRefIDs(t *testing.T) {
	t.Run("conforming refs yield their blob ids in order", func(t *testing.T) {
		meta := map[string]any{"attachments": []any{
			map[string]any{"id": "att-1", "mime": "image/png"},
			map[string]any{"id": "att-2"},
		}}
		want := []string{"att-1", "att-2"}
		if got := AttachmentRefIDs(meta); !reflect.DeepEqual(got, want) {
			t.Fatalf("AttachmentRefIDs(two refs) = %v, want %v", got, want)
		}
	})

	t.Run("blank and non-conforming entries are skipped while the conforming ones survive", func(t *testing.T) {
		meta := map[string]any{"attachments": []any{
			map[string]any{"id": ""},
			"not-a-map",
			map[string]any{"mime": "text/plain"},
			map[string]any{"id": 7},
			map[string]any{"id": "att-9"},
		}}
		want := []string{"att-9"}
		if got := AttachmentRefIDs(meta); !reflect.DeepEqual(got, want) {
			t.Fatalf("AttachmentRefIDs(mixed refs) = %v, want %v", got, want)
		}
	})

	t.Run("meta with no attachments key, a non-list value or a nil map yields nil rather than an empty slice", func(t *testing.T) {
		for name, meta := range map[string]map[string]any{
			"nil map":      nil,
			"no key":       {"reply_to": "c-1"},
			"not a list":   {"attachments": "att-1"},
			"empty list":   {"attachments": []any{}},
			"all rejected": {"attachments": []any{map[string]any{"id": ""}}},
		} {
			got := AttachmentRefIDs(meta)
			if got != nil {
				t.Fatalf("AttachmentRefIDs(%s) = %#v, want nil", name, got)
			}
		}
	})
}

func TestUnreadCounts(t *testing.T) {
	messages := []ChatMessage{
		{ID: "m1", Sender: "ann", Recipient: "reader", TS: 10},
		{ID: "m2", Sender: "ann", Recipient: "reader", TS: 20},
		{ID: "m3", Sender: "bob", Recipient: "reader", TS: 30},
		{ID: "m4", Sender: "reader", Recipient: "ann", TS: 40},
		{ID: "m5", Sender: "ann", Recipient: "bob", TS: 50},
	}

	t.Run("with no receipt every message addressed to the reader counts, per sender", func(t *testing.T) {
		got := UnreadCounts(messages, nil, "reader")
		want := map[string]int{"ann": 2, "bob": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UnreadCounts(no receipts) = %v, want %v", got, want)
		}
	})

	t.Run("a watermark clears only that peer, and only messages at or below it", func(t *testing.T) {
		got := UnreadCounts(messages, []ChatRead{{ReaderID: "reader", PeerID: "ann", LastReadTS: 10}}, "reader")
		want := map[string]int{"ann": 1, "bob": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UnreadCounts(ann watermark at m1) = %v, want %v", got, want)
		}
	})

	t.Run("another reader's receipt clears nothing, and the reader's own sends never count", func(t *testing.T) {
		got := UnreadCounts(messages, []ChatRead{{ReaderID: "bob", PeerID: "ann", LastReadTS: 999}}, "reader")
		want := map[string]int{"ann": 2, "bob": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UnreadCounts(someone else's receipt) = %v, want %v", got, want)
		}
	})

	t.Run("a watermark past every message leaves an empty, non-nil map", func(t *testing.T) {
		receipts := []ChatRead{
			{ReaderID: "reader", PeerID: "ann", LastReadTS: 999},
			{ReaderID: "reader", PeerID: "bob", LastReadTS: 999},
		}
		got := UnreadCounts(messages, receipts, "reader")
		if got == nil || len(got) != 0 {
			t.Fatalf("UnreadCounts(everything read) = %#v, want an empty non-nil map", got)
		}
	})

	t.Run("a reader nobody addressed reads empty while the addressed reader reads counts", func(t *testing.T) {
		got := UnreadCounts(messages, nil, "nobody")
		if got == nil || len(got) != 0 {
			t.Fatalf("UnreadCounts(unaddressed reader) = %#v, want an empty non-nil map", got)
		}
		if counts := UnreadCounts(messages, nil, "bob"); !reflect.DeepEqual(counts, map[string]int{"ann": 1}) {
			t.Fatalf("UnreadCounts(bob) = %v, want map[ann:1]", counts)
		}
	})
}

func TestFoldRoleDef(t *testing.T) {
	t.Run("a live overlay wins whole and reports is_seed from whether a file seed exists", func(t *testing.T) {
		overlay := &RoleDef{RoleKey: "engineer", Name: "My Engineer", DefinitionMD: "# mine"}
		got := FoldRoleDef("engineer", overlay, "Engineer", "# seed", true)
		want := &FoldedRoleDef{Key: "engineer", Name: "My Engineer", DefinitionMD: "# mine",
			IsDefault: false, IsSeed: true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("FoldRoleDef(overlay over a seed):\n got %+v\nwant %+v", got, want)
		}
		got = FoldRoleDef("r-custom", overlay, "", "", false)
		want = &FoldedRoleDef{Key: "r-custom", Name: "My Engineer", DefinitionMD: "# mine",
			IsDefault: false, IsSeed: false}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("FoldRoleDef(custom role, overlay only):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("no overlay or a tombstoned one falls back to the seed and reads as default", func(t *testing.T) {
		want := &FoldedRoleDef{Key: "engineer", Name: "Engineer", DefinitionMD: "# seed",
			IsDefault: true, IsSeed: true}
		if got := FoldRoleDef("engineer", nil, "Engineer", "# seed", true); !reflect.DeepEqual(got, want) {
			t.Fatalf("FoldRoleDef(no overlay):\n got %+v\nwant %+v", got, want)
		}
		dead := &RoleDef{RoleKey: "engineer", Name: "My Engineer", DefinitionMD: "# mine", Tombstoned: true}
		if got := FoldRoleDef("engineer", dead, "Engineer", "# seed", true); !reflect.DeepEqual(got, want) {
			t.Fatalf("FoldRoleDef(tombstoned overlay):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("neither a seed nor a live overlay is an unknown role and answers nil", func(t *testing.T) {
		if got := FoldRoleDef("ghost", nil, "", "", false); got != nil {
			t.Fatalf("FoldRoleDef(no seed, no overlay) = %+v, want nil", got)
		}
		dead := &RoleDef{RoleKey: "ghost", Tombstoned: true}
		if got := FoldRoleDef("ghost", dead, "", "", false); got != nil {
			t.Fatalf("FoldRoleDef(tombstoned overlay, no seed) = %+v, want nil", got)
		}
	})
}

func TestFoldInsight(t *testing.T) {
	t.Run("a live overlay wins and is never default, an empty overlay text included", func(t *testing.T) {
		text, isDefault := FoldInsight(&Insight{RoleKey: "assistant", Text: "mine"}, "seed", true)
		if text != "mine" || isDefault {
			t.Fatalf("FoldInsight(live overlay) = (%q, %v), want (%q, false)", text, isDefault, "mine")
		}
		text, isDefault = FoldInsight(&Insight{RoleKey: "tester"}, "seed", true)
		if text != "" || isDefault {
			t.Fatalf("FoldInsight(overlay with empty text) = (%q, %v), want (\"\", false)", text, isDefault)
		}
	})

	t.Run("never written with a per-role seed reads the seed as the default", func(t *testing.T) {
		text, isDefault := FoldInsight(nil, "seed", true)
		if text != "seed" || !isDefault {
			t.Fatalf("FoldInsight(nil, seed) = (%q, %v), want (%q, true)", text, isDefault, "seed")
		}
		text, isDefault = FoldInsight(&Insight{RoleKey: "assistant", Text: "mine", Tombstoned: true}, "seed", true)
		if text != "seed" || !isDefault {
			t.Fatalf("FoldInsight(tombstoned, seed) = (%q, %v), want (%q, true)", text, isDefault, "seed")
		}
	})

	t.Run("never written with NO seed for this role reads genuinely empty and still default", func(t *testing.T) {
		text, isDefault := FoldInsight(nil, "ignored", false)
		if text != "" || !isDefault {
			t.Fatalf("FoldInsight(nil, no seed) = (%q, %v), want (\"\", true)", text, isDefault)
		}
	})
}

func TestFoldBootDocument(t *testing.T) {
	t.Run("a live overlay wins over the embedded seed and reads as edited", func(t *testing.T) {
		text, isDefault := FoldBootDocument(&BootDocument{Kind: "system_interaction", Text: "mine"}, "seed", true)
		if text != "mine" || isDefault {
			t.Fatalf("FoldBootDocument(live overlay) = (%q, %v), want (%q, false)", text, isDefault, "mine")
		}
		text, isDefault = FoldBootDocument(&BootDocument{Kind: "system_interaction"}, "seed", true)
		if text != "" || isDefault {
			t.Fatalf("FoldBootDocument(overlay with empty text) = (%q, %v), want (\"\", false)", text, isDefault)
		}
	})

	t.Run("a tombstoned overlay restores the embedded seed the edit never touched", func(t *testing.T) {
		text, isDefault := FoldBootDocument(&BootDocument{Kind: "boot_sequence", Text: "mine", Tombstoned: true}, "seed", true)
		if text != "seed" || !isDefault {
			t.Fatalf("FoldBootDocument(tombstoned) = (%q, %v), want (%q, true)", text, isDefault, "seed")
		}
		text, isDefault = FoldBootDocument(nil, "seed", true)
		if text != "seed" || !isDefault {
			t.Fatalf("FoldBootDocument(nil) = (%q, %v), want (%q, true)", text, isDefault, "seed")
		}
	})

	t.Run("a block this binary ships no seed for reads empty and default", func(t *testing.T) {
		text, isDefault := FoldBootDocument(nil, "ignored", false)
		if text != "" || !isDefault {
			t.Fatalf("FoldBootDocument(no seed) = (%q, %v), want (\"\", true)", text, isDefault)
		}
	})
}

func TestApplyDocEdits(t *testing.T) {
	t.Run("edits apply in order, each against the text the previous one produced", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("alpha beta", []LessonsEdit{
			{Old: "alpha", New: "gamma"},
			{Old: "gamma beta", New: "delta"},
		}, "get_insight")
		if err != nil {
			t.Fatalf("ApplyDocEdits(chained): %v", err)
		}
		if got != "delta" || applied != 2 {
			t.Fatalf("ApplyDocEdits(chained) = (%q, %d), want (%q, 2)", got, applied, "delta")
		}
	})

	t.Run("an empty old appends, joining with a newline only when the doc needs one", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("head", []LessonsEdit{{New: "tail"}}, "get_insight")
		if err != nil || got != "head\ntail" || applied != 1 {
			t.Fatalf("append to a doc with no trailing newline = (%q, %d, %v), want (%q, 1, nil)",
				got, applied, err, "head\ntail")
		}
		got, applied, err = ApplyDocEdits("head\n", []LessonsEdit{{New: "tail"}}, "get_insight")
		if err != nil || got != "head\ntail" || applied != 1 {
			t.Fatalf("append to a newline-terminated doc = (%q, %d, %v), want (%q, 1, nil)",
				got, applied, err, "head\ntail")
		}
		got, applied, err = ApplyDocEdits("", []LessonsEdit{{New: "tail"}}, "get_insight")
		if err != nil || got != "tail" || applied != 1 {
			t.Fatalf("append to an empty doc = (%q, %d, %v), want (%q, 1, nil)", got, applied, err, "tail")
		}
	})

	t.Run("an edit that leaves the text it was handed untouched does not increment the count", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("same", []LessonsEdit{{Old: "same", New: "same"}}, "get_insight")
		if err != nil || got != "same" || applied != 0 {
			t.Fatalf("replace with an identical value = (%q, %d, %v), want (%q, 0, nil)", got, applied, err, "same")
		}
		got, applied, err = ApplyDocEdits("doc\n", []LessonsEdit{{}}, "get_insight")
		if err != nil || got != "doc\n" || applied != 0 {
			t.Fatalf("append \"\" to a newline-terminated doc = (%q, %d, %v), want (%q, 0, nil)",
				got, applied, err, "doc\n")
		}
		got, applied, err = ApplyDocEdits("doc", []LessonsEdit{{}}, "get_insight")
		if err != nil || got != "doc\n" || applied != 1 {
			t.Fatalf("append \"\" to a doc with no trailing newline = (%q, %d, %v), want (%q, 1, nil)",
				got, applied, err, "doc\n")
		}
	})

	t.Run("a batch that undoes itself still counts both edits over byte-identical text", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("anchor", []LessonsEdit{
			{Old: "anchor", New: "middle"},
			{Old: "middle", New: "anchor"},
		}, "get_insight")
		if err != nil || got != "anchor" || applied != 2 {
			t.Fatalf("cancelling batch = (%q, %d, %v), want (%q, 2, nil)", got, applied, err, "anchor")
		}
	})

	t.Run("a missing anchor refuses the whole batch, names the index and the caller's own read tool", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("alpha", []LessonsEdit{
			{Old: "alpha", New: "beta"},
			{Old: "ghost", New: "x"},
		}, "get_insight")
		want := "edits[1]: old not found in the current doc — re-read (get_insight) and re-anchor; nothing was written"
		if err == nil || err.Error() != want {
			t.Fatalf("missing anchor:\n got %v\nwant %s", err, want)
		}
		if got != "" || applied != 0 {
			t.Fatalf("missing anchor returned (%q, %d), want (\"\", 0)", got, applied)
		}
	})

	t.Run("an ambiguous anchor refuses the batch and reports how many locations matched", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("aa bb aa cc aa", []LessonsEdit{{Old: "aa", New: "x"}}, "get_task_manual")
		want := "edits[0]: old matches 3 locations — re-read (get_task_manual) and widen the anchor until it is unique; nothing was written"
		if err == nil || err.Error() != want {
			t.Fatalf("ambiguous anchor:\n got %v\nwant %s", err, want)
		}
		if got != "" || applied != 0 {
			t.Fatalf("ambiguous anchor returned (%q, %d), want (\"\", 0)", got, applied)
		}
	})

	t.Run("no edits at all returns the doc unchanged with nothing applied", func(t *testing.T) {
		got, applied, err := ApplyDocEdits("doc", nil, "get_insight")
		if err != nil || got != "doc" || applied != 0 {
			t.Fatalf("ApplyDocEdits(no edits) = (%q, %d, %v), want (%q, 0, nil)", got, applied, err, "doc")
		}
	})
}

func TestLessonsShrinkBlocked(t *testing.T) {
	big := strings.Repeat("x", 200)

	t.Run("a non-blank doc going blank is blocked", func(t *testing.T) {
		if !LessonsShrinkBlocked("some text", "") {
			t.Fatal("LessonsShrinkBlocked(text -> \"\") = false, want true")
		}
		if !LessonsShrinkBlocked("some text", "  \n ") {
			t.Fatal("LessonsShrinkBlocked(text -> whitespace) = false, want true")
		}
	})

	t.Run("a doc that was already blank has nothing to protect", func(t *testing.T) {
		if LessonsShrinkBlocked("", "") {
			t.Fatal("LessonsShrinkBlocked(\"\" -> \"\") = true, want false")
		}
		if LessonsShrinkBlocked("   ", "") {
			t.Fatal("LessonsShrinkBlocked(whitespace -> \"\") = true, want false")
		}
	})

	t.Run("a doc at the 200-char guard floor shrinking past a tenth is blocked, one char below it is not", func(t *testing.T) {
		if !LessonsShrinkBlocked(big, strings.Repeat("y", 19)) {
			t.Fatal("LessonsShrinkBlocked(200 -> 19) = false, want true")
		}
		if LessonsShrinkBlocked(big, strings.Repeat("y", 20)) {
			t.Fatal("LessonsShrinkBlocked(200 -> 20) = true, want false")
		}
		if LessonsShrinkBlocked(strings.Repeat("x", 199), strings.Repeat("y", 1)) {
			t.Fatal("LessonsShrinkBlocked(199 -> 1) = true, want false")
		}
	})

	t.Run("an ordinary shrink and a growth are both allowed", func(t *testing.T) {
		if LessonsShrinkBlocked(big, strings.Repeat("y", 100)) {
			t.Fatal("LessonsShrinkBlocked(200 -> 100) = true, want false")
		}
		if LessonsShrinkBlocked(big, big+big) {
			t.Fatal("LessonsShrinkBlocked(200 -> 400) = true, want false")
		}
	})
}

func TestDocCapBlocked(t *testing.T) {
	t.Run("a write at or under the cap is allowed whatever was there before", func(t *testing.T) {
		if DocCapBlocked(10, "", strings.Repeat("x", 10)) {
			t.Fatal("DocCapBlocked(cap 10, first write of 10) = true, want false")
		}
		if DocCapBlocked(10, strings.Repeat("x", 30), strings.Repeat("y", 10)) {
			t.Fatal("DocCapBlocked(cap 10, over-cap doc rewritten to 10) = true, want false")
		}
	})

	t.Run("an over-cap write that is strictly shorter than the stored doc is allowed to converge", func(t *testing.T) {
		if DocCapBlocked(10, strings.Repeat("x", 30), strings.Repeat("y", 29)) {
			t.Fatal("DocCapBlocked(30 -> 29 over a cap of 10) = true, want false")
		}
	})

	t.Run("an over-cap write that is equal or longer is refused, equal length included", func(t *testing.T) {
		if !DocCapBlocked(10, strings.Repeat("x", 30), strings.Repeat("y", 30)) {
			t.Fatal("DocCapBlocked(30 -> 30 over a cap of 10) = false, want true")
		}
		if !DocCapBlocked(10, strings.Repeat("x", 30), strings.Repeat("y", 31)) {
			t.Fatal("DocCapBlocked(30 -> 31 over a cap of 10) = false, want true")
		}
		if !DocCapBlocked(10, "", strings.Repeat("y", 11)) {
			t.Fatal("DocCapBlocked(first write of 11 over a cap of 10) = false, want true")
		}
	})

	t.Run("the measure is runes, not bytes", func(t *testing.T) {
		if DocCapBlocked(3, "", "台北市") {
			t.Fatal("DocCapBlocked(cap 3, three CJK runes) = true, want false — the cap counts runes")
		}
		if !DocCapBlocked(3, "", "台北市府") {
			t.Fatal("DocCapBlocked(cap 3, four CJK runes) = false, want true")
		}
	})
}

func TestDocCapRefusal(t *testing.T) {
	got := docCapRefusal(1000, "insight doc", strings.Repeat("x", 1500), "台北"+strings.Repeat("y", 1498))
	want := "the insight doc you are writing is 1500 chars, over the 1000-char cap, and is not " +
		"shorter than the 1500 chars already stored — nothing was written. What is already stored " +
		"is never truncated, but every update must land at or under the cap, or at least come out " +
		"SHORTER than what is there now. Drop stale or superseded material as part of this write " +
		"(or in a shrinking write first), then write again."
	if got != want {
		t.Fatalf("docCapRefusal:\n got %s\nwant %s", got, want)
	}
	if second := docCapRefusal(200, "manual's SOP", "", "abc"); second != "the manual's "+
		"SOP you are writing is 3 chars, over the 200-char cap, and is not shorter than the "+
		"0 chars already stored — nothing was written. What is already stored is never truncated, "+
		"but every update must land at or under the cap, or at least come out SHORTER than what is "+
		"there now. Drop stale or superseded material as part of this write (or in a shrinking "+
		"write first), then write again." {
		t.Fatalf("docCapRefusal(first write):\n got %s", second)
	}
}

func TestDocWipeRefusal(t *testing.T) {
	got := docWipeRefusal("global context", " — or reset_global_context to restore the factory text")
	want := "this would replace the existing global context with an empty one — pass " +
		"allow_shrink=true if that is intended — or reset_global_context to restore the factory " +
		"text; nothing was written"
	if got != want {
		t.Fatalf("docWipeRefusal(with a way out):\n got %s\nwant %s", got, want)
	}
	got = docWipeRefusal("insight doc", "")
	want = "this would replace the existing insight doc with an empty one — pass allow_shrink=true " +
		"if that is intended; nothing was written"
	if got != want {
		t.Fatalf("docWipeRefusal(no way out):\n got %s\nwant %s", got, want)
	}
}

func TestFoldUserContext(t *testing.T) {
	t.Run("a live row is the owner's own block and reads as not-default", func(t *testing.T) {
		text, isDefault := FoldUserContext(&UserContext{Text: "my studio rules"})
		if text != "my studio rules" || isDefault {
			t.Fatalf("FoldUserContext(live row) = (%q, %v), want (%q, false)", text, isDefault, "my studio rules")
		}
		text, isDefault = FoldUserContext(&UserContext{})
		if text != "" || isDefault {
			t.Fatalf("FoldUserContext(live row, empty text) = (%q, %v), want (\"\", false)", text, isDefault)
		}
	})

	t.Run("no row or a tombstoned one folds to the empty seed", func(t *testing.T) {
		text, isDefault := FoldUserContext(nil)
		if text != "" || !isDefault {
			t.Fatalf("FoldUserContext(nil) = (%q, %v), want (\"\", true)", text, isDefault)
		}
		text, isDefault = FoldUserContext(&UserContext{Text: "gone", Tombstoned: true})
		if text != "" || !isDefault {
			t.Fatalf("FoldUserContext(tombstoned) = (%q, %v), want (\"\", true)", text, isDefault)
		}
	})
}

func TestValidTaskLock(t *testing.T) {
	for _, l := range []string{"", "reassigning"} {
		if !ValidTaskLock(l) {
			t.Fatalf("ValidTaskLock(%q) = false, want true", l)
		}
	}
	for _, l := range []string{"waiting_capacity", "Reassigning", "locked", "none"} {
		if ValidTaskLock(l) {
			t.Fatalf("ValidTaskLock(%q) = true, want false", l)
		}
	}
}

func TestValidHandoff(t *testing.T) {
	for _, h := range []string{"return_to_creator", "follow_up", "none"} {
		if !ValidHandoff(h) {
			t.Fatalf("ValidHandoff(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"", "None", "returnToCreator", "followup"} {
		if ValidHandoff(h) {
			t.Fatalf("ValidHandoff(%q) = true, want false", h)
		}
	}
}

func TestTaskNeedsHandoffDeclaration(t *testing.T) {
	t.Run("creator and executor are different actors and nothing has been declared", func(t *testing.T) {
		if !TaskNeedsHandoffDeclaration("owner", "ann", HandoffUndeclared) {
			t.Fatal("TaskNeedsHandoffDeclaration(owner, ann, \"\") = false, want true")
		}
	})

	t.Run("a self-created task has nobody to hand back to", func(t *testing.T) {
		if TaskNeedsHandoffDeclaration("ann", "ann", HandoffUndeclared) {
			t.Fatal("TaskNeedsHandoffDeclaration(ann, ann, \"\") = true, want false")
		}
	})

	t.Run("a blank creator or a blank executor cannot name two sides, so no obligation is invented", func(t *testing.T) {
		if TaskNeedsHandoffDeclaration("", "ann", HandoffUndeclared) {
			t.Fatal("TaskNeedsHandoffDeclaration(\"\", ann, \"\") = true, want false")
		}
		if TaskNeedsHandoffDeclaration("owner", "", HandoffUndeclared) {
			t.Fatal("TaskNeedsHandoffDeclaration(owner, \"\", \"\") = true, want false")
		}
		if TaskNeedsHandoffDeclaration("", "", HandoffUndeclared) {
			t.Fatal("TaskNeedsHandoffDeclaration(\"\", \"\", \"\") = true, want false")
		}
	})

	t.Run("an already-declared task is never re-asked, whatever the declaration says", func(t *testing.T) {
		for _, h := range []string{HandoffReturnToCreator, HandoffFollowUp, HandoffNone} {
			if TaskNeedsHandoffDeclaration("owner", "ann", h) {
				t.Fatalf("TaskNeedsHandoffDeclaration(owner, ann, %q) = true, want false", h)
			}
		}
	})
}

func TestCanonicalTaskExecutorKind(t *testing.T) {
	t.Run("the two closed-set values pass through unchanged", func(t *testing.T) {
		for _, kind := range []string{TaskExecutorStaff, TaskExecutorOutsource} {
			got, err := CanonicalTaskExecutorKind(kind)
			if err != nil || got != kind {
				t.Fatalf("CanonicalTaskExecutorKind(%q) = (%q, %v), want (%q, nil)", kind, got, err, kind)
			}
		}
	})

	t.Run("the empty string is an error here, unlike CanonicalKind which folds it to a default", func(t *testing.T) {
		got, err := CanonicalTaskExecutorKind("")
		want := `task executor kind "" not in {"staff", "outsource"}`
		if got != "" || err == nil || err.Error() != want {
			t.Fatalf("CanonicalTaskExecutorKind(\"\") = (%q, %v), want (\"\", %s)", got, err, want)
		}
		if folded, err := CanonicalKind(""); err != nil || folded != KindStaff {
			t.Fatalf("CanonicalKind(\"\") = (%q, %v), want (%q, nil) — the deliberate divergence",
				folded, err, KindStaff)
		}
	})

	t.Run("the pre-rename value is refused with a message naming the rename", func(t *testing.T) {
		got, err := CanonicalTaskExecutorKind("member")                                                                // kind-vocab-guard:legacy
		want := `task executor kind "member" was renamed to "staff" (T-101); the closed set is {"staff", "outsource"}` // kind-vocab-guard:legacy
		if got != "" || err == nil || err.Error() != want {
			t.Fatalf("CanonicalTaskExecutorKind(member) = (%q, %v), want (\"\", %s)", got, err, want)
		}
	})

	t.Run("warden is not a task executor kind — machines never execute tasks", func(t *testing.T) {
		got, err := CanonicalTaskExecutorKind(KindWarden)
		want := `task executor kind "warden" not in {"staff", "outsource"}`
		if got != "" || err == nil || err.Error() != want {
			t.Fatalf("CanonicalTaskExecutorKind(warden) = (%q, %v), want (\"\", %s)", got, err, want)
		}
	})
}

func TestValidArtifactKind(t *testing.T) {
	for _, k := range []string{"file", "image", "link"} {
		if !ValidArtifactKind(k) {
			t.Fatalf("ValidArtifactKind(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "File", "url", "document", "text"} {
		if ValidArtifactKind(k) {
			t.Fatalf("ValidArtifactKind(%q) = true, want false", k)
		}
	}
}

func TestValidTaskStatus(t *testing.T) {
	for _, s := range []string{"not_started", "in_progress", "waiting_owner",
		"waiting_external", "ready_for_done", "done", "terminated", "duplicated"} {
		if !ValidTaskStatus(s) {
			t.Fatalf("ValidTaskStatus(%q) = false, want true", s)
		}
	}
	t.Run("reassigning is no longer a status — it moved to the orthogonal lock", func(t *testing.T) {
		if ValidTaskStatus(TaskStatusReassigning) {
			t.Fatal("ValidTaskStatus(reassigning) = true, want false")
		}
		if !ValidTaskLock(TaskStatusReassigning) {
			t.Fatal("ValidTaskLock(reassigning) = false, want true")
		}
	})
	for _, s := range []string{"", "Done", "pending", "superseded", "waiting_capacity"} {
		if ValidTaskStatus(s) {
			t.Fatalf("ValidTaskStatus(%q) = true, want false", s)
		}
	}
}

func TestValidTaskPriority(t *testing.T) {
	for _, p := range []string{"high", "mid", "low", "frozen"} {
		if !ValidTaskPriority(p) {
			t.Fatalf("ValidTaskPriority(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "High", "medium", "urgent", "normal"} {
		if ValidTaskPriority(p) {
			t.Fatalf("ValidTaskPriority(%q) = true, want false", p)
		}
	}
}

func TestValidStepStatus(t *testing.T) {
	for _, s := range []string{"pending", "in_progress", "waiting_owner",
		"waiting_external", "done", "superseded"} {
		if !ValidStepStatus(s) {
			t.Fatalf("ValidStepStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "not_started", "terminated", "duplicated", "Done"} {
		if ValidStepStatus(s) {
			t.Fatalf("ValidStepStatus(%q) = true, want false", s)
		}
	}
}

func TestTaskIsTerminal(t *testing.T) {
	for _, s := range []string{"done", "terminated", "duplicated"} {
		if !TaskIsTerminal(s) {
			t.Fatalf("TaskIsTerminal(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "not_started", "in_progress", "waiting_owner",
		"waiting_external", "reassigning", "superseded"} {
		if TaskIsTerminal(s) {
			t.Fatalf("TaskIsTerminal(%q) = true, want false", s)
		}
	}

	t.Run("ready_for_done is NOT terminal — the task is still open there", func(t *testing.T) {
		if TaskIsTerminal(TaskStatusReadyForDone) {
			t.Fatal("TaskIsTerminal(ready_for_done) = true, want false: adding it here would " +
				"shut every write path the close-out window exists for, and mark_task_terminated's own way in")
		}
	})
}

func TestTaskRecordFrozen(t *testing.T) {
	for _, s := range []string{"done", "terminated", "duplicated"} {
		if !TaskRecordFrozen(s) {
			t.Fatalf("TaskRecordFrozen(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "not_started", "in_progress", "waiting_owner",
		"waiting_external", "ready_for_done", "reassigning", "superseded"} {
		if TaskRecordFrozen(s) {
			t.Fatalf("TaskRecordFrozen(%q) = true, want false", s)
		}
	}
}

func TestTaskProgress(t *testing.T) {
	t.Run("every non-superseded row is one leaf and done rows count on both sides", func(t *testing.T) {
		steps := []TaskStep{
			{Status: StepStatusDone},
			{Status: StepStatusInProgress},
			{Status: StepStatusPending},
			{Status: StepStatusWaitingOwner},
			{Status: StepStatusWaitingExternal},
			{Status: StepStatusDone},
		}
		done, total := TaskProgress(steps)
		if done != 2 || total != 6 {
			t.Fatalf("TaskProgress(mixed plan) = (%d, %d), want (2, 6)", done, total)
		}
	})

	t.Run("superseded rows are pure history and count toward neither side", func(t *testing.T) {
		steps := []TaskStep{
			{Status: StepStatusSuperseded},
			{Status: StepStatusDone},
			{Status: StepStatusSuperseded},
		}
		done, total := TaskProgress(steps)
		if done != 1 || total != 1 {
			t.Fatalf("TaskProgress(with superseded rows) = (%d, %d), want (1, 1)", done, total)
		}
	})

	t.Run("an empty plan and an all-superseded plan both count zero of zero", func(t *testing.T) {
		done, total := TaskProgress(nil)
		if done != 0 || total != 0 {
			t.Fatalf("TaskProgress(nil) = (%d, %d), want (0, 0)", done, total)
		}
		done, total = TaskProgress([]TaskStep{{Status: StepStatusSuperseded}})
		if done != 0 || total != 0 {
			t.Fatalf("TaskProgress(all superseded) = (%d, %d), want (0, 0)", done, total)
		}
	})
}

func TestCurrentStep(t *testing.T) {
	t.Run("the first non-terminal step in timeline order is the current one", func(t *testing.T) {
		steps := []TaskStep{
			{ID: "s1", Name: "read", Status: StepStatusDone},
			{ID: "s2", Name: "build", Status: StepStatusInProgress},
			{ID: "s3", Name: "ship", Status: StepStatusPending},
		}
		id, name := CurrentStep(steps)
		if id != "s2" || name != "build" {
			t.Fatalf("CurrentStep(mid-plan) = (%q, %q), want (s2, build)", id, name)
		}
	})

	t.Run("a superseded row is frozen history and is skipped like a done one", func(t *testing.T) {
		steps := []TaskStep{
			{ID: "s1", Name: "old", Status: StepStatusSuperseded},
			{ID: "s2", Name: "done", Status: StepStatusDone},
			{ID: "s3", Name: "waiting", Status: StepStatusWaitingOwner},
		}
		id, name := CurrentStep(steps)
		if id != "s3" || name != "waiting" {
			t.Fatalf("CurrentStep(past superseded) = (%q, %q), want (s3, waiting)", id, name)
		}
	})

	t.Run("an empty plan or an all-terminal plan honestly answers that there is no current step", func(t *testing.T) {
		if id, name := CurrentStep(nil); id != "" || name != "" {
			t.Fatalf("CurrentStep(nil) = (%q, %q), want (\"\", \"\")", id, name)
		}
		steps := []TaskStep{
			{ID: "s1", Name: "first", Status: StepStatusDone},
			{ID: "s2", Name: "second", Status: StepStatusSuperseded},
		}
		if id, name := CurrentStep(steps); id != "" || name != "" {
			t.Fatalf("CurrentStep(all terminal) = (%q, %q), want (\"\", \"\") — never the first row", id, name)
		}
	})
}

func TestDeriveTaskStatus(t *testing.T) {
	t.Run("zero steps and an all-superseded plan both derive not_started", func(t *testing.T) {
		if got := DeriveTaskStatus(nil); got != TaskStatusNotStarted {
			t.Fatalf("DeriveTaskStatus(nil) = %q, want %q", got, TaskStatusNotStarted)
		}
		steps := []TaskStep{{Status: StepStatusSuperseded}, {Status: StepStatusSuperseded}}
		if got := DeriveTaskStatus(steps); got != TaskStatusNotStarted {
			t.Fatalf("DeriveTaskStatus(all superseded) = %q, want %q", got, TaskStatusNotStarted)
		}
	})

	t.Run("waiting_owner outranks waiting_external, ready_for_done and in_progress alike", func(t *testing.T) {
		steps := []TaskStep{
			{Status: StepStatusDone},
			{Status: StepStatusWaitingExternal},
			{Status: StepStatusWaitingOwner},
			{Status: StepStatusInProgress},
		}
		if got := DeriveTaskStatus(steps); got != TaskStatusWaitingOwner {
			t.Fatalf("DeriveTaskStatus(a waiting_owner step present) = %q, want %q", got, TaskStatusWaitingOwner)
		}
	})

	t.Run("waiting_external outranks ready_for_done and in_progress once no step waits on the owner", func(t *testing.T) {
		steps := []TaskStep{
			{Status: StepStatusDone},
			{Status: StepStatusInProgress},
			{Status: StepStatusWaitingExternal},
		}
		if got := DeriveTaskStatus(steps); got != TaskStatusWaitingExternal {
			t.Fatalf("DeriveTaskStatus(a waiting_external step present) = %q, want %q", got, TaskStatusWaitingExternal)
		}
	})

	t.Run("all non-superseded steps done derives ready_for_done, never done", func(t *testing.T) {
		steps := []TaskStep{
			{Status: StepStatusDone},
			{Status: StepStatusSuperseded},
			{Status: StepStatusDone},
		}
		if got := DeriveTaskStatus(steps); got != TaskStatusReadyForDone {
			t.Fatalf("DeriveTaskStatus(all done) = %q, want %q", got, TaskStatusReadyForDone)
		}
	})

	t.Run("no step set derives any of the three terminal statuses", func(t *testing.T) {
		for _, steps := range [][]TaskStep{
			nil,
			{{Status: StepStatusDone}},
			{{Status: StepStatusDone}, {Status: StepStatusSuperseded}},
			{{Status: StepStatusPending}},
			{{Status: StepStatusInProgress}},
			{{Status: StepStatusWaitingOwner}},
			{{Status: StepStatusWaitingExternal}},
		} {
			if got := DeriveTaskStatus(steps); TaskIsTerminal(got) {
				t.Fatalf("DeriveTaskStatus(%+v) = %q, a terminal status — terminals are reached "+
					"only by their own action, never by derivation", steps, got)
			}
		}
	})

	t.Run("all steps still pending derives not_started, and one started step derives in_progress", func(t *testing.T) {
		pending := []TaskStep{{Status: StepStatusPending}, {Status: StepStatusPending}}
		if got := DeriveTaskStatus(pending); got != TaskStatusNotStarted {
			t.Fatalf("DeriveTaskStatus(all pending) = %q, want %q", got, TaskStatusNotStarted)
		}
		started := []TaskStep{{Status: StepStatusPending}, {Status: StepStatusInProgress}}
		if got := DeriveTaskStatus(started); got != TaskStatusInProgress {
			t.Fatalf("DeriveTaskStatus(one started) = %q, want %q", got, TaskStatusInProgress)
		}
		partial := []TaskStep{{Status: StepStatusDone}, {Status: StepStatusPending}}
		if got := DeriveTaskStatus(partial); got != TaskStatusInProgress {
			t.Fatalf("DeriveTaskStatus(one done, one pending) = %q, want %q", got, TaskStatusInProgress)
		}
	})
}

func TestRecomputeTaskStatus(t *testing.T) {
	t.Run("the status is re-projected from the steps and waiting_reason mirrors the FIRST waiting_external step", func(t *testing.T) {
		task := Task{ID: "t-1", Status: TaskStatusNotStarted, WaitingReason: "stale", Lock: TaskLockReassigning}
		steps := []TaskStep{
			{Status: StepStatusDone},
			{Status: StepStatusWaitingExternal, WaitingReason: "waiting on the vendor"},
			{Status: StepStatusWaitingExternal, WaitingReason: "second reason"},
		}
		RecomputeTaskStatus(&task, steps)
		want := Task{ID: "t-1", Status: TaskStatusWaitingExternal,
			WaitingReason: "waiting on the vendor", Lock: TaskLockReassigning}
		if !reflect.DeepEqual(task, want) {
			t.Fatalf("RecomputeTaskStatus:\n got %+v\nwant %+v", task, want)
		}
	})

	t.Run("no waiting_external step clears the display reason", func(t *testing.T) {
		task := Task{ID: "t-1", Status: TaskStatusWaitingExternal, WaitingReason: "waiting on the vendor"}
		RecomputeTaskStatus(&task, []TaskStep{{Status: StepStatusDone}})
		want := Task{ID: "t-1", Status: TaskStatusReadyForDone}
		if !reflect.DeepEqual(task, want) {
			t.Fatalf("RecomputeTaskStatus(no waiting step):\n got %+v\nwant %+v", task, want)
		}
	})

	t.Run("every terminal status is an action's decision and is left completely untouched", func(t *testing.T) {
		for _, status := range []string{TaskStatusDone, TaskStatusTerminated, TaskStatusDuplicated} {
			task := Task{ID: "t-1", Status: status, WaitingReason: "kept"}
			before := task
			RecomputeTaskStatus(&task, []TaskStep{{Status: StepStatusDone}})
			if !reflect.DeepEqual(task, before) {
				t.Fatalf("RecomputeTaskStatus(%s):\n got %+v\nwant it unchanged %+v", status, task, before)
			}
		}
	})

	t.Run("a task closed with mark_task_done is not flipped back to ready_for_done by a recompute", func(t *testing.T) {
		// The step set of a task closed by mark_task_done is ALL DONE, which
		// derives to ready_for_done. Without done on the early-exit list the
		// recompute would answer ready_for_done and silently REOPEN the closed
		// task — both values are legal statuses for that step set, so nothing
		// downstream would report an error.
		task := Task{ID: "t-1", Status: TaskStatusDone}
		RecomputeTaskStatus(&task, []TaskStep{{Status: StepStatusDone}, {Status: StepStatusDone}})
		if task.Status != TaskStatusDone {
			t.Fatalf("RecomputeTaskStatus(done, all steps done) status = %q, want %q",
				task.Status, TaskStatusDone)
		}
	})

	t.Run("ready_for_done is recomputed like any other derived status when a step reopens", func(t *testing.T) {
		task := Task{ID: "t-1", Status: TaskStatusReadyForDone}
		RecomputeTaskStatus(&task, []TaskStep{{Status: StepStatusInProgress}})
		if task.Status != TaskStatusInProgress {
			t.Fatalf("RecomputeTaskStatus(ready_for_done -> a live step) status = %q, want %q",
				task.Status, TaskStatusInProgress)
		}
	})
}

func TestValidatePlanParallelShape(t *testing.T) {
	t.Run("a plain sequential plan and a well-formed parallel group are both legal", func(t *testing.T) {
		fresh := []TaskStep{
			{Name: "a"},
			{Name: "b", ParallelGroup: "g1"},
			{Name: "c", ParallelGroup: "g1"},
			{Name: "join"},
			{Name: "gate", IsGate: true},
		}
		if msg := ValidatePlanParallelShape(nil, fresh); msg != "" {
			t.Fatalf("ValidatePlanParallelShape(legal plan) = %q, want the empty string", msg)
		}
	})

	t.Run("a gate inside a parallel group is refused and the message names the step", func(t *testing.T) {
		fresh := []TaskStep{
			{Name: "lane a", ParallelGroup: "g1"},
			{Name: "ask the owner", ParallelGroup: "g1", IsGate: true},
		}
		want := "step 'ask the owner': a gate step cannot sit inside a parallel group — " +
			"put the gate on its own step after the group's join step"
		if msg := ValidatePlanParallelShape(nil, fresh); msg != want {
			t.Fatalf("ValidatePlanParallelShape(gate in a group):\n got %q\nwant %q", msg, want)
		}
	})

	t.Run("a split group is refused, and the check runs over the kept prefix joined to the fresh plan", func(t *testing.T) {
		fresh := []TaskStep{
			{Name: "a", ParallelGroup: "g1"},
			{Name: "b"},
			{Name: "c", ParallelGroup: "g1"},
		}
		want := "steps sharing parallel_group 'g1' must sit next to each other — " +
			"move them together, or give the later run a different group key"
		if msg := ValidatePlanParallelShape(nil, fresh); msg != want {
			t.Fatalf("ValidatePlanParallelShape(split group):\n got %q\nwant %q", msg, want)
		}
		kept := []TaskStep{{Name: "kept", ParallelGroup: "g1"}}
		spaced := []TaskStep{{Name: "x"}, {Name: "y", ParallelGroup: "g1"}}
		if msg := ValidatePlanParallelShape(kept, spaced); msg != want {
			t.Fatalf("ValidatePlanParallelShape(kept prefix splits the group):\n got %q\nwant %q", msg, want)
		}
		adjacent := []TaskStep{{Name: "y", ParallelGroup: "g1"}, {Name: "x"}}
		if msg := ValidatePlanParallelShape(kept, adjacent); msg != "" {
			t.Fatalf("ValidatePlanParallelShape(kept prefix adjoins the group) = %q, want the empty string", msg)
		}
	})

	t.Run("a one-lane group in the fresh plan is refused, but a legacy kept-only group never blocks a replan", func(t *testing.T) {
		fresh := []TaskStep{{Name: "solo", ParallelGroup: "g1"}, {Name: "next"}}
		want := "parallel_group 'g1' holds only one step — running in parallel takes " +
			"at least two; drop the parallel_group to keep the step sequential"
		if msg := ValidatePlanParallelShape(nil, fresh); msg != want {
			t.Fatalf("ValidatePlanParallelShape(one-lane group):\n got %q\nwant %q", msg, want)
		}
		kept := []TaskStep{{Name: "legacy", ParallelGroup: "old"}}
		if msg := ValidatePlanParallelShape(kept, []TaskStep{{Name: "fresh"}}); msg != "" {
			t.Fatalf("ValidatePlanParallelShape(legacy kept-only group) = %q, want the empty string", msg)
		}
		if msg := ValidatePlanParallelShape(kept, []TaskStep{{Name: "fresh", ParallelGroup: "old"}}); msg != "" {
			t.Fatalf("ValidatePlanParallelShape(fresh step joins the kept group) = %q, want the empty string", msg)
		}
	})

	t.Run("an empty submission is legal", func(t *testing.T) {
		if msg := ValidatePlanParallelShape(nil, nil); msg != "" {
			t.Fatalf("ValidatePlanParallelShape(nothing) = %q, want the empty string", msg)
		}
	})
}

func TestCodenamePrefix(t *testing.T) {
	t.Run("each known family maps to its letter, case-insensitively and anywhere in the name", func(t *testing.T) {
		for model, want := range map[string]string{
			"claude-opus-4-6":        "O",
			"OPUS":                   "O",
			"claude-sonnet-4-5":      "S",
			"anthropic/Sonnet-Large": "S",
			"claude-haiku-4-5":       "H",
			"HAIKU-fast":             "H",
		} {
			if got := CodenamePrefix(model); got != want {
				t.Fatalf("CodenamePrefix(%q) = %q, want %q", model, got, want)
			}
		}
	})

	t.Run("an unrecognised model gets the honest X marker rather than a known family's letter", func(t *testing.T) {
		for _, model := range []string{"", "gpt-5", "gemini-3-pro", "llama"} {
			if got := CodenamePrefix(model); got != "X" {
				t.Fatalf("CodenamePrefix(%q) = %q, want %q", model, got, "X")
			}
		}
	})

	t.Run("opus is checked before sonnet when a name mentions both", func(t *testing.T) {
		if got := CodenamePrefix("sonnet-and-opus"); got != "O" {
			t.Fatalf("CodenamePrefix(sonnet-and-opus) = %q, want %q", got, "O")
		}
	})
}

func TestDeriveCodename(t *testing.T) {
	t.Run("the first codename of a family is number one", func(t *testing.T) {
		if got := DeriveCodename("claude-opus-4-6", nil); got != "O-1" {
			t.Fatalf("DeriveCodename(opus, nothing issued) = %q, want %q", got, "O-1")
		}
	})

	t.Run("the sequence is MAX+1 over the same prefix and never reuses a gap", func(t *testing.T) {
		existing := []string{"O-1", "O-2", "O-7", "S-40"}
		if got := DeriveCodename("claude-opus-4-6", existing); got != "O-8" {
			t.Fatalf("DeriveCodename(opus, %v) = %q, want %q", existing, got, "O-8")
		}
		if got := DeriveCodename("claude-sonnet-4-5", existing); got != "S-41" {
			t.Fatalf("DeriveCodename(sonnet, %v) = %q, want %q", existing, got, "S-41")
		}
		if got := DeriveCodename("claude-haiku-4-5", existing); got != "H-1" {
			t.Fatalf("DeriveCodename(haiku, %v) = %q, want %q", existing, got, "H-1")
		}
	})

	t.Run("codenames that do not parse as this family's sequence are ignored", func(t *testing.T) {
		existing := []string{"O", "O-", "O-abc", "OO-9", "o-9", "O-3"}
		if got := DeriveCodename("opus", existing); got != "O-4" {
			t.Fatalf("DeriveCodename(opus, %v) = %q, want %q", existing, got, "O-4")
		}
	})

	t.Run("an unrecognised model mints on the X sequence", func(t *testing.T) {
		if got := DeriveCodename("gpt-5", []string{"X-4", "O-9"}); got != "X-5" {
			t.Fatalf("DeriveCodename(gpt-5) = %q, want %q", got, "X-5")
		}
	})
}

func TestParseManualFields(t *testing.T) {
	t.Run("a stored fields array decodes to the declared field list in order", func(t *testing.T) {
		got, err := ParseManualFields(`[{"name":"PR Link","required":true,"is_key":true},{"name":"Notes"}]`)
		if err != nil {
			t.Fatalf("ParseManualFields: %v", err)
		}
		want := []ManualField{
			{Name: "PR Link", Required: true, IsKey: true},
			{Name: "Notes"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ParseManualFields:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a blank blob is no fields at all and answers nil, while a stored [] answers an empty slice", func(t *testing.T) {
		got, err := ParseManualFields("")
		if err != nil {
			t.Fatalf("ParseManualFields(\"\"): %v", err)
		}
		if got != nil {
			t.Fatalf("ParseManualFields(\"\") = %#v, want nil", got)
		}
		got, err = ParseManualFields("[]")
		if err != nil {
			t.Fatalf("ParseManualFields(\"[]\"): %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("ParseManualFields(\"[]\") = %#v, want an empty non-nil slice", got)
		}
	})

	t.Run("a corrupt blob is an error naming the column, never a silent empty", func(t *testing.T) {
		got, err := ParseManualFields("{not json")
		if got != nil {
			t.Fatalf("ParseManualFields(corrupt) = %#v, want nil", got)
		}
		if err == nil || !strings.HasPrefix(err.Error(), "task_manual fields: bad JSON: ") {
			t.Fatalf("ParseManualFields(corrupt) error = %v, want a task_manual fields prefix", err)
		}
		var syntaxErr *json.SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Fatalf("ParseManualFields(corrupt) error does not wrap the decoder's own error: %v", err)
		}
		if _, err := ParseManualFields(`{"name":"x"}`); err == nil {
			t.Fatal("ParseManualFields(a JSON object rather than an array) = nil error, want an error")
		}
	})
}

func TestNormalizeInputs(t *testing.T) {
	t.Run("keys are lowercased and outer-trimmed while inner whitespace stays distinct", func(t *testing.T) {
		got, collisions := NormalizeInputs(map[string]any{
			"  PR Link ": "https://example.test/1",
			"PR  Link":   "double space",
			"Notes":      nil,
		})
		want := map[string]any{
			"pr link":  "https://example.test/1",
			"pr  link": "double space",
			"notes":    nil,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("NormalizeInputs:\n got %#v\nwant %#v", got, want)
		}
		if collisions != nil {
			t.Fatalf("NormalizeInputs collisions = %#v, want nil", collisions)
		}
	})

	t.Run("colliding keys keep the first in sort order and report every later collider's ORIGINAL name", func(t *testing.T) {
		got, collisions := NormalizeInputs(map[string]any{
			"PR Link": "upper",
			"pr link": "lower",
			"Pr Link": "mixed",
		})
		want := map[string]any{"pr link": "upper"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("NormalizeInputs(colliders):\n got %#v\nwant %#v", got, want)
		}
		wantCollisions := []string{"Pr Link", "pr link"}
		if !reflect.DeepEqual(collisions, wantCollisions) {
			t.Fatalf("NormalizeInputs collisions = %#v, want %#v", collisions, wantCollisions)
		}
	})

	t.Run("a nil or empty map yields an empty non-nil map and no collisions", func(t *testing.T) {
		for name, in := range map[string]map[string]any{"nil": nil, "empty": {}} {
			got, collisions := NormalizeInputs(in)
			if got == nil || len(got) != 0 {
				t.Fatalf("NormalizeInputs(%s) = %#v, want an empty non-nil map", name, got)
			}
			if collisions != nil {
				t.Fatalf("NormalizeInputs(%s) collisions = %#v, want nil", name, collisions)
			}
		}
	})
}

func TestInputValueMissing(t *testing.T) {
	t.Run("an absent key, a JSON null, and a string blank after trimming are all missing", func(t *testing.T) {
		if !InputValueMissing(nil, false) {
			t.Fatal("InputValueMissing(absent) = false, want true")
		}
		if !InputValueMissing(nil, true) {
			t.Fatal("InputValueMissing(JSON null) = false, want true")
		}
		for _, s := range []string{"", " ", "\t\n"} {
			if !InputValueMissing(s, true) {
				t.Fatalf("InputValueMissing(%q) = false, want true", s)
			}
		}
	})

	t.Run("a non-blank string and every non-string value count as present, zero and false included", func(t *testing.T) {
		for _, v := range []any{"x", " x ", 0.0, 0, false, []any{}, map[string]any{}} {
			if InputValueMissing(v, true) {
				t.Fatalf("InputValueMissing(%#v) = true, want false", v)
			}
		}
	})
}

func TestDedupeKeyValue(t *testing.T) {
	fields := []ManualField{
		{Name: "PR Link", IsKey: true},
		{Name: "Notes"},
		{Name: "Ticket", IsKey: true},
	}

	t.Run("the is_key values join in the manual's declaration order with a unit separator", func(t *testing.T) {
		got := DedupeKeyValue(fields, map[string]any{
			"pr link": "  https://example.test/1  ",
			"Ticket":  "T-9",
			"Notes":   "ignored",
		})
		want := "https://example.test/1\x1fT-9"
		if got != want {
			t.Fatalf("DedupeKeyValue = %q, want %q", got, want)
		}
	})

	t.Run("a missing key field contributes an empty part rather than collapsing the key", func(t *testing.T) {
		got := DedupeKeyValue(fields, map[string]any{"Ticket": "T-9"})
		want := "\x1fT-9"
		if got != want {
			t.Fatalf("DedupeKeyValue(one key absent) = %q, want %q", got, want)
		}
	})

	t.Run("non-string values render as their JSON literal", func(t *testing.T) {
		got := DedupeKeyValue(fields, map[string]any{"PR Link": 42.0, "Ticket": true})
		want := "42\x1ftrue"
		if got != want {
			t.Fatalf("DedupeKeyValue(non-string values) = %q, want %q", got, want)
		}
	})

	t.Run("no key fields, or key fields with no values at all, means there is no dedupe basis", func(t *testing.T) {
		noKeys := []ManualField{{Name: "Notes"}, {Name: "Ticket"}}
		if got := DedupeKeyValue(noKeys, map[string]any{"Notes": "x", "Ticket": "T-9"}); got != "" {
			t.Fatalf("DedupeKeyValue(no is_key fields) = %q, want the empty string", got)
		}
		if got := DedupeKeyValue(fields, nil); got != "" {
			t.Fatalf("DedupeKeyValue(no inputs) = %q, want the empty string", got)
		}
		if got := DedupeKeyValue(fields, map[string]any{"PR Link": "  ", "Ticket": nil}); got != "" {
			t.Fatalf("DedupeKeyValue(blank key values) = %q, want the empty string", got)
		}
		if got := DedupeKeyValue(nil, map[string]any{"PR Link": "x"}); got != "" {
			t.Fatalf("DedupeKeyValue(no fields) = %q, want the empty string", got)
		}
	})

	t.Run("the value keeps its case even though the field name matching does not", func(t *testing.T) {
		got := DedupeKeyValue([]ManualField{{Name: "Path", IsKey: true}},
			map[string]any{"  PATH  ": "/Users/Eva/Repo"})
		if got != "/Users/Eva/Repo" {
			t.Fatalf("DedupeKeyValue(case-sensitive value) = %q, want %q", got, "/Users/Eva/Repo")
		}
	})
}

func TestDisplayName(t *testing.T) {
	names := map[string]string{"acct-1": "Studio account", "acct-2": ""}

	t.Run("an overlay label replaces the id", func(t *testing.T) {
		if got := DisplayName("acct-1", names); got != "Studio account" {
			t.Fatalf("DisplayName(labelled) = %q, want %q", got, "Studio account")
		}
	})

	t.Run("an empty label and an id with no overlay both fall back to the id itself", func(t *testing.T) {
		if got := DisplayName("acct-2", names); got != "acct-2" {
			t.Fatalf("DisplayName(empty label) = %q, want %q", got, "acct-2")
		}
		if got := DisplayName("m-server-self", names); got != "m-server-self" {
			t.Fatalf("DisplayName(no overlay) = %q, want %q", got, "m-server-self")
		}
		if got := DisplayName("m-server-self", nil); got != "m-server-self" {
			t.Fatalf("DisplayName(nil map) = %q, want %q", got, "m-server-self")
		}
	})
}

func TestValidWebhookStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		want   bool
	}{
		{name: "enabled is accepted", status: WebhookStatusEnabled, want: true},
		{name: "disabled is accepted", status: WebhookStatusDisabled, want: true},
		{name: "blank is rejected", status: "", want: false},
		{name: "an unknown status is rejected", status: "paused", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidWebhookStatus(tc.status); got != tc.want {
				t.Fatalf("ValidWebhookStatus(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestWholeDocWipeBlocked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before string
		after  string
		want   bool
	}{
		{name: "content replaced by whitespace is blocked", before: "stored text", after: "  \n", want: true},
		{name: "blank content remains allowed to stay blank", before: " \n", after: "", want: false},
		{name: "content replaced by other content is allowed", before: "stored text", after: "updated", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WholeDocWipeBlocked(tc.before, tc.after); got != tc.want {
				t.Fatalf("WholeDocWipeBlocked(%q, %q) = %v, want %v", tc.before, tc.after, got, tc.want)
			}
		})
	}
}

func TestStepIsTerminal(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{status: StepStatusDone, want: true},
		{status: StepStatusSuperseded, want: true},
		{status: StepStatusPending, want: false},
		{status: StepStatusInProgress, want: false},
		{status: StepStatusWaitingOwner, want: false},
		{status: "unknown", want: false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			if got := StepIsTerminal(tc.status); got != tc.want {
				t.Fatalf("StepIsTerminal(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestCanAgentStepTransition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to string
		want     bool
	}{
		{name: "pending starts work", from: StepStatusPending, to: StepStatusInProgress, want: true},
		{name: "work completes", from: StepStatusInProgress, to: StepStatusDone, want: true},
		{name: "work waits for an external condition", from: StepStatusInProgress, to: StepStatusWaitingExternal, want: true},
		{name: "external condition resumes work", from: StepStatusWaitingExternal, to: StepStatusInProgress, want: true},
		{name: "pending cannot complete directly", from: StepStatusPending, to: StepStatusDone, want: false},
		{name: "waiting owner cannot be changed by an agent report", from: StepStatusWaitingOwner, to: StepStatusInProgress, want: false},
		{name: "terminal work cannot reopen", from: StepStatusDone, to: StepStatusInProgress, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanAgentStepTransition(tc.from, tc.to); got != tc.want {
				t.Fatalf("CanAgentStepTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestNormalizeFieldKey(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "  PR Link ", want: "pr link"},
		{input: "PR  Link", want: "pr  link"},
		{input: "備註", want: "備註"},
		{input: "   ", want: ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := normalizeFieldKey(tc.input); got != tc.want {
				t.Fatalf("normalizeFieldKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTaskNo(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
	}{
		{name: "long task id is preserved", id: "T-72dd79b666d0"},
		{name: "case is preserved", id: "t-72dd79b666d0"},
		{name: "empty id stays empty", id: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TaskNo(tc.id); got != tc.id {
				t.Fatalf("TaskNo(%q) = %q, want the original id", tc.id, got)
			}
		})
	}
}
