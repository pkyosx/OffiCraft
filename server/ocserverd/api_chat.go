package main

// api_chat.go — the chat surface (handlers.handle_post_chat …
// handle_list_chat_reads): sender ALWAYS the verified JWT sub, attachments
// decoded/validated all-or-nothing before any blob is stored, the light
// meta["attachments"] refs the only message→blob linkage, and the monotonic
// per-conversation read watermark.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	chatListDefaultLimit        = 30
	chatAttachmentImageMaxBytes = 20 * 1024 * 1024
	chatAttachmentMaxBytes      = 100 * 1024 * 1024
	chatAttachmentsMaxCount     = 10
	// ── the wake snapshot's chat budget ──────────────────────────────────────
	//
	// The snapshot used to take a FIXED 30 newest messages and cut every body
	// at 500 runes, on every line alike. Two things were wrong with that and
	// they are separate faults:
	//
	//  1. A fixed message COUNT does not bound the payload — 30 messages is
	//     600 characters or 60,000 depending on who was talking — and it does
	//     not spend the room where it is worth spending. What a waking agent
	//     needs is a CHARACTER budget, filled newest-first.
	//  2. ~~One global newest-N has no notion of a conversation LINE~~ — this
	//     used to be listed as the second fault, and the per-line FLOOR built to
	//     answer it is what the owner removed on 2026-08-13. The floor's reserved
	//     messages were billed to the budget and then never evicted by it, so the
	//     block grew without any upper bound at all: measured on one real member,
	//     chat_chars 80,140 across 496 messages (~165 lines × 3) against the
	//     ceiling of the day — 6.7× over, paid by every agent on every single
	//     wake. Owner ruling, verbatim: 「不要管每條對話線」/「只管從最新一則訊息
	//     往前推,直到超出我們 budget 上限前最後一則」. So the budget is now a real
	//     CEILING: global newest-first, stop at the last message that still fits.
	//
	// The total the chat block may spend is counted in RUNES — one CJK character
	// is one, matching every other cap in this file (see chatBodyMaxChars).
	// 🔴 It covers EVERYTHING overview.chat_chars counts: the messages and their
	// folded cards, plus the snapshot header and the cut hint that ride outside
	// the array. Nothing is exempt from it any more — that exemption WAS the
	// defect. It is a budget, not a target: a quiet studio produces a much
	// smaller payload.
	//
	// 🔴 THE NUMBER ITSELF NO LONGER LIVES HERE. It was the constant 8000 (owner
	// 2026-08-13, 「聊天區塊字數留 8000 就好」) until T-c9b4 made it the
	// `chat.budget_chars` SETTING — default chatBudgetCharsDefault, adjustable
	// from the settings page between minChatBudgetChars and maxChatBudgetChars
	// (domain.go). There is exactly one source: the chat.budget_chars setting, read at
	// request time by resumeSnapshotParts (s.chatBudget()) and handed to
	// resumeChatPackBudget.
	// Do not reintroduce a constant here — a second copy is how the peek and the
	// snapshot start disagreeing about how big the block is.
	//
	// resumeChatFetch caps how many of the caller's newest messages are READ
	// before packing — GLOBALLY, not per conversation line (see
	// ListChatInvolving). It bounds the read itself so a busy studio cannot drag
	// its whole history into memory just to have it dropped by the budget.
	//
	// 🔴 WHY 500, and why the number is a FLOOR-derived one rather than a taste
	// call: the packer must never run out of candidates before it runs out of
	// budget, or the snapshot would silently under-fill. The CHEAPEST possible
	// message costs 27 runes (resumeChatMessageChars counts ts_display — always
	// 26 runes of resumeTimeLayout — plus at least one rune for the "0" of
	// body_omitted_chars; body and both names can be empty). 500 × 27 = 13,500 >
	// maxChatBudgetChars — the CEILING of the adjustable budget, not the default,
	// because the guarantee has to hold at every value the owner can dial in. That
	// is exactly why maxChatBudgetChars is 13000 and not higher; raising it past
	// 13,500 means raising this number FIRST. Even an all-minimum stream therefore
	// fills the budget with rows to spare. Realistic messages cost far more, so the loop almost always
	// stops on the budget long before this cap.
	//
	// 🔴 It replaces a PER-LINE quota of 40. On the measured member (~165 lines)
	// that read 6,600 rows to fill one budget's worth; this reads at most 500. It is
	// still ONE query and still a full chat_message scan (sender/recipient carry
	// no index) — what changed is how many rows come back.
	//
	// A caller whose stream fills this cap always reports the cut through
	// chat_earlier_omitted.
	resumeChatFetch = 500
	// resumeChatOtherPreview is where ANOTHER AGENT's message body is COLLAPSED.
	// 120 runes is a lead, not a summary: enough to recognise which exchange
	// this is and decide whether to go and read it, which is all a third party's
	// traffic has to do for a wake.
	//
	// 🔴 TWO LINES ARE EXEMPT and carried IN FULL, because on those two the
	// content IS the wake:
	//   - the SELF hand-off (sender == recipient == the subject). An agent's
	//     baton to its own next session is literally a post_chat to itself;
	//     collapsing it truncates the handover instructions the wake exists to
	//     resume from.
	//   - anything to or from the owner (wireOwnerID). Instructions from the
	//     human, and what was answered back, are not summarisable material.
	// Both exemptions are applied in resumeChatCarriesFullBody.
	resumeChatOtherPreview = 120
	// resumeTimeLayout renders an epoch second for a READER: full date, full
	// time, and the zone OFFSET, e.g. "2026-08-13 09:47:11 +08:00".
	//
	// The offset is in the string because there is no timezone SETTING anywhere
	// in the studio to read it from — the server's own local zone is the only
	// zone that exists here, and a local time printed without its offset cannot
	// be interpreted by anyone who is not this process. Introducing a timezone
	// setting to avoid that is a governance change (another thing the owner must
	// set, another thing that can be wrong), not a formatting one.
	//
	// 🔴 The DATE IS ALWAYS WRITTEN, same-day messages included. "Drop the date
	// when it is today" is the obvious-looking optimisation and it is wrong
	// here: the reader is an agent that just woke up and does not know what day
	// it is, so a bare "09:47:11" is unreadable to exactly the audience this
	// field was added for. generated_at is the anchor it reads these against.
	resumeTimeLayout = "2006-01-02 15:04:05 -07:00"
	// resumeOwnerDisplayName is the owner's name in a wake snapshot. The owner
	// has NO roster row, so it cannot be resolved like every other id and must
	// be special-cased somewhere; this is that somewhere.
	//
	// It is deliberately NOT the owner.name setting: settings.go states that
	// nickname is not an agent read path, and turning it into one would both
	// reverse that and add a settings query to the path every agent runs on
	// every wake. What a member needs from this field is WHO IT IS TALKING TO,
	// and that is the role, not the human's chosen nickname.
	resumeOwnerDisplayName = "Owner"
	// resumeChatCutHint tells a reader how to get back what chat_earlier_omitted
	// says MAY be missing. It names the tool AND the exact parameter pairing
	// because the reader is mid-wake with nothing loaded: a hint that requires
	// looking something up is not a hint. The 422-on-one-of-two is stated
	// because that is the failure a first attempt actually hits (see
	// HandleListChatApiChatGet).
	//
	// 🔴 It says MAY, not DOES. The marker is raised when the read filled its
	// window as well as when the budget stopped the pack, and the read never
	// looks past the window — so a caller holding exactly resumeChatFetch
	// messages and nothing older raises it too (see resumeChatBlock for why that
	// one-sidedness is the right side to err on). Wording it as a fact would
	// make this text false in exactly that case.
	resumeChatCutHint = "這條線上**可能**還有更早的訊息沒被帶進來。它在讀取或字數上限被切斷，而沒有人往切口後面看過——所以就算其實沒有更舊的，這一句也會出現；只有真的去抓才知道。（這跟 `body_omitted_chars` 是**兩回事**：那個是「這一則就在這裡，只是被摺短了」，確定的事實；這一句講的是「整則整則可能不在」，是個可能。）要確認並讀回來：呼叫 `get_chat`，`with` 填對方的 id，再把這份資料裡「對方那條線最舊的那一則」的 `before_ts` 與 `before_id` 一起帶上。這兩個游標欄位**必須成對送**，只送一個會被退回（422）。如果某個人的**整條線一則都不在**這份資料裡（他最後一則太舊，整條被擠出去了），那就沒有游標可抄——這時只填 `with`、不帶游標，直接從最新的往回讀，並**一起帶上 `peek`**（值要填字串 `true`，填別的會被當成沒填、而且不會報錯）：不帶 `peek` 的那條路會順手把那條線標成已讀（連你還沒看過的新訊息一起），而帶游標的補抓不會這樣。"
	// resumeDutyPreview caps a roster row's duty and resumeTaskTitlePreview
	// caps a contractor's task title (T-1b09). Both exist because this
	// payload is read by EVERY member on EVERY wake, so an unbounded field
	// here is paid fleet-wide, forever.
	//
	// 1000 is the owner's number (2026-08-03, verbatim: 「1000字 多的截斷」),
	// and it happens to be the SAME number he set for the cap on a duty
	// document itself — 「After separation of insight duty should not exceed
	// 1000」. ⚠️ Same origin, TWO INDEPENDENT VALUES: this one is a compiled
	// constant, that one is a SETTING (dutyCapCharsDefault is only its shipped
	// default, and the owner can raise it from the settings page). Raising the
	// setting does not — and should not — drag this constant along with it.
	//
	// 🔴 "DOES THIS CAP EVER BIND" HAS BEEN ANSWERED BOTH WAYS HERE, AND
	// NEITHER ANSWER IS A PROPERTY OF THIS CONSTANT — read this before you
	// rely on either. An earlier version of this comment called this cap "a
	// safety net that normally does not fire", and 61291d3 (2026-08-03)
	// deleted that reading as FALSE — false at the time it was written, not
	// merely overtaken later. Each of the two readings below describes only
	// the role definitions of one period; neither is a standing fact:
	//   - 2026-08-03, BEFORE the insight/operating-manual separation landed:
	//     duties still carried their manual material and the cap bound for
	//     almost every role. "Rarely fires" was a LIE on the day it was
	//     written, and the code ran permanently in the regime that phrase
	//     called exceptional.
	//     The char counts that reading was argued with are deliberately NOT
	//     restated here. The set inherited from that era contradicts itself:
	//     a longest role of 9,112 cannot fit inside "nine roles totalling
	//     ~7–8k", and neither figure agrees with the 35–4,594 range quoted
	//     alongside them. When numbers from one source disagree among
	//     themselves, the whole set goes — there is no way to tell which half
	//     happens to be true, and the most plausible-looking figure is the
	//     most dangerous one to keep. That measurement is in the past and
	//     cannot be retaken from this repo, so the counts are simply not
	//     stated; the qualitative fact above is the part this argument rests
	//     on and the part that survives.
	//   - 2026-08-04, AFTER the separation landed: a runtime `list_roles`
	//     reading of ONE instance saw 8 roles, longest 455 chars, none over
	//     the cap. So "rarely binds" was true as of that 2026-08-04
	//     measurement — a reading taken on a date, not a property of this
	//     constant, and nothing in this repo re-checks it or can re-derive it.
	// The ONLY thing that moved it between the two is that the separation
	// landed. If you are about to depend on "it rarely binds", MEASURE IT
	// AGAIN: both of the readings above were at some point written into this
	// file as if they were standing properties of the cap, and both had to be
	// corrected afterwards.
	//
	// 🔴 Do not take "it rarely fires now" as licence to lower it. That cost
	// was put to the owner WITH the numbers in rc-d88c445397a3 (an
	// independent review argued for 150–200 until the separation lands), and
	// he ruled ②: keep 1000. It is his number — do not lower it here.
	//
	// Task titles measured ~99 chars average, 147 max — five untruncated
	// contractor titles alone outweigh the whole machine block, so that one
	// stays tight.
	resumeDutyPreview      = 1000
	resumeTaskTitlePreview = 40
	// 🔴 resumeNote used to say "bodies truncated", full stop. After the
	// collapse/cut split that sentence is FALSE TWICE OVER: most bodies are not
	// shortened at all (the owner's line and your own hand-off are carried
	// whole), and the thing it did not mention — whole messages left out — is
	// the one a reader most needs told. The note now names both, in the two
	// different words the payload uses for them.
	resumeNote = "這是一份**開機快照**，不是完整資料。\n聊天：只帶最近的往來，而且是照**字數**（不是則數）收的，收到裝不下為止，由舊到新排。每則都附寄件與收件者的名字、以及帶時區的時間（請跟最上面的 `generated_at` 對照著看）；有回覆卡的會一併附上。\n有兩種「不完整」，意思不一樣，不要混：\n· `body_omitted_chars` > 0 ＝ **這一則就在這裡，只是被摺短了**，數字是被摺掉的字數，這是確定的事實（你自己寫給自己的交接、以及你跟 owner 之間的往來——他說的和你對他說的都算——一律不摺）。要看全文，把那一則的 `id` 放進 `get_chat` 的 `ids`。\n· `chat_earlier_omitted` ＝ **可能整則整則不見了**。這是「可能」不是「一定」：那條線在讀取或字數上限被切斷，而沒有人往切口後面看過，所以就算其實沒有更舊的也會標。它自己會附上怎麼去抓。\n任務：只給精簡列，沒有計畫細節。名冊：工作室裡每個人的狀態、所在機器與職責（過長會截斷，`…` 是切口）。機器：機器清單，以及你在哪一台。\n先看 `overview` 的數量與大小，再決定要拉什麼：單張任務用 `get_task`（`detail_chars` 很大的就交給分身去拉），你的卡片用 `list_reply_cards`（記得給 `limit`），要更多聊天或任務用 `list_chat`／`list_tasks`。"
	// peekNote guides the two-step boot (T-7974): peek_resume_summary_size is
	// size-only (no content); the agent reads estimated_total_chars and, when
	// it is small, calls resume_summary directly in its own context, else has
	// a cheap sub-agent (e.g. haiku) call resume_summary and hand back a
	// compressed digest — the full payload never burns the main session.
	//
	// 🔴 It states the SUM, not a list of ingredients. Three prose copies of
	// this used to itemise what the number covers, each with a DIFFERENT
	// incomplete subset, and all three left out the same item: the cut hint,
	// which resumeSnapshotParts adds to chat_chars and which is a FIXED block
	// of several hundred runes (measured 560 at the time of writing — it is a
	// constant in this file, so re-measure it there rather than trusting this
	// number). An itemised list can only go stale against that function; the
	// four addends are checkable against the code that computes them.
	peekNote                     = "Size-only preview of resume_summary — counts/sizes ONLY, no chat or task content. estimated_total_chars is exactly chat_chars + tasks_detail_chars + roster_chars + machines_chars, all four reported in overview: the WHOLE chat block as the snapshot renders it (chat_chars is the rendered block's cost, NOT the sum of the message bodies), plus the plan text its task rows omit and the two studio-floor blocks. So it is what pulling the snapshot actually costs. Use it to decide: if small (rule of thumb < 20000 chars, ≈ 5k tokens) call resume_summary directly in your main session; if large, spawn a cheap sub-agent (e.g. haiku) to call resume_summary and return a compressed digest, so the full payload never burns your own context."
	attachmentOctetStream        = "application/octet-stream"
	attachmentDefaultPastedImage = "pasted-image"
	// chatBodyMaxChars caps a chat message BODY at 4,000 UTF-8 CHARACTERS
	// (runes via utf8.RuneCountInString — NOT bytes, so 2,000 CJK chars = 6,000
	// bytes still passes). Attachments are NOT counted — long material has an
	// escape hatch: `ocagent upload` it and keep the message a short pointer.
	// Calibrated on the send-side survey (kyle-f8fe-survey.md, 3,882 messages
	// 2026-07-09..18): agent↔owner p99=1,683 (this leaves 2.4x headroom — zero
	// false positives on normal conversation); agent↔agent p99=4,894 sits just
	// above the cap, so the only tail it blocks is the ~40% of agent↔agent
	// messages that paste material (reports / baton hand-off notes) inline —
	// exactly the content that belongs in an attachment. A server constant so a
	// tighter value can follow the post-guideline distribution.
	//
	// 🔴 SCOPE: the cap is enforced by the POST /api/chat handler, NOT by the
	// write — so it binds only what arrives through that handler, and not the
	// owner even there (sender == wireOwnerID: a human is never blocked by the
	// system). Every OTHER producer of a chat message writes the row directly
	// and is unbound by it. This comment used to carry a two-item list of the
	// exempt paths; T-f059 added a third (the sched:* delivery) and nothing
	// alarmed, so the list is replaced by the query that cannot go stale:
	// `grep -n 'msg := ChatMessage{' server/ocserverd/*.go` names every writer,
	// and each one answers for its own body length.
	chatBodyMaxChars = 4000
)

