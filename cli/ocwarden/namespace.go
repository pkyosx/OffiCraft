// namespace.go — the single derivation point for same-machine multi-instance
// namespacing (OC_NAMESPACE). A namespace is ONE short suffix that keys every
// per-instance host resource the warden touches:
//
//	ns == ""    → root=~/.officraft    label=com.officraft.ocwarden    socket=officraft
//	ns == "x"   → root=~/.officraft-x  label=com.officraft.ocwarden.x  socket=officraft-x
//
// The EMPTY namespace is the default and MUST derive values byte-identical to
// the historical constants — the main instance's install/spawn/teardown output
// does not change by a single byte (the golden tests pin this). The namespace
// arrives ONLY via the OC_NAMESPACE env (stamped into the warden plist by
// `ocwarden install`, which itself receives it from the server's install.sh /
// bootstrap-here line); nothing else may invent a suffix, so the four axes
// (root / label / socket / agent home) can never disagree.
//
// The charset is locked to the strict intersection of what a launchd label, a
// filesystem path component, and a tmux -L socket name all accept: lowercase
// [a-z0-9-], 1..16 chars. Anything else is REFUSED loudly (installer and
// runtime both validate) — a malformed namespace silently folding back to the
// main instance's paths would be far worse than a hard error.
package main

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// envNamespaceKey is the single propagation env for the instance namespace.
const envNamespaceKey = "OC_NAMESPACE"

// namespaceShape locks the charset (see the package comment for why).
var namespaceShape = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// namespaceFromEnv reads + validates OC_NAMESPACE. Empty (unset) is the main
// instance and valid; a non-empty value outside the locked charset is an error.
func namespaceFromEnv(env func(string) string) (string, error) {
	ns := env(envNamespaceKey)
	if ns == "" {
		return "", nil
	}
	if !namespaceShape.MatchString(ns) {
		return "", fmt.Errorf("OC_NAMESPACE must match [a-z0-9-]{1,16}, got: %q", ns)
	}
	return ns, nil
}

// wardenLabelFor derives the launchd label: canonical for the empty namespace,
// dot-suffixed otherwise (label syntax uses dots).
func wardenLabelFor(ns string) string {
	if ns == "" {
		return wardenLabel
	}
	return wardenLabel + "." + ns
}

// officraftRootFor derives the per-machine data root: ~/.officraft for the
// empty namespace, ~/.officraft-<ns> otherwise (path syntax uses a dash).
func officraftRootFor(home, ns string) string {
	if ns == "" {
		return filepath.Join(home, ".officraft")
	}
	return filepath.Join(home, ".officraft-"+ns)
}

// tmuxSocketFor derives the tmux -L socket: the shared canonical socket for the
// empty namespace, dash-suffixed otherwise.
func tmuxSocketFor(ns string) string {
	if ns == "" {
		return tmuxSocket
	}
	return tmuxSocket + "-" + ns
}

// tokfileFor derives the exec-warden token file under the namespaced root
// (byte-identical to $HOME + defaultTokfileRel for the empty namespace).
func tokfileFor(home, ns string) string {
	return filepath.Join(officraftRootFor(home, ns), "warden", "exec-warden.tok")
}
