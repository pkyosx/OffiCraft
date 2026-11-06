package main

// 🔴 A secret card's value travels in exactly two places: the owner's provide
// request body and the author's reveal response body. Every refusal here names
// fields and limits only; the deltas carry the same id/from/status hint as any
// other card.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	secretCardSweepCadence = time.Minute

	// A value at its rune limit, with every rune sent as a \uXXXX surrogate pair,
	// still fits; this only stops an unbounded read.
	replyCardSecretBodyBytes = 1 << 20

	secretCardSweepTrigger = "secret-card-sweep"
)

func replyCardSecretDTOOf(c ReplyCard) *replyCardSecretDTO {
	if c.Kind != replyCardKindSecret {
		return nil
	}
	return &replyCardSecretDTO{
		State:       secretCardState(c),
		ProvidedTS:  positiveTS(c.SecretProvidedTS),
		RetrievedTS: positiveTS(c.SecretRetrievedTS),
		ExpiresTS:   positiveTS(c.SecretDestroyAfterTS),
	}
}

func secretCardState(c ReplyCard) string {
	switch {
	case c.Status == replyCardStatusWaiting:
		return string(ReplyCardSecretDTOStateWaiting)
	case c.Status == replyCardStatusExpired:
		return string(ReplyCardSecretDTOStateExpired)
	case c.SecretRetrievedTS > 0:
		return string(ReplyCardSecretDTOStateRetrieved)
	case c.SecretProvidedTS > 0:
		return string(ReplyCardSecretDTOStateProvided)
	default:
		return string(ReplyCardSecretDTOStateReplied)
	}
}

func positiveTS(ts float64) *float64 {
	if ts <= 0 {
		return nil
	}
	return &ts
}

// A retrieved card's destroy_after is 0 exactly when its ciphertext is gone.
func secretCardValueIsLive(c ReplyCard) bool {
	if c.Kind != replyCardKindSecret || c.Status != replyCardStatusAnswered || c.SecretProvidedTS <= 0 {
		return false
	}
	return c.SecretRetrievedTS <= 0 || c.SecretDestroyAfterTS > 0
}

func provideSecretGate(card ReplyCard, getTask func(string) (*Task, error)) error {
	if card.Kind != replyCardKindSecret {
		return refuseInTx(http.StatusConflict, "reply card '"+card.ID+"' is not a secret card")
	}
	if card.Status != replyCardStatusWaiting {
		return refuseInTx(http.StatusConflict,
			"reply card '"+card.ID+"' is "+card.Status+" — only a waiting secret card takes a value")
	}
	return firstAnswerGate(card, getTask)
}

func (s *apiServer) HandleProvideReplyCardSecretApiReplyCardsCardIdSecretPost(w http.ResponseWriter, r *http.Request, cardId string) {
	card, err := s.dal.GetReplyCard(cardId)
	if err != nil {
		internalError(w, err)
		return
	}
	if card == nil {
		writeError(w, http.StatusNotFound, "reply card '"+cardId+"' not found")
		return
	}
	if err := provideSecretGate(*card, s.dal.GetTask); err != nil {
		writeTxError(w, err)
		return
	}
	value, problem, ok := decodeProvidedSecret(w, r)
	if !ok {
		return
	}
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	aead, err := s.sealingCipher()
	if err != nil {
		writeVaultError(w, err)
		return
	}
	nonce, ct, err := sealSecretValue(aead, card.ID, value)
	if err != nil {
		internalError(w, err)
		return
	}
	ttl := s.secretCardUnretrievedTTL()
	now := nowSecs()
	settled, rel, err := s.settleReplyCardOn(card.ID, now, nil, func(tx *writeTx, cur *ReplyCard, getTask func(string) (*Task, error)) error {
		if err := provideSecretGate(*cur, getTask); err != nil {
			return err
		}
		cur.Status = replyCardStatusAnswered
		cur.AnsweredTS = now
		cur.SecretProvidedTS = now
		cur.SecretDestroyAfterTS = now + float64(ttl)
		return insertReplyCardSecretOn(tx, ReplyCardSecret{
			CardID: cur.ID, Nonce: nonce, ValueCT: ct, KeyVersion: vaultKeyVersion,
		})
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishReplyCard(settled, requestTrigger(r))
	s.announceCardHoldRelease(rel, requestTrigger(r))
	s.writeReplyCardTransitionReceipt(w, settled)
}

// The shared body decoders quote the JSON error, and a bad escape inside the
// value makes that error quote a character of the value. So nothing the decoder
// says about this body reaches the wire.
func decodeProvidedSecret(w http.ResponseWriter, r *http.Request) (value, problem string, ok bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, replyCardSecretBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusBadRequest, "value is over the "+strconv.Itoa(secretValueMaxChars)+"-char limit")
			return "", "", false
		}
		writeError(w, http.StatusUnprocessableEntity, "could not read request body")
		return "", "", false
	}
	var body ReplyCardSecretProvideDTO
	if _, err := decodeJSONStrict(raw, &body, "value"); err != nil {
		var missing missingJSONFieldError
		if errors.As(err, &missing) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return "", "", false
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid request body: expected a JSON object with one string field, value")
		return "", "", false
	}
	return body.Value, validateSecretValue(body.Value), true
}

// revealTarget is one id of a reveal, resolved: a stored secret, or a secret
// card with its sealed row.
type revealTarget struct {
	secret *StoredSecret
	card   *ReplyCard
	sealed *ReplyCardSecret
}

type revealRefusals struct {
	unknown, notAuthor, unavailable []string
}

func (f *revealRefusals) any() bool {
	return len(f.unknown)+len(f.notAuthor)+len(f.unavailable) > 0
}

