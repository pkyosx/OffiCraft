// Skeleton generated from server/ocserverd/api_members.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"testing"
)

const apiTestPNGBytes = "\x89PNG\r\n\x1a\nfake"

const apiTestOffboardNotice = "# 停止\n\n下線過程中，所有重要資料都不可以只留在本機 —— 重啟之後你可能在另一台機器上。\n\n- git commit 要推送到 remote。\n- 產物（artifact）要上傳到 task 底下。\n\n## 1. 你收到的是哪一種\n\n你讀到的是這一份，就代表**沒有人在對你倒數**：收尾照自己的節奏做完，收乾淨比收得快重要。\n\n⚠️ **但「沒有時鐘」不等於「沒有終點」。**\n**先確認這一段對不對你說話：它只對「外包 worker（`ow-` 開頭）被搬到另一台機器」那一條路成立。**\n你是正職成員的話，重新聚焦／換機器／換模型／自我重啟都是**先把你停掉、下一個 tick 才叫接班的起來**，\n接班的不會在你收尾時同時活著，你手上的 token 整段收尾都有效 —— **不要為了一個不存在的窗口趕工**，\n收乾淨仍然比收得快重要。外包 worker 跨機器搬家則相反：舊那一輪還活在 A、新的 START 已經送到 B，\n兩台機器的防重複守衛各自只看得到自己那台，所以兩輪會真的同時活著。在那條路上：\n**接班的那一輪一開機回報，你手上的 token 就當場失效**——不是被倒數收掉，是接班的來了。\n症狀跟 token 過期一模一樣：**你之後每一個 MCP 呼叫都 401，而且沒有任何一句話告訴你為什麼**。\n所以**只有被搬家的外包 worker** 要先把別人需要的東西寫出去（`post_chat` 給自己、task step、\n教訓回寫），細節放後面；寫完再慢慢收本機的暫存。這是 owner 明知代價後選的（交接可能斷在半路），\n不是故障，**別重試、別當成 server 壞了**。\n\n## 2. 開始下線\n\n1. 呼叫 MCP `report_stopping()`。\n2. 用 MCP `post_chat` 發給自己：現況、在途工作、阻塞點、下一步，以及有哪些 sub agent 在做什麼、跑多久了。\n3. 把在途的 sub agent 寫進 task step。**這一格沒被更新過，就代表它沒交件，下一代要重派。**\n\n## 3. 結束 sub agent\n\n- 等 sub agent 自己完成。\n- **每個 sub agent 一結束就當場更新** `post_chat` 與 task step，不要留到最後。\n\n## 4. 收尾\n\n1. 用 `ocagent clean <path>` 移除暫存檔/資料夾（不要用 `rm -rf`，他可能讓你彈出確認視窗而停住）。\n   - 它回非 0 多半是**這次指錯路徑**（例如指到工作目錄外面），不是壞了：換一個路徑再叫一次，不要為了收暫存停在這裡。\n2. 把這一輪的重要教訓回寫到長期記憶。**要寫進哪一份、怎麼寫，看開機說明「記憶與學習」那一節，那裡是權威**；只送改動的那一段。\n3. 呼叫 MCP `report_stopped()`，**然後讀回應裡的 `stop_effect`**。這一呼**不一定**會結束你的 session，四個值意思不同：\n   - `collected` —— 這一呼把刀送出去了，你正在被收。⚠️ **如果你收到這個值之後還活著、還被派事，那就是它沒成功**（伺服器把刀送出去之前的最後一步寫入可能失敗而沒有回報）。**這種情況不要再呼一次**（見 `already_reported`，重呼什麼都不會做），直接 `post_chat` 告訴有權改 `desired_state` 的人。\n   - `latched_for_collect` —— 這一呼沒送刀，但下一個 tick 會來收你。也是正在被收。\n   - `recorded_only` —— 🔴 **只是被記下來，沒有任何人在收你**。不會有刀、你不會停，過一下就會被再叫起來繼續花錢。**不要以為自己已經停了**；再呼一次也不會改變（見下一項）。要真的停下來，得由有權改 `desired_state` 的人來改，用 `post_chat` 告訴他你收到的是這個值。\n   - `already_reported` —— 你之前已經報過停了，**這一呼什麼都沒做**。第一次那呼的結果（不管是好是壞）仍然算數，重呼不是重試。\n"

