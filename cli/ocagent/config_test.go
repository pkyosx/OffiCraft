// Skeleton generated from cli/ocagent/config.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestRequireBase(t *testing.T) {
	t.Skip("TODO: requireBase is the OC_BASE half of the mis-wire guard, the twin of the \"no OC_TOKEN configured\" refusals upload/download/diff already carry.")
}

func TestLoadConfig(t *testing.T) {
	t.Skip("TODO: loadConfig resolves OC_* env into a Config (mirrors agent/oc_agent.py load_config).")
}

func TestFallbackAgentsHome(t *testing.T) {
	t.Skip("TODO: fallbackAgentsHome derives THIS INSTANCE's agents root: <root>/agents where root is ~/.officraft for the main instance and ~/.officraft-<ns> otherwise (path syntax uses a dash — see the shared table).")
}

func TestJwtSub(t *testing.T) {
	t.Skip("TODO: jwtSub reads the `sub` claim of a JWT WITHOUT verifying (the agent holds no secret — it only decodes its OWN token to learn its identity; the server re-verifies every gated call).")
}
