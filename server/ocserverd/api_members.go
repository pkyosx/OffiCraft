package main

// api_members.go — the roster + presence + lifecycle handlers
// (handlers.handle_list_members … handle_dismiss_member + the three
// self-report presence tools). Every durable write funnels through the DAL
// and fans a member delta through the hub (the Python Repository
// commit-funnel behaviour).
//
// Reconcile dispatch note: activate/deactivate fire the EVENT-DRIVEN
// single-member reconcile (reconcile.go reconcileMemberNow — the Python
// _dispatch_reconcile_now click seam, sharing the cadence's store so the 30s
// tick stays an idempotent backstop); force-stop and the first stopped-report
// of a refocus-marked member fire the immediate robust STOP
// (dispatchRobustStopNow — handlers._dispatch_robust_stop_now).

import (
	"errors"
	"fmt"
	"net/http"
)

// minSelfRestartSecs is the restart_self minimum-liveness floor (T-4c71): a
// self-triggered recycle within this many seconds of the session connecting is
// refused (429), so a freshly respawned agent cannot immediately self-restart
// and spin a respawn storm. Owner-approved at 10 minutes — a flat floor, kept
// distinct from the context-high boot-storm guard's MinBootSecs.
const minSelfRestartSecs = 600.0

// putMember validates + persists a member and fans the member delta: a
// dismiss (roster_status=removed, the soft delete) rides as op=remove
// (deleted:true, payload null — Repository.put_member parity); every other
// write is a patch carrying the partial convenience payload (spec/sse.md
// §2.2: {id, name, status, desired_state, owner_id}).
func (s *apiServer) putMember(m Member, trigger string) error {
	if err := ValidateMember(m); err != nil {
		return err
	}
	if err := s.dal.PutMember(m); err != nil {
		return err
	}
	op := "patch"
	if m.RosterStatus == RosterStatusRemoved {
		op = "remove"
	}
	// A member delta reaches ITS OWN connection (the wind-down / recycle hooks
	// key on a member delta naming self — cli/ocagent shouldWindDown) plus the
	// owner cockpit; other agents ignore it (spec/sse.md §4).
	s.hub.Publish("member", op, "member", wireOwnerID+"::"+m.ID, s.offboardDeltaPayload(m),
		audienceMembers(m.ID), trigger)
	return nil
}

// memberDeltaPayload is the member delta's partial convenience payload
// (repository._member_payload — the client reconciles by refetch). The avatar
// choice is deliberately NOT here: it is per theme, so a payload field would
// have to name a theme, and the client already refetches to reconcile.
func memberDeltaPayload(m Member) map[string]any {
	return map[string]any{
		"id":            m.ID,
		"name":          m.Name,
		"status":        m.RosterStatus,
		"desired_state": m.DesiredState,
		"owner_id":      wireOwnerID,
	}
}

// offboardDeltaPayload is memberDeltaPayload plus the offboard notice, and it is
// the whole of "改回真的推播" (owner 2026-08-16, card rc-66b82a584c4d): the
// SERVER composes the sentence and carries the 下線程序 steps in the frame it
// pushes, instead of the agent fetching them back over HTTP once it notices it
// is being collected.
//
// The notice rides ONLY a member that is actually being wound down — a refocus
// epoch, or a graceful 下線 (offboardKindOf decides). Attaching it to every
// member delta would put a document fold and a couple of kilobytes on every
// roster change. ⚠️ Within those states it rides EVERY write to that row, not
// just the first: the client is what de-duplicates, by keying on the sentence
// it last printed.
//
// An empty notice omits the key rather than sending "": the client's fallback
// arms on the key being absent, and an empty string would read as "the server
// sent me a notice and it said nothing".
func (s *apiServer) offboardDeltaPayload(m Member) map[string]any {
	payload := memberDeltaPayload(m)
	kind, carries := offboardKindOf(m, nowSecs())
	if !carries {
		return payload
	}
	if notice := s.offboardNoticeFor(m, kind); notice != "" {
		payload["offboard_notice"] = notice
	}
	return payload
}

// offboardKindOf answers the two questions every offboard delta turns on: does
// this member carry a notice at all, and is it the SOFT one or the FINAL call.
//
// The owner's ruling (2026-08-16) is that his own two buttons and the agent's
// own context pressure walk the SAME sequence, and that what tells the
// situations apart is whether there is still room:
//
//   - SOFT (停止) — 下線 (desired offline + a stopping anchor, the graceful
//     arm) and EVERY refocus cause except the one below: 重新聚焦, 改機器,
//     model/runtime, restart_self, and the FIRST context threshold. It says
//     work the sequence, then call restart_self yourself; no countdown clause,
//     because on these arms there is no clock AT ALL — not now, and not later.
//   - FINAL (加速停止) — the two 加速停止 causes, and only those: the SECOND
//     context threshold (context_high) and the owner's own press
//     (accelerated_stop). The collection is already under way and the recycle
//     clock is running, so the sentence has to say so.
//
// 🔴 The membership is decided by winddownKindFor, not here and not in
// recycleGraceFor. Both of those used to carry their own copy of the list, and
// the copies were kept identical by hand (T-ed79).
//
// 🔴 The soft arm is the ONLY reason 下線 reaches the agent at all. Before this,
// a deactivate stamped stopping_since and nothing else, so the notice condition
// (refocus_since > 0) was false and the agent was collected having never been
// shown the sequence — while the client-side wind-down declared "durable state
// already server-side — nothing extra to flush" on its behalf, which was not
// true of any session holding an unwritten hand-off.
// There is NO soft→final promotion any more (owner 2026-08-19, card
// rc-c540367065ad). 重新聚焦 used to open soft and, ten minutes later, change
// its mind and say "you have 120 seconds" — a split only an agent that ran past
// the soft window ever saw. The owner's ruling is that 重新聚焦 is the same
// shape as 下線: no countdown in the sentence because no clock is running, and
// the collection is the agent's own stopped report or the owner's force-stop.
// The pair has to move together — recycleGraceFor is the clock, this is the
// sentence, and changing one without the other is what makes a silent deadline.
// ⚠️ `now` is READ BY NOTHING in here any more, and that is the invariant, not
// an oversight: after T-c996 neither soft arm turns on a clock, so there is no
// longer any time at which this answer changes. It stays in the signature
// because the question this function answers is still "what would this member
// be told AT time T" — and a later arm that does need a clock must be handed
// one here rather than reaching for a global one, which is how the sentence and
// the clock came apart in the first place.
func offboardKindOf(m Member, now float64) (kind string, carries bool) {
	_ = now
	if m.DesiredState == DesiredStateOffline {
		// Only the graceful arm: a member with no stopping anchor is not being
		// wound down (and a cancelled wake is force-stopped outright, which is
		// deliberately silent — see HandleForceStopMember).
		//
		// 🔴 THIS SOFT IS HARD-CODED, and deliberately does NOT read
		// winddownKindFor (T-ed79). That function answers "what does this
		// refocus_op mean", and this arm has no refocus_op to ask about: 下線 is
		// a desired_state transition, not a wind-down CAUSE, and it stamps no
		// epoch. Routing it through the single source would mean feeding it ""
		// and depending on the DEFAULT arm — which agrees today (soft) but
		// agrees by accident: the default exists so that an unruled *cause*
		// gets no deadline, and coupling 下線's ruling to it would let a future
		// change to the cause default silently move a ruling the owner made
		// about a different thing. Two rulings that coincide are not one ruling.
		//
		// It stays SOFT forever, because nothing collects it on a clock: the
		// owner ruled 下線 runs no countdown at all (rc-27d1710174dd), so a
		// notice claiming 120 seconds here would be a promise nobody keeps —
		// an agent would cut its hand-off short to beat a deadline that does
		// not exist. Escalation on this arm is the owner pressing force-stop,
		// and that path deliberately says nothing.
		//
		// 🔴 …and "deliberately says nothing" has to be enforced HERE, not just
		// asserted in prose. force-stop sets desired_state=offline AND stamps
		// stopping_since before it publishes, so on the sentence above alone the
		// member it just killed receives a full SOFT notice on its own stream —
		// telling a session that is about to be cut off to work the sequence and
		// call restart_self. Independent e2e verification observed exactly that
		// frame; the owner's ruling is that force-stop sends no message at all.
		//
		// 🔴 RECONFIRMED 2026-08-18, and written down because it was nearly
		// reversed that day: the owner first said a force-stop should tell the
		// agent what to do (c-7b2163781ee2), was shown that this would overturn
		// the named ruling above, and chose silence again (c-5c8bc3d7362d).
		// Nothing in the code changed, which is exactly why the review is worth
		// recording — the next person to notice this arm should know it has been
		// looked at deliberately, not merely never revisited.
		//
		// 🔴 And it is enforced for BOTH kinds since T-c996. It used to be
		// enforced here for staff only: forcedEpochLive reads forced_stop_at,
		// OutsourceWorker had no such field, so the predicate was false for every
		// worker and the arm that must stay silent was the one that could still
		// speak. An outsource 停止 now stamps the same anchors (api_outsource.go)
		// — it kills on the spot, so it IS this shape, whatever it is named.
		if m.StoppingSince > 0 && !forcedEpochLive(m) {
			// 🔴 …WITH ONE EXCEPTION, AND ONLY ONE: the owner pressed 加速停止 on
			// this stop (T-ed79). The paragraphs above rule out a clock the
			// SERVER starts; this is a clock the owner started, on the rung
			// between "wait indefinitely" and "cut it off with no sentence at
			// all". It reads winddownKindFor rather than testing the constant
			// again, because the clock (decideDown) reads the same function —
			// which is the whole reason 下線's hard-coded soft above is safe to
			// leave hard-coded: the ONE arm that can carry a clock is the one
			// arm that asks the single source.
			if kind, clocked := winddownKindFor(m.RefocusOp); clocked {
				return kind, true
			}
			return offboardKindSoft, true
		}
		return "", false
	}
	if m.RefocusSince <= 0 {
		return "", false
	}
	// 🔴 ONE READ, not a second list (T-ed79). This arm used to spell the
	// judgement out again — 重新聚焦 soft, everything else final — beside a
	// recycleGraceFor that spelled out the same judgement in the other file.
	// The sentence and the clock now come from winddownKindFor, so a cause that
	// is told "no countdown" is, by construction, a cause nothing collects on a
	// clock. A countdown clause on an uncollected arm starts a timer in the
	// agent's head that nothing is counting; a clock on an unannounced arm cuts
	// a hand-off off with no warning at all. Both are the same bug, and they
	// are only reachable by making these two disagree.
	kind, _ = winddownKindFor(m.RefocusOp)
	return kind, true
}

