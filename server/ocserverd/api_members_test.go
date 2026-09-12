// Skeleton generated from server/ocserverd/api_members.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

const apiTestPNGBytes = "\x89PNG\r\n\x1a\nfake"

const apiTestOffboardNotice = "# 停止\n\n下線過程中，所有重要資料都不可以只留在本機 —— 重啟之後你可能在另一台機器上。\n\n- git commit 要推送到 remote。\n- 產物（artifact）要上傳到 task 底下。\n\n## 1. 你收到的是哪一種\n\n你讀到的是這一份，就代表**沒有人在對你倒數**：收尾照自己的節奏做完，收乾淨比收得快重要。\n\n⚠️ **但「沒有時鐘」不等於「沒有終點」。**\n**先確認這一段對不對你說話：它只對「外包 worker（`ow-` 開頭）被搬到另一台機器」那一條路成立。**\n你是正職成員的話，重新聚焦／換機器／換模型／自我重啟都是**先把你停掉、下一個 tick 才叫接班的起來**，\n接班的不會在你收尾時同時活著，你手上的 token 整段收尾都有效 —— **不要為了一個不存在的窗口趕工**，\n收乾淨仍然比收得快重要。外包 worker 跨機器搬家則相反：舊那一輪還活在 A、新的 START 已經送到 B，\n兩台機器的防重複守衛各自只看得到自己那台，所以兩輪會真的同時活著。在那條路上：\n**接班的那一輪一開機回報，你手上的 token 就當場失效**——不是被倒數收掉，是接班的來了。\n症狀跟 token 過期一模一樣：**你之後每一個 MCP 呼叫都 401，而且沒有任何一句話告訴你為什麼**。\n所以**只有被搬家的外包 worker** 要先把別人需要的東西寫出去（`post_chat` 給自己、task step、\n傳承寫入），細節放後面；寫完再慢慢收本機的暫存。這是 owner 明知代價後選的（交接可能斷在半路），\n不是故障，**別重試、別當成 server 壞了**。\n\n## 2. 開始下線\n\n1. 呼叫 MCP `report_stopping()`。\n2. 用 MCP `post_chat` 發給自己：現況、在途工作、阻塞點、下一步，以及有哪些 sub agent 在做什麼、跑多久了。\n3. 把在途的 sub agent 寫進 task step。**這一格沒被更新過，就代表它沒交件，下一代要重派。**\n\n## 3. 結束 sub agent\n\n- 等 sub agent 自己完成。\n- **每個 sub agent 一結束就當場更新** `post_chat` 與 task step，不要留到最後。\n\n## 4. 收尾\n\n1. 用 `ocagent clean <path>` 移除暫存檔/資料夾（不要用 `rm -rf`，他可能觸發無人回應的確認視窗：你會停在該處，或那一呼被當場拒絕）。\n   - 它回非 0 多半是**這次指錯路徑**（例如指到工作目錄外面），不是壞了：換一個路徑再叫一次，不要為了收暫存停在這裡。\n2. 把這一輪值得留下的經驗寫成傳承（MCP `write_lore_entry`）。**要不要寫、怎麼寫，看開機說明「4. 記憶與傳承」那一節，那裡是權威**；寫下去就不能改，所以一條寫成一筆。\n3. 呼叫 MCP `report_stopped()`，**然後讀回應裡的 `stop_effect`**。這一呼**不一定**會結束你的 session，四個值意思不同：\n   - `collected` —— 這一呼把刀送出去了，你正在被收。⚠️ **如果你收到這個值之後還活著、還被派事，那就是它沒成功**（伺服器把刀送出去之前的最後一步寫入可能失敗而沒有回報）。**這種情況不要再呼一次**（見 `already_reported`，重呼什麼都不會做），直接 `post_chat` 告訴有權改 `desired_state` 的人。\n   - `latched_for_collect` —— 這一呼沒送刀，但下一個 tick 會來收你。也是正在被收。\n   - `recorded_only` —— 🔴 **只是被記下來，沒有任何人在收你**。不會有刀、你不會停，過一下就會被再叫起來繼續花錢。**不要以為自己已經停了**；再呼一次也不會改變（見下一項）。要真的停下來，得由有權改 `desired_state` 的人來改，用 `post_chat` 告訴他你收到的是這個值。\n   - `already_reported` —— 你之前已經報過停了，**這一呼什麼都沒做**。第一次那呼的結果（不管是好是壞）仍然算數，重呼不是重試。\n"

const apiTestAcceleratedNotice = "你的結束時刻是 1970-01-01T00:18:40Z。\n# 加速停止\n\n下線過程中，所有重要資料都不可以只留在本機 —— 重啟之後你可能在另一台機器上。\n\n- git commit 要推送到 remote。\n- 產物（artifact）要上傳到 task 底下。\n\n## 1. 你收到的是哪一種\n\n你讀到的是這一份，就代表**你在倒數中**：上面那一行的結束時刻就是死線。過了那個時刻你會被直接收掉，所以**先保交接、後保細節**。\n\n⚠️ **倒數不是唯一的終點，而且第二個終點可能更早到。**\n**先確認這一段對不對你說話：它只對「外包 worker（`ow-` 開頭）被搬到另一台機器」那一條路成立。**\n你是正職成員的話，重新聚焦／換機器／換模型／自我重啟都是**先把你停掉、下一個 tick 才叫接班的起來**，\n接班的不會在你收尾時同時活著，你手上的 token 整段收尾都有效 —— **不要為了一個不存在的窗口趕工**，\n收乾淨仍然比收得快重要。外包 worker 跨機器搬家則相反：舊那一輪還活在 A、新的 START 已經送到 B，\n兩台機器的防重複守衛各自只看得到自己那台，所以兩輪會真的同時活著。在那條路上：\n**接班的那一輪一開機回報，你手上的 token 就當場失效**——不是被倒數收掉，是接班的來了。\n症狀跟 token 過期一模一樣：**你之後每一個 MCP 呼叫都 401，而且沒有任何一句話告訴你為什麼**。\n所以**只有被搬家的外包 worker** 要先把別人需要的東西寫出去（`post_chat` 給自己、task step、\n傳承寫入），細節放後面；寫完再慢慢收本機的暫存。這是 owner 明知代價後選的（交接可能斷在半路），\n不是故障，**別重試、別當成 server 壞了**。\n\n## 2. 開始下線\n\n1. 呼叫 MCP `report_stopping()`。\n2. 用 MCP `post_chat` 發給自己：現況、在途工作、阻塞點、下一步，以及有哪些 sub agent 在做什麼、跑多久了。\n3. 把在途的 sub agent 寫進 task step。**這一格沒被更新過，就代表它沒交件，下一代要重派。**\n\n## 3. 結束 sub agent\n\n- 請 sub agent 立刻把手上的東西收尾並結束，把目前狀態寫回 server，至少包含已驗證的內容、證據位置、剩餘工作與未檢查範圍。\n- **每個 sub agent 一結束就當場更新** `post_chat` 與 task step，不要留到最後。\n\n## 4. 收尾\n\n1. 用 `ocagent clean <path>` 移除暫存檔/資料夾（不要用 `rm -rf`，他可能觸發無人回應的確認視窗：你會停在該處，或那一呼被當場拒絕）。\n   - 它回非 0 多半是**這次指錯路徑**（例如指到工作目錄外面），不是壞了：換一個路徑再叫一次，不要為了收暫存停在這裡。\n2. 把這一輪值得留下的經驗寫成傳承（MCP `write_lore_entry`）。**要不要寫、怎麼寫，看開機說明「4. 記憶與傳承」那一節，那裡是權威**；寫下去就不能改，所以一條寫成一筆。\n3. 呼叫 MCP `report_stopped()`，**然後讀回應裡的 `stop_effect`**。這一呼**不一定**會結束你的 session，四個值意思不同：\n   - `collected` —— 這一呼把刀送出去了，你正在被收。⚠️ **如果你收到這個值之後還活著、還被派事，那就是它沒成功**（伺服器把刀送出去之前的最後一步寫入可能失敗而沒有回報）。**這種情況不要再呼一次**（見 `already_reported`，重呼什麼都不會做），直接 `post_chat` 告訴有權改 `desired_state` 的人。\n   - `latched_for_collect` —— 這一呼沒送刀，但下一個 tick 會來收你。也是正在被收。\n   - `recorded_only` —— 🔴 **只是被記下來，沒有任何人在收你**。不會有刀、你不會停，過一下就會被再叫起來繼續花錢。**不要以為自己已經停了**；再呼一次也不會改變（見下一項）。要真的停下來，得由有權改 `desired_state` 的人來改，用 `post_chat` 告訴他你收到的是這個值。\n   - `already_reported` —— 你之前已經報過停了，**這一呼什麼都沒做**。第一次那呼的結果（不管是好是壞）仍然算數，重呼不是重試。\n"

