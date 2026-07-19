package main

// api_chat.go — the chat surface (handlers.handle_post_chat …
// handle_list_chat_reads): sender ALWAYS the verified JWT sub, attachments
// decoded/validated all-or-nothing before any blob is stored, the light
// meta["attachments"] refs the only message→blob linkage, and the monotonic
// per-conversation read watermark.

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	chatListDefaultLimit        = 30
	chatAttachmentImageMaxBytes = 20 * 1024 * 1024
	chatAttachmentMaxBytes      = 100 * 1024 * 1024
	chatAttachmentsMaxCount     = 10
	resumeChatN                 = 30
	resumeChatBodyPreview       = 500
	resumeNote                  = "This is a BOUNDED wake snapshot (recent chat involving you, bodies truncated; your open tasks as LIGHT rows — no plan detail). Peek `overview` first (sizes/counts), then pull only what you need: get_task per task (hand a big detail_chars pull to a sub-agent), list_reply_cards (use `limit`) for your cards, list_chat / list_tasks for more."
	// peekNote guides the two-step boot (T-7974): peek_resume_summary_size is
	// size-only (no content); the agent reads estimated_total_chars and, when
	// it is small, calls resume_summary directly in its own context, else has
	// a cheap sub-agent (e.g. haiku) call resume_summary and hand back a
	// compressed digest — the full payload never burns the main session.
	peekNote                     = "Size-only preview of resume_summary — counts/sizes ONLY, no chat or task content. Use estimated_total_chars to decide: if small (rule of thumb < 20000 chars, ≈ 5k tokens) call resume_summary directly in your main session; if large, spawn a cheap sub-agent (e.g. haiku) to call resume_summary and return a compressed digest, so the full payload never burns your own context."
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
	// tighter value can follow the post-guideline distribution. Owner (sender ==
	// wireOwnerID — a human is never blocked by the system) and the hook:* ingest
	// path (external payload, separate webhook handler) are exempt.
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
		att, err := decodeChatAttachment(
			strOrEmpty(a.DataB64), strOrEmpty(a.Filename), strOrEmpty(a.Mime))
		if err != nil {
			return nil, http.StatusBadRequest, err.Error()
		}
		resolved = append(resolved, resolvedAttachment{att: att, store: true})
	}
	return resolved, 0, ""
}

// storeResolvedAttachments stores every fresh blob and returns the light
// [{id, mime, filename}] refs the message meta carries.
func (s *apiServer) storeResolvedAttachments(resolved []resolvedAttachment) ([]any, error) {
	refs := make([]any, 0, len(resolved))
	for _, ra := range resolved {
		if ra.store {
			if err := s.dal.PutChatAttachment(*ra.att); err != nil {
				return nil, err
			}
		}
		refs = append(refs, attachmentRef(ra.att))
	}
	return refs, nil
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
	// exempt by sender identity (the human is never blocked by the system); the
	// hook:* ingest path never reaches here (separate webhook handler). The
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
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		for _, a := range *body.Attachments {
			// An item with neither id nor data_b64 is silently dropped (legacy
			// tolerance, unchanged).
			if strOrEmpty(a.DataB64) != "" || trimmedOrEmpty(a.Id) != "" {
				inputs = append(inputs, a)
			}
		}
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
	if len(resolved) > 0 {
		refs, err := s.storeResolvedAttachments(resolved)
		if err != nil {
			internalError(w, err)
			return
		}
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
		Recipient: body.To,
		Body:      strOrEmpty(body.Body),
		TS:        nowSecs(),
		Meta:      meta,
	}
	if err := s.dal.PutChat(msg); err != nil {
		internalError(w, err)
		return
	}
	// The chat convenience payload is exactly {id, from, to} (spec/sse.md §2.2).
	// Addressed to both participants + owner (spec §4).
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), msg.Sender)
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
func (s *apiServer) HandleListChatApiChatGet(w http.ResponseWriter, r *http.Request, params HandleListChatApiChatGetParams) {
	with := strOrEmpty(params.With)
	peek := trimmedOrEmpty(params.Peek) == "true"
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
		msgs, err := s.dal.ListChatBefore(with, *params.BeforeTs, *params.BeforeId, limit)
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
// (handlers.handle_resume_summary): the caller's recent chat (bodies
// truncated) + the caller's open tasks as LIGHT rows (SPEC §6.2 — a handover
// resumes in-flight tasks, not just chat; assembled by resumeTasksFor,
// api_tasks.go; T-3f31: no plan detail rides the snapshot) + the overview
// size/概要 block (peek-then-decide) + identity + the fixed bounded-snapshot
// note.
func (s *apiServer) HandleResumeSummaryApiResumeSummaryGet(w http.ResponseWriter, r *http.Request) {
	actor := currentActor(r)
	chat, tasks, overview, err := s.resumeSnapshotParts(actor)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resumeSummaryDTO{
		Identity: &actor,
		Chat:     chat,
		Tasks:    tasks,
		Overview: overview,
		Note:     resumeNote,
	})
}

