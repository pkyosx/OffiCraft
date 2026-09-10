package main

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestJudgeCutoverEffect(t *testing.T) {
	sampled := time.Unix(1700000000, 0)
	born := sampled.Add(-time.Hour)

	cases := []struct {
		name  string
		probe carrierProbe
		want  cutoverEffect
	}{
		{"a carrier younger than the leader, on the anchor shape, is proof", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 300}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectEffective},
		{"several carriers all younger than the leader are proof", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 300}, {pid: 901, elapsed: 599}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectEffective},
		{"a carrier that predates the anchor file is the deterministic negative", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 7200}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectNotEffective},
		{"one carrier predating the anchor condemns the machine even beside a young one", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 300}, {pid: 901, elapsed: 7200}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectNotEffective},
		{"an unread anchor birth time is no evidence, so no accusation", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 7200}},
			sampledAt: sampled,
		}, effectUnproven},
		{"a carrier born exactly with the anchor is not older than it", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 7200,
			carriers:  []carrier{{pid: 900, elapsed: 3600}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectEffective},
		{"a probe that could not be completed proves nothing", carrierProbe{
			shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 300}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
		{"a legacy machine is not judged on its carriers", carrierProbe{
			ok: true, shape: shapeLegacy, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 7200}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
		{"an unknown shape is not judged either", carrierProbe{
			ok: true, shape: shapeUnknown, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 300}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
		{"no carriers at all is not a vacuous green", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
		{"a carrier the same age as the leader cannot be ordered against it", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 600}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
		{"a carrier older than the leader is amber, not red", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 601}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
		{"an unreadable leader age cannot order anything", carrierProbe{
			ok: true, shape: shapeAnchor, leaderElapsed: 0,
			carriers:  []carrier{{pid: 900, elapsed: 300}},
			sampledAt: sampled, anchorBirth: born, anchorBirthKnown: true,
		}, effectUnproven},
	}
	for _, c := range cases {
		if got := judgeCutoverEffect(c.probe); got != c.want {
			t.Errorf("%s: judgeCutoverEffect = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTmuxServerPID(t *testing.T) {
	const socket = "officraft"
	argv := argvKey("tmux", "-L", socket, "display-message", "-p", "#{pid}")

	cases := []struct {
		name string
		out  string
		err  error
		want int
	}{
		{"a running server answers with its pid", "4242\n", nil, 4242},
		{"a pid with surrounding whitespace", "  4242  ", nil, 4242},
		{"no server on the socket", "", errors.New("no server running on /tmp/tmux-501/officraft"), 0},
		{"an answer that is not a number", "not-a-pid\n", nil, 0},
		{"an empty answer", "", nil, 0},
		{"a signed number is not a pid", "-1\n", nil, 0},
	}
	for _, c := range cases {
		rec := newCutoverRec()
		if c.err != nil {
			rec.runErr[argv] = c.err
		} else {
			rec.runOut[argv] = c.out
		}
		ops := rec.ops()
		if got := tmuxServerPID(ops.run, socket); got != c.want {
			t.Errorf("%s: tmuxServerPID = %d, want %d", c.name, got, c.want)
		}
		if want := []string{argv}; !reflect.DeepEqual(rec.runs, want) {
			t.Errorf("%s: ran %v, want %v", c.name, rec.runs, want)
		}
	}
}

func TestTmuxMemberSessionCount(t *testing.T) {
	const socket = "officraft"
	argv := argvKey("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")

	cases := []struct {
		name       string
		out        string
		err        error
		want       int
		wantListed bool
	}{
		{"member sessions are counted, others are not",
			"member-alice\nmember-bob\nocserver\n", nil, 2, true},
		{"a socket carrying nothing but bookkeeping sessions",
			"ocserver\nscratch\n", nil, 0, true},
		{"leading whitespace does not hide a member",
			"  member-alice\n", nil, 1, true},
		{"a name that merely starts like one",
			"members-of-the-board\nmember-alice\n", nil, 1, true},
		{"no server at all is a readable zero",
			"", errors.New("no server running on /tmp/tmux-501/officraft"), 0, true},
		{"a socket whose file is gone is a readable zero",
			"", errors.New("error connecting to /tmp/tmux-501/officraft (No such file or directory)"), 0, true},
		{"a probe that broke is NOT zero sessions",
			"", errors.New("tmux: command not found"), 0, false},
		{"an empty listing from a live server",
			"", nil, 0, true},
	}
	for _, c := range cases {
		rec := newCutoverRec()
		if c.err != nil {
			rec.runErr[argv] = c.err
		} else {
			rec.runOut[argv] = c.out
		}
		ops := rec.ops()
		got, listed := tmuxMemberSessionCount(ops.run, socket)
		if got != c.want || listed != c.wantListed {
			t.Errorf("%s: tmuxMemberSessionCount = (%d, %v), want (%d, %v)", c.name, got, listed, c.want, c.wantListed)
		}
	}
}

func TestProcessElapsedSecs(t *testing.T) {
	t.Run("a live pid's age is read with the portable etime field", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runOut[argvKey("ps", "-p", "4242", "-o", "etime=")] = "   01:02:03\n"
		ops := rec.ops()

		got, ok := processElapsedSecs(ops.run, 4242)

		if got != 3723 || !ok {
			t.Errorf("processElapsedSecs = (%d, %v), want (3723, true)", got, ok)
		}
		if want := []string{"ps -p 4242 -o etime="}; !reflect.DeepEqual(rec.runs, want) {
			t.Errorf("ran %v, want %v — etimes is a GNU extension BSD ps does not have", rec.runs, want)
		}
	})

	cases := []struct {
		name string
		pid  int
		out  string
		err  error
	}{
		{"pid zero is not a process", 0, "", nil},
		{"a negative pid is not a process", -1, "", nil},
		{"a pid ps cannot read", 4242, "", errors.New("ps: no such process")},
		{"an unparseable age", 4242, "not-a-time\n", nil},
		{"an age of zero is not an age", 4242, "00:00\n", nil},
		{"an empty answer", 4242, "", nil},
	}
	for _, c := range cases {
		rec := newCutoverRec()
		if c.err != nil {
			rec.runErr[argvKey("ps", "-p", "4242", "-o", "etime=")] = c.err
		} else {
			rec.runOut[argvKey("ps", "-p", "4242", "-o", "etime=")] = c.out
		}
		ops := rec.ops()
		got, ok := processElapsedSecs(ops.run, c.pid)
		if got != 0 || ok {
			t.Errorf("%s: processElapsedSecs = (%d, %v), want (0, false) — never a guess", c.name, got, ok)
		}
	}
}

func TestParseEtime(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"00:05", 5, true},
		{"01:00", 60, true},
		{"59:59", 3599, true},
		{"1:02:03", 3723, true},
		{"01:02:03", 3723, true},
		{"23:59:59", 86399, true},
		{"9-01:02:03", 781323, true},
		{"01-00:00:00", 86400, true},
		{"1048576-00:00:00", 90596966400, true},
		{"1048577-00:00:00", 0, false},
		{"", 0, false},
		{"5", 0, false},
		{"00", 0, false},
		{"1:2:3:4", 0, false},
		{"aa:bb", 0, false},
		{"00:60", 0, false},
		{"60:00", 0, false},
		{"24:00:00", 0, false},
		{"00:00:100", 0, false},
		{"0:5", 0, false},
		{"-1:00", 0, false},
		{"1-00:00", 0, false},
		{"1-05", 0, false},
		{"1234567890-00:00:00", 0, false},
		{" 01:02", 0, false},
	}
	for _, c := range cases {
		got, ok := parseEtime(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("parseEtime(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestEtimeField(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		minDigits int
		maxDigits int
		max       int
		want      int
		wantOK    bool
	}{
		{"a two-digit seconds field", "07", 2, 2, 59, 7, true},
		{"the upper bound itself is allowed", "59", 2, 2, 59, 59, true},
		{"one over the bound is refused", "60", 2, 2, 59, 0, false},
		{"a one-digit hours field", "5", 1, 2, 23, 5, true},
		{"a two-digit hours field", "05", 1, 2, 23, 5, true},
		{"too few digits", "7", 2, 2, 59, 0, false},
		{"too many digits", "007", 2, 2, 59, 0, false},
		{"an empty field", "", 1, 2, 23, 0, false},
		{"a signed field", "-5", 1, 2, 23, 0, false},
		{"a field with a space", " 5", 1, 2, 23, 0, false},
		{"a non-digit field", "ab", 1, 2, 23, 0, false},
		{"a nine-digit days field", "123456789", 1, 9, 1 << 20, 0, false},
		{"a days field at its bound", "1048576", 1, 9, 1 << 20, 1048576, true},
		{"zero is a legal field", "00", 2, 2, 59, 0, true},
	}
	for _, c := range cases {
		got, ok := etimeField(c.in, c.minDigits, c.maxDigits, c.max)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: etimeField(%q, %d, %d, %d) = (%d, %v), want (%d, %v)",
				c.name, c.in, c.minDigits, c.maxDigits, c.max, got, ok, c.want, c.wantOK)
		}
	}
}

func TestAtoiStrict(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"0", 0},
		{"7", 7},
		{"4242", 4242},
		{"0004242", 4242},
		{"", 0},
		{" 7", 0},
		{"7 ", 0},
		{"+7", 0},
		{"-7", 0},
		{"7a", 0},
		{"a7", 0},
		{"7.0", 0},
		{"99999999999999999999", 0},
	}
	for _, c := range cases {
		if got := atoiStrict(c.in); got != c.want {
			t.Errorf("atoiStrict(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSampleCarrierProbe(t *testing.T) {
	const (
		anchor = "/Users/eva/.officraft/warden/officraft"
		socket = "officraft"
		ppid   = 4242
	)
	now := time.Unix(1700000000, 0)
	anchorBorn := time.Unix(1699000000, 0)

	// convertedHost is a machine on the anchor shape carrying one member session.
	convertedHost := func() *cutoverRec {
		rec := newCutoverRec()
		rec.ppidExe[ppid] = anchor
		rec.runOut[argvKey("ps", "-p", "4242", "-o", "etime=")] = "10:00\n"
		rec.runOut[argvKey("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")] = "member-alice\n"
		rec.runOut[argvKey("tmux", "-L", socket, "display-message", "-p", "#{pid}")] = "900\n"
		rec.runOut[argvKey("ps", "-p", "900", "-o", "etime=")] = "05:00\n"
		rec.births[anchor] = anchorBorn
		return rec
	}

	t.Run("every operand is read and reported without judgement", func(t *testing.T) {
		rec := convertedHost()

		got := sampleCarrierProbe(rec.ops(), anchor, socket, ppid, now)

		want := carrierProbe{
			shape: shapeAnchor, leaderElapsed: 600,
			carriers:         []carrier{{pid: 900, elapsed: 300}},
			anchorBirth:      anchorBorn,
			anchorBirthKnown: true,
			sampledAt:        now,
			ok:               true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("probe = %#v, want %#v", got, want)
		}
	})

	t.Run("a machine with no member sessions has no carriers but is still a complete sample", func(t *testing.T) {
		rec := convertedHost()
		rec.runOut[argvKey("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")] = "ocserver\n"

		got := sampleCarrierProbe(rec.ops(), anchor, socket, ppid, now)

		want := carrierProbe{
			shape: shapeAnchor, leaderElapsed: 600,
			anchorBirth: anchorBorn, anchorBirthKnown: true,
			sampledAt: now, ok: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("probe = %#v, want %#v", got, want)
		}
	})

	t.Run("an unreadable anchor birth time still completes the sample", func(t *testing.T) {
		rec := convertedHost()
		delete(rec.births, anchor)

		got := sampleCarrierProbe(rec.ops(), anchor, socket, ppid, now)

		want := carrierProbe{
			shape: shapeAnchor, leaderElapsed: 600,
			carriers:  []carrier{{pid: 900, elapsed: 300}},
			sampledAt: now, ok: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("probe = %#v, want %#v — an unread birth time is not a birth time of zero", got, want)
		}
	})

	t.Run("a legacy machine is still sampled in full", func(t *testing.T) {
		rec := convertedHost()
		rec.ppidExe[ppid] = "/sbin/launchd"

		got := sampleCarrierProbe(rec.ops(), anchor, socket, ppid, now)

		want := carrierProbe{
			shape: shapeLegacy, leaderElapsed: 600,
			carriers:         []carrier{{pid: 900, elapsed: 300}},
			anchorBirth:      anchorBorn,
			anchorBirthKnown: true,
			sampledAt:        now, ok: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("probe = %#v, want %#v", got, want)
		}
	})

	t.Run("every unreadable operand leaves an incomplete sample", func(t *testing.T) {
		cases := []struct {
			name string
			mut  func(*cutoverRec)
			want carrierProbe
		}{
			{"the leader's age cannot be read",
				func(r *cutoverRec) {
					delete(r.runOut, argvKey("ps", "-p", "4242", "-o", "etime="))
				},
				carrierProbe{shape: shapeAnchor, sampledAt: now}},
			{"the session list is unreadable",
				func(r *cutoverRec) {
					delete(r.runOut, argvKey("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}"))
					r.runErr[argvKey("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")] =
						errors.New("tmux: command not found")
				},
				carrierProbe{shape: shapeAnchor, leaderElapsed: 600, sampledAt: now}},
			{"the tmux server's own age cannot be read",
				func(r *cutoverRec) {
					delete(r.runOut, argvKey("ps", "-p", "900", "-o", "etime="))
				},
				carrierProbe{shape: shapeAnchor, leaderElapsed: 600, sampledAt: now}},
			{"the tmux server pid is unreadable while sessions exist",
				func(r *cutoverRec) {
					delete(r.runOut, argvKey("tmux", "-L", socket, "display-message", "-p", "#{pid}"))
				},
				carrierProbe{shape: shapeAnchor, leaderElapsed: 600, sampledAt: now}},
		}
		for _, c := range cases {
			rec := convertedHost()
			c.mut(rec)
			got := sampleCarrierProbe(rec.ops(), anchor, socket, ppid, now)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("%s: probe = %#v, want %#v", c.name, got, c.want)
			}
			if judgeCutoverEffect(got) != effectUnproven {
				t.Errorf("%s: an incomplete sample judged %q, want %q", c.name, judgeCutoverEffect(got), effectUnproven)
			}
		}
	})

	t.Run("an unconfigured anchor path or socket reads nothing at all", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			anchor string
			socket string
		}{
			{"no anchor path", "", socket},
			{"no socket", anchor, ""},
			{"neither", "", ""},
		} {
			rec := convertedHost()
			got := sampleCarrierProbe(rec.ops(), c.anchor, c.socket, ppid, now)
			if want := (carrierProbe{sampledAt: now}); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: probe = %#v, want %#v", c.name, got, want)
			}
			if len(rec.runs) != 0 {
				t.Errorf("%s: ran %v, want nothing", c.name, rec.runs)
			}
		}
	})
}