const (
	offboardKindSoft  = "soft"
	offboardKindFinal = "final"
)

// forcedEpochLive: the stop this member is currently under was opened by a
// FORCE-stop, not by 下線. It is the one judgement that separates "cut off
// deliberately" from "working its close-out", and three places need it — the
// notice (a forced path says nothing), the SSE stop gate (a forced member's
// reconnect is refused) and deactivate (a forced epoch must not be re-stamped
// into a softer one). One definition, because two copies of this could disagree
// about the same member.
//
// stopping_since > 0 is what scopes it to a LIVE epoch: activate clears the stop
// anchors but deliberately KEEPS forced_stop_at as the durable record that a
// past session was cut off, so reading that column alone would treat every
// member ever force-stopped as permanently forced.
//
// 🔴 The >= is LOAD-BEARING, not defensive. force-stop stamps stopping_since
// and forced_stop_at from two nowSecs() calls with no I/O between them, and at
// 1.78e9 a float64 tick is ~238ns — so the two anchors landing on the SAME
// value is the NORMAL path, not a coincidence. Independent review measured it:
// every failure dump from the mutants that exercise the real handler shows the
// two columns identical. Tidying this into > breaks force-stop outright.
func forcedEpochLive(m Member) bool {
	return m.ForcedStopAt > 0.0 && m.StoppingSince > 0.0 &&
		m.ForcedStopAt >= m.StoppingSince
}

// offboardCloserFor names the tool that ACTUALLY ends this member's sequence.
// A member still wanted online is being handed over and re-starts itself; one
// the owner has taken down is not coming back, and restart_self refuses it by
// design (it is a RE-start). Telling it otherwise would be an instruction that
// can only answer 409 — on an arm where nothing collects it on a clock, so it
// would sit there refused until someone pressed force-stop.
func offboardCloserFor(m Member) string {
	if m.DesiredState == DesiredStateOffline {
		return offboardCloserReportStopped
	}
	return offboardCloserRestartSelf
}

// offboardNoticeFor composes the sentence for a member that is being wound
// down: the ONE approved sentence, plus the deadline clause when this is the
// final call (the deadline is winddownDeadlineOf, i.e. the anchor plus
// stop.accelerated_grace_secs — owner-settable since T-ed79, not a constant). It reads the session's own gauge so the agent is told where it
// actually is, not just that it is over the line — the owner's requirement that
// the notice carry 「他現在 context / round 狀況，以及我們兩個系統數字是多少」.
func (s *apiServer) offboardNoticeFor(m Member, kind string) string {
	cfg := s.ctxHighConfig()
	// The gauge is absent on a server assembled without one (and a session that
	// never reported has no entry either). Both arms below then OMIT the
	// position rather than dropping the notice or printing a placeholder:
	// WHERE the session is is useful, being told it is being collected is
	// essential, and a literal "?" is neither.
	var record map[string]any
	if s.gauge != nil {
		record = s.gauge.Get(m.ID)
	}
	var where string
	if NormalizeRuntime(m.Runtime) == RuntimeCodex {
		final := s.codexCompactionThresholdSetting()
		notice := s.codexNoticeRoundSetting()
		if notice < 1 {
			notice = final - 1
		}
		// SAME RULE AS THE CLAUDE ARM BELOW, and it has to be stated twice
		// because the two arms read different keys. This used to print a
		// literal "?" ("compaction round ? (your limits: round 3 / round 4)")
		// whenever the gauge held no compaction_count — which is every
		// refocus-triggered close-out, because that arm is not fired by a
		// round count at all. Two spellings of "no value" in one output is
		// the next reader's trap; this one omits the position too, and the
		// limits still name the band.
		round := ""
		if record != nil {
			if v, ok := asNumber(record["compaction_count"]); ok {
				round = fmt.Sprintf("%d", int(v))
			}
		}
		if kind == offboardKindFinal {
			round = fmt.Sprintf("%d", final)
		}
		if round == "" {
			where = fmt.Sprintf("close-out (your limits: round %d / round %d)",
				notice, final)
		} else {
			where = fmt.Sprintf("compaction round %s (your limits: round %d / round %d)",
				round, notice, final)
		}
	} else {
		// NO VALUE -> SAY THE LIMITS, NOT A QUESTION MARK (T-0974 shipping
		// verification, 2026-08-20). This used to print a LITERAL "?" into the
		// sentence ("context ?% (your limits: 55% / 65%)") whenever the gauge
		// held no context_pct - which is EVERY refocus-triggered close-out,
		// because that arm is not fired by a pct at all. What the reader sees
		// is a broken field, and it disagrees with how this same file treats a
		// missing value elsewhere (the "[station ...]" clause omits itself
		// rather than printing a placeholder; the codex arm above now omits
		// too — it did NOT until this same ticket). Two spellings of "no
		// value" in one output is the next reader's trap, so this one omits
		// too: the limits are still named, because they are what tells the
		// reader which band it is in.
		if record != nil {
			if v, ok := asNumber(record["context_pct"]); ok {
				where = fmt.Sprintf("context %v%% (your limits: %d%% / %d%%)",
					formatPct(v), cfg.NoticePct, cfg.HandoverPct)
			}
		}
		if where == "" {
			where = fmt.Sprintf("close-out (your limits: %d%% / %d%%)",
				cfg.NoticePct, cfg.HandoverPct)
		}
	}
	// The deadline quoted in the sentence and the deadline the cockpit shows come
	// from ONE expression (T-d6a7). offboardKindOf only answers "final" for a
	// clocked arm, so this is positive exactly when the sentence needs it.
	notice := offboardNotice(where, offboardCloserFor(m), kind == offboardKindFinal,
		winddownDeadlineOf(m, s.reconcileConfigLive()),
		s.offboardText())
	if clause := s.offboardManualWriteBackFor(m); clause != "" {
		notice += "\n\n" + clause
	}
	return notice
}