// The wind-down sentence an outsource worker gets is composed of two documents,
// and these two sentinels stand in for their contents so the expected value can
// be written out by hand. Comparing the output against the real documents would
// pass just as well for a build that stopped reading them and hardcoded the
// words instead.
const (
	apiTestOffboardDocSentinel  = "OFFBOARD-DOC-SENTINEL"
	apiTestCloseoutBodySentinel = "CLOSEOUT-BODY-SENTINEL"
)

// apiTestSentinelWindDownDocs rewrites 〈停止〉 whole and 〈任務結案〉's editable
// half, through the same write face the owner edits them with.
func apiTestSentinelWindDownDocs(t *testing.T, api *apiServer) {
	t.Helper()
	for _, doc := range []struct {
		spec bootDocSpec
		body string
	}{
		{api.offboardSpec(), apiTestOffboardDocSentinel},
		{api.mustBootDocSpec(docKindTaskCloseout, bootDocSingletonKey), apiTestCloseoutBodySentinel},
	} {
		w := httptest.NewRecorder()
		api.replaceBootDoc(w, httptest.NewRequest(http.MethodPost, "/x", nil), doc.spec, doc.body, true)
		if w.Code != http.StatusOK {
			t.Fatalf("seed %s with a sentinel: status %d (%s)", doc.spec.Kind, w.Code, w.Body.String())
		}
	}
}

func apiTestMemberFrame(seq int, op, id string, payload any, trigger string) map[string]any {
	return map[string]any{
		"seq": seq, "topic": "member", "op": op,
		"data": map[string]any{
			"entity": "member", "key": "owner::" + id,
			"epoch": seq, "deleted": op == "remove", "payload": payload,
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func apiTestMemberPayload(id, name, status, desired string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "status": status,
		"desired_state": desired, "owner_id": "owner",
	}
}

func apiTestMemberRow(t *testing.T, d *DAL, id string) Member {
	t.Helper()
	m, err := d.GetMember(id)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", id, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%q): no such row", id)
	}
	return *m
}

func apiTestWantEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	t.Fatalf("%s:\n want %s\n  got %s", label, wantJSON, gotJSON)
}

func apiTestAuthedRequest(t *testing.T, api *apiServer, d *DAL, token string) *http.Request {
	t.Helper()
	var captured *http.Request
	gate := requireAuth(api.keys, nil, d.GetMember, http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) { captured = r }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/self/waking", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	gate.ServeHTTP(rec, req)
	if captured == nil {
		t.Fatalf("the auth gate refused the credential: %d %s", rec.Code, rec.Body.String())
	}
	return captured
}

func apiTestTypedTaskWorker(t *testing.T, api *apiServer, h http.Handler, d *DAL, owner, typeKey string) Member {
	t.Helper()
	apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
	if typeKey != "" {
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v %v", task, err)
		}
		task.TypeKey = typeKey
		if err := d.PutTask(*task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
	}
	_ = api
	return apiTestMemberRow(t, d, "ow-abc123")
}

func TestPutMember(t *testing.T) {
	t.Run("a valid row is persisted whole and its patch reaches the dashboard and that member alone", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		m := before
		m.Name = "Kipling"
		m.DesiredState = DesiredStateOnline
		if err := api.putMember(m, "owner"); err != nil {
			t.Fatalf("putMember: %v", err)
		}

		want := before
		want.Name = "Kipling"
		want.DesiredState = DesiredStateOnline
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), want)
		frame := apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kipling", "active", "online"), "owner")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a removed roster status rides as op=remove carrying a null payload, and the row stays in the table", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")

		m := before
		m.RosterStatus = RosterStatusRemoved
		if err := api.putMember(m, "owner"); err != nil {
			t.Fatalf("putMember: %v", err)
		}

		want := before
		want.RosterStatus = RosterStatusRemoved
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), want)
		frame := apiTestMemberFrame(1, "remove", "kip", nil, "owner")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
	})

	t.Run("a kind outside the closed set is refused by name, writes nothing and fans nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")

		m := before
		m.Kind = "ghost"
		m.Name = "Kipling"
		err := api.putMember(m, "owner")
		if err == nil {
			t.Fatalf("want a refusal, got nil")
		}
		if err.Error() != `member kip: kind "ghost" not in {"staff", "warden", "outsource"}` {
			t.Fatalf("refusal text: %q", err.Error())
		}
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), before)
		dashboard.wantFrames()
	})

	t.Run("a runtime outside the closed set is refused while a blank runtime is accepted", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")

		m := before
		m.Runtime = "gpt"
		err := api.putMember(m, "owner")
		if err == nil {
			t.Fatalf("want a refusal, got nil")
		}
		if err.Error() != `member kip: runtime "gpt" not in {"claude", "codex"}` {
			t.Fatalf("refusal text: %q", err.Error())
		}
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), before)
		dashboard.wantFrames()

		blank := before
		blank.Runtime = ""
		if err := api.putMember(blank, "owner"); err != nil {
			t.Fatalf("a blank runtime must be accepted: %v", err)
		}
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), before)
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "owner"))
	})
}

