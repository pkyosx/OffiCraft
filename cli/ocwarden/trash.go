// The warden-side trash reaper: the DELETE half of the retired "agents mv,
// warden rm" procedure. The only destructive capability in
// the package, so it is fail-closed: anything not provably "the trash dir of an
// agent workdir directly under the agents root" is REFUSED loudly, never
// "cleaned anyway".
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Not configurable on purpose: a configurable name is one more input that can be
// pointed somewhere else.
const trashDirName = "trash"

// root is agents/ (members and outsource workers) or the legacy workers/ sibling
// for residuals. Returns true ONLY when a trash dir was actually removed.
func purgeTrash(root, workdir string, logf func(string, ...any)) bool {
	warn := func(format string, a ...any) bool {
		if logf != nil {
			logf("[ocwarden trash] REFUSED: "+format, a...)
		}
		return false
	}

	if root == "" || workdir == "" {
		return warn("empty root (%q) or workdir (%q)", root, workdir)
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(workdir) {
		return warn("non-absolute root (%q) or workdir (%q)", root, workdir)
	}
	// Clean-equality rejects `id = "../.."` before Join can escape the root.
	if filepath.Clean(root) != root || filepath.Clean(workdir) != workdir {
		return warn("unclean root (%q) or workdir (%q)", root, workdir)
	}
	// Dir()==root, not HasPrefix (which admits "/x/agentsEVIL"); also rejects
	// workdir==root itself.
	if filepath.Dir(workdir) != root {
		return warn("workdir %q is not a direct child of agents root %q", workdir, root)
	}

	trash := filepath.Join(workdir, trashDirName)
	if trash == workdir || trash == root || filepath.Dir(trash) != workdir {
		return warn("derived trash path %q is not <workdir>/%s", trash, trashDirName)
	}

	// Lstat, never stat: we must see the LINK. RemoveAll would unlink rather than
	// recurse, but we refuse rather than depend on that detail.
	info, err := os.Lstat(trash)
	if os.IsNotExist(err) {
		return false
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

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return warn("cannot resolve agents root %q: %v", root, err)
	}
	realWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return warn("cannot resolve workdir %q: %v", workdir, err)
	}
	// Redo the direct-child test on RESOLVED paths, requiring the basename to
	// survive: a plain Dir(realWorkdir)==realRoot passes a workdir symlinked at a
	// NEIGHBOUR agent. Resolving both sides lets an ancestor symlink (macOS /var ->
	// /private/var) still pass. Comparing only trash against workdir/trash is a
	// tautology once both are carried by the same symlink — a panic() in that
	// branch once never fired across the whole package.
	if realWorkdir != filepath.Join(realRoot, filepath.Base(workdir)) {
		return warn("workdir %q resolves to %q — not the %q child of agents root %q (resolved %q)",
			workdir, realWorkdir, filepath.Base(workdir), root, realRoot)
	}
	// TOCTOU backstop for the Lstat above only (trash swapped for a symlink since);
	// not a containment check.
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

// launchd captures warden stderr into <logDir>/ocwarden.err.log per the plist.
// Paths only; never file CONTENT.
func stderrLogf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}
