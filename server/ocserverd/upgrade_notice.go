package main

// upgrade_notice.go — the post-upgrade MIGRATION NOTICE to the assistant
// (T-79; owner ruling on card rc-754f28ac53cd, 2026-09-05, option [2]:
// 「對，但不要開票 —— 直接發一則帶足材料的訊息給 mira 就好」).
//
// WHY THIS EXISTS, and why it is not a pre-written sentence. Three earlier
// shapes were put to the owner and all three were refused, for one reason:
// a message composed ahead of time can only ever RESTATE what the recipient
// could already look up. What nobody can look up is what THIS change means
// for the fleet — who has to be reborn to read the new boot text, whose
// documents just went stale, whose in-flight work was invalidated. That
// judgement needs the diff in front of it, so the station does not attempt
// it: this file ships MATERIAL to an AI member and lets her decide. The
// station has no LLM of its own (verified: zero anthropic/openai calls in
// this binary), so handing the work to a member is not one option among
// several — it is the only shape available.
//
// WHY IT IS PERSISTED RATHER THAN SENT BEFORE THE RE-EXEC. An upgrade ends
// with syscall.Exec replacing this process image, so anything still in
// flight — a GitHub round trip in particular — dies unsent, and blocking the
// restart on the network would make a slow GitHub delay every upgrade. So
// the swap records a small durable marker (recordPendingUpgradeNotice, one
// local DB write on a path that is already writing) and the NEXT boot
// delivers it (deliverPendingUpgradeNotice). Three properties fall out of
// that, all of them wanted:
//   - The message can say 「已經換到 X」 in the past tense and be true. A
//     notice sent before the exec would be a prediction, and a failed exec
//     would make it a false one.
//   - The re-exec is never delayed by the network.
//   - Delivery is exactly-once WITHOUT a second flag: the marker is deleted
//     once delivered, and until then it is re-examined on every boot. A
//     failed exec (old build keeps serving) leaves it pending, and the sha
//     guard below is what tells the two cases apart.
//
// WHY EVERY UPGRADE GETS A MESSAGE, INCLUDING THE BORING ONES. Measured on
// 2026-09-04: eight upgrades in one day, two of which touched the shared
// layer. Firing the full material eight times trains the reader to skip it;
// staying silent on the other six creates the opposite failure — "no message"
// becomes indistinguishable from "nothing happened", which is exactly the
// blind spot this ticket exists to close. So the LENGTH is what varies, not
// the existence: an upgrade that touches neither seeds/ nor spec/ is one
// line, and one line is cheap enough to be worth reading every time.
// (⚠️ The owner ruled on WHO ACTS, not on this; it is the executor's call and
// is flagged as such on the acceptance card.)

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// pendingUpgradeNoticeKey is the settings row that carries one undelivered
// notice. Single-valued on purpose: a station that upgrades twice before it
// manages to deliver has only one interesting story to tell — "you were on
// A, you are now on C" — and the second record overwriting the first says
// exactly that, provided the FROM side is preserved (see below).
const pendingUpgradeNoticeKey = "upgrade.pending_notice"

// upgradeCompareTimeout bounds the one outbound GitHub call this file makes.
// Deliberately larger than updateCheckTimeout: a compare body carrying a few
// hundred files is megabytes, and this runs on a boot goroutine where being
// slow costs nothing. Failing it costs the file list, not the message.
const upgradeCompareTimeout = 20 * time.Second

// upgradeCompareMaxBody caps what we are willing to read from one compare.
// GitHub itself caps `files` at 300 entries, and each entry carries its full
// patch, so this is a ceiling on a bounded thing rather than a guess.
const upgradeCompareMaxBody = 32 << 20

// upgradeNoticeMaxListed is how many ordinary (non-shared-layer) filenames
// the message spells out before it switches to a count. The shared-layer
// ones are never elided — they are the reason the message exists.
const upgradeNoticeMaxListed = 40

// pendingUpgradeNotice is the durable marker written by the process that
// performed the swap and read by the process that came up from it.
type pendingUpgradeNotice struct {
	FromVersion string  `json:"from_version"`
	FromSHA     string  `json:"from_sha"`
	ToVersion   string  `json:"to_version"`
	ToSHA       string  `json:"to_sha"`
	RecordedTS  float64 `json:"recorded_ts"`
}

