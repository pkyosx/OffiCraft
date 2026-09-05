package main

// dal_lore_proposal.go — T-33. 回饋與提案：一個 agent 讀到一條幫倒忙的傳承，
// 提出「應該長這樣」，而審核看到的差異就是會落地的東西.
//
// 🔴 WHY A WHOLE VERSION AND NOT A PATCH — this is the owner's ruling of
// 2026-09-02 and the reason the table is shaped the way it is. Verbatim: 「我覺得
// 讓 agent submit new full version 即可 / diff view 我們自己產出」. A patch keeps
// TWO artefacts around — what the proposer said he was changing, and what
// applying it actually produces — and the gap between them looks completely
// normal to a reviewer: plausible description, approve, something else lands.
// A whole version has no second artefact. The diff is computed from the exact
// bytes that would be written, so there is no intermediate version for the two
// to disagree about.
//
// 🔴 提案帶的是**完整的新版本，包含`events`的整份事件清單**（負責人 2026-09-03
// 的裁定，卡 rc-e5c34500face：「改得動 —— 提案就該帶完整的新版本，包含所有
// 事件」）。他推翻的講法是「`events`是機器串出來的事實，提案只是意見，意見不該
// 改得動事實」，而那個講法的洞是：**機器串錯的時候，就沒有任何一條路修得了它**。
// 「重跑一次」不成立 —— 沒有經過 API 的動作蓋不到記錄者，那些格只能空著，所以
// 重跑會把人工補上的東西一起沖掉。⇒ 提案改得動事件，才是唯一修得了的路。
//
// 🔴 WHAT THIS FILE DOES AND DOES NOT DO ABOUT ACCEPTING, said plainly because a
// reader will look for it. ApplyLoreProposal (檔案末尾) is the MECHANISM: 核可
// 一份提案時，本體那幾格被寫上去、lore_event 被**整批**換成提案帶的那一份、L0 多一列
// 記著審核者核可的那串位元組。它**不判斷誰可以核可**，也不寫裁決紀錄。
//
// 誰有資格核可這一格**已經裁定了**：負責人於 rc-a896af93d4f9 圈選「你 ＋ 銀月
// （沿用現有前例）」，也就是 owner 與 admin agent；那道裁定寫在路由表的
// `Requires: principalAdminAgent` 上，路由是
// POST /api/lore/entries/{entry_id}/proposals/{proposal_id}/accept
// （handler 在 api_lore_proposal.go）。這一層還是不判斷 —— 政策只寫在路由表
// 那一行，這裡寫第二份就會有兩個會各自漂移的答案。
//
// ⚠️ 仍然**沒有裁定**的還有兩件，所以兩件都沒有實作：退回（decline）長什麼樣，
// 以及要不要留一列裁決紀錄。落地時唯一的紀錄還是新的 lore_revision 那一列的
// actor_id ＝ 核可的人。先做機制、政策留白仍然是刻意的：替負責人補上他沒說過的
// 那兩件，等於自己決定誰可以丟掉別人的提案。
//
// 🔴 過期提案是跟 PR 一模一樣的坑, and it is the whole reason `base_sha256`
// exists. A proposal written on Monday, reviewed on Friday, with the entry
// rewritten underneath it on Wednesday: applying it discards Wednesday silently,
// and the result looks entirely correct. The digest is compared in two places on
// purpose —
//
//   * at SUBMIT (CreateLoreProposal): the proposer names the digest he read; a
//     mismatch is refused 409 rather than stored, because a proposal that was
//     already stale when it was filed can only ever mislead a reviewer.
//   * at READ (ListLoreProposals): every row is re-compared against the entry's
//     CURRENT latest revision, because the interesting case is the one that went
//     stale AFTER it was filed — submit-time checking alone cannot see it.
//
// One comparison would have been the wrong number in either direction: only at
// submit and Friday's reviewer is blind; only at read and a proposal can be
// filed against a version nobody is looking at any more.

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrLoreProposalKindUnknown  = errors.New("lore: the proposal `kind` is not `update` or `remove`")
	ErrLoreProposalFaultUnknown = errors.New("lore: the proposal `fault` is not `stale`, `never-true` or `misled`")
	ErrLoreProposalEncountered  = errors.New("lore: `encountered` is blank — say what you were doing when this entry reached you")
	ErrLoreProposalEvidence     = errors.New("lore: `evidence` is blank — say what you actually saw, not what you think")
	ErrLoreProposalBaseBlank    = errors.New("lore: `base_sha256` is blank — a proposal has to name the version it was written against")
	ErrLoreProposalRemoveBody   = errors.New("lore: a `remove` proposal carries body fields — a removal proposes no new version, and a version nobody will ever write is exactly the description/result gap this shape exists to remove")
	ErrLoreProposalRemoveEvents = errors.New("lore: a `remove` proposal carries events — a removal proposes no new version at all, `events` included")
	// 🔴 「沒送 events」跟「送了一份空的 events」被分開，而且前者是拒絕。
	// 兩者長得一樣的話，一次漏填就會在審核者完全看不見的地方把`events`清空 ——
	// 而那正是這張表存在要消滅的描述／結果落差。空陣列是一個看得見的主張
	//（「這條條目不該有事件」），漏填不是主張，是漏填。
	ErrLoreProposalEventsMissing = errors.New(
		"lore: an `update` proposal must carry `events` — 提案帶的是完整的新版本，" +
			"`events`也在裡面。要主張「這條不該有事件」請明白送一個空陣列 `[]`；" +
			"省略它會讓一次漏填跟一次刪除長得一模一樣")
	ErrLoreProposalNotUpdate = errors.New(
		"lore: only an `update` proposal carries a version to apply — the act a `remove` asks for is retire_lore_entry")
	ErrLoreProposalUnknown  = errors.New("lore: no proposal carries that id")
	ErrLoreProposalNoChange = errors.New("lore: the proposed version is identical to the one it was written against — there is nothing to review")
	ErrLoreProposalStale    = errors.New("lore: this entry changed while you were reviewing it")
	ErrLoreEntryNoOriginal  = errors.New("lore: the entry has no preserved original to propose against")
)

