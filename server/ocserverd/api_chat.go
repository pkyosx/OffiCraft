package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
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
	// Chat budget: global newest-first, stopping at the last message that fits — no
	// per-line floor (owner ruling 2026-08-13). The budget is the chat.budget_chars
	// setting; do not reintroduce a constant here, or the peek and the snapshot start
	// disagreeing about the block's size.
	//
	// resumeChatFetch is 500 so the packer never runs out of candidates before budget:
	// the cheapest message costs 27 runes and 500 × 27 = 13,500 > maxChatBudgetChars
	// (13000). Raising maxChatBudgetChars past 13,500 means raising this FIRST.
	resumeChatFetch = 500

	resumeChatOtherPreview = 120
	// The offset is written because no studio timezone setting exists: the server's
	// local zone is the only one. 🔴 The date is always written, same-day included —
	// a waking agent does not know what day it is.
	resumeTimeLayout = "2006-01-02 15:04:05 -07:00"
	// The owner has no roster row. Deliberately not the owner.name setting:
	// settings.go states the nickname is not an agent read path.
	resumeOwnerDisplayName = "Owner"
	// Says MAY, not DOES: the marker is also raised when the read filled its window,
	// whether or not anything older exists.
	resumeChatCutHint = "這條線上**可能**還有更早的訊息沒被帶進來。它在讀取或字數上限被切斷，而沒有人往切口後面看過——所以就算其實沒有更舊的，這一句也會出現；只有真的去抓才知道。（這跟 `body_omitted_chars` 是**兩回事**：那個是「這一則就在這裡，只是被摺短了」，確定的事實；這一句講的是「整則整則可能不在」，是個可能。）要確認並讀回來：呼叫 `get_chat`，`with` 填對方的 id，再把這份資料裡「對方那條線最舊的那一則」的 `before_ts` 與 `before_id` 一起帶上。這兩個游標欄位**必須成對送**，只送一個會被退回（422）。如果某個人的**整條線一則都不在**這份資料裡（他最後一則太舊，整條被擠出去了），那就沒有游標可抄——這時只填 `with`、不帶游標，直接從最新的往回讀就好。`get_chat` **不會把任何東西標成已讀**（不管有沒有帶游標）：要標已讀是另一隻 API，`POST /api/chat/mark-read`，明確送出才會寫。"
	// Read by every member on every wake, so an unbounded field is paid fleet-wide.
	// 1000 is the owner's number and he ruled to keep it (rc-d88c445397a3) — do not
	// lower it. Same origin as the duty-doc cap but an independent value: that one is
	// a setting (dutyCapCharsDefault), and raising it must not drag this along.
	// Task titles measured ~99 chars average, 147 max, so that cap stays tight.
	resumeDutyPreview      = 1000
	resumeTaskTitlePreview = 40

	resumeNote = "這是一份**開機快照**，不是完整資料。\n聊天：只帶最近的往來，而且是照**字數**（不是則數）收的，收到裝不下為止，由舊到新排。每則都附寄件與收件者的名字、以及帶時區的時間（請跟最上面的 `generated_at` 對照著看）；有回覆卡的會一併附上。\n有兩種「不完整」，意思不一樣，不要混：\n· `body_omitted_chars` > 0 ＝ **這一則就在這裡，只是被摺短了**，數字是被摺掉的字數，這是確定的事實（你自己寫給自己的交接、以及你跟 owner 之間的往來——他說的和你對他說的都算——一律不摺）。要看全文，把那一則的 `id` 放進 `get_chat` 的 `ids`。\n· `chat_earlier_omitted` ＝ **可能整則整則不見了**。這是「可能」不是「一定」：那條線在讀取或字數上限被切斷，而沒有人往切口後面看過，所以就算其實沒有更舊的也會標。它自己會附上怎麼去抓。\n任務：只給精簡列，沒有計畫細節；其中 `answered_card_steps` ＝ **這一步卡在一張 owner 已經回答、卻還沒有人接手的卡上**（`overview` 的 `steps_on_answered_card` ＝ **這份快照帶的這幾列裡**有幾步這樣卡著，不是你所有任務的總數：任務列只帶最近更新的前幾張，你手上票多的時候，更舊的那些就算卡著也不會出現在這裡，也不會被算進去——所以 0 不等於沒有，要確認請用 `list_tasks`／`list_reply_cards`）——那不表示那一步做完了，他的答覆也可能是不通過、要改做，先用 `get_reply_card` 把答案讀完再決定怎麼走。名冊：工作室裡每個人的狀態、所在機器與職責（過長會截斷，`…` 是切口）。機器：機器清單，以及你在哪一台。\n先看 `overview` 的數量與大小，再決定要拉什麼：單張任務用 `get_task`（`detail_chars` 很大的就交給分身去拉），你的卡片用 `list_reply_cards`（記得給 `limit`），要更多聊天或任務用 `list_chat`／`list_tasks`。"

	peekNote                     = "Size-only preview of resume_summary — counts/sizes ONLY, no chat or task content. estimated_total_chars is exactly chat_chars + tasks_detail_chars + roster_chars + machines_chars + steps_on_answered_card_chars, all five reported in overview: the WHOLE chat block as the snapshot renders it (chat_chars is the rendered block's cost, NOT the sum of the message bodies), plus the plan text its task rows omit, the two studio-floor blocks, and the answered-card pointers its task rows carry. steps_on_answered_card > 0 means that many steps AMONG THE FEW MOST-RECENTLY-UPDATED TASKS the snapshot carries — not across all your tasks — are sitting on a reply card the owner ALREADY answered while the step is still in_progress, and nobody has acted on the answer yet; pull resume_summary (or the cards) and read it before anything else. It is a FLOOR, not a total: the task block is capped at the most recently updated tasks, so when you hold more tasks than that cap, an older task stuck on an answered card is not counted here and 0 does not prove there is none — use list_tasks / list_reply_cards to be sure. So it is what pulling the snapshot actually costs. Use it to decide: if small (rule of thumb < 20000 chars, ≈ 5k tokens) call resume_summary directly in your main session; if large, spawn a cheap sub-agent (e.g. haiku) to call resume_summary and return a compressed digest, so the full payload never burns your own context."
	attachmentOctetStream        = "application/octet-stream"
	attachmentDefaultPastedImage = "pasted-image"
	// 4000 was calibrated on a 3,882-message send-side survey (2026-07): agent↔owner
	// p99 1,683; agent↔agent p99 4,894, the blocked tail being material pasted inline.
	// 🔴 Enforced only by the POST /api/chat handler (owner exempt); every other
	// producer writes the row directly and is unbound — enumerate them with
	// `grep -n 'msg := ChatMessage{' server/ocserverd/*.go`.
	chatBodyMaxChars = 4000
	// 128 is the owner's number (c-92c734ef561e). Existing rows were left alone (no
	// migration or truncation), so a read can return a longer name than a write
	// accepts. 🔴 The refusal states the length and limit and nothing else — owner
	// ruling c-b9bb4cfde26a.
	shortLabelMaxChars = 128
)

