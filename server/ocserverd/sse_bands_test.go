package main

import (
	"reflect"
	"testing"
)

func TestAsNumber(t *testing.T) {
	t.Run("accepts the supported numeric gauge values", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			value any
			want  float64
		}{
			{name: "float64", value: float64(12.5), want: 12.5},
			{name: "int", value: int(12), want: 12},
			{name: "int64", value: int64(12), want: 12},
		} {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				got, ok := asNumber(tc.value)
				if !ok || got != tc.want {
					t.Fatalf("asNumber(%T) = %v, %v; want %v, true", tc.value, got, ok, tc.want)
				}
			})
		}
	})

	t.Run("rejects values that are not supported numbers", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			value any
		}{
			{name: "nil", value: nil},
			{name: "bool", value: true},
			{name: "string", value: "12"},
		} {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				if got, ok := asNumber(tc.value); ok || got != 0 {
					t.Fatalf("asNumber(%T) = %v, %v; want 0, false", tc.value, got, ok)
				}
			})
		}
	})
}

func TestBandFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pct      float64
		present  bool
		handover int
		want     string
	}{
		{name: "missing pct stays quiet", want: levelNone},
		{name: "zero threshold disables the band", pct: 90, present: true, handover: 0, want: levelNone},
		{name: "negative threshold disables the band", pct: 90, present: true, handover: -1, want: levelNone},
		{name: "below handover stays quiet", pct: 64.9, present: true, handover: 65, want: levelNone},
		{name: "at handover enters the handover band", pct: 65, present: true, handover: 65, want: levelHandover},
		{name: "above handover remains in the handover band", pct: 80, present: true, handover: 65, want: levelHandover},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var pct *float64
			if tc.present {
				value := tc.pct
				pct = &value
			}
			if got := bandFor(pct, tc.handover); got != tc.want {
				t.Fatalf("bandFor(%v, %d) = %q, want %q", pct, tc.handover, got, tc.want)
			}
		})
	}
}

func TestClaudeNoticePct(t *testing.T) {
	for _, tc := range []struct {
		handover int
		wantAt   int
		wantOK   bool
	}{
		{handover: -1, wantAt: 0, wantOK: false},
		{handover: 0, wantAt: 0, wantOK: false},
		{handover: 10, wantAt: 0, wantOK: false},
		{handover: 11, wantAt: 1, wantOK: true},
		{handover: 65, wantAt: 55, wantOK: true},
	} {
		gotAt, gotOK := claudeNoticePct(tc.handover)
		if gotAt != tc.wantAt || gotOK != tc.wantOK {
			t.Fatalf("claudeNoticePct(%d) = %d, %v; want %d, %v", tc.handover, gotAt, gotOK, tc.wantAt, tc.wantOK)
		}
	}
}

func TestCodexNoticeDue(t *testing.T) {
	for _, tc := range []struct {
		name        string
		count       any
		pct         float64
		pctPresent  bool
		noticeRound int
		codexLimit  int
		want        bool
	}{
		{name: "configured notice round at threshold", count: 4, pct: 60, pctPresent: true, noticeRound: 4, codexLimit: 5, want: true},
		{name: "below context point stays quiet", count: 4, pct: 59.9, pctPresent: true, noticeRound: 4, codexLimit: 5, want: false},
		{name: "other compaction round stays quiet", count: 3, pct: 80, pctPresent: true, noticeRound: 4, codexLimit: 5, want: false},
		{name: "nonpositive notice round falls back to the preceding limit round", count: 4, pct: 60, pctPresent: true, noticeRound: 0, codexLimit: 5, want: true},
		{name: "nonpositive limit falls back to the default limit", count: 2, pct: 60, pctPresent: true, noticeRound: 0, codexLimit: 0, want: true},
		{name: "noninteger compaction count is not a round", count: float64(4), pct: 80, pctPresent: true, noticeRound: 4, codexLimit: 5, want: false},
		{name: "missing pct stays quiet", count: 4, noticeRound: 4, codexLimit: 5, want: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var pct *float64
			if tc.pctPresent {
				value := tc.pct
				pct = &value
			}
			got := codexNoticeDue(map[string]any{"compaction_count": tc.count}, pct, tc.noticeRound, tc.codexLimit)
			if got != tc.want {
				t.Fatalf("codexNoticeDue(%v, %v, %d, %d) = %v, want %v", tc.count, pct, tc.noticeRound, tc.codexLimit, got, tc.want)
			}
		})
	}
}