// loreProposalKinds / loreProposalFaults are the two closed sets, declared once
// here and mirrored by a CHECK constraint in 00083. The CHECK is the backstop;
// this is what produces an error a caller can read.
//
// 🔴 `fault` IS THE OWNER'S THREE, NOT lore_feedback's `shape`. He named them on
// 2026-09-02: 過時（當時對現在不對）／本來就錯／害他走錯路. They are three
// different repairs — a stale entry wants rewriting against today, an entry that
// was never true wants retiring with `falsified`, and one that MISLED wants its
// 標題格（heading）fixed so it stops being retrieved for situations it does not describe
// — so an undifferentiated 「這條不好」 tells a reviewer nothing about what to do.
var (
	loreProposalKinds  = map[string]bool{"update": true, "remove": true}
	loreProposalFaults = map[string]bool{"stale": true, "never-true": true, "misled": true}
)

// LoreProposal is one submitted proposal, as sent.
type LoreProposal struct {
	EntryID string
	Kind    string

	// BaseSHA256 is the digest of the revision the proposer actually read. It is
	// caller-supplied and that is the entire mechanism: a value the server filled
	// in for itself would always match and would prove nothing.
	BaseSHA256 string

	Encountered string
	Fault       string
	Evidence    string

	// 🔴 提案帶的是**完整的新版本**：三格 + `events`的整份事件清單。
	// 負責人 2026-09-03 裁定（卡 rc-e5c34500face）：「改得動 —— 提案就該帶完整的
	// 新版本，包含所有事件」。
	// ⚠️ 這裡以前是四格，多的那一格是 `Trigger`；`rc-9002654dd81c`（2026-09-06）
	// 把它併進 Heading（宣告在下面那一段）。
	Content    string
	RetireWhen string
	Impact     string

	// 🔴 這裡叫 Impact，而 lore_proposal 那一欄仍然叫 `problem`，兩邊的 SQL 也照
	// 樣寫 `problem`。00084 只把 lore_entry 的那一欄改了名，沒有動提案表，而那是
	// 對的：欄位名字在提案表裡只是儲存位置，改它要一次 migration，換來的是零。
	// ⚠️ 代價寫在這裡而不是靠人記得：讀這幾段 SQL 的人會看到欄名與欄位對不上，
	// 而那不是 bug。線上的名字（spec 與 DTO）已經全部是 `impact`。
	//
	// 🔴 Heading 與 ImpactStars 現在**在**提案上，而它們是同一個裁定的兩半。
	// owner 2026-09-05 於 rc-bbccbeb3d9e6 逐字：「任何修改都是提案的一環」。
	// 在此之前提案表沒有這兩欄，後果不是「少了兩格」而是一份**主動說謊的原文**：
	// loreRevisionBody 印 heading，loreProposalEntry 給它零值，核可寫下的原文
	// 就宣稱這條沒有標題 —— 實測配陽性對照坐實過（見 00084 檔內那一段）。
	// 而第二個後果更難看見：「什麼都沒改的提案要被拒絕」是比兩串 digest，提案
	// 那一串永遠少一格 ⇒ 兩串永遠不等 ⇒ **那道守衛恆真、永遠不擋任何東西**。
	Heading     string
	ImpactStars int

	// Events 是提案主張的**整份**`events`，不是增量。核可時 lore_event 會被整批
	// 換成這一份。
	//
	// 🔴 nil 跟 []LoreEvent{} 在這裡是**兩件不同的事**，而且必須是：
	//   nil            — 提案沒提到`events` ⇒ 在 `update` 上是拒絕
	//                    （ErrLoreProposalEventsMissing）
	//   []LoreEvent{}  — 提案主張「這條條目不該有事件」⇒ 合法，而且審核者在
	//                    events_removed 裡看得到它要刪掉哪幾筆
	// 把兩者折成同一件事，等於讓一次漏填在沒有人主張過的情況下清空`events`。
	Events []LoreEvent

	ActorID string
}