func TestPersistMemberOpReceipt(t *testing.T) {
	t.Run("only the five receipt columns land, and the fanned delta carries the caller's snapshot rather than the stored row", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		ok := true
		m := before
		m.Name = "Kipling"
		m.LastOp = "activate"
		m.LastOpOK = &ok
		m.LastOpLog = "start dispatched"
		m.LastOpReason = "owner pressed it"
		m.LastOpAt = 1700000000
		if err := api.persistMemberOpReceipt(m, "owner"); err != nil {
			t.Fatalf("persistMemberOpReceipt: %v", err)
		}

		want := before
		want.LastOp = "activate"
		want.LastOpOK = &ok
		want.LastOpLog = "start dispatched"
		want.LastOpReason = "owner pressed it"
		want.LastOpAt = 1700000000
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), want)
		frame := apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kipling", "active", ""), "owner")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a receipt whose ok flag is nil is stored as nil rather than as false", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		notOK := false
		stamped := apiTestMemberRow(t, d, "kip")
		stamped.LastOp = "deactivate"
		stamped.LastOpOK = &notOK
		if err := api.persistMemberOpReceipt(stamped, "owner"); err != nil {
			t.Fatalf("persistMemberOpReceipt: %v", err)
		}
		if got := apiTestMemberRow(t, d, "kip"); got.LastOpOK == nil || *got.LastOpOK {
			t.Fatalf("want a stored false, got %v", got.LastOpOK)
		}

		cleared := apiTestMemberRow(t, d, "kip")
		cleared.LastOpOK = nil
		if err := api.persistMemberOpReceipt(cleared, "owner"); err != nil {
			t.Fatalf("persistMemberOpReceipt: %v", err)
		}
		if got := apiTestMemberRow(t, d, "kip"); got.LastOpOK != nil {
			t.Fatalf("want a stored nil, got %v", *got.LastOpOK)
		}
	})

	t.Run("a receipt naming a row the roster does not carry writes nobody and still fans the delta", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		if err := api.persistMemberOpReceipt(Member{ID: "nope", Name: "Nobody",
			Kind: KindStaff, RosterStatus: RosterStatusActive, LastOp: "activate"}, "owner"); err != nil {
			t.Fatalf("persistMemberOpReceipt: %v", err)
		}

		if row, err := d.GetMember("nope"); err != nil || row != nil {
			t.Fatalf("want no row minted, got %v %v", row, err)
		}
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "nope",
			apiTestMemberPayload("nope", "Nobody", "active", ""), "owner"))
	})
}

func TestWindDownAnchorRowOfMember(t *testing.T) {
	t.Run("the four pointers name the member's own anchor columns and the id is a copy", func(t *testing.T) {
		m := Member{ID: "kip", Name: "Kip", Kind: KindStaff, RosterStatus: RosterStatusActive,
			StoppingSince: 1, StoppedSince: 2, RefocusSince: 3, RefocusOp: refocusOpRefocus}
		row := windDownAnchorRowOfMember(&m)

		if row.ID != "kip" {
			t.Fatalf("row id: %q", row.ID)
		}
		if *row.StoppingSince != 1 || *row.StoppedSince != 2 || *row.RefocusSince != 3 ||
			*row.RefocusOp != refocusOpRefocus {
			t.Fatalf("row reads %v %v %v %q", *row.StoppingSince, *row.StoppedSince,
				*row.RefocusSince, *row.RefocusOp)
		}

		row.ID = "somebody-else"
		*row.StoppingSince = 10
		*row.StoppedSince = 20
		*row.RefocusSince = 30
		*row.RefocusOp = refocusOpAcceleratedStop
		apiTestWantEqual(t, "member", m, Member{ID: "kip", Name: "Kip", Kind: KindStaff,
			RosterStatus: RosterStatusActive, StoppingSince: 10, StoppedSince: 20,
			RefocusSince: 30, RefocusOp: refocusOpAcceleratedStop})
	})

	t.Run("a row taken over a copy leaves the original member untouched", func(t *testing.T) {
		original := Member{ID: "kip", Kind: KindStaff, StoppingSince: 1, StoppedSince: 2,
			RefocusSince: 3, RefocusOp: refocusOpRefocus}
		copied := original
		row := windDownAnchorRowOfMember(&copied)
		*row.StoppingSince = 99
		*row.RefocusOp = ""

		apiTestWantEqual(t, "the original", original, Member{ID: "kip", Kind: KindStaff,
			StoppingSince: 1, StoppedSince: 2, RefocusSince: 3, RefocusOp: refocusOpRefocus})
		apiTestWantEqual(t, "the copy", copied, Member{ID: "kip", Kind: KindStaff,
			StoppingSince: 99, StoppedSince: 2, RefocusSince: 3, RefocusOp: ""})
	})
}

func TestWindDownAnchorRowOfWorker(t *testing.T) {
	t.Run("the four pointers name the worker's own anchor columns and the id is a copy", func(t *testing.T) {
		w := OutsourceWorker{ID: "ow-abc123", Codename: "Contractor", Status: WorkerStatusActive,
			StoppingSince: 1, StoppedSince: 2, RefocusSince: 3, RefocusOp: refocusOpRefocus}
		row := windDownAnchorRowOfWorker(&w)

		if row.ID != "ow-abc123" {
			t.Fatalf("row id: %q", row.ID)
		}
		if *row.StoppingSince != 1 || *row.StoppedSince != 2 || *row.RefocusSince != 3 ||
			*row.RefocusOp != refocusOpRefocus {
			t.Fatalf("row reads %v %v %v %q", *row.StoppingSince, *row.StoppedSince,
				*row.RefocusSince, *row.RefocusOp)
		}

		row.ID = "ow-somebody-else"
		*row.StoppingSince = 10
		*row.StoppedSince = 20
		*row.RefocusSince = 30
		*row.RefocusOp = refocusOpAcceleratedStop
		apiTestWantEqual(t, "worker", w, OutsourceWorker{ID: "ow-abc123", Codename: "Contractor",
			Status: WorkerStatusActive, StoppingSince: 10, StoppedSince: 20,
			RefocusSince: 30, RefocusOp: refocusOpAcceleratedStop})
	})

	t.Run("a row taken over a copy leaves the original worker untouched", func(t *testing.T) {
		original := OutsourceWorker{ID: "ow-abc123", StoppingSince: 1, StoppedSince: 2,
			RefocusSince: 3, RefocusOp: refocusOpRefocus}
		copied := original
		row := windDownAnchorRowOfWorker(&copied)
		*row.StoppedSince = 99
		*row.RefocusSince = 0

		apiTestWantEqual(t, "the original", original, OutsourceWorker{ID: "ow-abc123",
			StoppingSince: 1, StoppedSince: 2, RefocusSince: 3, RefocusOp: refocusOpRefocus})
		apiTestWantEqual(t, "the copy", copied, OutsourceWorker{ID: "ow-abc123",
			StoppingSince: 1, StoppedSince: 99, RefocusSince: 0, RefocusOp: refocusOpRefocus})
	})
}

