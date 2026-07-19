package main

// assets.go — the repo-file assets + folds the handlers consume: the
// language-neutral seed .md files (repo-root seeds/), the three-block
// boot-context assembly (spec/lifecycle.md §2), the derived MCP catalog hash,
// and the prebuilt binary paths.
//
// PATH ANCHOR: a static binary has no source path to derive the repo root
// from. Like the oc.toml default in config.go, every asset resolves
// CWD-relative — `bin/serve`-style launchers and the conformance harness run
// the daemon from the repo root. Tests inject their own root.
//
// SINGLE-BINARY, EMBED-ONLY: seeds ride inside the binary via go:embed (same
// staging pattern as spa.go's webdist — go:embed cannot reach outside the
// module directory, so bin/build-seedsdist copies repo-root seeds/*.md into
// seedsdist/ before a seed-carrying binary is built), and so do the prebuilt
// ocwarden/ocagent binaries + the frozen spec/mcp-catalog.json (bindist/,
// staged by bin/build-bindist — server-platform binaries only). Every read is
// EMBED-ONLY: the copy this ocserverd was built with is the only copy served.
// There is deliberately NO disk override — a stale seeds/, spec/mcp-catalog.json,
// or bin/ocwarden sitting under the CWD (a frozen repo checkout beside the
// binary) must never shadow the version-locked embed. Disk-first once let
// exactly that happen three times over (the T-e731 trilogy: stale
// boot/worker/role/lessons seeds, a stale tools/list catalog, and a stale
// bootstrap-here warden — each silent, each a content-level version regression
// with no error). This is serveBinary's stance (api_machines.go — already
// embed-only for the download routes) applied to every asset seam. A lone
// binary on a repo-less machine boots agents, installs its own warden
// (bootstrap-here materializes the embedded ocwarden to an executable file),
// and serves the binary/catalog routes from the embed alone. The committed
// prebuilt bin/ocserverd is therefore built with BOTH seedsdist AND bindist
// STAGED (it must boot agents and install-capable standalone), pristine
// (.gitkeep-only) only for webdist.

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The staged seed files (see the module comment). `all:` tolerates the
// .gitkeep-only placeholder state on a clean checkout.
//
//go:embed all:seedsdist
var seedsdistEmbed embed.FS

// seedsdistFS returns the embedded seeds root (the seedsdist/ subtree).
func seedsdistFS() fs.FS {
	sub, err := fs.Sub(seedsdistEmbed, "seedsdist")
	if err != nil {
		// The embed directive guarantees the subtree exists; reaching this is
		// a programmer error.
		panic(err)
	}
	return sub
}

// The staged prebuilt binaries + frozen MCP catalog (bin/build-bindist builds
// ocwarden/ocagent for the server's OWN GOOS/GOARCH and copies them, plus
// spec/mcp-catalog.json, into bindist/ before a self-contained binary is
// built). Same embed-only contract as seedsdist (no disk override); the embed
// carries the SERVER-platform binaries only — that is all the exec paths
// (bootstrap/teardown-here) ever need, since they install on the server host
// itself, which is by definition the same platform.
//
//go:embed all:bindist
var bindistEmbed embed.FS

// bindistFS returns the embedded binary-asset root (the bindist/ subtree).
func bindistFS() fs.FS {
	sub, err := fs.Sub(bindistEmbed, "bindist")
	if err != nil {
		panic(err)
	}
	return sub
}

// assetRoot is the repo root the file assets resolve against ("." in
// production; tests point it at the checkout).
type assetRoot string

const (
	// The seed role roster: exactly one role is seeded.
	seedRoleAssistant     = "assistant"
	seedRoleAssistantName = "Assistant"

	// The single fixed lessons task_type key.
	seedLessonsTaskType = "general"

	// The owner placeholder every seed file substitutes at read time.
	ownerPlaceholder = "{OWNER_ID}"
)

// seedRoleName returns the seed display name for roleKey, or "" when it is
// not a seed role.
func seedRoleName(roleKey string) string {
	if roleKey == seedRoleAssistant {
		return seedRoleAssistantName
	}
	return ""
}

func seedRoleKeys() []string {
	return []string{seedRoleAssistant}
}

// readSeedFile reads a seeds/*.md seed, substituting the owner placeholder.
// Embed-only (see the module comment): the seed baked into this binary, never
// a seeds/ under the CWD.
func (root assetRoot) readSeedFile(filename string) (string, error) {
	return root.readSeedFileFrom(filename, seedsdistFS())
}