// LoreProposalResult is what the submission actually stored, read back off the
// rendering rather than echoed.
type LoreProposalResult struct {
	ProposalID     string
	BaseRevisionID int64
	BaseSHA256     string
	SHA256         string
}

// LoreProposalRow is one proposal as served, with `Stale` computed at read time
// against the entry as it stands NOW.
type LoreProposalRow struct {
	ID             string
	EntryID        string
	Kind           string
	BaseRevisionID int64
	BaseSHA256     string
	Encountered    string
	Fault          string
	Evidence       string
	Heading        string
	Content        string
	RetireWhen     string
	Impact         string
	ImpactStars    int
	Body           string
	SHA256         string
	ActorID        string
	CreatedTS      float64

	// Events 是這份提案主張的**整份**`events`，讀回來的順序是事情發生的順序。
	// 一份 `remove` 提案沒有事件，因為它沒有主張任何版本。
	Events []LoreEvent

	// 🔴 EventsAdded / EventsRemoved 是**給審核者看的**，而且是這一批補上的
	// 需求：一份提案改得動事件之後，「他改了哪幾筆」就不能只靠審核者自己把兩份
	// 清單擺在一起用眼睛比。兩份清單還是照樣送（Events 與 LoreProposalList
	// .CurrentEvents），所以這個差異是**可以被重算驗證的**，不是要人相信的。
	//
	// 🔴 跟 Stale 一樣：**算出來的，不存**。存下來的差異在寫下那天是對的，之後
	// 每一天都可能是錯的 —— 條目的事件在提案被審之前被別人改過，就會讓審核者
	// 看著一份對照的是舊現況的差異做決定。
	//
	// 比對用的鍵是 (happened_ts, what, actor, place, object)，跟
	// loreRevisionBody 排序用的那一組一模一樣。用 id 比不行：提案的事件根本
	// 沒有 lore_event 的 id，而且同一筆事實重打一次會拿到不同的 id。
	EventsAdded   []LoreEvent
	EventsRemoved []LoreEvent

	// Stale: the entry's latest revision is no longer the one this proposal was
	// written against. NOT stored — a stored flag would be right on the day it
	// was written and wrong every day after, which is the second-truth failure
	// this whole ticket is about.
	Stale bool
}

// LoreProposalList is an entry's proposals together with the version they are
// all being compared against.
//
// 🔴 CurrentSHA256 TRAVELS WITH THE LIST rather than being left for the reader to
// fetch. `Stale` is a comparison, and a comparison served without the thing it
// compared against cannot be checked by whoever reads it.
type LoreProposalList struct {
	EntryID           string
	CurrentRevisionID int64
	CurrentSHA256     string

	// CurrentEvents 是條目**現在**的`events`，跟 CurrentSHA256 同一個理由旅行在
	// 一起：每一列的 EventsAdded / EventsRemoved 都是一次比較，而一次比較送出來
	// 卻不附上被比較的那一邊，讀的人只能相信它。
	CurrentEvents []LoreEvent

	Proposals []LoreProposalRow
}

// loreProposalEntry 把一份提案的本體幾格包成 LoreEntry，好讓**共用的**渲染器摘要
// 它。`events`不在 LoreEntry 裡（見 dal_lore.go），它是 loreRevisionBody 的第二
// 個參數，呼叫處傳的是提案自己帶的那一份。
//
// 🔴 ONE RENDERER, AND THAT IS LOAD-BEARING. loreRevisionBody is what the L0
// journal stores and what `sha256` on a revision digests. A proposal rendered by
// a second, near-identical function would produce a digest that could not be
// compared with a revision's — and 「這份提案就是那一版」 would stop being an
// answerable question the moment the two drifted by one newline.
//
// 🔴 Heading 與 ImpactStars 都在這裡，而那正是上面那段「一個渲染器」的意思：
// 渲染器印哪幾格，提案就必須帶得動哪幾格。前一版把這兩格留成零值，並在註解裡
// 寫下「哪一天有人讓渲染器印標題，這裡就會開始把空標題摘要進去」—— 那一天到了，
// 而它是被四支測試抓到的，不是被這段註解擋住的。註解不是守衛。
func loreProposalEntry(p LoreProposal) LoreEntry {
	return LoreEntry{
		Heading:     p.Heading,
		Content:     p.Content,
		RetireWhen:  p.RetireWhen,
		Impact:      p.Impact,
		ImpactStars: p.ImpactStars,
	}
}

