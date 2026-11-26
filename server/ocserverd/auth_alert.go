package main

// A login refusal where the password was CORRECT and the second factor was
// not means the password is out; the answer is changing it, so the server tells
// the seeded assistant, who can nag the owner.
//
// 🔴 Two security constraints:
//	1. IT MUST NOT BE FELT FROM OUTSIDE: /api/login makes 「密碼對」 and
//	   「密碼錯」 indistinguishable (throttle.go), so the caller's path is only a
//	   mutex, an increment and a comparison; the write runs on a goroutine.
//	2. IT MUST NOT BE A MEGAPHONE: the trigger is attacker-controlled, so it is
//	   one message per authAlertInterval carrying the folded attempt count.
//
// ⚠️ It is a mailbox message, NOT a wake: a wake would let an unauthenticated
// caller start an agent process. This is a nag, not an alarm.

import (
	"strconv"
	"time"
)

// Set by what the reader can act on (change the password), not by the attack
// rate; it is also the gap that keeps the folded count meaningful.
const authAlertInterval = 15 * time.Minute

// ⚠️ Attempts in a suppressed window are reported by the NEXT alert, so the
// tail of a burst that stops is never reported — deliberate: the alternative is
// a timer that keeps firing after an attack ends.
func (s *apiServer) noteFactorRefusedAfterCorrectPassword(now time.Time) {
	s.authAlertMu.Lock()
	s.authAlertPending++
	if !s.authAlertLastAt.IsZero() && now.Sub(s.authAlertLastAt) < authAlertInterval {
		s.authAlertMu.Unlock()
		return
	}
	count := s.authAlertPending
	s.authAlertPending = 0
	s.authAlertLastAt = now
	s.authAlertMu.Unlock()

	// 🔴 `go`, NOT a direct call — constraint 1. The window is already stamped,
	// so a flood cannot queue a second goroutine behind a slow delivery.
	go surviveLockInTx("password-exposed alert", func() { s.dispatchAuthAlert(count) })
}

// dispatchAuthAlert exists so a test can install a blocking deliverer and prove
// the caller never waits. Production never sets the field.
func (s *apiServer) dispatchAuthAlert(count int) {
	if s.authAlertDeliver != nil {
		s.authAlertDeliver(count)
		return
	}
	s.deliverPasswordExposedAlert(count)
}

func (s *apiServer) deliverPasswordExposedAlert(count int) {
	recipient, err := s.resolveChatRecipient(seedMiraID)
	if err != nil {
		outsourceLog("auth-alert: no assistant to warn (%s): %v — the owner will "+
			"NOT be told that their password was accepted with a wrong second factor",
			seedMiraID, err)
		return
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    wireSystemSender,
		Recipient: recipient,
		Body:      passwordExposedAlertBody(count),
		TS:        nowSecs(),
		Meta: map[string]any{
			"auth_alert": map[string]any{
				"kind":     "password_ok_factor_refused",
				"attempts": count,
			},
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		outsourceLog("auth-alert: durable message to %s failed (the owner will NOT "+
			"be told about %d accepted-password login(s) with a wrong code): %v",
			recipient, count, err)
		return
	}
	// Same payload and audience as every chat delta (spec/sse.md §2.2).
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)
}

func passwordExposedAlertBody(count int) string {
	attempts := "1 time"
	if count != 1 {
		attempts = strconv.Itoa(count) + " times"
	}
	return "🔴 Sign-in alert: someone signed in to this server with the **correct password**, but the second-step verification code was wrong" +
		" (" + attempts + " in the recent period).\n\n" +
		"This means the password itself is in someone else's hands — the second factor is stopping them for now, but the password will not become safe again on its own. " +
		"It is also possible that the owner changed phones and has not re-linked the authenticator yet; the two cases look exactly the same, so ask first and do not jump to a conclusion.\n\n" +
		"Please remind the owner: confirm whether these attempts were him, and if they were not, change the password right away" +
		" (personal menu › Change password). Until he changes it, this alert appears again at most once every " +
		strconv.Itoa(int(authAlertInterval.Minutes())) + " minutes, " +
		"with the count accumulating in the same message, so a growing number means it is still happening."
}
