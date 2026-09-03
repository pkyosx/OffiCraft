package main

// lore_gate.go — T-33. The station-wide LORE feature switch, on the ROUTE side.
//
// 🔴 WHAT THE SWITCH IS, IN THE OWNER'S OWN TERMS. 「我們可以有個功能切換鈕嗎
// 打開的話 Lore 才能被讀寫以及顯示在 UI 上」、「預設是關閉起來的」、
// 「功能關閉的狀況下 他無法寫入，我們 resume summary 也不會給他 對象清單」.
// One switch, one station (「我們可以在自己的 site 上打開這個功能優先體驗一陣子」),
// default OFF, and OFF means the feature is not there.
//
// 🔴 「FALLBACK 到原本的 LEARNING / LESSON」 IS NOT A CODE PATH, AND NOTHING IN
// THIS FILE IMPLEMENTS ONE. It is what an agent DOES when this gate refuses it:
// it goes back to patch_lessons / write_task_learnings, the tools it has always
// had and which are untouched by this switch. The station never carries a
// memory from one store into another.
//
// ⚠️ WHY THAT DISTINCTION IS LOAD-BEARING RATHER THAN PEDANTIC. A real transfer
// would have to WRITE into a document that has its own character cap, so it can
// fail — and a failure would strand a memory that is in neither store while the
// call that triggered it has already returned success. Every question that
// follows (retry? drop it? truncate whose text?) exists only if the transfer
// does. Read the owner's sentence the other way and the whole problem set is
// gone.
//
// 🔴 THE REFUSAL SPEAKS. It is not a 404, not a silent empty result, and not a
// bare "forbidden": it names the feature, names the setting, says the write did
// not happen, and says what to do instead. An agent that gets an unexplained
// error concludes it malformed its own request and RETRIES — and while the
// switch is off, every retry fails identically. Wording that removes the reason
// to retry is the only thing that stops that loop.

import (
	"net/http"
)

// loreDisabledMessage is the ONE wording of the refusal, in one place so every
// gated route says the same sentence.
//
// 🔴 IT IS BILINGUAL BY CONSTRUCTION, NOT BY TRANSLATION. The station's members
// read Chinese and the setting key / endpoint are ASCII identifiers; spelling
// both out means the reader learns the exact string an owner has to change,
// rather than a description of it.
//
// 🔴 IT NAMES THE ALTERNATIVE. Telling an agent only that lore is off leaves it
// holding something it just learned with nowhere to put it, which is how a
// refusal turns into lost knowledge — the tools it already had are the answer,
// and they are said out loud here so the answer arrives with the refusal.
const loreDisabledMessage = "傳承（lore）功能在這個站目前是關閉的" +
	"（設定 lore.enabled = false，預設就是關的）。" +
	"這不是你的請求寫錯了，也不是暫時性的錯誤：只要開關還關著，重試永遠會得到這一句。" +
	"你剛才要寫的東西沒有被寫進去。" +
	"請改用你原本就有的 learning / lesson 工具" +
	"（patch_lessons、write_task_learnings、patch_task_learnings）把它記下來。" +
	"要打開這個功能的是站長：PATCH /api/settings {\"lore_enabled\": true}" +
	"（update_settings），打開之後下一次呼叫就會生效。" +
	" | The lore feature is switched OFF on this station (setting lore.enabled = " +
	"false, which is the default). This is not a malformed request and retrying " +
	"will never succeed while the switch is off; nothing was written. Use the " +
	"learning / lesson tools you already have instead. Only the owner can turn it " +
	"on, with PATCH /api/settings {\"lore_enabled\": true}."

// loreFeatureGate wraps ONE lore route so it answers only while the feature is
// switched on.
//
// 🔴 IT READS THE SWITCH ON EVERY REQUEST, and that is the promise being kept:
// the owner was told 「你一開，他們當下就寫得進去」. loreEnabledSnapshot() reads
// the live in-memory value that PATCH /api/settings updates under settingsMu in
// the same critical section as the DB write, so the request AFTER the save sees
// the new value. Capturing the flag when the table is built — the obvious
// cheaper thing — would mean the switch only took effect on restart, which is
// the opposite of what was promised and would look identical from outside until
// somebody actually tried it.
//
// 403 rather than 404: the route EXISTS and the caller's identity is fine; what
// is missing is a station-level permission to use this feature at all. A 404
// would say the endpoint is not a thing, which would send a reader looking for a
// version mismatch instead of at a setting.
func (s *apiServer) loreFeatureGate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.loreEnabledSnapshot() {
			writeError(w, http.StatusForbidden, loreDisabledMessage)
			return
		}
		next(w, r)
	}
}
