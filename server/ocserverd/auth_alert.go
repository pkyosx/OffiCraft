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
	go s.dispatchAuthAlert(count)
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
	attempts := "1 次"
	if count != 1 {
		attempts = strconv.Itoa(count) + " 次"
	}
	return "🔴 登入警訊：有人用**正確的密碼**登入這台伺服器，但第二階段驗證碼是錯的" +
		"（最近這段時間共 " + attempts + "）。\n\n" +
		"這代表密碼本身已經在別人手上——第二因素目前擋住了他，但密碼不會自己變回安全。" +
		"也有可能是老闆自己換了手機、驗證器還沒重新綁定，兩種情況看起來一模一樣，所以請先問，不要直接下結論。\n\n" +
		"請提醒老闆：確認這幾次是不是他本人，如果不是，請立刻更換密碼" +
		"（個人選單 › 更改密碼）。在他換掉之前，這則提醒每 " +
		strconv.Itoa(int(authAlertInterval.Minutes())) + " 分鐘最多再出現一次，" +
		"次數會累加在同一則裡，所以看到數字變大就是還在持續。"
}
