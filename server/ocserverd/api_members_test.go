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
	t.Run("a well-formed GET /api/members answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/members reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed GET /api/members/{member_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/members/{member_id} reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/members/{member_id}/relocate answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/relocate request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/members/{member_id}/relocate reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/relocate request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/members/{member_id}/force-stop answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/force-stop request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/members/{member_id}/force-stop reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/force-stop request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(t *testing.T) {
	t.Run("a well-formed POST /api/members/{member_id}/accelerated-stop answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/accelerated-stop request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/members/{member_id}/accelerated-stop reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/accelerated-stop request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleRefocusMemberApiMembersMemberIdRefocusPost(t *testing.T) {
	t.Run("a well-formed POST /api/members/{member_id}/refocus answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/refocus request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/members/{member_id}/refocus reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/refocus request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/self/waking answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/waking request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/self/waking reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/waking request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReportStoppingApiSelfStoppingPost(t *testing.T) {
	t.Run("a well-formed POST /api/self/stopping answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/stopping request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/self/stopping reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/stopping request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReportStoppedApiSelfStoppedPost(t *testing.T) {
	t.Run("a well-formed POST /api/self/stopped answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/stopped request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/self/stopped reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/stopped request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleRestartSelfApiSelfRefocusPost(t *testing.T) {
	t.Run("a well-formed POST /api/self/refocus answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/refocus request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/self/refocus reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/self/refocus request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