func TestPutMember(t *testing.T) {
	t.Skip("TODO: putMember validates + persists a member and fans the member delta: a dismiss (roster_status=removed, the soft delete) rides as op=remove (deleted:true, payload null — Repository.put_member parity); every other write is a patch carrying the partial convenience payload (spec/sse.md §2.2: {id, name, status, desired_state, owner_id}).")
}

func TestPersistMemberOpReceipt(t *testing.T) {
	t.Skip("TODO: persistMemberOpReceipt stores the five last_op* columns of an ALREADY-STAMPED member row through their sole writer, then fans the member delta (T-55).")
}

func TestWindDownAnchorRowOfMember(t *testing.T) {
	t.Skip("TODO: windDownAnchorRowOfMember / windDownAnchorRowOfWorker are pure address-taking adapters and must stay that way, for the reason stopVerbRowOfMember gives: any logic here would be logic that exists twice again.")
}

func TestWindDownAnchorRowOfWorker(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPersistWindDownAnchors(t *testing.T) {
	t.Skip("TODO: persistWindDownAnchors is THE body of the anchor write, for both populations.")
}

func TestPublishMemberPatch(t *testing.T) {
	t.Skip("TODO: publishMemberPatch fans the member delta and nothing else.")
}

func TestMemberDeltaPayload(t *testing.T) {
	t.Skip("TODO: memberDeltaPayload is the member delta's partial convenience payload (repository._member_payload — the client reconciles by refetch).")
}

func TestOffboardDeltaPayload(t *testing.T) {
	t.Skip("TODO: offboardDeltaPayload is memberDeltaPayload plus the offboard notice, and it is the whole of \"改回真的推播\" (owner 2026-08-16, card rc-66b82a584c4d): the SERVER composes the sentence and carries the 〈停止〉 steps in the frame it pushes, instead of the agent fetching them back over HTTP once it notices it is being collected.")
}

func TestOffboardKindOf(t *testing.T) {
	t.Skip("TODO: offboardKindOf answers the two questions every offboard delta turns on: does this member carry a notice at all, and is it the SOFT one or the FINAL call.")
}

func TestForcedEpochLive(t *testing.T) {
	t.Skip("TODO: forcedEpochLive: the stop this member is currently under was opened by a FORCE-stop, not by 下線.")
}

func TestStopEpochAnchor(t *testing.T) {
	t.Skip("TODO: stopEpochAnchor answers what stopping_since must hold after a 停止 lands on this row: NOW for an ordinary stop, and the value it already carries when the epoch under way is a live FORCED one.")
}

func TestOffboardNoticeFor(t *testing.T) {
	t.Skip("TODO: offboardNoticeFor is the WHOLE wind-down sentence for this member: the document its arm reads, plus the manual write-back clause when the member has one.")
}

func TestOffboardManualWriteBackFor(t *testing.T) {
	t.Skip("TODO: offboardManualWriteBackFor resolves the 記憶回寫 clause for THIS member: the worker's bound task decides whether there is a 手冊 to write back into, and offboardManualWriteBack composes the sentence.")
}

func TestResolveAvatarMember(t *testing.T) {
	t.Skip("TODO: resolveAvatarMember admits active staff and outsource rows but rejects wardens: a machine is infrastructure, not a person with a visual identity.")
}

func TestPublishMemberAvatarChanged(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestMemberAvatarResult(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestHandlePutMemberAvatarApiMembersMemberIdAvatarPut(t *testing.T) {
	t.Run("raster bytes answer the minted avatar address and fan the member delta to the dashboard and to that member", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		rec := apiRequest(t, h, "PUT", "/api/members/kip/avatar", owner, apiTestPNGBytes)
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  "kip",
			"avatar_url": apiAnyString,
			"mime":       "image/png",
		})
		frame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("bytes that are no raster image at all answer 422 and fan nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "PUT", "/api/members/kip/avatar", owner, "not-an-image")
		if rec.Code != 422 {
			t.Fatalf("want 422, got %d (%s)", rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantError(t, data, "validation_error", "avatar must be PNG, JPEG, or WEBP raster bytes")
		dashboard.wantFrames()
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "PUT", "/api/members/nope/avatar", owner, apiTestPNGBytes)
		if rec.Code != 404 {
			t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		rec := apiRequest(t, h, "PUT", "/api/members/kip/avatar", "", apiTestPNGBytes)
		if rec.Code != 401 {
			t.Fatalf("want 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestHandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete(t *testing.T) {
	t.Run("clearing an avatar answers the empty address and fans the member delta to the dashboard and to that member", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiRequest(t, h, "PUT", "/api/members/kip/avatar", owner, apiTestPNGBytes)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		rec := apiRequest(t, h, "DELETE", "/api/members/kip/avatar", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantBody(t, data, map[string]any{"member_id": "kip", "avatar_url": ""})
		frame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "DELETE", "/api/members/nope/avatar", owner, "")
		if rec.Code != 404 {
			t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		rec := apiRequest(t, h, "DELETE", "/api/members/kip/avatar", "", "")
		if rec.Code != 401 {
			t.Fatalf("want 401, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestHandleListMembersApiMembersGet(t *testing.T) {
	t.Run("the listing carries every roster row in wire order with the presence and unread count derived for the caller", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		kip := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/chat", kip, `{"to":"owner","body":"hello"}`); status != 200 {
			t.Fatalf("seed chat: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/members", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			map[string]any{
				"id": "kip", "avatar_url": "", "name": "Kip", "kind": "staff",
				"role_key": "engineer", "role_name": "", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "",
				"desired_state": "", "desired_machine_id": "", "machine": "",
				"presence": "online", "refocus_since": 0, "refocus_op": "",
				"refocus_deadline": 0, "last_op": "", "last_op_ok": nil,
				"last_op_log": "", "last_op_reason": "", "last_op_at": 0,
				"forced_stop_at": 0, "unread_count": 1, "roster_status": "active",
				"owner_id": "owner", "schema_version": 3,
			},
			map[string]any{
				"id": "mira", "avatar_url": "", "name": "Mira", "kind": "staff",
				"role_key": "assistant", "role_name": "Assistant", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "medium",
				"desired_state": "offline", "desired_machine_id": "m-server-self",
				"machine": "", "presence": "offline", "refocus_since": 0,
				"refocus_op": "", "refocus_deadline": 0, "last_op": "",
				"last_op_ok": nil, "last_op_log": "", "last_op_reason": "",
				"last_op_at": 0, "forced_stop_at": 0, "unread_count": 0,
				"roster_status": "active", "owner_id": "owner", "schema_version": 3,
			},
			map[string]any{
				"id": "m-server-self", "avatar_url": "", "name": "伺服器這一台",
				"kind": "warden", "role_key": "", "role_name": "", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "medium",
				"desired_state": "offline", "desired_machine_id": "",
				"machine": "m-server-self", "presence": "offline", "refocus_since": 0,
				"refocus_op": "", "refocus_deadline": 0, "last_op": "",
				"last_op_ok": nil, "last_op_log": "", "last_op_reason": "",
				"last_op_at": 0, "forced_stop_at": 0, "unread_count": 0,
				"roster_status": "active", "owner_id": "owner", "schema_version": 3,
			},
		})
		dashboard.wantFrames()
	})

	t.Run("fields=light answers the same wire shape with every derived field left empty", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		kip := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/chat", kip, `{"to":"owner","body":"hello"}`); status != 200 {
			t.Fatalf("seed chat: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/members?fields=light", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			map[string]any{
				"id": "kip", "avatar_url": "", "name": "Kip", "kind": "staff",
				"role_key": "engineer", "role_name": "", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "",
				"desired_state": "", "desired_machine_id": "", "machine": "",
				"presence": "", "refocus_since": 0, "refocus_op": "",
				"refocus_deadline": 0, "last_op": "", "last_op_ok": nil,
				"last_op_log": "", "last_op_reason": "", "last_op_at": 0,
				"forced_stop_at": 0, "unread_count": 0, "roster_status": "active",
				"owner_id": "owner", "schema_version": 3,
			},
			map[string]any{
				"id": "mira", "avatar_url": "", "name": "Mira", "kind": "staff",
				"role_key": "assistant", "role_name": "Assistant", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "",
				"desired_state": "", "desired_machine_id": "", "machine": "",
				"presence": "", "refocus_since": 0, "refocus_op": "",
				"refocus_deadline": 0, "last_op": "", "last_op_ok": nil,
				"last_op_log": "", "last_op_reason": "", "last_op_at": 0,
				"forced_stop_at": 0, "unread_count": 0, "roster_status": "active",
				"owner_id": "owner", "schema_version": 3,
			},
			map[string]any{
				"id": "m-server-self", "avatar_url": "", "name": "伺服器這一台",
				"kind": "warden", "role_key": "", "role_name": "", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "",
				"desired_state": "", "desired_machine_id": "", "machine": "",
				"presence": "", "refocus_since": 0, "refocus_op": "",
				"refocus_deadline": 0, "last_op": "", "last_op_ok": nil,
				"last_op_log": "", "last_op_reason": "", "last_op_at": 0,
				"forced_stop_at": 0, "unread_count": 0, "roster_status": "active",
				"owner_id": "owner", "schema_version": 3,
			},
		})
		dashboard.wantFrames()
	})

	t.Run("a dismissed member drops off the listing, and a roster with nobody left answers an empty array", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}

		rec := apiRequest(t, h, "GET", "/api/members", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			map[string]any{
				"id": "mira", "avatar_url": "", "name": "Mira", "kind": "staff",
				"role_key": "assistant", "role_name": "Assistant", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "medium",
				"desired_state": "offline", "desired_machine_id": "m-server-self",
				"machine": "", "presence": "offline", "refocus_since": 0,
				"refocus_op": "", "refocus_deadline": 0, "last_op": "",
				"last_op_ok": nil, "last_op_log": "", "last_op_reason": "",
				"last_op_at": 0, "forced_stop_at": 0, "unread_count": 0,
				"roster_status": "active", "owner_id": "owner", "schema_version": 3,
			},
			map[string]any{
				"id": "m-server-self", "avatar_url": "", "name": "伺服器這一台",
				"kind": "warden", "role_key": "", "role_name": "", "runtime": "claude",
				"model": "", "actual_model": "", "actual_runtime": "",
				"actual_effort": "", "actual_machine": "", "effort": "medium",
				"desired_state": "offline", "desired_machine_id": "",
				"machine": "m-server-self", "presence": "offline", "refocus_since": 0,
				"refocus_op": "", "refocus_deadline": 0, "last_op": "",
				"last_op_ok": nil, "last_op_log": "", "last_op_reason": "",
				"last_op_at": 0, "forced_stop_at": 0, "unread_count": 0,
				"roster_status": "active", "owner_id": "owner", "schema_version": 3,
			},
		})

		for _, id := range []string{"mira", "m-server-self"} {
			m, err := d.GetMember(id)
			if err != nil {
				t.Fatalf("GetMember(%q): %v", id, err)
			}
			m.RosterStatus = RosterStatusRemoved
			if err := d.PutMember(*m); err != nil {
				t.Fatalf("PutMember(%q): %v", id, err)
			}
		}
		rec = apiRequest(t, h, "GET", "/api/members", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleHireMemberApiMembersPost(t *testing.T) {
	t.Run("a hire answers the minted id and fans the new member's own delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members", owner, `{"name":"Ada","kind":"staff","role_key":"engineer"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": apiAnyString})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            apiAnyString,
					"name":          "Ada",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a name that is only whitespace answers 422 and hires nobody", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members", owner, `{"name":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "member requires a name")
		dashboard.wantFrames()
	})

	t.Run("a kind outside the closed set answers 422 naming what was sent and hires nobody", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members", owner, `{"name":"Ada","kind":"ghost"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `member kind "ghost" not in {"staff", "warden", "outsource"}`)
		dashboard.wantFrames()
	})

	t.Run("an agent hiring with kind or role_key answers 403 and hires nobody", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members", agent, `{"name":"Ada","kind":"staff"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"hiring with kind/role_key is privilege-bearing; it requires an owner or an admin-role caller")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members", "", `{"name":"Ada"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetMemberApiMembersMemberIdGet(t *testing.T) {
	t.Run("one staff row answers the full projection with the caller's unread count and the live presence", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		kip := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/chat", kip, `{"to":"owner","body":"hello"}`); status != 200 {
			t.Fatalf("seed chat: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/members/kip", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "kip", "avatar_url": "", "name": "Kip", "kind": "staff",
			"role_key": "engineer", "role_name": "", "runtime": "claude",
			"model": "", "actual_model": "", "actual_runtime": "",
			"actual_effort": "", "actual_machine": "", "effort": "",
			"desired_state": "", "desired_machine_id": "", "machine": "",
			"presence": "online", "refocus_since": 0, "refocus_op": "",
			"refocus_deadline": 0, "last_op": "", "last_op_ok": nil,
			"last_op_log": "", "last_op_reason": "", "last_op_at": 0,
			"forced_stop_at": 0, "unread_count": 1, "roster_status": "active",
			"owner_id": "owner", "schema_version": 3,
		})
		dashboard.wantFrames()
	})

	t.Run("an outsource worker id resolves on this row too", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", Status: WorkerStatusActive,
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/members/ow-abc123", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "ow-abc123", "avatar_url": "", "name": "Contractor",
			"kind": "outsource", "role_key": "", "role_name": "", "runtime": "claude",
			"model": "", "actual_model": "", "actual_runtime": "",
			"actual_effort": "", "actual_machine": "", "effort": "",
			"desired_state": "", "desired_machine_id": "", "machine": "",
			"presence": "offline", "refocus_since": 0, "refocus_op": "",
			"refocus_deadline": 0, "last_op": "", "last_op_ok": nil,
			"last_op_log": "", "last_op_reason": "", "last_op_at": 0,
			"forced_stop_at": 0, "unread_count": 0, "roster_status": "active",
			"owner_id": "owner", "schema_version": 3,
		})
		dashboard.wantFrames()
	})

	t.Run("a dismissed member answers 404 naming it, as does an id nothing carries", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/members/kip", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'kip' not found")

		status, data = apiJSON(t, h, "GET", "/api/members/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/kip", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateMemberApiMembersMemberIdPatch(t *testing.T) {
	t.Run("a rename answers the member id and fans the delta to the dashboard and to that member's own connection", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip", owner, `{"name":"Kip L"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip L",
					"status":        "active",
					"desired_state": "",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/nope", owner, `{"name":"x"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip", "", `{"name":"x"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleActivateMemberApiMembersMemberIdActivatePost(t *testing.T) {
	t.Run("an activation answers the member id and fans a delta saying the member is wanted online", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "online",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a machine id nothing carries answers 404 naming the machine and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{"machine_id":"nope"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleRelocateMemberApiMembersMemberIdRelocatePost(t *testing.T) {
	t.Run("relocating a stopped member stores the pin and leaves the held-down receipt on the row", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members/mira/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "mira"})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::mira",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":            "mira",
					"name":          "Mira",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame, frame)
		self.wantFrames(frame, frame)
		bystander.wantFrames()

		m, err := d.GetMember("mira")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.DesiredMachineID != "m-server-self" {
			t.Fatalf("the pin must be stored, got %q", m.DesiredMachineID)
		}
		if m.DesiredState != "offline" {
			t.Fatalf("a relocate must not touch desired_state, got %q", m.DesiredState)
		}
		if m.LastOpReason != "held_down: the relocate was saved, but nothing was started — this member is stopped; 活化 it when you want it to run" {
			t.Fatalf("held-down receipt: got %q", m.LastOpReason)
		}
	})

	t.Run("relocating a member that is wanted online defers the move behind a wind-down and says so", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":                  "kip",
			"relocation_pending":  true,
			"relocation_deferred": true,
		})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":              "kip",
					"name":            "Kip",
					"status":          "active",
					"desired_state":   "online",
					"owner_id":        "owner",
					"offboard_notice": apiTestOffboardNotice,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.DesiredMachineID != "m-server-self" {
			t.Fatalf("the pin must be stored, got %q", m.DesiredMachineID)
		}
		if m.RefocusOp != "relocate" || m.RefocusSince <= 0 {
			t.Fatalf("a wind-down must be armed, got op=%q since=%v", m.RefocusOp, m.RefocusSince)
		}
	})

	t.Run("a body with no machine_id at all answers 422 and moves nobody", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/mira/relocate", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: machine_id")
		dashboard.wantFrames()
		m, err := d.GetMember("mira")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.DesiredMachineID != "m-server-self" {
			t.Fatalf("the pin must be untouched, got %q", m.DesiredMachineID)
		}
	})

	t.Run("an explicitly empty machine_id answers 400 because a relocate no longer unpins", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/mira/relocate", owner, `{"machine_id":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"machine_id must name a machine: a relocate moves an agent to a specific "+
				"machine, and no longer clears its placement")
		dashboard.wantFrames()
	})

	t.Run("a machine id nothing carries answers 404 naming the machine and moves nobody", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/mira/relocate", owner,
			`{"machine_id":"ghost"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'ghost' not found")
		dashboard.wantFrames()
		m, err := d.GetMember("mira")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.DesiredMachineID != "m-server-self" {
			t.Fatalf("the pin must be untouched, got %q", m.DesiredMachineID)
		}
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/nope/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/mira/relocate", agent,
			`{"machine_id":"m-server-self"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		m, err := d.GetMember("mira")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.LastOpReason != "" {
			t.Fatalf("a refused relocate must leave no receipt, got %q", m.LastOpReason)
		}
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/mira/relocate", "",
			`{"machine_id":"m-server-self"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestMemberHeldDownReceipt(t *testing.T) {
	t.Skip("TODO: memberHeldDownReceipt is the sentence a staff owner-verb leaves on the row when it was SAVED and nothing was started, because the owner has this member held down (T-ed79 #4 / #14).")
}

func TestClearMemberHandoverMarker(t *testing.T) {
	t.Skip("TODO: clearMemberHandoverMarker zeroes the 換手 epoch a staff STOP has just made meaningless — the worker /stop's two lines, given a name (T-ed79 parity #9).")
}

func TestStopVerbRowOfMember(t *testing.T) {
	t.Skip("TODO: stopVerbRowOfMember / stopVerbRowOfWorker are pure address-taking adapters and must stay that way: any logic here would be logic that exists twice again, which is the thing this file just deleted.")
}

func TestStopVerbRowOfWorker(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestApplyStopVerbRow(t *testing.T) {
	t.Skip("TODO: applyStopVerbRow is THE body of 停止, for both populations.")
}

func TestHandleDeactivateMemberApiMembersMemberIdDeactivatePost(t *testing.T) {
	t.Run("a deactivation answers the member id and fans a delta carrying the stop notice the agent is to read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":              "kip",
					"name":            "Kip",
					"status":          "active",
					"desired_state":   "offline",
					"owner_id":        "owner",
					"offboard_notice": apiTestOffboardNotice,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/nope/deactivate", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleForceStopMemberApiMembersMemberIdForceStopPost(t *testing.T) {
	t.Run("a force-stop drops the intent, records the cut-off instant and fans the delta without a notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.DesiredState != "offline" {
			t.Fatalf("want desired_state offline, got %q", m.DesiredState)
		}
		if m.ForcedStopAt <= 0 {
			t.Fatalf("the cut-off instant must be recorded, got %v", m.ForcedStopAt)
		}
		if m.StoppingSince <= 0 {
			t.Fatalf("the stop epoch must be open, got %v", m.StoppingSince)
		}
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/nope/force-stop", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an outsource id answers 404 because this row is the staff verb", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", Status: WorkerStatusActive,
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/mira/force-stop", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		m, err := d.GetMember("mira")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.ForcedStopAt != 0 || m.StoppingSince != 0 {
			t.Fatalf("a refused force-stop must leave the row alone, got forced=%v stopping=%v",
				m.ForcedStopAt, m.StoppingSince)
		}
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(t *testing.T) {
	t.Run("escalating an open wind-down re-stamps the epoch, names the cause and tells the member", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestListen(t, api, "kip")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`); status != 200 {
			t.Fatalf("deactivate: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
					// The final notice quotes the deadline instant this press
					// just opened, so its text cannot be written down in advance.
					"offboard_notice": apiAnyString,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "accelerated_stop" {
			t.Fatalf("want refocus_op accelerated_stop, got %q", m.RefocusOp)
		}
		if m.StoppingSince <= 0 {
			t.Fatalf("the epoch must be re-stamped, got %v", m.StoppingSince)
		}
	})

	t.Run("a member with no live session answers 409 and nothing is put on a clock", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`); status != 200 {
			t.Fatalf("deactivate: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 requires a live session — there is nothing to accelerate on a "+
				"member that is not connected")
		dashboard.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "" {
			t.Fatalf("a refused escalation must name no cause, got %q", m.RefocusOp)
		}
	})

	t.Run("a connected member nobody has asked to stop answers 409 naming the rung below", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		self := apiTestListen(t, api, "kip")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 escalates a wind-down that is already open — this member has not "+
				"been asked to stop. Press 停止 (deactivate) or 重新聚焦 (refocus) first")
		dashboard.wantFrames()
		self.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "" || m.StoppingSince != 0 {
			t.Fatalf("a refused escalation must open no epoch, got op=%q since=%v",
				m.RefocusOp, m.StoppingSince)
		}
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/nope/accelerated-stop", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleRefocusMemberApiMembersMemberIdRefocusPost(t *testing.T) {
	t.Run("a live member that is wanted online gets the refocus epoch and the wind-down notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":              "kip",
					"name":            "Kip",
					"status":          "active",
					"desired_state":   "online",
					"owner_id":        "owner",
					"offboard_notice": apiTestOffboardNotice,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "refocus" || m.RefocusSince <= 0 {
			t.Fatalf("want a refocus epoch, got op=%q since=%v", m.RefocusOp, m.RefocusSince)
		}
		if m.DesiredState != "online" {
			t.Fatalf("a refocus must leave the member wanted online, got %q", m.DesiredState)
		}
	})

	t.Run("a member with no live session answers 409 and no epoch is opened", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"refocus requires the member to have a live session and to be wanted "+
				"online (§3.4 #14)")
		dashboard.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusSince != 0 {
			t.Fatalf("a refused refocus must open no epoch, got %v", m.RefocusSince)
		}
	})

	t.Run("a member already being stopped queues a restart instead of turning the stop around", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`); status != 200 {
			t.Fatalf("deactivate: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":              "kip",
					"name":            "Kip",
					"status":          "active",
					"desired_state":   "offline",
					"owner_id":        "owner",
					"offboard_notice": apiTestOffboardNotice,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame, frame)
		self.wantFrames(frame, frame)

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if !m.RestartAfterStop {
			t.Fatalf("the restart must be queued behind the stop in flight")
		}
		if m.RefocusSince != 0 {
			t.Fatalf("the stop in flight keeps its anchors, got refocus_since=%v", m.RefocusSince)
		}
		if m.LastOpReason != "held_down: the refocus was saved and this member is still being stopped — the stop in flight is honoured as-is, and it will be started again once it is down" {
			t.Fatalf("queued-restart receipt: got %q", m.LastOpReason)
		}
	})

	t.Run("a member already further along the wind-down ladder answers 409 rather than stepping back", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", owner, `{}`); status != 200 {
			t.Fatalf("refocus: %d %v", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", owner, `{}`); status != 200 {
			t.Fatalf("accelerated-stop: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"refocus is 停止 and this member is already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); a later stage is never "+
				"replaced by an earlier one")
		dashboard.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "accelerated_stop" {
			t.Fatalf("the later rung must stand, got %q", m.RefocusOp)
		}
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/nope/refocus", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleDismissMemberApiMembersMemberIdDelete(t *testing.T) {
	t.Run("a dismissal answers the member id and fans a removal carrying no payload to the dashboard and to that member", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "kip"})
		frame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   1,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a member id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/members/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestResolveSelf(t *testing.T) {
	t.Skip("TODO: ── self-report presence (identity from token, NO member_id target) ────────── resolveSelf is the caller's own live member (404 when it has no roster row — e.g.")
}

func TestStampAgentIatFloor(t *testing.T) {
	t.Skip("TODO: stampAgentIatFloor raises the caller's own member credential floor to the `iat` of the token the caller is holding right now (T-14 項目 4B).")
}

func TestHandleReportWakingApiSelfWakingPost(t *testing.T) {
	t.Run("a boot report stamps the wake, stores the reported model and answers the caller's standing intent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/self/waking", agent, `{"model":"claude-opus-5"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "online",
			"refocus_op":       "",
			"refocus_deadline": 0,
		})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "online",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.WakingSince <= 0 {
			t.Fatalf("the wake must be stamped, got %v", m.WakingSince)
		}
		if m.ActualModel != "claude-opus-5" {
			t.Fatalf("want the reported model stored, got %q", m.ActualModel)
		}
		if m.AgentIatFloor <= 0 {
			t.Fatalf("the credential floor must be raised, got %v", m.AgentIatFloor)
		}
	})

	t.Run("a boot report on a member the owner has already stopped keeps the stop trace", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`); status != 200 {
			t.Fatalf("deactivate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/self/waking", agent, `{"model":"claude-opus-5"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "offline",
			"refocus_op":       "",
			"refocus_deadline": 0,
		})
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.StoppingSince <= 0 {
			t.Fatalf("the cancelled-mid-boot trace must survive, got %v", m.StoppingSince)
		}
	})

	t.Run("a caller whose roster row is gone answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/waking", agent, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'kip' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/self/waking", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReportStoppingApiSelfStoppingPost(t *testing.T) {
	t.Run("a stopping report opens the caller's wind-down and fans it without touching the wake anchor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/self/waking", agent, `{}`); status != 200 {
			t.Fatalf("waking: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/self/stopping", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "online",
			"refocus_op":       "",
			"refocus_deadline": 0,
		})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "online",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.StoppingSince <= 0 {
			t.Fatalf("the wind-down must be open, got %v", m.StoppingSince)
		}
		if m.WakingSince <= 0 {
			t.Fatalf("the wake anchor must survive a stopping report, got %v", m.WakingSince)
		}
	})

	t.Run("a caller whose roster row is gone answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/stopping", agent, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'kip' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/self/stopping", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReportStoppedApiSelfStoppedPost(t *testing.T) {
	t.Run("the first stopped report anchors the close-out and reports that it was collected", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "online",
			"refocus_op":       "",
			"refocus_deadline": 0,
			"stop_effect":      "collected",
		})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"status":        "active",
					"desired_state": "online",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.StoppedSince <= 0 {
			t.Fatalf("the close-out must be anchored, got %v", m.StoppedSince)
		}
	})

	t.Run("a repeat report changes nothing and says so", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("first stopped report: %d %v", status, data)
		}
		first, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "online",
			"refocus_op":       "",
			"refocus_deadline": 0,
			"stop_effect":      "already_reported",
		})
		again, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if again.StoppedSince != first.StoppedSince {
			t.Fatalf("the anchor must not be re-stamped: %v then %v",
				first.StoppedSince, again.StoppedSince)
		}
	})

	t.Run("a caller whose roster row is gone answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'kip' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/self/stopped", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleRestartSelfApiSelfRefocusPost(t *testing.T) {
	t.Run("a live caller gets its own refocus epoch and the wind-down notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/self/refocus", agent, `{"reason":"context is full"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "online",
			"refocus_op":       "restart_self",
			"refocus_deadline": 0,
		})
		frame := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":              "kip",
					"name":            "Kip",
					"status":          "active",
					"desired_state":   "online",
					"owner_id":        "owner",
					"offboard_notice": apiTestOffboardNotice,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()

		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "restart_self" || m.RefocusSince <= 0 {
			t.Fatalf("want a restart_self epoch, got op=%q since=%v", m.RefocusOp, m.RefocusSince)
		}
	})

	t.Run("a caller with no live session answers 409 and opens no epoch", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/refocus", agent, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"restart_self requires a live session to recycle, on a member that is "+
				"still wanted online")
		dashboard.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusSince != 0 {
			t.Fatalf("a refused restart must open no epoch, got %v", m.RefocusSince)
		}
	})

	t.Run("a session that has only just started is refused with the minimum-liveness floor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		// The SSE first-connect edge is what anchors the session; the fixture's
		// hub registration does not run it.
		api.onFirstConnect("kip")
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/refocus", agent, `{}`)
		if status != 429 {
			t.Fatalf("want 429, got %d (%v)", status, data)
		}
		apiWantError(t, data, "client_error",
			"restart_self refused: only 0s since this session started; the "+
				"minimum-liveness floor is 600s (prevents a respawn storm)")
		dashboard.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusSince != 0 {
			t.Fatalf("a refused restart must open no epoch, got %v", m.RefocusSince)
		}
	})

	t.Run("a caller already further along the wind-down ladder answers 409 rather than stepping back", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		apiTestListen(t, api, "kip")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/refocus", owner, `{}`); status != 200 {
			t.Fatalf("refocus: %d %v", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/accelerated-stop", owner, `{}`); status != 200 {
			t.Fatalf("accelerated-stop: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/refocus", agent, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"restart_self is 停止 and you are already further along the "+
				"wind-down ladder (下線 → 加速 → 強制); finish the close-out you "+
				"were given instead")
		dashboard.wantFrames()
		m, err := d.GetMember("kip")
		if err != nil {
			t.Fatalf("GetMember: %v", err)
		}
		if m.RefocusOp != "accelerated_stop" {
			t.Fatalf("the later rung must stand, got %q", m.RefocusOp)
		}
	})

	t.Run("a caller whose roster row is gone answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/self/refocus", agent, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'kip' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/self/refocus", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}