func (f *revealRefusals) write(w http.ResponseWriter) {
	switch {
	case len(f.unknown) > 0:
		writeError(w, http.StatusNotFound, "unknown secret id(s): "+strings.Join(f.unknown, ", "))
	case len(f.notAuthor) > 0:
		writeError(w, http.StatusForbidden, "only a secret card's author may retrieve its value: "+
			strings.Join(f.notAuthor, ", "))
	default:
		writeError(w, http.StatusConflict, "no retrievable value on secret card(s): "+
			strings.Join(f.unavailable, ", ")+" — not provided, already destroyed, past its deadline, or its task is closed")
	}
}

func resolveRevealTargetOn(tx *writeTx, id, actor string, now float64, refused *revealRefusals) (*revealTarget, error) {
	if !secretCardIDShape.MatchString(id) {
		sec, err := getSecretOn(tx, id)
		if err != nil {
			return nil, err
		}
		if sec == nil {
			refused.unknown = append(refused.unknown, id)
			return nil, nil
		}
		return &revealTarget{secret: sec}, nil
	}
	card, err := getReplyCardOn(tx, id)
	if err != nil {
		return nil, err
	}
	if card == nil || card.Kind != replyCardKindSecret {
		refused.unknown = append(refused.unknown, id)
		return nil, nil
	}
	if card.FromMember != actor {
		refused.notAuthor = append(refused.notAuthor, id)
		return nil, nil
	}
	sealed, err := getReplyCardSecretOn(tx, id)
	if err != nil {
		return nil, err
	}
	closed, err := secretCardTaskClosedOn(tx, *card)
	if err != nil {
		return nil, err
	}
	pastDeadline := card.SecretDestroyAfterTS > 0 && now >= card.SecretDestroyAfterTS
	if sealed == nil || closed || pastDeadline {
		refused.unavailable = append(refused.unavailable, id)
		return nil, nil
	}
	return &revealTarget{card: card, sealed: sealed}, nil
}

func secretCardTaskClosedOn(q sqlRowQuerier, card ReplyCard) (bool, error) {
	if card.TaskID == "" {
		return false, nil
	}
	t, err := getTaskOn(q, card.TaskID)
	if err != nil {
		return false, err
	}
	return t == nil || TaskIsTerminal(t.Status), nil
}

// Only the first retrieval moves the deadline; a later one inside the window
// must not extend it.
func stampSecretCardRetrievedOn(tx *writeTx, card *ReplyCard, now float64, retention int) error {
	card.SecretRetrievedTS = now
	card.SecretDestroyAfterTS = now + float64(retention)
	if retention == 0 {
		card.SecretDestroyAfterTS = 0
		if err := deleteReplyCardSecretOn(tx, card.ID); err != nil {
			return err
		}
	}
	return putReplyCardOn(tx, *card)
}

// destroySecretCardValueOn ends a live value. Unretrieved, the card closes as
// expired; retrieved, it stays answered, since the author already has the value.
func destroySecretCardValueOn(tx *writeTx, cur *ReplyCard, now float64) error {
	if err := deleteReplyCardSecretOn(tx, cur.ID); err != nil {
		return err
	}
	cur.SecretDestroyAfterTS = 0
	if cur.SecretRetrievedTS <= 0 {
		cur.Status = replyCardStatusExpired
		cur.ExpiredTS = now
	}
	return nil
}

// sweepSecretCards is the one destroyer behind the tick, the boot pass and the
// calls right after a task closes or a member leaves. A failed lookup leaves the
// value for the next pass: it is not evidence that the author or the task is gone.
func (s *apiServer) sweepSecretCards(now float64, trigger string) (int, error) {
	cards, err := s.dal.ListReplyCardsHoldingSecret()
	if err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for _, c := range cards {
		settled, _, err := s.settleReplyCardOn(c.ID, now, nil, func(tx *writeTx, cur *ReplyCard, _ func(string) (*Task, error)) error {
			due, err := secretCardDestroyDueOn(tx, *cur, now)
			if err != nil {
				return err
			}
			if !due {
				return errReplyCardSettledMeanwhile
			}
			return destroySecretCardValueOn(tx, cur, now)
		})
		if errors.Is(err, errReplyCardSettledMeanwhile) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("card %s: %w", c.ID, err)
			}
			continue
		}
		if settled.Status == replyCardStatusExpired {
			s.publishReplyCard(settled, trigger)
		} else {
			s.publishReplyCardToOwnerOnly(settled, trigger)
		}
		n++
	}
	return n, firstErr
}

func secretCardDestroyDueOn(tx *writeTx, card ReplyCard, now float64) (bool, error) {
	if card.SecretDestroyAfterTS > 0 && now >= card.SecretDestroyAfterTS {
		return true, nil
	}
	closed, err := secretCardTaskClosedOn(tx, card)
	if err != nil || closed {
		return closed, err
	}
	author, err := membersOn(tx)(card.FromMember)
	if err != nil {
		return false, err
	}
	return author == nil || author.RosterStatus != RosterStatusActive, nil
}

func (s *apiServer) sweepSecretCardsBestEffort(trigger string) {
	if _, err := s.sweepSecretCards(nowSecs(), trigger); err != nil {
		taskLog("secret-card sweep failed (the next tick retries): %v", err)
	}
}

func (s *apiServer) startSecretCardSweepCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			surviveLockInTx("secret-card sweep cadence", func() { s.runSecretCardSweepTick(nowSecs()) })
		}
	}()
}

func (s *apiServer) runSecretCardSweepTick(now float64) {
	if _, err := s.sweepSecretCards(now, secretCardSweepTrigger); err != nil {
		taskLog("secret-card sweep failed (the next tick retries): %v", err)
	}
}
