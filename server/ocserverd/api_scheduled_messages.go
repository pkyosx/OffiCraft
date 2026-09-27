package main

// Owner-facing CRUD over scheduled_message rows; the firing lives in
// scheduled_message.go and the slot arithmetic in schedule_slot.go.
//
// 🔴 The recipient is resolved with resolveChatRecipient, NOT resolveMember: a
// scheduled message IS a chat message, so chat's recipient rule applies.

import (
	"net/http"
	"time"
)

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

// POST — timezone is required unconditionally: an omitted one would sooner or
// later be read as "the server's zone". hour/minute are required only for
// non-custom cadences, a rule the OpenAPI schema cannot express, so
// ValidateScheduledMessageWallClockPresence is the only thing standing between
// an omitted hour and a silent midnight.
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
		// Defaulted because the cadence is editable: a daily schedule PATCHed to
		// weekly later must have a defined day.
		DayOfWeek:  intOr(body.DayOfWeek, 0),
		DayOfMonth: intOr(body.DayOfMonth, 1),

		Hour:   intOr(body.Hour, 0),
		Minute: intOr(body.Minute, 0),

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
	// The cursor starts AT the current slot, so a new schedule never fires
	// immediately (created 10:00 for daily 09:00 ⇒ not today).
	m.LastFiredSlot = currentSlotKey(m, time.Unix(int64(m.CreatedTS), 0))
	if err := s.dal.PutScheduledMessage(m); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduledMessageReceiptOf(m))
}

// resolveCustomMonths is the ONLY place where an ABSENT set carries meaning.
// Absent = a client that predates the field, whose schedules already meant
// every month; an explicit [] asks for a schedule that never fires and must
// reach validation VERBATIM (422) — never substitute all twelve for it. nil and
// [] only differ while this is still a *[]int, so the decision is made here.
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
	if _, err := applyScheduledMessagePatch(m, body); err != nil {
		writeTxError(w, err)
		return
	}
	// The patch is applied again to the row as it stands in the transaction that
	// writes it: re-aimed is judged against that row, and the columns this
	// request did not send keep what is stored there.
	var fresh *ScheduledMessage
	err = s.dal.inTx(func(tx *writeTx) error {
		cur, err := resolveScheduledMessageOn(tx, memberId, scheduleId)
		if err != nil {
			return err
		}
		reAimed, err := applyScheduledMessagePatch(cur, body)
		if err != nil {
			return err
		}
		// 🔴 Writes the owner's columns ONLY, never the cursor: a tick can deliver a
		// slot while this request works on its snapshot, and a whole-row re-put
		// would roll the cursor back and send that slot again.
		if err := updateScheduledMessageSettingsOn(tx, *cur); err != nil {
			return err
		}
		if reAimed {
			// To the slot current NOW: an edit never fires the slot it crossed.
			if err := aimScheduledMessageCursorOn(tx, cur.ID, currentSlotKey(*cur, time.Now())); err != nil {
				return err
			}
		}
		// Re-read so the cursor fields on the wire are the row's.
		fresh, err = getScheduledMessageOn(tx, cur.ID)
		if err == nil && fresh == nil {
			err = errNotFound
		}
		return err
	})
	if err != nil {
		writeResolveTxError(w, err, "scheduled message", scheduleId)
		return
	}
	writeJSON(w, http.StatusOK, scheduledMessageReceiptOf(*fresh))
}

// applyScheduledMessagePatch patches m in place and reports whether the edit
// re-aims the schedule; a 422 comes back as a txRefusal.
func applyScheduledMessagePatch(m *ScheduledMessage, body ScheduledMessageUpdateDTO) (bool, error) {
	// 🔴 Re-aimed is judged by VALUE against the stored row — sets in canonical
	// form, and only fields the resulting cadence reads — never by which fields
	// were sent. The cockpit (and any generated client) PATCHes the whole form
	// back on every save; a spurious re-aim landing between a slot elapsing and
	// the next tick swallows that delivery permanently and silently.
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
	// Switching AWAY from `custom` keeps the stored sets, unread (owner ruling
	// rc-68c581070e55).
	// Months resolve AFTER m.Cadence is patched: the question is about the
	// cadence this row will HAVE.
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
			return false, refuseInTx(http.StatusUnprocessableEntity,
				"status must be one of ['enabled' 'disabled']; got '"+string(*body.Status)+"'")
		}
		m.Status = string(*body.Status)
	}
	// 🔴 Leaving `custom` must state hour/minute: a custom row's 0/0 were never
	// chosen, and inheriting them would be a silent midnight. Stricter than the
	// create-side rule by design — do not "simplify" it away.
	if wasCustom && m.Cadence != ScheduledMessageCadenceCustom {
		if err := ValidateScheduledMessageWallClockPresence(
			m.Cadence, body.Hour != nil, body.Minute != nil); err != nil {
			return false, refuseInTx(http.StatusUnprocessableEntity, err.Error())
		}
	}
	return reAimed, scheduledMessageRefusal(*m)
}

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
	writeJSON(w, http.StatusOK, scheduledMessageDeleteReceiptDTO{
		ID: m.ID, MemberID: m.MemberID, Deleted: true,
	})
}

func (s *apiServer) resolveScheduledMessage(memberID, scheduleID string) (*ScheduledMessage, error) {
	return resolveScheduledMessageOn(s.dal.rdb, memberID, scheduleID)
}

func resolveScheduledMessageOn(q sqlRowQuerier, memberID, scheduleID string) (*ScheduledMessage, error) {
	recipient, err := resolveChatRecipientOn(q, memberID)
	if err != nil {
		return nil, err
	}
	m, err := getScheduledMessageOn(q, scheduleID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.MemberID != recipient {
		return nil, errNotFound
	}
	return m, nil
}

func (s *apiServer) validateScheduledMessage(w http.ResponseWriter, m ScheduledMessage) bool {
	if err := scheduledMessageRefusal(m); err != nil {
		writeTxError(w, err)
		return false
	}
	return true
}

func scheduledMessageRefusal(m ScheduledMessage) error {
	if !ValidScheduledMessageCadence(m.Cadence) {
		return refuseInTx(http.StatusUnprocessableEntity,
			"cadence must be one of "+scheduledMessageCadenceList()+"; got '"+m.Cadence+"'")
	}
	if err := ValidateScheduledMessageBody(m.Body); err != nil {
		return refuseInTx(http.StatusUnprocessableEntity, err.Error())
	}
	if err := ValidateScheduledMessageSlotFields(m.Hour, m.Minute, m.DayOfWeek, m.DayOfMonth); err != nil {
		return refuseInTx(http.StatusUnprocessableEntity, err.Error())
	}
	if m.Cadence == ScheduledMessageCadenceCustom {
		if err := ValidateScheduledMessageCustomSets(m.CustomMonths, m.CustomDays, m.CustomHours, m.CustomMinutes); err != nil {
			return refuseInTx(http.StatusUnprocessableEntity, err.Error())
		}
	}
	// 🔴 Refused here, never softened into UTC downstream: a schedule that runs
	// at the wrong hour looks exactly like one that runs correctly.
	if err := ValidateScheduledMessageTimezone(m.Timezone); err != nil {
		return refuseInTx(http.StatusUnprocessableEntity, err.Error())
	}
	return nil
}
