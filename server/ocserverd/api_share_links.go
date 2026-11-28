package main

// Share links are stored credentials: the URL carries a random token and the
// share_link row keeps only its hash.
//
// 🔴 SIGNING-KEY INDEPENDENCE: nothing here reads the key ring, so removing a
// signing key leaves every share link working. That is the proposed answer to
// an owner question still open; if the owner rules the other way, the change
// belongs in liveShareLinkFor, not at its callers.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	shareLinkDefaultTTLSecs = 7 * 24 * 60 * 60
	shareLinkMaxAheadSecs   = 365 * 24 * 60 * 60
	shareLinkParam          = "link"
)

const shareLinkExpiryWindowMsg = "expires_ts must be later than now and at most 365 days " +
	"(31536000 seconds) ahead, in epoch seconds; send null for a link that never expires"

func newShareLinkToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func shareLinkTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Omitted is 7 days from now and an explicit null is no expiry, so the caller
// has to say which keys were sent.
func resolveShareLinkExpiry(sent map[string]bool, expiresTS *float64, now float64) (*float64, bool) {
	if !sent["expires_ts"] {
		ts := now + shareLinkDefaultTTLSecs
		return &ts, true
	}
	if expiresTS == nil {
		return nil, true
	}
	if *expiresTS <= now || *expiresTS > now+shareLinkMaxAheadSecs {
		return nil, false
	}
	ts := *expiresTS
	return &ts, true
}

func newShareLinkDTO(l ShareLink) ShareLinkDTO {
	dto := ShareLinkDTO{
		Id:          l.ID,
		Kind:        ShareLinkDTOKind(l.Kind),
		CreatedBy:   l.CreatedBy,
		CreatedTs:   l.CreatedTS,
		ExpiresTs:   l.ExpiresTS,
		Before:      l.DiffBefore,
		After:       l.DiffAfter,
		LabelBefore: l.LabelBefore,
		LabelAfter:  l.LabelAfter,
	}
	if l.Kind == shareLinkKindAttachment {
		dto.AttachmentId = l.AttachmentID
		dto.AttachmentFilename = l.AttachmentFilename
		if dto.AttachmentFilename == nil {
			empty := ""
			dto.AttachmentFilename = &empty
		}
	}
	return dto
}

func (s *apiServer) mintShareLink(w http.ResponseWriter, r *http.Request, l ShareLink, urlFor func(token string) string) {
	token := newShareLinkToken()
	l.ID = "sl-" + newHexID(12)
	l.TokenHash = shareLinkTokenHash(token)
	l.CreatedBy = currentActor(r)
	if err := s.dal.InsertShareLink(l); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ShareLinkCreatedDTO{Url: urlFor(token), ShareLink: newShareLinkDTO(l)})
}

func (s *apiServer) HandleCreateChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkPost(w http.ResponseWriter, r *http.Request, attachmentId string) {
	var body ChatAttachmentShareLinkCreateDTO
	sent, ok := decodeJSONBodyPresent(w, r, &body)
	if !ok {
		return
	}
	now := nowSecs()
	expires, ok := resolveShareLinkExpiry(sent, body.ExpiresTs, now)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, shareLinkExpiryWindowMsg)
		return
	}
	att, err := s.dal.GetChatAttachment(attachmentId)
	if err != nil {
		internalError(w, err)
		return
	}
	if att == nil {
		writeError(w, http.StatusNotFound, "attachment '"+attachmentId+"' not found")
		return
	}
	id := att.ID
	s.mintShareLink(w, r, ShareLink{
		Kind: shareLinkKindAttachment, AttachmentID: &id, AttachmentFilename: att.Filename,
		CreatedTS: now, ExpiresTS: expires,
	}, func(token string) string {
		return "/api/chat/attachment/" + url.PathEscape(id) + "?" + shareLinkParam + "=" + token
	})
}

func (s *apiServer) HandleCreateDiffShareLinkApiDiffShareLinkPost(w http.ResponseWriter, r *http.Request) {
	var body DiffShareLinkCreateDTO
	sent, ok := decodeJSONBodyPresent(w, r, &body, "before", "after")
	if !ok {
		return
	}
	if !diffSidesSayable(w, body.Before, body.After) {
		return
	}
	now := nowSecs()
	expires, ok := resolveShareLinkExpiry(sent, body.ExpiresTs, now)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, shareLinkExpiryWindowMsg)
		return
	}
	before, after := body.Before, body.After
	labelBefore, labelAfter := optString(body.LabelBefore), optString(body.LabelAfter)
	s.mintShareLink(w, r, ShareLink{
		Kind: shareLinkKindDiff, DiffBefore: &before, DiffAfter: &after,
		LabelBefore: &labelBefore, LabelAfter: &labelAfter,
		CreatedTS: now, ExpiresTS: expires,
	}, func(token string) string {
		return diffPagePath + "?" + diffPageQuery(before, after, labelBefore, labelAfter, token)
	})
}

