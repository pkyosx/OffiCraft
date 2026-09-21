// cutovereffect.go — T-17b4: is the anchor cutover ACTUALLY IN EFFECT for the
// processes that carry agents?
//
// WHY THIS EXISTS (the incident this file is the answer to)
// ---------------------------------------------------------
// On 2026-07-31 a machine completed the legacy→anchor cutover, the monitor page
// showed a green `anchor` badge from 11:58 onwards, and the cutover had no
// effect whatsoever: every agent was hanging off a tmux server created NINE DAYS
// earlier, so the TCC responsible process for all of them was still the old
// swappable ocwarden. A hand `tmux kill-server` at 14:52 is what actually made it
// take effect.
//
// The green badge was NOT a fabrication and NOT a plist self-report: warden_shape
// (cutover.go detectShape) reads the live PARENT PROCESS's exe, which is about as
// honest a signal as exists. It was simply answering a different question than
// the one people read off it — "who is WARDEN's parent right now" instead of
// "who is responsible for the AGENT processes". Those two populations diverge the
// moment launchd restarts warden under the new identity while the tmux server
// that carries the agents keeps running under the old one.
//
// So the lesson here is narrower and harder than "do not trust self-reports": a
// 100% honest, 100% live observation of the WRONG SUBJECT is still a false green.
// This file therefore names its subject explicitly — the tmux server processes
// that CARRY agent sessions — and proves the carriers are younger than the anchor
// identity they are supposed to have inherited.
//
// THE VERDICT IS THREE-VALUED, ON PURPOSE
// ----------------------------------------
// The damage in the incident came from a BOOLEAN light. `unproven` is a first
// class verdict, not an implementation detail: folding it into `effective` is
// committing the same bug again. Every unreadable input folds toward `unproven`
// (fail-closed), and only the deterministic negative below ever produces
// `not_effective`.
//
// WHAT IT DOES NOT DO
// -------------------
// It only makes the state VISIBLE. It never restarts a tmux server, never
// interrupts an agent and never suggests an automatic repair — the owner ruled
// that option out explicitly. Deciding when to act on a `not_effective` machine
// is a human's job.
package main

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// errNoBirthTime is what statBirthTime returns where the platform cannot answer.
var errNoBirthTime = errors.New("this platform does not expose an inode birth time")

// cutoverEffect is the three-valued verdict. The zero value is deliberately not
// one of these: a caller that forgets to set it gets "" and every consumer reads
// an unset verdict as "not reported", never as green.
type cutoverEffect string

const (
	// effectEffective: PROVEN. launchd is running the anchor right now, and every
	// process carrying an agent session is younger than the current anchor job
	// leader — so it can only have been forked under the anchor identity.
	effectEffective cutoverEffect = "effective"
	// effectNotEffective: PROVEN OTHERWISE. A carrier is older than the anchor
	// FILE itself, so it cannot possibly hold the anchor identity. This is the
	// only deterministic negative available, and it is used ONLY in the negative
	// direction (see below).
	effectNotEffective cutoverEffect = "not_effective"
	// effectUnproven: cannot be shown either way. NOT a synonym for green and
	// never to be rendered like one.
	effectUnproven cutoverEffect = "unproven"
)

// carrierProbe is the one sample this verdict is computed from. Everything is
// read inside a single sampling window and through the cutoverOps seam, so a
// test binary can never reach a real ps/tmux/stat.
type carrierProbe struct {
	// shape is the live detectShape verdict (C1's operand).
	shape wardenShape
	// leaderElapsed is E(L): how many seconds the launchd job leader (the anchor
	// process, warden's parent) has been running.
	leaderElapsed int
	// carriers is C: the tmux server processes on this instance's socket that
	// carry at least one member-* session, with their elapsed seconds.
	carriers []carrier
	// anchorBirth is B: the anchor file's inode birth time — the moment this
	// machine's anchor identity came into existence. The anchor is never
	// rewritten once installed (ensureAnchorPresent promotes via create-if-absent
	// os.Link), so its birthtime is a filesystem FACT about that identity rather
	// than anyone's claim about it. Only meaningful when anchorBirthKnown.
	anchorBirth time.Time
	// anchorBirthKnown says whether B was actually read. It is a SEPARATE field
	// rather than "anchorBirth is the zero time" because the zero time is a real
	// instant in year 1: every carrier is born after it, so a sentinel makes the
	// "B is unavailable" branch indistinguishable from "B is very old" by
	// outcome, and therefore untestable — a guard no test can redden is a guard
	// that can be deleted by accident. Off darwin this is always false
	// (birthtime_other.go), which is also CI's platform.
	anchorBirthKnown bool
	// sampledAt is the wall clock of this sample, used ONLY to turn a carrier's
	// elapsed seconds into a creation instant for the negative comparison.
	sampledAt time.Time
	// ok is false when ANY operand above could not be read. Fail-closed: the
	// verdict is then unproven regardless of what the readable half says.
	ok bool
}