func TestGaugeBootTS(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record map[string]any
		wantTS float64
		wantOK bool
	}{
		{name: "nil record", record: nil, wantOK: false},
		{name: "missing boot timestamp", record: map[string]any{}, wantOK: false},
		{name: "float timestamp", record: map[string]any{"boot_ts": 100.5}, wantTS: 100.5, wantOK: true},
		{name: "integer timestamp", record: map[string]any{"boot_ts": int(100)}, wantTS: 100, wantOK: true},
		{name: "non numeric timestamp", record: map[string]any{"boot_ts": "100"}, wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotTS, gotOK := gaugeBootTS(tc.record)
			if gotTS != tc.wantTS || gotOK != tc.wantOK {
				t.Fatalf("gaugeBootTS(%v) = %v, %v; want %v, %v", tc.record, gotTS, gotOK, tc.wantTS, tc.wantOK)
			}
		})
	}
}

func TestGaugeSecsSinceBoot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record map[string]any
		now    float64
		want   float64
		wantOK bool
	}{
		{name: "usable boot timestamp returns age", record: map[string]any{"boot_ts": 100.5}, now: 145.5, want: 45, wantOK: true},
		{name: "integer boot timestamp returns integer age", record: map[string]any{"boot_ts": int64(100)}, now: 145, want: 45, wantOK: true},
		{name: "missing boot timestamp fails open", record: map[string]any{}, now: 145, wantOK: false},
		{name: "non numeric boot timestamp fails open", record: map[string]any{"boot_ts": false}, now: 145, wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := gaugeSecsSinceBoot(tc.record, tc.now)
			if !tc.wantOK {
				if got != nil {
					t.Fatalf("gaugeSecsSinceBoot(%v, %v) = %v, want nil", tc.record, tc.now, *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				if got == nil {
					t.Fatalf("gaugeSecsSinceBoot(%v, %v) = nil, want %v", tc.record, tc.now, tc.want)
				}
				t.Fatalf("gaugeSecsSinceBoot(%v, %v) = %v, want %v", tc.record, tc.now, *got, tc.want)
			}
		})
	}
}

func TestActionableContextPct(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record map[string]any
		guard  bool
		want   float64
		wantOK bool
	}{
		{name: "guard off accepts a pct without timestamps", record: map[string]any{"context_pct": 45}, want: 45, wantOK: true},
		{name: "newer report drives the guarded decision", record: map[string]any{"context_pct": 55.5, "context_pct_ts": 101, "boot_ts": 100}, guard: true, want: 55.5, wantOK: true},
		{name: "equal report is stale", record: map[string]any{"context_pct": 55, "context_pct_ts": 100, "boot_ts": 100}, guard: true, wantOK: false},
		{name: "older report is stale", record: map[string]any{"context_pct": 55, "context_pct_ts": 99, "boot_ts": 100}, guard: true, wantOK: false},
		{name: "missing report timestamp is not actionable", record: map[string]any{"context_pct": 55, "boot_ts": 100}, guard: true, wantOK: false},
		{name: "non numeric pct is not actionable", record: map[string]any{"context_pct": "55"}, wantOK: false},
		{name: "nil record is not actionable", record: nil, wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := actionableContextPct(tc.record, tc.guard)
			if !tc.wantOK {
				if got != nil {
					t.Fatalf("actionableContextPct(%v, %v) = %v, want nil", tc.record, tc.guard, *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				if got == nil {
					t.Fatalf("actionableContextPct(%v, %v) = nil, want %v", tc.record, tc.guard, tc.want)
				}
				t.Fatalf("actionableContextPct(%v, %v) = %v, want %v", tc.record, tc.guard, *got, tc.want)
			}
		})
	}
}