// imageMimeExt maps a sniffed image mime to the default pasted-image
// extension (handlers._IMAGE_MIME_EXT).
var imageMimeExt = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// publishChatRead fans one chat_read delta for an EFFECTIVE watermark
// (repository.put_chat_read parity: key {owner}::{reader}::{peer}, payload
// {reader, peer, last_read_ts} — spec/sse.md §2.2). Callers fan ONLY when
// PutChatRead reports the watermark actually advanced — a stale/equal report
// is "no write, no fan" on the Python side.
func (s *apiServer) publishChatRead(receipt ChatRead, trigger string) {
	// No agent consumes chat_read on the wire (the ocagent listener has no
	// chat_read case); only the owner cockpit renders read receipts — owner-only.
	s.hub.Publish("chat_read", "patch", "chat_read",
		wireOwnerID+"::"+receipt.ReaderID+"::"+receipt.PeerID,
		map[string]any{
			"reader":       receipt.ReaderID,
			"peer":         receipt.PeerID,
			"last_read_ts": receipt.LastReadTS,
		}, audienceOwnerOnly(), trigger)
}

// sniffAttachmentMime is the best-effort image magic-byte sniff; a non-image
// is application/octet-stream (handlers._sniff_attachment_mime).
func sniffAttachmentMime(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(raw, []byte("GIF87a")) || bytes.HasPrefix(raw, []byte("GIF89a")):
		return "image/gif"
	case len(raw) >= 12 && bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")):
		return "image/webp"
	}
	return attachmentOctetStream
}

// chatBadRequest carries a handler-raised 400 message through the decode path.
type chatBadRequest struct{ msg string }

func (e chatBadRequest) Error() string { return e.msg }

// resolveChatRecipient accepts only a durable chat address: the single owner
// or an active AI member (staff or outsource). Presence is deliberately NOT a
// condition. An offline, waking, or stopped member still owns a mailbox and
// must receive messages posted before its next connection; a removed member,
// warden, or invented id has no chat consumer and is refused instead of
// becoming an orphaned conversation.
func (s *apiServer) resolveChatRecipient(id string) (string, error) {
	id = trimString(id)
	if id == wireOwnerID {
		return id, nil
	}
	m, err := s.dal.GetMember(id)
	if err != nil {
		return "", err
	}
	if m == nil || m.RosterStatus != RosterStatusActive ||
		(m.Kind != KindAssistant && m.Kind != KindOutsource) {
		return "", errNotFound
	}
	return id, nil
}

// decodeChatAttachment decodes one posted attachment (data-URI or bare
// base64), resolves the mime (caller → data-URI → sniff), enforces the size
// caps, and defaults a pasted image's filename
// (handlers._decode_chat_attachment). A client fault is a chatBadRequest.
func decodeChatAttachment(dataB64, filename, mimeType string) (*ChatAttachment, error) {
	payload := strings.TrimSpace(dataB64)
	declaredMime := ""
	if strings.HasPrefix(payload, "data:") {
		header, rest, found := strings.Cut(payload, ",")
		if !found || !strings.Contains(header, ";base64") {
			return nil, chatBadRequest{"attachment must be base64-encoded"}
		}
		declaredMime = strings.TrimSpace(strings.SplitN(
			strings.TrimPrefix(header, "data:"), ";", 2)[0])
		payload = rest
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, chatBadRequest{"attachment is not valid base64"}
	}
	resolved := strings.TrimSpace(mimeType)
	if resolved == "" {
		resolved = declaredMime
	}
	return resolveChatAttachment(raw, filename, resolved)
}

// resolveChatAttachment builds a storable blob from RAW bytes: mime (declared
// → sniff), the size caps, the pasted-image filename default, and a fresh id.
// The shared tail of the base64 decode path and the streaming upload path —
// ONE validation mechanism, not two. A client fault is a chatBadRequest.
func resolveChatAttachment(raw []byte, filename, mimeType string) (*ChatAttachment, error) {
	if len(raw) == 0 {
		return nil, chatBadRequest{"attachment is empty"}
	}
	resolved := strings.TrimSpace(mimeType)
	if resolved == "" {
		resolved = sniffAttachmentMime(raw)
	}
	isImage := strings.HasPrefix(resolved, "image/")
	if isImage && len(raw) > chatAttachmentImageMaxBytes {
		return nil, chatBadRequest{"image exceeds the 20 MB size limit"}
	}
	if !isImage && len(raw) > chatAttachmentMaxBytes {
		return nil, chatBadRequest{"attachment exceeds the 100 MB size limit"}
	}
	var name *string
	if trimmed := strings.TrimSpace(filename); trimmed != "" {
		name = &trimmed
	} else if isImage {
		ext, ok := imageMimeExt[resolved]
		if !ok {
			ext = "png"
		}
		defaulted := attachmentDefaultPastedImage + "." + ext
		name = &defaulted
	}
	return &ChatAttachment{
		ID:       "att-" + newHexID(12),
		Mime:     resolved,
		Data:     raw,
		Filename: name,
	}, nil
}

