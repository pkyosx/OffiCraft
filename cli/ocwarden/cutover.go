// cutover.go — automatic legacy→anchor launchd SHAPE migration.
//
// The anchor shape's plist names the never-replaced TCC identity anchor
// `officraft`, which forks the sibling ocwarden. Self-update only swaps BINARIES
// and never touches the plist, so a machine installed before the anchor shape
// keeps its legacy plist (`[…/warden/ocwarden, run]`) forever, and every
// self-update voids its TCC grants because launchd's job leader is the file being
// replaced. OffiCraft has external users, so "re-run the installer" is not a plan.
//
// `ocwarden run` checks its shape once at startup (self-update exec's in place, so
// an updated machine runs this within seconds). On a legacy machine it spawns a
// DETACHED grandchild (`ocwarden cutover-anchor`, setsid) that survives its
// parent's launchd job being booted out, and that shells out to the existing
// `ocwarden install --force`.
//
// The plist backup is taken HERE, before install, not in runInstall: writePlist
// overwrites the old plist unconditionally and `plutil -lint` runs AFTER the
// write, so a lint failure would leave an unverified plist that the next reboot
// silently adopts. Rollback fires on ANY non-zero install exit.
//
// Every failure path must land on "old shape, warden alive": unknown shape,
// failed preflight, held lock or an existing cutover.failed → do nothing;
// install fails → restore the backed-up plist, re-bootstrap, verify.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Tri-state on purpose: the server reads a missing/unknown shape as unknown and
// never infers, and reporting a guess as a verdict is worse than nothing.
type wardenShape string

const (
	shapeAnchor  wardenShape = "anchor"
	shapeLegacy  wardenShape = "legacy"
	shapeUnknown wardenShape = "unknown"
)

const (
	// Deliberately absent from the usage banner: an internal migration step.
	cutoverSubcmd = "cutover-anchor"

	cutoverLockName = "cutover.lock"

	// cutoverFailedName stops a boot-loop (a rolled-back machine would detect
	// "legacy" again and reconvert forever). Removing it by hand is the
	// deliberate way to retry.
	cutoverFailedName = "cutover.failed"

	// install.go's writePlist keeps no copy of its own, so this backup is the
	// ONLY copy of the old shape once the install starts.
	plistPrevSuffix = ".prev"

	// A sibling of anchorPath, not a temp dir, so the promotion can be an os.Link
	// (link cannot cross filesystems; a copy fallback is the non-atomic overwrite
	// this shape exists to avoid).
	anchorProbeSuffix = ".probe"

	// cutoverInstallBudget must not share the 30s probe budget: killing a slow
	// install reads as a failure, rolls back AND writes cutover.failed, which
	// never retries — a slow machine would fail PERMANENTLY. Derived from
	// install's own bounded steps:
	//
	//	claude resolve   <= 2 x claudeProbeBudget (20s)        =  40s
	//	codex resolve    <= 2 x claudeProbeBudget (20s)        =  40s
	//	ocagent download <= selfUpdateRequestBudget (60s)      =  60s
	//	bootout poll     <= bootoutPollAttempts x Interval     =   5s
	//	verify           <= 30 x 1s + 6 x 1s                   =  36s
	//	                                                   total ~181s
	//
	// 10 minutes is ~3x headroom; its only job is to stop an install that hung
	// forever.
	cutoverInstallBudget = 10 * time.Minute

	// A conversion is bounded (install's verify caps at ~36s); an older lock is a
	// corpse from a killed process.
	staleLockAge = 15 * time.Minute
)

type cutoverOps struct {
	ppidExe      func(pid int) (string, error)
	run          func(name string, args ...string) (string, error)
	runExit      func(name string, args ...string) (int, error)
	runInstaller func(name string, args ...string) (string, error)
	readFile     func(path string) ([]byte, error)
	writeFile    func(path string, data []byte, perm os.FileMode) error
	// writeFile's perm is umask-masked, and a staged copy landing 0644 fails the
	// preflight — mirrors copyAnchorIfAbsent.
	chmod func(path string, perm os.FileMode) error
	// os.Link, NOT os.Rename: EEXIST instead of clobbering makes "never replace
	// an existing anchor" a property of the syscall.
	link       func(oldpath, newpath string) error
	remove     func(path string) error
	createExcl func(path string) (bool, error)
	modTime    func(path string) (time.Time, error)
	// birthTime is st_birthtime, fixed at creation (mtime is settable). The
	// anchor is never rewritten once promoted, so its birthtime is when this
	// machine's anchor identity came into existence — the operand
	// cutovereffect.go's deterministic negative rests on.
	birthTime     func(path string) (time.Time, error)
	spawnDetached func(bin string, args []string, env []string, logPath string) error
	sleep         func(time.Duration)
}

