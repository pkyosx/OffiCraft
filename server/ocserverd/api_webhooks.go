package main

// Webhooks: owner-facing CRUD plus the PUBLIC /in inlet. Delivery reuses the
// chat channel: an accepted /in POST synthesises ONE ordinary chat_message from
// `hook:<endpoint_id>` and fans the same "chat" delta, so it inherits chat's
// offline queue, SSE push and on-wake catch-up, and never auto-wakes the member
// (SPEC §2). meta.webhook.purpose is what the member's 用途守衛 (seed §3)
// judges the untrusted payload against.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// A public unauthenticated inlet must not be an amplification / memory sink.
// 🔴 Over-cap is a REFUSAL, not a truncation (owner rc-a40ef8c66781): a cut body
// also fed truncated bytes to the HMAC gates, so legitimate signed calls were
// classified sig_failed.
const webhookPayloadMaxBytes = 1 << 20

const (
	webhookLogHeadersMaxBytes = 4 << 10
	webhookLogBodyMaxBytes    = 16 << 10
)

const webhookInternalErrorMessage = "internal server error"

func writeWebhookInternalError(w http.ResponseWriter, err error) {
	log.Printf("[webhook] internal error: %v", err)
	writeError(w, http.StatusInternalServerError, webhookInternalErrorMessage)
}

const (
	webhookOutcomeDelivered     = "delivered"
	webhookOutcomeChallenge     = "challenge"
	webhookOutcomePing          = "ping"
	webhookOutcomeDroppedPrefix = "dropped:"
)

// logWebhookRequest is STRICTLY best-effort: the inlet's byte-identical silent
// face must never be perturbed by observability, so every error is swallowed.
func logWebhookRequestOn(ex sqlExecer, token string, r *http.Request, payload []byte, outcome string, ts float64) {
	truncated := false
	headers, err := json.Marshal(r.Header)
	if err != nil {
		headers = []byte("{}")
	}
	if len(headers) > webhookLogHeadersMaxBytes {
		headers = headers[:webhookLogHeadersMaxBytes]
		truncated = true
	}
	body := payload
	if len(body) > webhookLogBodyMaxBytes {
		body = body[:webhookLogBodyMaxBytes]
		truncated = true
	}
	_ = insertWebhookRequestLogOn(ex, token, WebhookRequestLog{
		TS:        ts,
		Outcome:   outcome,
		Headers:   string(headers),
		Body:      string(body),
		Truncated: truncated,
	})
}

func (s *apiServer) recordWebhookOversizeRejection(token string, r *http.Request, payload []byte) {
	if e, err := s.dal.GetWebhookByToken(token); err != nil || e == nil {
		return
	}
	ts := nowSecs()
	_ = s.dal.inTx(func(tx *writeTx) error {
		e, err := getWebhookByTokenOn(tx, token)
		if err != nil || e == nil {
			return err
		}
		_ = markWebhookDroppedOn(tx, e.Token, WebhookDropReasonOversize, ts)
		logWebhookRequestOn(tx, e.Token, r, payload,
			webhookOutcomeDroppedPrefix+WebhookDropReasonOversize, ts)
		return nil
	})
}

func newWebhookToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (s *apiServer) HandleListWebhooksApiMembersMemberIdWebhooksGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	rows, err := s.dal.ListWebhooksByMember(m.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	out := []webhookEndpointDTO{}
	for _, e := range rows {
		out = append(out, newWebhookEndpointDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleCreateWebhookApiMembersMemberIdWebhooksPost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body WebhookCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "endpoint_id") {
		return
	}
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	endpointID := trimString(body.EndpointId)
	if err := ValidateWebhookEndpointID(endpointID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	platform := WebhookPlatformGeneric
	if body.Platform != nil && string(*body.Platform) != "" {
		platform = string(*body.Platform)
	}
	if !ValidWebhookPlatform(platform) {
		writeError(w, http.StatusUnprocessableEntity,
			"platform must be one of ['generic' 'slack' 'github']; got '"+platform+"'")
		return
	}
	signingSecret := strOrEmpty(body.SigningSecret)
	if platform != WebhookPlatformGeneric && signingSecret == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"signing_secret is required when platform is '"+platform+"'")
		return
	}
	taken := func(q sqlRowQuerier) error {
		existing, err := getWebhookByMemberEndpointOn(q, m.ID, endpointID)
		if err == nil && existing != nil {
			err = refuseInTx(http.StatusConflict,
				"a webhook endpoint '"+endpointID+"' already exists for this member")
		}
		return err
	}
	if err := taken(s.dal.rdb); err != nil {
		writeTxError(w, err)
		return
	}
	e := WebhookEndpoint{
		Token:         newWebhookToken(),
		MemberID:      m.ID,
		EndpointID:    endpointID,
		Purpose:       strOrEmpty(body.Purpose),
		Status:        WebhookStatusEnabled,
		CreatedTS:     nowSecs(),
		Platform:      platform,
		SigningSecret: signingSecret,
	}
	err = s.dal.inTx(func(tx *writeTx) error {
		if _, err := resolveMemberOn(tx, memberId, anyMember); err != nil {
			return err
		}
		if err := taken(tx); err != nil {
			return err
		}
		return s.dal.PutWebhookEndpoint(e)
	})
	if err != nil {
		writeResolveTxError(w, err, "member", memberId)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(e))
}