type carrier struct {
	pid     int
	elapsed int
}

// judgeCutoverEffect is the decision, kept free of I/O so it can be tested as a
// truth table.
//
//	C1  shape == anchor                 launchd really is running the anchor NOW
//	C2  forall c in C: E(c) < E(L)      every carrier is younger than the leader
//	C3  C is non-empty                  no vacuous truth over an empty carrier set
//
//	effective      <=> C1 and C2 and C3
//	not_effective  <=  C1 and exists c in C whose creation instant precedes B
//	otherwise         unproven
//
// WHY C2 COMPARES ELAPSED SECONDS AND NOT WALL CLOCKS: both operands come from
// the same sample of the same clock and are subtracted, so time zones, NTP
// steps, DST and sleep-induced jumps cannot touch the comparison. The obvious
// alternative — "carrier created before the cutover finished" — needs the
// cutover log's UTC timestamp against ps's local time, an 8h systematic skew on
// this fleet that can invert the comparison outright on a same-day cutover. The
// green light must never depend on a clock.
//
// WHY C2 FAILING IS ONLY `unproven`: E(L) is the CURRENT leader's age, not "when
// this machine first ran the anchor". A `launchctl kickstart -k` or a reboot
// makes the leader young again, so a perfectly legitimate anchor-born carrier
// can look older than it. That is a false negative, which is safe in direction
// but noisy — hence amber, not red.
//
// WHY THE NEGATIVE USES B AND NOT E(L): a carrier older than the anchor FILE
// cannot have inherited an identity that did not exist yet. That conclusion
// survives any number of leader restarts. B is used ONLY here: it is a LOWER
// BOUND on the identity's existence, not the cutover's completion (the anchor is
// materialised before the lock is taken, so even a refused conversion leaves the
// file behind), so using it in the positive direction would green-light a
// carrier forked in the window between "anchor file exists" and "plist swapped".
func judgeCutoverEffect(p carrierProbe) cutoverEffect {
	if !p.ok {
		return effectUnproven
	}
	if p.shape != shapeAnchor {
		return effectUnproven
	}
	// Deterministic negative first: a carrier predating the anchor file is proof,
	// and it must not be masked by C2/C3 also failing. It requires B to have been
	// READ — an unread B is not "the beginning of time", it is no evidence, and
	// accusing a machine on no evidence is the mirror image of the false green.
	if p.anchorBirthKnown {
		for _, c := range p.carriers {
			born := p.sampledAt.Add(-time.Duration(c.elapsed) * time.Second)
			if born.Before(p.anchorBirth) {
				return effectNotEffective
			}
		}
	}
	if len(p.carriers) == 0 {
		return effectUnproven
	}
	// An unreadable leader age (processElapsedSecs never guesses, so it arrives
	// as 0) cannot order anything.
	//
	// ⚠️ HONEST NOTE, because the alternative is a comment that lies: mutation
	// testing shows NO test reddens when this check alone is deleted, and that is
	// not a coverage gap to be papered over with a new case — it is arithmetic.
	// C2 below rejects any carrier with `elapsed >= leaderElapsed`, and every
	// carrier age is positive by construction, so a zero or negative leader age
	// already fails C2 for every carrier. The check is kept anyway, as intent:
	// without it, C2's comparison silently becomes load-bearing for a second,
	// unrelated purpose, and the next person to touch that line has no way to
	// know it. The fail-closed behaviour ITSELF is pinned twice — by the "leader's
	// age is unavailable" row in the judge's table and by the sampler's own
	// refusal when processElapsedSecs fails.
	//
	// 🔴 READ THIS BEFORE TOUCHING C2. The reason nothing guards this line is that
	// C2 currently rejects every carrier with `elapsed >= leaderElapsed`. LOOSEN
	// C2 — to `>`, to a subset of carriers, to anything — and this check stops
	// being redundant and becomes the ONLY thing standing between an unreadable
	// leader age and a green verdict, with no test watching it. If you widen C2,
	// come back here and write the case that was impossible to write today.
	if p.leaderElapsed <= 0 {
		return effectUnproven
	}
	for _, c := range p.carriers {
		if c.elapsed >= p.leaderElapsed {
			return effectUnproven
		}
	}
	return effectEffective
}