// readSeedFileFrom is readSeedFile over an injectable embedded FS (tests pass
// fstest.MapFS; production passes the go:embed seedsdist). The assetRoot
// receiver no longer consults disk — a stale on-disk seed must never shadow
// the embed — so it goes unnamed.
func (assetRoot) readSeedFileFrom(filename string, embedded fs.FS) (string, error) {
	raw, err := fs.ReadFile(embedded, filename)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(raw), ownerPlaceholder, wireOwnerID), nil
}

// seedRoleDefinitionMD returns the file-backed role-definition markdown for a
// SEED roleKey ("" + false when unknown).
func (root assetRoot) seedRoleDefinitionMD(roleKey string) (string, bool, error) {
	if seedRoleName(roleKey) == "" {
		return "", false, nil
	}
	text, err := root.readSeedFile("role_def_" + roleKey + ".md")
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

// ── boot-context fold (spec/lifecycle.md §2 is normative) ────────────────────

// defaultBootRole is the fallback when neither an explicit role nor a member
// role_key is given.
const defaultBootRole = seedRoleAssistant

// resolveBootRoleKey: explicit role → member.role_key → "assistant".
func resolveBootRoleKey(role string, member *Member) string {
	if role != "" {
		return role
	}
	if member != nil && member.RoleKey != "" {
		return member.RoleKey
	}
	return defaultBootRole
}

// foldRoleDefDTO folds one role definition (owner overlay ⊕ file seed) into
// the wire DTO; nil = unknown role (caller 404s / fails closed).
func (s *apiServer) foldRoleDefDTO(roleKey string) (*roleDefDTO, error) {
	overlay, err := s.dal.GetRoleDef(roleKey)
	if err != nil {
		return nil, err
	}
	seedName := seedRoleName(roleKey)
	seedMD, hasSeed, err := s.root.seedRoleDefinitionMD(roleKey)
	if err != nil {
		return nil, err
	}
	folded := FoldRoleDef(roleKey, overlay, seedName, seedMD, hasSeed)
	if folded == nil {
		return nil, nil
	}
	return &roleDefDTO{
		Key:           folded.Key,
		Name:          folded.Name,
		DefinitionMD:  folded.DefinitionMD,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     folded.IsDefault,
		IsSeed:        folded.IsSeed,
	}, nil
}

// foldLessonsDTO folds a per-role lessons doc (owner overlay ⊕ the ONE shared
// file seed).
func (s *apiServer) foldLessonsDTO(roleKey, taskType string) (*lessonsDTO, error) {
	overlay, err := s.dal.GetLessons(roleKey, taskType)
	if err != nil {
		return nil, err
	}
	seedText, err := s.root.readSeedFile("lessons.md")
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldLessons(overlay, seedText)
	return &lessonsDTO{
		RoleKey:       roleKey,
		TaskType:      taskType,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
	}, nil
}

// foldUserContextDTO folds the owner's user-custom ADDITIVE block.
func (s *apiServer) foldUserContextDTO() (*globalContextDTO, error) {
	row, err := s.dal.GetUserContext()
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldUserContext(row)
	return &globalContextDTO{
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
		OrgName:       s.orgNameSnapshot(),
	}, nil
}

// bootContext is the folded boot package.
type bootContext struct {
	RoleKey  string
	Name     string
	TaskType string
	Context  string
}

// buildBootContext resolves the role + folds the three docs + assembles the
// boot context (lifecycle.md §2.2 normative
// order: system-interaction seed, # Role, # Lessons, user-custom block when
// non-blank, boot-sequence seed — joined "\n\n" + one trailing "\n"). nil =
// unknown role (caller maps to 404 / fail-closed).
func (s *apiServer) buildBootContext(role string, member *Member, taskType string) (*bootContext, error) {
	roleKey := resolveBootRoleKey(role, member)
	roleDTO, err := s.foldRoleDefDTO(roleKey)
	if err != nil {
		return nil, err
	}
	if roleDTO == nil {
		return nil, nil
	}
	if taskType == "" {
		taskType = seedLessonsTaskType
	}
	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		return nil, err
	}
	lessons, err := s.foldLessonsDTO(roleKey, taskType)
	if err != nil {
		return nil, err
	}
	sysSeed, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		return nil, err
	}
	bootSeed, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		return nil, err
	}
	roleTitle := roleDTO.Name
	if roleTitle == "" {
		roleTitle = roleDTO.Key
	}
	parts := []string{
		strings.TrimSpace(sysSeed),
		"# Role: " + roleTitle + "\n\n" + strings.TrimSpace(roleDTO.DefinitionMD),
		"# Lessons (" + lessons.RoleKey + " / " + lessons.TaskType + ")\n\n" +
			strings.TrimSpace(lessons.Text),
	}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			"# 使用者自訂（Owner Additions）\n\n"+strings.TrimSpace(userCtx.Text))
	}
	parts = append(parts, strings.TrimSpace(bootSeed))

	name := roleDTO.Name
	if member != nil {
		name = member.Name
	}
	return &bootContext{
		RoleKey:  roleKey,
		Name:     name,
		TaskType: taskType,
		Context:  strings.Join(parts, "\n\n") + "\n",
	}, nil
}