func (s *apiServer) HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	var body WebhookUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if _, err := s.resolveWebhook(memberId, endpointId, anyMember); err != nil {
		writeResolveError(w, err, "webhook endpoint", endpointId)
		return
	}
	if body.Status != nil && !ValidWebhookStatus(*body.Status) {
		writeError(w, http.StatusUnprocessableEntity,
			"status must be one of ['enabled' 'disabled']; got '"+*body.Status+"'")
		return
	}
	// The patch lands on the row as the transaction reads it: the inlet moves the
	// counters of an endpoint in use, and a whole-row write from an earlier copy
	// would put them back.
	var e *WebhookEndpoint
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if e, err = resolveWebhookOn(tx, memberId, endpointId, anyMember); err != nil {
			return err
		}
		if body.Status != nil {
			e.Status = *body.Status
		}
		if body.Purpose != nil {
			e.Purpose = *body.Purpose
		}
		if body.SigningSecret != nil {
			e.SigningSecret = *body.SigningSecret
		}
		return s.dal.PutWebhookEndpoint(*e)
	})
	if err != nil {
		writeResolveTxError(w, err, "webhook endpoint", endpointId)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(*e))
}

func (s *apiServer) HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	if _, err := s.resolveWebhook(memberId, endpointId, anyMember); err != nil {
		writeResolveError(w, err, "webhook endpoint", endpointId)
		return
	}
	var gone WebhookEndpoint
	err := s.dal.inTx(func(tx *writeTx) error {
		e, err := resolveWebhookOn(tx, memberId, endpointId, anyMember)
		if err != nil {
			return err
		}
		gone = *e
		return deleteWebhookEndpointOn(tx, e.Token)
	})
	if err != nil {
		writeResolveTxError(w, err, "webhook endpoint", endpointId)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(gone))
}

// 🔴 The member lookup scope is a PARAMETER: this serves PATCH, DELETE and the
// delivery-log read, and a hard-wired scope here would decide the contractor
// question for all three out of sight of each.
func (s *apiServer) resolveWebhook(memberID, endpointID string, scope memberScope) (*WebhookEndpoint, error) {
	return resolveWebhookOn(s.dal.rdb, memberID, endpointID, scope)
}

func resolveWebhookOn(q sqlRowQuerier, memberID, endpointID string, scope memberScope) (*WebhookEndpoint, error) {
	m, err := resolveMemberOn(q, memberID, scope)
	if err != nil {
		return nil, err
	}
	e, err := getWebhookByMemberEndpointOn(q, m.ID, endpointID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errNotFound
	}
	return e, nil
}