func TestPersistWindDownAnchors(t *testing.T) {
	t.Run("the four anchors land on the member row, nothing else moves and no delta is fanned", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")

		m := before
		m.Name = "Kipling"
		m.StoppingSince = 1000
		m.StoppedSince = 1100
		m.RefocusSince = 900
		m.RefocusOp = refocusOpRefocus
		if err := api.persistWindDownAnchors(windDownAnchorRowOfMember(&m)); err != nil {
			t.Fatalf("persistWindDownAnchors: %v", err)
		}

		want := before
		want.StoppingSince = 1000
		want.StoppedSince = 1100
		want.RefocusSince = 900
		want.RefocusOp = refocusOpRefocus
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), want)
		dashboard.wantFrames()
		self.wantFrames()
	})

	t.Run("zeroed anchors are written as zeroes rather than skipped", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		open := apiTestMemberRow(t, d, "kip")
		open.StoppingSince = 1000
		open.RefocusSince = 900
		open.RefocusOp = refocusOpRefocus
		if err := api.persistWindDownAnchors(windDownAnchorRowOfMember(&open)); err != nil {
			t.Fatalf("persistWindDownAnchors: %v", err)
		}
		before := apiTestMemberRow(t, d, "kip")

		cleared := before
		cleared.StoppingSince = 0
		cleared.StoppedSince = 0
		cleared.RefocusSince = 0
		cleared.RefocusOp = ""
		if err := api.persistWindDownAnchors(windDownAnchorRowOfMember(&cleared)); err != nil {
			t.Fatalf("persistWindDownAnchors: %v", err)
		}

		want := before
		want.StoppingSince = 0
		want.StoppedSince = 0
		want.RefocusSince = 0
		want.RefocusOp = ""
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), want)
	})

	t.Run("an id the roster does not carry is a clean no-op that mints nobody", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		ghost := Member{ID: "nope", StoppingSince: 1000, RefocusOp: refocusOpRefocus}
		if err := api.persistWindDownAnchors(windDownAnchorRowOfMember(&ghost)); err != nil {
			t.Fatalf("persistWindDownAnchors: %v", err)
		}

		if row, err := d.GetMember("nope"); err != nil || row != nil {
			t.Fatalf("want no row minted, got %v %v", row, err)
		}
		dashboard.wantFrames()
	})

	t.Run("the worker face writes the same four columns onto an outsource row", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		before, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || before == nil {
			t.Fatalf("GetOutsourceWorker: %v %v", before, err)
		}
		dashboard := apiTestListen(t, api, "")

		w := *before
		w.Codename = "Renamed"
		w.StoppingSince = 1000
		w.StoppedSince = 1100
		w.RefocusSince = 900
		w.RefocusOp = refocusOpAcceleratedStop
		if err := api.persistWorkerWindDownAnchors(w); err != nil {
			t.Fatalf("persistWorkerWindDownAnchors: %v", err)
		}

		after, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || after == nil {
			t.Fatalf("GetOutsourceWorker: %v %v", after, err)
		}
		want := *before
		want.StoppingSince = 1000
		want.StoppedSince = 1100
		want.RefocusSince = 900
		want.RefocusOp = refocusOpAcceleratedStop
		apiTestWantEqual(t, "stored worker", *after, want)
		dashboard.wantFrames()
	})
}

func TestPublishMemberPatch(t *testing.T) {
	t.Run("an active member's patch reaches the dashboard and that member alone, and writes nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.publishMemberPatch(before, "owner")

		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), before)
		frame := apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "owner")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a removed member rides as op=remove carrying a null payload", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.RosterStatus = RosterStatusRemoved
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")

		api.publishMemberPatch(m, "server")

		frame := apiTestMemberFrame(1, "remove", "kip", nil, "server")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
	})

	t.Run("a member under a graceful stop carries the wind-down sentence in the frame", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000
		dashboard := apiTestListen(t, api, "")

		api.publishMemberPatch(m, "owner")

		payload := apiTestMemberPayload("kip", "Kip", "active", "offline")
		payload["offboard_notice"] = apiTestOffboardNotice
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "kip", payload, "owner"))
	})
}

func TestMemberDeltaPayload(t *testing.T) {
	t.Run("a staff row is projected onto the five convenience keys and nothing else", func(t *testing.T) {
		got := memberDeltaPayload(Member{ID: "kip", Name: "Kip", Kind: KindStaff,
			RoleKey: "engineer", Runtime: "claude", Model: "sonnet",
			DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
			StoppingSince: 1000, ForcedStopAt: 900})
		apiTestWantEqual(t, "payload", got, map[string]any{
			"id": "kip", "name": "Kip", "status": "active",
			"desired_state": "online", "owner_id": "owner",
		})
	})

	t.Run("a dismissed row keeps the same five keys and reports its removed roster status", func(t *testing.T) {
		got := memberDeltaPayload(Member{ID: "kip", Name: "Kip", Kind: KindStaff,
			RosterStatus: RosterStatusRemoved})
		apiTestWantEqual(t, "payload", got, map[string]any{
			"id": "kip", "name": "Kip", "status": "removed",
			"desired_state": "", "owner_id": "owner",
		})
	})

	t.Run("an outsource row is projected the same way, by codename-as-name", func(t *testing.T) {
		got := memberDeltaPayload(memberFromWorker(OutsourceWorker{ID: "ow-abc123",
			Codename: "Contractor", Status: WorkerStatusAssigned, TaskID: "T-1",
			DesiredState: DesiredStateOffline}))
		apiTestWantEqual(t, "payload", got, map[string]any{
			"id": "ow-abc123", "name": "Contractor", "status": "active",
			"desired_state": "offline", "owner_id": "owner",
		})
	})
}

func TestOffboardDeltaPayload(t *testing.T) {
	t.Run("a member nobody is winding down carries the five keys and no notice key at all", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		apiTestWantEqual(t, "payload", api.offboardDeltaPayload(apiTestMemberRow(t, d, "kip")),
			map[string]any{"id": "kip", "name": "Kip", "status": "active",
				"desired_state": "", "owner_id": "owner"})
	})

	t.Run("a member under a graceful stop carries the soft wind-down document", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000

		apiTestWantEqual(t, "payload", api.offboardDeltaPayload(m),
			map[string]any{"id": "kip", "name": "Kip", "status": "active",
				"desired_state": "offline", "owner_id": "owner",
				"offboard_notice": apiTestOffboardNotice})
	})

	t.Run("a member the owner accelerated carries the accelerated document with its deadline rendered", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000
		m.RefocusOp = refocusOpAcceleratedStop

		apiTestWantEqual(t, "payload", api.offboardDeltaPayload(m),
			map[string]any{"id": "kip", "name": "Kip", "status": "active",
				"desired_state": "offline", "owner_id": "owner",
				"offboard_notice": apiTestAcceleratedNotice})
	})

	t.Run("a member whose stop epoch was forced is told nothing at all", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000
		m.ForcedStopAt = 1000

		apiTestWantEqual(t, "payload", api.offboardDeltaPayload(m),
			map[string]any{"id": "kip", "name": "Kip", "status": "active",
				"desired_state": "offline", "owner_id": "owner"})
	})
}