// loreProposalShapeError validates everything that can be decided WITHOUT
// looking at the entry.
//
// 🔴 SHAPE IS CHECKED BEFORE THE DIGEST, DELIBERATELY. The staleness refusal
// tells a proposer to go and rebuild his version on what is there now; sending
// him to do that on a body that would have been refused anyway wastes the trip.
// So a malformed proposal against a moved entry answers 422 (fix this), and a
// well-formed one answers 409 (rebase this).
func loreProposalShapeError(p LoreProposal) error {
	if p.ActorID == "" {
		return ErrLoreActorBlank
	}
	if !loreProposalKinds[p.Kind] {
		return fmt.Errorf("%w: %q", ErrLoreProposalKindUnknown, p.Kind)
	}
	if !loreProposalFaults[p.Fault] {
		return fmt.Errorf("%w: %q", ErrLoreProposalFaultUnknown, p.Fault)
	}
	if strings.TrimSpace(p.Encountered) == "" {
		return ErrLoreProposalEncountered
	}
	if strings.TrimSpace(p.Evidence) == "" {
		return ErrLoreProposalEvidence
	}
	if strings.TrimSpace(p.BaseSHA256) == "" {
		return ErrLoreProposalBaseBlank
	}
	if p.Kind == "remove" {
		// A removal proposes no new version. Carrying one would put a version on
		// the reviewer's screen that no accept path would ever write — the
		// description/result gap in miniature, inside the shape built to close it.
		for _, f := range []string{p.Heading, p.Content, p.RetireWhen, p.Impact} {
			if strings.TrimSpace(f) != "" {
				return ErrLoreProposalRemoveBody
			}
		}
		// 星等同理：一份 `remove` 不主張任何版本，帶一個星等等於在審核者眼前
		// 放一個沒有任何核可路徑會寫下去的數字。
		if p.ImpactStars != 0 {
			return ErrLoreProposalRemoveBody
		}
		// `events`同理。一份 `remove` 帶著事件，會讓審核者看到一份沒有任何核可
		// 路徑會寫下去的`events`。⚠️ 這裡拒絕的是**非空**，不是 nil：`remove`
		// 沒有「必須明白說`events`」的問題，因為它整份版本都不主張。
		if len(p.Events) > 0 {
			return ErrLoreProposalRemoveEvents
		}
		return nil
	}
	// 🔴 AN `update` IS HELD TO THE SAME FIELD RULES AS A WRITE, and the errors
	// are the WRITE's errors rather than new ones. Accepting a proposal means
	// writing a version through the ordinary write path, so a proposal that path
	// would refuse is a proposal that can never be accepted — and it would sit in
	// the queue looking exactly like one that could.
	// 🔴 標題與星等現在也走寫入路徑自己的檢查，理由跟下面兩格一模一樣：核可
	// 一份提案等於走一次普通寫入，寫入會拒絕的東西在這裡就要被拒絕。
	if err := loreHeadingError(p.Heading); err != nil {
		return err
	}
	if strings.TrimSpace(p.Content) == "" {
		return ErrLoreContentBlank
	}
	if err := loreImpactStarsError(p.ImpactStars); err != nil {
		return err
	}
	// 🔴 `events`在 `update` 上是**必填**，而空陣列就滿足它。理由跟上面那兩格
	// 「空白就拒絕」不一樣：這一格不是不能空，是不能**沒說**。提案帶的是完整的
	// 新版本，核可時 lore_event 會被整批換成它 —— 所以一份沒說`events`的提案，
	// 落地時等於在沒有人主張過的情況下清空事件。空陣列是主張，nil 不是。
	if p.Events == nil {
		return ErrLoreProposalEventsMissing
	}
	return nil
}

