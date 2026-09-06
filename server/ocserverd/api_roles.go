package main

// api_roles.go — the role journal: user-custom context block, role
// definitions, and per-role lessons (handlers.handle_get_global_context …
// handle_replace_lessons). role_def / lessons are OWNER OVERLAYS over the
// file seeds; reset is an idempotent tombstone; a custom role hard-deletes
// with a complete cascade.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"unicode/utf8"
)

func historyJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// ── user-custom context block ────────────────────────────────────────────────

// GET /api/global-context — the folded user-custom ADDITIVE block.
func (s *apiServer) HandleGetGlobalContextApiGlobalContextGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/global-context — whole-block replace ({text}).
func (s *apiServer) HandleReplaceGlobalContextApiGlobalContextPost(w http.ResponseWriter, r *http.Request) {
	var body GlobalContextReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	text := body.Text
	// T-2d99 wipe guard: emptying a block that had content needs to be said
	// out loud. /api/global-context/reset is the dedicated way back to empty.
	if !(body.AllowShrink != nil && *body.AllowShrink) {
		current, err := s.foldUserContextDTO()
		if err != nil {
			internalError(w, err)
			return
		}
		if WholeDocWipeBlocked(current.Text, text) {
			writeError(w, http.StatusBadRequest,
				docWipeRefusal("global context", ", or use reset_global_context"))
			return
		}
	}
	if err := s.dal.SaveWithDocumentHistory("global_context", "global", currentActor(r), userContextSnapshotIn, func(ex sqlExecer) error {
		return putUserContextOn(ex, UserContext{Text: text, Tombstoned: false})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	// T-91: the receipt is READ BACK from the fold rather than assembled from
	// `text`. is_default is the one field a caller cannot predict from the verb
	// it called, and only the fold knows it — assembling it here would have to
	// guess, which is exactly the mistake the old `IsDefault: false` made.
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, globalContextReceiptOf(dto))
}

// globalContextReceiptOf reduces the read face's DTO to the write face's
// receipt. Both write verbs go through it so they cannot answer with two
// different shapes for one document.
func globalContextReceiptOf(dto *globalContextDTO) globalContextReceiptDTO {
	return globalContextReceiptDTO{
		IsDefault: dto.IsDefault,
		SizeChars: utf8.RuneCountInString(dto.Text),
		Sha256:    receiptSha256(dto.Text),
	}
}

// POST /api/global-context/reset — idempotent tombstone back to empty.
func (s *apiServer) HandleResetGlobalContextApiGlobalContextResetPost(w http.ResponseWriter, r *http.Request) {
	if err := s.dal.SaveWithDocumentHistory("global_context", "global", currentActor(r), userContextSnapshotIn, func(ex sqlExecer) error {
		return putUserContextOn(ex, UserContext{Text: "", Tombstoned: true})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldUserContextDTO()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, globalContextReceiptOf(dto))
}

// ── role definitions ─────────────────────────────────────────────────────────

// listRoleKeys is the role roster in wire order: seed roles FIRST, then every
// custom role (non-tombstoned overlay with no file seed). Shared by GET
// /api/roles and the peek_doc_sizes overview so the two can never disagree
// about which roles exist.
func (s *apiServer) listRoleKeys() ([]string, error) {
	keys := []string{}
	seeds := map[string]bool{}
	for _, roleKey := range seedRoleKeys() {
		seeds[roleKey] = true
		keys = append(keys, roleKey)
	}
	overlays, err := s.dal.ListRoleDefs()
	if err != nil {
		return nil, err
	}
	for _, overlay := range overlays {
		if seeds[overlay.RoleKey] || overlay.Tombstoned {
			continue
		}
		keys = append(keys, overlay.RoleKey)
	}
	return keys, nil
}

// GET /api/roles — seed roles (folded with any owner edit) FIRST, then every
// custom role (non-tombstoned overlay with no file seed).
//
// The rows carry NO definition_md: a listing is where a caller picks a role,
// and the persona body is the bulk of the document. Each row still reports
// size_chars / cap_chars measured on the folded document, so "which definition
// is nearly full" is answerable without the text; get_role reads the one you
// picked. The fold itself is unchanged and shared with get_role, so the two
// faces cannot disagree about is_default / is_seed / the size.
func (s *apiServer) HandleListRolesApiRolesGet(w http.ResponseWriter, r *http.Request) {
	dtos := []roleDefListItemDTO{}
	keys, err := s.listRoleKeys()
	if err != nil {
		internalError(w, err)
		return
	}
	for _, roleKey := range keys {
		dto, err := s.foldRoleDefDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if dto != nil {
			dtos = append(dtos, newRoleDefListItemDTO(*dto))
		}
	}
	writeJSON(w, http.StatusOK, dtos)
}

// GET /api/roles/{role} — one folded role definition (unknown → 404).
func (s *apiServer) HandleGetRoleApiRolesRoleGet(w http.ResponseWriter, r *http.Request, role string) {
	dto, err := s.foldRoleDefDTO(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if dto == nil {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/roles — create ONE custom role + its ONE founding member. The
// server mints both ids; the definition starts from the fixed template; the
// member starts offline; member_name omitted ⇒ picked from the name pool.
func (s *apiServer) HandleCreateRoleApiRolesPost(w http.ResponseWriter, r *http.Request) {
	var body RoleCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "name") {
		return
	}
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "role requires a name")
		return
	}
	if body.Effort != nil && !validEffort(*body.Effort) {
		writeError(w, http.StatusUnprocessableEntity,
			"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
		return
	}
	// UNSET when the caller names none — the cockpit's 招攬新成員 sends only a
	// name, so this is THE path a founding member is born on. Leaving it empty
	// hands the choice to resolveEmptyRuntimeForPlacement at placement time
	// (T-ae8b), instead of pinning every new member to claude at birth.
	runtime := ""
	if body.Runtime != nil {
		runtime = string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
	}
	memberName := trimmedOrEmpty(body.MemberName)
	if memberName == "" {
		members, err := s.dal.ListMembers()
		if err != nil {
			internalError(w, err)
			return
		}
		taken := make([]string, 0, len(members))
		for _, m := range members { // removed rows included — audit names never double
			taken = append(taken, m.Name)
		}
		memberName = PickMemberName(taken, nil)
	}
	roleKey := "r-" + newHexID(12)
	if err := s.dal.PutRoleDef(RoleDef{
		RoleKey:      roleKey,
		Name:         name,
		DefinitionMD: CustomRoleTemplateMD,
		Tombstoned:   false,
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	effort := strOrEmpty(body.Effort)
	if effort == "" {
		effort = "medium"
	}
	member := Member{
		ID:               "m-" + newHexID(12),
		Name:             memberName,
		Kind:             KindStaff,
		RoleKey:          roleKey,
		Runtime:          runtime,
		Model:            trimmedOrEmpty(body.Model),
		Effort:           effort,
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
	if err := s.putMember(member, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// T-91: the two MINTED IDS and the (possibly server-CHOSEN) member name are
	// the whole of what this write produced that the caller could not compute.
	// The role's definition_md is the shipped CustomRoleTemplateMD every custom
	// role starts on — a constant, readable through get_role — and the member
	// row is readable through get_member; neither is news.
	writeJSON(w, http.StatusOK, roleCreateResultDTO{
		RoleKey:    roleKey,
		MemberID:   member.ID,
		MemberName: member.Name,
	})
}

// POST /api/roles/{role} — edit ({name?, definition_md?}). Unknown → 404. A
// SEED role is name-locked (a supplied name is IGNORED, not rejected).
func (s *apiServer) HandleUpdateRoleApiRolesRolePost(w http.ResponseWriter, r *http.Request, role string) {
	var body RoleDefUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	current, err := s.foldRoleDefDTO(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	name := current.Name
	if body.Name != nil && seedRoleName(role) == "" {
		name = *body.Name
	}
	definitionMD := current.DefinitionMD
	if body.DefinitionMd != nil {
		definitionMD = *body.DefinitionMd
	}
	// T-ae38 hard cap on DUTY. Duty is the one role-journal segment that had no
	// cap at all until this ticket, and it is checked HERE and at the
	// document-history restore door (api_document_history.go, case
	// "role_definition") — BOTH, because either alone is decorative: edit down
	// to 999 and then restore a 4,000-char earlier revision and the cap is gone.
	// One read, reused by the response below, so the size the caller is told is
	// provably the number its write was judged against.
	//
	// Same three-line rule as every other capped doc (DocCapBlocked): an
	// already-over-cap Duty is never truncated, but its next write must come
	// out SHORTER. The shipped assistant seed now sits well UNDER the default
	// (see dutyCapCharsDefault), so on day one that rule binds nothing that
	// ships — it exists for hand-written Duties that grow past the cap.
	cap := s.dutyCap()
	if DocCapBlocked(cap, current.DefinitionMD, definitionMD) {
		writeError(w, http.StatusBadRequest,
			docCapRefusal(cap, "role definition doc", current.DefinitionMD, definitionMD))
		return
	}
	// The NAME is not versioned (owner ruling, T-1f39), so a write that only
	// renames the role retains nothing — otherwise a rename would push a real
	// revision of the TEXT out of the three retained slots without changing a
	// word of it. Same rule the task manual's two series follow.
	streams := roleDefHistoryStreams(role, currentActor(r), definitionMD != current.DefinitionMD)
	if err := s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		return putRoleDefOn(ex, RoleDef{
			RoleKey:      role,
			Name:         name,
			DefinitionMD: definitionMD,
			Tombstoned:   false,
		})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	// T-91: `definition_md` no longer rides home — the caller sent it. It is
	// still assembled from the LOCALS rather than from a re-read, which is what
	// the cap comment above promises: the size the caller is told is provably
	// the number its write was judged against. `name` is the field that earns
	// its place here, because a rename of a seed role is silently ignored a few
	// lines up and this is the only place that says so.
	writeJSON(w, http.StatusOK, roleDefReceiptDTO{
		Key:       role,
		Name:      name,
		IsDefault: false, // an overlay now exists — FoldRoleDef reads that as not-default
		IsSeed:    seedRoleName(role) != "",
		SizeChars: utf8.RuneCountInString(definitionMD),
		CapChars:  cap,
		Sha256:    receiptSha256(definitionMD),
	})
}

// roleDefReceiptOf reduces the read face's DTO to the write face's receipt, for
// the verbs that ANSWER FROM A RE-READ (reset). update_role assembles its own
// from the values it just judged — see the comment there.
func roleDefReceiptOf(dto *roleDefDTO) roleDefReceiptDTO {
	return roleDefReceiptDTO{
		Key:       dto.Key,
		Name:      dto.Name,
		IsDefault: dto.IsDefault,
		IsSeed:    dto.IsSeed,
		SizeChars: dto.SizeChars,
		CapChars:  dto.CapChars,
		Sha256:    receiptSha256(dto.DefinitionMD),
	}
}

// POST /api/roles/{role}/reset — tombstone the overlay back to the seed
// (unknown SEED role → 404: there must be a seed to reset to).
func (s *apiServer) HandleResetRoleApiRolesRoleResetPost(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) == "" {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	current, err := s.foldRoleDefDTO(role)
	if err != nil || current == nil {
		internalError(w, err)
		return
	}
	if err := s.dal.SaveWithDocumentHistory("role_definition", role, currentActor(r), roleDefSnapshotIn(role), func(ex sqlExecer) error {
		return putRoleDefOn(ex, RoleDef{RoleKey: role, Tombstoned: true})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	dto, err := s.foldRoleDefDTO(role)
	if err != nil || dto == nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roleDefReceiptOf(dto))
}

// DELETE /api/roles/{role} — HARD-delete a CUSTOM role + everything it owns.
// Seed role → 403; unknown → 404; any online member → 409; then the complete
// cascade (members hard-deleted with their conversations, receipts, lessons,
// in-memory observation entries, and finally the overlay itself).
func (s *apiServer) HandleDeleteRoleApiRolesRoleDelete(w http.ResponseWriter, r *http.Request, role string) {
	if seedRoleName(role) != "" {
		writeError(w, http.StatusForbidden,
			"role '"+role+"' is a built-in seed role and cannot be deleted")
		return
	}
	overlay, err := s.dal.GetRoleDef(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if overlay == nil || overlay.Tombstoned {
		writeError(w, http.StatusNotFound, "role '"+role+"' not found")
		return
	}
	all, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	var members []Member
	var live []string
	for _, m := range all {
		if m.RoleKey != role {
			continue
		}
		members = append(members, m)
		if m.RosterStatus == RosterStatusActive && s.hub.IsOnline(m.ID) {
			live = append(live, m.ID)
		}
	}
	if len(live) > 0 {
		sort.Strings(live)
		msg := "role '" + role + "' has online member(s): "
		for i, id := range live {
			if i > 0 {
				msg += ", "
			}
			msg += id
		}
		writeError(w, http.StatusConflict, msg+" — stop them before deleting")
		return
	}
	deletedMsgs, deletedAtts, deletedReads := 0, 0, 0
	removedIDs := []string{}
	for _, m := range members {
		msgs, atts, err := s.dal.DeleteChatInvolving(m.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		deletedMsgs += msgs
		deletedAtts += atts
		if msgs > 0 {
			// Cascade delta parity (repository.delete_chat_involving): fans
			// iff anything was deleted; refetch-only, no payload. Owner-only:
			// the payload carries no from/to to address peers (agents don't
			// act on a chat deletion — they re-list on their next fetch), and
			// the removed member m is being hard-deleted; the owner cockpit is
			// the one view that must refresh.
			s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+m.ID, nil, audienceOwnerOnly(), requestTrigger(r))
		}
		reads, err := s.dal.DeleteChatReadsInvolving(m.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		deletedReads += reads
		if reads > 0 {
			s.hub.Publish("chat_read", "patch", "chat_read", wireOwnerID+"::"+m.ID, nil, audienceOwnerOnly(), requestTrigger(r))
		}
		s.telemetry.Delete(m.ID)
		s.gauge.Delete(m.ID)
		if _, err := s.dal.HardDeleteMember(m.ID); err != nil {
			internalError(w, err)
			return
		}
		s.hub.Publish("member", "remove", "member", wireOwnerID+"::"+m.ID, nil,
			audienceMembers(m.ID), requestTrigger(r))
		removedIDs = append(removedIDs, m.ID)
	}
	deletedLessons, err := s.dal.DeleteLessonsForRole(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if deletedLessons > 0 {
		// Cascade rides as patch keyed by the bare role
		// (repository.delete_lessons_for_role → _publish_overlay).
		s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	}
	// T-3809: the role's insight goes with the role, same transaction posture as
	// its lessons above. Deliberately NOT reported in the response DTO — that
	// would be a new wire field, and the count answers nothing a caller acts on;
	// the delta below is what any open surface actually needs.
	deletedInsight, err := s.dal.DeleteInsightForRole(role)
	if err != nil {
		internalError(w, err)
		return
	}
	if deletedInsight > 0 {
		s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	}
	if _, err := s.dal.DeleteRoleDef(role); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("role_def", "remove", "role_def", wireOwnerID+"::"+role, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, roleDeleteResultDTO{
		Role:                   role,
		RemovedMemberIDs:       removedIDs,
		DeletedChatMessages:    deletedMsgs,
		DeletedChatAttachments: deletedAtts,
		DeletedChatReads:       deletedReads,
		DeletedLessons:         deletedLessons,
	})
}

// ── lessons ──────────────────────────────────────────────────────────────────

// errLessonsTaskTypeRetired is the answer to an MCP lessons call that still
// carries `task_type`.
//
// 🔴 WHY THIS REFUSES INSTEAD OF IGNORING, stated where the decision lives.
// T-2 removed the lessons classification axis. The three shapes a removal can
// take are: keep accepting the field and ignore it, keep accepting it and
// honour it, or refuse. Ignoring is the WORST of the three here and not by a
// small margin — this endpoint's whole defect was that a task_type nobody
// validated sent a write to a bucket the caller did not mean, answered 200,
// and said nothing. A silently-dropped field reproduces that exact experience
// (the caller believes it addressed a classification; the write went
// elsewhere) while removing the last trace of evidence. So the field is
// refused, by name, with the replacement stated.
var errLessonsTaskTypeRetired = errors.New(
	"task_type was removed from the lessons tools (T-2): a lessons doc is " +
		"addressed by role_key ALONE. Drop the field and retry — it is " +
		"refused rather than ignored so that a call which believes it named " +
		"a classification cannot quietly land somewhere else")

// lessonsRetiredQueryParam is the ONE query key the lessons routes used to
// carry and no longer do. Named once so the three routes cannot drift apart
// about what is retired.
const lessonsRetiredQueryParam = "task_type"

// refuseRetiredLessonsQuery answers the retired task_type when it arrives as a
// QUERY parameter on any of the three lessons HTTP routes, and reports whether
// the handler may proceed.
//
// 🔴 WHY THIS EXISTS SEPARATELY FROM THE TWO DOORS THAT WERE ALREADY THERE.
// T-2 shipped two refusals and they covered two of the three ways the field
// can arrive:
//
//   - MCP tool face → fillLessonsIdentityArgs refuses by PRESENCE, before
//     dispatch. Covers every agent, because agents reach these routes only
//     through the tool face.
//   - REST request BODY → decodeJSONBodyStrict refuses it as an unknown key
//     (422). Covers the two POSTs.
//
// The third way had no door at all: a QUERY parameter. Nothing on these routes
// reads the query string, so `?task_type=whatever` was dropped on the floor and
// the request answered 200 — on the GET, and equally on both POSTs. That is
// not a cosmetic gap, it is the ORIGINAL DEFECT of this ticket reproduced on a
// different face: a caller that believes it named a classification is told
// nothing, and gets an answer that looks like the one it asked for. The two
// existing refusals say so in their own words; this closes the face they did
// not reach, with the SAME message, so the three faces cannot tell a caller
// three different stories.
//
// PRESENCE, not blankness — deliberately identical to the MCP rule.
// `?task_type=` is still a caller that believes the axis exists.
//
// 🔑 WHY THIS ONE MAY BE JUDGED BEFORE AUTHZ. The rule the lessons routes
// follow is: a refusal may precede the authz check only when it LEAKS NOTHING
// ABOUT SERVER STATE. This one qualifies — it is a pure judgment on the
// caller's own request ("you sent a field that no longer exists"), the answer
// is identical for every caller, every role and every station, and it reveals
// only what the caller itself just sent. That is the same class as malformed
// JSON, which is likewise answered before any identity is consulted.
// A refusal that consults STATE — "this role does not exist here" — must come
// AFTER authz instead, or an unauthorized caller could use it to enumerate what
// exists. So the two orders are one rule, not two habits.
//
// Scoped to the retired key BY NAME. This is not a general unknown-query
// rejector: the router binds declared query parameters and ignores the rest
// across every route on the station, and changing THAT is a station-wide
// posture change nobody asked for. The claim being made here is narrow and
// checkable: the one name T-2 retired is answered instead of swallowed.
func refuseRetiredLessonsQuery(w http.ResponseWriter, r *http.Request) bool {
	if _, present := r.URL.Query()[lessonsRetiredQueryParam]; !present {
		return true
	}
	writeError(w, http.StatusBadRequest, errLessonsTaskTypeRetired.Error())
	return false
}

// isLessonsTool reports whether name is one of the three lessons MCP tools.
// One predicate so the identity fold and the retired-argument refusal cannot
// disagree about which tools they cover.
func isLessonsTool(name string) bool {
	return name == "get_lessons" || name == "replace_lessons" || name == "patch_lessons"
}

// fillLessonsIdentityArgs folds the identity-derivable default into a
// get_lessons / replace_lessons / patch_lessons MCP call so an agent's lessons
// round-trip lands on the SAME per-role doc the boot context injects into its
// persona (T-d483), and refuses the retired task_type argument.
//
// The one path param is REQUIRED by the route. For the MCP tool face a blank
// role_key folds to the caller's OWN role — the roster's role_key for the
// verified sub (resolveBootRoleKey, the same source the write authz reads). We
// do that before the shared path validation, mirroring buildBootContext's own
// key derivation and keeping the learning loop on one concrete route.
//
// A non-agent caller (owner/machine) has no identity role, so a blank role_key is
// left untouched and the shared path validation reports it as required.
//
// Returns a non-nil error when the call must be refused outright; the caller
// renders it as a tool-level 400 rather than dispatching the route.
func (s *apiServer) fillLessonsIdentityArgs(r *http.Request, name string, arguments map[string]any) error {
	if !isLessonsTool(name) {
		return nil
	}
	// PRESENCE, not blankness: `task_type: ""` is still a caller that believes
	// the axis exists, and telling it so is the entire point.
	if _, present := arguments["task_type"]; present {
		return errLessonsTaskTypeRetired
	}
	if blankArg(arguments["role_key"]) && currentScope(r) == "agent" {
		member, err := s.dal.GetMember(currentActor(r))
		if err == nil && member != nil {
			arguments["role_key"] = resolveBootRoleKey("", member)
		}
	}
	return nil
}

// blankArg shares emptyPathParam's unset predicate: absent, null, empty, or
// whitespace-only string. Keeping one predicate prevents identity folding and
// shared path validation from disagreeing about blank path values.
func blankArg(v any) bool {
	return emptyPathParam(v)
}

// lessonsWriteAuthz enforces the per-role lessons WRITE authz shared by
// replace_lessons and patch_lessons: a caller at or above principalAdminAgent
// (owner, and the admin agent) writes ANY role's lessons; everyone else writes
// ONLY its own member's role_key (read from the roster by the verified sub,
// never a client field). Answers the error itself and reports whether the
// caller may proceed.
//
// T-5336 — WHY THE JUDGE IS THE PRINCIPAL CLASS, NOT THE TOKEN SCOPE. This
// used to read `currentScope(r) != "agent" → allow`, i.e. it recognised
// exactly ONE privileged caller: the owner (the only non-agent scope minted).
// The admin agent's token scope IS "agent" (api_auth.go mints every member
// token with scope="agent"), so Mira's admin standing did not exist on this
// path at all and she was folded into the self-role-only rule — she could
// write only role_key="assistant" (her own), and 403'd on every other role.
// The principal ladder is the office's ONE authority answer (authz.go), so
// this asks it instead of re-deriving a weaker one from the scope claim.
//
// SCOPE OF THE CHANGE, stated without optimism — it moves in BOTH directions.
// The widening: admin agents go from "self role only" to "any role". Plain
// agents (rank agent) keep the exact self-role-only rule they had; the owner
// passed before and passes now.
// The narrowing (real, measured — not a no-op; the argument lives at the two
// route rows in routes.go): a warden-kind member row carrying a NON-EMPTY
// role_key used to be able to write THAT role's lessons, because a warden's
// token is agent-scoped like every other member token (mintMemberToken →
// mintAgentToken) and the old self-role compare therefore matched. Such a row
// now 403s at the route floor, because classifyMember ranks kind=="warden" as
// machine regardless of role_key. Both production warden creation points leave
// role_key empty (api_machines.go onboard, dbseed.go); the only way to build a
// role-bearing warden is an explicit privileged hire.
func (s *apiServer) lessonsWriteAuthz(w http.ResponseWriter, r *http.Request, roleKey string) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	member, err := s.dal.GetMember(currentActor(r))
	if err != nil {
		internalError(w, err)
		return false
	}
	memberRole := ""
	if member != nil {
		memberRole = member.RoleKey
	}
	if memberRole != roleKey {
		writeError(w, http.StatusForbidden,
			"an agent may only write its own role's lessons")
		return false
	}
	return true
}

// requireLessonsAddressableRole refuses a lessons WRITE addressed to a role_key
// that NOTHING on this station can ever address again, and reports whether the
// handler may proceed.
//
// 🔴 THE HOLE THIS CLOSES. Nothing on the two lessons write routes ever
// validated role_key. The path parameter is free text, so a caller with admin
// capability could write a real lessons document under a name that names
// nothing. It got a 200 and a receipt. What it created was a document that
// draws on the SAME lessons cap as every other one, is folded into no boot
// context, and cannot appear in peek_doc_sizes (which walks the roster). Write
// succeeds, quota is spent, nobody can find it — the "drawer nobody opens" this
// ticket exists to end, surviving on the ROLE NAME rather than on the
// classification T-2 removed.
//
// 🔑 THE PREDICATE IS "CAN ANYTHING ADDRESS THIS DOCUMENT?", and the two ways
// are enumerated from MEASUREMENT, not from reading the code and believing it:
//
//  1. the role folds (foldRoleDefDTO != nil) — the very first thing
//     buildBootContext does, so such a doc is loaded by every boot of that role
//     and reported by peek_doc_sizes; or
//  2. some member carries this role_key — such a member cannot BOOT, but the
//     owner can mint it an agent token (POST /api/mint, which does not go
//     through the boot fold at all), and get_lessons then serves this document
//     to it. Measured end to end through the production routes, not inferred.
//
// Refused: a name on NEITHER list. Nothing can reach that document — no boot, no
// listing, and no identity that could be minted a token for it.
//
// 🔴 TWO WRONG VERSIONS OF THIS GATE SHIPPED BEFORE THIS ONE. Both are recorded
// because each was wrong in a way that reading the code did not reveal.
//
// (a) The FIRST draft accepted branch 2 on the stated ground that such a member
// "folds this document into its persona on every wake". THAT WAS FALSE, and the
// falsehood reached the tool descriptions before an independent review measured
// it: buildBootContext folds the ROLE first and yields nil, and both paths that
// mint a MEMBER token abort on that nil. Such a member never boots. The branch
// was right; the reason given for it was fiction.
//
// (b) The correction then over-swung and dropped branch 2 entirely, on the
// ground that a member which cannot boot has no reader at all. THAT WAS ALSO
// FALSE, and this time the wire caught it: /api/mint is a THIRD token path that
// neither the first draft nor the review had counted, and conformance's auth
// matrix builds exactly this shape through the public API (hire with a role_key
// naming no role, then mint) and pins that agent's self-write at 200. Four
// cells went red. The document IS reachable — by the agent it belongs to.
//
// The lesson worth more than the gate: BOTH errors came from reasoning about
// reachability instead of measuring it. The branch list above is now the shape
// of an experiment that was actually run.
//
// 404, not 403: this is "there is no such role", the same answer GET
// /api/roles/{role} gives. Authz is judged FIRST, so a below-admin caller still
// gets its 403 and this never becomes a way to probe what exists.
//
// READ is deliberately untouched: get_lessons on an unknown role folds to the
// seed and answers 200, which spends no cap and hides nothing.
func (s *apiServer) requireLessonsAddressableRole(w http.ResponseWriter, roleKey string) bool {
	roleDTO, err := s.foldRoleDefDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return false
	}
	if roleDTO != nil {
		return true
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return false
	}
	for _, m := range members {
		if m.RoleKey == roleKey {
			return true
		}
	}
	writeError(w, http.StatusNotFound,
		"role '"+roleKey+"' not found — a lessons doc must be addressable by "+
			"something: a role that folds (list_roles), or a member carrying that "+
			"role_key (list_members). This name is neither, so the document could "+
			"be read by nobody: no boot would load it, no member could be given a "+
			"token for it, and peek_doc_sizes (keyed by role) would never list it, "+
			"while it still spent the lessons cap. Check the role_key and retry")
	return false
}

// GET /api/lessons/{role_key} — the folded per-role lessons doc.
// READ is unrestricted for any authenticated identity.
func (s *apiServer) HandleGetLessonsApiLessonsRoleKeyGet(w http.ResponseWriter, r *http.Request, roleKey string) {
	if !refuseRetiredLessonsQuery(w, r) {
		return
	}
	dto, err := s.foldLessonsDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/lessons/{role_key} — whole-doc replace. Per-role
// WRITE authz (lessonsWriteAuthz): a caller at or above principalAdminAgent
// (owner / admin agent) writes ANY role; everyone else writes ONLY its own
// member's role_key (read from the roster by the verified sub, never a client
// field).
func (s *apiServer) HandleReplaceLessonsApiLessonsRoleKeyPost(w http.ResponseWriter, r *http.Request, roleKey string) {
	if !refuseRetiredLessonsQuery(w, r) {
		return
	}
	var body LessonsReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	if !s.lessonsWriteAuthz(w, r, roleKey) {
		return
	}
	if !s.requireLessonsAddressableRole(w, roleKey) {
		return
	}
	text := body.Text
	current, err := s.foldLessonsDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	// T-2d99 wipe guard: replace_lessons was the one destructive whole-doc
	// seam with NO guard at all — patch_lessons has had LessonsShrinkBlocked
	// since r-76. Emptying a doc that had content now needs allow_shrink.
	if !(body.AllowShrink != nil && *body.AllowShrink) {
		if WholeDocWipeBlocked(current.Text, text) {
			writeError(w, http.StatusBadRequest, docWipeRefusal("lessons doc", ""))
			return
		}
	}
	// T-3351 hard cap. Checked UNCONDITIONALLY — allow_shrink governs the
	// opposite direction (shrinking too far) and is not a bypass for this one.
	// One read, reused by the response below.
	cap := s.learningCap()
	if DocCapBlocked(cap, current.Text, text) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "lessons doc", current.Text, text))
		return
	}
	if err := s.dal.SaveWithDocumentHistory("lessons", roleKey, currentActor(r), lessonsSnapshotIn(roleKey), func(ex sqlExecer) error {
		return putLessonsOn(ex, Lessons{
			RoleKey:    roleKey,
			Text:       text,
			Tombstoned: false,
		})
	}); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	// T-91: `text` no longer rides home — the caller sent it, and a lessons doc
	// was measured at 76k chars. Still assembled from the LOCALS, which is what
	// the cap comment above promises: the size reported is the number this write
	// was judged against. `is_default` is deliberately NOT on this shape — this
	// face stamped it false unconditionally, so it could never say anything.
	writeJSON(w, http.StatusOK, lessonsReceiptDTO{
		RoleKey:   roleKey,
		SizeChars: utf8.RuneCountInString(text),
		CapChars:  cap,
		Sha256:    receiptSha256(text),
	})
}

// POST /api/lessons/{role_key}/patch — anchor-addressed patch
// (T-8327). Write cost ∝ the CHANGE, not the doc: a whole-doc replace_lessons
// stops fitting in one model output as the doc grows (76k chars observed), so
// this is the primary write seam and replace stays the last resort.
//
// Semantics (spec/openapi.json is normative): edits apply IN ORDER against the
// doc get_lessons serves (overlay ⊕ seed fold); a non-empty `old` must match
// exactly once (0/>1 hits → flat 400, WHOLE batch rejected, zero writes — the
// unique anchor doubling as an optimistic lock under last-write-wins
// concurrency); an empty `old` appends. A patch that wipes the doc, or shrinks
// a substantial doc to <10%, is refused without allow_shrink=true (the r-76
// wipe-guard posture). Same per-role write authz as replace_lessons.
func (s *apiServer) HandlePatchLessonsApiLessonsRoleKeyPatchPost(w http.ResponseWriter, r *http.Request, roleKey string) {
	if !refuseRetiredLessonsQuery(w, r) {
		return
	}
	var body LessonsPatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if len(body.Edits) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"edits requires at least one {old, new} entry")
		return
	}
	if !s.lessonsWriteAuthz(w, r, roleKey) {
		return
	}
	if !s.requireLessonsAddressableRole(w, roleKey) {
		return
	}
	current, err := s.foldLessonsDTO(roleKey)
	if err != nil {
		internalError(w, err)
		return
	}
	edits := make([]LessonsEdit, len(body.Edits))
	for i, e := range body.Edits {
		// T-2d99: an edit that carries NEITHER old NOR new is a malformed
		// entry, not a request to append nothing. Folding nil→"" here used to
		// route it into the empty-old APPEND branch, where appending "" is a
		// perfect no-op — so a batch whose keys never parsed answered 200 with
		// an unchanged doc. Refuse it instead; the whole batch is rejected and
		// nothing is written, matching the anchor-miss posture.
		if e.Old == nil && e.New == nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"edits[%d]: neither old nor new was given — an edit needs at least one of them "+
					"(empty old appends new); nothing was written", i))
			return
		}
		edits[i] = LessonsEdit{Old: strOrEmpty(e.Old), New: strOrEmpty(e.New)}
	}
	// get_lessons: the tool that serves THIS doc (the overlay ⊕ seed fold the
	// caller anchored against).
	next, applied, err := ApplyDocEdits(current.Text, edits, "get_lessons")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	if !allowShrink && LessonsShrinkBlocked(current.Text, next) {
		writeError(w, http.StatusBadRequest,
			"patch would empty (or shrink to under a tenth of) the lessons doc — pass allow_shrink=true if this is intended, or use replace_lessons; nothing was written")
		return
	}
	// T-3351 hard cap, judged on the RESULT of the patch (not the patch's own
	// size) — the whole point is that a small patch onto a huge doc is what
	// grows it. Unconditional: allow_shrink is not a bypass.
	// One read, reused by the receipt below: the number the caller is told is
	// then provably the number its write was judged against.
	cap := s.learningCap()
	if DocCapBlocked(cap, current.Text, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(cap, "lessons doc", current.Text, next))
		return
	}
	// 🔴 A batch that changed NOTHING must not reach the write. The question
	// that decides it is whether `next` differs from the stored doc — NOT how
	// many edits ApplyDocEdits counted. Those are different questions, and the
	// difference is reachable: `applied` counts an edit that changed the
	// INTERMEDIATE result it was handed, so two uniquely-anchored edits that
	// undo one another (anchor → middle, then middle → anchor) report
	// applied == 2 over a document that never moved. A gate written as
	// `applied > 0` passes that batch straight through.
	//
	// What the write accomplishes when the text is unchanged is RETAINING A
	// HISTORY VERSION of text that is not being replaced — and document history
	// keeps only the three most recent versions per doc (dal.go,
	// documentHistoryKeep = 3). Three such patches therefore evict every
	// restorable version the owner had, which is the owner's undo path in the
	// cockpit, and they do it with no signal at all: the doc still reads the
	// same, the receipt still answers 200, nothing goes red. That is worse than
	// a loud failure, and both ways in are reached by accident rather than by
	// abuse — agents send `old == new` batches deliberately as a cheap "count
	// the occurrences / read back sha256+size_chars without pulling the whole
	// doc into context" probe, which is a legitimate use of this endpoint's
	// receipt and must stay one, and an agent that edits a line then reverts it
	// within one call arrives at the same place carrying applied == 2.
	//
	// The receipt below is deliberately OUTSIDE this gate and unchanged: same
	// fields, same values (whatever applied_edits counted, plus the anchors over
	// the doc as it stands), so callers cannot tell the two paths apart by
	// shape. The write and the SSE delta are what is skipped — nothing was
	// written, so nothing is announced. update_task_manual and update_role
	// already gate their retention on "did this field actually change"
	// (roleDefHistoryStreams / taskManualHistoryStreams); the anchor-patch seams
	// were the outliers.
	if next != current.Text {
		if err := s.dal.SaveWithDocumentHistory("lessons", roleKey, currentActor(r), lessonsSnapshotIn(roleKey), func(ex sqlExecer) error {
			return putLessonsOn(ex, Lessons{
				RoleKey:    roleKey,
				Text:       next,
				Tombstoned: false,
			})
		}); err != nil {
			internalError(w, err)
			return
		}
		s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+roleKey, nil, audienceOwnerOnly(), requestTrigger(r))
	}
	writeJSON(w, http.StatusOK, lessonsPatchResultDTO{
		RoleKey:       roleKey,
		AppliedEdits:  applied,
		SizeChars:     utf8.RuneCountInString(next),
		CapChars:      cap,
		Sha256:        receiptSha256(next),
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     false,
	})
}
