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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
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

// The staged product-guide docs (bin/build-docsdist copies the repo-root
// docs/guide/ tree — every *.md plus the assets/ image subtree — into
// docsdist/ before a doc-carrying binary is built). Same embed-only contract
// as seedsdist: the doc bytes baked into THIS binary are the only copy served,
// so the 座艙's 使用說明 nav tab and Mira's get_doc MCP tool read one identical
// source (zero copy → zero drift). `all:` tolerates the .gitkeep-only
// placeholder state on a clean checkout (O-46 content may be unmerged).
//
//go:embed all:docsdist
var docsdistEmbed embed.FS

// docsdistFS returns the embedded docs root (the docsdist/ subtree).
func docsdistFS() fs.FS {
	sub, err := fs.Sub(docsdistEmbed, "docsdist")
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

// seedBlockMD reads one shipped boot-context seed and reports whether it EXISTS
// (T-791e). It is readSeedFile with the missing-file case answered instead of
// raised, for the same reason seedInsightMD does it: "there is no factory
// version of this document" is a legitimate answer that the reset/compare faces
// turn into a 404, while any other IO error must still propagate (fail-closed —
// a read that failed for a real reason may never be laundered into "no seed").
func (root assetRoot) seedBlockMD(filename string) (string, bool, error) {
	text, err := root.readSeedFile(filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return text, true, nil
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

// seedInsightMD returns the file-backed INSIGHT markdown for a SEED roleKey
// ("" + false when the role has no insight seed).
//
// 🔴 PER-ROLE BY CONSTRUCTION — `insight_<roleKey>.md`, deliberately NOT the
// ONE-SHARED-FILE shape lessons uses (`readSeedFile("lessons.md")`, same bytes
// for every role). Copying that shape here would ship the ASSISTANT's judgement
// calls to every role out of the box — how to ghost-write someone else's memory,
// when to stop and ask the owner, how to move context — and those are WRONG for
// a tester or an engineer. One role's insight is not another role's insight.
//
// 🔴 THE PRESENCE OF THE FILE IS THE ROSTER — and that is a corrected design,
// not the obvious one. The first version gated on `seedRoleName(roleKey) != ""`
// (copying seedRoleDefinitionMD) and only then interpolated the name. That gate
// returns early for every role but `assistant`, so the interpolation was never
// load-bearing: replacing it with a single shared filename was UNOBSERVABLE —
// mutation-tested, 0 tests went red. The per-role guarantee rested entirely on
// a roster with one entry in it, and would have turned false, silently, the day
// anyone added a second role to seedRoleKeys(). Deriving the answer from the
// filename alone makes "each role reads its own file, or none" the only thing
// the code can express, and makes the shared-seed mutation red.
//
// TRAVERSAL: roleKey arrives from a URL path segment, so it is validated
// EXPLICITLY (safeSeedRoleKey) instead of being laundered through a roster.
// Anything outside [A-Za-z0-9_-]+ resolves to "no seed" — it can never reach
// the filename.
//
// A MISSING file is not an error: fs.ErrNotExist means this role simply has no
// insight seed, and its doc stays genuinely empty. Any other IO error
// propagates (fail-closed) — a read that fails for a real reason must never be
// laundered into "there is no seed".
func (root assetRoot) seedInsightMD(roleKey string) (string, bool, error) {
	return root.seedInsightMDFrom(roleKey, seedsdistFS())
}

// seedInsightMDFrom is seedInsightMD over an injectable embedded FS, so a test
// can present a world with MORE THAN ONE seeded role — the only world in which
// "per-role" is an observable property at all.
func (root assetRoot) seedInsightMDFrom(roleKey string, embedded fs.FS) (string, bool, error) {
	if !safeSeedRoleKey(roleKey) {
		return "", false, nil
	}
	text, err := root.readSeedFileFrom(insightSeedFilename(roleKey), embedded)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return text, true, nil
}

// insightSeedFilename names the PER-ROLE insight seed. The role key is in the
// filename; that is the whole mechanism.
func insightSeedFilename(roleKey string) string {
	return "insight_" + roleKey + ".md"
}

// safeSeedRoleKey reports whether roleKey may be interpolated into a seed
// filename. Deliberately an ALLOWLIST of characters rather than a denylist of
// traversal sequences: "..", "/", NUL and every encoding trick anyone thinks of
// later are all excluded by not being in the set, and a key that fails simply
// has no seed (never an error — an odd role key is not a server fault).
func safeSeedRoleKey(roleKey string) bool {
	if roleKey == "" {
		return false
	}
	for _, r := range roleKey {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
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
		SizeChars:     utf8.RuneCountInString(folded.DefinitionMD),
		CapChars:      s.dutyCap(),
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
func (s *apiServer) foldLessonsDTO(roleKey string) (*lessonsDTO, error) {
	overlay, err := s.dal.GetLessons(roleKey)
	if err != nil {
		return nil, err
	}
	seedText, err := s.root.readSeedFile("lessons.md")
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldLessons(overlay, seedText)
	return &lessonsDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      s.learningCap(),
		RoleKey:       roleKey,
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
	RoleKey string
	Name    string
	Context string
}

// bootSequenceSeedName picks the boot-sequence seed for a runtime. It is the
// SINGLE source of truth for that choice: the member fold (buildBootContext,
// below) and the outsource-worker shared core (workerGlobalContext,
// worker_sharedcore.go) both call it, so the two paths cannot decide it with
// two different expressions.
//
// This exists because they once did. The worker path hard-coded
// "boot_sequence.md" (PR #170 removed the worker-only seed filtering that had
// been masking it), so a worker running the codex runtime was handed the Claude
// boot sequence — which tells it to run a bare `ocagent listen` in the
// background under Monitor — while its own codex runtime tail told it NOT to
// start a listener because the App Server sidecar owns it. Two contradictory
// instructions in one boot context. Parity between staff and outsource is "read
// the seed for the runtime you are actually running", exactly as staff does; it
// is NOT "filter the Claude seed down".
//
// "" normalises to claude (NormalizeRuntime), so an unset runtime keeps the
// historical default.
func bootSequenceSeedName(runtime string) string {
	if NormalizeRuntime(runtime) == RuntimeCodex {
		return bootSequenceSeedCodex
	}
	return bootSequenceSeedClaude
}

// The two shipped boot-sequence seed filenames + the one system-interaction
// seed. Named so the doc-key derivation below can compare against the ANSWER of
// bootSequenceSeedName instead of asking the runtime a second question.
const (
	bootSequenceSeedClaude   = "boot_sequence.md"
	bootSequenceSeedCodex    = "boot_sequence_codex.md"
	systemInteractionSeedMD  = "system_interaction.md"
	bootSequenceKeyClaude    = RuntimeClaude
	bootSequenceKeyCodex     = RuntimeCodex
	systemInteractionDocKey  = "global"
	docKindSystemInteraction = "system_interaction"
	docKindBootSequence      = "boot_sequence"
)

// The 〈停止〉 document (T-c9c0). A SINGLETON like the system-interaction block —
// one document keyed "global" for every agent and every runtime — because unlike
// the boot sequence, being collected is the same procedure whatever runtime you
// are: report, write the in-flight work back, hand yourself over, stop. There is
// deliberately no runtime axis to get wrong here.
const (
	offboardSeedMD  = "offboard.md"
	offboardDocKey  = "global"
	docKindOffboard = "offboard"
)

// bootSequenceDocKey names the EDITABLE DOCUMENT that carries a runtime's boot
// sequence (T-791e).
//
// 🔴 It reads the answer of bootSequenceSeedName rather than testing the runtime
// itself, on purpose: that function is documented as the single place in the
// tree that decides which runtime gets which sequence, and it holds that title
// only as long as nobody writes a second `== RuntimeCodex` beside it. The two
// boot sequences contradict each other in step 3 (claude: mount your own
// `ocagent listen`; codex: do NOT, the sidecar owns it), so a second decision
// point that drifts hands a worker the sequence that keeps it from booting —
// silently, because a worker that never comes online is never there to say so.
func bootSequenceDocKey(runtime string) string {
	if bootSequenceSeedName(runtime) == bootSequenceSeedCodex {
		return bootSequenceKeyCodex
	}
	return bootSequenceKeyClaude
}

// bootSequenceSeedForKey resolves a boot_sequence DOCUMENT KEY (as it arrives on
// the URL) back to its seed filename, reporting whether the key names a real
// document at all.
//
// The validity test is `bootSequenceDocKey(key) == key`: the keys ARE the
// runtime names, so a key that survives a round trip through the single decision
// point is one this server serves, and everything else ("", "Codex", "opus") is
// not. Written this way rather than as a literal set so the set cannot fall out
// of step with the decision point it is supposed to mirror.
func bootSequenceSeedForKey(key string) (string, bool) {
	if bootSequenceDocKey(key) != key {
		return "", false
	}
	return bootSequenceSeedName(key), true
}

// buildBootContext resolves the role + folds the role docs + assembles the
// boot context (lifecycle.md §2.2 normative order: system-interaction seed,
// user-custom block when non-blank, # Role, # Insight when non-blank, # Lessons,
// boot-sequence seed — joined "\n\n" + one trailing "\n"). nil = unknown role
// (caller maps to 404 / fail-closed).
func (s *apiServer) buildBootContext(role string, member *Member) (*bootContext, error) {
	roleKey := resolveBootRoleKey(role, member)
	roleDTO, err := s.foldRoleDefDTO(roleKey)
	if err != nil {
		return nil, err
	}
	if roleDTO == nil {
		return nil, nil
	}
	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		return nil, err
	}
	lessons, err := s.foldLessonsDTO(roleKey)
	if err != nil {
		return nil, err
	}
	insight, err := s.foldInsightDTO(roleKey)
	if err != nil {
		return nil, err
	}
	// 🔴 THE EDITED VERSION WINS, THE SEED IS THE FALLBACK (T-791e). These two
	// blocks used to be bare readSeedFile calls, i.e. the shipped bytes and
	// nothing else. They now go through the same overlay ⊕ seed fold the
	// cockpit's editor reads and writes, which is what makes an edit take effect
	// on the next boot instead of on the next release. An installation that has
	// never edited them folds to the identical seed bytes.
	sysSeed, err := s.systemInteractionText()
	if err != nil {
		return nil, err
	}
	var memberRuntime string
	if member != nil {
		memberRuntime = member.Runtime
	}
	bootSeed, err := s.bootSequenceText(memberRuntime)
	if err != nil {
		return nil, err
	}
	roleTitle := roleDTO.Name
	if roleTitle == "" {
		roleTitle = roleDTO.Key
	}
	// Lessons section title — injected IDEMPOTENTLY (T-8327). The injection
	// wraps the authoritative doc in a title the doc itself does not carry;
	// when a generation treats its boot segment as the doc base and writes it
	// back (replace_lessons), the title becomes doc content and a naive
	// re-prepend would then stack one more title per generation (the observed
	// +38-char drift: server doc 50,625 vs boot segment 50,663). Strip any
	// leading copies of the EXACT title line before prepending exactly one, so
	// the boot context always carries a single title AND an already-poisoned
	// doc self-heals in the assembled context.
	//
	// 🔴 TWO TITLES ARE STRIPPED, NOT ONE. Until T-2 this title carried the
	// lessons bucket — "# Lessons (assistant / general)" — so a doc poisoned
	// BEFORE that change carries the old wording, and stripping only the new
	// one would leave it wedged at the top of the doc forever with no way for
	// the self-heal to reach it. Both forms are removed; the legacy form's
	// bucket half is 'general' because that is the only bucket 00061 left
	// behind and the only one this title could ever have named after it.
	lessonsTitle := "# Lessons (" + lessons.RoleKey + ")"
	legacyLessonsTitle := "# Lessons (" + lessons.RoleKey + " / general)"
	lessonsBody := strings.TrimSpace(lessons.Text)
	for {
		stripped := false
		for _, title := range []string{lessonsTitle, legacyLessonsTitle} {
			for strings.HasPrefix(lessonsBody, title) {
				rest := lessonsBody[len(title):]
				if rest != "" && !strings.HasPrefix(rest, "\n") {
					break // title is a prefix of a longer line, not a duplicate title line
				}
				lessonsBody = strings.TrimSpace(rest)
				stripped = true
			}
		}
		if !stripped {
			break
		}
	}
	// T-4595 — the user-custom block moved from below the persona to above it
	// (it used to sit between the lessons and the boot sequence). Staff and
	// outsource boot contexts are now the SAME FOUR SLOTS in the same order:
	//
	//	1. 系統互動 (shared seed)
	//	2. 使用者自訂 (shared, skipped entirely when blank)
	//	3. the persona — staff: 角色說明 → 判準（when non-blank）→ 長期筆記;
	//	   outsource: NOTHING (no role)
	//	4. 啟動步驟 (shared seed, recency-authoritative tail)
	//
	// Only slot 3 differs between the two, and that is the whole difference.
	// Putting the owner's additions ABOVE the persona is what makes the two
	// assemblies line up; leaving it wedged between the lessons and the boot
	// sequence would keep one seam that only staff have.
	parts := []string{strings.TrimSpace(sysSeed)}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			userAdditionsTitle+"\n\n"+strings.TrimSpace(userCtx.Text))
	}
	parts = append(parts,
		"# Role: "+roleTitle+"\n\n"+strings.TrimSpace(roleDTO.DefinitionMD))
	// Insight (T-3809) — the persona's third block, between Duty (# Role) and
	// Learning (# Lessons), which is the order the three documents are defined
	// in: what she does → how she works → what she learned doing it.
	//
	// 🔴 The condition is the FOLDED TEXT being non-blank, exactly like the
	// 使用者自訂 block above — deliberately NOT insight.IsDefault and NOT
	// insight.HasSeed. Those two answer different questions (whether the text
	// came from the factory seed rather than an overlay, and whether a seed FILE
	// exists for this role at all), so either one used as the gate would emit
	// the section for roles whose insight is genuinely empty — an orphan title
	// with nothing under it — or suppress it for a role that has written one.
	if insightBody := strings.TrimSpace(insight.Text); insightBody != "" {
		parts = append(parts, "# Insight ("+roleKey+")\n\n"+insightBody)
	}
	parts = append(parts, lessonsTitle+"\n\n"+lessonsBody)
	// 傳承（lore）對象目錄 (T-33) — the TAIL of slot 3, after 長期筆記 and
	// before 啟動步驟. buildWorkerBootContext calls the same function at the same
	// relative position; that symmetry is what keeps the two documents one
	// assembly rather than two that drift.
	//
	// 🔴 Unlike the rest of slot 3 this block is NOT role-specific — it is the
	// station's subject directory, so both audiences read it. What is filtered
	// per reader is only the `private` wall inside the query.
	//
	// Blank ⇒ the whole section is absent, the 使用者自訂 rule.
	var memberID string
	if member != nil {
		memberID = member.ID
	}
	memorySection, err := s.foldLoreSection(memberID)
	if err != nil {
		return nil, err
	}
	if memorySection != "" {
		parts = append(parts, memorySection)
	}
	parts = append(parts, strings.TrimSpace(bootSeed))
	name := roleDTO.Name
	if member != nil {
		name = member.Name
	}
	return &bootContext{
		RoleKey: roleKey,
		Name:    name,
		Context: strings.Join(parts, "\n\n") + "\n",
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

// The six event-procedure documents T-3201 adds. Same shape as the offboard
// singleton above — one document per event, key "global" — because what an
// agent should do when a task closes does not depend on which runtime it is.
//
// 🔴 THE SEEDS ARE THE PROGRAM TEXT THESE NOTICES USED TO BE, MOVED WITHOUT A
// WORD CHANGED. Every sentence in these six files was a Go string literal
// (sse_bands.go's offboard sentence builder and decideTaskCloseNudge,
// api_tasks.go's reassign notices, api_tasks_handoff.go's dependency-released
// notice); the interpolation points are the ONLY thing that changed shape,
// becoming the {name} variables this kind declares. Six are wired to their send
// sites and the Go text they replaced is deleted: the two stop procedures, plus
// 轉派程序（前任）, 解除阻擋 and — once the owner ruled the duplicated 交接備註
// away (rc-0c36d8739b8f) — the two 接手程序. 任務收尾 was the last one left, and
// T-7870 wired it: the pure decideTaskCloseNudge now decides only WHETHER a
// nudge is owed, and closeTask — which is a method, and was already reading the
// manual two lines above the call — fetches the words. All seven carry their
// text from the document now, and no Go literal of any of them survives.
// bootDocSingletonKey is the document key every non-boot_sequence boot document
// uses. Named rather than repeated so a caller addressing "the one document of
// this kind" says so, instead of spelling a magic "global" that reads like the
// 全域脈絡 document it is not.
// userAdditionsTitle is the ONE title the 使用者自訂 block is served under.
//
// 🔴 IT WAS TWO COPIES, AND THAT IS THE WHOLE REASON IT IS A CONSTANT (T-3201).
// The same literal sat in buildBootContext (staff) and workerSharedHead
// (outsource), so the day someone corrected one of them, staff and contractors
// would have booted under two different headings and nothing would have said
// so. It is also the only line the program ADDS to any of the three boot
// documents — the read-only head of 使用者自訂, in the vocabulary T-3201 gives
// the other nine — which is why it is named here rather than inlined twice.
const userAdditionsTitle = "# 使用者自訂（Owner Additions）"

const bootDocSingletonKey = "global"

const (
	acceleratedStopSeedMD  = "accelerated_stop.md"
	acceleratedStopDocKey  = "global"
	docKindAcceleratedStop = "accelerated_stop"

	taskCloseoutSeedMD  = "task_closeout.md"
	taskCloseoutDocKey  = "global"
	docKindTaskCloseout = "task_closeout"

	taskReassignPredecessorSeedMD  = "task_reassign_predecessor.md"
	taskReassignPredecessorDocKey  = "global"
	docKindTaskReassignPredecessor = "task_reassign_predecessor"

	taskTakeoverWithPredecessorSeedMD  = "task_takeover_with_predecessor.md"
	taskTakeoverWithPredecessorDocKey  = "global"
	docKindTaskTakeoverWithPredecessor = "task_takeover_with_predecessor"

	taskTakeoverFreshSeedMD  = "task_takeover_fresh.md"
	taskTakeoverFreshDocKey  = "global"
	docKindTaskTakeoverFresh = "task_takeover_fresh"

	taskUnblockedSeedMD  = "task_unblocked.md"
	taskUnblockedDocKey  = "global"
	docKindTaskUnblocked = "task_unblocked"
)