// recordPendingUpgradeNotice is called by runUpgrade once the verified swap
// has LANDED on disk and before the re-exec is scheduled. It never returns an
// error to its caller and never blocks on anything remote: an upgrade that
// cannot write its notice is still an upgrade, and refusing to restart over a
// missing message would trade a real capability for a report about it.
//
// The FROM side is preserved across a re-record: if a notice is already
// pending (the previous upgrade never got the chance to deliver), the older
// FROM is kept so the surviving message still spans the whole distance the
// station actually travelled.
func (s *apiServer) recordPendingUpgradeNotice(toVersion, toSHA string) {
	notice := pendingUpgradeNotice{
		FromVersion: appVersion,
		FromSHA:     s.processSHA,
		ToVersion:   toVersion,
		ToSHA:       toSHA,
		RecordedTS:  nowSecs(),
	}
	if prev, err := s.readPendingUpgradeNotice(); err == nil && prev != nil && prev.FromSHA != "" {
		notice.FromVersion, notice.FromSHA = prev.FromVersion, prev.FromSHA
	}
	blob, err := json.Marshal(notice)
	if err != nil { // unreachable for this struct; logged rather than ignored
		outsourceLog("upgrade-notice: cannot encode the pending notice for %s: %v", toVersion, err)
		return
	}
	if err := s.dal.PutSetting(pendingUpgradeNoticeKey, string(blob)); err != nil {
		outsourceLog("upgrade-notice: cannot record the pending notice for %s "+
			"(the assistant will NOT be told this station upgraded): %v", toVersion, err)
	}
}

// readPendingUpgradeNotice returns the pending marker, or (nil, nil) when
// there is none. A row that cannot be decoded is reported as an error rather
// than treated as absent, so a corrupted marker is visible instead of
// silently swallowing one upgrade's story.
func (s *apiServer) readPendingUpgradeNotice() (*pendingUpgradeNotice, error) {
	raw, err := s.dal.GetSetting(pendingUpgradeNoticeKey)
	if err != nil {
		return nil, err
	}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	var notice pendingUpgradeNotice
	if err := json.Unmarshal([]byte(*raw), &notice); err != nil {
		return nil, err
	}
	return &notice, nil
}

// startUpgradeNoticeDelivery mounts the one-shot boot delivery. Mounted
// unconditionally by cmdServe: with no pending marker its whole body is one
// indexed read, and gating it behind a toggle would mean the first upgrade
// after someone forgot to arm it is the one that goes unreported.
func (s *apiServer) startUpgradeNoticeDelivery() {
	go s.deliverPendingUpgradeNotice()
}

