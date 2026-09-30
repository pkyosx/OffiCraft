package main

// 🔴 EMBED-ONLY, deliberately NO disk override: a stale seeds/,
// spec/mcp-catalog.json or bin/ocwarden under the CWD must never shadow the
// version-locked embed (disk-first once caused three silent content-level
// version regressions). go:embed cannot reach outside the module, so
// bin/build-seedsdist, bin/build-bindist and bin/build-docsdist stage the
// repo-root files into seedsdist/, bindist/ and docsdist/ before the build; the
// committed bin/ocserverd is built with seedsdist AND bindist staged.

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// `all:` tolerates the .gitkeep-only placeholder state on a clean checkout.
//
//go:embed all:seedsdist
var seedsdistEmbed embed.FS

func seedsdistFS() fs.FS {
	sub, err := fs.Sub(seedsdistEmbed, "seedsdist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Server-platform binaries only: the exec paths (bootstrap/teardown-here)
// install on the server host itself.
//
//go:embed all:bindist
var bindistEmbed embed.FS

func bindistFS() fs.FS {
	sub, err := fs.Sub(bindistEmbed, "bindist")
	if err != nil {
		panic(err)
	}
	return sub
}

//go:embed all:docsdist
var docsdistEmbed embed.FS

func docsdistFS() fs.FS {
	sub, err := fs.Sub(docsdistEmbed, "docsdist")
	if err != nil {
		panic(err)
	}
	return sub
}

type assetRoot string

const (
	seedRoleAssistant     = "assistant"
	seedRoleAssistantName = "Assistant"

	ownerPlaceholder = "{OWNER_ID}"
)

func seedRoleName(roleKey string) string {
	if roleKey == seedRoleAssistant {
		return seedRoleAssistantName
	}
	return ""
}

func seedRoleKeys() []string {
	return []string{seedRoleAssistant}
}

func (root assetRoot) readSeedFile(filename string) (string, error) {
	return root.readSeedFileFrom(filename, seedsdistFS())
}

func (assetRoot) readSeedFileFrom(filename string, embedded fs.FS) (string, error) {
	raw, err := fs.ReadFile(embedded, filename)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(raw), ownerPlaceholder, wireOwnerID), nil
}

// Only fs.ErrNotExist means "no seed"; any other IO error must propagate
// (fail-closed), never be laundered into "no seed".
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

// 🔴 PER-ROLE by filename, and the FILE's presence is the roster — deliberately
// not gated on seedRoleName like seedRoleDefinitionMD: with a one-entry roster
// that gate made a shared-file mutation unobservable (0 tests red).
//
// roleKey arrives from a URL path segment, hence safeSeedRoleKey.
func (root assetRoot) seedInsightMD(roleKey string) (string, bool, error) {
	return root.seedInsightMDFrom(roleKey, seedsdistFS())
}

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

func insightSeedFilename(roleKey string) string {
	return "insight_" + roleKey + ".md"
}

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

// The SOP text lives in seeds/task_manual_<type_key>.md; the rest of the
// shipped manual is here. The keys sit outside the minted "tm-" space so no
// station-created manual can already hold one.
type builtinTaskManual struct {
	TypeKey     string
	DisplayName string
	Purpose     string
	Fields      []ManualField
	Assignee    map[string]any
}

var builtinTaskManuals = []builtinTaskManual{
	{
		TypeKey:     "builtin-role-design",
		DisplayName: "建立／修改角色",
		Purpose:     "建立新的角色，或調整既有角色的角色定義與判準（Insight）。",
		Fields:      []ManualField{{Name: "role_name", Required: true, IsKey: true}},
		Assignee:    map[string]any{"kind": TaskExecutorStaff, "member_id": seedMiraID},
	},
	{
		TypeKey:     "builtin-task-manual-design",
		DisplayName: "建立／修改任務手冊",
		Purpose:     "建立新的任務手冊，或調整既有任務手冊的內容與負責成員。",
		Fields:      []ManualField{{Name: "manual_name", Required: true, IsKey: true}},
		Assignee:    map[string]any{"kind": TaskExecutorStaff, "member_id": seedMiraID},
	},
}

func builtinTaskManualFor(typeKey string) (builtinTaskManual, bool) {
	for _, b := range builtinTaskManuals {
		if b.TypeKey == typeKey {
			return b, true
		}
	}
	return builtinTaskManual{}, false
}

func isBuiltinTaskManual(typeKey string) bool {
	_, ok := builtinTaskManualFor(typeKey)
	return ok
}

// A registered built-in whose SOP file is missing is an error, not "no seed":
// serving it blank would let the first edit persist an empty SOP as the manual.
func (root assetRoot) seedTaskManual(typeKey string) (*TaskManual, error) {
	b, ok := builtinTaskManualFor(typeKey)
	if !ok {
		return nil, nil
	}
	sop, err := root.readSeedFile("task_manual_" + typeKey + ".md")
	if err != nil {
		return nil, err
	}
	fields, err := json.Marshal(b.Fields)
	if err != nil {
		return nil, err
	}
	assignee, err := json.Marshal(b.Assignee)
	if err != nil {
		return nil, err
	}
	return &TaskManual{
		TypeKey:     b.TypeKey,
		DisplayName: b.DisplayName,
		Purpose:     b.Purpose,
		Fields:      string(fields),
		SopMD:       sop,
		Assignee:    string(assignee),
	}, nil
}

const defaultBootRole = seedRoleAssistant

func resolveBootRoleKey(role string, member *Member) string {
	if role != "" {
		return role
	}
	if member != nil && member.RoleKey != "" {
		return member.RoleKey
	}
	return defaultBootRole
}

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

type bootContext struct {
	RoleKey string
	Name    string
	Context string
}

// 🔴 The SINGLE decision of which runtime gets which boot sequence: the member
// fold and the outsource-worker shared core (worker_sharedcore.go) both call
// it. "" normalises to claude.
func bootSequenceSeedName(runtime string) string {
	if NormalizeRuntime(runtime) == RuntimeCodex {
		return bootSequenceSeedCodex
	}
	return bootSequenceSeedClaude
}

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

const (
	offboardSeedMD  = "offboard.md"
	offboardDocKey  = "global"
	docKindOffboard = "offboard"
)

// 🔴 Reads bootSequenceSeedName's answer rather than testing the runtime: a
// second `== RuntimeCodex` that drifts would hand a worker the other runtime's
// boot sequence (only codex's hands control back), and a worker that never
// comes online is never there to say so.
func bootSequenceDocKey(runtime string) string {
	if bootSequenceSeedName(runtime) == bootSequenceSeedCodex {
		return bootSequenceKeyCodex
	}
	return bootSequenceKeyClaude
}

func bootSequenceSeedForKey(key string) (string, bool) {
	if bootSequenceDocKey(key) != key {
		return "", false
	}
	return bootSequenceSeedName(key), true
}

// Section order is normative (spec/lifecycle.md §2.2).
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
	insight, err := s.foldInsightDTO(roleKey)
	if err != nil {
		return nil, err
	}
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
	// Staff and outsource boot contexts are the SAME slots in the same order:
	// 系統互動, 使用者自訂 (skipped when blank), persona (staff only), 啟動步驟
	// (recency-authoritative tail). Only the persona slot differs.
	parts := []string{strings.TrimSpace(sysSeed)}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			userAdditionsTitle+"\n\n"+strings.TrimSpace(userCtx.Text))
	}
	parts = append(parts,
		"# Role: "+roleTitle+"\n\n"+strings.TrimSpace(roleDTO.DefinitionMD))
	// 🔴 Gated on the FOLDED TEXT being non-blank — NOT insight.IsDefault or
	// insight.HasSeed, which answer different questions.
	if insightBody := strings.TrimSpace(insight.Text); insightBody != "" {
		parts = append(parts, "# Insight ("+roleKey+")\n\n"+insightBody)
	}
	// 傳承 is keyed by the MEMBER (owner, card rc-a43100fd0486).
	//
	// 🔴 THE CAP IS s.loreRoleCap() AND THAT IS NOT A MISTAKE: it is the
	// member-fold budget under a stale name (see domain.go); s.loreManualCap()
	// is the OTHER exit.
	//
	// ⚠️ member is nil on the cockpit's role preview path: the block is OMITTED
	// there, since role_key would resurrect the removed scope and picking a
	// member would show one person's 傳承 as the role's.
	if member != nil {
		loreSel, err := selectMemberLore(s.dal, member.ID, s.loreRoleCap())
		if err != nil {
			return nil, err
		}
		if block := renderLoreBlock(loreSel); block != "" {
			parts = append(parts, block)
		}
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

// Normative: M1 §3.2.
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

// Mirrors the warden's selfUpdateHashPrefixLen: the shared "which build"
// fingerprint on both sides of the wire. An eyeball tag, not a checksum.
const binHashPrefixLen = 12

func binHashPrefix(data []byte) string {
	sum := sha256.Sum256(data)
	full := hex.EncodeToString(sum[:])
	if len(full) > binHashPrefixLen {
		return full[:binHashPrefixLen]
	}
	return full
}

// Fingerprint equality IS "this machine already holds the latest build" (the
// warden's reconcileBinary uses the same raw-content oracle). A missing embed
// entry is omitted, so the comparison answers unknown, never a false verdict.
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

func (assetRoot) readMCPCatalogFrom(embedded fs.FS) ([]byte, error) {
	return fs.ReadFile(embedded, "mcp-catalog.json")
}

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

// 🔴 The ONE title the 使用者自訂 block is served under; workerSharedHead
// (outsource) uses it too, so staff and contractors boot under one heading.
const userAdditionsTitle = "# 使用者自訂（Owner Additions）"

// Named so "the one document of this kind" is not a magic "global" that reads
// like the 全域脈絡 document it is not.
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

	taskUnblockedSeedMD  = "task_unblocked.md"
	taskUnblockedDocKey  = "global"
	docKindTaskUnblocked = "task_unblocked"

	taskReadyForDoneSeedMD  = "task_ready_for_done.md"
	taskReadyForDoneDocKey  = "global"
	docKindTaskReadyForDone = "task_ready_for_done"
)