// attachmentRef is the ONE light-ref shape a record stamps for a stored blob
// ({id, mime, filename} — meta["attachments"] / reply-card answer_attachments /
// the upload response); filename folds nil → "".
func attachmentRef(att *ChatAttachment) map[string]any {
	filename := ""
	if att.Filename != nil {
		filename = *att.Filename
	}
	return map[string]any{"id": att.ID, "mime": att.Mime, "filename": filename}
}

// POST /api/chat/attachments — the SEND-side streaming seam: the raw body IS
// the file bytes (never base64 through a tool call; `ocagent upload` is the
// canonical client). Mime comes from ?mime= (the request Content-Type is
// deliberately ignored — every client defaults it to application/octet-stream,
// indistinguishable from an explicit declaration), else the magic-byte sniff;
// caps/filename-defaulting are the inline path's exactly
// (resolveChatAttachment — one mechanism, not two). Responds the light ref
// {id, mime, filename} that post_chat accepts back as a reference.
func (s *apiServer) HandleUploadChatAttachmentApiChatAttachmentsPost(w http.ResponseWriter, r *http.Request, params HandleUploadChatAttachmentApiChatAttachmentsPostParams) {
	// Bound the read at cap+1: one extra byte proves over-cap without ever
	// buffering an unbounded body.
	raw, err := io.ReadAll(io.LimitReader(r.Body, chatAttachmentMaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	if len(raw) > chatAttachmentMaxBytes {
		writeError(w, http.StatusBadRequest,
			"attachment exceeds the 100 MB size limit")
		return
	}
	att, rerr := resolveChatAttachment(
		raw, trimmedOrEmpty(params.Filename), trimmedOrEmpty(params.Mime))
	if rerr != nil {
		writeError(w, http.StatusBadRequest, rerr.Error())
		return
	}
	if err := s.dal.PutChatAttachment(*att); err != nil {
		internalError(w, err)
		return
	}
	filename := ""
	if att.Filename != nil {
		filename = *att.Filename
	}
	writeJSON(w, http.StatusOK, chatAttachmentUploadDTO{
		ID: att.ID, Mime: att.Mime, Filename: filename,
	})
}

// resolvedAttachment is one resolved message-attachment item; store=false
// marks a ref to an already-stored blob.
type resolvedAttachment struct {
	att   *ChatAttachment
	store bool
}

// resolveChatAttachmentInputs resolves EVERY item (refs looked up, inline
// items decoded) BEFORE any new blob is stored — all-or-nothing, so a
// rejected item never leaves earlier siblings orphaned. Shared by post_chat
// and the M3 task-message box (one attachment mechanism, not two). A
// violation answers (nil, status, problem); the caller writes the error.
func (s *apiServer) resolveChatAttachmentInputs(inputs []ChatAttachmentInputDTO) ([]resolvedAttachment, int, string) {
	var resolved []resolvedAttachment
	for _, a := range inputs {
		if refID := trimmedOrEmpty(a.Id); refID != "" {
			if strOrEmpty(a.DataB64) != "" {
				return nil, http.StatusBadRequest,
					"attachment carries both id and data_b64"
			}
			if isMemberAvatarAttachmentID(refID) {
				return nil, http.StatusBadRequest,
					"attachment '" + refID + "' is reserved for a member avatar"
			}
			// The stored blob is authoritative — a filename/mime sent alongside
			// the ref is ignored (lets the upload response be pasted back verbatim).
			att, err := s.dal.GetChatAttachment(refID)
			if err != nil {
				return nil, http.StatusInternalServerError,
					"internal error: " + err.Error()
			}
			if att == nil {
				return nil, http.StatusBadRequest,
					"attachment '" + refID + "' not found"
			}
			resolved = append(resolved, resolvedAttachment{att: att})
			continue
		}
		// Neither id nor bytes: the sender named a file it never sent. This
		// used to be dropped on the chat faces (200, message posted, file
		// silently absent — the sender believed it arrived) while the
		// reply-card face already refused it; one mechanism, two answers.
		// Owner ruled 2026-07-27 (rc-3a589dfec503): refuse everywhere. Only
		// requests that were already malformed change — they turn from a false
		// success into an honest error.
		if strOrEmpty(a.DataB64) == "" {
			return nil, http.StatusBadRequest,
				"attachment carries neither id nor data_b64"
		}
		att, err := decodeChatAttachment(
			strOrEmpty(a.DataB64), strOrEmpty(a.Filename), strOrEmpty(a.Mime))
		if err != nil {
			return nil, http.StatusBadRequest, err.Error()
		}
		resolved = append(resolved, resolvedAttachment{att: att, store: true})
	}
	return resolved, 0, ""
}

// pendingAttachments projects the resolved items into (a) the light
// [{id, mime, filename}] refs the record's meta carries and (b) the fresh blobs
// that still have to be written. NOTHING is stored here (T-e2b2): the caller
// hands both halves to ONE transactional DAL write, so a failure anywhere in
// the record's write path leaves no blob behind — a blob written before the
// record that names it is unreachable by every consumer (the gallery and the
// deletion cascade both start from the record's refs).
func pendingAttachments(resolved []resolvedAttachment) ([]any, []ChatAttachment) {
	refs := make([]any, 0, len(resolved))
	var fresh []ChatAttachment
	for _, ra := range resolved {
		if ra.store {
			fresh = append(fresh, *ra.att)
		}
		refs = append(refs, attachmentRef(ra.att))
	}
	return refs, fresh
}

// POST /api/chat — post one message. Sender = verified sub; the server mints
// the id and timestamps; attachments are decoded/validated BEFORE any is
// stored; an empty message (no text, no attachments) is 400.
func (s *apiServer) HandlePostChatApiChatPost(w http.ResponseWriter, r *http.Request) {
	var body ChatPostDTO
	if !decodeJSONBodyRequired(w, r, &body, "to") {
		return
	}
	// Enforce the body char cap BEFORE any attachment blob is stored, so a
	// rejected over-limit post never orphans a freshly-written blob. Owner is
	// exempt by sender identity (the human is never blocked by the system);
	// server-synthesised messages never reach here at all — they write the row
	// directly, see the SCOPE note on chatBodyMaxChars for how to enumerate
	// them. The
	// actionable 400 tells the agent to move the content to an attachment — a
	// dead end for a naive retry loop, not a transient error to hammer.
	if currentActor(r) != wireOwnerID {
		if n := utf8.RuneCountInString(strOrEmpty(body.Body)); n > chatBodyMaxChars {
			writeError(w, http.StatusBadRequest, "message body is "+
				strconv.Itoa(n)+" chars, over the "+strconv.Itoa(chatBodyMaxChars)+
				"-char limit. Put long content in an attachment (ocagent upload) "+
				"and keep the message to a short pointer.")
			return
		}
	}
	meta := map[string]any{}
	if body.Meta != nil {
		for k, v := range *body.Meta {
			meta[k] = v
		}
	}
	// EVERY item goes to the resolver — an item carrying neither id nor
	// data_b64 is a 400 there, not a silent drop (T-e2b2).
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		inputs = *body.Attachments
	}
	if len(inputs) > chatAttachmentsMaxCount {
		writeError(w, http.StatusBadRequest,
			// Deliberately still counted BEFORE per-item validation (review F4 /
			// R5): with incomplete items no longer pre-filtered away, an
			// over-cap list of junk now answers the cap message rather than the
			// per-item one. Kept this order because base64-decoding up to N
			// items before answering is real work this cap exists to bound.
			// Stated honestly: the request BODY is already fully read by then
			// (there is no MaxBytesReader on this route), so the cap bounds
			// decode and storage, NOT peak memory of the request. Either
			// message is true — the sender did send more than ten items.
			"a message may carry at most 10 attachments")
		return
	}
	resolved, status, problem := s.resolveChatAttachmentInputs(inputs)
	if problem != "" {
		writeError(w, status, problem)
		return
	}
	recipient, err := s.resolveChatRecipient(body.To)
	if err != nil {
		writeResolveError(w, err, "chat recipient", trimString(body.To))
		return
	}
	var fresh []ChatAttachment
	if len(resolved) > 0 {
		var refs []any
		refs, fresh = pendingAttachments(resolved)
		meta["attachments"] = refs
	}
	if strOrEmpty(body.Body) == "" && meta["attachments"] == nil {
		writeError(w, http.StatusBadRequest,
			"message must carry text or an attachment")
		return
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    currentActor(r),
		Recipient: recipient,
		Body:      strOrEmpty(body.Body),
		TS:        nowSecs(),
		Meta:      meta,
	}
	if err := s.dal.PutChatWithAttachments(msg, fresh); err != nil {
		internalError(w, err)
		return
	}
	// The chat convenience payload is exactly {id, from, to} (spec/sse.md §2.2).
	// Addressed to both participants + owner (spec §4).
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), msg.Sender)
	if msg.Recipient == wireOwnerID && msg.Sender != wireOwnerID {
		s.enqueueWebPush(webPushPayload{
			Kind: "chat", ChatID: msg.ID, ChatPeerID: msg.Sender, Title: "OffiCraft 有新訊息",
			Body: "你有一則新訊息。",
		})
	}
	writeJSON(w, http.StatusOK, s.servedChatMessageDTO(msg))
}

// servedChatMessageDTO builds the chat-message view AND joins the live reply
// card status (reply_card_status) for a card-bearing message — the read-time
// field the inline ChatReplyCard reads to lazy-load answered cards (waiting →
// load the composer eagerly; answered → collapse, fetch only on expand). The
// stored meta only ever holds the id (stamped waiting at open, never updated on
// answer), so the status MUST be joined here. Best-effort: a lookup miss/error
// leaves "" (the FE then just fetches the card, as it did before this field).
func (s *apiServer) servedChatMessageDTO(m ChatMessage) chatMessageDTO {
	dto := newChatMessageDTO(m)
	if id := replyCardIDFromMeta(m.Meta); id != "" {
		if c, err := s.dal.GetReplyCard(id); err == nil && c != nil {
			dto.ReplyCardStatus = c.Status
		}
	}
	return dto
}