// resumeSnapshotParts assembles the caller's wake snapshot in three parts:
// the recent chat (bodies truncated to resumeChatBodyPreview), the caller's
// open tasks as LIGHT rows, and the overview size/概要 block. resume_summary
// serves all three; peek_resume_summary_size serves only the overview (+
// identity) — both go THROUGH this one assembly so the sizes the peek reports
// can never drift from what a full resume_summary would carry (T-7974: the
// two-step boot lets an agent size the snapshot before deciding whether to
// pull it into its own context or hand it to a cheap sub-agent).
func (s *apiServer) resumeSnapshotParts(actor string) ([]chatMessageDTO, []resumeTaskDTO, resumeOverviewDTO, error) {
	msgs, err := s.dal.ListChatInvolving(actor, resumeChatN)
	if err != nil {
		return nil, nil, resumeOverviewDTO{}, err
	}
	chat := []chatMessageDTO{}
	chatChars := 0
	for _, m := range msgs {
		dto := s.servedChatMessageDTO(m)
		if len([]rune(dto.Body)) > resumeChatBodyPreview {
			dto.Body = string([]rune(dto.Body)[:resumeChatBodyPreview]) + "…"
		}
		// chat_chars sums the truncated body runes THIS snapshot carries (the
		// dominant payload contributor) — the peek's size signal for "how big
		// is resume_summary itself", distinct from tasks_detail_chars (the
		// text a later get_task pull would load).
		chatChars += len([]rune(dto.Body))
		chat = append(chat, dto)
	}
	tasks, tasksOpenTotal, err := s.resumeTasksFor(actor)
	if err != nil {
		return nil, nil, resumeOverviewDTO{}, err
	}
	// The caller's own reply-card counts (peek signals for list_reply_cards):
	// cards it INITIATED, waiting vs answered-within-24h (the same window the
	// answered pane serves).
	cardsWaiting, cardsAnsweredRecent := 0, 0
	if actor != "" {
		cards, err := s.dal.ListReplyCards()
		if err != nil {
			return nil, nil, resumeOverviewDTO{}, err
		}
		now := nowSecs()
		for _, c := range cards {
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
	detailChars := 0
	for _, t := range tasks {
		detailChars += t.DetailChars
	}
	overview := resumeOverviewDTO{
		ChatCount:           len(chat),
		ChatChars:           chatChars,
		TasksReturned:       len(tasks),
		TasksOpenTotal:      tasksOpenTotal,
		TasksDetailChars:    detailChars,
		CardsWaiting:        cardsWaiting,
		CardsAnsweredRecent: cardsAnsweredRecent,
	}
	return chat, tasks, overview, nil
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
	_, _, overview, err := s.resumeSnapshotParts(actor)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resumeSummarySizeDTO{
		Identity: &actor,
		Overview: overview,
		// estimated_total_chars ≈ the context cost of pulling resume_summary
		// AND then expanding every task via get_task: the chat bodies the
		// snapshot carries plus the plan text those rows omit. The single
		// number the boot threshold gates on (see the note / boot_sequence).
		EstimatedTotalChars: overview.ChatChars + overview.TasksDetailChars,
		Note:                peekNote,
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