// POST /in — the PUBLIC inlet. Identity comes SOLELY from ?t=; an unknown or
// disabled token, an absent member and a missing token all answer the SAME
// silent 200 — never reveal whether an endpoint exists. A repeated ?t= is
// refused 422 by the generated parameter binding before this handler runs.
func (s *apiServer) HandleReceiveWebhookInPost(w http.ResponseWriter, r *http.Request, params HandleReceiveWebhookInPostParams) {
	// Read the body regardless of token validity (no timing difference); cap+1
	// proves over-cap without buffering an unbounded body.
	payload, err := io.ReadAll(io.LimitReader(r.Body, webhookPayloadMaxBytes+1))
	// 🔴 A CUT BODY IS NOT A SHORT BODY: gates below would treat it as the whole
	// request and answer "ok", so the sender never resends. Refuse it before the
	// size verdict too.
	if err != nil {
		writeWebhookInternalError(w, err)
		return
	}

	token := ""
	if params.T != nil {
		token = *params.T
	}
	// 🔴 The size verdict precedes any identity or signature work, and the 413 is
	// byte-identical whatever the token resolves to (asserted by test); the
	// recording call only finds somewhere to write. It must also precede the
	// platform gates: an HMAC over a cut body is a lie about the sender.
	if len(payload) > webhookPayloadMaxBytes {
		s.recordWebhookOversizeRejection(token, r, payload)
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("webhook payload is too large (max %d bytes)", webhookPayloadMaxBytes))
		return
	}
	pre, err := s.dal.GetWebhookByToken(token)
	if err != nil {
		writeWebhookInternalError(w, err)
		return
	}
	if pre == nil {
		s.writeWebhookAccepted(w)
		return
	}
	// The signature is verified before the transaction opens, so the write
	// connection is never held across an HMAC over the payload.
	sigOK := webhookSignatureOK(pre, r, payload)
	// The endpoint and its recipient are judged again inside the transaction
	// that records the outcome: disabled, deleted or re-pointed in between, the
	// request is answered as it stands there.
	receivedTS := nowSecs()
	var answer webhookInletAnswer
	err = s.dal.inTx(func(tx *writeTx) error {
		e, err := getWebhookByTokenOn(tx, token)
		if err != nil {
			return err
		}
		verified := sigOK
		if e != nil && (e.Platform != pre.Platform || e.SigningSecret != pre.SigningSecret) {
			verified = webhookSignatureOK(e, r, payload)
		}
		answer, err = receiveWebhookOn(tx, e, r, payload, verified, receivedTS)
		return err
	})
	if err != nil {
		// A request the endpoint as first read would not deliver keeps the silent
		// face when its bookkeeping could not land; a failed delivery is a 500.
		fallback, ferr := s.webhookAnswerWithoutWrites(pre, r, payload, sigOK)
		if ferr != nil || !fallback.decided {
			writeWebhookInternalError(w, err)
			return
		}
		answer = fallback
	}
	if msg := answer.delivered; msg != nil {
		// The payload every chat delta carries (spec/sse.md §2.2).
		s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
			map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
			audienceMembers(msg.Sender, msg.Recipient), triggerServer)
	}
	if answer.challenge != "" {
		writeJSON(w, http.StatusOK, map[string]any{"challenge": answer.challenge})
		return
	}
	s.writeWebhookAccepted(w)
}

type webhookInletAnswer struct {
	// decided: answered without a delivery (drop, ping, challenge).
	decided bool
	// challenge is Slack's url_verification echo; "" answers the silent ok.
	challenge string
	delivered *ChatMessage
}

// webhookGate is what the endpoint's own gates say about one request, before
// the recipient is looked up: a drop reason, a handshake, a ping, or on to
// delivery (all empty).
type webhookGate struct {
	drop, challenge string
	ping            bool
}

// Verification runs on the SAME raw bytes the inlet read, never after a JSON
// decode. Only Slack's url_verification handshake answers a distinct body —
// Slack needs the challenge echoed to activate the subscription — and it is
// the one request a Slack endpoint takes unsigned.
func webhookSignatureOK(e *WebhookEndpoint, r *http.Request, payload []byte) bool {
	switch e.Platform {
	case WebhookPlatformSlack:
		return verifySlackSignature(e.SigningSecret,
			r.Header.Get("X-Slack-Signature"),
			r.Header.Get("X-Slack-Request-Timestamp"),
			payload, time.Now().Unix())
	case WebhookPlatformGithub:
		return verifyGithubSignature(e.SigningSecret, r.Header.Get("X-Hub-Signature-256"), payload)
	}
	return true
}

func judgeWebhookGate(e *WebhookEndpoint, r *http.Request, payload []byte, verified bool) webhookGate {
	if e.Status != WebhookStatusEnabled {
		return webhookGate{drop: WebhookDropReasonDisabled}
	}
	switch e.Platform {
	case WebhookPlatformSlack:
		if challenge, ok := slackURLVerificationChallenge(payload); ok {
			return webhookGate{challenge: challenge}
		}
		if !verified {
			return webhookGate{drop: WebhookDropReasonSigFailed}
		}
	case WebhookPlatformGithub:
		if !verified {
			return webhookGate{drop: WebhookDropReasonSigFailed}
		}
		if r.Header.Get("X-GitHub-Event") == "ping" {
			return webhookGate{ping: true}
		}
	}
	// 🔴 THE INLET'S DOOR IS THE CHAT DOOR: resolveChatRecipientOn decides who may
	// receive (ACTIVE, staff or outsource), so no separate rule belongs here —
	// 「請不要再製造分岔」(owner). wireOwnerID is refused first: it is a legal chat
	// address but never a member row, so it can only come from corrupt data on
	// the one UNAUTHENTICATED surface.
	if e.MemberID == wireOwnerID {
		return webhookGate{drop: WebhookDropReasonMemberGone}
	}
	return webhookGate{}
}