// ── by-id re-read (T-a828) ───────────────────────────────────────────────────
//
// The wake snapshot COLLAPSES other agents' long messages and marks each one
// with `body_omitted_chars` > 0, whose whole meaning is "the text is still
// here, go and re-read it with get_chat". Until this seam existed get_chat took
// a PEER plus a paging CURSOR and nothing else — there was no way to name one
// message — so that marker pointed at a door with no handle, and a fold was in
// practice a silent drop. `?ids=` is the handle.
const (
	// chatByIDsMax is the hard ceiling on how many messages one call may name.
	//
	// It is a RESPONSE BOUND, not a taste: these messages come back with their
	// bodies WHOLE (that is the entire point), and a body is capped at
	// chatBodyMaxChars, so this constant times that cap is the worst case a
	// single call can be made to emit — 20 × 4,000 = 80,000 runes.
	//
	// 🔴 It is deliberately NOT "enough to unfold a whole snapshot in one call".
	// The snapshot's own chat block is capped at the chat budget setting and a
	// folded message costs it very little, so a busy line can carry more folds
	// than this; unfolding all of them at once would hand back a payload many
	// times the budget the snapshot was shrunk to. The reader is meant to name
	// the ones that matter and call again — the refusal below says so.
	chatByIDsMax = 20
	// chatByIDsTooManyMsg states the limit, because a refusal that does not say
	// what the limit is leaves the caller to bisect for it.
	chatByIDsTooManyMsg = "get_chat accepts at most %d ids per call (asked for %d) — " +
		"messages come back with their bodies whole, so the count is what bounds " +
		"the response; name the ones you actually need and call again for the rest"
	// chatByIDsNotFoundMsg is the ALL-OR-NOTHING refusal for an id no message
	// carries. Skipping the unknown one and returning the rest was the obvious
	// alternative and it is wrong HERE specifically: a short array is exactly
	// what a fold looks like, so the caller could not tell "that message is
	// gone" from "I mistyped one id" from "the server dropped it" — and this
	// seam exists to end that ambiguity, not to reproduce it one level up.
	chatByIDsNotFoundMsg = "no message carries id %s — the whole call is refused rather " +
		"than answered short, because a shortened answer is indistinguishable from the " +
		"folded message you are trying to read back; drop that id and ask again"
	// chatByIDsNotYoursMsg is the permission refusal. It states the rule and
	// offers NO way around it, deliberately: there is no parameter, no flag and
	// no other endpoint that widens a by-id read past the caller's own
	// conversations, and a refusal that hints at one teaches a bypass that
	// either does not exist or should not be used.
	//
	// 🔴 IT SPEAKS FOR THIS PATH, NOT FOR THE WHOLE TOOL. It used to open by
	// naming the endpoint as the thing that is bounded to the caller's own
	// messages, and that claim is FALSE: the ordinary listing filters on `with`
	// — a PARTICIPANT — not on the caller, so a plain read of a peer's line
	// answers with a conversation the caller was never in, and that is the
	// designed behaviour, not a leak (`caller_only` is what narrows a listing to
	// the caller). Only the by-ids read is bounded to the caller's own ends. A
	// refusal that overstates its own reach teaches every agent that reads it a
	// rule the server does not enforce, which is worse than saying nothing.
	chatByIDsNotYoursMsg = "message %s is between other members — a by-ids read returns only " +
		"messages you sent or received (one such id refuses the whole call), and holding " +
		"an id is not permission to read someone else's conversation"
)

