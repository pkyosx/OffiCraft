package main

// clean — the ONE entry an agent uses to get rid of a file or a folder it made.
//
// It exists because the offboard document used to spell out the PROCEDURE
// (「臨時檔案 `mv` 進 `<你的工作目錄>/trash/`（不要自己 `rm`）」), and a procedure
// written in prose is a second source of truth that nothing keeps in step: move
// the quarantine directory, change who reclaims it, decide something should be
// kept — and that paragraph becomes a lie with nothing to redden. Owner
// 2026-08-16: 「收拾程序我在想是不是應該改成 ocagent 的 command... 例如
// `ocagent clean <path>` 不要把實作暴露出來」, and 2026-08-20 on the scope:
// 「可以指定 file / folder, 用來取代 rm -rf」.
//
// That second truth is now GONE for the quarantine location: every seed that
// used to name a directory now names this command instead — seeds/offboard.md
// §4, and seeds/system_interaction.md §3.5 and §3.6 — so this file is the only
// place that decides where quarantine lives.
//
// ⚠️ This command's scope is FILES AND FOLDERS ONLY. Branches, worktrees and
// processes are deliberately not here: nothing registers which of those belong
// to which agent, so a command that moves things cannot be asked to guess
// (owner 2026-08-20 scope ruling). Do not grow it into them.
//
// ⚠️ seeds/system_interaction.md 附錄 A — NOT the offboard document — tells the
// reader that an ocagent lacking this subcommand answers 「unknown subcommand」,
// and to skip that item and note it in the hand-off rather than stall. The
// dispatch in main.go prints usage and exits 2, whose first line carries that
// exact phrase; changing it so an unknown subcommand fails some OTHER way would
// make that sentence wrong.
//
// 🔴 IT DELETES NOTHING. "Replaces rm -rf" is about the ENTRY, not the effect:
// the owner's own contract for this command says quarantine/move, never rm. So
// the target is RENAMED under <root>/trash/ and stays readable — a wrong path
// costs a move, not the file. Nothing in here may grow an os.Remove or
// os.RemoveAll of a caller-named path — and that is not a promise in prose:
// TestCleanNeverDeletesAnything (clean_test.go) parses THIS FILE's syntax tree
// and reddens on either call by name. Said in a file whose entire argument is
// that prose nobody can falsify becomes a lie, an unenforced sentence here
// would have been the same mistake one paragraph later.
//
// WHAT IT IS NOT
//
//   - It does NOT collect branches, worktrees or processes. Those are different
//     resources and ocagent has ZERO knowledge of which worktree or which pid
//     belongs to this agent — nothing registers them anywhere. Guessing is not
//     available to a command that moves things, so the offboard document keeps
//     describing those three by hand and says why (owner 2026-08-20, scope
//     ruling above).
//   - It does NOT scan. Every path is named by the caller; there is no "find my
//     junk" mode, because "what is junk" is exactly the judgement a command
//     cannot make on the agent's behalf.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// quarantineDirName is where this command decides the quarantine location, and
// the intent is that it becomes the ONLY place — move it here and every agent
// follows on the next binary, which is the entire point of turning the
// procedure into a command.
//
// It IS the only place now: seeds/offboard.md §4 names the command instead of a
// directory, so changing this constant changes where quarantine lives for every
// agent on the next binary and leaves no prose behind to contradict it.
//
// ⚠️ ocwarden is the other half of the contract and it is NOT downstream of this
// constant — it reclaims by its own literal (cli/ocwarden/trash.go). Moving
// quarantine here without moving it there strands the files instead of freeing
// them.
const quarantineDirName = "trash"

// OC_BASE CLASSIFICATION (T-86): EXEMPT, and the apparent contradiction in the
// original proposal dissolves once the two variables are kept apart. clean
// contacts no station — it moves a file into a quarantine directory on this
// host — so OC_BASE, which is only ever a station ADDRESS, has nothing to say
// here and the guard the other subcommands carry is not added.
//
// What clean does read is OC_ID / OC_TOKEN, and it reads them for IDENTITY, not
// for an address: they are how it works out which workdir is its own. That
// requirement is already enforced, loudly and by refusal, in cleanRoot below —
// so clean is exempt from the OC_BASE guard and NOT exempt from naming its
// missing identity, which is the consistent reading of "it needs configuration"
// rather than a case of being both exempt and refusing.
//
// cleanRoot resolves the ONLY tree this command may touch: this agent's own
// workdir. Same derivation as cursorPath / replyCardSeenPath /
// reportStampPath (listen.go, contextreport.go) — one expression for "where my
// files live", not five.
//
// An empty id is a REFUSAL, not a fallback. cursorPath degrades to "anon"
// because losing a dedup cursor costs a refetch; here the same fallback would
// point two identity-less sessions at ONE quarantine tree and let either move
// the other's files. The cheap failure and the expensive one are not the same
// call.
func cleanRoot(cfg Config) (string, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return "", errors.New("no agent id (OC_ID / OC_TOKEN): cannot tell which workdir is mine")
	}
	if strings.TrimSpace(cfg.Home) == "" {
		return "", errors.New("no agent home (OC_AGENT_HOME): cannot tell which workdir is mine")
	}
	root := filepath.Join(cfg.Home, strings.ToLower(cfg.ID))
	// Resolve the root through symlinks ONCE, so a symlinked home (a real shape
	// on macOS, where /tmp is a link to /private/tmp) does not make every target
	// look like an escape.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return filepath.Clean(root), nil
}