func (s *apiServer) HandleListShareLinksApiShareLinksGet(w http.ResponseWriter, r *http.Request, params HandleListShareLinksApiShareLinksGetParams) {
	f := shareLinkFilter{
		AttachmentID: optString(params.AttachmentId),
		Before:       optString(params.Before),
		After:        optString(params.After),
	}
	if (f.Before == "") != (f.After == "") {
		writeError(w, http.StatusUnprocessableEntity, "before and after filter one comparison together; give both or neither")
		return
	}
	if f.AttachmentID != "" && f.Before != "" {
		writeError(w, http.StatusUnprocessableEntity, "filter by attachment_id or by before + after, not both")
		return
	}
	rows, err := s.dal.ListLiveShareLinks(f, nowSecs())
	if err != nil {
		internalError(w, err)
		return
	}
	out := ShareLinksDTO{ShareLinks: make([]ShareLinkDTO, 0, len(rows))}
	for _, l := range rows {
		out.ShareLinks = append(out.ShareLinks, newShareLinkDTO(l))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleRevokeShareLinkApiShareLinksShareLinkIdDelete(w http.ResponseWriter, r *http.Request, shareLinkId string) {
	var revoked ShareLink
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getLiveShareLinkOn(tx, shareLinkId, nowSecs())
		if err != nil {
			return err
		}
		if cur == nil {
			return refuseInTx(http.StatusNotFound, "share link '"+shareLinkId+"' not found")
		}
		revoked = *cur
		return deleteShareLinkOn(tx, shareLinkId)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newShareLinkDTO(revoked))
}

func (s *apiServer) HandleGetChatAttachmentPreviewLinkApiChatAttachmentsAttachmentIdPreviewLinkGet(w http.ResponseWriter, r *http.Request, attachmentId string) {
	att, err := s.dal.GetChatAttachment(attachmentId)
	if err != nil {
		internalError(w, err)
		return
	}
	if att == nil {
		writeError(w, http.StatusNotFound, "attachment '"+attachmentId+"' not found")
		return
	}
	query := previewLinkQuery(s.keys, attachmentId, int64(nowSecs())+previewLinkTTLSecs)
	if query == "" {
		internalError(w, errNoSigningKey)
		return
	}
	writeJSON(w, http.StatusOK, ChatAttachmentPreviewLinkDTO{
		Url: "/api/chat/attachment/" + url.PathEscape(attachmentId) + "?" + query,
	})
}

func previewLinkQuery(kr *keyring, attachmentID string, exp int64) string {
	sig := previewSigForRing(kr, attachmentID, exp)
	if sig == "" {
		return ""
	}
	return "sig=" + sig + "&" + previewExpParam + "=" + strconv.FormatInt(exp, 10)
}

func (s *apiServer) pruneExpiredShareLinks(now float64) {
	if _, err := s.dal.PruneExpiredShareLinks(now); err != nil {
		log.Printf("[share-link] prune failed (the next tick retries): %v", err)
	}
}

// The caller still has to check that the link names the target requested.
func (s *apiServer) liveShareLinkFor(token string) (*ShareLink, error) {
	return s.dal.GetLiveShareLinkByTokenHash(shareLinkTokenHash(token), nowSecs())
}

const (
	shareLinkGoneMsg     = "this share link is no longer valid: it has expired, been revoked, or never existed"
	shareLinkGoneTitle   = "This share link is no longer valid"
	shareLinkGoneBody    = "It has expired or been revoked. Ask whoever sent it for a new link."
	shareLinkGoneTitleZh = "這個分享連結已失效"
	shareLinkGoneBodyZh  = "連結已過期或已被收回。請向傳給你的人索取新的連結。"
)

// A browser opening the link gets a page; any other client the envelope.
func (s *apiServer) writeShareLinkGone(w http.ResponseWriter, r *http.Request) {
	if !prefersHTML(r) {
		writeError(w, http.StatusGone, shareLinkGoneMsg)
		return
	}
	title, body, lang := shareLinkGoneTitle, shareLinkGoneBody, "en"
	if s.displayLanguageSnapshot() == "zh" {
		title, body, lang = shareLinkGoneTitleZh, shareLinkGoneBodyZh, "zh-Hant"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusGone)
	_, _ = w.Write([]byte(`<!doctype html><html lang="` + lang + `"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>` + html.EscapeString(title) + `</title>` +
		`<style>body{font-family:system-ui,sans-serif;margin:0;min-height:100vh;display:flex;` +
		`align-items:center;justify-content:center;background:#f6f6f4;color:#222}` +
		`main{max-width:28rem;padding:2rem 1rem;text-align:center}h1{font-size:1.25rem}` +
		`p{color:#555;line-height:1.5}@media (prefers-color-scheme:dark){body{background:#1c1c1c;color:#eee}p{color:#aaa}}</style>` +
		`</head><body><main><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(body) +
		`</p></main></body></html>`))
}

func prefersHTML(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		mediaType, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		switch strings.ToLower(strings.TrimSpace(mediaType)) {
		case "text/html", "application/xhtml+xml":
			return true
		case "application/json":
			return false
		}
	}
	return false
}
