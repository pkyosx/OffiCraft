package main

// 🔴 A secret value travels in exactly two places: the owner's create/update
// request body and the AI's reveal response body. It must never reach a log
// line, an error message, an SSE frame, a chat row or telemetry — which is why
// these handlers publish no events and every refusal names only fields and
// limits.

import (
	"bytes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	secretValueMaxChars   = 65536
	secretRevealMaxIDs    = 16
	secretRevealBodyBytes = 64 << 10
)

const secretOwnerRevealMsg = "secrets are revealed only to AI members through `ocagent secret`; " +
	"the owner manages them at /api/secrets"

func validateSecretName(raw string) (string, string) {
	name := trimString(raw)
	if name == "" {
		return "", "name must not be blank"
	}
	if n := utf8.RuneCountInString(name); n > shortLabelMaxChars {
		return "", "name is " + strconv.Itoa(n) + " chars, over the " +
			strconv.Itoa(shortLabelMaxChars) + "-char limit"
	}
	return name, ""
}

func validateSecretNote(note string) string {
	if n := utf8.RuneCountInString(note); n > chatBodyMaxChars {
		return "note is " + strconv.Itoa(n) + " chars, over the " +
			strconv.Itoa(chatBodyMaxChars) + "-char limit"
	}
	return ""
}

// The refusal must not echo any part of the value, not even its length.
func validateSecretValue(value string) string {
	if value == "" {
		return "value must not be empty"
	}
	if utf8.RuneCountInString(value) > secretValueMaxChars {
		return "value is over the " + strconv.Itoa(secretValueMaxChars) + "-char limit"
	}
	return ""
}

func writeVaultError(w http.ResponseWriter, err error) {
	if errors.Is(err, errVaultUnconfigured) || errors.Is(err, errVaultKeyMissing) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	internalError(w, err)
}

// sealingCipher may mint the key only while no row exists to be orphaned.
func (s *apiServer) sealingCipher() (cipher.AEAD, error) {
	return s.vault.cipherFor(func() (bool, error) {
		n, err := s.dal.CountSecrets()
		return n == 0, err
	})
}

func (s *apiServer) HandleListSecretsApiSecretsGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dal.ListSecrets()
	if err != nil {
		internalError(w, err)
		return
	}
	out := secretsDTO{Secrets: []secretDTO{}}
	for _, sec := range rows {
		out.Secrets = append(out.Secrets, newSecretDTO(sec))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleCreateSecretApiSecretsPost(w http.ResponseWriter, r *http.Request) {
	var body SecretCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "name", "value") {
		return
	}
	name, problem := validateSecretName(body.Name)
	if problem == "" {
		problem = validateSecretValue(strOrEmpty(body.Value))
	}
	if problem == "" {
		problem = validateSecretNote(strOrEmpty(body.Note))
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
	id := "sec-" + newHexID(12)
	nonce, ct, err := sealSecretValue(aead, id, *body.Value)
	if err != nil {
		internalError(w, err)
		return
	}
	now := nowSecs()
	sec := Secret{
		ID: id, Name: name, Note: strOrEmpty(body.Note),
		Nonce: nonce, ValueCT: ct, KeyVersion: vaultKeyVersion,
		CreatedTS: now, UpdatedTS: now,
	}
	if err := s.dal.inTx(func(tx *writeTx) error { return insertSecretOn(tx, sec) }); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newSecretDTO(sec))
}

func (s *apiServer) HandleUpdateSecretApiSecretsSecretIdPatch(w http.ResponseWriter, r *http.Request, secretId string) {
	var body SecretUpdateDTO
	if !decodeJSONBodyStrict(w, r, &body) {
		return
	}
	var name string
	if body.Name != nil {
		var problem string
		if name, problem = validateSecretName(*body.Name); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
	}
	if body.Note != nil {
		if problem := validateSecretNote(*body.Note); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
	}
	var aead cipher.AEAD
	if body.Value != nil {
		if problem := validateSecretValue(*body.Value); problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		var err error
		if aead, err = s.sealingCipher(); err != nil {
			writeVaultError(w, err)
			return
		}
	}
	var updated Secret
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getSecretOn(tx, secretId)
		if err != nil {
			return err
		}
		if cur == nil {
			return refuseInTx(http.StatusNotFound, "secret '"+secretId+"' not found")
		}
		if body.Name != nil {
			cur.Name = name
		}
		if body.Note != nil {
			cur.Note = *body.Note
		}
		if body.Value != nil {
			nonce, ct, err := sealSecretValue(aead, cur.ID, *body.Value)
			if err != nil {
				return err
			}
			cur.Nonce, cur.ValueCT, cur.KeyVersion = nonce, ct, vaultKeyVersion
		}
		cur.UpdatedTS = nowSecs()
		updated = *cur
		return updateSecretOn(tx, *cur)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newSecretDTO(updated))
}

func (s *apiServer) HandleDeleteSecretApiSecretsSecretIdDelete(w http.ResponseWriter, r *http.Request, secretId string) {
	var deleted Secret
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getSecretOn(tx, secretId)
		if err != nil {
			return err
		}
		if cur == nil {
			return refuseInTx(http.StatusNotFound, "secret '"+secretId+"' not found")
		}
		deleted = *cur
		return deleteSecretOn(tx, secretId)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newSecretDTO(deleted))
}