// tmuxServerPID returns the pid of the tmux server on socket, or 0 when there is
// no server / the probe is unreadable. `display-message -p '#{pid}'` is a
// server-scope format, so it answers without naming a session.
func tmuxServerPID(run func(string, ...string) (string, error), socket string) int {
	out, err := run("tmux", "-L", socket, "display-message", "-p", "#{pid}")
	if err != nil {
		return 0
	}
	return atoiStrict(strings.TrimSpace(out))
}

// tmuxMemberSessionCount reports how many member-* sessions live on socket, and
// whether the enumeration itself was readable. An unreadable enumeration is NOT
// "zero sessions": the two are different claims, and only one of them is allowed
// to influence a verdict.
func tmuxMemberSessionCount(run func(string, ...string) (string, error), socket string) (int, bool) {
	out, err := run("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		// "no server running on ..." is a clean, positive absence — zero sessions.
		if tmuxClassifyAbsent(err.Error()) {
			return 0, true
		}
		return 0, false
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), memberSessionPrefix) {
			n++
		}
	}
	return n, true
}

// psElapsedArgs is the ONE definition of the argv that reads a process's age.
// Production calls it and the test fake keys its scripted answers off it, so
// the two can never end up describing different commands — which is exactly how
// the defect documented on processElapsedSecs stayed green.
//
// Sharing the definition is only half of the guard, because a shared WRONG argv
// is still wrong in both places at once. The other half is
// bin/tests/ps-field-support-guard.sh, which reads the `-o <field>=` literal out
// of this call site and runs the real `ps` to prove that string is something
// this host actually understands.
func psElapsedArgs(pid int) []string {
	return []string{"-p", strconv.Itoa(pid), "-o", "etime="}
}

// processElapsedSecs reads a pid's elapsed running seconds.
//
// 🔴 `-o etime=`, NOT `-o etimes=`. The seconds-valued `etimes` is a GNU/procps
// extension that BSD ps does not have, and this warden only ever runs on macOS
// (the anchor identity question is a TCC question). There, `ps -p 1 -o etimes=`
// exits 1 with "keyword not found" — so the original reader could not read ANY
// age on the only platform it runs on. Every machine in the fleet folded to
// `unproven` forever: the three-valued light this file exists to provide had
// exactly one reachable value, and the red state — the incident it all answers
// to — could never appear. The portable field costs one parse.
//
// The unit suite could not catch that, and worse, endorsed it: the fake was
// keyed on the same wrong argv, so it answered the broken flag politely and the
// suite went green. Evidence existing is not the same as evidence bearing on
// the thing you are trying to prove.
//
// Returns (0,false) on any fault or unparseable output — never a guess, because
// a fabricated age is exactly how a false green would be manufactured.
func processElapsedSecs(run func(string, ...string) (string, error), pid int) (int, bool) {
	if pid <= 0 {
		return 0, false
	}
	out, err := run("ps", psElapsedArgs(pid)...)
	if err != nil {
		return 0, false
	}
	n, ok := parseEtime(strings.TrimSpace(out))
	if !ok || n <= 0 {
		return 0, false
	}
	return n, true
}

