// Skeleton generated from server/ocserverd/authz.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestClassifyMember(t *testing.T) {
	t.Skip("TODO: classifyMember classifies an AGENT-scoped caller's member row into its principal class (service.authz.classify_member).")
}

func TestResolvePrincipal(t *testing.T) {
	t.Skip("TODO: resolvePrincipal is THE single resolver (service.authz.resolve_principal): verified claims → principal class.")
}

func TestRevocationRefusal(t *testing.T) {
	t.Skip("TODO: ── machine-roster credential revocation (T-9cf8) ─────────────────────────── WHY THIS EXISTS: \"機器清單應該由我的機器名冊決定 — 一但刪除了 該機器的 token 應該要失效\".")
}

func TestPermanentCredentialRefusal(t *testing.T) {
	t.Skip("TODO: permanentCredentialRefusal confines exp-less JWTs to the credential class that is allowed to be permanent: an active warden roster row.")
}

func TestAgentIatFloorRefusal(t *testing.T) {
	t.Skip("TODO: agentIatFloorRefusal is the AGENT-side credential floor (T-14 項目 4B): an agent-scope token whose `iat` is STRICTLY LESS THAN its own member's agent_iat_floor was minted for a generation that member has already replaced, and is refused.")
}

func TestMachineRevokedMsg(t *testing.T) {
	t.Skip("TODO: machineRevokedMsg is the refusal text.")
}

func TestRequirePrincipalClass(t *testing.T) {
	t.Skip("TODO: requirePrincipalClass wraps a handler with the ONE RBAC enforcement choke the route table attaches (service.authz.require_principal_class): the request's principal (resolved from the claims the auth middleware stashed + the roster lookup) must rank at or above minimum, or the request is a flat 403.")
}

func TestAssertAllRoutesDeclareRequires(t *testing.T) {
	t.Skip("TODO: assertAllRoutesDeclareRequires is the fail-closed boot assertion (service.authz.assert_all_routes_declare_requires, app.py spirit): EVERY route row must declare a KNOWN requires class, consistent with its auth label (auth==\"public\" ⟺ requires==\"public\").")
}

func TestAssertAllRoutesLabelled(t *testing.T) {
	t.Skip("TODO: assertAllRoutesLabelled is the deny-by-default auth-label boot assertion (plumbing.auth.assert_all_routes_labelled): every route must carry a KNOWN auth label; anything else refuses to start.")
}
