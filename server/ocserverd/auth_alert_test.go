// Skeleton generated from server/ocserverd/auth_alert.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNoteFactorRefusedAfterCorrectPassword(t *testing.T) {
	deliveries := make(chan int, 2)
	api := &apiServer{authAlertDeliver: func(count int) { deliveries <- count }}
	first := time.Unix(1000, 0)

	api.noteFactorRefusedAfterCorrectPassword(first)
	select {
	case got := <-deliveries:
		if got != 1 {
			t.Fatalf("first alert count = %d, want 1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first alert was not dispatched")
	}

	api.noteFactorRefusedAfterCorrectPassword(first.Add(time.Second))
	select {
	case got := <-deliveries:
		t.Fatalf("suppressed alert count = %d, want no alert inside the interval", got)
	default:
	}

	api.noteFactorRefusedAfterCorrectPassword(first.Add(authAlertInterval))
	select {
	case got := <-deliveries:
		if got != 2 {
			t.Fatalf("next alert count = %d, want 2", got)
		}
	case <-time.After(time.Second):
		t.Fatal("next alert was not dispatched")
	}
}

func TestDispatchAuthAlert(t *testing.T) {
	deliveries := make(chan int, 1)
	api := &apiServer{authAlertDeliver: func(count int) { deliveries <- count }}

	api.dispatchAuthAlert(7)
	select {
	case got := <-deliveries:
		if got != 7 {
			t.Fatalf("delivered count = %d, want 7", got)
		}
	case <-time.After(time.Second):
		t.Fatal("custom alert delivery was not called")
	}
}

func TestDeliverPasswordExposedAlert(t *testing.T) {
	t.Run("an alert writes the assistant a system message and fans the chat delta to her and the owner", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		assistant := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")

		api.deliverPasswordExposedAlert(3)

		frame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":   apiAnyString,
					"from": "system",
					"to":   "mira",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "server",
		}
		dashboard.wantFrames(frame)
		assistant.wantFrames(frame)
		bystander.wantFrames()

		rec := apiRequest(t, h, "GET", "/api/chat?with=mira&limit=5", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var chat any
		if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "chat", chat, map[string]any{
			"messages": []any{map[string]any{
				"id":                 apiAnyString,
				"from":               "system",
				"from_name":          "",
				"to":                 "mira",
				"to_name":            "",
				"body":               "🔴 登入警訊：有人用**正確的密碼**登入這台伺服器，但第二階段驗證碼是錯的（最近這段時間共 3 次）。\n\n這代表密碼本身已經在別人手上——第二因素目前擋住了他，但密碼不會自己變回安全。也有可能是老闆自己換了手機、驗證器還沒重新綁定，兩種情況看起來一模一樣，所以請先問，不要直接下結論。\n\n請提醒老闆：確認這幾次是不是他本人，如果不是，請立刻更換密碼（個人選單 › 更改密碼）。在他換掉之前，這則提醒每 15 分鐘最多再出現一次，次數會累加在同一則裡，所以看到數字變大就是還在持續。",
				"body_omitted_chars": 0,
				"ts":                 apiAnyNumber,
				"ts_display":         "",
				"meta": map[string]any{
					"auth_alert": map[string]any{
						"kind":     "password_ok_factor_refused",
						"attempts": 3,
					},
				},
				"reply_card_status": "",
				"attachments":       []any{},
				"reply_to":          "",
			}},
		})
	})

	t.Run("an install whose roster no longer carries the assistant writes nothing and fans nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if _, err := d.HardDeleteMember(seedMiraID); err != nil {
			t.Fatalf("HardDeleteMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.deliverPasswordExposedAlert(3)

		dashboard.wantFrames()
	})
}

func TestPasswordExposedAlertBody(t *testing.T) {
	t.Run("one refused attempt reads as one attempt", func(t *testing.T) {
		if got := passwordExposedAlertBody(1); got != "🔴 登入警訊：有人用**正確的密碼**登入這台伺服器，但第二階段驗證碼是錯的（最近這段時間共 1 次）。\n\n這代表密碼本身已經在別人手上——第二因素目前擋住了他，但密碼不會自己變回安全。也有可能是老闆自己換了手機、驗證器還沒重新綁定，兩種情況看起來一模一樣，所以請先問，不要直接下結論。\n\n請提醒老闆：確認這幾次是不是他本人，如果不是，請立刻更換密碼（個人選單 › 更改密碼）。在他換掉之前，這則提醒每 15 分鐘最多再出現一次，次數會累加在同一則裡，所以看到數字變大就是還在持續。" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("several refused attempts read as that count", func(t *testing.T) {
		if got := passwordExposedAlertBody(3); got != "🔴 登入警訊：有人用**正確的密碼**登入這台伺服器，但第二階段驗證碼是錯的（最近這段時間共 3 次）。\n\n這代表密碼本身已經在別人手上——第二因素目前擋住了他，但密碼不會自己變回安全。也有可能是老闆自己換了手機、驗證器還沒重新綁定，兩種情況看起來一模一樣，所以請先問，不要直接下結論。\n\n請提醒老闆：確認這幾次是不是他本人，如果不是，請立刻更換密碼（個人選單 › 更改密碼）。在他換掉之前，這則提醒每 15 分鐘最多再出現一次，次數會累加在同一則裡，所以看到數字變大就是還在持續。" {
			t.Fatalf("got %q", got)
		}
	})
}