// requestedChatIDs normalises the repeatable ?ids= parameter: blanks dropped,
// duplicates collapsed, request order preserved. An empty result means the
// parameter was not usefully sent, and the caller falls through to the ordinary
// listing path — `?ids=` alone is the same request as no `?ids=` at all, the
// same way `?statuses=` is on the task list.
func requestedChatIDs(ids *[]string) []string {
	if ids == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range *ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// chatMessageInvolvesCaller is the participation boundary, and it is one
// expression on purpose: a caller may read a message back only when it is one
// of the two ends of it.
//
// The comparison is against the VERIFIED JWT sub (§14 — identity comes from
// auth, never from a parameter) and against the STORED sender/recipient, so
// nothing in the request can move this line. It is written against
// currentActor(r) rather than a hoisted local so that it is VISIBLE to
// authz_surface_gate_test.go: this is authorisation living outside the route
// table's Requires column, and that gate exists precisely so such a predicate
// has to be enumerated with a reason rather than added quietly.
func chatMessageInvolvesCaller(r *http.Request, m ChatMessage) bool {
	return m.Sender == currentActor(r) || m.Recipient == currentActor(r)
}

// chatMessagesTheCallerWasIn returns the first named id the caller was not a
// party to, so the refusal can name it.
func chatMessagesTheCallerWasIn(r *http.Request, byID map[string]ChatMessage, ids []string) (foreign string, ok bool) {
	for _, id := range ids {
		if !chatMessageInvolvesCaller(r, byID[id]) {
			return id, false
		}
	}
	return "", true
}

// serveChatByIDs answers `?ids=` — the named messages IN FULL, oldest→newest.
//
// 🔴 NO WATERMARK ADVANCE. Re-reading a message the snapshot already showed you
// (shortened) is not reading the conversation, and sliding a read receipt from
// here would mark a whole thread read on the strength of one unfolded line —
// the same reasoning that keeps a history page from advancing it.
//
// REFUSAL ORDER is cap → unknown id → not yours, and each refusal names the id
// it is about. The unknown-before-foreign order means an id that exists but
// belongs to two other members answers 403 while an id that exists nowhere
// answers 404, so a caller CAN learn that some id exists. That is accepted with
// eyes open: ids are server-minted `c-` + 12 random hex (48 bits), so they are
// not enumerable, and the case this seam was built for is an agent holding an
// id it was already shown. The alternative — one indistinguishable refusal —
// costs every honest caller the ability to tell a typo from a boundary.
func (s *apiServer) serveChatByIDs(w http.ResponseWriter, r *http.Request, ids []string) {
	if len(ids) > chatByIDsMax {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf(chatByIDsTooManyMsg, chatByIDsMax, len(ids)))
		return
	}
	msgs, err := s.dal.ListChatByIDs(ids)
	if err != nil {
		internalError(w, err)
		return
	}
	byID := make(map[string]ChatMessage, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	for _, id := range ids {
		if _, found := byID[id]; !found {
			writeError(w, http.StatusNotFound, fmt.Sprintf(chatByIDsNotFoundMsg, id))
			return
		}
	}
	if foreign, ok := chatMessagesTheCallerWasIn(r, byID, ids); !ok {
		writeError(w, http.StatusForbidden, fmt.Sprintf(chatByIDsNotYoursMsg, foreign))
		return
	}
	out := []chatMessageDTO{}
	for _, m := range msgs {
		out = append(out, s.servedChatMessageDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/chat — the stream oldest→newest, capped to the most recent limit
// (default 30; negative = uncapped; 0 = empty). ?with= filters to a
// participant, and listing a specific conversation ADVANCES the caller's read
// watermark to the newest returned ts (auto read-receipt).
//
// SCROLLBACK (T-bf82): ?before_ts=&before_id= (both together, else 422) is a
// composite keyset cursor — the page is the `limit` messages strictly OLDER
// than (before_ts, before_id) in the stream's total (ts, id) order, still
// oldest→newest. A HISTORY PAGE NEVER ADVANCES THE READ WATERMARK: reading
// old context is not reading the conversation's newest messages — sliding the
// watermark from a history page would falsely clear unread that lives above
// the loaded window. The cursorless path below is byte-compatible unchanged.
//
// ?peek=true (T-cf91) is the READ-ONLY conversation view: with ?with= it
// filters + caps EXACTLY like the marking path but SKIPS the read-watermark
// advance — a background window (or any refresh that must not consume unread)
// gets the same recent conversation window without a read-receipt side effect.
// This replaces the old client-side workaround of pulling the WHOLE company
// stream (limit=-1) just to dodge the ?with= auto-mark and filtering in the
// browser: the payload was the entire chat history, growing without bound.
// Omitting peek (or any value other than "true") is byte-for-byte the old
// behaviour — the marking auto-receipt still fires on a plain ?with= list.
//
// ?ids= (T-a828) is answered FIRST and ON ITS OWN — see serveChatByIDs. It is
// not a filter layered on the listing below: with/limit/before_*/peek are not
// consulted at all, so nothing about the paths above changes for a caller that
// does not send it. A request whose ids are all blank is not a by-id read and
// falls through here unchanged.
func (s *apiServer) HandleListChatApiChatGet(w http.ResponseWriter, r *http.Request, params HandleListChatApiChatGetParams) {
	if ids := requestedChatIDs(params.Ids); len(ids) > 0 {
		s.serveChatByIDs(w, r, ids)
		return
	}
	with := strOrEmpty(params.With)
	peek := trimmedOrEmpty(params.Peek) == "true"
	callerOnly := params.CallerOnly != nil && *params.CallerOnly
	actor := currentActor(r)
	limit := chatListDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.BeforeTs != nil || params.BeforeId != nil {
		if params.BeforeTs == nil || params.BeforeId == nil {
			writeError(w, http.StatusUnprocessableEntity,
				"before_ts and before_id must be supplied together")
			return
		}
		// History page: cursor-bounded SQL read (LIMIT in the query — never a
		// full-table pull) and NO PutChatRead — see the handler note above.
		caller := ""
		if callerOnly {
			caller = actor
		}
		msgs, err := s.dal.listChatBefore(with, caller, *params.BeforeTs, *params.BeforeId, limit)
		if err != nil {
			internalError(w, err)
			return
		}
		out := []chatMessageDTO{}
		for _, m := range msgs {
			out = append(out, s.servedChatMessageDTO(m))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	msgs, err := s.dal.ListChat()
	if err != nil {
		internalError(w, err)
		return
	}
	if with != "" {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Sender == with || m.Recipient == with {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if callerOnly {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Sender == actor || m.Recipient == actor {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if limit >= 0 {
		if limit == 0 {
			msgs = nil
		} else if len(msgs) > limit {
			msgs = msgs[len(msgs)-limit:]
		}
	}
	if with != "" && !peek && len(msgs) > 0 {
		newest := msgs[0].TS
		for _, m := range msgs {
			if m.TS > newest {
				newest = m.TS
			}
		}
		effective, advanced, err := s.dal.PutChatRead(ChatRead{
			ReaderID: currentActor(r), PeerID: with, LastReadTS: newest,
		})
		if err != nil {
			internalError(w, err)
			return
		}
		if advanced {
			s.publishChatRead(effective, requestTrigger(r))
		}
	}
	out := []chatMessageDTO{}
	for _, m := range msgs {
		out = append(out, s.servedChatMessageDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// isPreviewableMime: a mime the browser renders in a new tab (image/*, text/*,
// application/pdf) — the preview/download split (handlers._is_previewable_mime).
func isPreviewableMime(m string) bool {
	return strings.HasPrefix(m, "image/") || strings.HasPrefix(m, "text/") ||
		m == "application/pdf"
}

// GET /api/chat/attachment/{attachment_id} — serve the raw blob under its
// stored mime. Non-image previewables go inline + CSP sandbox (an inline HTML
// blob must never script on this origin); other non-images download under
// their original name (RFC 5987 filename* + ASCII fallback).
func (s *apiServer) HandleGetChatAttachmentApiChatAttachmentAttachmentIdGet(w http.ResponseWriter, r *http.Request, attachmentId string) {
	att, err := s.dal.GetChatAttachment(attachmentId)
	if err != nil {
		internalError(w, err)
		return
	}
	if att == nil {
		writeError(w, http.StatusNotFound, "attachment '"+attachmentId+"' not found")
		return
	}
	if !strings.HasPrefix(att.Mime, "image/") {
		name := attachmentId
		if att.Filename != nil && *att.Filename != "" {
			name = *att.Filename
		}
		asciiName := strings.Map(func(r rune) rune {
			if r > 127 {
				return -1
			}
			return r
		}, name)
		if asciiName == "" {
			asciiName = attachmentId
		}
		safe := strings.ReplaceAll(asciiName, `"`, `\"`)
		dispSuffix := `filename="` + safe + `"; filename*=UTF-8''` +
			url.QueryEscape(name)
		if isPreviewableMime(att.Mime) {
			w.Header().Set("Content-Disposition", "inline; "+dispSuffix)
			w.Header().Set("Content-Security-Policy", "sandbox")
		} else {
			w.Header().Set("Content-Disposition", "attachment; "+dispSuffix)
		}
	}
	mediaType := att.Mime
	if mediaType == "" {
		mediaType = attachmentOctetStream
	}
	if _, _, err := mime.ParseMediaType(mediaType); err != nil {
		mediaType = attachmentOctetStream
	}
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(att.Data)
}

// GET /api/chat/attachments/{attachment_id}/share-link — mint the permanent
// share link for ONE attachment: the serve path carrying its ?sig= HMAC
// credential (sharesig.go). Gated like every chat route; 404 for an unknown
// blob id so a caller cannot mint links into the void. The URL is
// server-relative — the client prefixes its own origin.
func (s *apiServer) HandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet(w http.ResponseWriter, r *http.Request, attachmentId string) {
	att, err := s.dal.GetChatAttachment(attachmentId)
	if err != nil {
		internalError(w, err)
		return
	}
	if att == nil {
		writeError(w, http.StatusNotFound, "attachment '"+attachmentId+"' not found")
		return
	}
	writeJSON(w, http.StatusOK, ChatAttachmentShareLinkDTO{
		Url: "/api/chat/attachment/" + attachmentId +
			"?sig=" + shareSigFor(s.secret, attachmentId),
	})
}

// GET /api/chat/attachments?with=<member_id> — the flattened member gallery:
// every attachment of the member's conversations, newest→oldest, each row
// carrying the message's sender identity. READ-ONLY (no watermark advance);
// a blank with is 422.
func (s *apiServer) HandleListChatAttachmentsApiChatAttachmentsGet(w http.ResponseWriter, r *http.Request, params HandleListChatAttachmentsApiChatAttachmentsGetParams) {
	peer := trimmedOrEmpty(params.With)
	if peer == "" {
		writeError(w, http.StatusUnprocessableEntity, "with is required")
		return
	}
	msgs, err := s.dal.ListChat()
	if err != nil {
		internalError(w, err)
		return
	}
	var involved []ChatMessage
	for _, m := range msgs {
		if m.Sender == peer || m.Recipient == peer {
			involved = append(involved, m)
		}
	}
	// newest→oldest, STABLE (equal-ts messages keep stream order; a message's
	// posted attachment order is preserved).
	sort.SliceStable(involved, func(i, j int) bool {
		return involved[i].TS > involved[j].TS
	})
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	names := map[string]string{}
	for _, m := range members { // ANY roster status — dismissed still reads by name
		names[m.ID] = m.Name
	}
	entries := []chatGalleryEntryDTO{}
	for _, m := range involved {
		refs, _ := m.Meta["attachments"].([]any)
		for _, refAny := range refs {
			ref, _ := refAny.(map[string]any)
			attID, _ := ref["id"].(string)
			if attID == "" {
				continue // never fabricate a serve URL for a ref with no id
			}
			mimeType, _ := ref["mime"].(string)
			filename, _ := ref["filename"].(string)
			entries = append(entries, chatGalleryEntryDTO{
				ID:        attID,
				URL:       "/api/chat/attachment/" + attID,
				Filename:  filename,
				Mime:      mimeType,
				IsImage:   strings.HasPrefix(mimeType, "image/"),
				MessageID: m.ID,
				From:      m.Sender,
				FromName:  names[m.Sender],
				To:        m.Recipient,
				TS:        m.TS,
			})
		}
	}
	writeJSON(w, http.StatusOK, entries)
}

// POST /api/chat/mark-read — advance the caller's per-conversation watermark
// (monotonic; the reader is ALWAYS the verified sub). Blank peer → 422.
func (s *apiServer) HandleMarkChatReadApiChatMarkReadPost(w http.ResponseWriter, r *http.Request) {
	var body MarkChatReadDTO
	if !decodeJSONBodyRequired(w, r, &body, "peer") {
		return
	}
	peer := trimString(body.Peer)
	if peer == "" {
		writeError(w, http.StatusUnprocessableEntity, "peer is required")
		return
	}
	var lastRead float64
	if body.LastReadTs != nil {
		lastRead = *body.LastReadTs
	}
	receipt := ChatRead{ReaderID: currentActor(r), PeerID: peer, LastReadTS: lastRead}
	if err := ValidateChatRead(receipt); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	effective, advanced, err := s.dal.PutChatRead(receipt)
	if err != nil {
		internalError(w, err)
		return
	}
	if advanced {
		s.publishChatRead(effective, requestTrigger(r))
	}
	writeJSON(w, http.StatusOK, chatReadDTO{
		ReaderID:   effective.ReaderID,
		PeerID:     effective.PeerID,
		LastReadTS: effective.LastReadTS,
	})
}

// GET /api/chat/reads — read receipts, optionally filtered to one peer
// conversation (?with=).
func (s *apiServer) HandleListChatReadsApiChatReadsGet(w http.ResponseWriter, r *http.Request, params HandleListChatReadsApiChatReadsGetParams) {
	receipts, err := s.dal.ListChatReads("", strOrEmpty(params.With))
	if err != nil {
		internalError(w, err)
		return
	}
	out := []chatReadDTO{}
	for _, rec := range receipts {
		out = append(out, chatReadDTO{
			ReaderID:   rec.ReaderID,
			PeerID:     rec.PeerID,
			LastReadTS: rec.LastReadTS,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/resume-summary — the bounded, identity-locked wake snapshot
// (handlers.handle_resume_summary): the caller's recent chat (budget-packed,
// other agents' bodies collapsed, reply cards folded in place) + the caller's
// open tasks as LIGHT rows (SPEC §6.2 — a handover resumes in-flight tasks, not
// just chat; assembled by resumeTasksFor, api_tasks.go; T-3f31: no plan detail
// rides the snapshot) + the overview size/概要 block (peek-then-decide) +
// identity + the fixed bounded-snapshot note.
func (s *apiServer) HandleResumeSummaryApiResumeSummaryGet(w http.ResponseWriter, r *http.Request) {
	actor := currentActor(r)
	snap, err := s.resumeSnapshotParts(actor)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resumeSummaryDTO{
		Identity:           &actor,
		GeneratedAt:        snap.GeneratedAt,
		Chat:               snap.Chat,
		ChatEarlierOmitted: snap.ChatCut,
		Tasks:              snap.Tasks,
		Roster:             snap.Roster,
		Machines:           &snap.Machines,
		Overview:           snap.Overview,
		Note:               resumeNote,
	})
}

// resumeWakeSnapshot is one assembled wake snapshot. It replaced a six-value return
// when the chat block grew a cut marker and the payload grew a header: past
// about four returns the call sites stop being readable and a mis-ordered pair
// of same-typed values compiles silently.
type resumeWakeSnapshot struct {
	// GeneratedAt anchors every ts_display in the payload. It is stamped ONCE,
	// here, so the header and the rows cannot disagree about when "now" was.
	GeneratedAt string
	Chat        []chatMessageDTO
	ChatCut     resumeChatCutDTO
	Tasks       []resumeTaskDTO
	Roster      []resumeRosterMemberDTO
	Machines    resumeMachinesDTO
	Overview    resumeOverviewDTO
}

// resumeSnapshotParts assembles the caller's wake snapshot: the recent chat
// (budget-packed per conversation line, other agents' bodies collapsed, reply
// cards folded in place), the caller's open tasks as LIGHT rows, the studio
// floor, and the overview size/概要 block. resume_summary serves all of it;
// peek_resume_summary_size serves only the overview (+ identity) — both go
// THROUGH this one assembly so the sizes the peek reports can never drift from
// what a full resume_summary would carry (T-7974: the two-step boot lets an
// agent size the snapshot before deciding whether to pull it into its own
// context or hand it to a cheap sub-agent).
//
// 🔴 There is exactly ONE size estimator in this file and it is the one below.
// Every character the new chat format adds — display names, rendered
// timestamps, folded card content, the collapse marker, the cut hint, the
// header — is counted HERE, into overview.chat_chars, precisely because the
// peek and the snapshot share this path. A second estimator written "just for
// the peek" is how the peek starts lying about the thing it exists to measure.
func (s *apiServer) resumeSnapshotParts(actor string) (resumeWakeSnapshot, error) {
	snap := resumeWakeSnapshot{
		GeneratedAt: resumeDisplayTime(nowSecs()),
		Chat:        []chatMessageDTO{},
	}
	// The floor is folded FIRST because the chat block needs its member list to
	// resolve display names. That list is a by-product of a query the boot path
	// ALREADY runs (see resumeFloorParts' cost note) — the names cost nothing
	// extra, and adding a second member query to resolve them would have.
	roster, machines, rosterChars, machinesChars, names, err := s.resumeFloorParts(actor)
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	snap.Roster, snap.Machines = roster, machines

	// ONE ListReplyCards serves BOTH the peek counts and the inline fold.
	//
	// 🔴 COST: ListReplyCards is a FULL TABLE SCAN and it is already on this
	// path (the waiting / answered-recently counts below). Folding card content
	// into chat therefore adds NO query — it reads the same rows through a map
	// built in the same pass. A per-message GetReplyCard inside the chat loop
	// would have been one point query per carded message, on every wake.
	// The map also REPLACES servedChatMessageDTO's per-message GetReplyCard for
	// reply_card_status, so this path runs NO MORE queries than before — and
	// strictly fewer whenever the snapshot carries at least one carded message,
	// which is the only case where the replaced per-message lookups existed at
	// all. On a snapshot with no cards there is nothing to save and the count
	// is simply unchanged.
	cardsByID := map[string]ReplyCard{}
	cardsWaiting, cardsAnsweredRecent := 0, 0
	if actor != "" {
		cards, err := s.dal.ListReplyCards()
		if err != nil {
			return resumeWakeSnapshot{}, err
		}
		now := nowSecs()
		for _, c := range cards {
			cardsByID[c.ID] = c
			// The counts stay scoped to cards the subject INITIATED, unchanged.
			if c.FromMember != actor {
				continue
			}
			switch {
			case c.Status == replyCardStatusWaiting:
				cardsWaiting++
			case c.Status == replyCardStatusAnswered &&
				now-c.AnsweredTS <= replyCardAnsweredWindowSecs:
				cardsAnsweredRecent++
			}
		}
	}

	msgs, err := s.dal.ListChatInvolving(actor, resumeChatFetch)
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	chat, cut, chatChars := resumeChatBlock(
		actor, msgs, names, cardsByID,
		resumeChatPackBudget(s.chatBudget(), snap.GeneratedAt))
	snap.Chat, snap.ChatCut = chat, cut

	tasks, tasksOpenTotal, err := s.resumeTasksFor(actor)
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	snap.Tasks = tasks
	detailChars := 0
	for _, t := range tasks {
		detailChars += t.DetailChars
	}
	snap.Overview = resumeOverviewDTO{
		ChatCount: len(chat),
		// chat_chars is the WHOLE chat block's rune cost — every message's
		// carried body plus everything the wake format wraps around it, plus
		// the snapshot header (generated_at) and the cut hint, which are part
		// of what a caller must read even though they sit outside the array.
		// It is NOT "the sum of the bodies" any more; peekNote says so too.
		//
		// 🔴 This sum is what the chat budget setting bounds, and the packer was
		// handed its budget with exactly these two addends already subtracted
		// (resumeChatPackBudget) — so it can never come out above the ceiling.
		ChatChars: chatChars + utf8.RuneCountInString(snap.GeneratedAt) +
			utf8.RuneCountInString(cut.Hint),
		TasksReturned:       len(tasks),
		TasksOpenTotal:      tasksOpenTotal,
		TasksDetailChars:    detailChars,
		CardsWaiting:        cardsWaiting,
		CardsAnsweredRecent: cardsAnsweredRecent,
		RosterChars:         rosterChars,
		MachinesChars:       machinesChars,
	}
	return snap, nil
}

// resumeDisplayTime renders an epoch second as resumeTimeLayout in the SERVER's
// local zone. "" for a zero/absent timestamp — an unanswered card must not be
// dressed up as having happened at the epoch.
func resumeDisplayTime(ts float64) string {
	if ts <= 0 {
		return ""
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Local().Format(resumeTimeLayout)
}

// resumeDisplayName resolves an id to a display name for the wake snapshot.
// The owner is special-cased because it has no roster row (see
// resumeOwnerDisplayName); anything else unresolved stays "" rather than
// echoing its own id back — a name that is really an id is worse than no name,
// because a reader cannot tell which of the two fields it is looking at.
func resumeDisplayName(id string, names map[string]string) string {
	if id == wireOwnerID {
		return resumeOwnerDisplayName
	}
	return names[id]
}

// resumeChatCarriesFullBody decides, PER MESSAGE, whether this one is exempt
// from collapsing. See resumeChatOtherPreview for why these two and only these
// two: the self hand-off IS the baton a wake resumes from, and the owner's line
// is instruction from the human. Everything else is third-party traffic that
// only has to be recognisable.
func resumeChatCarriesFullBody(subject string, m ChatMessage) bool {
	if subject != "" && m.Sender == subject && m.Recipient == subject {
		return true
	}
	return m.Sender == wireOwnerID || m.Recipient == wireOwnerID
}

// resumeChatMessageDTO projects ONE message for the wake snapshot: names beside
// ids, a rendered timestamp beside the epoch one, the body collapsed unless
// exempt, and the reply card folded in place.
func resumeChatMessageDTO(subject string, m ChatMessage, names map[string]string, cards map[string]ReplyCard) chatMessageDTO {
	d := newChatMessageDTO(m)
	d.FromName = resumeDisplayName(m.Sender, names)
	d.ToName = resumeDisplayName(m.Recipient, names)
	d.TSDisplay = resumeDisplayTime(m.TS)
	if !resumeChatCarriesFullBody(subject, m) {
		if r := []rune(d.Body); len(r) > resumeChatOtherPreview {
			// body_omitted_chars counts what was FOLDED AWAY, not what is left:
			// the reader already has what is left, in front of it.
			omitted := len(r) - resumeChatOtherPreview
			if resumeChatCollapseIsWorthIt(omitted) {
				d.BodyOmittedChars = omitted
				d.Body = string(r[:resumeChatOtherPreview]) + "…"
			}
		}
	}
	if id := replyCardIDFromMeta(m.Meta); id != "" {
		if c, ok := cards[id]; ok {
			d.ReplyCardStatus = c.Status
			// Scope: cards the SUBJECT initiated. A card the subject merely
			// answered belongs to whoever asked, and folding it in here would
			// put someone else's pending decision in this agent's wake.
			if c.FromMember == subject {
				options := c.Options
				if options == nil {
					options = []string{}
				}
				d.Card = &chatInlineReplyCardDTO{
					Options:           options,
					AnswerOptionIdx:   c.AnswerOptionIdx,
					AnswerText:        c.AnswerText,
					AnsweredTS:        c.AnsweredTS,
					AnsweredAtDisplay: resumeDisplayTime(c.AnsweredTS),
				}
			}
		}
	}
	return d
}

// resumeChatCollapseIsWorthIt answers whether folding a body actually SAVES
// anything, and it exists because for a while it did not have to.
//
// 🔴 A COLLAPSE IS NOT FREE. Marking a message as folded costs the payload the
// ellipsis that replaces the cut text (1 rune) plus the digits of
// body_omitted_chars (resumeChatMessageChars counts exactly those), and it costs
// the READER a marker beside the message and, if it matters, a get_chat round
// trip to recover what was taken. Folding a body that was barely over the
// preview therefore buys a handful of runes and pays for them twice — owner,
// 2026-08-13: 「省不到就不要折」.
//
// So the rule is the literal one: fold only when the saving is STRICTLY GREATER
// than what the marker itself costs. The marker cost is DERIVED here rather than
// written down as a constant, because it is the same arithmetic
// resumeChatMessageChars bills — a constant would be a second copy of it and
// would go stale the first time the marker changes shape.
//
// Boundary: equal is NOT worth it. A fold that breaks even has made the payload
// no smaller and the message harder to read, which is a pure loss.
func resumeChatCollapseIsWorthIt(omitted int) bool {
	markerCost := 1 /* the … that replaces the cut text */ +
		len(strconv.Itoa(omitted))
	return omitted > markerCost
}

// resumeChatMessageChars is the rune cost ONE projected message puts on the
// wire. It counts the body as CARRIED (post-collapse) plus everything the wake
// format adds around it. Ids are deliberately NOT counted: they were already on
// the wire before this format existed and were never in chat_chars, so counting
// them now would move the number for a reason that has nothing to do with what
// changed.
func resumeChatMessageChars(d chatMessageDTO) int {
	n := utf8.RuneCountInString(d.Body) +
		utf8.RuneCountInString(d.FromName) +
		utf8.RuneCountInString(d.ToName) +
		utf8.RuneCountInString(d.TSDisplay) +
		len(strconv.Itoa(d.BodyOmittedChars))
	if d.Card != nil {
		for _, o := range d.Card.Options {
			n += utf8.RuneCountInString(o)
		}
		n += utf8.RuneCountInString(d.Card.AnswerText) +
			utf8.RuneCountInString(d.Card.AnsweredAtDisplay)
		if d.Card.AnswerOptionIdx != nil {
			n += len(strconv.Itoa(*d.Card.AnswerOptionIdx))
		}
	}
	return n
}

// resumeChatBlock packs the wake snapshot's chat and reports what it left out.
//
// msgs arrives oldest→newest and is the caller's GLOBAL newest window
// (ListChatInvolving, capped at resumeChatFetch). Packing is one pass:
// walk NEWEST FIRST and take messages while they fit; the first one that would
// push the block past `budget` STOPS the walk and everything older is left out.
//
// 🔴 STOP, not skip. The previous packer kept going past a message that did not
// fit, hoping a smaller older one would — which spends the budget more fully but
// hands the reader a stream with holes punched in it at unpredictable places.
// The owner's ruling is literally a prefix: 「只管從最新一則訊息往前推,直到超出
// 我們 budget 上限前最後一則」. A contiguous newest-first run is also what makes
// the boundary checkable: the block is at the budget, and the very next message
// would exceed it.
//
// 🔴 There is NO per-line reserve any more. It was removed on 2026-08-13
// (「不要管每條對話線」) because its reserved messages were billed to the budget
// and never evicted by it, so the block had no upper bound at all — see
// the chat budget setting. The cost is real and is not hidden: a quiet
// correspondent whose last message is older than the budget reaches now falls
// off the snapshot entirely. It is not SILENT, though — that is what
// chat_earlier_omitted and its hint are for, and dropping anything raises them.
//
// `budget` is passed in rather than read from the setting directly,
// because the caller must subtract what rides OUTSIDE this array yet still
// counts against the same ceiling (the snapshot header and the cut hint). A
// packer that spent the whole constant would put overview.chat_chars over it by
// construction, which is the defect this whole change exists to remove.
//
// Returns the messages oldest→newest, the cut marker, and the block's rune cost.
func resumeChatBlock(subject string, msgs []ChatMessage, names map[string]string, cards map[string]ReplyCard, budget int) ([]chatMessageDTO, resumeChatCutDTO, int) {
	type packedMsg struct {
		dto  chatMessageDTO
		cost int
	}
	all := make([]packedMsg, 0, len(msgs))
	for _, m := range msgs {
		d := resumeChatMessageDTO(subject, m, names, cards)
		all = append(all, packedMsg{dto: d, cost: resumeChatMessageChars(d)})
	}

	// The read filled its window, so older messages MAY exist that were never
	// even fetched. Reported as a cut whether or not the budget stopped the walk.
	//
	// Deliberately one-sided: a caller with exactly resumeChatFetch messages and
	// nothing older reports a cut that is not there. That costs a reader one
	// wasted get_chat; the opposite error costs it a conversation it never learns
	// exists.
	atFetchCap := len(all) >= resumeChatFetch

	used := 0
	first := len(all) // index of the OLDEST message that made it in
	dropped := false
	for i := len(all) - 1; i >= 0; i-- { // newest first: `all` is oldest→newest
		if used+all[i].cost > budget {
			// Everything from here back is older, so the walk is over.
			dropped = true
			break
		}
		used += all[i].cost
		first = i
	}

	// `all` is already the one chronological stream the chat surface serves, so
	// the surviving suffix is in order with no re-sort needed.
	chat := []chatMessageDTO{}
	for i := first; i < len(all); i++ {
		chat = append(chat, all[i].dto)
	}

	cut := resumeChatCutDTO{}
	if dropped || atFetchCap {
		cut.Omitted = true
		cut.Hint = resumeChatCutHint
	}
	return chat, cut, used
}

// resumeChatPackBudget is what resumeChatBlock may spend on MESSAGES, once the
// runes that ride outside the array but inside overview.chat_chars are set
// aside: the snapshot header (generated_at) and the cut hint.
//
// 🔴 The hint is reserved UNCONDITIONALLY, even though it is only emitted when
// something was actually left out. Reserving it only when needed is circular —
// whether the hint appears depends on whether the pack overflowed, which depends
// on the budget. Reserving it always makes `chat_chars <= budget`
// true in every case, and makes the bound TIGHT in exactly the case that matters
// (the block that dropped something carries the hint, so it lands on the ceiling
// rather than under it). A snapshot that dropped nothing simply comes in a few
// hundred runes under — which is the cheap side to err on.
//
// Never negative: a pathologically long hint would otherwise make the budget
// negative and empty the chat block silently.
//
// `budget` is the LIVE `chat.budget_chars` setting, passed in rather than read
// from a constant (T-c9b4). This function stays pure so the caller — which has
// the *apiServer receiver and therefore the accessor — is the single place the
// number is sourced.
func resumeChatPackBudget(budget int, generatedAt string) int {
	b := budget -
		utf8.RuneCountInString(generatedAt) -
		utf8.RuneCountInString(resumeChatCutHint)
	if b < 0 {
		return 0
	}
	return b
}

// resumeFloorParts assembles the studio floor a waking agent lands on: the
// roster (T-1b09, owner ruling rc-4e98c0481852 — "All members and contractors
// and their online / offline status") and the machine block (rc-09476f535b59 —
// the machine list plus which one you are on). It also returns the character
// size of each block so the peek can report what the payload actually carries.
//
// 🔴 COST DISCIPLINE — read this before adding anything here. resume_summary is
// called by EVERY agent on EVERY wake, so a query in this function is paid
// fleet-wide, forever. In particular this deliberately does NOT reuse the
// GET /api/members path: that one computes unread counts through a full
// ListChat() table scan (api_helpers.go unreadCountsForRequest), and hanging
// the most expensive query in the system off the boot path would multiply it by
// fleet size. Everything below is one bounded query or in-memory:
//   - ONE ListMembersIncludingOutsource (single SELECT over the member table)
//   - ONE hub.OnlineMembers map (in-memory; NOT one IsOnline call per member)
//   - observedHost / PresenceState (pure + in-memory)
//   - ONE role lookup per DISTINCT role, deduped below — not per member
//   - contractors only: GetOutsourceWorker + GetTask, both POINT queries.
//     Deliberately not ListOpenTasksByExecutor: task.executor_id carries no
//     index, so that path is a full task-table scan per contractor. State the
//     cost accurately: the boot path ALREADY runs two such scans for the
//     caller's own tasks (resumeTasksFor), so the rejected variant would not
//     introduce the scan — it would MULTIPLY an existing one by the contractor
//     count, which is why it is still worth refusing.
//   - ONE AllTaskStepProgress (T-925f — a contractor's progress_done/total):
//     the same single grouped-COUNT query list_tasks already pays, called
//     ONCE for the whole roster and read from the resulting map per
//     contractor — never a per-contractor ListTaskSteps, which would drag
//     back every step's Name/DoD text onto a path every agent boots through.
func (s *apiServer) resumeFloorParts(actor string) ([]resumeRosterMemberDTO, resumeMachinesDTO, int, int, map[string]string, error) {
	members, err := s.dal.ListMembersIncludingOutsource()
	if err != nil {
		return nil, resumeMachinesDTO{}, 0, 0, nil, err
	}
	displayNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		return nil, resumeMachinesDTO{}, 0, 0, nil, err
	}
	stepProgress, err := s.dal.AllTaskStepProgress()
	if err != nil {
		return nil, resumeMachinesDTO{}, 0, 0, nil, err
	}
	online := s.hub.OnlineMembers()
	now := nowSecs()

	// One role fold per DISTINCT role_key. Roles repeat across members (four
	// members can share one role), so folding per member would pay the same
	// lookup several times on a path every agent runs.
	dutyByRole := map[string]string{}
	roleNameByRole := map[string]string{}
	resolveRole := func(roleKey string) (string, string) {
		if roleKey == "" {
			return "", ""
		}
		if name, ok := roleNameByRole[roleKey]; ok {
			return name, dutyByRole[roleKey]
		}
		def, err := s.foldRoleDefDTO(roleKey)
		if err != nil || def == nil {
			// A member pointing at a role that no longer resolves still
			// belongs on the floor — it is reachable and its presence is
			// real. Degrade to empty role text, never drop the row.
			roleNameByRole[roleKey], dutyByRole[roleKey] = "", ""
			return "", ""
		}
		roleNameByRole[roleKey] = def.Name
		dutyByRole[roleKey] = dutyText(def.DefinitionMD)
		return roleNameByRole[roleKey], dutyByRole[roleKey]
	}

	// names is the id→display-name table the chat block resolves from_name /
	// to_name with. It is built from the ALREADY-LOADED member slice — no extra
	// query, which is the only reason names could be added to the wake snapshot
	// at all under this function's cost discipline.
	//
	// 🔴 It is populated BEFORE the roster filters below, and that is the whole
	// point: the roster carries only ACTIVE non-machine rows, but a chat message
	// keeps its sender forever. A dismissed colleague, a released contractor and
	// a warden all still have to read by name in a hand-off — dropping them here
	// would silently degrade exactly the old conversations a wake goes looking
	// for. The owner is NOT in this table at all (it has no member row); see
	// resumeDisplayName.
	names := make(map[string]string, len(members))
	for _, m := range members {
		names[m.ID] = m.Name
	}

	// Members first, contractors after (each already name-ordered by the
	// DAL). Contractor codenames are opaque and short-lived; interleaving
	// them with the people who hold standing roles makes the block harder to
	// scan for its ONE purpose — finding someone to ask.
	staff := []resumeRosterMemberDTO{}
	contractors := []resumeRosterMemberDTO{}
	machines := []resumeMachineDTO{}
	// "Where am I" is answered from the row this loop already holds, not by a
	// second point query for the caller. Captured BEFORE the roster-status and
	// warden filters below, deliberately: this route admits warden tokens
	// (Requires: principalMachine) and a just-deactivated caller, and both must
	// keep getting a real answer for their own machine — filtering first would
	// silently return "" for exactly those callers.
	callerHost := ""
	for _, m := range members {
		if m.ID == actor {
			callerHost = s.observedHost(m)
		}
		if m.RosterStatus != RosterStatusActive {
			continue
		}
		if m.Kind == machineKind {
			// A warden row IS a machine, not a colleague — it belongs in the
			// machine block, never in the roster.
			name := m.Name
			if alias := displayNames[m.ID]; alias != "" {
				name = alias
			}
			machines = append(machines, resumeMachineDTO{
				MachineID:   m.ID,
				DisplayName: name,
				Online:      online[m.ID],
			})
			continue
		}
		row := resumeRosterMemberDTO{
			ID:       m.ID,
			Name:     m.Name,
			Kind:     m.Kind,
			Machine:  s.observedHost(m),
			Presence: PresenceState(m, now, online[m.ID]),
		}
		if m.Kind == KindOutsource {
			row.CurrentTask, row.TaskStatus, row.WaitingReason,
				row.ProgressDone, row.ProgressTotal = s.contractorTaskFields(m.ID, stepProgress)
			contractors = append(contractors, row)
			continue
		}
		row.RoleName, row.Duty = resolveRole(m.RoleKey)
		staff = append(staff, row)
	}
	roster := append(staff, contractors...)

	machinesBlock := resumeMachinesDTO{
		List: machines,
		// The caller's OWN machine goes through the same observedHost the
		// roster rows use, so "where am I" and "where is he" can never
		// disagree inside one snapshot. Never a hostname: our hosts report
		// the same name as each other, so a hostname-derived answer picks
		// the wrong box silently.
		YouAreOn: callerHost,
	}
	return roster, machinesBlock, rosterChars(roster), machinesChars(machinesBlock), names, nil
}

// contractorTaskFields returns the TRUNCATED title of the one task a
// contractor is bound to (owner ruling rc-a02d8bc7fe23: 正職給職責、外包給任務
// 標題 — a contractor id is minted per task, so its task title IS its duty),
// plus that task's status, waiting_reason, and step progress (T-925f, owner
// ruling rc-6935feeb293a 選①). status/waiting_reason ride the SAME GetTask
// row contractorTaskFields already loaded for the title — no extra query.
// progress comes from stepProgress, the roster-wide map resumeFloorParts
// built with ONE AllTaskStepProgress call; a task with no steps is simply
// absent from that map and progress stays 0/0. Any lookup miss degrades to
// the zero values across the board: a contractor whose task cannot be read is
// still on the floor and still reachable, which is what this block is for.
func (s *apiServer) contractorTaskFields(workerID string, stepProgress map[string]TaskStepProgress) (title, status, waitingReason string, progressDone, progressTotal int) {
	w, err := s.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil || w.TaskID == "" {
		return "", "", "", 0, 0
	}
	t, err := s.dal.GetTask(w.TaskID)
	if err != nil || t == nil {
		return "", "", "", 0, 0
	}
	title = truncateRunes(t.Title, resumeTaskTitlePreview)
	status = t.Status
	waitingReason = t.WaitingReason
	if p, ok := stepProgress[t.ID]; ok {
		progressDone, progressTotal = p.Done, p.Total
	}
	return title, status, waitingReason, progressDone, progressTotal
}

// dutyText is the role's own definition text, capped at resumeDutyPreview
// (owner 2026-08-03: 「1000字 多的截斷」).
//
// It deliberately does NOT summarize, reformat, or pick a "best line" out of
// the definition. An earlier draft took the first non-heading line, and the
// owner replaced that with a flat cap — which is the better rule for a reason
// worth keeping: a heuristic that chooses WHICH line to show silently changes
// what a role appears to be responsible for whenever someone reorders their own
// role doc. A flat cap can only ever cut the tail, and the ellipsis says so.
//
// The ONE thing removed before the cap is the doc's own title line
// (stripLeadingTitle) — a syntactic prefix, not a choice about content. Say
// this accurately anywhere it is described: the cap is applied to the
// definition MINUS its title, not to the raw markdown.
func dutyText(md string) string {
	// Strip BEFORE the cap, never after: capping first and stripping second
	// would spend the budget on the title and then delete it. The shape this
	// guards is ANY Duty longer than the cap that opens with its own title —
	// not a claim about what ships today (as of the 2026-08-04 measurement the
	// longest role doc was 455 runes, far under the cap, so the set observed
	// then no longer demonstrated the case; that reading is dated and may not
	// hold now). Pinned by TestResumeDutyStripsBeforeCapping.
	return truncateRunes(stripLeadingTitle(md), resumeDutyPreview)
}

// stripLeadingTitle drops the ONE markdown title line a role doc opens with —
// 「# 助理」 — before the cap is applied, so the budget is not spent
// restating the role name the row already carries in RoleName.
//
// It removes the FIRST heading line only, deliberately, and not every leading
// heading: a terse role doc can be written as an outline whose 「## 負責…」
// lines ARE the duty, and eating the whole leading run would delete exactly
// that content. One title line is what the owner was told this would remove.
//
// It is not the line-selection heuristic he overruled — it ranks nothing and
// reads no content, only a fixed syntactic prefix. The honest limit of that
// claim: moving a paragraph ABOVE the title changes the output (the first line
// is then not a heading, so nothing is stripped). What it cannot do is change
// WHICH line is shown based on what the lines say.
//
// A title-only document comes back whole: an empty duty reads as "this member
// has no role", a different fact from "this member's role doc is only a title".
//
// Two known limits, both deliberate:
//   - SETEXT headings (a line underlined with === or ---) are NOT stripped.
//     Only ATX is. Conservative: it costs TWO lines of budget (the text line
//     AND the underline — a setext heading is two lines by construction),
//     never content.
//   - A role doc with no h1 that opens straight into 「## 章節」 loses that
//     first section heading — it is syntactically a title line. The content
//     under it survives, but reads as an unlabelled lead-in. Not worth a
//     content-aware rule: deciding "is this heading a title or a section?"
//     is exactly the judgement the flat-cap rule exists to avoid.
func stripLeadingTitle(md string) string {
	trimmed := strings.TrimRight(md, " \t\r\n")
	// Skip leading blank lines WITHOUT collapsing the first content line's
	// indentation — four spaces of indent make it an indented code block, not
	// a title, and isATXHeading needs to see that. (That care ends at the
	// heading decision. The TrimSpace on the way out strips whitespace only —
	// no content is ever dropped — but it DE-INDENTS the first surviving line,
	// which changes that line's indent RELATIVE to the ones after it, so the
	// block's markdown parse can change: an indented code block under a title
	// splits into a paragraph plus a code block, and a line that was literal
	// text INSIDE a code block can become a real heading. Rendering only — duty
	// is carried as a plain string and nothing parses it — but not merely
	// cosmetic.)
	rest := trimmed
	for rest != "" {
		line, tail, found := strings.Cut(rest, "\n")
		if strings.TrimSpace(line) != "" {
			break
		}
		if !found {
			rest = ""
			break
		}
		rest = tail
	}
	line, tail, _ := strings.Cut(rest, "\n")
	if !isATXHeading(line) {
		return strings.TrimSpace(trimmed)
	}
	body := strings.TrimSpace(tail)
	if body == "" {
		return strings.TrimSpace(trimmed)
	}
	return body
}

// isATXHeading reports whether line is a markdown ATX heading, by the syntax
// rule rather than by "starts with #": 0–3 spaces of indent, then 1–6 '#',
// then a space or end of line. A bare HasPrefix("#") is not the same test and
// silently eats real content — 「#1 順位：先看 X」 and 「#hashtag」 are body
// text, and a line indented four spaces is a code block.
func isATXHeading(line string) bool {
	line = strings.TrimRight(line, " \t\r")
	if indent := len(line) - len(strings.TrimLeft(line, " ")); indent > 3 {
		return false
	}
	line = strings.TrimLeft(line, " ")
	hashes := len(line) - len(strings.TrimLeft(line, "#"))
	if hashes < 1 || hashes > 6 {
		return false
	}
	rest := line[hashes:]
	return rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")
}

// truncateRunes caps s at max RUNES (not bytes — one CJK character is one
// rune, three bytes) and marks the cut with an ellipsis so a reader can tell a
// short duty from a truncated one.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// rosterChars / machinesChars size the two blocks the way the peek reports
// them: the TEXT this payload actually carries. Ids and machine bindings count
// too — they are part of what the caller must read.
func rosterChars(rows []resumeRosterMemberDTO) int {
	n := 0
	for _, r := range rows {
		n += utf8.RuneCountInString(r.ID) + utf8.RuneCountInString(r.Name) +
			utf8.RuneCountInString(r.Kind) + utf8.RuneCountInString(r.RoleName) +
			utf8.RuneCountInString(r.Duty) + utf8.RuneCountInString(r.CurrentTask) +
			utf8.RuneCountInString(r.Machine) + utf8.RuneCountInString(r.Presence) +
			utf8.RuneCountInString(r.TaskStatus) + utf8.RuneCountInString(r.WaitingReason) +
			len(strconv.Itoa(r.ProgressDone)) + len(strconv.Itoa(r.ProgressTotal))
	}
	return n
}

func machinesChars(m resumeMachinesDTO) int {
	n := utf8.RuneCountInString(m.YouAreOn)
	for _, x := range m.List {
		n += utf8.RuneCountInString(x.MachineID) + utf8.RuneCountInString(x.DisplayName)
	}
	return n
}

// GET /api/resume-summary-size — the size-only PEEK of the wake snapshot
// (T-7974 two-step boot, MCP tool peek_resume_summary_size): identity-locked,
// returns ONLY the overview counts/sizes + a derived estimated_total_chars +
// guidance note — NO chat bodies, NO task rows, NO content of any kind. A
// waking agent peeks this FIRST (a few hundred bytes) to decide whether to
// call resume_summary directly in its own context or hand the pull to a cheap
// sub-agent. The counts are assembled through resumeSnapshotParts, the SAME
// code resume_summary runs, so they are consistent by construction.
func (s *apiServer) HandlePeekResumeSummarySizeApiResumeSummarySizeGet(w http.ResponseWriter, r *http.Request) {
	actor := currentActor(r)
	snap, err := s.resumeSnapshotParts(actor)
	if err != nil {
		internalError(w, err)
		return
	}
	overview := snap.Overview
	writeJSON(w, http.StatusOK, resumeSummarySizeDTO{
		Identity: &actor,
		Overview: overview,
		// estimated_total_chars ≈ the context cost of pulling resume_summary
		// AND then expanding every task via get_task: the WHOLE chat block the
		// snapshot carries — whatever chat_chars counts, which is the RENDERED
		// block and not the sum of the bodies (resumeSnapshotParts is the one
		// place that says what goes into it; do not restate the list here, the
		// three prose copies that did each restated a different, incomplete
		// one) — plus the plan text those rows omit. The single number the boot
		// threshold gates on (see the note / boot_sequence).
		//
		// T-1b09: the roster and machine blocks are ADDED here because they are
		// part of what pulling the snapshot costs. Leaving them out would have
		// made the boot threshold understate the real payload by the size of
		// the whole studio floor (measured 7–8k chars while role definitions
		// still carry their operating-manual material) — an agent would decide
		// "small enough to read directly" against a number that no longer
		// describes what it is about to read. They are still reported
		// SEPARATELY in overview as roster_chars / machines_chars, so a caller
		// can tell the two kinds of cost apart; what is deliberately NOT folded
		// in anywhere is tasks_detail_chars' relationship to them — that one
		// counts text this payload does NOT carry.
		EstimatedTotalChars: overview.ChatChars + overview.TasksDetailChars +
			overview.RosterChars + overview.MachinesChars,
		Note: peekNote,
	})
}

// GET /api/members/{member_id}/resume-summary — the SAME bounded wake
// snapshot as /api/resume-summary, for a TARGET member instead of the
// caller (T-8b0d; control-others — routes.go requires=principalAdminAgent,
// so only an owner-scoped token OR an admin-role (assistant) member may
// pull another member's resume snapshot). Assembled by the identical,
// unmodified resumeSnapshotParts(actor) the self-scoped route uses, called
// with actor=member_id — no near-copy of the assembly, so this payload can
// never drift from what resume_summary itself would carry for that member.
// 404 if member_id does not resolve to a LIVE roster row — but the resolver is
// resolveResumeSummaryTarget, NOT resolveMember: this is the ONE member verb
// the owner released to workers (T-4595), so the kind='outsource' fold that
// every other /api/members/{member_id}/... verb keeps is deliberately absent
// here and an `ow-` id gets a 200. resolveMember's other two refusals (absent
// row, soft-removed row) still apply, so a released worker's summary stops
// being readable the moment its roster row goes.
// The original /api/resume-summary route and its identity lock (actor :=
// currentActor(r), caller = target, always) are untouched by this addition.
func (s *apiServer) HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveResumeSummaryTarget(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	snap, err := s.resumeSnapshotParts(m.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resumeSummaryDTO{
		Identity:           &m.ID,
		GeneratedAt:        snap.GeneratedAt,
		Chat:               snap.Chat,
		ChatEarlierOmitted: snap.ChatCut,
		Tasks:              snap.Tasks,
		Roster:             snap.Roster,
		// machines.you_are_on resolves for the TARGET member, not for the
		// admin doing the lookup — this route answers "what does THAT agent
		// wake up to", so every field must be from that agent's vantage.
		Machines: &snap.Machines,
		Overview: snap.Overview,
		Note:     resumeNote,
	})
}

// GET /api/chat/unread-count — the 辦公室 nav red-dot signal: the caller's
// unread across the owner's LIVE conversations — active members + not-yet-
// released outsource workers (removed / released senders are excluded, matching
// what the office actually shows). Kept as its own cheap endpoint so the dot can
// refetch on every "chat" / "chat_read" SSE delta without pulling the roster.
func (s *apiServer) HandleChatUnreadCountApiChatUnreadCountGet(w http.ResponseWriter, r *http.Request) {
	actor := currentActor(r)
	messages, err := s.dal.ListChat()
	if err != nil {
		internalError(w, err)
		return
	}
	receipts, err := s.dal.ListChatReads(actor, "")
	if err != nil {
		internalError(w, err)
		return
	}
	unread := UnreadCounts(messages, receipts, actor)
	// Count only conversations the owner can still see and clear: active
	// members + live (not-yet-released) outsource workers. Removed members and
	// released workers are gone from the office, so their leftover unread must
	// not keep the dot lit (owner 2026-07-14: 外包要算、已移除的不算).
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		internalError(w, err)
		return
	}
	live := make(map[string]bool, len(members)+len(workers))
	for _, m := range members {
		if m.RosterStatus != RosterStatusRemoved {
			live[m.ID] = true
		}
	}
	for _, wk := range workers {
		if wk.Status != WorkerStatusReleased {
			live[wk.ID] = true
		}
	}
	total := 0
	for sender, n := range unread {
		if live[sender] {
			total += n
		}
	}
	writeJSON(w, http.StatusOK, chatUnreadCountDTO{Unread: total})
}