func TestNewCutoverEffectReporter(t *testing.T) {
	const (
		anchor = "/Users/eva/.officraft/warden/officraft"
		socket = "officraft"
		ppid   = 4242
	)
	rec := newCutoverRec()
	rec.ppidExe[ppid] = anchor
	rec.runOut[argvKey("ps", "-p", "4242", "-o", "etime=")] = "10:00\n"
	rec.runOut[argvKey("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")] = "member-alice\n"
	rec.runOut[argvKey("tmux", "-L", socket, "display-message", "-p", "#{pid}")] = "900\n"
	rec.runOut[argvKey("ps", "-p", "900", "-o", "etime=")] = "15:00\n"
	rec.births[anchor] = time.Now().Add(-time.Hour)
	bindCutoverOps(t, rec)

	report := newCutoverEffectReporter(anchor, socket, ppid)

	if got := report(); got != "unproven" {
		t.Errorf("a carrier older than the leader reported %q, want %q", got, "unproven")
	}

	rec.runOut[argvKey("ps", "-p", "900", "-o", "etime=")] = "05:00\n"
	if got := report(); got != "effective" {
		t.Errorf("after the carrier was replaced the reporter said %q, want %q — it must re-sample every cycle", got, "effective")
	}

	rec.runOut[argvKey("ps", "-p", "900", "-o", "etime=")] = "9-00:00:00\n"
	if got := report(); got != "not_effective" {
		t.Errorf("a carrier predating the anchor reported %q, want %q", got, "not_effective")
	}
}
