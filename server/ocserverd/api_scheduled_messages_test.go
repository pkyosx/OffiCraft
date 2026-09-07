// Skeleton generated from server/ocserverd/api_scheduled_messages.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHandleListScheduledMessagesApiMembersMemberIdScheduledMessagesGet(t *testing.T) {
	t.Run("a well-formed GET /api/members/{member_id}/scheduled-messages answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/scheduled-messages request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/members/{member_id}/scheduled-messages reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/scheduled-messages request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost(t *testing.T) {
	t.Run("a well-formed POST /api/members/{member_id}/scheduled-messages answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/scheduled-messages request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/members/{member_id}/scheduled-messages reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/scheduled-messages request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestResolveCustomMonths(t *testing.T) {
	t.Skip("TODO: resolveCustomMonths decides what `custom_months` a request means, and it is the ONLY place in the server where an ABSENT set carries a meaning.")
}

func TestHandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch(t *testing.T) {
	t.Run("a well-formed PATCH /api/members/{member_id}/scheduled-messages/{schedule_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/members/{member_id}/scheduled-messages/{schedule_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to PATCH /api/members/{member_id}/scheduled-messages/{schedule_id} reaches this handler with member_id, schedule_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/members/{member_id}/scheduled-messages/{schedule_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete(t *testing.T) {
	t.Run("a well-formed DELETE /api/members/{member_id}/scheduled-messages/{schedule_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a DELETE /api/members/{member_id}/scheduled-messages/{schedule_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to DELETE /api/members/{member_id}/scheduled-messages/{schedule_id} reaches this handler with member_id, schedule_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a DELETE /api/members/{member_id}/scheduled-messages/{schedule_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestResolveScheduledMessage(t *testing.T) {
	t.Skip("TODO: resolveScheduledMessage returns the schedule addressed by (member, schedule_id), folding an absent member, an absent schedule, OR a schedule belonging to a DIFFERENT member onto errNotFound.")
}

func TestValidateScheduledMessage(t *testing.T) {
	t.Skip("TODO: validateScheduledMessage applies the domain invariants to a fully assembled row and writes the 422 face on the first failure.")
}
