package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// cutoverLog collects the converter's lines with their leading UTC timestamp
// stripped, so the narrative can be compared literally.
type cutoverLog struct{ lines []string }

func (l *cutoverLog) logf(format string, a ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, a...))
}

// stripTimestamps removes the "<RFC3339> " prefix cutoverCmd stamps on each line.
func stripTimestamps(t *testing.T, raw string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		stamp, rest, found := strings.Cut(line, " ")
		if !found {
			out = append(out, line)
			continue
		}
		if _, err := time.Parse(time.RFC3339, stamp); err != nil {
			out = append(out, line)
			continue
		}
		out = append(out, rest)
	}
	return out
}

const cutoverTarget = "gui/501/com.officraft.ocwarden"

// convertibleMachine is a legacy machine whose plist is readable and whose
// rollback path can succeed if it is ever taken.
func convertibleMachine(p wardenPaths) *cutoverRec {
	rec := newCutoverRec()
	rec.files[p.plistPath] = "<plist>old shape</plist>"
	rec.runOut[argvKey("launchctl", "bootout", cutoverTarget)] = ""
	rec.runOut[argvKey("launchctl", "bootstrap", p.guiDomain, p.plistPath)] = ""
	rec.runOut[argvKey("launchctl", "kickstart", "-k", cutoverTarget)] = ""
	rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n\tpid = 4242\n"
	rec.runQueue[argvKey("launchctl", "print", cutoverTarget)] = []runAnswer{{err: errors.New("Could not find service")}}
	return rec
}