func TestOffboardKindOf(t *testing.T) {
	cases := []struct {
		name    string
		member  Member
		kind    string
		carries bool
	}{
		{"a member with no wind-down at all carries no notice",
			Member{ID: "kip"}, "", false},
		{"a desired-offline member with no stop anchor carries no notice",
			Member{ID: "kip", DesiredState: DesiredStateOffline}, "", false},
		{"a graceful stop epoch is the soft sentence",
			Member{ID: "kip", DesiredState: DesiredStateOffline, StoppingSince: 1000},
			offboardKindSoft, true},
		{"a stop epoch the owner accelerated is the final call",
			Member{ID: "kip", DesiredState: DesiredStateOffline, StoppingSince: 1000,
				RefocusOp: refocusOpAcceleratedStop}, offboardKindFinal, true},
		{"a stop epoch marked by an unclocked cause is still the soft sentence",
			Member{ID: "kip", DesiredState: DesiredStateOffline, StoppingSince: 1000,
				RefocusOp: refocusOpRefocus}, offboardKindSoft, true},
		{"a stop epoch that a force-stop opened says nothing",
			Member{ID: "kip", DesiredState: DesiredStateOffline, StoppingSince: 1000,
				ForcedStopAt: 1000}, "", false},
		{"an online member with no refocus epoch carries no notice",
			Member{ID: "kip", DesiredState: DesiredStateOnline}, "", false},
		{"an online member under 重新聚焦 is the soft sentence",
			Member{ID: "kip", DesiredState: DesiredStateOnline, RefocusSince: 1000,
				RefocusOp: refocusOpRefocus}, offboardKindSoft, true},
		{"an online member at the second context threshold is the final call",
			Member{ID: "kip", DesiredState: DesiredStateOnline, RefocusSince: 1000,
				RefocusOp: refocusOpContextHigh}, offboardKindFinal, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, carries := offboardKindOf(c.member, 0)
			if kind != c.kind || carries != c.carries {
				t.Fatalf("want (%q, %v), got (%q, %v)", c.kind, c.carries, kind, carries)
			}
			late, lateCarries := offboardKindOf(c.member, 1.8e9)
			if late != kind || lateCarries != carries {
				t.Fatalf("the clock moved the answer: (%q, %v) then (%q, %v)",
					kind, carries, late, lateCarries)
			}
		})
	}
}

func TestForcedEpochLive(t *testing.T) {
	cases := []struct {
		name   string
		member Member
		want   bool
	}{
		{"a member that was never force-stopped is not under a forced epoch",
			Member{StoppingSince: 1000}, false},
		{"a force-stop record with no open stop epoch is not a live one",
			Member{ForcedStopAt: 1000}, false},
		{"a force-stop stamped after this epoch opened is live",
			Member{StoppingSince: 1000, ForcedStopAt: 1001}, true},
		{"a force-stop stamped on the same tick as the epoch is live",
			Member{StoppingSince: 1000, ForcedStopAt: 1000}, true},
		{"a force-stop that predates this epoch belongs to an earlier session",
			Member{StoppingSince: 1000, ForcedStopAt: 999}, false},
		{"a member with neither anchor is not under a forced epoch",
			Member{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := forcedEpochLive(c.member); got != c.want {
				t.Fatalf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestARefocusStampWouldReachTheAgent(t *testing.T) {
	for _, tc := range []struct {
		name          string
		desiredState  string
		wantReachable bool
	}{
		{name: "an online intent is reachable by the member", desiredState: DesiredStateOnline, wantReachable: true},
		{name: "an offline intent cannot receive a refocus stamp", desiredState: DesiredStateOffline, wantReachable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := aRefocusStampWouldReachTheAgent(Member{DesiredState: tc.desiredState}); got != tc.wantReachable {
				t.Fatalf("a refocus stamp reachability: want %v, got %v", tc.wantReachable, got)
			}
		})
	}
}

func TestAStopWasEverAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stoppingSince float64
		wantAsked     bool
	}{
		{name: "a member with no stop anchor was never asked to stop", stoppingSince: 0, wantAsked: false},
		{name: "a positive stop anchor records that the owner asked", stoppingSince: 1000, wantAsked: true},
		{name: "a negative sentinel is not a stop request", stoppingSince: -1, wantAsked: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := aStopWasEverAskedFor(Member{StoppingSince: tc.stoppingSince}); got != tc.wantAsked {
				t.Fatalf("stop request history: want %v, got %v", tc.wantAsked, got)
			}
		})
	}
}

func TestGracefulStopEpochOpen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member Member
		want   bool
	}{
		{name: "an ordinary open stop can still be worked", member: Member{StoppingSince: 1000}, want: true},
		{name: "a forced stop is already cut off", member: Member{StoppingSince: 1000, ForcedStopAt: 1000}, want: false},
		{name: "an older force-stop record does not close the current ordinary epoch", member: Member{StoppingSince: 1000, ForcedStopAt: 999}, want: true},
		{name: "a row without an open stop epoch is not graceful", member: Member{ForcedStopAt: 1000}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := gracefulStopEpochOpen(tc.member); got != tc.want {
				t.Fatalf("graceful stop epoch: want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestStopEpochAnchor(t *testing.T) {
	cases := []struct {
		name   string
		member Member
		want   float64
	}{
		{"a first stop on a clean row anchors at now", Member{}, 2000},
		{"an ordinary re-stamp moves the anchor to now",
			Member{StoppingSince: 1000}, 2000},
		{"a live forced epoch keeps the anchor it already carries",
			Member{StoppingSince: 1000, ForcedStopAt: 1000}, 1000},
		{"a force-stop older than this epoch does not hold the anchor back",
			Member{StoppingSince: 1000, ForcedStopAt: 999}, 2000},
		{"a force-stop record with no open epoch does not hold the anchor back",
			Member{ForcedStopAt: 1000}, 2000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stopEpochAnchor(c.member, 2000); got != c.want {
				t.Fatalf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestOffboardNoticeFor(t *testing.T) {
	t.Run("the soft kind answers the 停止 document verbatim", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000

		if got := api.offboardNoticeFor(m, offboardKindSoft); got != apiTestOffboardNotice {
			t.Fatalf("soft notice: %q", got)
		}
	})

	t.Run("the final kind answers the 加速停止 document with this epoch's deadline rendered", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000
		m.RefocusOp = refocusOpAcceleratedStop

		if got := api.offboardNoticeFor(m, offboardKindFinal); got != apiTestAcceleratedNotice {
			t.Fatalf("final notice: %q", got)
		}
	})

	t.Run("a final call with no clock behind it renders nothing at all", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.RefocusOp = refocusOpAcceleratedStop

		if got := api.offboardNoticeFor(m, offboardKindFinal); got != "" {
			t.Fatalf("want an empty notice, got %q", got)
		}
	})

	t.Run("an outsource member bound to a typed task carries the write-back clause after the document", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		w := apiTestTypedTaskWorker(t, api, h, d, owner, "delivery")

		apiTestSentinelWindDownDocs(t, api)

		want := "OFFBOARD-DOC-SENTINEL\n\nCLOSEOUT-BODY-SENTINEL"
		if got := api.offboardNoticeFor(w, offboardKindSoft); got != want {
			t.Fatalf("outsource notice: %q, want %q", got, want)
		}
	})

	t.Run("a staff member is asked for no write-back clause", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		if got := api.offboardNoticeFor(apiTestMemberRow(t, d, "kip"), offboardKindSoft); got != apiTestOffboardNotice {
			t.Fatalf("staff notice: %q", got)
		}
	})
}

func TestOffboardManualWriteBackFor(t *testing.T) {
	t.Run("an outsource member bound to a typed task is given the 任務結案 body", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		w := apiTestTypedTaskWorker(t, api, h, d, owner, "delivery")

		apiTestSentinelWindDownDocs(t, api)

		if got := api.offboardManualWriteBackFor(w); got != "CLOSEOUT-BODY-SENTINEL" {
			t.Fatalf("clause: %q", got)
		}
	})

	t.Run("an outsource member whose task has no type is asked for nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		w := apiTestTypedTaskWorker(t, api, h, d, owner, "")

		if got := api.offboardManualWriteBackFor(w); got != "" {
			t.Fatalf("want no clause, got %q", got)
		}
	})

	t.Run("an outsource member bound to no task at all is asked for nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		w := apiTestTypedTaskWorker(t, api, h, d, owner, "delivery")
		unbound := w
		unbound.LinkedTaskID = nil

		if got := api.offboardManualWriteBackFor(unbound); got != "" {
			t.Fatalf("want no clause, got %q", got)
		}
		empty := ""
		unbound.LinkedTaskID = &empty
		if got := api.offboardManualWriteBackFor(unbound); got != "" {
			t.Fatalf("want no clause, got %q", got)
		}
	})

	t.Run("a staff member bound to the same typed task is asked for nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		w := apiTestTypedTaskWorker(t, api, h, d, owner, "delivery")
		staff := apiTestMemberRow(t, d, "kip")
		staff.LinkedTaskID = w.LinkedTaskID

		if got := api.offboardManualWriteBackFor(staff); got != "" {
			t.Fatalf("want no clause, got %q", got)
		}
	})

	t.Run("an outsource member whose task row is gone is asked for nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		w := apiTestTypedTaskWorker(t, api, h, d, owner, "delivery")
		missing := "T-does-not-exist"
		w.LinkedTaskID = &missing

		if got := api.offboardManualWriteBackFor(w); got != "" {
			t.Fatalf("want no clause, got %q", got)
		}
	})
}