var imageMimeExt = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// Key and payload per spec/sse.md §2.2.
func (s *apiServer) publishChatRead(receipt ChatRead, trigger string) {
	// Owner-only: no agent consumes chat_read (the ocagent listener has no case).
	s.hub.Publish("chat_read", "patch", "chat_read",
		wireOwnerID+"::"+receipt.ReaderID+"::"+receipt.PeerID,
		map[string]any{
			"reader":       receipt.ReaderID,
			"peer":         receipt.PeerID,
			"last_read_ts": receipt.LastReadTS,
		}, audienceOwnerOnly(), trigger)
}

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

// Only .json, by owner ruling (rc-ab005893c16c); YAML/CSV deliberately not.
func attachmentMimeForName(filename string) string {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".json") {
		return "application/json"
	}
	return ""
}

func attachmentMimeBase(mimeType string) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
}

type chatBadRequest struct{ msg string }

func (e chatBadRequest) Error() string { return e.msg }

// Presence is deliberately NOT a condition: an offline or stopped member still owns
// a mailbox and must receive messages posted before its next connection.
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
		(m.Kind != KindStaff && m.Kind != KindOutsource) {
		return "", errNotFound
	}
	return id, nil
}

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

func resolveChatAttachment(raw []byte, filename, mimeType string) (*ChatAttachment, error) {
	if len(raw) == 0 {
		return nil, chatBadRequest{"attachment is empty"}
	}
	resolved := strings.TrimSpace(mimeType)
	if resolved == "" {
		resolved = sniffAttachmentMime(raw)
		if resolved == attachmentOctetStream {
			if byName := attachmentMimeForName(filename); byName != "" {
				resolved = byName
			}
		}
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
		if n := utf8.RuneCountInString(trimmed); n > shortLabelMaxChars {
			return nil, chatBadRequest{"attachment filename is " +
				strconv.Itoa(n) + " chars, over the " +
				strconv.Itoa(shortLabelMaxChars) + "-char limit"}
		}
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

// The one light-ref shape for a stored blob: meta["attachments"], reply-card
// answer_attachments, and the upload response.
func attachmentRef(att *ChatAttachment) map[string]any {
	filename := ""
	if att.Filename != nil {
		filename = *att.Filename
	}
	return map[string]any{"id": att.ID, "mime": att.Mime, "filename": filename}
}

// POST /api/chat/attachments — the raw body is the file (`ocagent upload`). The
// request Content-Type is deliberately ignored: every client defaults it to
// application/octet-stream, indistinguishable from a declaration; use ?mime=.
func (s *apiServer) HandleUploadChatAttachmentApiChatAttachmentsPost(w http.ResponseWriter, r *http.Request, params HandleUploadChatAttachmentApiChatAttachmentsPostParams) {
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

type resolvedAttachment struct {
	att   *ChatAttachment
	store bool
}

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
		// Neither id nor bytes: refused, not dropped — owner ruling rc-3a589dfec503.
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

// Stores NOTHING: the caller hands both halves to one transactional write. A blob
// written before its record is unreachable (the gallery and the deletion cascade
// start from the record's refs).
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

func (s *apiServer) HandlePostChatApiChatPost(w http.ResponseWriter, r *http.Request) {
	var body ChatPostDTO
	if !decodeJSONBodyRequired(w, r, &body, "to") {
		return
	}
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
	// Drop any caller-supplied reply link: one arriving pre-made in meta would bypass
	// the existence check below.
	delete(meta, chatReplyToMetaKey)
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		inputs = *body.Attachments
	}
	if len(inputs) > chatAttachmentsMaxCount {
		writeError(w, http.StatusBadRequest,
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
	// Existence is the only gate — no same-conversation check (owner ruling
	// 2026-08-21): quoting a line out of another thread is the use case.
	if replyTo := trimString(strOrEmpty(body.ReplyTo)); replyTo != "" {
		quoted, err := s.dal.ListChatByIDs([]string{replyTo})
		if err != nil {
			internalError(w, err)
			return
		}
		if len(quoted) == 0 {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf(chatReplyToUnknownMsg, replyTo))
			return
		}
		meta[chatReplyToMetaKey] = replyTo
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
	// Payload {id, from, to} per spec/sse.md §2.2; audience per spec §4.
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), msg.Sender)
	if msg.Recipient == wireOwnerID && msg.Sender != wireOwnerID {
		s.enqueueWebPush(webPushPayload{
			Kind: "chat", ChatMessageID: msg.ID, ChatPeerID: msg.Sender, Title: "OffiCraft 有新訊息",
			Body: "你有一則新訊息。",
		})
	}
	// 🔴 A receipt, not the message. Do not reintroduce a post-commit read here: its
	// failure would 500 about a message already stored, fanned and pushed.
	writeJSON(w, http.StatusOK, chatPostReceiptOf(msg))
}

func chatPostReceiptOf(m ChatMessage) chatPostReceiptDTO {
	return chatPostReceiptDTO{
		ID:          m.ID,
		To:          m.Recipient,
		TS:          m.TS,
		Attachments: newChatMessageDTO(m).Attachments,
	}
}

// Stored meta holds only the card id (never updated on answer), so
// reply_card_status is joined here, best-effort.
func (s *apiServer) servedChatMessageDTO(m ChatMessage) (chatMessageDTO, error) {
	dto := newChatMessageDTO(m)
	if id := replyCardIDFromMeta(m.Meta); id != "" {
		if c, err := s.dal.GetReplyCard(id); err == nil && c != nil {
			dto.ReplyCardStatus = c.Status
		}
	}
	// The quote is joined HERE so every door that serves a message serves it (the
	// listing, the history page, the by-ids read). Unlike the card status it is not
	// best-effort: its absence is drawn on screen as an assertion.
	quote, err := s.chatReplyQuote(dto.ReplyTo, nil)
	if err != nil {
		return chatMessageDTO{}, err
	}
	dto.ReplyToChat = quote
	return dto, nil
}

// 🔴 No condition, no cache, no batch — one point read per replying message (owner
// ruling 2026-08-21).
//
// 🔴 A read failure is not a miss. Absence renders as the fixed sentence "This
// message no longer exists", so an error goes up as a 500 instead — owner ruling
// 2026-08-21: bad data should be noisy. The price, measured: one unreadable quoted
// row 500s ?ids=, paging and both resume-summary endpoints, so an agent that
// replied to it cannot boot.
func (s *apiServer) chatReplyQuote(replyTo string, names map[string]string) (*chatReplyQuoteDTO, error) {
	if replyTo == "" {
		return nil, nil
	}
	quoted, err := s.dal.ListChatByIDs([]string{replyTo})
	if err != nil {
		return nil, err
	}
	if len(quoted) == 0 {
		return nil, nil
	}
	return newChatReplyQuoteDTO(quoted[0], names), nil
}

const (
	// A response bound: bodies come back whole, so 20 × chatBodyMaxChars is the worst
	// case. Deliberately not enough to unfold a whole snapshot's folds in one call.
	chatByIDsMax = 20

	chatByIDsTooManyMsg = "get_chat accepts at most %d ids per call (asked for %d) — " +
		"messages come back with their bodies whole, so the count is what bounds " +
		"the response; name the ones you actually need and call again for the rest"

	chatByIDsNotFoundMsg = "no message carries id %s — the whole call is refused rather " +
		"than answered short, because a shortened answer is indistinguishable from the " +
		"folded message you are trying to read back; drop that id and ask again"
	// A ROW count, not a payload bound (200 rows measured 687 KB). Window path only:
	// on the anchorless legacy path limit=-1 (uncapped) is a spec-verbatim promise
	// with committed callers.
	chatWindowMaxLimit = 200

	chatWindowBadLimitMsg = "limit must be between 1 and %d when start_id or end_id is " +
		"given (got %d) — the legacy anchorless listing keeps its own semantics, " +
		"where a negative limit is uncapped and 0 is an empty page"

	chatWindowAnchorNotFoundMsg = "no message carries id %s — a window anchor must name a " +
		"real message, because an empty page is what a real window at the edge of the " +
		"stream returns and the two must not be indistinguishable"

	chatWindowMixedCursorsMsg = "start_id/end_id cannot be combined with the deprecated " +
		"before_ts/before_id cursor — the two families disagree about direction; " +
		"send one family or the other"

	chatWindowContradictionMsg = "start_id %s is newer than end_id %s — the window is " +
		"empty by construction; refused rather than answered with an empty array, " +
		"which is what a real empty window returns"

	chatReplyToMetaKey = "reply_to"

	chatReplyToUnknownMsg = "reply_to names no message (%s) — you can only reply to a message " +
		"that exists; re-read the conversation and use the id it carries"

	chatCursorUnreadableMsg = "cursor is not a cursor this API issued — copy the previous " +
		"response's next_cursor back verbatim; it is opaque, and constructing or " +
		"editing one is not supported"

	chatCursorWrongWayMsg = "this cursor continues %s and cannot be used on a path that " +
		"continues %s — a cursor belongs to the query that minted it; start that " +
		"walk again without one"

	chatCursorMixedMsg = "cursor cannot be combined with before_ts/before_id or " +
		"start_id/end_id — one keyset walk per request; send the cursor alone"

	chatUnreadMixedMsg = "unread cannot be combined with before_ts/before_id or " +
		"start_id/end_id — those name a position in the whole stream, unread names " +
		"the set your read watermarks define; page unread with cursor instead"

	chatUnknownParamMsg = "unknown query parameter(s) on GET /api/chat: %s — this route " +
		"refuses parameters it does not declare rather than ignoring them, because an " +
		"ignored parameter silently answers a question you did not ask; accepted here: %s"

	chatUnreadTrue = "true"

	chatCursorOlder = "o"
	chatCursorNewer = "n"

	chatCursorOlderName = "towards older messages"
	chatCursorNewerName = "towards newer messages"
)

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

// 🔴 No watermark advance: re-reading an unfolded line is not reading the thread.
// 🔴 No participation check — a deliberate widening (owner ruling): the
// listing already serves the same rows by participant. If this reach is ever
// wrong, it is wrong for both doors; fix both.
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
	s.writeChatPage(w, msgs, "")
}

// Unknown query parameters are refused on THIS route only (owner ruling
// rc-84f98080af16); other routes ignore them. The accepted set is read off the
// generated params struct, plus the ?token= credential extractToken accepts for
// clients that cannot set a header (EventSource, <img src>).

func chatDeclaredQueryParams() []string {
	out := make([]string, 0, len(chatQueryParamSet))
	for name := range chatQueryParamSet {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func unknownChatQueryParams(r *http.Request) []string {
	var unknown []string
	for name := range r.URL.Query() {
		if !chatQueryParamSet[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

var chatQueryParamSet = func() map[string]bool {
	out := map[string]bool{authTokenQueryParam: true}
	t := reflect.TypeOf(HandleListChatApiChatGetParams{})
	for i := 0; i < t.NumField(); i++ {
		tag, ok := t.Field(i).Tag.Lookup("form")
		if !ok {
			continue
		}
		if name, _, _ := strings.Cut(tag, ","); name != "" {
			out[name] = true
		}
	}
	return out
}()

// Cursor: base64url of "<dir>\x00<ts>\x00<id>". 'g' with precision -1 round-trips
// the float64 exactly, which the strict keyset comparison needs.

func encodeChatCursor(dir string, a chatAnchor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(
		dir + "\x00" + strconv.FormatFloat(a.TS, 'g', -1, 64) + "\x00" + a.ID))
}

func decodeChatCursor(token, want string) (chatAnchor, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return chatAnchor{}, chatCursorUnreadableMsg, false
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || parts[2] == "" {
		return chatAnchor{}, chatCursorUnreadableMsg, false
	}
	dir := parts[0]
	if dir != chatCursorOlder && dir != chatCursorNewer {
		return chatAnchor{}, chatCursorUnreadableMsg, false
	}
	ts, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return chatAnchor{}, chatCursorUnreadableMsg, false
	}
	if dir != want {
		return chatAnchor{}, fmt.Sprintf(chatCursorWrongWayMsg,
			chatCursorDirName(dir), chatCursorDirName(want)), false
	}
	return chatAnchor{TS: ts, ID: parts[2]}, "", true
}

func chatCursorDirName(dir string) string {
	if dir == chatCursorNewer {
		return chatCursorNewerName
	}
	return chatCursorOlderName
}

func chatPageWindow(limit int) int {
	if limit <= 0 {
		return limit
	}
	return limit + 1
}

func (s *apiServer) writeChatPage(w http.ResponseWriter, msgs []ChatMessage, nextCursor string) {
	out := []chatMessageDTO{}
	for _, m := range msgs {
		dto, err := s.servedChatMessageDTO(m)
		if err != nil {
			internalError(w, err)
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, chatListDTO{Messages: out, NextCursor: nextCursor})
}

// PRESENCE, not emptiness, selects the window path: a blank ?start_id= is refused
// as an unknown id rather than falling back to the unbounded legacy listing (the
// generated binder yields a pointer to "" for it and nil when absent — measured).
func requestedChatWindow(params HandleListChatApiChatGetParams) (startID string, hasStart bool, endID string, hasEnd bool) {
	if params.StartId != nil {
		startID, hasStart = *params.StartId, true
	}
	if params.EndId != nil {
		endID, hasEnd = *params.EndId, true
	}
	return startID, hasStart, endID, hasEnd
}

type chatWindowRequest struct {
	filter     chatListFilter
	limit      int
	startID    string
	hasStart   bool
	endID      string
	hasEnd     bool
	beforeSent bool
	cursorSent bool
}

// Refusal order is fixed and tested: mixed cursors, limit, unknown anchor (404),
// contradictory pair.
func (s *apiServer) serveChatWindow(w http.ResponseWriter, r *http.Request, req chatWindowRequest) {
	if req.beforeSent {
		writeError(w, http.StatusUnprocessableEntity, chatWindowMixedCursorsMsg)
		return
	}
	if req.cursorSent {
		writeError(w, http.StatusUnprocessableEntity, chatCursorMixedMsg)
		return
	}
	if req.limit < 1 || req.limit > chatWindowMaxLimit {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf(chatWindowBadLimitMsg, chatWindowMaxLimit, req.limit))
		return
	}
	wanted := []string{}
	if req.hasStart {
		wanted = append(wanted, req.startID)
	}
	if req.hasEnd && req.endID != req.startID {
		wanted = append(wanted, req.endID)
	}
	found, err := s.dal.ListChatByIDs(wanted)
	if err != nil {
		internalError(w, err)
		return
	}
	byID := make(map[string]ChatMessage, len(found))
	for _, m := range found {
		byID[m.ID] = m
	}
	// Anchors resolve WITHOUT the listing filters on purpose: a real id outside the
	// filter must not 404 as nonexistent; the window just comes back empty.
	var start, end *chatAnchor
	if req.hasStart {
		m, ok := byID[req.startID]
		if !ok {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf(chatWindowAnchorNotFoundMsg, req.startID))
			return
		}
		start = &chatAnchor{TS: m.TS, ID: m.ID}
	}
	if req.hasEnd {
		m, ok := byID[req.endID]
		if !ok {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf(chatWindowAnchorNotFoundMsg, req.endID))
			return
		}
		end = &chatAnchor{TS: m.TS, ID: m.ID}
	}
	if start != nil && end != nil && start.newerThan(*end) {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf(chatWindowContradictionMsg, req.startID, req.endID))
		return
	}
	msgs, err := s.dal.listChatWindow(req.filter, start, end, req.limit)
	if err != nil {
		internalError(w, err)
		return
	}
	s.writeChatPage(w, msgs, "")
}

// 🔴 This route never writes a read watermark, the unread path included — owner
// ruling 2026-09-02; POST /api/chat/mark-read is the only door. (Measured harm: a
// listener-only agent had its unread cleared by polling.)
func (s *apiServer) HandleListChatApiChatGet(w http.ResponseWriter, r *http.Request, params HandleListChatApiChatGetParams) {
	if unknown := unknownChatQueryParams(r); len(unknown) > 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(chatUnknownParamMsg,
			strings.Join(unknown, ", "), strings.Join(chatDeclaredQueryParams(), ", ")))
		return
	}
	if ids := requestedChatIDs(params.Ids); len(ids) > 0 {
		s.serveChatByIDs(w, r, ids)
		return
	}
	actor := currentActor(r)
	filter := chatListFilter{
		participant: strOrEmpty(params.With),
		sender:      strOrEmpty(params.Sender),
		recipient:   strOrEmpty(params.Recipient),
	}
	limit := chatListDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	startID, hasStart, endID, hasEnd := requestedChatWindow(params)
	beforeSent := params.BeforeTs != nil || params.BeforeId != nil
	cursorSent := params.Cursor != nil

	if strOrEmpty(params.Unread) == chatUnreadTrue {
		if beforeSent || hasStart || hasEnd {
			writeError(w, http.StatusUnprocessableEntity, chatUnreadMixedMsg)
			return
		}
		s.serveChatUnread(w, actor, filter, limit, params.Cursor)
		return
	}
	if hasStart || hasEnd {
		s.serveChatWindow(w, r, chatWindowRequest{
			filter:     filter,
			limit:      limit,
			startID:    startID,
			hasStart:   hasStart,
			endID:      endID,
			hasEnd:     hasEnd,
			beforeSent: beforeSent,
			cursorSent: cursorSent,
		})
		return
	}
	if beforeSent {
		if cursorSent {
			writeError(w, http.StatusUnprocessableEntity, chatCursorMixedMsg)
			return
		}
		if params.BeforeTs == nil || params.BeforeId == nil {
			writeError(w, http.StatusUnprocessableEntity,
				"before_ts and before_id must be supplied together")
			return
		}
		s.serveChatOlder(w, filter, chatAnchor{TS: *params.BeforeTs, ID: *params.BeforeId}, limit)
		return
	}
	if cursorSent {
		before, refusal, ok := decodeChatCursor(*params.Cursor, chatCursorOlder)
		if !ok {
			writeError(w, http.StatusUnprocessableEntity, refusal)
			return
		}
		s.serveChatOlder(w, filter, before, limit)
		return
	}
	msgs, err := s.dal.listChatLatest(filter, chatPageWindow(limit))
	if err != nil {
		internalError(w, err)
		return
	}
	msgs, next := trimChatPageOlder(msgs, limit)
	s.writeChatPage(w, msgs, next)
}