func TestRunCutover(t *testing.T) {
	root := "/Users/eva/.officraft"
	p := cutoverPaths(root)
	prevPath := p.plistPath + ".prev"
	sentinel := filepath.Join(root, "warden", "cutover.failed")
	const exe = "/Users/eva/.officraft/warden/ocwarden"

	t.Run("a successful install converts the machine and keeps the backup", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.installerOut = "[ocwarden install] done\n"
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 0 {
			t.Fatalf("runCutover = %d, want 0", code)
		}
		if want := []string{argvKey(exe, "install", "--force")}; !reflect.DeepEqual(rec.installerCalls, want) {
			t.Errorf("installer calls = %v, want %v", rec.installerCalls, want)
		}
		if rec.files[prevPath] != "<plist>old shape</plist>" {
			t.Errorf("plist backup = %q, want the pre-conversion plist", rec.files[prevPath])
		}
		if len(rec.runs) != 0 {
			t.Errorf("a successful conversion ran launchctl itself: %v", rec.runs)
		}
		if _, wrote := rec.files[sentinel]; wrote {
			t.Error("a successful conversion wrote the failure sentinel")
		}
		wantLog := []string{
			fmt.Sprintf("backed up current plist -> %s (24 bytes)", prevPath),
			fmt.Sprintf("running: %s install --force", exe),
			"install output:\n[ocwarden install] done\n",
			fmt.Sprintf("SUCCESS: converted to anchor shape; plist backup kept at %s", prevPath),
		}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})

	t.Run("an unreadable plist aborts before anything is written", func(t *testing.T) {
		rec := newCutoverRec()
		rec.readErr[p.plistPath] = errors.New("no such file or directory")
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 1 {
			t.Errorf("runCutover = %d, want 1", code)
		}
		if len(rec.writes) != 0 || len(rec.installerCalls) != 0 || len(rec.runs) != 0 {
			t.Errorf("writes=%v installs=%v runs=%v, want nothing at all", rec.writes, rec.installerCalls, rec.runs)
		}
		wantLog := []string{fmt.Sprintf(
			"ABORT: cannot read current plist %s: no such file or directory (nothing to roll back to)", p.plistPath)}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})

	t.Run("a backup that cannot be written aborts before the installer runs", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.writeErr[prevPath] = errors.New("read-only file system")
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 1 {
			t.Errorf("runCutover = %d, want 1", code)
		}
		if len(rec.installerCalls) != 0 || len(rec.runs) != 0 {
			t.Errorf("installs=%v runs=%v, want nothing", rec.installerCalls, rec.runs)
		}
		wantLog := []string{fmt.Sprintf("ABORT: cannot write plist backup %s: read-only file system", prevPath)}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})

	t.Run("an install that failed before touching the plist leaves the machine to retry", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.installerErr = errors.New("exit status 1")
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 0 {
			t.Errorf("runCutover = %d, want 0 — an untouched machine is not a failure", code)
		}
		if len(rec.runs) != 0 {
			t.Errorf("a rollback was attempted on an untouched machine: %v", rec.runs)
		}
		if _, wrote := rec.files[sentinel]; wrote {
			t.Error("a sentinel was written, which would block every future attempt")
		}
		if rec.files[p.plistPath] != "<plist>old shape</plist>" {
			t.Errorf("plist = %q, want it untouched", rec.files[p.plistPath])
		}
		wantLog := []string{
			fmt.Sprintf("backed up current plist -> %s (24 bytes)", prevPath),
			fmt.Sprintf("running: %s install --force", exe),
			"install FAILED (exit status 1) but nothing was modified — machine is untouched, leaving no sentinel so the next start retries",
		}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})

	t.Run("an install that half-converted the machine is rolled back and the sentinel written", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.installerErr = errors.New("exit status 1")
		rec.installerOut = "bootstrap failed\n"
		rec.installerWrites[p.plistPath] = "<plist>half-written anchor shape</plist>"
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 0 {
			t.Errorf("runCutover = %d, want 0 — a successful rollback is not an operator incident", code)
		}
		if rec.files[p.plistPath] != "<plist>old shape</plist>" {
			t.Errorf("plist = %q, want the pre-conversion plist restored", rec.files[p.plistPath])
		}
		body := rec.files[sentinel]
		wantTail := "install failed: exit status 1\nrolled back successfully\n\nRemove this file to allow another attempt.\n"
		if !strings.HasSuffix(body, wantTail) {
			t.Errorf("sentinel = %q, want it to end with %q", body, wantTail)
		}
		wantRuns := []string{
			argvKey("launchctl", "bootout", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "bootstrap", p.guiDomain, p.plistPath),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "kickstart", "-k", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
			argvKey("launchctl", "print", cutoverTarget),
		}
		if !reflect.DeepEqual(rec.runs, wantRuns) {
			t.Errorf("launchctl calls =\n%v\nwant\n%v", rec.runs, wantRuns)
		}
		wantLog := []string{
			fmt.Sprintf("backed up current plist -> %s (24 bytes)", prevPath),
			fmt.Sprintf("running: %s install --force", exe),
			"install output:\nbootstrap failed\n",
			"install FAILED (exit status 1) — rolling back to the pre-conversion shape",
			fmt.Sprintf("restored %s from backup", p.plistPath),
			"rollback complete: launchd is running the pre-conversion shape again",
			fmt.Sprintf("wrote %s — further conversion attempts are blocked until it is removed", sentinel),
		}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log =\n%s\nwant\n%s", strings.Join(log.lines, "\n"), strings.Join(wantLog, "\n"))
		}
	})

	t.Run("a rollback that itself failed is the one case a human is called for", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.installerErr = errors.New("exit status 1")
		rec.installerWrites[p.plistPath] = "<plist>half-written anchor shape</plist>"
		rec.runErr[argvKey("launchctl", "bootstrap", p.guiDomain, p.plistPath)] = errors.New("Bootstrap failed: 5")
		delete(rec.runOut, argvKey("launchctl", "bootstrap", p.guiDomain, p.plistPath))
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 1 {
			t.Errorf("runCutover = %d, want 1", code)
		}
		body := rec.files[sentinel]
		wantTail := "install failed: exit status 1\nrollback FAILED: re-bootstrap old shape: Bootstrap failed: 5\n\n" +
			"Remove this file to allow another attempt.\n"
		if !strings.HasSuffix(body, wantTail) {
			t.Errorf("sentinel = %q, want it to end with %q", body, wantTail)
		}
		last := log.lines[len(log.lines)-2]
		if want := "🔴 ROLLBACK FAILED: re-bootstrap old shape: Bootstrap failed: 5"; last != want {
			t.Errorf("log = %#v, want %q before the sentinel line", log.lines, want)
		}
	})

	t.Run("a machine still registered after bootout is bootstrapped anyway", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.installerErr = errors.New("exit status 1")
		rec.installerWrites[p.plistPath] = "<plist>half-written anchor shape</plist>"
		rec.runQueue[argvKey("launchctl", "print", cutoverTarget)] = nil
		log := &cutoverLog{}

		if code := runCutover(rec.ops(), p, exe, log.logf); code != 0 {
			t.Errorf("runCutover = %d, want 0", code)
		}
		if !slicesContain(log.lines, fmt.Sprintf("WARN: %s still registered after bootout; bootstrapping anyway", cutoverTarget)) {
			t.Errorf("log = %#v, want the still-registered warning", log.lines)
		}
		if sleeps := len(rec.sleeps); sleeps != bootoutPollAttempts+6 {
			t.Errorf("sleeps = %d, want %d (the full bootout poll plus the settle window)", sleeps, bootoutPollAttempts+6)
		}
	})
}