func TestResolveAvatarMember(t *testing.T) {
	t.Run("an active staff row is answered whole", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		got, err := api.resolveAvatarMember("kip")
		if err != nil {
			t.Fatalf("resolveAvatarMember: %v", err)
		}
		apiTestWantEqual(t, "member", *got, apiTestMemberRow(t, d, "kip"))
	})

	t.Run("a warden row is answered too, so the refusal is the handler's and not this resolver's", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		got, err := api.resolveAvatarMember("m-server-self")
		if err != nil {
			t.Fatalf("resolveAvatarMember: %v", err)
		}
		apiTestWantEqual(t, "member", *got, apiTestMemberRow(t, d, "m-server-self"))
		if got.Kind != KindWarden {
			t.Fatalf("want a warden row, got kind %q", got.Kind)
		}
	})

	t.Run("an outsource row is answered whole", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)

		got, err := api.resolveAvatarMember("ow-abc123")
		if err != nil {
			t.Fatalf("resolveAvatarMember: %v", err)
		}
		apiTestWantEqual(t, "member", *got, apiTestMemberRow(t, d, "ow-abc123"))
	})

	t.Run("a dismissed row is not found", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}

		got, err := api.resolveAvatarMember("kip")
		if got != nil || !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound and no member, got %v %v", got, err)
		}
	})

	t.Run("an id the roster never carried is not found", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got, err := api.resolveAvatarMember("nope")
		if got != nil || !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound and no member, got %v %v", got, err)
		}
	})
}

func TestPublishMemberAvatarChanged(t *testing.T) {
	t.Run("a staff member's change rides the member topic to the dashboard and to that member alone", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.publishMemberAvatarChanged(m, "owner")

		frame := apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "owner")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a staff member under a graceful stop carries the wind-down sentence with it", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		m := apiTestMemberRow(t, d, "kip")
		m.DesiredState = DesiredStateOffline
		m.StoppingSince = 1000
		dashboard := apiTestListen(t, api, "")

		api.publishMemberAvatarChanged(m, "owner")

		payload := apiTestMemberPayload("kip", "Kip", "active", "offline")
		payload["offboard_notice"] = apiTestOffboardNotice
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "kip", payload, "owner"))
	})

	t.Run("an outsource member's change rides the outsource_worker topic and reaches the owner alone", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		m := apiTestMemberRow(t, d, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "ow-abc123")

		api.publishMemberAvatarChanged(m, "owner")

		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "owner"))
		self.wantFrames()
	})
}

func TestMemberAvatarResult(t *testing.T) {
	decode := func(t *testing.T, dto MemberAvatarDTO) map[string]any {
		t.Helper()
		raw, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return got
	}

	t.Run("a stored avatar answers its minted address with the mime and filename beside it", func(t *testing.T) {
		filename := "portrait.png"
		got := decode(t, memberAvatarResult(
			Member{ID: "kip", AvatarAttachmentID: "ava-0001"}, "image/png", &filename))

		apiWantBody(t, got, map[string]any{
			"member_id":  "kip",
			"avatar_url": "/api/chat/attachment/ava-0001",
			"mime":       "image/png",
			"filename":   "portrait.png",
		})
	})

	t.Run("a member carrying no avatar answers the empty address", func(t *testing.T) {
		got := decode(t, memberAvatarResult(Member{ID: "kip"}, "", nil))

		apiWantBody(t, got, map[string]any{"member_id": "kip", "avatar_url": ""})
	})

	t.Run("a blank mime omits the key while a filename left nil omits its own", func(t *testing.T) {
		got := decode(t, memberAvatarResult(
			Member{ID: "kip", AvatarAttachmentID: "ava-0002"}, "", nil))

		apiWantBody(t, got, map[string]any{
			"member_id":  "kip",
			"avatar_url": "/api/chat/attachment/ava-0002",
		})
	})
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
				"terminal_attach_command": "tmux -L officraft attach -t member-kip",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-mira",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-m-server-self",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-kip",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-mira",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-m-server-self",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-mira",
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
				"terminal_attach_command": "tmux -L officraft attach -t member-m-server-self",
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
			"terminal_attach_command": "tmux -L officraft attach -t member-kip",
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
			"terminal_attach_command": "tmux -L officraft attach -t member-ow-abc123",
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

	t.Run("every level the effort vocabulary accepts is stored, and one outside it answers 422 storing nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
			status, data := apiJSON(t, h, "PATCH", "/api/members/kip", owner,
				`{"effort":"`+level+`"}`)
			if status != 200 {
				t.Fatalf("effort %q: want 200, got %d (%v)", level, status, data)
			}
			apiWantBody(t, data, map[string]any{"id": "kip"})
			status, stored := apiJSON(t, h, "GET", "/api/members/kip", owner, "")
			if status != 200 {
				t.Fatalf("effort %q read back: want 200, got %d (%v)", level, status, stored)
			}
			if stored["effort"] != level {
				t.Fatalf("effort %q: stored effort = %#v", level, stored["effort"])
			}
		}

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip", owner, `{"effort":"turbo"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"effort must be one of [high low max medium xhigh]; got 'turbo'")
		status, stored := apiJSON(t, h, "GET", "/api/members/kip", owner, "")
		if status != 200 {
			t.Fatalf("read back after the refusal: want 200, got %d (%v)", status, stored)
		}
		if stored["effort"] != "max" {
			t.Fatalf("the refused level was stored anyway: effort = %#v", stored["effort"])
		}
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
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":                 "kip",
			"activation_pending": true,
			"last_op_reason":     apiAnyString,
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
					"desired_state": "online",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		memberPayload := apiTestMemberPayload("kip", "Kip", "active", "online")
		dashboard.wantFrames(frame,
			apiTestMemberFrame(2, "patch", "kip", memberPayload, "server"),
			apiTestMemberFrame(3, "patch", "kip", memberPayload, "server"))
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
	t.Run("the sentence names the verb that was saved and the 活化 that would start it", func(t *testing.T) {
		want := "held_down: the 重新聚焦 was saved, but nothing was started — " +
			"this member is stopped; 活化 it when you want it to run"
		if got := memberHeldDownReceipt("重新聚焦"); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	})

	t.Run("a different verb changes only the verb", func(t *testing.T) {
		want := "held_down: the 改機器 was saved, but nothing was started — " +
			"this member is stopped; 活化 it when you want it to run"
		if got := memberHeldDownReceipt("改機器"); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	})

	t.Run("an empty verb still leaves the held-down reason readable", func(t *testing.T) {
		want := "held_down: the  was saved, but nothing was started — " +
			"this member is stopped; 活化 it when you want it to run"
		if got := memberHeldDownReceipt(""); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	})
}