func (s *apiServer) HandleListSecretAccessLogApiSecretsAccessLogGet(w http.ResponseWriter, r *http.Request, params HandleListSecretAccessLogApiSecretsAccessLogGetParams) {
	f := SecretAccessLogFilter{
		SecretID: strOrEmpty(params.SecretId),
		MemberID: strOrEmpty(params.MemberId),
		Limit:    intOr(params.Limit, 0),
	}
	if params.Action != nil {
		f.Action = string(*params.Action)
		if f.Action != secretActionReveal && f.Action != secretActionDenied {
			writeError(w, http.StatusUnprocessableEntity, "action must be 'reveal' or 'denied'")
			return
		}
	}
	rows, err := s.dal.ListSecretAccessLog(f)
	if err != nil {
		internalError(w, err)
		return
	}
	out := secretAccessLogDTO{Entries: []secretAccessLogEntryDTO{}}
	for _, e := range rows {
		out.Entries = append(out.Entries, newSecretAccessLogEntryDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// The route floor is agent, which the owner clears; the owner is refused here
// because a value revealed to the cockpit has no audit trail of which AI used it.
func (s *apiServer) callerIsOwner(r *http.Request) bool {
	return s.principalOfRequest(r) == principalOwner
}

func (s *apiServer) HandleListSelfSecretsApiSelfSecretsGet(w http.ResponseWriter, r *http.Request) {
	if s.callerIsOwner(r) {
		writeError(w, http.StatusForbidden, secretOwnerRevealMsg)
		return
	}
	rows, err := s.dal.ListSecrets()
	if err != nil {
		internalError(w, err)
		return
	}
	out := selfSecretsDTO{Secrets: []selfSecretDTO{}}
	for _, sec := range rows {
		out.Secrets = append(out.Secrets, selfSecretDTO{ID: sec.ID, Name: sec.Name, Note: sec.Note})
	}
	writeJSON(w, http.StatusOK, out)
}

// revealRequestedIDs reads the ids a body asked for even when the body is
// otherwise invalid, so a refusal can still be logged against them. Distinct,
// in order, capped so a hostile body cannot flood the log; [""] when none.
func revealRequestedIDs(raw []byte) []string {
	var loose struct {
		IDs []any `json:"ids"`
	}
	_ = json.Unmarshal(raw, &loose)
	seen := map[string]bool{}
	var ids []string
	for _, v := range loose.IDs {
		id, ok := v.(string)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) == secretRevealMaxIDs {
			break
		}
	}
	if len(ids) == 0 {
		return []string{""}
	}
	return ids
}

// logRevealDenied must not turn a refusal into a different answer: a failed
// audit write is logged and the caller still gets the refusal.
func (s *apiServer) logRevealDenied(r *http.Request, ids []string) {
	member, machine, now := currentActor(r), currentMachineClaim(r), nowSecs()
	err := s.dal.inTx(func(tx *writeTx) error {
		for _, id := range ids {
			if err := insertSecretAccessLogOn(tx, secretActionDenied, id, member, machine, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("[secrets] WARNING: could not record a denied reveal by %s: %v", member, err)
	}
}

func (s *apiServer) HandleRevealSelfSecretsApiSelfSecretsRevealPost(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, secretRevealBodyBytes))
	if err != nil {
		s.logRevealDenied(r, []string{""})
		writeError(w, http.StatusUnprocessableEntity, "could not read request body")
		return
	}
	asked := revealRequestedIDs(raw)
	if s.callerIsOwner(r) {
		s.logRevealDenied(r, asked)
		writeError(w, http.StatusForbidden, secretOwnerRevealMsg)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var body SecretRevealRequestDTO
	if !decodeJSONBodyRequired(w, r, &body, "ids") {
		s.logRevealDenied(r, asked)
		return
	}
	if problem := validateRevealIDs(body.Ids); problem != "" {
		s.logRevealDenied(r, asked)
		writeError(w, http.StatusUnprocessableEntity, problem)
		return
	}
	// Loaded before the transaction: the vault lock may not be taken inside one.
	aead, keyErr := s.vault.cipherFor(nil)
	member, machine, now := currentActor(r), currentMachineClaim(r), nowSecs()
	var missing []string
	out := secretRevealDTO{Secrets: make([]secretValueDTO, 0, len(body.Ids))}
	err = s.dal.inTx(func(tx *writeTx) error {
		found := make([]Secret, 0, len(body.Ids))
		for _, id := range body.Ids {
			sec, err := getSecretOn(tx, id)
			if err != nil {
				return err
			}
			if sec == nil {
				missing = append(missing, id)
				continue
			}
			found = append(found, *sec)
		}
		if len(missing) > 0 {
			for _, id := range body.Ids {
				if err := insertSecretAccessLogOn(tx, secretActionDenied, id, member, machine, now); err != nil {
					return err
				}
			}
			return nil
		}
		if keyErr != nil {
			return keyErr
		}
		for _, sec := range found {
			value, err := openSecretValue(aead, sec.ID, sec.Nonce, sec.ValueCT)
			if err != nil {
				return err
			}
			out.Secrets = append(out.Secrets, secretValueDTO{ID: sec.ID, Name: sec.Name, Note: sec.Note, Value: value})
			if err := insertSecretAccessLogOn(tx, secretActionReveal, sec.ID, member, machine, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeVaultError(w, err)
		return
	}
	if len(missing) > 0 {
		writeError(w, http.StatusNotFound, "unknown secret id(s): "+strings.Join(missing, ", "))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func validateRevealIDs(ids []string) string {
	if len(ids) < 1 || len(ids) > secretRevealMaxIDs {
		return "ids must name 1–" + strconv.Itoa(secretRevealMaxIDs) + " secrets"
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return "ids must be unique: '" + id + "' appears more than once"
		}
		seen[id] = true
	}
	return ""
}