// canonicalise resolves a path through symlinks WITHOUT requiring it to exist:
// it walks up to the deepest ancestor that RESOLVES, resolves that, and
// re-attaches the tail it walked past. EvalSymlinks alone cannot be used
// because it fails on a missing leaf — and a missing leaf is ordinary here (an
// idempotent re-run, or a quarantine directory that has not been created yet).
//
// What it still catches is the escape that matters: a symlink anywhere in the
// part that DOES resolve.
//
// 🔴 The loop condition is EvalSymlinks succeeding, not Lstat succeeding. A
// DANGLING symlink is a path that Lstat answers for but EvalSymlinks cannot
// resolve — with the Lstat condition the walk stopped there and the whole path
// came back completely unresolved, so on any host whose workdir sits behind a
// symlink (macOS: /tmp → /private/tmp, /var → /private/var) an ordinary
// dangling link compared as OUTSIDE the root and `clean` refused it.
func canonicalise(abs string) string {
	abs = filepath.Clean(abs)
	existing := abs
	var tail []string
	for {
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			existing = resolved
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			// Walked to the filesystem root without resolving anything.
			break
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
	return filepath.Clean(filepath.Join(append([]string{existing}, tail...)...))
}

// isUnder reports whether path is strictly inside root (the root itself is not).
func isUnder(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// insideRoot runs the whole in-my-workdir judgement on ONE path. It is called
// twice per argument — see resolveInsideRoot for why two paths, not one.
func insideRoot(root, path, arg string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("outside my workdir: %s", arg)
	}
	if rel == "." {
		return fmt.Errorf("that is my workdir itself, not something in it: %s", arg)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("outside my workdir: %s", arg)
	}
	// The quarantine directory is not cleanable: moving trash into trash nests
	// forever, and the whole point of quarantine is that it is the one place
	// this command does not touch.
	//
	// 🔴 EqualFold, not ==. This ran as a case-SENSITIVE comparison while the
	// filesystem underneath it (macOS APFS, the default here) is case-
	// INSENSITIVE, so `clean <root>/TRASH/tmp/x` named the very directory this
	// line protects, missed the string compare, and nested the parked file at
	// trash/TRASH/... — the "nests forever" outcome the sentence above claims
	// to prevent, reachable by holding shift.
	//
	// On a case-sensitive host TRASH/ really is a different directory and this
	// costs one false refusal there. That is the right side to be wrong on: a
	// false refusal costs a rename and a retry, a false accept moves evidence
	// that was already parked.
	if strings.EqualFold(strings.Split(rel, string(filepath.Separator))[0], quarantineDirName) {
		return fmt.Errorf("that is already in %s/: %s", quarantineDirName, arg)
	}
	return nil
}

// resolveInsideRoot proves one caller-named path lands strictly inside root and
// returns the path the move must actually operate on.
//
// 🔴 Two paths, two jobs — conflating them was a real escape.
//
//	VALIDATE against canonicalise(abs): every symlink resolved, so a link
//	pointing out of the tree is caught no matter how it is spelled.
//	OPERATE on abs, the caller's own spelling, UNRESOLVED.
//
// Returning the canonical path meant that `clean <a symlink>` handed
// os.Rename the POINTEE. Rename does not follow a leaf symlink, so given
// `inlink -> d.txt` (both in-tree) the command quarantined `d.txt` — a path
// the caller never named — left `inlink` behind, now dangling, and exited 0.
// A command that replaces `rm -rf` on the close-out path may not report
// success while moving a different file than the one it was given.
//
// So the leaf symlink is treated as the object it is: it is what gets moved,
// its target is not touched. Three shapes, all defined and all tested:
//   - points OUT of the tree → REFUSED (canonicalise lands outside)
//   - points INSIDE the tree → the LINK moves, the pointee stays
//   - dangling               → the LINK moves (no bytes live behind it),
//     INCLUDING when it dangles at a path outside the tree. That is the fourth
//     shape and it is deliberately NOT lumped in with "points out": nothing is
//     out there to reach, canonicalise resolves the deepest existing ancestor
//     and gets no further, and the only byte that exists is the link itself,
//     which is in the tree and is what the caller named. Refusing it would
//     strand exactly the debris this command exists to clear.
//
// Rejecting a symlink leaf outright was the other candidate. It is worse: an
// agent's own leftovers routinely include symlinks (node_modules/.bin, venv,
// build caches), so `clean` would refuse exactly the junk it exists to clear
// and the agent would reach for `rm -rf` again — the outcome this command was
// written to prevent.
//
// The intermediate components are a different question and stay resolved: a
// symlinked DIRECTORY in the middle of the path is traversed by Rename itself,
// so validating it unresolved would prove nothing about where the move lands.
//
// The other subtle half is that the target may NOT EXIST (idempotent re-runs),
// which is why canonicalise resolves the deepest ancestor it can rather than
// demanding the whole path resolve.
//
// "Strictly inside" excludes the root itself: `ocagent clean <my workdir>`
// would quarantine the agent's whole home INTO its own quarantine directory,
// which is both nonsense and unrecoverable in one step.
func resolveInsideRoot(root, arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", fmt.Errorf("cannot resolve: %w", err)
	}
	abs = filepath.Clean(abs)

	// The path to OPERATE on: directory canonicalised, leaf left alone. The
	// directory has to be resolved or nothing compares — cleanRoot resolves the
	// root, and on macOS an agent typing /tmp/... would otherwise look outside a
	// root spelled /private/tmp/.... The leaf is left unresolved because it is
	// the object the caller named.
	full := filepath.Join(canonicalise(filepath.Dir(abs)), filepath.Base(abs))

	// Both must be inside: the fully-resolved path is the security check, and
	// the path that will actually be renamed has to be checked too — a guard
	// covering only the other one would be guarding something that never runs.
	if err := insideRoot(root, canonicalise(abs), arg); err != nil {
		return "", err
	}
	if err := insideRoot(root, full, arg); err != nil {
		return "", err
	}
	return full, nil
}

