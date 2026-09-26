// cutovereffect.go — is the anchor cutover actually in effect for the processes
// that carry agents?
//
// The subject is the tmux server processes that CARRY member sessions, not
// warden's parent: warden_shape (cutover.go detectShape) answers "who is
// warden's parent now", which diverges from "who is TCC-responsible for the
// agents" when launchd restarts warden under the anchor while the tmux server
// keeps running under the old identity — an honest live reading of the wrong
// subject showed a false green.
//
// The verdict is three-valued on purpose: `unproven` must never be folded into
// `effective`. This file only makes the state visible — never restarts tmux,
// interrupts an agent, or auto-repairs (owner ruling).
package main

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var errNoBirthTime = errors.New("this platform does not expose an inode birth time")

type cutoverEffect string

const (
	effectEffective    cutoverEffect = "effective"
	effectNotEffective cutoverEffect = "not_effective"
	effectUnproven     cutoverEffect = "unproven"
)

type carrierProbe struct {
	shape         wardenShape
	leaderElapsed int
	carriers      []carrier
	// The anchor is never rewritten once installed (ensureAnchorPresent promotes
	// via create-if-absent os.Link), so its birthtime is a filesystem fact about
	// the identity.
	anchorBirth time.Time
	// Separate flag, not a zero-time sentinel: every carrier is born after year
	// 1, so a sentinel makes "birth unavailable" indistinguishable from "born
	// very early" and the guard untestable. Always false off darwin (CI's platform).
	anchorBirthKnown bool
	sampledAt        time.Time
	ok               bool
}

type carrier struct {
	pid     int
	elapsed int
}

// The carrier-vs-leader check compares elapsed seconds, not wall clocks: the
// cutover log is UTC and ps prints local time (an 8h skew on this fleet), which
// can invert a same-day comparison.
//
// Failing that check is only `unproven`: a `launchctl kickstart -k` or reboot
// makes the current leader young again, so a legitimate anchor-born carrier can
// look older than it.
//
// The negative uses the anchor file's birth time, never the leader's age, and
// uses it ONLY for the negative: the anchor file is materialised before the
// lock is taken (a refused conversion still leaves it), so using it in the
// positive direction would green-light a carrier forked between "anchor
// exists" and "plist swapped".
func judgeCutoverEffect(p carrierProbe) cutoverEffect {
	if !p.ok {
		return effectUnproven
	}
	if p.shape != shapeAnchor {
		return effectUnproven
	}
	// Negative first: it must not be masked by the unproven returns below.
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
	// Currently redundant — the loop below already fails every carrier when the
	// leader age is 0 — so no test reddens if this is deleted. If you loosen that
	// `>=`, this becomes the only guard between an unreadable leader age and a
	// green verdict; write the test then.
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

// `display-message -p '#{pid}'` is a server-scope format, so it answers without
// naming a session.
func tmuxServerPID(run func(string, ...string) (string, error), socket string) int {
	out, err := run("tmux", "-L", socket, "display-message", "-p", "#{pid}")
	if err != nil {
		return 0
	}
	return atoiStrict(strings.TrimSpace(out))
}

func tmuxMemberSessionCount(run func(string, ...string) (string, error), socket string) (int, bool) {
	out, err := run("tmux", "-L", socket, "list-sessions", "-F", "#{session_name}")
	if err != nil {
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

// The one definition of this argv: the test fake keys off it, and
// bin/tests/ps-field-support-guard.sh reads the `-o <field>=` literal out of
// this call site and runs the real ps on it.
func psElapsedArgs(pid int) []string {
	return []string{"-p", strconv.Itoa(pid), "-o", "etime="}
}

// 🔴 `-o etime=`, NOT `-o etimes=`: `etimes` is a GNU/procps extension BSD ps
// lacks (`ps -p 1 -o etimes=` exits 1 on macOS), which once left every machine
// stuck at `unproven` while the unit fake, keyed on the same argv, stayed
// green.
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

// ps etime format: [[dd-]hh:]mm:ss, e.g. "05:12", "01:48:50", "52-03:47:57".
// Deliberately strict: a shape not fully recognised is unreadable (verdict
// folds to unproven), never a number that merely happened to parse.
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
	h, hok := etimeField(hours, 1, 2, 23)
	m, mok := etimeField(mins, 1, 2, 59)
	sec, sok := etimeField(secs, 2, 2, 59)
	if !hok || !mok || !sok {
		return 0, false
	}
	return days*86400 + h*3600 + m*60 + sec, true
}

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

// ppid is the launchd job leader (warden's parent — the anchor process itself
// when the machine is converted).
func sampleCarrierProbe(ops cutoverOps, anchorPath, socket string, ppid int, now time.Time) carrierProbe {
	p := carrierProbe{sampledAt: now}
	if anchorPath == "" || socket == "" {
		return p
	}
	p.shape = detectShape(ops, ppid, anchorPath)
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
	// The birth time only reaches the negative verdict, so its absence can cost
	// a red but never create a green — the one operand that does not fail the
	// sample.
	if birth, err := ops.birthTime(anchorPath); err == nil {
		p.anchorBirth, p.anchorBirthKnown = birth, true
	}
	p.ok = true
	return p
}

func sampleCutoverEffect(ops cutoverOps, anchorPath, socket string, ppid int, now time.Time) cutoverEffect {
	return judgeCutoverEffect(sampleCarrierProbe(ops, anchorPath, socket, ppid, now))
}

// Re-sampled every cycle, never cached: the conversion boots the job out and
// launchd restarts it, so the reporting process is not the one that cached.
// Empty anchorPath/socket must report `unproven`, not omit the field —
// omission means a warden build that predates this reporter.
func newCutoverEffectReporter(anchorPath, socket string, ppid int) func() string {
	return func() string {
		return string(sampleCutoverEffect(newCutoverOps(), anchorPath, socket, ppid, time.Now()))
	}
}