// CreateLoreProposal files one proposal against the version its author read.
//
// The staleness refusal is the reason this function is not a plain INSERT.
func (d *DAL) CreateLoreProposal(p LoreProposal, nowTS float64) (LoreProposalResult, error) {
	var out LoreProposalResult
	if err := loreProposalShapeError(p); err != nil {
		return out, err
	}
	// 🔴 事件的逐列檢查跟寫入路徑用**同一個** loreEventError，理由跟本體幾格用寫入
	// 路徑自己的錯誤是同一個：核可一份提案等於走一次普通寫入，所以寫入會拒絕的
	// 事件在這裡就要被拒絕，否則它會躺在佇列裡，看起來跟一份可以被核可的提案
	// 一模一樣。它排在形狀檢查之後、過期檢查之前，跟本體幾格同一層。
	for _, ev := range p.Events {
		if err := d.loreEventError(ev); err != nil {
			return out, err
		}
	}

	entry, err := d.GetLoreEntry(p.EntryID)
	if err != nil {
		return out, err
	}
	if entry == nil {
		return out, fmt.Errorf("%w: %q", ErrLoreEntryUnknown, p.EntryID)
	}
	base, err := d.LatestLoreRevision(p.EntryID)
	if err != nil {
		return out, err
	}
	if base == nil {
		// An entry with no L0 row is the state CreateLoreEntry's transaction
		// exists to rule out. Proposing against it would mean proposing against
		// nothing, so it is refused rather than defaulted to an empty base.
		return out, fmt.Errorf("%w: %q", ErrLoreEntryNoOriginal, p.EntryID)
	}

	// 🔴 THE COMPARISON. Everything else in this file is arrangements around it.
	if p.BaseSHA256 != base.SHA256 {
		return out, fmt.Errorf(
			"%w: entry %s — you wrote this against %s, but it now stands at %s "+
				"(revision %d). Re-read the entry and rebuild your version on what "+
				"is there now; filing it against the older text would discard "+
				"whoever changed it, silently",
			ErrLoreProposalStale, p.EntryID, p.BaseSHA256, base.SHA256, base.ID)
	}

	var body, sum string
	if p.Kind == "update" {
		// 🔴 用**提案自己帶的那份事件清單**渲染，不是條目目前的。
		// loreRevisionBody 把`events`也算進 sha256（見 dal_lore_write.go），所以
		// 審核者比對的那串位元組，就是核可時會落地的那一份 —— 本體幾格與事件都是。
		// 負責人 2026-09-03 裁定（卡 rc-e5c34500face）：「改得動 —— 提案就該帶
		// 完整的新版本，包含所有事件」。
		//
		// 🔴 他推翻的講法是「`events`是機器串出來的事實，意見不該改得動事實」，
		// 而那個講法的洞是：**機器串錯的時候沒有任何一條路修得了它**。重跑會把
		// 人工補上的東西一起沖掉（沒經過 API 的動作蓋不到記錄者，那些格只能空著），
		// 所以提案改得動事件，是唯一修得了的路。
		body = loreRevisionBody(loreProposalEntry(p), p.Events)
		sum = loreSHA256(body)
		// The digests are comparable because ONE renderer produced both — see
		// loreProposalEntry. A proposal that changes nothing is refused rather
		// than stored: it costs a reviewer a read and can end in no change.
		// ⚠️ 「什麼都沒改」現在也把`events`算進去：只動了事件、本體幾格一字未改的提案
		// 會摘要成不同的一串，所以它不會被這一行誤殺。
		if sum == base.SHA256 {
			return out, ErrLoreProposalNoChange
		}
	}

	out.ProposalID = "lp-" + newHexID(12)
	// 🔴 提案本體與它的事件是**一個 transaction**。分開寫的失敗模式是一份存在、
	// 讀得到、但`events`只有一半的提案 —— 而那在審核者的畫面上跟一份完整的提案
	// 長得一模一樣，正是這整張票在治的病。
	err = d.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO lore_proposal (
				id, entry_id, kind, base_revision_id, base_sha256,
				encountered, fault, evidence,
				heading, content, retire_when, problem, impact_stars,
				body, sha256, actor_id, created_ts)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			out.ProposalID, p.EntryID, p.Kind, base.ID, base.SHA256,
			p.Encountered, p.Fault, p.Evidence,
			p.Heading, p.Content, p.RetireWhen, p.Impact, p.ImpactStars,
			body, sum, p.ActorID, nowTS); err != nil {
			return err
		}
		for i, ev := range p.Events {
			if _, err := tx.Exec(`
				INSERT INTO lore_proposal_event
					(proposal_id, seq, happened_ts, what, actor, place, object)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				out.ProposalID, i, ev.HappenedTS, ev.What,
				ev.Actor, ev.Place, ev.Object); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return LoreProposalResult{}, err
	}
	out.BaseRevisionID = base.ID
	out.BaseSHA256 = base.SHA256
	out.SHA256 = sum
	return out, nil
}

// ListLoreProposals serves an entry's proposals, NEWEST FIRST, each marked with
// whether it still stands against the entry as it is now.
//
// Newest first because a proposer who rewrote his proposal against a newer
// version wants the newer one read; oldest-first would lead with the one he
// himself replaced.
//
// 🔴 ORDERED BY created_ts, NOT BY id. A proposal id is "lp-" + newHexID(12) —
// random hex, carrying no time at all — so `ORDER BY id DESC` returns an
// arbitrary order that LOOKS like an order. It is the failure this whole route
// is least able to notice: every row is present, every field is right, and the
// only thing wrong is which one the reviewer reads first. id stays as the
// tie-break so two proposals filed in the same second still come back in a
// stable order rather than whatever the scan happens to produce.
func (d *DAL) ListLoreProposals(entryID string) (LoreProposalList, error) {
	out := LoreProposalList{EntryID: entryID, Proposals: []LoreProposalRow{}}
	current, err := d.LatestLoreRevision(entryID)
	if err != nil {
		return out, err
	}
	if current == nil {
		return out, fmt.Errorf("%w: %q", ErrLoreEntryNoOriginal, entryID)
	}
	out.CurrentRevisionID = current.ID
	out.CurrentSHA256 = current.SHA256
	// 現況的`events`。每一列的 EventsAdded / EventsRemoved 都是拿它比出來的，所以
	// 它跟 CurrentSHA256 一起旅行 —— 讀的人要重算得出來，才不必相信。
	currentEvents, err := d.ListLoreEvents(entryID)
	if err != nil {
		return out, err
	}
	out.CurrentEvents = currentEvents
	haveNow := loreEventKeySet(currentEvents)

	rows, err := d.rdb.Query(`
		SELECT id, entry_id, kind, base_revision_id, base_sha256,
		       encountered, fault, evidence,
		       heading, content, retire_when, problem, impact_stars,
		       body, sha256, actor_id, created_ts
		FROM lore_proposal WHERE entry_id = ? ORDER BY created_ts DESC, id DESC`, entryID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p LoreProposalRow
		if err := rows.Scan(
			&p.ID, &p.EntryID, &p.Kind, &p.BaseRevisionID, &p.BaseSHA256,
			&p.Encountered, &p.Fault, &p.Evidence,
			&p.Heading, &p.Content, &p.RetireWhen, &p.Impact, &p.ImpactStars,
			&p.Body, &p.SHA256, &p.ActorID, &p.CreatedTS,
		); err != nil {
			return LoreProposalList{}, err
		}
		// 🔴 COMPUTED HERE, EVERY TIME, AGAINST THE DIGEST — not against the
		// revision id. An id comparison would answer the same today and would
		// start lying the day anything writes a revision row whose text is
		// unchanged: the proposal would read as stale while the words it was
		// written against are still exactly what is there.
		p.Stale = p.BaseSHA256 != current.SHA256
		out.Proposals = append(out.Proposals, p)
	}
	if err := rows.Err(); err != nil {
		return LoreProposalList{}, err
	}
	// 🔴 事件在 rows 掃完之後才讀，不是在迴圈裡面。同一個 *sql.DB 上開著一個
	// 還沒讀完的 rows 再發第二個查詢，在 SQLite 上會拿到一條被鎖住的連線；
	// 這不是效能問題，是會卡住的問題。
	for i := range out.Proposals {
		evs, err := d.listLoreProposalEvents(out.Proposals[i].ID)
		if err != nil {
			return LoreProposalList{}, err
		}
		out.Proposals[i].Events = evs
		// 一份 `remove` 不主張任何版本，所以它的差異是空的 —— 不是「刪掉全部
		// 事件」。它要求的是 retire，而 retire 不動`events`。
		if out.Proposals[i].Kind != "update" {
			out.Proposals[i].EventsAdded = []LoreEvent{}
			out.Proposals[i].EventsRemoved = []LoreEvent{}
			continue
		}
		proposed := loreEventKeySet(evs)
		added := []LoreEvent{}
		for _, ev := range evs {
			if !haveNow[loreEventKey(ev)] {
				added = append(added, ev)
			}
		}
		removed := []LoreEvent{}
		for _, ev := range currentEvents {
			if !proposed[loreEventKey(ev)] {
				removed = append(removed, ev)
			}
		}
		out.Proposals[i].EventsAdded = added
		out.Proposals[i].EventsRemoved = removed
	}
	return out, nil
}

// loreEventKey 是一筆事件的身分：五格的內容本身，不是它的 id。
//
// 🔴 用 id 比對是行不通的，而且不是效率問題：提案的事件根本沒有 lore_event 的
// id（它們在另一張表），而且同一件事實被重打一次會拿到一個不同的 id —— 用 id 比
// 會把「原封不動留著的那一筆」報成「刪一筆、加一筆」，審核者看到的差異就會比
// 實際的改動大，而那種噪音會讓人停止讀差異。
//
// 分隔符號用 \x00，因為五格都是自由文字：用 \t 或 | 的話，一筆 what 裡面剛好
// 有那個字元的事件，就能跟另一筆不同的事件撞出同一把鍵。
func loreEventKey(ev LoreEvent) string {
	return strings.Join([]string{
		strconv.FormatFloat(ev.HappenedTS, 'f', -1, 64),
		ev.What, ev.Actor, ev.Place, ev.Object,
	}, "\x00")
}

func loreEventKeySet(evs []LoreEvent) map[string]bool {
	out := make(map[string]bool, len(evs))
	for _, ev := range evs {
		out[loreEventKey(ev)] = true
	}
	return out
}

// listLoreProposalEvents 讀回一份提案主張的整份`events`，**按事情發生的順序**
// （happened_ts，seq 只是同一刻的 tie-break）—— 跟 ListLoreEvents 同一條規則，
// 因為審核者是把兩份清單擺在一起看的，兩邊用不同的順序就會逼他自己排。
func (d *DAL) listLoreProposalEvents(proposalID string) ([]LoreEvent, error) {
	rows, err := d.rdb.Query(`
		SELECT happened_ts, what, actor, place, object
		FROM lore_proposal_event WHERE proposal_id = ?
		ORDER BY happened_ts, seq`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoreEvent{}
	for rows.Next() {
		var ev LoreEvent
		if err := rows.Scan(&ev.HappenedTS, &ev.What,
			&ev.Actor, &ev.Place, &ev.Object); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ── 核可：把提案帶的那一份完整版本寫上去 ─────────────────────────────────────

// LoreProposalApplied is what accepting one proposal actually wrote.
type LoreProposalApplied struct {
	ProposalID  string
	EntryID     string
	RevisionID  int64
	SHA256      string
	EventsAfter int
}

// GetLoreProposal returns ONE filed proposal with its events, or nil.
func (d *DAL) GetLoreProposal(proposalID string) (*LoreProposalRow, error) {
	var p LoreProposalRow
	err := d.rdb.QueryRow(`
		SELECT id, entry_id, kind, base_revision_id, base_sha256,
		       encountered, fault, evidence,
		       heading, content, retire_when, problem, impact_stars,
		       body, sha256, actor_id, created_ts
		FROM lore_proposal WHERE id = ?`, proposalID).Scan(
		&p.ID, &p.EntryID, &p.Kind, &p.BaseRevisionID, &p.BaseSHA256,
		&p.Encountered, &p.Fault, &p.Evidence,
		&p.Heading, &p.Content, &p.RetireWhen, &p.Impact, &p.ImpactStars,
		&p.Body, &p.SHA256, &p.ActorID, &p.CreatedTS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	evs, err := d.listLoreProposalEvents(p.ID)
	if err != nil {
		return nil, err
	}
	p.Events = evs
	return &p, nil
}

// ApplyLoreProposal writes an accepted `update` proposal onto its entry: the
// four cells, THE WHOLE FIFTH — events replaced wholesale, not merged — and one
// new L0 revision carrying the exact bytes the reviewer approved.
//
// 🔴 整批換掉，不是合併。負責人 2026-09-03 的裁定（卡 rc-e5c34500face）是
// 「提案就該帶完整的新版本，包含所有事件」，而「完整」只有在核可時真的整份取代
// 才成立。合併語意會讓提案永遠只加得了事件、刪不掉 —— 而「機器串錯了一筆」正是
// 這條路存在的理由：重跑修不了它（重跑會把人工補的一起沖掉），刪不掉的話就沒有
// 任何一條路修得了。
//
// 🔴 寫進 lore_revision 的是提案**存下來的那串 body 與 sha256**，不是在這裡重新
// 渲染的。審核者核可的是那串位元組；重新渲染會在渲染器改過的那天悄悄寫下一份
// 別的東西，而且兩邊都看起來完全正常。
//
// 🔴 過期在這裡**再檢查一次**。提交時檢查過了，讀取時每一列也重算了 `stale`，
// 但這兩者都不是「按下核可的那一刻」：條目可以在審核者讀完清單到他按下去之間
// 被改掉。少了這一次檢查，這條路就會安靜地把中間那個人的修改丟掉 —— 正是
// base_sha256 這整套機制存在要擋的那件事。
//
// ⚠️ 這一層**不判斷誰可以核可**，也不寫 lore_governance_event。這個函式提供的是
// 「核可時會發生什麼」這個機制，不是「誰可以核可」這個政策。
//
// 那道政策現在有了：負責人於 rc-a896af93d4f9 裁定 owner 與 admin agent 可以核可
// （「你 ＋ 銀月（沿用現有前例）」），寫在路由
// POST /api/lore/entries/{entry_id}/proposals/{proposal_id}/accept 的
// `Requires: principalAdminAgent` 上，並且只寫在那裡 —— 這個函式仍然不看呼叫者
// 是誰，因為同一條規則寫兩份就會有兩個各自漂移的答案。
//
// ⚠️ 退回（decline）長什麼樣、要不要留一列裁決紀錄，**都還沒有裁定**，所以兩者
// 都不存在。落地時唯一留下的紀錄是新的 lore_revision 那一列的 actor_id ＝
// 核可的人。
//
// 🔴 「完整的新版本」這句話在 v8 之後**不再涵蓋整條條目**，而這是這個函式現在
// 最容易被誤讀的地方：UPDATE 只碰當時的四個本體格（`trigger` / `content` /
// `retire_when` / `impact`），`heading` 與 `impact_stars` 原封不動留在
// 條目上。**這一版起不再是這樣**：owner 2026-09-05 於 rc-bbccbeb3d9e6 逐字裁
// 「任何修改都是提案的一環」⇒ lore_proposal 補上了 heading 與 impact_stars
// （00084 的兩支 ALTER TABLE），而這裡把它們一起寫回條目。
// ⇒ v8 的標題與星等**現在有修改路徑了**；在此之前寫錯只能新寫一條去 supersede。
func (d *DAL) ApplyLoreProposal(proposalID, actorID string, nowTS float64) (LoreProposalApplied, error) {
	var out LoreProposalApplied
	if actorID == "" {
		return out, ErrLoreActorBlank
	}
	p, err := d.GetLoreProposal(proposalID)
	if err != nil {
		return out, err
	}
	if p == nil {
		return out, fmt.Errorf("%w: %q", ErrLoreProposalUnknown, proposalID)
	}
	if p.Kind != "update" {
		return out, fmt.Errorf("%w: %q is a %q proposal", ErrLoreProposalNotUpdate, proposalID, p.Kind)
	}
	current, err := d.LatestLoreRevision(p.EntryID)
	if err != nil {
		return out, err
	}
	if current == nil {
		return out, fmt.Errorf("%w: %q", ErrLoreEntryNoOriginal, p.EntryID)
	}
	if p.BaseSHA256 != current.SHA256 {
		return out, fmt.Errorf(
			"%w: entry %s — proposal %s was written against %s, but it now stands "+
				"at %s (revision %d). Accepting it would discard whoever changed it "+
				"in between, silently",
			ErrLoreProposalStale, p.EntryID, p.ID, p.BaseSHA256, current.SHA256, current.ID)
	}

	// 🔴 標題在**核可時**再擋一次，而這不是重複的檢查。形狀檢查只看得到走過它
	// 的提案；在 heading 這一欄存在**之前**送出的提案（試用站上就有 27 份）從來
	// 沒有經過那道門，而它們的 heading 讀回來是空字串。核可它們會把條目上好端端
	// 的標題清成空的 —— 用「沒有人送」冒充「有人主張要清掉」。
	// ⚠️ 這一道擋的是既有資料，不是使用者的輸入，所以它會永遠存在而不是過渡措施：
	// 只要 lore_proposal 留著那 27 列，這條路就永遠可能被走到。
	// 🔴 標題超長也擋在**同一個**呼叫裡，而它不是「順便」：核可會把 p.Heading
	// 寫回 lore_entry（下面那支 UPDATE），所以這一步就是一次寫入 —— 只擋新寫入
	// 而不擋這裡，等於替 140 字元上限留了一條繞得過去的路。
	// ⚠️ 兩種拒絕的說明不能共用一句：空白是「這份提案沒有主張標題」（那 27 份
	// 舊提案），超長是「這份提案主張了一個寫不進去的標題」。把後者套上前者那句
	// 話，會叫寫的人去補一個他其實已經有的東西。
	if err := loreHeadingError(p.Heading); err != nil {
		if errors.Is(err, ErrLoreHeadingBlank) {
			return out, fmt.Errorf("%w — proposal %s carries no 標題, so accepting it would blank "+
				"the entry's own. It was filed before 標題 became a cell; rewrite it against the "+
				"current version instead", err, p.ID)
		}
		return out, fmt.Errorf("%w（proposal %s）", err, p.ID)
	}

	// shrink_chars records how much a rewrite REMOVED, measured in runes against
	// the revision it replaces. ⚠️ 這是實作判斷，不是裁定：欄位叫 chars，而 Go 的
	// len() 數的是位元組 —— 一段中文用位元組數會報出三倍的「縮短」。負數夾成 0，
	// 因為「這次改寫變長了」不是一個負的縮短量。
	shrink := len([]rune(current.Body)) - len([]rune(p.Body))
	if shrink < 0 {
		shrink = 0
	}

	err = d.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
			UPDATE lore_entry
			SET heading = ?, content = ?, retire_when = ?,
			    impact = ?, impact_stars = ?, updated_ts = ?
			WHERE id = ?`,
			p.Heading, p.Content, p.RetireWhen,
			p.Impact, p.ImpactStars, nowTS, p.EntryID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("%w: %q", ErrLoreEntryUnknown, p.EntryID)
		}
		// 🔴 整批換掉：先清空再寫入提案帶的那一份。這是這個函式的重點，其他每
		// 一行都是它周圍的安排。刪掉的那幾筆事件不是消失無蹤 —— 舊的 L0 原文
		// （lore_revision）把它們原封不動留著，那正是 loreRevisionBody 把`events`
		// 算進 body 的理由。
		if _, err := tx.Exec(`DELETE FROM lore_event WHERE entry_id = ?`, p.EntryID); err != nil {
			return err
		}
		for _, ev := range p.Events {
			if _, err := tx.Exec(`
				INSERT INTO lore_event (entry_id, happened_ts, what, actor, place, object)
				VALUES (?, ?, ?, ?, ?, ?)`,
				p.EntryID, ev.HappenedTS, ev.What, ev.Actor, ev.Place, ev.Object); err != nil {
				return err
			}
		}
		res, err = tx.Exec(`
			INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
			VALUES (?, ?, ?, ?, ?, ?)`,
			p.EntryID, p.Body, p.SHA256, actorID, nowTS, shrink)
		if err != nil {
			return err
		}
		out.RevisionID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return LoreProposalApplied{}, err
	}
	out.ProposalID = p.ID
	out.EntryID = p.EntryID
	out.SHA256 = p.SHA256
	out.EventsAfter = len(p.Events)
	return out, nil
}