// parseEtime converts ps's elapsed-time field to seconds. The format is
// [[dd-]hh:]mm:ss with the smaller fields zero-padded to two digits — real
// readings from this fleet: "05:12", "01:48:50", "52-03:47:57".
//
// Deliberately strict, for the same reason atoiStrict is: every value it
// returns becomes an operand of a verdict about whether a machine is healthy,
// so a shape this function does not fully understand must come back as
// "unreadable" (which folds the verdict to unproven) rather than as a number
// that merely happened to parse. In particular the minute and second fields
// must BE two digits and be in range: "1:2:3" and "00:99" are not ps output,
// and reading them as 3723s / 99s would be inventing an age out of something
// unrecognised.
func parseEtime(s string) (int, bool) {
	days, hasDays := 0, false
	if i := strings.IndexByte(s, '-'); i >= 0 {
		d, ok := etimeField(s[:i], 1, 9, 1<<20)
		if !ok {
			return 0, false
		}
		days, hasDays = d, true
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	// A day count only ever appears with a full hh:mm:ss tail.
	if hasDays && len(parts) != 3 {
		return 0, false
	}
	hours, mins, secs := "0", "", ""
	switch len(parts) {
	case 2:
		mins, secs = parts[0], parts[1]
	case 3:
		hours, mins, secs = parts[0], parts[1], parts[2]
	default:
		return 0, false
	}
	// ps rolls hours into days past 24, so the hour field is never out of range;
	// minutes and seconds are always printed two-wide.
	h, hok := etimeField(hours, 1, 2, 23)
	m, mok := etimeField(mins, 1, 2, 59)
	sec, sok := etimeField(secs, 2, 2, 59)
	if !hok || !mok || !sok {
		return 0, false
	}
	return days*86400 + h*3600 + m*60 + sec, true
}

// etimeField parses one all-digit field of an etime, enforcing both its digit
// WIDTH and its upper bound. Width matters because ps zero-pads: a field of an
// unexpected length means this parser does not actually recognise the string it
// is looking at, and the honest answer to that is "unreadable".
func etimeField(s string, minDigits, maxDigits, max int) (int, bool) {
	if len(s) < minDigits || len(s) > maxDigits {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n > max {
		return 0, false
	}
	return n, true
}

// atoiStrict parses a all-digits string, returning 0 for anything else (empty,
// signed, padded with junk). Deliberately stricter than strconv.Atoi's error
// handling being ignored somewhere upstream.
func atoiStrict(s string) int {
	if s == "" {
		return 0
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// sampleCarrierProbe reads every operand through the seam and returns what it
// found, WITHOUT judging.
//
// Split from the verdict on purpose. Several of this function's decisions are
// invisible downstream — an unreadable session list is not "zero sessions", an
// unread birthtime is not "a birthtime of zero" — but all of them fold to the
// same `unproven`, so a test that reads only the verdict cannot tell a correct
// collector from a broken one. Mutation testing showed exactly that: folding
// the two cases the other way left the whole package green. The operands have
// to be assertable directly, and now they are.
//
// ppid is the launchd job leader (warden's parent — the anchor process itself
// when the machine is converted).
func sampleCarrierProbe(ops cutoverOps, anchorPath, socket string, ppid int, now time.Time) carrierProbe {
	p := carrierProbe{sampledAt: now}
	if anchorPath == "" || socket == "" {
		return p
	}
	p.shape = detectShape(ops, ppid, anchorPath)
	// Read the rest even when the shape is not anchor: the operands are cheap and
	// keeping the sample complete keeps the judge a pure truth table.
	if e, ok := processElapsedSecs(ops.run, ppid); ok {
		p.leaderElapsed = e
	} else {
		return p
	}
	members, listed := tmuxMemberSessionCount(ops.run, socket)
	if !listed {
		return p
	}
	if members > 0 {
		pid := tmuxServerPID(ops.run, socket)
		e, ok := processElapsedSecs(ops.run, pid)
		if !ok {
			return p
		}
		p.carriers = append(p.carriers, carrier{pid: pid, elapsed: e})
	}
	// B is only ever used to REACH the negative verdict, so losing it can only
	// cost a red, never create a green — this is the one operand whose absence
	// does NOT fail the whole sample closed. It is recorded as unknown rather
	// than as a zero time: see carrierProbe.anchorBirthKnown.
	if birth, err := ops.birthTime(anchorPath); err == nil {
		p.anchorBirth, p.anchorBirthKnown = birth, true
	}
	p.ok = true
	return p
}

// sampleCutoverEffect takes the whole sample through the seam and judges it.
func sampleCutoverEffect(ops cutoverOps, anchorPath, socket string, ppid int, now time.Time) cutoverEffect {
	return judgeCutoverEffect(sampleCarrierProbe(ops, anchorPath, socket, ppid, now))
}

// newCutoverEffectReporter builds the 30s heartbeat's collector, mirroring
// newShapeReporter: re-sampled every cycle (never cached — the conversion boots
// the job out and launchd restarts it, so the process that reports is not the
// one that would have cached), and routed through newCutoverOps so the test
// binary's rebound seam covers it.
//
// An empty anchorPath or socket means the install paths could not be derived,
// and the honest answer to that is `unproven` — NOT an omitted field. Omission
// is reserved for warden builds that predate this release.
func newCutoverEffectReporter(anchorPath, socket string, ppid int) func() string {
	return func() string {
		return string(sampleCutoverEffect(newCutoverOps(), anchorPath, socket, ppid, time.Now()))
	}
}