func TestRollback(t *testing.T) {
	root := "/Users/eva/.officraft"
	p := cutoverPaths(root)
	old := []byte("<plist>old shape</plist>")

	t.Run("the old plist is restored and launchd is made to read it", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.files[p.plistPath] = "<plist>anchor shape</plist>"
		log := &cutoverLog{}

		if err := rollback(rec.ops(), p, old, cutoverTarget, log.logf); err != nil {
			t.Fatalf("rollback = %v, want nil", err)
		}
		if rec.files[p.plistPath] != string(old) {
			t.Errorf("plist = %q, want %q", rec.files[p.plistPath], old)
		}
		if want := []string{p.plistPath + " 0644 24"}; !reflect.DeepEqual(rec.writes, want) {
			t.Errorf("writes = %v, want %v", rec.writes, want)
		}
		wantLog := []string{fmt.Sprintf("restored %s from backup", p.plistPath)}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})

	t.Run("a plist that cannot be restored stops before touching launchd", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.writeErr[p.plistPath] = errors.New("permission denied")
		log := &cutoverLog{}

		err := rollback(rec.ops(), p, old, cutoverTarget, log.logf)

		want := fmt.Sprintf("restore plist %s: permission denied", p.plistPath)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if len(rec.runs) != 0 {
			t.Errorf("launchctl was called after the restore failed: %v", rec.runs)
		}
	})

	t.Run("a re-bootstrap that will not take is reported as itself", func(t *testing.T) {
		rec := convertibleMachine(p)
		delete(rec.runOut, argvKey("launchctl", "bootstrap", p.guiDomain, p.plistPath))
		rec.runErr[argvKey("launchctl", "bootstrap", p.guiDomain, p.plistPath)] = errors.New("Bootstrap failed: 5")
		log := &cutoverLog{}

		err := rollback(rec.ops(), p, old, cutoverTarget, log.logf)

		want := "re-bootstrap old shape: Bootstrap failed: 5"
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if slicesContain(rec.runs, argvKey("launchctl", "kickstart", "-k", cutoverTarget)) {
			t.Error("a failed bootstrap was still kickstarted")
		}
	})

	t.Run("a bootstrap that exits 0 and registers nothing names the plist to look at", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.runQueue[argvKey("launchctl", "print", cutoverTarget)] = nil
		rec.runErr[argvKey("launchctl", "print", cutoverTarget)] = errors.New("Could not find service")
		delete(rec.runOut, argvKey("launchctl", "print", cutoverTarget))
		rec.runErr[argvKey("launchctl", "kickstart", "-k", cutoverTarget)] = errors.New("No such process")
		delete(rec.runOut, argvKey("launchctl", "kickstart", "-k", cutoverTarget))
		log := &cutoverLog{}

		err := rollback(rec.ops(), p, old, cutoverTarget, log.logf)

		want := fmt.Sprintf("re-bootstrap of the old shape exited 0 but registered nothing — launchd does not know %s, "+
			"so the old job was never loaded; the plist to look at is %s: Could not find service "+
			"(the next verb then reported: No such process)", cutoverTarget, p.plistPath)
		if err == nil || err.Error() != want {
			t.Errorf("err =\n%v\nwant\n%q", err, want)
		}
		if !slicesContain(log.lines, fmt.Sprintf(
			"WARN: launchd still does not know %s after re-bootstrap exited 0; kickstarting anyway", cutoverTarget)) {
			t.Errorf("log = %#v, want the unregistered warning", log.lines)
		}
	})

	t.Run("a kickstart that fails on a registered job is reported plainly", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.runErr[argvKey("launchctl", "kickstart", "-k", cutoverTarget)] = errors.New("Input/output error")
		delete(rec.runOut, argvKey("launchctl", "kickstart", "-k", cutoverTarget))
		log := &cutoverLog{}

		err := rollback(rec.ops(), p, old, cutoverTarget, log.logf)

		if want := "kickstart old shape: Input/output error"; err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("an old shape that does not come back is a rollback failure", func(t *testing.T) {
		rec := convertibleMachine(p)
		rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n"
		log := &cutoverLog{}

		err := rollback(rec.ops(), p, old, cutoverTarget, log.logf)

		want := fmt.Sprintf("old shape did not come back: %s reported no live pid within 30s", cutoverTarget)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})
}