func TestDecideHandoverNotice(t *testing.T) {
	t.Run("a quiet tick does not evaluate the notice source", func(t *testing.T) {
		calls := 0
		got := decideHandoverNotice(
			"agent-7", RuntimeClaude,
			map[string]any{"context_pct": 54, "context_pct_ts": 101, "boot_ts": 100},
			SseContextHighConfig{NoticePct: 55, HandoverPct: 65, StaleGuard: true},
			4, 5,
			func() string {
				calls++
				return "checkpoint"
			},
		)
		if got != nil {
			t.Fatalf("quiet decision = %+v, want nil", got)
		}
		if calls != 0 {
			t.Fatalf("notice source ran %d times, want 0", calls)
		}
	})

	t.Run("a Claude session at its configured notice point gets the full directed signal", func(t *testing.T) {
		calls := 0
		got := decideHandoverNotice(
			"agent-7", RuntimeClaude,
			map[string]any{"context_pct": 55, "context_pct_ts": 101, "boot_ts": 100},
			SseContextHighConfig{NoticePct: 55, HandoverPct: 65, StaleGuard: true},
			4, 5,
			func() string {
				calls++
				return "checkpoint this turn"
			},
		)
		want := &contextHighSignal{
			Topic:  contextHighTopic,
			To:     "agent-7",
			Level:  levelWarn,
			Pct:    jsonFloat(55),
			Reason: "checkpoint this turn",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("signal = %+v, want %+v", got, want)
		}
		if calls != 1 {
			t.Fatalf("notice source ran %d times, want 1", calls)
		}
	})

	t.Run("a Codex session uses its configured notice round and context point", func(t *testing.T) {
		got := decideHandoverNotice(
			"agent-8", RuntimeCodex,
			map[string]any{"context_pct": 60, "context_pct_ts": 101, "boot_ts": 100, "compaction_count": 4},
			SseContextHighConfig{StaleGuard: true},
			4, 5,
			func() string { return "save the checkpoint" },
		)
		want := &contextHighSignal{
			Topic:  contextHighTopic,
			To:     "agent-8",
			Level:  levelWarn,
			Pct:    jsonFloat(60),
			Reason: "save the checkpoint",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("signal = %+v, want %+v", got, want)
		}
	})

	t.Run("an empty notice leaves the tick quiet after the gate passes", func(t *testing.T) {
		got := decideHandoverNotice(
			"agent-7", RuntimeClaude,
			map[string]any{"context_pct": 55, "context_pct_ts": 101, "boot_ts": 100},
			SseContextHighConfig{NoticePct: 55, HandoverPct: 65, StaleGuard: true},
			4, 5,
			func() string { return "" },
		)
		if got != nil {
			t.Fatalf("empty notice signal = %+v, want nil", got)
		}
	})
}

func TestFormatPct(t *testing.T) {
	for _, tc := range []struct {
		name string
		pct  float64
		want any
	}{
		{name: "whole percentage uses an integer value", pct: 45, want: int64(45)},
		{name: "fractional percentage stays fractional", pct: 45.5, want: float64(45.5)},
		{name: "zero uses an integer value", pct: 0, want: int64(0)},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := formatPct(tc.pct); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("formatPct(%v) = %T(%v), want %T(%v)", tc.pct, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestTokenExpiryRemaining(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims map[string]any
		now    int64
		want   int64
		wantOK bool
	}{
		{name: "future float expiry returns remaining seconds", claims: map[string]any{"exp": 1100.0}, now: 1000, want: 100, wantOK: true},
		{name: "future integer expiry returns remaining seconds", claims: map[string]any{"exp": int64(1100)}, now: 1000, want: 100, wantOK: true},
		{name: "expiry at now is unusable", claims: map[string]any{"exp": 1000.0}, now: 1000, wantOK: false},
		{name: "expired token is unusable", claims: map[string]any{"exp": 999.0}, now: 1000, wantOK: false},
		{name: "missing expiry is unusable", claims: map[string]any{}, now: 1000, wantOK: false},
		{name: "non numeric expiry is unusable", claims: map[string]any{"exp": "1100"}, now: 1000, wantOK: false},
		{name: "nil claims are unusable", claims: nil, now: 1000, wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, gotOK := tokenExpiryRemaining(tc.claims, tc.now)
			if got != tc.want || gotOK != tc.wantOK {
				t.Fatalf("tokenExpiryRemaining(%v, %d) = %d, %v; want %d, %v", tc.claims, tc.now, got, gotOK, tc.want, tc.wantOK)
			}
		})
	}
}

func TestTokenExpiryClaims(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exp    float64
		now    int64
		want   int64
		wantOK bool
	}{
		{name: "at the warning boundary is included", exp: 2800, now: 1000, want: 1800, wantOK: true},
		{name: "inside the warning window is included", exp: 1500, now: 1000, want: 500, wantOK: true},
		{name: "outside the warning window is excluded", exp: 2801, now: 1000, wantOK: false},
		{name: "expired claims are excluded", exp: 999, now: 1000, wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, gotOK := tokenExpiryClaims(map[string]any{"exp": tc.exp}, tc.now)
			if got != tc.want || gotOK != tc.wantOK {
				t.Fatalf("tokenExpiryClaims(%v, %d) = %d, %v; want %d, %v", tc.exp, tc.now, got, gotOK, tc.want, tc.wantOK)
			}
		})
	}
}

func TestTokenExpiryNextCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims map[string]any
		now    int64
		want   int64
	}{
		{name: "invalid claims use the reminder cadence", claims: nil, now: 1000, want: 1030},
		{name: "far expiry wakes at the warning boundary", claims: map[string]any{"exp": 2801.0}, now: 1000, want: 1001},
		{name: "boundary expiry uses the reminder cadence", claims: map[string]any{"exp": 2800.0}, now: 1000, want: 1030},
		{name: "near expiry uses the reminder cadence", claims: map[string]any{"exp": 1100.0}, now: 1000, want: 1030},
		{name: "expired claims use the reminder cadence", claims: map[string]any{"exp": 999.0}, now: 1000, want: 1030},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenExpiryNextCheck(tc.claims, tc.now); got != tc.want {
				t.Fatalf("tokenExpiryNextCheck(%v, %d) = %d, want %d", tc.claims, tc.now, got, tc.want)
			}
		})
	}
}

func TestDecideTokenExpirySignal(t *testing.T) {
	for _, tc := range []struct {
		name         string
		member       *Member
		gauge        map[string]any
		claims       map[string]any
		now          int64
		lastReminder int64
		wantSignal   bool
		wantLast     int64
		wantExpiry   int64
	}{
		{
			name:       "restartable member receives a warning",
			member:     &Member{ID: "agent-1", Kind: KindStaff},
			claims:     map[string]any{"exp": 1500.0},
			now:        1000,
			wantSignal: true,
			wantLast:   1000,
			wantExpiry: 500,
		},
		{name: "missing member stays quiet", claims: map[string]any{"exp": 1500.0}, now: 1000, wantLast: 0},
		{name: "warden stays quiet", member: &Member{ID: "warden-1", Kind: KindWarden}, claims: map[string]any{"exp": 1500.0}, now: 1000, lastReminder: 970, wantLast: 970},
		{name: "member in refocus stays quiet", member: &Member{ID: "agent-1", Kind: KindStaff, RefocusSince: 900}, claims: map[string]any{"exp": 1500.0}, now: 1000, lastReminder: 970, wantLast: 970},
		{name: "fresh session stays quiet during the liveness floor", member: &Member{ID: "agent-1", Kind: KindStaff}, gauge: map[string]any{"boot_ts": 900.0}, claims: map[string]any{"exp": 1500.0}, now: 1000, lastReminder: 970, wantLast: 970},
		{name: "far expiry stays quiet", member: &Member{ID: "agent-1", Kind: KindStaff}, claims: map[string]any{"exp": 3001.0}, now: 1000, lastReminder: 970, wantLast: 970},
		{name: "invalid expiry stays quiet", member: &Member{ID: "agent-1", Kind: KindStaff}, claims: map[string]any{"exp": "1500"}, now: 1000, lastReminder: 970, wantLast: 970},
		{name: "recent reminder stays quiet", member: &Member{ID: "agent-1", Kind: KindStaff}, claims: map[string]any{"exp": 1500.0}, now: 1000, lastReminder: 980, wantLast: 980},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, gotLast := decideTokenExpirySignal("agent-1", tc.claims, tc.member, tc.gauge, tc.now, tc.lastReminder)
			if gotLast != tc.wantLast {
				t.Fatalf("last reminder = %d, want %d", gotLast, tc.wantLast)
			}
			if !tc.wantSignal {
				if got != nil {
					t.Fatalf("signal = %+v, want nil", got)
				}
				return
			}
			want := &tokenExpirySignal{
				Topic:     tokenExpiryTopic,
				To:        "agent-1",
				ExpiresIn: tc.wantExpiry,
				Reason:    "agent token expires in 500s; checkpoint this turn, then call restart_self to receive a fresh token",
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("signal = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDirectedFrameText(t *testing.T) {
	t.Run("wraps the directed payload as a data-only SSE frame", func(t *testing.T) {
		got, err := directedFrameText("context-high", contextHighSignal{
			Topic:  contextHighTopic,
			To:     "agent-1",
			Level:  levelWarn,
			Pct:    jsonFloat(45),
			Reason: "checkpoint",
		})
		if err != nil {
			t.Fatalf("directedFrameText: %v", err)
		}
		want := "data: {\"topic\":\"context-high\",\"data\":{\"topic\":\"context-high\",\"to\":\"agent-1\",\"level\":\"warn\",\"pct\":45.0,\"reason\":\"checkpoint\"}}\n\n"
		if string(got) != want {
			t.Fatalf("frame = %q, want %q", got, want)
		}
	})

	t.Run("returns the marshal error for an unsupported payload", func(t *testing.T) {
		got, err := directedFrameText("context-high", func() {})
		if err == nil {
			t.Fatal("unsupported payload must return an error")
		}
		if got != nil {
			t.Fatalf("frame = %q, want nil", got)
		}
	})
}

func TestDecideTaskCloseNudge(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		executor string
		want     bool
	}{
		{name: "done task with executor gets a nudge", status: TaskStatusDone, executor: "agent-1", want: true},
		{name: "terminated task with executor gets a nudge", status: TaskStatusTerminated, executor: "agent-1", want: true},
		{name: "duplicated task with executor gets a nudge", status: TaskStatusDuplicated, executor: "agent-1", want: true},
		{name: "open task stays quiet", status: TaskStatusInProgress, executor: "agent-1", want: false},
		{name: "unassigned terminal task stays quiet", status: TaskStatusDone, want: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := decideTaskCloseNudge(Task{
				ID:         "T-125",
				TypeKey:    "tm-05f7c776d6ff",
				Status:     tc.status,
				ExecutorID: tc.executor,
			})
			if !tc.want {
				if got != nil {
					t.Fatalf("nudge = %+v, want nil", got)
				}
				return
			}
			want := &taskCloseSignal{
				Topic:  taskCloseTopic,
				To:     "agent-1",
				TaskID: "T-125",
				TaskNo: "T-125",
				Type:   "tm-05f7c776d6ff",
				Status: tc.status,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("nudge = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDecodeWardenCommandFrame(t *testing.T) {
	for _, tc := range []struct {
		name        string
		frame       string
		want        wardenCommandDigest
		wantDecoded bool
	}{
		{
			name:        "valid command frame returns its verb and member",
			frame:       "data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"stop\",\"args\":{\"member_id\":\"agent-1\"}}}\n\n",
			want:        wardenCommandDigest{Verb: "stop", MemberID: "agent-1"},
			wantDecoded: true,
		},
		{
			name:        "surrounding whitespace is accepted",
			frame:       "  data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"renew\",\"args\":{\"member_id\":\"agent-2\"}}}\n\n  ",
			want:        wardenCommandDigest{Verb: "renew", MemberID: "agent-2"},
			wantDecoded: true,
		},
		{name: "wrong topic is rejected", frame: "data: {\"topic\":\"context-high\",\"data\":{}}\n\n", wantDecoded: false},
		{name: "malformed JSON is rejected", frame: "data: {not-json}\n\n", wantDecoded: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, decoded := decodeWardenCommandFrame([]byte(tc.frame))
			if decoded != tc.wantDecoded || got != tc.want {
				t.Fatalf("decodeWardenCommandFrame(%q) = %+v, %v; want %+v, %v", tc.frame, got, decoded, tc.want, tc.wantDecoded)
			}
		})
	}
}