// offboardManualWriteBackFor resolves the 記憶回寫 clause for THIS member: the
// worker's bound task decides whether there is a 手冊 to write back into, and
// offboardManualWriteBack composes the sentence.
//
// 🔴 OUTSOURCE ONLY, deliberately. A 正職 has a role of its own and its learnings
// may belong to the ROLE rather than to any one task's type — which document a
// staff member writes into is ruled by the boot doc's 「記憶與學習」 section, and
// naming one document here would overrule it from the wrong place. A worker has
// no role and outlives nothing: its one task IS its memory, which is why the
// owner's ruling names 外包 and this gate does too.
//
// Best-effort by construction: an unreadable task row, a task with no type, or a
// deleted manual all fall back to saying LESS (no clause, or the bare key as the
// label) — never to blocking the 預告, which is the message that actually has to
// arrive.
func (s *apiServer) offboardManualWriteBackFor(m Member) string {
	if m.Kind != KindOutsource || m.LinkedTaskID == nil || *m.LinkedTaskID == "" {
		return ""
	}
	t, err := s.dal.GetTask(*m.LinkedTaskID)
	if err != nil || t == nil {
		return ""
	}
	label := ""
	if t.TypeKey != "" {
		if manual, err := s.dal.GetTaskManual(t.TypeKey); err == nil && manual != nil {
			label = manualDisplayLabel(manual.DisplayName, t.TypeKey)
		}
	}
	return offboardManualWriteBack(t.TypeKey, label)
}

// resolveAvatarMember admits active staff and outsource rows but rejects
// wardens: a machine is infrastructure, not a person with a visual identity.
func (s *apiServer) resolveAvatarMember(memberID string) (*Member, error) {
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	return m, nil
}

func (s *apiServer) publishMemberAvatarChanged(m Member, trigger string) {
	if m.Kind == KindOutsource {
		s.publishOutsourceWorker(workerFromMember(m), trigger)
		return
	}
	s.hub.Publish("member", "patch", "member", wireOwnerID+"::"+m.ID,
		s.offboardDeltaPayload(m), audienceMembers(m.ID), trigger)
}

// PUT /api/members/{member_id}/theme-avatar — owner-only visual identity.
//
// 🔴 OWNER-ONLY, AND THAT IS A RULING, NOT AN OVERSIGHT (owner 2026-07-27,
// carried forward from the retired personal-avatar route). A member's face is
// how the owner tells the fleet apart at a glance, so it is governance: an
// agent or a machine token must not be able to change how it appears to the
// human watching. The route therefore sits behind the owner gate AND stays out
// of MCP — it is a cockpit control, not an agent tool. routes.go pins both
// properties and routes_t6020_governance_test.go tells maintainers to read this
// note before widening either one.
//
// The write is scoped to ONE (member, theme) pair. A choice the same member
// made in another theme is never touched, which is the whole point of the
// association: switching themes and switching back restores each theme's own
// image rather than re-resolving one index against a different pool.
func (s *apiServer) HandleSetMemberThemeAvatarApiMembersMemberIdThemeAvatarPut(
	w http.ResponseWriter,
	r *http.Request,
	memberID string,
) {
	var body MemberThemeAvatarUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "theme_id", "icon_id") {
		return
	}
	if body.ThemeId == "" || body.IconId == "" {
		writeError(w, http.StatusUnprocessableEntity, "theme_id and icon_id must not be empty")
		return
	}
	m, err := s.resolveAvatarMember(memberID)
	if err != nil {
		writeResolveError(w, err, "member", memberID)
		return
	}
	if m.Kind == KindWarden {
		writeError(w, http.StatusUnprocessableEntity, "a machine cannot have a theme avatar")
		return
	}
	// Resolve the icon against the NAMED theme's matching pool, not the active
	// one: the cockpit may write a choice for a theme it is not showing, and an
	// id the pool can not resolve is exactly how a member silently ends up
	// wearing another member's face.
	if !s.themeIconIDsFor(body.ThemeId)[body.IconId] {
		writeError(w, http.StatusUnprocessableEntity,
			"icon_id is not an image in that theme's pool")
		return
	}
	if avatarPoolKindFor(m.Kind) == "" {
		writeError(w, http.StatusUnprocessableEntity, "this member kind has no avatar pool")
		return
	}
	if err := s.dal.SetMemberThemeAvatar(m.ID, body.ThemeId, body.IconId); err != nil {
		internalError(w, err)
		return
	}
	s.invalidateAvatarSelections()
	s.publishMemberAvatarChanged(*m, requestTrigger(r))
	writeJSON(w, http.StatusOK, MemberThemeAvatarDTO{
		MemberId: m.ID, ThemeId: body.ThemeId, IconId: body.IconId,
	})
}