func TestCutoverWaitGone(t *testing.T) {
	rec := newCutoverRec()
	if !cutoverWaitGone(rec.ops(), cutoverTarget) {
		t.Error("cutoverWaitGone = false, want true when launchd already does not know the label")
	}
	if len(rec.runs) != 1 || len(rec.sleeps) != 0 {
		t.Errorf("runs=%d sleeps=%d, want 1/0 — a label already gone costs one probe", len(rec.runs), len(rec.sleeps))
	}

	stubborn := newCutoverRec()
	stubborn.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n"
	if cutoverWaitGone(stubborn.ops(), cutoverTarget) {
		t.Error("cutoverWaitGone = true, want false while the label is still registered")
	}
	if len(stubborn.runs) != bootoutPollAttempts || len(stubborn.sleeps) != bootoutPollAttempts {
		t.Errorf("runs=%d sleeps=%d, want %d/%d", len(stubborn.runs), len(stubborn.sleeps),
			bootoutPollAttempts, bootoutPollAttempts)
	}
	for _, d := range stubborn.sleeps {
		if d != bootoutPollInterval {
			t.Errorf("slept %s between probes, want %s", d, bootoutPollInterval)
		}
	}

	late := newCutoverRec()
	late.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n"
	late.runQueue[argvKey("launchctl", "print", cutoverTarget)] = []runAnswer{
		{out: "state = running\n"}, {out: "state = running\n"}, {err: errors.New("Could not find service")},
	}
	if !cutoverWaitGone(late.ops(), cutoverTarget) {
		t.Error("cutoverWaitGone = false, want true once the async bootout lands")
	}
	if len(late.runs) != 3 || len(late.sleeps) != 2 {
		t.Errorf("runs=%d sleeps=%d, want 3/2", len(late.runs), len(late.sleeps))
	}
}