func realCutoverOps() cutoverOps {
	refuseInTestBinary("realCutoverOps")
	r := newCmdRunner(30 * time.Second)
	return cutoverOps{
		ppidExe:      psExePath(r),
		run:          r.Run,
		runExit:      realRunExit,
		runInstaller: runInstallerCombined,
		readFile:     os.ReadFile,
		writeFile:    os.WriteFile,
		chmod:        os.Chmod,
		link:         os.Link,
		remove:       os.Remove,
		createExcl: func(path string) (bool, error) {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				if os.IsExist(err) {
					return false, nil
				}
				return false, err
			}
			_ = f.Close()
			return true, nil
		},
		modTime: func(path string) (time.Time, error) {
			fi, err := os.Stat(path)
			if err != nil {
				return time.Time{}, err
			}
			return fi.ModTime(), nil
		},
		birthTime:     statBirthTime,
		spawnDetached: spawnDetachedProcess,
		sleep:         time.Sleep,
	}
}

var newCutoverOps = realCutoverOps

// On macOS `ps -o comm=` prints the full executable path.
func psExePath(r CmdRunner) func(int) (string, error) {
	return func(pid int) (string, error) {
		out, err := r.Run("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil
	}
}

// Setsid puts the child outside this process group, so it survives `launchctl
// bootout` of this job (measured on macOS 26.5) — no one-shot launchd job
// needed. Output goes to logPath because the child outlives the parent's launchd
// log stream. refuseInTestBinary matters doubly here: Setsid + Release means an
// escaped process OUTLIVES `go test`.
func spawnDetachedProcess(bin string, args []string, env []string, logPath string) error {
	refuseInTestBinary("spawnDetachedProcess(" + bin + ")")
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
			defer f.Close()
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Deliberately NOT "does ~/.officraft/warden/officraft exist": a machine can have
// the anchor file on disk and still be booted from a legacy plist, which is the
// exact state this migration exists to fix.
func detectShape(ops cutoverOps, ppid int, anchorPath string) wardenShape {
	exe, err := ops.ppidExe(ppid)
	if err != nil || exe == "" {
		return shapeUnknown
	}
	if exe == anchorPath {
		return shapeAnchor
	}
	if ppid == 1 || filepath.Base(exe) == "launchd" {
		return shapeLegacy
	}
	return shapeUnknown
}

// newShapeReporter feeds the 30s heartbeat so the FLEET can tell converted
// machines from unconverted ones. Re-read every cycle, not cached: the
// conversion boots the job out, so the process that reports is never the one
// that cached. An empty anchorPath answers `unknown`, never an omitted field
// (omission means a build predating this) and never a guess (detectShape
// against "" would call every launchd-parented warden `legacy`).
func newShapeReporter(anchorPath string, ppid int) func() string {
	return func() string {
		if anchorPath == "" {
			return string(shapeUnknown)
		}
		return string(detectShape(newCutoverOps(), ppid, anchorPath))
	}
}

// ensureAnchorPresent materialises the anchor when this machine has none. The
// preflight needs an executable at p.anchorPath, and only `ocwarden install` —
// the very thing the conversion runs — ever puts one there, so on the machines
// this migration targets the gate was unsatisfiable (observed on three of three
// fleet machines: the converter was never spawned).
//
// The preflight stays on this side rather than inside runInstall because of the
// consequence of failing: in there it is a non-zero install exit → rollback +
// cutover.failed → the machine NEVER retries; out here it is a quiet skip, the
// right answer for a usually transient condition such as quarantine.
//
// STAGE → PROBE → PROMOTE: p.anchorPath is never written, truncated or replaced
// here; a truncated write or failed chmod lands on the staging path, so it can
// never be adopted on the next boot by copyAnchorIfAbsent (which preserves
// whatever it finds). A machine that fails anywhere is left exactly as found.
//
// The source precedence is copyAnchorIfAbsent's (sibling, then embedded)
// because TCC identifies the anchor BY its bytes: two sources would be two
// identities.
func ensureAnchorPresent(ops cutoverOps, p wardenPaths, logf func(string, ...any)) error {
	// Fast path only. An earlier revision used this stat as the never-replace
	// check, so any odd stat error read as "absent" and re-wrote the machine's own
	// anchor: identical bytes, new inode, new TCC identity. The promote step is
	// what refuses to overwrite.
	if _, err := ops.modTime(p.anchorPath); err == nil {
		return nil
	}
	probe := p.anchorPath + anchorProbeSuffix
	defer func() { _ = ops.remove(probe) }()

	// In a home install anchorSrc and anchorPath are the same file, so this read
	// normally misses and the embedded copy lands; a release-tarball layout keeps
	// the bytes it shipped with.
	data, err := ops.readFile(p.anchorSrc)
	if err != nil || len(data) == 0 {
		data = embeddedAnchor()
	}
	if len(data) == 0 {
		return fmt.Errorf("no anchor at %s, no usable source at %s, and this ocwarden carries no embedded anchor", p.anchorPath, p.anchorSrc)
	}
	if err := ops.writeFile(probe, data, 0o755); err != nil {
		return fmt.Errorf("stage anchor at %s: %w", probe, err)
	}
	if err := ops.chmod(probe, 0o755); err != nil {
		return fmt.Errorf("make the staged anchor %s executable: %w", probe, err)
	}
	// Probing the STAGED path assumes the anchor ignores its own name. That an
	// argument exits 2 is tested in cli/officraft; that the FILENAME cannot change
	// the answer is structural and untested (realMain returns on len(args) before
	// calling executable()).
	if err := anchorPreflight(ops, probe); err != nil {
		return fmt.Errorf("the anchor this ocwarden would deploy does not satisfy the preflight: %w", err)
	}
	if err := ops.link(probe, p.anchorPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			logf("[ocwarden] anchor cutover: %s appeared while this run was staging one; keeping the existing anchor", p.anchorPath)
			return nil
		}
		return fmt.Errorf("promote the staged anchor to %s: %w", p.anchorPath, err)
	}
	logf("[ocwarden] anchor cutover: no anchor on this machine; staged, probed and promoted %s (%d bytes)", p.anchorPath, len(data))
	return nil
}

// `officraft <any arg>` prints usage and exits 2 with ZERO side effects
// (cli/officraft/main.go realMain). Any other outcome (missing file, Gatekeeper
// kill, wrong arch) means the anchor cannot be launchd's job leader.
func anchorPreflight(ops cutoverOps, anchorPath string) error {
	code, err := ops.runExit(anchorPath, "--preflight")
	if err != nil {
		return fmt.Errorf("anchor preflight: cannot execute %s: %w", anchorPath, err)
	}
	if code != 2 {
		return fmt.Errorf("anchor preflight: %s exited %d, want 2 (a build whose anchor does not reject arguments is not the anchor this expects)", anchorPath, code)
	}
	return nil
}

// Bounded by the self-update probe budget: the anchor's usage path returns
// instantly, so a slower one is hung or quarantined and must read as "do not
// convert" rather than block startup.
func realRunExit(name string, args ...string) (int, error) {
	refuseInTestBinary("realRunExit(" + name + ")")
	ctx, cancel := context.WithTimeout(context.Background(), selfUpdateProbeBudget)
	defer cancel()
	err := exec.CommandContext(ctx, name, args...).Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// Best-effort and NEVER fails the warden: a warden that will not start is
// strictly worse than one running the old shape.
func maybeStartAnchorCutover(p wardenPaths, ppid int, logf func(string, ...any)) {
	ops := newCutoverOps()
	wardenDir := filepath.Join(p.root, "warden")

	shape := detectShape(ops, ppid, p.anchorPath)
	if shape != shapeLegacy {
		return
	}
	failedPath := filepath.Join(wardenDir, cutoverFailedName)
	if _, err := ops.readFile(failedPath); err == nil {
		logf("[ocwarden] anchor cutover: skipped — a previous attempt rolled back (%s)", failedPath)
		return
	}
	// After the sentinel check, so a machine that already rejected the
	// conversion is touched by nothing.
	if err := ensureAnchorPresent(ops, p, logf); err != nil {
		logf("[ocwarden] anchor cutover: skipped — %v", err)
		return
	}
	// Still on p.anchorPath, not the staged copy: THE FILE LAUNCHD WILL RUN is
	// what must answer correctly.
	if err := anchorPreflight(ops, p.anchorPath); err != nil {
		logf("[ocwarden] anchor cutover: skipped — %v", err)
		return
	}
	lockPath := filepath.Join(wardenDir, cutoverLockName)
	if !acquireCutoverLock(ops, lockPath) {
		logf("[ocwarden] anchor cutover: skipped — another conversion holds %s", lockPath)
		return
	}
	env := append(os.Environ(),
		"OC_BASE="+p.ocBase,
		"OC_TOKEN="+p.ocToken,
		"OC_CUTOVER_LOCK="+lockPath,
	)
	if p.namespace != "" {
		env = append(env, "OC_NAMESPACE="+p.namespace)
	}
	logPath := filepath.Join(p.logDir, "cutover.log")
	if err := ops.spawnDetached(p.binPath, []string{cutoverSubcmd}, env, logPath); err != nil {
		_ = ops.remove(lockPath)
		logf("[ocwarden] anchor cutover: could not start converter: %v", err)
		return
	}
	logf("[ocwarden] anchor cutover: legacy shape detected; detached converter started (log: %s)", logPath)
}

func acquireCutoverLock(ops cutoverOps, lockPath string) bool {
	created, err := ops.createExcl(lockPath)
	if err != nil {
		return false
	}
	if created {
		return true
	}
	mt, err := ops.modTime(lockPath)
	if err != nil || time.Since(mt) < staleLockAge {
		return false
	}
	if err := ops.remove(lockPath); err != nil {
		return false
	}
	created, err = ops.createExcl(lockPath)
	return err == nil && created
}

// Returns the output WHETHER OR NOT IT FAILED: the shared execRunner drops stdout
// on a non-zero exit, and the installer narrates on stdout, so a rolled-back
// machine's cutover.log would read only "install FAILED (exit status 1)" — every
// cause indistinguishable after the fact.
func runInstallerCombined(name string, args ...string) (string, error) {
	refuseInTestBinary("runInstallerCombined(" + name + ")")
	ctx, cancel := context.WithTimeout(context.Background(), cutoverInstallBudget)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}