// deliverPendingUpgradeNotice is ONE boot-time delivery attempt (split out so
// a test can run it synchronously). It returns whether a message was sent.
func (s *apiServer) deliverPendingUpgradeNotice() (sent bool) {
	notice, err := s.readPendingUpgradeNotice()
	if err != nil {
		outsourceLog("upgrade-notice: cannot read the pending notice — no migration "+
			"message will be sent for it: %v", err)
		return false
	}
	if notice == nil {
		return false
	}
	// 🔴 The guard that tells "the swap took" apart from "the exec failed and
	// the OLD build is still serving". Both leave the marker in place; only
	// the first one is a story worth telling, and telling it from the old
	// process would be a lie in the past tense.
	if notice.FromSHA != "" && notice.FromSHA == s.processSHA {
		outsourceLog("upgrade-notice: still running %s — the swap to %s has not taken "+
			"effect in this process; leaving the notice pending", shortSHA(s.processSHA), notice.ToVersion)
		return false
	}

	files, truncated, cmpErr := fetchUpgradeChangedFiles(s.releaseAPIBaseURL(), notice.FromSHA, notice.ToSHA)
	if cmpErr != nil {
		// Degrade to less material, never to silence: the versions and the
		// compare link alone still let the reader do the judging by hand.
		outsourceLog("upgrade-notice: the changed-file list for %s..%s could not be "+
			"fetched (%v) — sending the notice without it", shortSHA(notice.FromSHA), shortSHA(notice.ToSHA), cmpErr)
	}

	recipient, err := s.resolveChatRecipient(seedMiraID)
	if err != nil {
		outsourceLog("upgrade-notice: no assistant to tell (%s): %v — nobody will be "+
			"told that this station moved to %s", seedMiraID, err, notice.ToVersion)
		return false
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    wireSystemSender,
		Recipient: recipient,
		Body:      upgradeNoticeBody(*notice, files, truncated, cmpErr),
		TS:        nowSecs(),
		Meta: map[string]any{
			"upgrade_notice": map[string]any{
				"from_version": notice.FromVersion,
				"from_sha":     notice.FromSHA,
				"to_version":   notice.ToVersion,
				"to_sha":       notice.ToSHA,
				"shared_layer": len(sharedLayerFiles(files)) > 0,
			},
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		// Keep the marker: an undelivered notice is worth one more attempt on
		// the next boot, and re-sending is bounded by the delete below.
		outsourceLog("upgrade-notice: durable message to %s failed (the assistant will "+
			"NOT be told about the move to %s): %v", recipient, notice.ToVersion, err)
		return false
	}
	// Same convenience payload and audience as every chat delta (spec/sse.md
	// §2.2): both participants plus the owner.
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)

	if err := s.dal.DeleteSetting(pendingUpgradeNoticeKey); err != nil {
		// The message IS delivered; failing to clear the marker would repeat
		// it on the next boot, which is noisy but not wrong. Say so.
		outsourceLog("upgrade-notice: the notice for %s was delivered but the marker "+
			"could not be cleared — it may be sent again on the next boot: %v", notice.ToVersion, err)
	}
	return true
}

// githubCompare is the slice of GitHub's compare body this server reads.
type githubCompare struct {
	Files []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
	} `json:"files"`
}

// fetchUpgradeChangedFiles asks GitHub which files moved between two commits.
// Free of any token (public repo) and off the upgrade's critical path.
//
// truncated is true when the answer sits on GitHub's own 300-file ceiling —
// the list is then a floor, not the whole set, and the message says so rather
// than presenting a partial list as complete.
func fetchUpgradeChangedFiles(base, fromSHA, toSHA string) (files []string, truncated bool, err error) {
	if strings.TrimSpace(fromSHA) == "" || strings.TrimSpace(toSHA) == "" {
		return nil, false, fmt.Errorf("both commits are needed (from=%q to=%q)", fromSHA, toSHA)
	}
	req, err := http.NewRequest(http.MethodGet,
		base+"/repos/"+releaseRepo+"/compare/"+fromSHA+"..."+toSHA, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: upgradeCompareTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("github answered %d", resp.StatusCode)
	}
	var cmp githubCompare
	if err := json.NewDecoder(io.LimitReader(resp.Body, upgradeCompareMaxBody)).Decode(&cmp); err != nil {
		return nil, false, err
	}
	for _, f := range cmp.Files {
		if name := strings.TrimSpace(f.Filename); name != "" {
			files = append(files, name)
		}
	}
	return files, len(cmp.Files) >= githubCompareFileCeiling, nil
}

// githubCompareFileCeiling is GitHub's documented per-compare file cap. A
// response sitting exactly on it is indistinguishable from one that was cut,
// so both are reported as possibly incomplete.
const githubCompareFileCeiling = 300

// sharedLayerFiles picks out the paths whose change is the reason anyone
// needs to be told at all:
//   - seeds/ — the boot text EVERY member reads on waking. A running session
//     never re-reads it, so a change here is invisible until someone is
//     reborn, with no signal of any kind.
//   - spec/ — the tool surface. It is frozen into a session at connect time,
//     so a mismatch shows up as a tool that is described but cannot be
//     called, and it does not raise an error.
func sharedLayerFiles(files []string) []string {
	var out []string
	for _, f := range files {
		if strings.HasPrefix(f, "seeds/") || strings.HasPrefix(f, "spec/") {
			out = append(out, f)
		}
	}
	return out
}

// shortSHA is the 8-character form used in log lines and message text.
func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= 8 {
		return sha
	}
	return sha[:8]
}