// quarantineDest picks where one target lands under <root>/trash/, PRESERVING
// its path relative to the root so the move stays readable ("what was this?"
// is answerable a day later). A destination that already exists gets -2, -3, …
// rather than being overwritten — re-running clean must never destroy the
// evidence the previous run parked.
func quarantineDest(root, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, quarantineDirName, rel)
	dest := base
	for i := 2; ; i++ {
		// 🔴 Three outcomes, not two. This used to break only on ENOENT and
		// treat EVERYTHING else as "that slot is taken, try the next name" —
		// so a plain FILE sitting where trash/tmp has to be a directory
		// (ENOTDIR), a symlink loop (ELOOP) or an unreadable parent (EACCES)
		// span all 1000 candidates and then answered "too many quarantined
		// copies of <rel>" when there were none. Nothing unsafe happened, but
		// the one thing this command owes its caller is a true account of
		// where the file went, and that answer sent a human looking for 1000
		// copies that do not exist while never naming the file in the way.
		_, err := os.Lstat(dest)
		if err != nil {
			if os.IsNotExist(err) {
				break // free slot
			}
			return "", fmt.Errorf("cannot inspect the %s/ slot %s: %w", quarantineDirName, rel, err)
		}
		if i > 1000 {
			return "", fmt.Errorf("too many quarantined copies of %s", rel)
		}
		dest = fmt.Sprintf("%s-%d", base, i)
	}

	// 🔴 The landing site has to be proven inside the tree, not assumed.
	//
	// resolveInsideRoot guards the ENTRANCE; this guards the EXIT, and the two
	// are separate escapes. Checking only `<root>/trash` itself is NOT enough:
	// `dest` is `<root>/trash/<rel>`, and both MkdirAll and Rename follow every
	// intermediate component — so a symlink one level deeper (say
	// `<root>/trash/tmp -> /somewhere/else`) sends the file out of the tree
	// while the command prints an in-tree path and exits 0. That shape is
	// reachable in two ordinary steps: clean a directory that itself contains
	// an outward symlink (it is parked under trash/ intact), then later clean a
	// real path whose rel traverses that parked name.
	//
	// So: resolve the parent and require it to be under root BEFORE anything is
	// created. This mirrors what ocwarden does before purging trash
	// (cli/ocwarden/trash.go compares EvalSymlinks on both sides) — one half
	// checking with Lstat and the other with a real resolution would be two
	// different definitions of the same directory.
	parent := filepath.Dir(dest)
	// 🔴 Check BEFORE creating. MkdirAll follows symlinks too, so validating
	// afterwards is already too late: it would have created a directory on the
	// far side of the link. canonicalise gives the resolved path without needing
	// it to exist first, which is exactly the case here.
	//
	// 🔴 And the bar is the QUARANTINE, not the workdir. "Under root" was the
	// weaker claim and it let an escape through that never leaves the tree:
	// with `<root>/trash/tmp -> <root>/live`, cleaning `<root>/tmp/sub/x.txt`
	// passed (the destination resolves under root), exited 0, and printed
	// `<root>/trash/tmp/sub/x.txt` — while the file landed in
	// `<root>/live/sub/x.txt` and trash/ stayed empty. Reporting success while
	// naming a path the file is not at is this command's own worst failure
	// class; staying inside the tree does not soften it, because what the
	// agent is handed is the location of its parked copy.
	//
	// The comparison is against the LITERAL <root>/trash, deliberately not
	// against its resolution: if trash itself were a symlink, resolving both
	// sides would re-admit exactly this hole one level up. So a symlinked
	// quarantine at any level is refused — the same stance ocwarden takes
	// before purging it (cli/CLAUDE.md §5).
	quarantine := filepath.Join(root, quarantineDirName)
	resolvedParent := canonicalise(parent)
	if resolvedParent != quarantine && !isUnder(quarantine, resolvedParent) {
		return "", fmt.Errorf("the %s/ path does not stay in %s/", quarantineDirName, quarantineDirName)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("cannot open quarantine: %w", err)
	}
	// 🔴 There used to be a second EvalSymlinks(parent) re-check here, sold as
	// "the tail may have been created as a symlink between the two calls". It
	// was DEAD CODE — deleting it left every test green, and no test could be
	// written for it, because single-threaded it cannot fire:
	//
	//   • if parent already exists, canonicalise(parent) IS EvalSymlinks(parent)
	//     — the pre-check above already made exactly this comparison;
	//   • if it does not, MkdirAll only ever creates real directories, never
	//     symlinks, so the resolution afterwards equals the pre-check's answer;
	//   • every shape that could make the two disagree (dangling link, symlink
	//     loop, a plain file in the way) makes MkdirAll itself fail first, and
	//     that error returns above.
	//
	// The only remaining gap was a genuine race — another process swapping a
	// component in between — and the re-check did not close that either: Rename
	// still runs after it, so the same swap one instruction later wins anyway.
	// Closing it properly needs openat/renameat on a held fd, which is a real
	// change, not a re-check. An untested guard that looks like it closes a hole
	// it does not close is worse than its absence: the next reader trusts it.
	return dest, nil
}