func TestCutoverWaitRegistered(t *testing.T) {
	rec := newCutoverRec()
	rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n"
	if err := cutoverWaitRegistered(rec.ops(), cutoverTarget); err != nil {
		t.Errorf("cutoverWaitRegistered = %v, want nil for a registered label", err)
	}
	if len(rec.runs) != 1 || len(rec.sleeps) != 0 {
		t.Errorf("runs=%d sleeps=%d, want 1/0", len(rec.runs), len(rec.sleeps))
	}

	never := newCutoverRec()
	never.runErr[argvKey("launchctl", "print", cutoverTarget)] = errors.New("Could not find service")
	err := cutoverWaitRegistered(never.ops(), cutoverTarget)
	if err == nil || err.Error() != "Could not find service" {
		t.Errorf("cutoverWaitRegistered = %v, want the last probe's error", err)
	}
	if len(never.runs) != registerPollAttempts || len(never.sleeps) != registerPollAttempts {
		t.Errorf("runs=%d sleeps=%d, want %d/%d", len(never.runs), len(never.sleeps),
			registerPollAttempts, registerPollAttempts)
	}

	late := newCutoverRec()
	late.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n"
	late.runQueue[argvKey("launchctl", "print", cutoverTarget)] = []runAnswer{
		{err: errors.New("Could not find service")}, {err: errors.New("Could not find service")},
	}
	if err := cutoverWaitRegistered(late.ops(), cutoverTarget); err != nil {
		t.Errorf("cutoverWaitRegistered = %v, want nil once launchd catches up", err)
	}
	if len(late.runs) != 3 || len(late.sleeps) != 2 {
		t.Errorf("runs=%d sleeps=%d, want 3/2", len(late.runs), len(late.sleeps))
	}
}

func TestCutoverVerifyAlive(t *testing.T) {
	t.Run("one pid held across the settle window is alive", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n\tpid = 4242\n"

		if err := cutoverVerifyAlive(rec.ops(), cutoverTarget); err != nil {
			t.Fatalf("cutoverVerifyAlive = %v, want nil", err)
		}
		if len(rec.runs) != 7 || len(rec.sleeps) != 6 {
			t.Errorf("runs=%d sleeps=%d, want 7/6 (one read plus six settle samples)", len(rec.runs), len(rec.sleeps))
		}
	})

	t.Run("a pid that only appears later is still alive", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n\tpid = 4242\n"
		rec.runQueue[argvKey("launchctl", "print", cutoverTarget)] = []runAnswer{
			{err: errors.New("Could not find service")}, {out: "state = spawn scheduled\n"},
		}

		if err := cutoverVerifyAlive(rec.ops(), cutoverTarget); err != nil {
			t.Fatalf("cutoverVerifyAlive = %v, want nil", err)
		}
		if len(rec.sleeps) != 8 {
			t.Errorf("sleeps = %d, want 8 (two waiting for a pid, six settling)", len(rec.sleeps))
		}
	})

	t.Run("a job that never reports a pid is refused after thirty samples", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n"

		err := cutoverVerifyAlive(rec.ops(), cutoverTarget)

		want := fmt.Sprintf("%s reported no live pid within 30s", cutoverTarget)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if len(rec.runs) != 30 || len(rec.sleeps) != 30 {
			t.Errorf("runs=%d sleeps=%d, want 30/30", len(rec.runs), len(rec.sleeps))
		}
	})

	t.Run("a pid that changes during the settle window is a crash loop", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = "state = running\n\tpid = 9999\n"
		rec.runQueue[argvKey("launchctl", "print", cutoverTarget)] = []runAnswer{
			{out: "state = running\n\tpid = 4242\n"}, {out: "state = running\n\tpid = 4242\n"},
		}

		err := cutoverVerifyAlive(rec.ops(), cutoverTarget)

		want := fmt.Sprintf("%s is crash-looping (pid 4242 -> 9999)", cutoverTarget)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("a job that vanishes during the settle window is a crash loop too", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runErr[argvKey("launchctl", "print", cutoverTarget)] = errors.New("Could not find service")
		rec.runQueue[argvKey("launchctl", "print", cutoverTarget)] = []runAnswer{
			{out: "state = running\n\tpid = 4242\n"},
		}

		err := cutoverVerifyAlive(rec.ops(), cutoverTarget)

		want := fmt.Sprintf("%s is crash-looping (pid 4242 -> <none>)", cutoverTarget)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})
}