func (s *apiServer) serveChatOlder(w http.ResponseWriter, f chatListFilter, before chatAnchor, limit int) {
	msgs, err := s.dal.listChatBefore(f, before.TS, before.ID, chatPageWindow(limit))
	if err != nil {
		internalError(w, err)
		return
	}
	msgs, next := trimChatPageOlder(msgs, limit)
	s.writeChatPage(w, msgs, next)
}

func trimChatPageOlder(msgs []ChatMessage, limit int) ([]ChatMessage, string) {
	if limit <= 0 || len(msgs) <= limit {
		return msgs, ""
	}
	msgs = msgs[len(msgs)-limit:]
	return msgs, encodeChatCursor(chatCursorOlder, chatAnchor{TS: msgs[0].TS, ID: msgs[0].ID})
}

func (s *apiServer) serveChatUnread(w http.ResponseWriter, actor string, f chatListFilter, limit int, cursor *string) {
	var after *chatAnchor
	if cursor != nil {
		a, refusal, ok := decodeChatCursor(*cursor, chatCursorNewer)
		if !ok {
			writeError(w, http.StatusUnprocessableEntity, refusal)
			return
		}
		after = &a
	}
	if limit == 0 {
		s.writeChatPage(w, nil, "")
		return
	}
	msgs, err := s.dal.listChatUnread(actor, f, after, chatPageWindow(limit))
	if err != nil {
		internalError(w, err)
		return
	}
	msgs, next := trimChatPageNewer(msgs, limit)
	s.writeChatPage(w, msgs, next)
}