// upgradeCompareURL is the link the reader clicks to redo the judgement
// themselves — the third layer the ticket asks for ("did you actually go and
// look at what it sits under"). Pure string building; no network.
func upgradeCompareURL(fromSHA, toSHA string) string {
	return "https://github.com/" + releaseRepo + "/compare/" + fromSHA + "..." + toSHA
}

// upgradeNoticeBody is the message the assistant reads. Pure, so every branch
// is testable without a database or a network.
//
// Three shapes, chosen by what is actually known:
//   - the file list could not be fetched → say so plainly and hand over the
//     link, because "I don't know what changed" is a different message from
//     "nothing important changed" and must not be mistaken for it;
//   - nothing in seeds/ or spec/ → one line, cheap to read and cheap to skip;
//   - the shared layer moved → the full material, verdict first.
func upgradeNoticeBody(n pendingUpgradeNotice, files []string, truncated bool, cmpErr error) string {
	versions := fmt.Sprintf("%s (`%s`) → %s (`%s`)",
		n.FromVersion, shortSHA(n.FromSHA), n.ToVersion, shortSHA(n.ToSHA))
	link := upgradeCompareURL(n.FromSHA, n.ToSHA)

	if cmpErr != nil {
		return "⚠️ **站台已經換版，但我查不到這次動了哪些檔案。**\n\n" +
			"**版本**：" + versions + "\n" +
			"**比對**：" + link + "\n\n" +
			"取得檔案清單失敗（" + cmpErr.Error() + "）⇒ **有沒有踩到 `seeds/`（全體開機會讀的共用層）或 `spec/`（工具面）我不知道**。" +
			"這一則不能當成「沒事」，請自己點上面那條連結看過再判斷。"
	}

	shared := sharedLayerFiles(files)
	if len(shared) == 0 {
		tail := fmt.Sprintf("，共 %d 個檔案", len(files))
		if truncated {
			tail = "，檔案數超過 GitHub 單次比對的上限（只回了 300 個），所以這句「未動到」只涵蓋回得來的那部分"
		}
		return "⚪ 站台換版：" + versions + "，**未動到 `seeds/`（共用層）或 `spec/`（工具面）**" + tail + "。\n" +
			"多半不需要你做事；要自己看就點 " + link + "。"
	}

	var b strings.Builder
	b.WriteString("🔴 **站台已經換版，而且動到了全體共用的那一層 —— 這則需要你判斷。**\n\n")
	b.WriteString("**版本**：" + versions + "\n")
	b.WriteString("**比對**：" + link + "\n\n")
	b.WriteString("## 🔴 動到的共用層／工具面檔案\n")
	for _, f := range shared {
		b.WriteString("- `" + f + "`\n")
	}
	b.WriteString("\n`seeds/` 是每個成員**開機才會讀**的文字 —— 正在跑的 session 不會重讀，" +
		"所以在有人重生之前，這個改動對他們是完全沒有訊號的。" +
		"`spec/` 是工具面，連線那一刻就定住，不匹配的時候不會報錯。\n")

	if others := len(files) - len(shared); others > 0 {
		b.WriteString(fmt.Sprintf("\n## 這次同時動到的其他檔案（%d 個）\n", others))
		listed := 0
		for _, f := range files {
			if strings.HasPrefix(f, "seeds/") || strings.HasPrefix(f, "spec/") {
				continue
			}
			if listed >= upgradeNoticeMaxListed {
				b.WriteString(fmt.Sprintf("- …另外 %d 個，看連結\n", others-listed))
				break
			}
			b.WriteString("- `" + f + "`\n")
			listed++
		}
	}
	if truncated {
		b.WriteString("\n⚠️ **這份清單不完整** —— 檔案數碰到 GitHub 單次比對的 300 個上限，" +
			"沒列出來的部分要自己從連結看。\n")
	}

	b.WriteString("\n## 請你判斷並執行\n" +
		"這則只帶材料、不帶結論 —— 站台沒有能力判斷這次改動對艦隊的意思。請你看過之後決定：" +
		"**誰需要重生才會讀到新的開機說明、誰手上的文件已經過期、有沒有人正在做的事被這次改動推翻**，" +
		"然後直接去做（需要時自己發包）。")
	return b.String()
}