func TestCutoverPID(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
		want string
	}{
		{"a live job reports its pid", "state = running\n\tpid = 4242\n\tprogram = /x\n", nil, "4242"},
		{"a job with no pid line", "state = waiting\n", nil, ""},
		{"a label launchd does not know", "", errors.New("Could not find service"), ""},
		{"a pid-shaped word that is not the pid field", "\tprogram-pid = 77\n", nil, ""},
	}
	for _, c := range cases {
		rec := newCutoverRec()
		if c.err != nil {
			rec.runErr[argvKey("launchctl", "print", cutoverTarget)] = c.err
		} else {
			rec.runOut[argvKey("launchctl", "print", cutoverTarget)] = c.out
		}
		if got := cutoverPID(rec.ops(), cutoverTarget); got != c.want {
			t.Errorf("%s: cutoverPID = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWriteCutoverSentinel(t *testing.T) {
	root := "/Users/eva/.officraft"
	p := cutoverPaths(root)
	sentinel := filepath.Join(root, "warden", "cutover.failed")

	t.Run("the sentinel carries when and why, and says how to clear it", func(t *testing.T) {
		rec := newCutoverRec()
		log := &cutoverLog{}
		before := time.Now().UTC().Truncate(time.Second)

		writeCutoverSentinel(rec.ops(), p, "install failed: exit status 1\nrolled back successfully", log.logf)

		body := rec.files[sentinel]
		stamp, rest, found := strings.Cut(body, "\n")
		if !found {
			t.Fatalf("sentinel = %q, want a timestamp line", body)
		}
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			t.Errorf("first line = %q, want an RFC3339 timestamp: %v", stamp, err)
		} else if at.Before(before) || at.After(time.Now().UTC().Add(time.Second)) {
			t.Errorf("timestamp = %s, want the moment the sentinel was written", at)
		}
		want := "install failed: exit status 1\nrolled back successfully\n\nRemove this file to allow another attempt.\n"
		if rest != want {
			t.Errorf("body after the timestamp = %q, want %q", rest, want)
		}
		if len(rec.writes) != 1 || !strings.HasPrefix(rec.writes[0], sentinel+" 0644 ") {
			t.Errorf("writes = %v, want one 0644 write of %s", rec.writes, sentinel)
		}
		wantLog := []string{fmt.Sprintf(
			"wrote %s — further conversion attempts are blocked until it is removed", sentinel)}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})

	t.Run("a sentinel that cannot be written says the next start will retry", func(t *testing.T) {
		rec := newCutoverRec()
		rec.writeErr[sentinel] = errors.New("read-only file system")
		log := &cutoverLog{}

		writeCutoverSentinel(rec.ops(), p, "install failed", log.logf)

		wantLog := []string{fmt.Sprintf(
			"WARN: could not write %s: read-only file system (the next start will retry the conversion)", sentinel)}
		if !reflect.DeepEqual(log.lines, wantLog) {
			t.Errorf("log = %#v, want %#v", log.lines, wantLog)
		}
	})
}

func TestReleaseCutoverLock(t *testing.T) {
	const lock = "/Users/eva/.officraft/warden/cutover.lock"

	rec := newCutoverRec()
	releaseCutoverLock(rec.ops(), envMap(map[string]string{"OC_CUTOVER_LOCK": lock}))
	if want := []string{lock}; !reflect.DeepEqual(rec.removes, want) {
		t.Errorf("removes = %v, want %v", rec.removes, want)
	}

	unlocked := newCutoverRec()
	releaseCutoverLock(unlocked.ops(), envMap(map[string]string{}))
	if len(unlocked.removes) != 0 {
		t.Errorf("removes = %v, want none when this converter holds no lock", unlocked.removes)
	}

	stubborn := newCutoverRec()
	stubborn.removeErr[lock] = errors.New("permission denied")
	releaseCutoverLock(stubborn.ops(), envMap(map[string]string{"OC_CUTOVER_LOCK": lock}))
	if len(stubborn.removes) != 0 {
		t.Errorf("removes = %v, want none to have landed", stubborn.removes)
	}
}

func TestCutoverCmd(t *testing.T) {
	t.Run("a converter with no HOME stops before building any effects", func(t *testing.T) {
		rec := newCutoverRec()
		bindCutoverOps(t, rec)
		var out bytes.Buffer

		if code := cutoverCmd(envMap(map[string]string{}), &out); code != 1 {
			t.Errorf("cutoverCmd = %d, want 1", code)
		}
		if want := []string{"[cutover] FATAL: HOME must be set"}; !reflect.DeepEqual(stripTimestamps(t, out.String()), want) {
			t.Errorf("output = %#v, want %#v", stripTimestamps(t, out.String()), want)
		}
		if len(rec.writes) != 0 || len(rec.removes) != 0 || len(rec.installerCalls) != 0 {
			t.Errorf("writes=%v removes=%v installs=%v, want nothing", rec.writes, rec.removes, rec.installerCalls)
		}
	})

	t.Run("a converter with no OC_BASE refuses rather than guessing a station", func(t *testing.T) {
		rec := newCutoverRec()
		bindCutoverOps(t, rec)
		var out bytes.Buffer

		code := cutoverCmd(envMap(map[string]string{"HOME": "/Users/eva", "OC_TOKEN": jwtWardenOne}), &out)

		if code != 1 {
			t.Errorf("cutoverCmd = %d, want 1", code)
		}
		lines := stripTimestamps(t, out.String())
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "[cutover] FATAL: OC_BASE is not set") {
			t.Errorf("output = %#v, want one FATAL naming OC_BASE", lines)
		}
	})

	t.Run("a converted machine releases the lock it was handed", func(t *testing.T) {
		home := t.TempDir()
		exe, err := os.Executable()
		if err != nil {
			t.Fatalf("os.Executable: %v", err)
		}
		plist := filepath.Join(home, "Library", "LaunchAgents", "com.officraft.ocwarden.plist")
		lock := filepath.Join(home, ".officraft", "warden", "cutover.lock")
		rec := newCutoverRec()
		rec.files[plist] = "<plist>old shape</plist>"
		bindCutoverOps(t, rec)
		var out bytes.Buffer

		code := cutoverCmd(envMap(map[string]string{
			"HOME":            home,
			"OC_BASE":         "https://station.example",
			"OC_TOKEN":        jwtWardenOne,
			"OC_CUTOVER_LOCK": lock,
		}), &out)

		if code != 0 {
			t.Fatalf("cutoverCmd = %d, want 0\n%s", code, out.String())
		}
		if want := []string{argvKey(exe, "install", "--force")}; !reflect.DeepEqual(rec.installerCalls, want) {
			t.Errorf("installer calls = %v, want %v", rec.installerCalls, want)
		}
		if want := []string{lock}; !reflect.DeepEqual(rec.removes, want) {
			t.Errorf("removes = %v, want %v — the lock the parent took must be dropped", rec.removes, want)
		}
		wantLines := []string{
			fmt.Sprintf("[cutover] backed up current plist -> %s (24 bytes)", plist+".prev"),
			fmt.Sprintf("[cutover] running: %s install --force", exe),
			fmt.Sprintf("[cutover] SUCCESS: converted to anchor shape; plist backup kept at %s", plist+".prev"),
		}
		if got := stripTimestamps(t, out.String()); !reflect.DeepEqual(got, wantLines) {
			t.Errorf("output = %#v, want %#v", got, wantLines)
		}
	})
}