// ── catalog hash (normative M1 §3.2) ─────────────────────────────────────────

// catalogHashOf hashes the served MCP tool surface: every non-mcp_exclude row
// rendered "{METHOD} {path}", sorted, "\n"-joined, SHA-256, first 16 hex.
func catalogHashOf(specs []RouteSpec) string {
	var surface []string
	for _, spec := range specs {
		if !spec.MCPExclude {
			surface = append(surface, spec.Method+" "+spec.Path)
		}
	}
	sort.Strings(surface)
	sum := sha256.Sum256([]byte(strings.Join(surface, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// ── embedded prebuilt fingerprints (T-5f01 machine-table bin_status) ─────────

// binHashPrefixLen mirrors the warden's selfUpdateHashPrefixLen: the first 12
// hex chars of sha256 are the shared "which build" fingerprint vocabulary on
// both sides of the wire (warden heartbeat `binaries` ↔ these embed hashes).
// An eyeball tag, not a security checksum.
const binHashPrefixLen = 12

// binHashPrefix returns the first binHashPrefixLen hex chars of sha256(data).
func binHashPrefix(data []byte) string {
	sum := sha256.Sum256(data)
	full := hex.EncodeToString(sum[:])
	if len(full) > binHashPrefixLen {
		return full[:binHashPrefixLen]
	}
	return full
}

// bindistBinaryHashesFrom fingerprints the EMBEDDED prebuilt ocwarden/ocagent
// — the exact bytes GET /api/{warden,agent}/binary serves and the warden
// self-update swaps in verbatim, so fingerprint equality IS "this machine
// already holds the latest build" (the same raw-content oracle the warden's
// reconcileBinary uses, never a version stamp). A missing/empty embed entry
// (a pristine .gitkeep-only checkout in unit tests) is simply omitted: the
// comparison then answers unknown, never a false verdict.
func bindistBinaryHashesFrom(embedded fs.FS) map[string]string {
	hashes := map[string]string{}
	for _, name := range []string{"ocwarden", "ocagent"} {
		data, err := fs.ReadFile(embedded, name)
		if err != nil || len(data) == 0 {
			continue
		}
		hashes[name] = binHashPrefix(data)
	}
	return hashes
}

// ── prebuilt binaries + frozen MCP catalog (embed-only) ──────────────────────

// readMCPCatalogFrom reads the frozen MCP catalog from the embedded bindist
// copy ALONE. Embed-only (see the module comment): a stale spec/mcp-catalog.json
// under the CWD must never shadow the descriptor surface this binary was built
// with. Receiver unnamed — disk is not consulted.
func (assetRoot) readMCPCatalogFrom(embedded fs.FS) ([]byte, error) {
	return fs.ReadFile(embedded, "mcp-catalog.json")
}

// materializeBinary writes data as an EXECUTABLE (0755) file <dir>/<name> and
// returns its path — the embed-fallback seam for the exec paths
// (bootstrap/teardown-here need a real on-disk binary to run). dir is the
// per-instance binary cache beside the SQLite data file (apiServer.binCacheDir)
// — stable and reusable across requests, never the CWD. Idempotent: an
// existing byte-identical file is reused; anything else is replaced via a
// same-directory temp file + rename (no half-written binary is ever exec'd).
func materializeBinary(dir, name string, data []byte) (string, error) {
	dst := filepath.Join(dir, name)
	if existing, err := os.ReadFile(dst); err == nil && bytes.Equal(existing, data) {
		if err := os.Chmod(dst, 0o755); err != nil {
			return "", err
		}
		return dst, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return dst, nil
}