// cmdClean is the whole command: validate EVERY path, then move.
//
// 🔴 The two phases are not cosmetic. This runs on the close-out path, under
// time pressure, and it replaces `rm -rf` — so a run that half-succeeds is the
// worst outcome available: the agent cannot tell what happened and neither can
// the next one. Every refusal therefore costs zero moves.
func cmdClean(cfg Config, args []string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "[ocagent] clean: at least one <path> argument is required")
		fmt.Fprintln(out, "usage: ocagent clean <path>...")
		return 2
	}

	root, err := cleanRoot(cfg)
	if err != nil {
		fmt.Fprintf(out, "[ocagent] clean: %v\n", err)
		return 2
	}

	// ── phase 1: validate all, move none ────────────────────────────────────
	// Each entry keeps the caller's spelling beside the resolved path: the
	// output has to name the path THEY typed, and pairing them here means the
	// two can never drift apart if the all-or-nothing gate below is ever
	// relaxed (index-matching against args would silently mislabel every line
	// after the first refusal).
	type target struct{ arg, full string }
	targets := make([]target, 0, len(args))
	var refusals []string
	for _, a := range args {
		full, err := resolveInsideRoot(root, a)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("  %s — %v", a, err))
			continue
		}
		targets = append(targets, target{arg: a, full: full})
	}
	if len(refusals) > 0 {
		fmt.Fprintf(out, "[ocagent] clean: refused, NOTHING was moved (my workdir is %s)\n", root)
		for _, r := range refusals {
			fmt.Fprintln(out, r)
		}
		return 2
	}

	// ── phase 2: move ───────────────────────────────────────────────────────
	rc := 0
	for _, t := range targets {
		if _, err := os.Lstat(t.full); os.IsNotExist(err) {
			// Idempotent: already gone is the state the caller asked for.
			fmt.Fprintf(out, "[ocagent] clean: %s — already gone\n", t.arg)
			continue
		}
		dest, err := quarantineDest(root, t.full)
		if err != nil {
			fmt.Fprintf(out, "[ocagent] clean: %s — %v\n", t.arg, err)
			rc = 1
			continue
		}
		if err := os.Rename(t.full, dest); err != nil {
			fmt.Fprintf(out, "[ocagent] clean: %s — not moved: %v\n", t.arg, err)
			rc = 1
			continue
		}
		fmt.Fprintf(out, "[ocagent] clean: %s → %s\n", t.arg, dest)
	}
	return rc
}
