// Skeleton generated from server/ocserverd/assets.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestSeedsdistFS(t *testing.T) {
	t.Skip("TODO: seedsdistFS returns the embedded seeds root (the seedsdist/ subtree).")
}

func TestBindistFS(t *testing.T) {
	t.Skip("TODO: bindistFS returns the embedded binary-asset root (the bindist/ subtree).")
}

func TestDocsdistFS(t *testing.T) {
	t.Skip("TODO: docsdistFS returns the embedded docs root (the docsdist/ subtree).")
}

func TestSeedRoleName(t *testing.T) {
	t.Skip("TODO: seedRoleName returns the seed display name for roleKey, or \"\" when it is not a seed role.")
}

func TestSeedBlockMD(t *testing.T) {
	t.Skip("TODO: seedBlockMD reads one shipped boot-context seed and reports whether it EXISTS (T-791e).")
}

func TestSeedRoleDefinitionMD(t *testing.T) {
	t.Skip("TODO: seedRoleDefinitionMD returns the file-backed role-definition markdown for a SEED roleKey (\"\" + false when unknown).")
}

func TestSeedInsightMDFrom(t *testing.T) {
	t.Skip("TODO: seedInsightMDFrom is seedInsightMD over an injectable embedded FS, so a test can present a world with MORE THAN ONE seeded role — the only world in which \"per-role\" is an observable property at all.")
}

func TestSafeSeedRoleKey(t *testing.T) {
	t.Skip("TODO: safeSeedRoleKey reports whether roleKey may be interpolated into a seed filename.")
}

func TestResolveBootRoleKey(t *testing.T) {
	t.Skip("TODO: resolveBootRoleKey: explicit role → member.role_key → \"assistant\".")
}

func TestFoldRoleDefDTO(t *testing.T) {
	t.Skip("TODO: foldRoleDefDTO folds one role definition (owner overlay ⊕ file seed) into the wire DTO; nil = unknown role (caller 404s / fails closed).")
}

func TestFoldLessonsDTO(t *testing.T) {
	t.Skip("TODO: foldLessonsDTO folds a per-role lessons doc (owner overlay ⊕ the ONE shared file seed).")
}

func TestFoldUserContextDTO(t *testing.T) {
	t.Skip("TODO: foldUserContextDTO folds the owner's user-custom ADDITIVE block.")
}

func TestBootSequenceSeedName(t *testing.T) {
	t.Skip("TODO: bootSequenceSeedName picks the boot-sequence seed for a runtime.")
}

func TestBootSequenceDocKey(t *testing.T) {
	t.Skip("TODO: bootSequenceDocKey names the EDITABLE DOCUMENT that carries a runtime's boot sequence (T-791e).")
}

func TestBootSequenceSeedForKey(t *testing.T) {
	t.Skip("TODO: bootSequenceSeedForKey resolves a boot_sequence DOCUMENT KEY (as it arrives on the URL) back to its seed filename, reporting whether the key names a real document at all.")
}

func TestBuildBootContext(t *testing.T) {
	t.Skip("TODO: buildBootContext resolves the role + folds the role docs + assembles the boot context (lifecycle.md §2.2 normative order: system-interaction seed, user-custom block when non-blank, # Role, # Insight when non-blank, # Lessons, boot-sequence seed — joined \"\\n\\n\" + one trailing \"\\n\").")
}

func TestCatalogHashOf(t *testing.T) {
	t.Skip("TODO: ── catalog hash (normative M1 §3.2) ───────────────────────────────────────── catalogHashOf hashes the served MCP tool surface: every non-mcp_exclude row rendered \"{METHOD} {path}\", sorted, \"\\n\"-joined, SHA-256, first 16 hex.")
}

func TestBinHashPrefix(t *testing.T) {
	t.Skip("TODO: binHashPrefix returns the first binHashPrefixLen hex chars of sha256(data).")
}

func TestBindistBinaryHashesFrom(t *testing.T) {
	t.Skip("TODO: bindistBinaryHashesFrom fingerprints the EMBEDDED prebuilt ocwarden/ocagent — the exact bytes GET /api/{warden,agent}/binary serves and the warden self-update swaps in verbatim, so fingerprint equality IS \"this machine already holds the latest build\" (the same raw-content oracle the warden's reconcileBinary uses, never a version stamp).")
}

func TestMaterializeBinary(t *testing.T) {
	t.Skip("TODO: materializeBinary writes data as an EXECUTABLE (0755) file <dir>/<name> and returns its path — the embed-fallback seam for the exec paths (bootstrap/teardown-here need a real on-disk binary to run).")
}
