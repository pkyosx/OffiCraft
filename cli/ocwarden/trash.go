// T-684c: the warden-side trash reaper — the DELETE half of "agents mv, warden rm".
//
// WHY THIS EXISTS (read before touching the guards):
//
// A headless agent told to "clean up your scratch files" used to run
// `rm -rf <workdir>/tmp/<task>` itself. Claude Code's harness has a BUILT-IN
// dangerous-rm confirmation ("Dangerous rm operation on working directory or its
// ancestor" + Yes/No) that NO settings/permission entry can waive; nobody is
// sitting in front of a headless agent to press Yes, so the agent hangs SILENTLY
// until it is reaped. The fix is NOT "mv is safer than rm" — an experiment showed
// relative/absolute x mv/rm all behave identically in that environment, so the verb
// has ZERO discriminating power. The fix is WHO EXECUTES THE DELETE: the seeds now
// tell agents to `mv` their scratch into <workdir>/trash/ and NEVER rm, and THIS
// file does the actual removal from ocwarden — an independent Go daemon started by
// launchd with no claude in the chain, so the harness gate simply does not apply.
//
// FAIL-CLOSED CONTRACT (this is the only destructive capability in the package):
// purgeTrash removes <workdir>/trash and NOTHING else. Every shape that is not
// provably "the trash dir of an agent workdir directly under the agents root" is
// REFUSED, LOUDLY (warden stderr → <logDir>/ocwarden.err.log), never silently
// skipped and never "cleaned anyway". Refusing costs a few stale megabytes;
// guessing wrong costs the owner's data.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// trashDirName is the ONE directory name the seeds tell agents to mv into and the
// ONE name this reaper will ever remove. Not configurable on purpose — a
// configurable name is one more input that can be pointed somewhere else.
const trashDirName = "trash"

// purgeTrash removes <workdir>/trashDirName, or refuses.
//
// root is the agent-state base the workdir MUST live directly under (agents/ for
// members and P5b outsource workers, the legacy workers/ sibling for pre-P5b
// residuals). logf (nil-skipped) receives refusal / outcome lines.
//
// Returns true ONLY when a trash dir was actually removed. false covers both
// "nothing to do" (no trash dir — the normal state) and "refused" (a guard
// tripped); the caller never acts on the difference, the log is the signal.
//
// GUARDS, in order — each one is a shape someone could steer this at:
//
//	G1 empty root / empty workdir      — an unresolved HOME or a blank member id
//	                                     would otherwise make Join() produce
//	                                     "trash" (relative, = CWD/trash).
//	G2 non-absolute root or workdir    — a relative path resolves against whatever
//	                                     CWD the daemon happens to have.
//	G3 unclean path (".." / "." / dup
//	   separators / trailing slash)    — `agent id = "../.."` makes
//	                                     Join(root, id) escape the agents root;
//	                                     Clean-equality rejects the input before
//	                                     the escape is even computed.
//	G4 workdir not a DIRECT child of
//	   root                            — the positive containment check. Not a
//	                                     HasPrefix test (that admits
//	                                     "/x/agentsEVIL"); Dir(workdir)==root is
//	                                     exact, and it also rejects
//	                                     workdir==root itself (Dir(root)!=root),
//	                                     so the whole agents tree can never be
//	                                     the target.
//	G5 trash is a SYMLINK              — lstat (never stat): a `trash -> /` symlink
//	                                     planted in a workdir would otherwise make
//	                                     RemoveAll follow it. RemoveAll actually
//	                                     unlinks a symlink rather than recursing,
//	                                     but we refuse anyway rather than depend on
//	                                     that implementation detail.
//	G6 trash is not a directory        — a plain file named trash is not ours.
//	G7 symlinked ANCESTOR redirection  — EvalSymlinks(workdir)/trash must equal
//	                                     EvalSymlinks(trash). Ancestor symlinks are
//	                                     legitimate on macOS (/var -> /private/var),
//	                                     so we compare the workdir-relative resolved
//	                                     paths instead of demanding trash resolve to
//	                                     itself; any redirection that moves trash out
//	                                     from under its own workdir is refused.
func purgeTrash(root, workdir string, logf func(string, ...any)) bool {
	warn := func(format string, a ...any) bool {
		if logf != nil {
			logf("[ocwarden trash] REFUSED: "+format, a...)
		}
		return false
	}

	// G1 — nothing derivable from empty strings.
	if root == "" || workdir == "" {
		return warn("empty root (%q) or workdir (%q)", root, workdir)
	}
	// G2 — absolute only.
	if !filepath.IsAbs(root) || !filepath.IsAbs(workdir) {
		return warn("non-absolute root (%q) or workdir (%q)", root, workdir)
	}
	// G3 — reject before normalising: an input that is not already clean carries
	// traversal ("..") or sloppiness we did not intend to accept.
	if filepath.Clean(root) != root || filepath.Clean(workdir) != workdir {
		return warn("unclean root (%q) or workdir (%q)", root, workdir)
	}
	// G4 — exact direct-child containment.
	if filepath.Dir(workdir) != root {
		return warn("workdir %q is not a direct child of agents root %q", workdir, root)
	}

	trash := filepath.Join(workdir, trashDirName)
	// Belt-and-braces on the join itself (unreachable given G3/G4, asserted anyway
	// because everything below this line deletes).
	if trash == workdir || trash == root || filepath.Dir(trash) != workdir {
		return warn("derived trash path %q is not <workdir>/%s", trash, trashDirName)
	}

	// G5/G6 — lstat, never stat: we must see the LINK, not its target.
	info, err := os.Lstat(trash)
	if os.IsNotExist(err) {
		return false // the normal state: nothing was ever moved here.
	}
	if err != nil {
		return warn("cannot lstat %q: %v", trash, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return warn("%q is a symlink — refusing to follow it", trash)
	}
	if !info.IsDir() {
		return warn("%q is not a directory (mode %v)", trash, info.Mode())
	}

	// G7 — an ancestor symlink must not have moved trash out from under workdir.
	realWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return warn("cannot resolve workdir %q: %v", workdir, err)
	}
	realTrash, err := filepath.EvalSymlinks(trash)
	if err != nil {
		return warn("cannot resolve %q: %v", trash, err)
	}
	if realTrash != filepath.Join(realWorkdir, trashDirName) {
		return warn("%q resolves to %q, outside its own workdir %q", trash, realTrash, realWorkdir)
	}

	if err := os.RemoveAll(trash); err != nil {
		return warn("removing %q failed: %v", trash, err)
	}
	if logf != nil {
		logf("[ocwarden trash] purged %q", trash)
	}
	return true
}

// stderrLogf is the production log sink for the reaper — warden stderr, which
// launchd captures into <logDir>/ocwarden.err.log per the plist. Paths only; the
// reaper never formats file CONTENT into a log line.
func stderrLogf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}