// GET /api/members — the roster (soft-removed rows omitted). online is the
// live SSE projection; machine the OBSERVED position; unread_count the pure
// inverse of the caller's chat_read watermark.
//
// ?fields=light (T-cf91) is the ADDITIVE identity-only projection for surfaces
// that render ONLY a member's name + role (the 請示卡頁 attributes each card to
// its asker and needs nothing else). It SKIPS the whole-chat scan +
// per-member chat_read watermark math (UnreadCounts over ListChat) and the
// per-member presence / observed-host derivation (hub + telemetry lookups) —
// none of which the name/role view reads. The light DTO keeps the SAME
// memberDTO wire shape (no new response schema — additive), but the fields
// those skipped computations feed are HONEST-EMPTY: unread_count 0, presence
// "", machine "", last_op* untouched-from-row. A consumer must not read those
// off a light response — the value is "not computed", not "known zero". The
// default (no fields param, or any value other than "light") is byte-for-byte
// the full roster as before. This mirrors the roster hook's matching change:
// the light consumer also stops treating chat SSE deltas as a roster refetch
// trigger (a message never changes a name or role), so a company-wide chat
// line no longer re-pulls this endpoint at all.
func (s *apiServer) HandleListMembersApiMembersGet(w http.ResponseWriter, r *http.Request, params HandleListMembersApiMembersGetParams) {
	members, err := s.dal.ListMembersIncludingOutsource()
	if err != nil {
		internalError(w, err)
		return
	}
	light := trimmedOrEmpty(params.Fields) == "light"

	// unread rides the caller's chat_read watermark over the whole chat stream —
	// the single most expensive part of this handler and exactly what the light
	// projection exists to avoid. Only compute it on the full path.
	var unread map[string]int
	if !light {
		var err error
		// The SAME computation the single-member handler runs (api_helpers.go) —
		// one field, one answer, whichever endpoint you ask.
		unread, err = s.unreadCountsForRequest(r)
		if err != nil {
			internalError(w, err)
			return
		}
	}

	out := []memberDTO{}
	for _, m := range members {
		if m.RosterStatus == RosterStatusRemoved {
			continue
		}
		roleName, err := s.memberRoleName(m)
		if err != nil {
			internalError(w, err)
			return
		}
		if light {
			out = append(out, s.newMemberLightDTO(m, roleName))
			continue
		}
		out = append(out, s.newMemberDTO(m, roleName, s.observedHost(m), unread[m.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/members — hire. The server mints the id; a blank name is 422. A
// body carrying kind/role_key is PRIVILEGE-BEARING (warden = machine
// principal, assistant = admin principal) and demands an admin_agent caller.
func (s *apiServer) HandleHireMemberApiMembersPost(w http.ResponseWriter, r *http.Request) {
	var body MemberHireDTO
	if !decodeJSONBodyRequired(w, r, &body, "name") {
		return
	}
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "member requires a name")
		return
	}
	privileged := trimmedOrEmpty(body.Kind) != "" || trimmedOrEmpty(body.RoleKey) != ""
	if privileged && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		writeError(w, http.StatusForbidden,
			"hiring with kind/role_key is privilege-bearing; "+
				"it requires an owner or an admin-role caller")
		return
	}
	if body.Effort != nil && !validEffort(*body.Effort) {
		writeError(w, http.StatusUnprocessableEntity,
			"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
		return
	}
	runtime := RuntimeClaude
	if body.Runtime != nil {
		runtime = string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
	}
	// The Go kind is a CLOSED set: the Python bare hire's kind="" folds to
	// "assistant" at this ingest seam (CanonicalKind — owner-approved mapping);
	// a kind outside the closed set is refused.
	kind, err := CanonicalKind(strOrEmpty(body.Kind))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	effort := strOrEmpty(body.Effort)
	if effort == "" {
		effort = "medium"
	}
	m := Member{
		ID:               "m-" + newHexID(12),
		Name:             name,
		Kind:             kind,
		RoleKey:          strOrEmpty(body.RoleKey),
		Runtime:          runtime,
		Model:            strOrEmpty(body.Model),
		Effort:           effort,
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
	if err := s.putMember(m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, m)
}

// GET /api/members/{member_id} — one roster member (removed → 404); machine
// is the OBSERVED position. SELF-READ exception (T-ea82): an outsource worker
// reading its OWN row (memberId == the verified sub) resolves — the ocagent
// recycle/wind-down hooks refetch GET /api/members/<self> and must see the
// worker's desired_state/refocus_since; any OTHER ow- target keeps the
// pre-fold 404 (resolveMember).
func (s *apiServer) HandleGetMemberApiMembersMemberIdGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if errors.Is(err, errNotFound) && memberId == currentActor(r) {
		m, err = s.resolveSelf(r)
	}
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	// unread_count is COMPUTED here, exactly as the list computes it. Handing
	// newMemberDTO a literal 0 (what this line used to do) made the roster badge
	// a one-way ratchet: the cockpit re-reads one member on a chat delta, so the
	// badge the delta was announcing was zeroed instead of raised. Pinned by
	// api_members_unread_parity_test.go.
	unread, err := s.unreadCountsForRequest(r)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.newMemberDTO(*m, roleName, s.observedHost(*m), unread[m.ID]))
}

// PATCH /api/members/{member_id} — partial edit (name/runtime/model/effort).
// Blank name or an unknown runtime/effort is rejected.
func (s *apiServer) HandleUpdateMemberApiMembersMemberIdPatch(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if body.Name != nil {
		name := trimString(*body.Name)
		if name == "" {
			writeError(w, http.StatusUnprocessableEntity, "member name cannot be blank")
			return
		}
		m.Name = name
	}
	// The three LAUNCH INTENTS are tracked separately from the display name:
	// only they are baked into a boot frame, so only they can be stale in a
	// running session — renaming a member must never recycle it.
	launchIntentChanged := false
	if body.Model != nil {
		launchIntentChanged = launchIntentChanged || *body.Model != m.Model
		m.Model = *body.Model
	}
	if body.Runtime != nil {
		runtime := string(*body.Runtime)
		if !ValidRuntime(runtime) {
			writeError(w, http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
			return
		}
		// COMPARE WHAT THE WIRE CARRIES, NOT WHAT THE COLUMN HOLDS. An unset
		// runtime is not a third value: NormalizeRuntime("") is claude, that is
		// what buildStartFrame stamps, and that is what the running session is
		// already on. A raw `runtime != m.Runtime` reads "" as different from
		// "claude" and charges the owner a wind-down + 重新聚焦 for a save that
		// changes nothing the agent could observe.
		//
		// THIS PACKAGE OPENED THAT READING. An earlier draft of this comment
		// claimed the defect predates T-b3d0; it does not. PutMember used to
		// bind NormalizeRuntime(m.Runtime), so every persisted row held a
		// concrete "claude" — the out-of-box assistant's included — and the raw
		// comparison never fired spuriously. The commit before this one is what
		// stopped normalizing on the way in, so that "nobody has picked yet"
		// stays distinguishable from "the owner picked claude"; that is what
		// makes "" durable, and therefore what makes this comparison wrong. The
		// damage is repaired inside the package that created it.
		//
		// seedOutOfBox never writing a runtime is true but does not carry the
		// claim on its own: it only covers what the seed literal contains, not
		// what the write path then stored. Rows that already exist keep
		// whatever is on disk (installs running the released code are on
		// "claude"); the rows that sit on "" are the ones written from here on
		// — a fresh seed, or a member dispatched before its machine has ever
		// reported capabilities (resolveEmptyRuntimeForPlacement leaves it
		// unset there by design). For those, the first owner to open
		// 成員設定 and press 儲存 on the runtime she was already running
		// would pay a recycle for a no-op edit.
		//
		// The WRITE below still lands: "" -> "claude" is a real intent the owner
		// stated, and persisting it stops placement from resolving that member
		// against some other machine later. Only the recycle is withheld.
		launchIntentChanged = launchIntentChanged ||
			NormalizeRuntime(runtime) != NormalizeRuntime(m.Runtime)
		m.Runtime = runtime
	}
	if body.Effort != nil {
		if !validEffort(*body.Effort) {
			writeError(w, http.StatusUnprocessableEntity,
				"effort must be one of [high low max medium]; got '"+*body.Effort+"'")
			return
		}
		launchIntentChanged = launchIntentChanged || *body.Effort != m.Effort
		m.Effort = *body.Effort
	}
	// T-b6d9: a launch intent used to be written and then simply ignored by the
	// live session — the owner pressed 儲存, got a 200, and the member went on
	// running the OLD model until something unrelated respawned it. Now the
	// change opens the SAME graceful wind-down 重新聚焦 has always had, so the
	// member finishes what it was doing and comes back on the new value. Same
	// single write: the epoch and the new value can never land apart.
	heldDown := false
	if launchIntentChanged {
		// A member the owner has stopped takes the new value on its next 活化 and
		// NOTHING happens now — which is right, and used to be indistinguishable
		// from "it took effect". The receipt is folded into this same putMember so
		// the value and the explanation land in one write and one delta.
		heldDown = !s.armMemberOwnerOpHandover(m, memberOpModel) &&
			m.DesiredState == DesiredStateOffline
		if heldDown {
			stampMemberOpReceipt(m, memberHeldDownReceipt(memberOpModel), nowSecs())
		}
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/activate — write desired_state=online intent.
// ALWAYS FORCE-REVIVE: both winding-down anchors clear unconditionally.
func (s *apiServer) HandleActivateMemberApiMembersMemberIdActivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberActivateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// The machine bind is held to the SAME rule as every other placement write
	// face: any non-blank id must name a real machine. activate is the one that
	// most needs it — it flips desired_state online in the same call, so an
	// unreachable pin here manufactures exactly the "wants to be online, can
	// never be dispatched, never heals" member this validation exists to prevent.
	// "" still clears the pin (the member then waits for a placement).
	if body.MachineId != nil && *body.MachineId != "" {
		if _, err := s.resolveMachine(*body.MachineId); err != nil {
			writeResolveError(w, err, "machine", *body.MachineId)
			return
		}
	}
	m.StoppingSince = 0.0
	m.WakingSince = 0.0
	m.DesiredState = DesiredStateOnline
	if body.MachineId != nil {
		m.DesiredMachineID = *body.MachineId
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile (the Python _dispatch_reconcile_now click seam):
	// decide + dispatch the START NOW, not on a later tick; the shared
	// reconcile store makes the cadence an idempotent backstop. The intent is
	// already persisted so the activate never FAILS on dispatch — but we OBSERVE
	// it (T-ba62): a decided START the warden could not accept (machine offline,
	// warden never installed, warden's SSE down) surfaces activation_pending=true,
	// exactly as relocate has reported relocation_pending since T-8655. Dropping
	// this return value was the whole bug: an activate against an unreachable
	// warden answered a clean 200 with zero signal, so "waking" and "nothing was
	// dispatched and nothing will be until the next cadence tick" looked identical.
	dec := s.reconcileMemberNow(m.ID)
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	dto := s.newMemberDTO(*m, roleName, "", 0)
	// POSITIVE determination (T-ba62 review R4), not a list of known failures.
	// `dec.DispatchUnlanded` alone was wrong: reconcileOne ALSO downgrades a
	// START to none when buildStartFrame cannot assemble a payload (missing
	// persona / token) and does NOT set DispatchUnlanded there — so a reachable
	// warden plus an unbuildable frame answered a clean 200 with no pending flag
	// and nothing dispatched. Ask instead whether a START actually went out; an
	// already-online member needs none, and every other outcome — backoff,
	// circuit-open, and failure modes not yet invented — is honestly "nothing
	// has been dispatched yet".
	if dec.Command != reconcileCmdStart && !s.hub.IsOnline(m.ID) {
		pending := true
		dto.ActivationPending = &pending
		// …and WHICH pending (T-ed79 #14). The flag is one bit and the comment
		// above lists at least four states that reach it. The tick has already
		// decided which one it is; stamp that on the row so the cockpit can say it
		// instead of showing a pending badge with nothing behind it. An arm that
		// named no code falls back to the generic "asked, nothing dispatched yet",
		// which is still strictly more than the blank it replaces — and is
		// deliberately NOT invented per-arm here: a new stall arm should name
		// itself at the decision site, not be guessed at from this end.
		reason := dec.ReasonCode
		if reason == "" {
			reason = spawnReasonWardenLost + ": 活化 was recorded, but nothing has been " +
				"dispatched yet — the machine's warden did not take the start. It will " +
				"be retried; if it stays here, check that machine"
		}
		s.stampMemberOpBlocked(m.ID, reason, nowSecs())
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/members/{member_id}/relocate — the owner cockpit's 改機器 for a roster
// member (admin-gated, route Requires=admin_agent — parity with the member
// lifecycle family). The member twin of the outsource-worker relocate: write the
// owner-pinned desired_machine_id, then run the SAME event-driven reconcile the
// activate click uses (reconcileMemberNow). A LIVE member is auto-migrated onto
// the chosen machine, but SINCE T-b6d9 GRACEFULLY: the pin is written together
// with a refocus epoch, the agent gets the ordinary 下線程序 wake, and
// the kill+re-spawn happens at the 收口 — which since T-ed79 is its own
// report_stopped or the owner's force-stop, and NOTHING ELSE. 🔴 A relocate has
// no RecycleGrace ceiling any more: winddownKindFor answers soft for it, so
// recycleGraceFor answers "no clock" and the recycle arm never times it out.
// This line used to name that ceiling — a window an owner would wait out and
// that never closes. It used to be an immediate robust STOP with no
// warning at all (fbc5280). An offline member just re-pins so the next wake
// lands there — no epoch, nothing to wind down. PLACEMENT ONLY — unlike activate it NEVER
// touches desired_state (or the stopping/waking anchors): a relocate is not a
// wake. 404 for an unknown / removed member; any non-"" machine_id that names no
// real machine is a 404, so a stale/typo'd id never pins the member to a
// placement that can never boot (the worker-relocate reasoning). machine_id is
// REQUIRED since owner 2026-07-27 (relocateNeedsMachineMsg): an absent key is a
// 422 and an explicit null / "" is a 400 — a relocate names a destination and no
// longer doubles as an unpin. The literal "auto" is NOT exempt from the resolve: it used to be
// waved through as a pseudo-machine, which pinned the member to a destination
// dispatch could never reach (IsOnline("auto") is always false) and reconcile
// never healed — the very hole a nonexistent concrete id was already 404'd for.
func (s *apiServer) HandleRelocateMemberApiMembersMemberIdRelocatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body MemberRelocateDTO
	if !decodeJSONBodyRequired(w, r, &body, "machine_id") {
		return
	}
	if body.MachineId == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	machineID := body.MachineId
	if _, err := s.resolveMachine(machineID); err != nil {
		writeResolveError(w, err, "machine", machineID)
		return
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		// P7c (gate rc-2786636f30e5, 外包對齊正職): the tool's semantics are "move
		// one agent" — an id that names no STAFF member falls through to the
		// outsource projection, so an admin agent's MCP relocate_member moves a
		// worker with the same verb. Since the P7d fold both live in the member
		// table, but resolveMember deliberately excludes kind='outsource', so
		// an ow- id still routes HERE — onto the worker relocate core (worker
		// spawn machinery), never the member reconcile path. The id namespaces
		// stay disjoint ("m-…"/named roster ids vs "ow-…"), so no shadowing.
		if errors.Is(err, errNotFound) {
			if worker, werr := s.dal.GetOutsourceWorker(memberId); werr == nil &&
				worker != nil && worker.Status != WorkerStatusReleased {
				s.relocateWorkerByID(w, r, memberId, machineID)
				return
			}
		}
		writeResolveError(w, err, "member", memberId)
		return
	}
	// The placement pin is the only INTENT mutation — desired_state is
	// deliberately left untouched (the activate contrast).
	m.DesiredMachineID = machineID
	// T-b6d9: a LIVE member used to be robust-STOPped on the spot by the
	// reconcile below — no 預告, no grace, not even a stopping_since, so it just
	// vanished from the cockpit with whatever it was mid-way through. It now
	// gets the same wind-down 重新聚焦 has always had; the winding-down anchors
	// ARE written in that case, and only in that case. Same putMember, so the
	// new pin and the epoch land together and the delta the agent wakes on
	// already names the destination.
	windDown := s.armMemberOwnerOpHandover(m, memberOpRelocate)
	// Held down: the pin is stored and nothing is moved. Same receipt, same
	// single write — see memberHeldDownReceipt.
	if !windDown && m.DesiredState == DesiredStateOffline {
		stampMemberOpReceipt(m, memberHeldDownReceipt(memberOpRelocate), nowSecs())
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven reconcile: with a wind-down open this decides "awaiting agent
	// dump" and dispatches NOTHING (the 收口 owns the move); it still runs so the
	// tick state is advanced here rather than up to 30s later, and it remains the
	// path that migrates a member nobody stamped for (the decideUp relocate arm,
	// now a backstop). An offline member is a no-op here (nothing to move). The
	// pin is already persisted so the relocate never FAILS on dispatch — but we
	// OBSERVE it: a decided recycle STOP / START that the warden could not accept
	// (old/new machine unreachable) surfaces relocation_pending=true, so the
	// caller sees "move scheduled, not yet landed" instead of a silent 200
	// success (T-8655). The cadence retries the pinned move regardless.
	dec := s.reconcileMemberNow(m.ID)
	roleName, err := s.memberRoleName(*m)
	if err != nil {
		internalError(w, err)
		return
	}
	dto := s.newMemberDTO(*m, roleName, "", 0)
	// relocation_pending means what it has always meant — "move scheduled, not
	// yet landed". T-b6d9 adds a SECOND way to be in that state: a wind-down was
	// opened, so nothing has been dispatched YET and the member is still on the
	// old machine until the 收口. Reporting a clean landed 200 there would be the
	// same silent false-success T-8655 removed for the unreachable-warden case.
	if dec.DispatchUnlanded || windDown {
		pending := true
		dto.RelocationPending = &pending
	}
	// …and WHICH of the two it is (T-927a). The wind-down case is a deliberate
	// deferral, not a delivery failure, so the caller must be able to hold back
	// the "nothing was dispatched" alert for it. Reported separately rather than
	// by narrowing relocation_pending: that field's meaning is on the frozen
	// wire and existing readers depend on it covering both.
	if windDown {
		deferred := true
		dto.RelocationDeferred = &deferred
	}
	writeJSON(w, http.StatusOK, dto)
}

// memberHeldDownReceipt is the sentence a staff owner-verb leaves on the row when
// it was SAVED and nothing was started, because the owner has this member held
// down (T-ed79 #4 / #14). It is the worker receipt's twin, verb-for-verb:
// respawnWorkerForOwnerOp has written exactly this for 改機器 / 換 model / 重啟
// since the reason-code family landed, and staff answered a clean 200 with an
// empty row.
//
// 🔴 WHY THE STATE NEEDS A NAME AT ALL. Three different situations reach the
// same silent 200 on these handlers — the owner pressed 停止 (this one), the
// member is simply offline, and this epoch's wind-down was already collected —
// and the owner has no way to tell them apart. Only the FIRST is one his own
// earlier action caused, and it is the only one a receipt can resolve for him
// ("重啟 it when you want it to run"). The other two are not stalls: an offline
// member picks the value up at its next wake, which is the ordinary story, and
// stamping a receipt for it would be noise on every edit made while a member is
// asleep.
func memberHeldDownReceipt(op string) string {
	return spawnReasonHeldDown + ": the " + op + " was saved, but nothing was " +
		"started — this member is stopped; 活化 it when you want it to run"
}

// clearMemberHandoverMarker zeroes the 換手 epoch a staff STOP has just made
// meaningless — the worker /stop's two lines, given a name (T-ed79 parity #9).
// Both staff stop verbs call it; the two reasons are different and both are
// worker-verbatim:
//
//   - 下線: a wind-down is a request to finish and come BACK. An explicit 停止
//     says no session follows, so there is nothing left to hand over to. The
//     epoch is not superseded, it is answered.
//   - 強制停止: the session is being cut off. Nothing is being waited for.
//
// 🔴 THE HARM IT REMOVES IS NOT ON THIS ROW, IT IS ON THE NEXT ONE. Neither stop
// verb is what reads refocus_since — decideDown owns a desired-offline member and
// never looks. The reader is the GENERATION AFTER: activate clears stopping_since
// and waking_since and deliberately clears NEITHER refocus_since nor
// stopped_since, so a marker left here survives 下線 → 活化 intact, and decideUp's
// recycle arm then reads "marker present, dump done" and robust-stops the
// brand-new session on its first tick — zero grace, no close-out, for an epoch
// that ended before it was born. armRefocusEpoch already describes this exact
// destructive reader; what was missing was anybody clearing the marker on the
// staff side.
//
// It does NOT touch stopping_since / stopped_since / forced_stop_at: those date
// the stop itself, and forced_stop_at in particular is the durable record that a
// session was cut off, which the next generation is precisely who needs to read
// (dal.go, migrations/00057).
func clearMemberHandoverMarker(m *Member) {
	m.RefocusSince = 0.0
	m.RefocusOp = ""
}

// POST /api/members/{member_id}/deactivate — desired_state=offline + an
// UNCONDITIONAL stopping_since re-stamp (one exception, below).
//
// The re-stamp does NOT restart a countdown: since rc-27d1710174dd the 下線 arm
// runs no clock at all. What the anchor dates is the close-out epoch — which
// reconnect the SSE stop gate admits (api_infra.go), and, paired with
// ForcedStopAt, whether forcedEpochLive reads this stop as a deliberate cut-off.
//
// ⚠️ NOT clearStaleStoppingOnOnline, whatever an older version of this comment
// said: that sweep skips every member whose desired_state is offline, and this
// handler writes offline two statements from here. The sweep only ever sees the
// self-driven arm (report_stopping, which touches no desired_state at all).
func (s *apiServer) HandleDeactivateMemberApiMembersMemberIdDeactivatePost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// 🔴 CANCELLING A WAKE IS NOT A GRACEFUL STOP (T-7526). Read BEFORE the
	// mutation below — stamping stopping_since is itself what ends the waking
	// projection.
	//
	// decideDown's first branch is `if !obs.Online { converged offline }`, and a
	// waking member is BY DEFINITION not online (deriveLiveness projects waking
	// only when !Online). So for the whole waking window the cadence dispatched
	// NOTHING: the process the earlier START already put on the machine booted
	// anyway, connected, went green, and only then — as a now-online member with
	// desired_state=offline — did decideDown even look at it (at the time that
	// armed a 120s grace; today that arm runs no clock at all). Either way the
	// owner's 取消 read as "the button did nothing", which is exactly what it did.
	//
	// There is also nothing to wind down: a member that has not connected has
	// taken no work, so the grace window it cannot enter would buy nothing.
	cancellingWake := PresenceState(*m, nowSecs(), s.hub.IsOnline(m.ID)) ==
		MemberPresenceWaking
	m.DesiredState = DesiredStateOffline
	clearMemberHandoverMarker(m)
	// …UNCONDITIONAL with ONE exception: a stop epoch that a FORCE-stop opened
	// must not be re-stamped into a softer one. The SSE stop gate separates
	// "close-out in flight" (admit the reconnect) from "cut off deliberately"
	// (refuse it) by comparing the two anchors — forced_stop_at >= this epoch's
	// stopping_since means forced — so re-stamping stopping_since to now would
	// move a force-stopped member to the ADMIT side, and the 下線 arm runs no
	// clock, so nothing would collect it afterwards. Found by independent
	// review; reachable through the API/MCP surface (the cockpit offers no 下線
	// button in stopping/stopped, but that is a UI fact, not a gate).
	//
	// The three conditions are each load-bearing. `stopping_since > 0` is what
	// keeps this narrow to a LIVE forced epoch: activate clears stopping_since
	// and waking_since but deliberately KEEPS forced_stop_at (it is the durable
	// record that a past session was cut off), so testing forced_stop_at alone
	// would strip the soft-offboard admission from every member that was ever
	// force-stopped.
	//
	// 🔴 "the stop anchors", which is what this used to say, is one anchor too
	// many: activate does NOT clear stopped_since. That is not a nit — it is the
	// reason a brand-new session can come up ONLINE carrying the PREVIOUS
	// generation's report with no epoch (下線 → 活化), which is exactly the state
	// stampContextHighRecycle's boot_ts test exists to tell apart from a live
	// session's own report. A reader who believes the shorter sentence will
	// conclude that state is unreachable and write the wrong guard.
	//
	// Consequence, deliberate: a forced epoch's anchor stops moving, so this
	// call no longer restarts the grace clock for it. Nothing reads it that way
	// — the 下線 arm returns decisionNone while the soft grace is on, and
	// offboardKindOf answers soft for desired-offline without consulting the
	// anchor's age.
	if !forcedEpochLive(*m) {
		m.StoppingSince = nowSecs()
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if cancellingWake {
		// The same immediate robust STOP force-stop uses. NOT widened to the
		// online case: a live member gets the no-countdown soft window instead,
		// and it is collected by its own report_stopped or by the owner pressing
		// 加速停止 / 強制停止 — never from here.
		s.dispatchRobustStopNow(m.ID)
	}
	// Event-driven reconcile: move the member into `stopping` immediately rather
	// than on the next tick. It arms NO clock — decideDown's online arm returns
	// decisionNone for the whole soft window, so nothing here will ever collect
	// the member; that is the owner's ruling, not a gap. Still run after a cancel
	// — the raw dispatch above does not touch the reconcile store.
	s.reconcileMemberNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/force-stop — STOP intent now (stamps
// stopping_since only if unset) + the immediate robust-STOP dispatch straight
// to the member's warden, bypassing the ~30s cadence
// (handlers.handle_force_stop_member).
//
// There is no grace clock here to bypass: the SERVER arms none on the 下線 arm
// (owner ruling rc-27d1710174dd). Three things end a soft offboard, and this is
// the last of them: the agent's own report_stopped, the deadline the owner opens
// by pressing 加速停止 (that clock is HIS, armed only by the press, which is why
// it does not reopen the ruling), and this endpoint. See the endpoint's
// description in spec/openapi.json, which says the same at length.
func (s *apiServer) HandleForceStopMemberApiMembersMemberIdForceStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.DesiredState = DesiredStateOffline
	clearMemberHandoverMarker(m)
	if m.StoppingSince <= 0.0 {
		m.StoppingSince = nowSecs()
	}
	// The record that this session was cut off (T-a9d6). Force-stop sends no
	// notice — the recipient is about to stop existing, so a sentence meant to
	// change its behaviour has no one to change — and that silence is exactly
	// why the fact has to be written down: everything a killed session leaves
	// behind is indistinguishable from what a session with nothing to say
	// leaves behind. Stamped on the member itself so the NEXT generation and the
	// cockpit can both see it; PutMember persists it forward-only with max(), so
	// a stale snapshot cannot erase the record.
	m.ForcedStopAt = nowSecs()
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.SetMemberForcedStopAt(m.ID, m.ForcedStopAt); err != nil {
		// Best-effort, and deliberately not fatal: the kill below is the point
		// of the call and the member IS being force-stopped. Reporting a
		// failure here would say "force-stop failed" about a member that is
		// about to be killed anyway — the same shape as the dismiss sweep.
		taskLog("force-stop %s: forced_stop_at not recorded: %v", m.ID, err)
	}
	s.dispatchRobustStopNow(m.ID)
	s.writeMemberDTO(w, *m)
}

// acceleratedStopNeedsAnOpenWindDownMsg is the ONE wording of the refusal that
// makes this an escalation rather than a second stop button. It names the rung
// below it, because a 409 that only says "no" leaves the owner guessing which of
// three buttons he was supposed to press first.
const acceleratedStopNeedsAnOpenWindDownMsg = "加速停止 escalates a wind-down that is " +
	"already open — this member has not been asked to stop. Press 停止 (deactivate) " +
	"or 重新聚焦 (refocus) first"

// POST /api/members/{member_id}/accelerated-stop — the MIDDLE rung of the
// owner's escalation 停止 → 加速停止 → 強制停止 (owner 2026-08-21 「可以給我按鈕
// 嗎」＋「停止 → 加速停止 → 強制停止」).
//
// 🔴 IT ESCALATES, IT DOES NOT INITIATE, and the 409 below is the whole
// difference. A member that has not been asked to stop has been told nothing, so
// putting it on a clock would be a deadline it never heard about — the exact
// shape T-ed79 exists to remove. Pressing 停止 (or 重新聚焦) first is what makes
// the member a party to the countdown this endpoint starts.
//
// 🔴 IT DOES NOT REOPEN rc-27d1710174dd (「不要兜底：只有你按強制下線才收它」).
// That ruling is about the SERVER starting a clock on its own; decideDown still
// runs none. This clock exists only because the owner pressed the button, which
// is the same authority force-stop has always had — with a sentence attached
// instead of silence.
//
// BOTH arms are handled, because the ladder has to work wherever the owner
// started it:
//
//   - 下線 (desired_state=offline + stopping_since): re-stamp stopping_since from
//     THIS press and write the cause. decideDown then collects at
//     stopping_since + the grace, and offboardKindOf answers `final` off the same
//     refocus_op, so the sentence quotes exactly that instant.
//   - 換手 (desired online + refocus_since): re-stamp refocus_since and write the
//     cause — the same promotion shape stampContextHighRecycle uses for
//     context_notice → context_high, and re-stamping is load-bearing for the same
//     reason: promoting in place would put the deadline at the ORIGINAL stamp,
//     already in the past, and collect the member on the tick that announced it.
//
// A force-stopped epoch is refused: that session was cut off deliberately and is
// not working a close-out, so a deadline addressed to it has no reader.
func (s *apiServer) HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	// A live session is required for the same reason 重新聚焦 requires one: the
	// notice this write fans travels down the member's own stream, and a clock
	// nobody is listening to is a silent deadline.
	if !s.hub.IsOnline(m.ID) {
		writeError(w, http.StatusConflict,
			"加速停止 requires a live session — there is nothing to accelerate on a "+
				"member that is not connected")
		return
	}
	now := nowSecs()
	switch {
	case m.DesiredState == DesiredStateOffline:
		if m.StoppingSince <= 0.0 || forcedEpochLive(*m) {
			writeError(w, http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
			return
		}
		// The owner's grace runs from HIS press, not from a 停止 he may have
		// pressed hours ago. The other anchors are deliberately untouched: this
		// is a promotion of the close-out already in flight, not a new one, and
		// zeroing stopped_since here would erase an agent's 「我收完了」 and
		// cancel the collection it had already earned.
		m.StoppingSince = now
	case m.RefocusSince > 0.0:
		m.RefocusSince = now
	default:
		writeError(w, http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
		return
	}
	m.RefocusOp = refocusOpAcceleratedStop
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Event-driven, so the clock the owner just started is visible on the next
	// read rather than up to a cadence tick later. It dispatches nothing on this
	// pass — the deadline is in the future by construction.
	s.reconcileMemberNow(m.ID)
	reconcileLog("加速停止: %s on the %s arm (collect at %.0f or on the stopped report)",
		m.ID, m.DesiredState, winddownDeadlineOf(*m, s.reconcileConfigLive()))
	s.writeMemberDTO(w, *m)
}

// POST /api/members/{member_id}/refocus — needs a live session (409 otherwise);
// stamps refocus_since.
//
// The gate is the SSE connection rather than the presence projection, for the
// same reason restart_self's is: a member that has begun closing out projects
// `stopping`, and refusing the owner there would mean 重新聚焦 stops working on
// an agent that is mid-hand-off — the moment he is most likely to press it.
func (s *apiServer) HandleRefocusMemberApiMembersMemberIdRefocusPost(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"refocus requires the member to have a live session and to be wanted "+
				"online (§3.4 #14)")
		return
	}
	armRefocusEpoch(m, refocusOpRefocus, nowSecs())
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// DELETE /api/members/{member_id} — dismiss: a SOFT delete (roster_status=
// removed + desired_state=offline); the audit row survives.
func (s *apiServer) HandleDismissMemberApiMembersMemberIdDelete(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m.RosterStatus = RosterStatusRemoved
	m.DesiredState = DesiredStateOffline
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// A dismissed member leaves the roster for good, so its per-theme avatar
	// choices are dead rows. Drop them here rather than at render time: a
	// dangling selection must never affect another theme, and a member id that
	// somehow returned would otherwise inherit the old face.
	//
	// BEST-EFFORT for the same reason the card sweep below is: putMember has
	// already persisted the dismissal and there is no transaction to roll it
	// back, so a 500 here would report "dismiss failed" for a member that IS
	// dismissed. A leftover row is invisible (the member never renders again)
	// and the settings write path prunes it on the next theme edit.
	if err := s.dal.DeleteMemberThemeAvatars(m.ID); err != nil {
		taskLog("dismiss %s: avatar selection sweep failed: %v", m.ID, err)
	}
	s.invalidateAvatarSelections()
	// T-4166: the asker is gone, so no answer can ever be delivered to its
	// waiting cards — retire them instead of leaving them in the owner's
	// 等我回覆 pane forever (each one pins the cockpit red dot on a member that
	// no longer exists). Same sweep the reassign / task-close seams use.
	//
	// BEST-EFFORT (review B5): putMember above ALREADY persisted the dismissal,
	// and there is no transaction to roll it back. 500-ing here would report
	// "dismiss failed" for a member that IS dismissed. Log instead — matching
	// expireWaitingCardsFromMember's own contract and the worker-dismissal twin.
	if _, err := s.expireWaitingCardsFromMember(m.ID, nowSecs(), requestTrigger(r)); err != nil {
		taskLog("dismiss %s: reply-card sweep failed (cards left waiting): %v", m.ID, err)
	}
	s.writeMemberDTO(w, *m)
}

// ── self-report presence (identity from token, NO member_id target) ──────────

// resolveSelf is the caller's own live member (404 when it has no roster row
// — e.g. the owner's sub has none: self-report is agent-only by construction).
// Unlike resolveMember it does NOT fold kind='outsource' onto errNotFound:
// since the graceful worker handover (T-ea82) an outsource worker walks the
// SAME 下線程序 wake as a member and reports its own presence through these
// self endpoints — only the member_id-target admin surface keeps the pre-fold
// ow- 404.
func (s *apiServer) resolveSelf(r *http.Request) (*Member, error) {
	m, err := s.dal.GetMember(currentActor(r))
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	return m, nil
}

// POST /api/self/waking — the boot report: stamps waking_since and clears ALL
// recycle markers. The reported model is stored separately from the owner's
// launch configuration.
func (s *apiServer) HandleReportWakingApiSelfWakingPost(w http.ResponseWriter, r *http.Request) {
	var body ReportWakingDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		// Worker fold (T-ea82): clear the recycle markers under outsourceMu via
		// the worker funnel — a member-path putMember here would race the tick's
		// read-modify-write and could lose the fold.
		fresh, werr := s.workerReportWaking(m.ID, body.Model, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	m.WakingSince = nowSecs()
	m.RefocusSince = 0.0
	m.RefocusOp = ""
	m.StoppedSince = 0.0
	// 🔴 …but NOT the stop trace of a member the owner has already cancelled
	// (T-7526). Clearing a stale anchor is right for an ORDINARY boot; doing it
	// unconditionally erased the only mark a mid-wake 取消 left behind, so the
	// agent that was already booting when the cancel landed came up painting a
	// fresh green over an intent that is still offline. The intent itself
	// (desired_state) is what says whether this boot is wanted.
	if m.DesiredState == DesiredStateOnline {
		m.StoppingSince = 0.0
	}
	if body.Model != nil {
		m.ActualModel = *body.Model
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/stopping — stamps the caller's stopping_since IF UNSET
// (waking_since deliberately NOT cleared; stopping dominates the projection).
func (s *apiServer) HandleReportStoppingApiSelfStoppingPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		fresh, werr := s.workerReportStopping(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	if m.StoppingSince <= 0.0 {
		m.StoppingSince = nowSecs()
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/stopped — anchors stopped_since ONCE (never re-stamped).
// That FIRST report fires the event-driven collect, so kill→respawn happens
// immediately rather than on the next ~30s tick. It no longer matters what
// opened the offboard: an agent that says it is done is collected either way
// (owner rc-b08d49dc3b03), and desired_state alone decides whether a new
// generation follows.
func (s *apiServer) HandleReportStoppedApiSelfStoppedPost(w http.ResponseWriter, r *http.Request) {
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	if m.Kind == KindOutsource {
		// Worker 收口 (T-ea82): the first stopped-report of a refocus-marked
		// worker runs the collect funnel (kill+respawn NOW) — the member
		// recycle-kill shape, riding the worker's own kill funnel instead of
		// dispatchRobustStopNow.
		fresh, werr := s.workerReportStopped(m.ID, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	// 🔴 A stopped-report is now ALWAYS collected (owner 2026-08-16, card
	// rc-b08d49dc3b03 option ①: 「收掉並重生」).
	//
	// It used to be collected only when something was already collecting it —
	// a refocus epoch was in flight. That was sound while the offboard sequence
	// was shown ONLY to a session being collected: the last step always had a
	// receiver. Then the notice began telling agents to close out on their own
	// (T-c382) and the sequence became a document any session could work
	// (T-c9c0), which opened a path nobody was waiting at the end of: an agent
	// finished its close-out, reported stopped, and NOTHING happened. It stayed
	// alive holding a session it had already declared finished — and the sweep
	// that clears stale stopping anchors erased the evidence, painting it green
	// again (the owner's T-2123 report, and the previous generation of THIS
	// member lived it).
	//
	// desired_state decides what follows, and neither arm needs a special case
	// here: online respawns on the next tick's plain START, offline stays down.
	recycleKill := m.StoppedSince <= 0.0
	if m.StoppedSince <= 0.0 {
		m.StoppedSince = nowSecs()
	}
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Dispatch AFTER putMember so the marker persistence + member-delta fan
	// (→ agent RecycleHook) has already landed before the STOP.
	if recycleKill {
		s.dispatchRobustStopNow(m.ID)
	}
	s.writeMemberDTO(w, *m)
}

// POST /api/self/refocus — restart_self(): the agent's SELF-TRIGGERED recycle
// (identity from token, NO member_id). A self-op is only ever able to restart
// the CALLER, so it is strictly weaker than the admin-gated refocus_member —
// zero privilege-escalation surface. The EFFECT is identical to refocus_member:
// stamp the caller's refocus_since and fan the member delta; the standard §4.5
// recycle orchestration (the agent's own RecycleHook → 下線程序 wake →
// report_stopped → server kill/respawn) carries the rest. Nothing is dispatched
// here (same as refocus_member — no reconcileMemberNow).
//
// Two abuse guards refuse LOUDLY (readable by the agent):
//   - LIVE-SESSION-ONLY (409): a self-restart is meaningless with no live
//     session to recycle. 🔴 The test is the SSE connection, not the presence
//     projection. Those differ for exactly the caller this endpoint exists for:
//     the offboard notice says 「work the sequence below, then call
//     restart_self yourself」, step 1 of that sequence is report_stopping, and
//     that stamps the anchor which makes PresenceState project `stopping`. So
//     an agent doing precisely what it was told was refused — and once a
//     close-out's anchor stopped being swept away every tick (T-2123) the
//     refusal lasted the whole soft window instead of clearing on the next
//     tick. A session holding an open stream has something to recycle; that is
//     the whole question here.
//   - MINIMUM-LIVENESS (429): a call within minSelfRestartSecs of this session
//     connecting is refused — the server-authoritative boot_ts (stamped on the
//     SSE first-connect edge, onFirstConnect) is the anchor; reusing the
//     bootStormTripped loop-guard so a missing boot_ts (server-restart amnesia)
//     FAILS OPEN, never a false 429 on a long-lived session.
func (s *apiServer) HandleRestartSelfApiSelfRefocusPost(w http.ResponseWriter, r *http.Request) {
	var body RestartSelfDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveSelf(r)
	if err != nil {
		writeResolveError(w, err, "member", currentActor(r))
		return
	}
	now := nowSecs()
	if !s.hub.IsOnline(m.ID) || !aRefocusStampWouldReachTheAgent(*m) {
		writeError(w, http.StatusConflict,
			"restart_self requires a live session to recycle, on a member that is "+
				"still wanted online")
		return
	}
	secsSinceBoot := gaugeSecsSinceBoot(s.gauge.Get(m.ID), now)
	if bootStormTripped(secsSinceBoot, minSelfRestartSecs) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
			"restart_self refused: only %.0fs since this session started; the "+
				"minimum-liveness floor is %.0fs (prevents a respawn storm)",
			*secsSinceBoot, minSelfRestartSecs))
		return
	}
	if m.Kind == KindOutsource {
		// Worker fold (T-ea82): stamp the refocus epoch + open the graceful
		// window via the worker funnel (the same shape the owner's refocus
		// button takes) — the standard SOP → stopped-report → collect carries
		// the rest.
		fresh, werr := s.workerRestartSelf(m.ID, now, requestTrigger(r))
		if werr != nil {
			writeResolveError(w, werr, "member", currentActor(r))
			return
		}
		if reason := trimmedOrEmpty(body.Reason); reason != "" {
			reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
		} else {
			reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
		}
		s.writeMemberDTO(w, *fresh)
		return
	}
	armRefocusEpoch(m, refocusOpRestartSelf, now)
	if err := s.putMember(*m, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	// Distinguish a self-restart from an owner refocus on the operator log
	// (both stamp refocus_since identically; the reason is the differentiator).
	if reason := trimmedOrEmpty(body.Reason); reason != "" {
		reconcileLog("recycle: %s self-restart (restart_self); reason: %s", m.ID, reason)
	} else {
		reconcileLog("recycle: %s self-restart (restart_self)", m.ID)
	}
	s.writeMemberDTO(w, *m)
}
