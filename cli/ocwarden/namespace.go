// namespace.go — the single derivation point for OC_NAMESPACE multi-instance
// namespacing. The EMPTY namespace MUST derive values byte-identical to the
// historical constants (the golden tests pin this). The namespace arrives ONLY via
// the OC_NAMESPACE env (stamped into the warden plist by `ocwarden install`, which
// receives it from the server's install.sh / bootstrap-here line); nothing else may
// invent a suffix, so root / label / socket / agent home can never disagree.
//
// The charset is the strict intersection of what a launchd label, a path component
// and a tmux -L socket name all accept. Anything else is REFUSED: silently folding
// back to the main instance's paths would be far worse than a hard error.
package main

import (
	"fmt"
	"path/filepath"
	"regexp"
)

const envNamespaceKey = "OC_NAMESPACE"

var namespaceShape = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

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

func wardenLabelFor(ns string) string {
	if ns == "" {
		return wardenLabel
	}
	return wardenLabel + "." + ns
}

func officraftRootFor(home, ns string) string {
	if ns == "" {
		return filepath.Join(home, ".officraft")
	}
	return filepath.Join(home, ".officraft-"+ns)
}

func tmuxSocketFor(ns string) string {
	if ns == "" {
		return tmuxSocket
	}
	return tmuxSocket + "-" + ns
}

func tokfileFor(home, ns string) string {
	return filepath.Join(officraftRootFor(home, ns), "warden", "exec-warden.tok")
}