func TestClearMemberHandoverMarker(t *testing.T) {
	t.Run("the 換手 epoch is zeroed and every other anchor on the row is left alone", func(t *testing.T) {
		m := Member{ID: "kip", Name: "Kip", Kind: KindStaff, RosterStatus: RosterStatusActive,
			DesiredState: DesiredStateOffline, RefocusSince: 1000, RefocusOp: refocusOpRefocus,
			StoppingSince: 1100, StoppedSince: 1200, ForcedStopAt: 900,
			RestartAfterStop: true, WakingSince: 800, SessionBootTS: 700}

		clearMemberHandoverMarker(&m)

		apiTestWantEqual(t, "member", m, Member{ID: "kip", Name: "Kip", Kind: KindStaff,
			RosterStatus: RosterStatusActive, DesiredState: DesiredStateOffline,
			RefocusSince: 0, RefocusOp: "", StoppingSince: 1100, StoppedSince: 1200,
			ForcedStopAt: 900, RestartAfterStop: true, WakingSince: 800, SessionBootTS: 700})
	})

	t.Run("a row that carries no 換手 epoch is left exactly as it stood", func(t *testing.T) {
		m := Member{ID: "kip", Kind: KindStaff, StoppingSince: 1100, ForcedStopAt: 900}

		clearMemberHandoverMarker(&m)

		apiTestWantEqual(t, "member", m, Member{ID: "kip", Kind: KindStaff,
			StoppingSince: 1100, ForcedStopAt: 900})
	})
}

func TestStopVerbRowOfMember(t *testing.T) {
	t.Run("the five pointers name the member's own 停止 columns", func(t *testing.T) {
		m := Member{ID: "kip", Kind: KindStaff, DesiredState: DesiredStateOnline,
			RefocusSince: 3, RefocusOp: refocusOpRefocus, RestartAfterStop: true,
			StoppingSince: 1}
		row := stopVerbRowOfMember(&m)

		if *row.DesiredState != DesiredStateOnline || *row.RefocusSince != 3 ||
			*row.RefocusOp != refocusOpRefocus || !*row.RestartAfterStop ||
			*row.StoppingSince != 1 {
			t.Fatalf("row reads %q %v %q %v %v", *row.DesiredState, *row.RefocusSince,
				*row.RefocusOp, *row.RestartAfterStop, *row.StoppingSince)
		}

		*row.DesiredState = DesiredStateOffline
		*row.RefocusSince = 30
		*row.RefocusOp = refocusOpAcceleratedStop
		*row.RestartAfterStop = false
		*row.StoppingSince = 10
		apiTestWantEqual(t, "member", m, Member{ID: "kip", Kind: KindStaff,
			DesiredState: DesiredStateOffline, RefocusSince: 30,
			RefocusOp: refocusOpAcceleratedStop, RestartAfterStop: false,
			StoppingSince: 10})
	})

	t.Run("a row taken over a copy leaves the original member untouched", func(t *testing.T) {
		original := Member{ID: "kip", Kind: KindStaff, DesiredState: DesiredStateOnline,
			RestartAfterStop: true, StoppingSince: 1}
		copied := original
		row := stopVerbRowOfMember(&copied)
		*row.DesiredState = DesiredStateOffline
		*row.RestartAfterStop = false

		apiTestWantEqual(t, "the original", original, Member{ID: "kip", Kind: KindStaff,
			DesiredState: DesiredStateOnline, RestartAfterStop: true, StoppingSince: 1})
		apiTestWantEqual(t, "the copy", copied, Member{ID: "kip", Kind: KindStaff,
			DesiredState: DesiredStateOffline, RestartAfterStop: false, StoppingSince: 1})
	})
}

func TestStopVerbRowOfWorker(t *testing.T) {
	t.Run("the five pointers name the worker's own 停止 columns", func(t *testing.T) {
		w := OutsourceWorker{ID: "ow-abc123", Codename: "Contractor",
			DesiredState: DesiredStateOnline, RefocusSince: 3, RefocusOp: refocusOpRefocus,
			RestartAfterStop: true, StoppingSince: 1}
		row := stopVerbRowOfWorker(&w)

		if *row.DesiredState != DesiredStateOnline || *row.RefocusSince != 3 ||
			*row.RefocusOp != refocusOpRefocus || !*row.RestartAfterStop ||
			*row.StoppingSince != 1 {
			t.Fatalf("row reads %q %v %q %v %v", *row.DesiredState, *row.RefocusSince,
				*row.RefocusOp, *row.RestartAfterStop, *row.StoppingSince)
		}

		*row.DesiredState = DesiredStateOffline
		*row.RefocusSince = 30
		*row.RefocusOp = refocusOpAcceleratedStop
		*row.RestartAfterStop = false
		*row.StoppingSince = 10
		apiTestWantEqual(t, "worker", w, OutsourceWorker{ID: "ow-abc123",
			Codename: "Contractor", DesiredState: DesiredStateOffline, RefocusSince: 30,
			RefocusOp: refocusOpAcceleratedStop, RestartAfterStop: false,
			StoppingSince: 10})
	})

	t.Run("a row taken over a copy leaves the original worker untouched", func(t *testing.T) {
		original := OutsourceWorker{ID: "ow-abc123", DesiredState: DesiredStateOnline,
			RestartAfterStop: true, StoppingSince: 1}
		copied := original
		row := stopVerbRowOfWorker(&copied)
		*row.StoppingSince = 10
		*row.RefocusOp = refocusOpRefocus

		apiTestWantEqual(t, "the original", original, OutsourceWorker{ID: "ow-abc123",
			DesiredState: DesiredStateOnline, RestartAfterStop: true, StoppingSince: 1})
		apiTestWantEqual(t, "the copy", copied, OutsourceWorker{ID: "ow-abc123",
			DesiredState: DesiredStateOnline, RestartAfterStop: true, StoppingSince: 10,
			RefocusOp: refocusOpRefocus})
	})
}

