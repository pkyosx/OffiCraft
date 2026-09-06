package main

// api_scheduled_messages.go — T-f059 定期訊息: the owner-facing CRUD over a
// member's scheduled_message rows. The clock-driven twin of api_webhooks.go's
// management face, verb for verb; the firing itself lives in
// scheduled_message.go and the slot arithmetic in schedule_slot.go.
//
// 🔴 The recipient is resolved with resolveChatRecipient, NOT resolveMember.
// A scheduled message IS a chat message, so it takes chat's recipient rule:
// staff and outsource workers alike can be scheduled to. (resolveMember would
// serve an ow- row too now that scope is a parameter — but only if every call
// site here remembered to pass anyMember, and chat's rule is the one that says
// what a recipient IS.)

import (
	"net/http"
	"time"
)

// GET /api/members/{member_id}/scheduled-messages — the member's schedules,
// oldest→newest.
func (s *apiServer) HandleListScheduledMessagesApiMembersMemberIdScheduledMessagesGet(w http.ResponseWriter, r *http.Request, memberId string) {
	recipient, err := s.resolveChatRecipient(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	rows, err := s.dal.ListScheduledMessagesByMember(recipient)
	if err != nil {
		internalError(w, err)
		return
	}
	out := []scheduledMessageDTO{}
	for _, m := range rows {
		out = append(out, newScheduledMessageDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/members/{member_id}/scheduled-messages — create. body/cadence/
// timezone are required unconditionally (spec): an omitted timezone would
// sooner or later be read as "the server's zone", which is the ambiguity this
// feature exists to remove.
//
// 🔴 hour/minute are required CONDITIONALLY, not unconditionally, and the
// conditional half is enforced below rather than here. They left the
// unconditional list in T-49e7 so a `custom` schedule need not send two values
// it never reads; the price is that a missing hour became representable, so
// ValidateScheduledMessageWallClockPresence refuses it for every other cadence.
// That rule is not expressible in the OpenAPI schema — it lives only in the
// field descriptions — so no generated client will catch it and this server is
// the only thing standing between an omitted hour and a silent midnight.
func (s *apiServer) HandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body ScheduledMessageCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "body", "cadence", "timezone") {
		return
	}
	if err := ValidateScheduledMessageWallClockPresence(
		string(body.Cadence), body.Hour != nil, body.Minute != nil); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	recipient, err := s.resolveChatRecipient(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m := ScheduledMessage{
		ID:       "sch-" + newHexID(12),
		MemberID: recipient,
		Label:    strOrEmpty(body.Label),
		Body:     body.Body,
		Cadence:  string(body.Cadence),
		// Both day fields carry a default because the cadence is editable: a
		// daily schedule PATCHed to weekly later must have a defined day.
		DayOfWeek:  intOr(body.DayOfWeek, 0),
		DayOfMonth: intOr(body.DayOfMonth, 1),
		// 0 here is reached ONLY by a `custom` schedule, which never reads these
		// two — every other cadence had to state them (checked above).
		Hour:   intOr(body.Hour, 0),
		Minute: intOr(body.Minute, 0),
		// Stored for every cadence, canonicalised by the write seam. Only
		// `custom` reads them, and only `custom` has them validated — a
		// non-custom row that carries a set it does not read cannot defer a
		// fault, because a PATCH that flips the cadence to `custom` judges the
		// WHOLE assembled row before it lands.
		// custom_months is the ONE set resolved rather than copied: an omitted
		// field on a `custom` create means all twelve months. Stored is nil
		// here because a create has no prior row to leave alone.
		CustomMonths:  resolveCustomMonths(string(body.Cadence), nil, body.CustomMonths),
		CustomDays:    intSliceOrNil(body.CustomDays),
		CustomHours:   intSliceOrNil(body.CustomHours),
		CustomMinutes: intSliceOrNil(body.CustomMinutes),
		Timezone:      trimString(body.Timezone),
		Status:        ScheduledMessageStatusEnabled,
		CreatedTS:     nowSecs(),
	}
	if !s.validateScheduledMessage(w, m) {
		return
	}
	// The delivery cursor starts AT the slot current right now, so a schedule
	// created at 10:00 for daily 09:00 does not fire today. Seeding it is what
	// makes "a new schedule never fires immediately" structural rather than a
	// property of when the first tick happens to land.
	m.LastFiredSlot = currentSlotKey(m, time.Unix(int64(m.CreatedTS), 0))
	if err := s.dal.PutScheduledMessage(m); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduledMessageReceiptOf(m))
}

// resolveCustomMonths decides what `custom_months` a request means, and it is
// the ONLY place in the server where an ABSENT set carries a meaning.
//
// 🔴 Why the rule lives here and not in the validator: nil and [] are the same
// value by the time a request becomes a ScheduledMessage, and they are two
// DIFFERENT requests. "The caller never mentioned months" is a client that
// predates the field, and its schedules already meant every month — refusing it
// would break every one of them. "The caller sent []" is someone asking for a
// schedule that never fires, which is the 422 migrations/00052 argues for. The
// distinction survives exactly as long as the *[]int does, so it is made here,
// and everything downstream sees a row that lists its months.
//
// The three inputs answer three different questions, in this order:
//
//	sent != nil     the caller stated something — honour it VERBATIM, including
//	                the empty slice, which validation then refuses. Never
//	                "helpfully" substitute all twelve for it.
//	not custom      no months are read at all; leave whatever is stored, which
//	                on a create is nothing and on a PATCH is the parked set a
//	                row switched away from `custom` keeps.
//	stored non-empty  an existing choice the caller did not touch. Absent means
//	                unchanged, the ordinary PATCH rule.
//
// Only when all three fall through — a `custom` row that must exist, with no
// months from the request and none on file — does the all-twelve default apply.
// That is a create, or a PATCH that switches a never-custom row to `custom`.
func resolveCustomMonths(cadence string, stored []int, sent *[]int) []int {
	if sent != nil {
		return *sent
	}
	if cadence != ScheduledMessageCadenceCustom {
		return stored
	}
	if len(stored) > 0 {
		return stored
	}
	return allCustomMonths()
}

// PATCH /api/members/{member_id}/scheduled-messages/{schedule_id} — partial
// edit, including the enable/disable toggle. id and member_id are immutable.
func (s *apiServer) HandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch(w http.ResponseWriter, r *http.Request, memberId, scheduleId string) {
	var body ScheduledMessageUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveScheduledMessage(memberId, scheduleId)
	if err != nil {
		writeResolveError(w, err, "scheduled message", scheduleId)
		return
	}
	// Whether the schedule was RE-AIMED, which is a different question from
	// whether it was edited: changing the label or the body leaves the slots
	// where they were, so the cursor must stay put too.
	//
	// 🔴 Compared by VALUE against the row as it stands, not by which fields the
	// caller happened to send. Re-aiming on mere presence means a caller that
	// PATCHes the whole form back moves the cursor to now on every save even when
	// nothing about the timing changed. Land one of those in the window between a
	// slot elapsing and the next tick (up to a minute) and that delivery is
	// swallowed permanently, with no error, no log line and a card that looks
	// entirely normal.
	//
	// This is no longer hypothetical: the card's per-row editor sends the whole
	// form on every save, so this comparison is the only thing standing between a
	// no-op save and a swallowed delivery. scheduled_message_patch_realign_test.go
	// pins both directions (unchanged form ⇒ cursor untouched; changed timing ⇒
	// re-aimed).
	//
	// 🔴 All four sets are compared in CANONICAL form on both sides, so
	// [20,0,40] and [0,20,40] are the same choice and move nothing. Comparing
	// them as sent would make the cockpit's whole-form save re-aim on every
	// press purely because a checkbox order differed — and a re-aim inside the
	// window between a slot elapsing and the next tick swallows that delivery
	// permanently, silently, on a card that looks entirely normal.
	// 🔴 A field the resulting cadence does NOT read cannot move the cursor,
	// because it cannot move a single slot. `custom_days` is documented as
	// "ignored by every other cadence", and a caller that PATCHes the whole form
	// back — the generated client, any "send every field" integration — sends all
	// four sets on a `daily` row. Comparing them anyway made that no-op save
	// re-aim, which inside the window between a slot elapsing and the next tick
	// swallows that delivery permanently, silently, on a card that looks entirely
	// normal: exactly the failure the canonical-form comparison above already
	// closes from the other direction (order) but left open from this one
	// (fields the cadence never reads).
	cadenceAfter := m.Cadence
	if body.Cadence != nil {
		cadenceAfter = string(*body.Cadence)
	}
	reads := func(field string) bool {
		return scheduledMessageCadenceReads(cadenceAfter, field)
	}
	reAimed := (body.Cadence != nil && string(*body.Cadence) != m.Cadence) ||
		(reads("day_of_week") && body.DayOfWeek != nil && *body.DayOfWeek != m.DayOfWeek) ||
		(reads("day_of_month") && body.DayOfMonth != nil && *body.DayOfMonth != m.DayOfMonth) ||
		(reads("hour") && body.Hour != nil && *body.Hour != m.Hour) ||
		(reads("minute") && body.Minute != nil && *body.Minute != m.Minute) ||
		(reads("custom_months") && body.CustomMonths != nil && canonicalIntSet(*body.CustomMonths) != canonicalIntSet(m.CustomMonths)) ||
		(reads("custom_days") && body.CustomDays != nil && canonicalIntSet(*body.CustomDays) != canonicalIntSet(m.CustomDays)) ||
		(reads("custom_hours") && body.CustomHours != nil && canonicalIntSet(*body.CustomHours) != canonicalIntSet(m.CustomHours)) ||
		(reads("custom_minutes") && body.CustomMinutes != nil && canonicalIntSet(*body.CustomMinutes) != canonicalIntSet(m.CustomMinutes)) ||
		(body.Timezone != nil && trimString(*body.Timezone) != m.Timezone)
	// Whether this edit leaves `custom` behind, asked BEFORE the patch is
	// applied — see the wall-clock guard further down for why it matters.
	wasCustom := m.Cadence == ScheduledMessageCadenceCustom
	if body.Label != nil {
		m.Label = *body.Label
	}
	if body.Body != nil {
		m.Body = *body.Body
	}
	if body.Cadence != nil {
		m.Cadence = string(*body.Cadence)
	}
	if body.DayOfWeek != nil {
		m.DayOfWeek = *body.DayOfWeek
	}
	if body.DayOfMonth != nil {
		m.DayOfMonth = *body.DayOfMonth
	}
	if body.Hour != nil {
		m.Hour = *body.Hour
	}
	if body.Minute != nil {
		m.Minute = *body.Minute
	}
	// 🔴 Each set is written ONLY when supplied, and switching AWAY from
	// `custom` leaves the stored sets in place rather than clearing them —
	// the PATCH `cadence` description in spec/openapi.json states exactly
	// that ("switching AWAY from `custom` leaves the stored sets in place,
	// unread, so switching back does not lose the choice").
	//
	// ⚠️ THAT SENTENCE ONCE CONTRADICTED THE RESPONSE SCHEMA IN THE SAME FILE,
	// which said the three fields are "Empty for every other cadence" — two
	// sentences in one reviewed contract that could not both be true, and
	// neither review nor the owner's read-through caught it. The owner settled
	// it on card rc-68c581070e55 (2026-08-11): the sets are KEPT, merely unread.
	// The response-schema sentences were rewritten to say so; this code did not
	// move, because it already followed the PATCH clause — the one that
	// describes THIS verb. Nothing here is open.
	// 🔴 Months go through the resolver rather than the `!= nil` copy the other
	// three use, and it runs AFTER m.Cadence has been patched — the question it
	// asks is about the cadence this row will HAVE, not the one it had. That is
	// what lets a PATCH switch a never-custom row to `custom` without naming
	// months: the old client that does exactly that still lands a schedule, and
	// it lands the all-twelve meaning it has always had. On a row that already
	// carries months, absent still means unchanged.
	m.CustomMonths = resolveCustomMonths(m.Cadence, m.CustomMonths, body.CustomMonths)
	if body.CustomDays != nil {
		m.CustomDays = *body.CustomDays
	}
	if body.CustomHours != nil {
		m.CustomHours = *body.CustomHours
	}
	if body.CustomMinutes != nil {
		m.CustomMinutes = *body.CustomMinutes
	}
	if body.Timezone != nil {
		m.Timezone = trimString(*body.Timezone)
	}
	if body.Status != nil {
		if !ValidScheduledMessageStatus(string(*body.Status)) {
			writeError(w, http.StatusUnprocessableEntity,
				"status must be one of ['enabled' 'disabled']; got '"+string(*body.Status)+"'")
			return
		}
		m.Status = string(*body.Status)
	}
	// 🔴 THIS GUARD EXISTS BECAUSE A CUSTOM ROW'S hour/minute WERE NEVER CHOSEN.
	// They are the fields `custom` does not read, so they hold their 0/0
	// defaults; inheriting them on the way out would hand back a schedule that
	// fires at midnight — a time nobody picked — and it would look exactly like
	// a schedule that was asked to run at midnight. That is the silent-zero this
	// feature refuses everywhere else, arriving through a cadence toggle instead
	// of through an omitted field. Only this transition is affected: a daily row
	// edited to weekly keeps the hour it already stated.
	// (Stricter than the create-side rule by design — do not "simplify" it away.)
	if wasCustom && m.Cadence != ScheduledMessageCadenceCustom {
		if err := ValidateScheduledMessageWallClockPresence(
			m.Cadence, body.Hour != nil, body.Minute != nil); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if !s.validateScheduledMessage(w, *m) {
		return
	}
	// 🔴 The edit writes the owner's columns ONLY. It does not carry the cursor
	// back, because everything above this line is a read-modify-write over a
	// snapshot taken before the request was even parsed: a tick can deliver a
	// slot in that gap, and a whole-row re-put would roll its advance back and
	// make the next tick send the same slot again. The monotonic fire test cannot
	// save that — the cursor itself would have gone backwards. See
	// UpdateScheduledMessageSettings.
	if err := s.dal.UpdateScheduledMessageSettings(*m); err != nil {
		internalError(w, err)
		return
	}
	if reAimed {
		// Re-aiming moves the cursor to the slot current NOW, so the edit never
		// fires the slot it crossed: moving a daily schedule from 09:00 to 08:00
		// at noon must not deliver today's 08:00 retroactively. This is the one
		// write to the cursor an edit may make, and it is stated, not inherited.
		if err := s.dal.AimScheduledMessageCursor(m.ID, currentSlotKey(*m, time.Now())); err != nil {
			internalError(w, err)
			return
		}
	}
	// Re-read rather than serialise the snapshot: the cursor fields on the wire
	// must be the ones actually in the row, including an advance this request
	// deliberately did not overwrite.
	//
	// 🔴 "Gone" and "storage broke" are two answers, not one. The UPDATE above
	// matches zero rows without erroring, so a DELETE landing between this
	// request's read and its write leaves fresh == nil with err == nil — and
	// folding those together fed internalError a nil error, whose first act is to
	// call err.Error(). That panicked: the caller got a dropped connection (EOF),
	// not a response. The row being gone is a 404, which is the same answer the
	// read at the top of this handler gives for a schedule that was never there.
	fresh, err := s.dal.GetScheduledMessage(m.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if fresh == nil {
		writeResolveError(w, errNotFound, "scheduled message", scheduleId)
		return
	}
	writeJSON(w, http.StatusOK, scheduledMessageReceiptOf(*fresh))
}

// DELETE /api/members/{member_id}/scheduled-messages/{schedule_id} —
// permanent removal (status=disabled is the reversible one).
func (s *apiServer) HandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete(w http.ResponseWriter, r *http.Request, memberId, scheduleId string) {
	m, err := s.resolveScheduledMessage(memberId, scheduleId)
	if err != nil {
		writeResolveError(w, err, "scheduled message", scheduleId)
		return
	}
	if err := s.dal.DeleteScheduledMessage(m.ID); err != nil {
		internalError(w, err)
		return
	}
	// T-91: a delete receipt, not the row that no longer exists. The route 404s
	// when the member or the schedule is absent, so `deleted` is never false
	// here — it is on the shape so the answer SAYS what the write did rather
	// than leaving an empty 200 to be interpreted.
	writeJSON(w, http.StatusOK, scheduledMessageDeleteReceiptDTO{
		ID: m.ID, MemberID: m.MemberID, Deleted: true,
	})
}

// resolveScheduledMessage returns the schedule addressed by (member,
// schedule_id), folding an absent member, an absent schedule, OR a schedule
// belonging to a DIFFERENT member onto errNotFound. That last case matters: the
// id alone would otherwise let one member's path edit another member's row.
func (s *apiServer) resolveScheduledMessage(memberID, scheduleID string) (*ScheduledMessage, error) {
	recipient, err := s.resolveChatRecipient(memberID)
	if err != nil {
		return nil, err
	}
	m, err := s.dal.GetScheduledMessage(scheduleID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.MemberID != recipient {
		return nil, errNotFound
	}
	return m, nil
}

// validateScheduledMessage applies the domain invariants to a fully assembled
// row and writes the 422 face on the first failure. Applied identically on
// create and on PATCH — a partial edit assembles a WHOLE row before it is
// judged, so no combination of individually-legal edits can compose an illegal
// schedule.
func (s *apiServer) validateScheduledMessage(w http.ResponseWriter, m ScheduledMessage) bool {
	if !ValidScheduledMessageCadence(m.Cadence) {
		writeError(w, http.StatusUnprocessableEntity,
			"cadence must be one of "+scheduledMessageCadenceList()+"; got '"+m.Cadence+"'")
		return false
	}
	if err := ValidateScheduledMessageBody(m.Body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	if err := ValidateScheduledMessageSlotFields(m.Hour, m.Minute, m.DayOfWeek, m.DayOfMonth); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	// The four sets are judged only for `custom`, the only cadence that reads
	// them. Because a PATCH assembles the WHOLE row before it is judged,
	// switching a schedule TO `custom` without sets — in the request or already
	// stored — is refused here rather than landing a cadence with no times.
	// custom_months reaches this point already resolved (see
	// resolveCustomMonths), so an empty months set here can only mean the
	// caller sent one explicitly.
	if m.Cadence == ScheduledMessageCadenceCustom {
		if err := ValidateScheduledMessageCustomSets(m.CustomMonths, m.CustomDays, m.CustomHours, m.CustomMinutes); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return false
		}
	}
	// 🔴 A timezone the tz database cannot resolve is refused HERE, at the write,
	// never softened into UTC downstream — a schedule that runs at the wrong
	// hour looks exactly like one that runs correctly.
	if err := ValidateScheduledMessageTimezone(m.Timezone); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	return true
}