// webhookRecipientOn answers the endpoint's chat recipient, or gone.
func webhookRecipientOn(q sqlRowQuerier, e *WebhookEndpoint) (recipient string, gone bool, err error) {
	recipient, err = resolveChatRecipientOn(q, e.MemberID)
	if errors.Is(err, errNotFound) {
		return "", true, nil
	}
	return recipient, false, err
}

// webhookAnswerWithoutWrites is the answer the endpoint as first read gives,
// with nothing recorded.
func (s *apiServer) webhookAnswerWithoutWrites(e *WebhookEndpoint, r *http.Request, payload []byte, verified bool) (webhookInletAnswer, error) {
	gate := judgeWebhookGate(e, r, payload, verified)
	if gate.drop != "" || gate.challenge != "" || gate.ping {
		return webhookInletAnswer{decided: true, challenge: gate.challenge}, nil
	}
	_, gone, err := webhookRecipientOn(s.dal.rdb, e)
	return webhookInletAnswer{decided: gone}, err
}

// receiveWebhookOn decides and records one inlet request on the caller's
// transaction. The counters and the request log stay best-effort (their
// errors are dropped; a failed statement does not end the transaction); only
// the delivered chat row is fatal.
func receiveWebhookOn(tx *writeTx, e *WebhookEndpoint, r *http.Request, payload []byte, verified bool, receivedTS float64) (webhookInletAnswer, error) {
	if e == nil {
		return webhookInletAnswer{decided: true}, nil
	}
	drop := func(reason string) (webhookInletAnswer, error) {
		_ = markWebhookDroppedOn(tx, e.Token, reason, receivedTS)
		logWebhookRequestOn(tx, e.Token, r, payload, webhookOutcomeDroppedPrefix+reason, receivedTS)
		return webhookInletAnswer{decided: true}, nil
	}
	gate := judgeWebhookGate(e, r, payload, verified)
	switch {
	case gate.drop != "":
		return drop(gate.drop)
	case gate.challenge != "":
		_ = touchWebhookReceivedOn(tx, e.Token, receivedTS)
		logWebhookRequestOn(tx, e.Token, r, payload, webhookOutcomeChallenge, receivedTS)
		return webhookInletAnswer{decided: true, challenge: gate.challenge}, nil
	case gate.ping:
		_ = touchWebhookReceivedOn(tx, e.Token, receivedTS)
		logWebhookRequestOn(tx, e.Token, r, payload, webhookOutcomePing, receivedTS)
		return webhookInletAnswer{decided: true}, nil
	}
	recipientID, gone, err := webhookRecipientOn(tx, e)
	if err != nil {
		return webhookInletAnswer{}, err
	}
	if gone {
		return drop(WebhookDropReasonMemberGone)
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    "hook:" + e.EndpointID,
		Recipient: recipientID,
		Body:      string(payload),
		TS:        nowSecs(),
		Meta: map[string]any{
			"webhook": map[string]any{
				"endpoint_id": e.EndpointID,
				"purpose":     e.Purpose,
			},
		},
	}
	if err := putChatOn(tx, msg); err != nil {
		return webhookInletAnswer{}, err
	}
	_ = markWebhookDeliveredOn(tx, e.Token, receivedTS)
	logWebhookRequestOn(tx, e.Token, r, payload, webhookOutcomeDelivered, receivedTS)
	return webhookInletAnswer{delivered: &msg}, nil
}

// requires=admin_agent: raw UNVERIFIED external payloads never reach a plain
// agent; kept off WebhookEndpointDTO so the list wire stays light.
func (s *apiServer) HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	e, err := s.resolveWebhook(memberId, endpointId, anyMember)
	if err != nil {
		writeResolveError(w, err, "webhook endpoint", endpointId)
		return
	}
	rows, err := s.dal.ListWebhookRequestLogs(e.Token)
	if err != nil {
		internalError(w, err)
		return
	}
	out := []webhookRequestLogDTO{}
	for _, l := range rows {
		out = append(out, newWebhookRequestLogDTO(l))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) writeWebhookAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