func TestApplyStopVerbRow(t *testing.T) {
	t.Run("a 停止 on a running member writes offline, clears the 換手 epoch and the queued restart, and anchors at now", func(t *testing.T) {
		m := Member{ID: "kip", Name: "Kip", Kind: KindStaff, RosterStatus: RosterStatusActive,
			DesiredState: DesiredStateOnline, RefocusSince: 900, RefocusOp: refocusOpRefocus,
			RestartAfterStop: true, StoppedSince: 800, WakingSince: 700}

		applyStopVerbRow(stopVerbRowOfMember(&m), m, 2000)

		apiTestWantEqual(t, "member", m, Member{ID: "kip", Name: "Kip", Kind: KindStaff,
			RosterStatus: RosterStatusActive, DesiredState: DesiredStateOffline,
			RefocusSince: 0, RefocusOp: "", RestartAfterStop: false,
			StoppingSince: 2000, StoppedSince: 800, WakingSince: 700})
	})

	t.Run("a 停止 landing on a live forced epoch leaves that epoch's anchor where it stands", func(t *testing.T) {
		m := Member{ID: "kip", Kind: KindStaff, DesiredState: DesiredStateOnline,
			StoppingSince: 1000, ForcedStopAt: 1000, RestartAfterStop: true}

		applyStopVerbRow(stopVerbRowOfMember(&m), m, 2000)

		apiTestWantEqual(t, "member", m, Member{ID: "kip", Kind: KindStaff,
			DesiredState: DesiredStateOffline, StoppingSince: 1000, ForcedStopAt: 1000,
			RestartAfterStop: false})
	})

	t.Run("the anchor is decided by the snapshot rather than by the row being written", func(t *testing.T) {
		m := Member{ID: "kip", Kind: KindStaff}
		snapshot := Member{ID: "kip", Kind: KindStaff, StoppingSince: 1000, ForcedStopAt: 1000}

		applyStopVerbRow(stopVerbRowOfMember(&m), snapshot, 2000)

		apiTestWantEqual(t, "member", m, Member{ID: "kip", Kind: KindStaff,
			DesiredState: DesiredStateOffline, StoppingSince: 1000})
	})

	t.Run("the worker face writes the same five columns", func(t *testing.T) {
		w := OutsourceWorker{ID: "ow-abc123", Codename: "Contractor",
			Status: WorkerStatusActive, DesiredState: DesiredStateOnline,
			RefocusSince: 900, RefocusOp: refocusOpRefocus, RestartAfterStop: true,
			StoppedSince: 800}

		applyStopVerbRow(stopVerbRowOfWorker(&w), memberFromWorker(w), 2000)

		apiTestWantEqual(t, "worker", w, OutsourceWorker{ID: "ow-abc123",
			Codename: "Contractor", Status: WorkerStatusActive,
			DesiredState: DesiredStateOffline, RefocusSince: 0, RefocusOp: "",
			RestartAfterStop: false, StoppingSince: 2000, StoppedSince: 800})
	})
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
		if !forcedEpochLive(*m) {
			t.Fatalf("the persisted anchors must identify a live forced epoch, got stopping=%v forced=%v",
				m.StoppingSince, m.ForcedStopAt)
		}
	})

	t.Run("a repeated force-stop never moves the durable cut-off record backwards", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, `{}`); status != 200 {
			t.Fatalf("first force-stop: %d %v", status, data)
		}
		first, err := d.GetMember("kip")
		if err != nil || first == nil {
			t.Fatalf("first GetMember: %v (%v)", first, err)
		}

		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, `{}`); status != 200 {
			t.Fatalf("second force-stop: %d %v", status, data)
		}
		second, err := d.GetMember("kip")
		if err != nil || second == nil {
			t.Fatalf("second GetMember: %v (%v)", second, err)
		}
		if second.ForcedStopAt < first.ForcedStopAt {
			t.Fatalf("the cut-off record moved backwards: first=%v second=%v",
				first.ForcedStopAt, second.ForcedStopAt)
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
	t.Run("an agent's own live row is answered whole", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		r := apiTestAuthedRequest(t, api, d, apiTestAgentToken(t, api, "kip", ""))

		got, err := api.resolveSelf(r)
		if err != nil {
			t.Fatalf("resolveSelf: %v", err)
		}
		apiTestWantEqual(t, "member", *got, apiTestMemberRow(t, d, "kip"))
	})

	t.Run("an outsource worker's own row is answered rather than folded away", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		r := apiTestAuthedRequest(t, api, d, apiTestAgentToken(t, api, "ow-abc123", ""))

		got, err := api.resolveSelf(r)
		if err != nil {
			t.Fatalf("resolveSelf: %v", err)
		}
		apiTestWantEqual(t, "member", *got, apiTestMemberRow(t, d, "ow-abc123"))
	})

	t.Run("the owner has no roster row of its own, so its token resolves to not found", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		r := apiTestAuthedRequest(t, api, d, owner)

		got, err := api.resolveSelf(r)
		if got != nil || !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound and no member, got %v %v", got, err)
		}
	})

	t.Run("a member dismissed after its credential was issued resolves to not found", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		r := apiTestAuthedRequest(t, api, d, apiTestAgentToken(t, api, "kip", ""))
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}

		got, err := api.resolveSelf(r)
		if got != nil || !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound and no member, got %v %v", got, err)
		}
	})
}

func TestStampAgentIatFloor(t *testing.T) {
	t.Run("the floor lands on the caller's own iat and no other column moves", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		issued := time.Now().Unix() - 100
		token, err := mintJWT("kip", "agent", 3600, api.keys.signingSecret(), issued, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		r := apiTestAuthedRequest(t, api, d, token)

		if err := api.stampAgentIatFloor(r); err != nil {
			t.Fatalf("stampAgentIatFloor: %v", err)
		}

		want := before
		want.AgentIatFloor = float64(issued)
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, "kip"), want)
		dashboard.wantFrames()
	})

	t.Run("a later credential raises the floor and an earlier one cannot lower it", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		early := time.Now().Unix() - 100
		late := time.Now().Unix()
		earlyToken, err := mintJWT("kip", "agent", 3600, api.keys.signingSecret(), early, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		lateToken, err := mintJWT("kip", "agent", 3600, api.keys.signingSecret(), late, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
		earlyRequest := apiTestAuthedRequest(t, api, d, earlyToken)
		lateRequest := apiTestAuthedRequest(t, api, d, lateToken)

		if err := api.stampAgentIatFloor(lateRequest); err != nil {
			t.Fatalf("stampAgentIatFloor: %v", err)
		}
		if got := apiTestMemberRow(t, d, "kip").AgentIatFloor; got != float64(late) {
			t.Fatalf("want the floor at %v, got %v", late, got)
		}
		if err := api.stampAgentIatFloor(earlyRequest); err != nil {
			t.Fatalf("stampAgentIatFloor: %v", err)
		}
		if got := apiTestMemberRow(t, d, "kip").AgentIatFloor; got != float64(late) {
			t.Fatalf("the floor moved backwards to %v", got)
		}
	})

	t.Run("the floor stamped for one member leaves every other roster row alone", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		mira := apiTestMemberRow(t, d, "mira")
		warden := apiTestMemberRow(t, d, "m-server-self")
		r := apiTestAuthedRequest(t, api, d, apiTestAgentToken(t, api, "kip", ""))

		if err := api.stampAgentIatFloor(r); err != nil {
			t.Fatalf("stampAgentIatFloor: %v", err)
		}

		apiTestWantEqual(t, "mira", apiTestMemberRow(t, d, "mira"), mira)
		apiTestWantEqual(t, "the warden", apiTestMemberRow(t, d, "m-server-self"), warden)
	})
}

func TestHandleReportWakingApiSelfWakingPost(t *testing.T) {
	t.Run("a boot report stamps the wake, stores the reported model and answers the caller's standing intent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		issued := time.Now().Unix() - 100
		agent, err := mintJWT("kip", "agent", 3600, api.keys.signingSecret(), issued, "")
		if err != nil {
			t.Fatalf("mintJWT: %v", err)
		}
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
		if m.AgentIatFloor != float64(issued) {
			t.Fatalf("the credential floor must equal the waking token's iat %d, got %v", issued, m.AgentIatFloor)
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