func trimChatPageNewer(msgs []ChatMessage, limit int) ([]ChatMessage, string) {
	if limit <= 0 || len(msgs) <= limit {
		return msgs, ""
	}
	msgs = msgs[:limit]
	last := msgs[len(msgs)-1]
	return msgs, encodeChatCursor(chatCursorNewer, chatAnchor{TS: last.TS, ID: last.ID})
}

func isPreviewableAttachment(mime, filename string) bool {
	base := attachmentMimeBase(mime)
	if strings.HasPrefix(base, "image/") || strings.HasPrefix(base, "text/") ||
		base == "application/pdf" || base == "application/json" {
		return true
	}
	return (base == "" || base == attachmentOctetStream) && attachmentMimeForName(filename) != ""
}

// Non-image previewables go inline under CSP sandbox: an inline HTML blob must never
// script on this origin.
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
	name := attachmentId
	if att.Filename != nil && *att.Filename != "" {
		name = *att.Filename
	}
	if !strings.HasPrefix(att.Mime, "image/") {
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
		if isPreviewableAttachment(att.Mime, name) {
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
	if attachmentMimeBase(mediaType) == attachmentOctetStream {
		if byName := attachmentMimeForName(name); byName != "" {
			mediaType = byName
		}
	}
	if _, _, err := mime.ParseMediaType(mediaType); err != nil {
		mediaType = attachmentOctetStream
	}
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(att.Data)
}

// The URL is server-relative; the client prefixes its own origin.
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
			"?sig=" + shareSigForRing(s.keys, attachmentId),
	})
}

func (s *apiServer) HandleListChatAttachmentsApiChatAttachmentsGet(w http.ResponseWriter, r *http.Request, params HandleListChatAttachmentsApiChatAttachmentsGetParams) {
	peer := trimmedOrEmpty(params.With)
	if peer == "" {
		writeError(w, http.StatusUnprocessableEntity, "with is required")
		return
	}
	refs, err := s.dal.ListChatAttachmentRefsFor(peer)
	if err != nil {
		internalError(w, err)
		return
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	names := map[string]string{}
	for _, m := range members {
		names[m.ID] = m.Name
	}
	// The "never fabricate a serve URL" guard lives in the migration: its backfill
	// and triggers drop id-less refs.
	entries := []chatGalleryEntryDTO{}
	for _, r := range refs {
		entries = append(entries, chatGalleryEntryDTO{
			ID:        r.AttachmentID,
			URL:       "/api/chat/attachment/" + r.AttachmentID,
			Filename:  r.Filename,
			Mime:      r.Mime,
			IsImage:   strings.HasPrefix(r.Mime, "image/"),
			MessageID: r.MessageID,
			From:      r.Sender,
			FromName:  names[r.Sender],
			To:        r.Recipient,
			TS:        r.TS,
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

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
	writeJSON(w, http.StatusOK, chatMarkReadReceiptDTO{
		PeerID:     effective.PeerID,
		LastReadTS: effective.LastReadTS,
		Advanced:   advanced,
	})
}

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

// GET /api/resume-summary — the wake snapshot: recent chat (this file), open tasks as
// light rows (resumeTasksFor, api_tasks.go; SPEC §6.2), studio floor, overview.
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

type resumeWakeSnapshot struct {
	GeneratedAt string
	Chat        []chatMessageDTO
	ChatCut     resumeChatCutDTO
	Tasks       []resumeTaskDTO
	Roster      []resumeRosterMemberDTO
	Machines    resumeMachinesDTO
	Overview    resumeOverviewDTO
}

// resume_summary and peek_resume_summary_size both go through this one assembly,
// and it holds the ONE size estimator — a second one written for the peek is how
// the peek starts lying.
func (s *apiServer) resumeSnapshotParts(actor string) (resumeWakeSnapshot, error) {
	snap := resumeWakeSnapshot{
		GeneratedAt: resumeDisplayTime(nowSecs()),
		Chat:        []chatMessageDTO{},
	}
	roster, machines, rosterChars, machinesChars, names, err := s.resumeFloorParts(actor)
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	snap.Roster, snap.Machines = roster, machines

	// ONE ListReplyCards (a full scan already on this path) feeds both the counts and
	// the inline card fold; never a per-message GetReplyCard.
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
			if c.FromMember != actor {
				continue
			}
			switch {
			case c.Status == replyCardStatusWaiting:
				cardsWaiting++
			case c.Status == replyCardStatusAnswered &&
				now-c.AnsweredTS <= replyCardRecentWindowSecs:
				cardsAnsweredRecent++
			}
		}
	}

	msgs, err := s.dal.ListChatInvolving(actor, resumeChatFetch)
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	chat, cut, chatChars, err := s.resumeChatBlock(
		actor, msgs, names, cardsByID,
		resumeChatPackBudget(s.chatBudget(), snap.GeneratedAt))
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	snap.Chat, snap.ChatCut = chat, cut

	tasks, tasksOpenTotal, err := s.resumeTasksFor(actor, cardsByID)
	if err != nil {
		return resumeWakeSnapshot{}, err
	}
	snap.Tasks = tasks
	detailChars := 0
	answeredSteps, answeredStepChars := 0, 0
	for _, t := range tasks {
		detailChars += t.DetailChars
		answeredSteps += len(t.AnsweredCardSteps)
		answeredStepChars += answeredCardStepChars(t.AnsweredCardSteps)
	}
	snap.Overview = resumeOverviewDTO{
		ChatCount: len(chat),
		ChatChars: chatChars + utf8.RuneCountInString(snap.GeneratedAt) +
			utf8.RuneCountInString(cut.Hint),
		TasksReturned:       len(tasks),
		TasksOpenTotal:      tasksOpenTotal,
		TasksDetailChars:    detailChars,
		CardsWaiting:        cardsWaiting,
		CardsAnsweredRecent: cardsAnsweredRecent,
		RosterChars:         rosterChars,
		MachinesChars:       machinesChars,

		StepsOnAnsweredCard:      answeredSteps,
		StepsOnAnsweredCardChars: answeredStepChars,
	}
	return snap, nil
}

func resumeDisplayTime(ts float64) string {
	if ts <= 0 {
		return ""
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Local().Format(resumeTimeLayout)
}

func resumeDisplayName(id string, names map[string]string) string {
	if id == wireOwnerID {
		return resumeOwnerDisplayName
	}
	return names[id]
}

// Exempt from collapsing: the self hand-off (an agent's baton to its next session
// is a post_chat to itself) and anything to or from the owner.
func resumeChatCarriesFullBody(subject string, m ChatMessage) bool {
	if subject != "" && m.Sender == subject && m.Recipient == subject {
		return true
	}
	return m.Sender == wireOwnerID || m.Recipient == wireOwnerID
}

func (s *apiServer) resumeChatMessageDTO(subject string, m ChatMessage, names map[string]string, cards map[string]ReplyCard) (chatMessageDTO, error) {
	d := newChatMessageDTO(m)
	quote, err := s.chatReplyQuote(d.ReplyTo, names)
	if err != nil {
		return chatMessageDTO{}, err
	}
	d.ReplyToChat = quote
	d.FromName = resumeDisplayName(m.Sender, names)
	d.ToName = resumeDisplayName(m.Recipient, names)
	d.TSDisplay = resumeDisplayTime(m.TS)
	if !resumeChatCarriesFullBody(subject, m) {
		if r := []rune(d.Body); len(r) > resumeChatOtherPreview {
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
			if c.FromMember == subject {
				options := c.Options
				if options == nil {
					options = []ReplyCardOption{}
				}
				d.Card = &chatInlineReplyCardDTO{
					Options:           options,
					AnswerOptionIdxs:  c.AnswerOptionIdxs,
					AnswerText:        c.AnswerText,
					AnsweredTS:        c.AnsweredTS,
					AnsweredAtDisplay: resumeDisplayTime(c.AnsweredTS),
				}
			}
		}
	}
	return d, nil
}

// Fold only when the saving strictly exceeds the marker's own cost (owner
// 2026-08-13: 「省不到就不要折」); that cost mirrors resumeChatMessageChars' billing.
func resumeChatCollapseIsWorthIt(omitted int) bool {
	markerCost := 1 +
		len(strconv.Itoa(omitted))
	return omitted > markerCost
}

// No id-shaped field is billed — a rule for every such field, present and future.
func resumeChatMessageChars(d chatMessageDTO) int {
	n := utf8.RuneCountInString(d.Body) +
		utf8.RuneCountInString(d.FromName) +
		utf8.RuneCountInString(d.ToName) +
		utf8.RuneCountInString(d.TSDisplay) +
		len(strconv.Itoa(d.BodyOmittedChars))
	if d.ReplyToChat != nil {
		n += utf8.RuneCountInString(d.ReplyToChat.FromName) +
			utf8.RuneCountInString(d.ReplyToChat.ToName) +
			utf8.RuneCountInString(d.ReplyToChat.Content)
	}
	if d.Card != nil {
		for _, o := range d.Card.Options {
			n += utf8.RuneCountInString(o.Text)
		}
		n += utf8.RuneCountInString(d.Card.AnswerText) +
			utf8.RuneCountInString(d.Card.AnsweredAtDisplay)
		for _, idx := range d.Card.AnswerOptionIdxs {
			n += len(strconv.Itoa(idx))
		}
	}
	return n
}

// 🔴 STOP at the first message that does not fit, never skip past it — the owner's
// ruling is a contiguous newest-first prefix. Project inside the walk: each
// projection costs a point query for the reply quote.
func (s *apiServer) resumeChatBlock(subject string, msgs []ChatMessage, names map[string]string, cards map[string]ReplyCard, budget int) ([]chatMessageDTO, resumeChatCutDTO, int, error) {
	// Deliberately one-sided: a full fetch window reports a cut even if nothing older
	// exists — one wasted get_chat beats a conversation the reader never learns of.
	atFetchCap := len(msgs) >= resumeChatFetch

	used := 0
	dropped := false
	rev := make([]chatMessageDTO, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		d, err := s.resumeChatMessageDTO(subject, msgs[i], names, cards)
		if err != nil {
			return nil, resumeChatCutDTO{}, 0, err
		}
		cost := resumeChatMessageChars(d)
		if used+cost > budget {
			dropped = true
			break
		}
		used += cost
		rev = append(rev, d)
	}

	chat := []chatMessageDTO{}
	for i := len(rev) - 1; i >= 0; i-- {
		chat = append(chat, rev[i])
	}

	cut := resumeChatCutDTO{}
	if dropped || atFetchCap {
		cut.Omitted = true
		cut.Hint = resumeChatCutHint
	}
	return chat, cut, used, nil
}

// The hint is reserved UNCONDITIONALLY: reserving it only when needed is circular
// (whether it appears depends on whether the pack overflowed).
func resumeChatPackBudget(budget int, generatedAt string) int {
	b := budget -
		utf8.RuneCountInString(generatedAt) -
		utf8.RuneCountInString(resumeChatCutHint)
	if b < 0 {
		return 0
	}
	return b
}

// Roster (owner ruling rc-4e98c0481852) and machine block (rc-09476f535b59).
// 🔴 Every agent runs this on every wake, so a query here is paid fleet-wide.
// Deliberately NOT the GET /api/members path (its unread counts are a chat-wide
// aggregate), and contractors take point queries, not ListOpenTasksByExecutor
// (task.executor_id has no index: a full task scan per contractor).
func (s *apiServer) resumeFloorParts(actor string) ([]resumeRosterMemberDTO, resumeMachinesDTO, int, int, map[string]string, error) {
	members, err := s.dal.ListMembers()
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
			roleNameByRole[roleKey], dutyByRole[roleKey] = "", ""
			return "", ""
		}
		roleNameByRole[roleKey] = def.Name
		dutyByRole[roleKey] = dutyText(def.DefinitionMD)
		return roleNameByRole[roleKey], dutyByRole[roleKey]
	}

	// Built BEFORE the roster filters below: a dismissed colleague, a released
	// contractor and a warden must still read by name in old conversations.
	names := make(map[string]string, len(members))
	for _, m := range members {
		names[m.ID] = m.Name
	}

	staff := []resumeRosterMemberDTO{}
	contractors := []resumeRosterMemberDTO{}
	machines := []resumeMachineDTO{}
	// Captured BEFORE the roster-status and warden filters: this route admits warden
	// tokens and a just-deactivated caller, who still need their own machine.
	callerHost := ""
	for _, m := range members {
		if m.ID == actor {
			callerHost = s.observedHost(m)
		}
		if m.RosterStatus != RosterStatusActive {
			continue
		}
		if m.Kind == machineKind {
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
		// Same observedHost as the roster rows, never a hostname: our hosts report the
		// same name as each other.
		YouAreOn: callerHost,
	}
	return roster, machinesBlock, rosterChars(roster), machinesChars(machinesBlock), names, nil
}

// A contractor id is minted per task, so its task title IS its duty (owner ruling
// rc-a02d8bc7fe23).
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

// A flat cap on the definition minus its title — deliberately no summarising or
// line picking: the owner replaced that heuristic with the cap.
func dutyText(md string) string {
	return truncateRunes(stripLeadingTitle(md), resumeDutyPreview)
}

// Removes only the FIRST heading line: in an outline-style role doc the following
// 「## 負責…」 lines ARE the duty. A title-only doc comes back whole (an empty duty
// would read as "no role").
func stripLeadingTitle(md string) string {
	trimmed := strings.TrimRight(md, " \t\r\n")
	// Skip leading blank lines WITHOUT de-indenting the first content line: four
	// spaces make it a code block, and isATXHeading must see that.
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

// By the syntax rule, not HasPrefix("#"): 「#1 順位」 and 「#hashtag」 are body text.
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

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

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

func answeredCardStepChars(rows []resumeAnsweredCardStepDTO) int {
	n := 0
	for _, r := range rows {
		n += utf8.RuneCountInString(r.StepID) + utf8.RuneCountInString(r.StepName) +
			utf8.RuneCountInString(r.CardID)
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
		// Every block serialised into the payload is an addend, plus tasks_detail_chars
		// (plan text the caller would have to fetch). ⚠️ Nothing catches a new block left
		// out of this sum — add its addend and assertion by hand.
		EstimatedTotalChars: overview.ChatChars + overview.TasksDetailChars +
			overview.RosterChars + overview.MachinesChars +
			overview.StepsOnAnsweredCardChars,
		Note: peekNote,
	})
}

// Auth is in routes.go (principalAdminAgent). anyMember scope: a contractor's
// summary is readable by design (rc-64b712bfc703).
func (s *apiServer) HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
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

		Machines: &snap.Machines,
		Overview: snap.Overview,
		Note:     resumeNote,
	})
}

// Its own cheap endpoint so the nav red dot can refetch on every chat / chat_read
// SSE delta without pulling the roster.
func (s *apiServer) HandleChatUnreadCountApiChatUnreadCountGet(w http.ResponseWriter, r *http.Request) {
	unread, err := s.unreadCountsForRequest(r)
	if err != nil {
		internalError(w, err)
		return
	}
	// Removed members and released workers do not count (owner 2026-07-14).
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
