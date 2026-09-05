package main

// dal_lore.go — T-33, the access layer for the tables migration
// 00063 introduces. Same convention as dal_tasks.go / dal_task_artifacts.go:
// explicit per-table methods, no generic repository.
//
// 🔴 SCOPE: THIS IS THE L1 SEAM, THE TWO JOIN TABLES AND THE EVENTS TABLE
// (第 5 格) THAT HANGS OFF L1, NOTHING MORE. The
// revision journal (L0), the governance counters (L2), the recall log, feedback
// and governance events all have tables in 00063 and NO functions here yet —
// they belong to the rounds that write the paths that use them. An empty seam is
// honest; a speculative one has to be maintained before anyone can say whether
// it has the right shape.
//
// 🔴 THERE IS NO DELETE IN THIS FILE, AND THERE IS NOT MEANT TO BE. "Throwing an
// entry away" is spelled `status='retired'` — it stops being retrieved and keeps
// existing. The owner chose that (rc-559af60bfba4) knowing what it costs: there
// is no way to remove a secret that gets written into an entry. Adding a hard
// delete here would quietly re-open a decision that was made deliberately, so if
// an erase path is ever wanted it arrives as its own ticket with its own
// tombstone design — not as a convenience method.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// LoreEntry mirrors the lore_entry table — the L1
// layer, and the ONLY layer whose columns are ever folded into a boot context.
//
// 🔴 THERE IS NO TrustScope FIELD, AND THERE IS NO LONGER ANYTHING TO DERIVE ONE
// FROM. The class used to be computed at read time from the entry's action
// names; owner ruled the action axis away on 2026-09-05, so memoryTrustScope()
// and the whole trust/method/cognitive classification are gone rather than
// answering the same constant for every entry. See 00084_lore_format_v8.sql.
//
// 🔴 Origin IS AN L1 FIELD, NOT AN L2 ONE, and that placement is load-bearing:
// a human origin sorts ahead within its tier and is exempt from the count cap,
// so the assembler must be able to SELECT it. L2 (lore_meta) is
// defined by the assembler NOT being able to see it, which is precisely why
// origin cannot live there.
type LoreEntry struct {
	ID string

	// 🔴 標題格 ＋ 前四格，固定。第五格（相關的完整資訊）不在這個 struct 裡，
	// 它是 lore_event 的 0..N 列——見 LoreEvent / ListLoreEvents 底下。
	//
	// 🔴 這裡刻意沒有 Events []LoreEvent 欄位。LoreEntry 目前是可比較的
	// （測試用 `*got != want` 一行比完整條目），加一個 slice 就會讓那些比較
	// 編不過，而把它們改成 reflect.DeepEqual 會讓「多了一個欄位沒被比到」變成
	// 一個編譯器再也抓不到的錯。事件由呼叫者明確地讀，這是刻意的。
	//
	// 🔴 Heading 是 v8 加的獨立標題格，而它**推翻**了這個檔案原本寫的
	// 「第一格兼任標題、五格裡根本沒有名字這一格」——那句話當初就是被寫下來
	// （而不是默默做掉）等著被推翻的。Trigger 仍然是讀者找到這條的那一軸，
	// 但它不再是這條的名字：v8 對標題有 Trigger 沒有的要求（寫「發生了什麼」、
	// 不得是祈使句），兩格說的不是同一句話。
	//
	// 🔴 而 `00081_lore.sql` 裡那段話**沒有被就地更正，而且不會被更正**。它現在是
	// 假的，但那支 migration 不能改：`migration.lock` 對每支 migration 的檔案內容做
	// sha256，更重要的是**它很可能已經被一個真的資料庫套用過**（正式庫那 14 張表
	// ＝ 00081 的 12 張 ＋ 00083 的 2 張，數字逐一對上）。改它的內容，那個庫就對不上
	// 一份它跑過的 migration。
	// ⇒ **更正寫在 `00084_lore_format_v8.sql`，而 00081 沒有任何回鏈。**
	// ⚠️ 這個洞是**知情留著的**，不是漏掉的：打開 00081 讀到那段的人，會停在一句
	// 假話上，而檔案裡沒有東西會告訴他往哪裡走。留著的理由是上面那句——用一個
	// 可讀性的改善去換「正式庫對不上一支跑過的 migration」的風險，不划算。
	// **這一行就是那個補償，而它只在這裡，不在他會打開的那個檔案裡。**
	Heading    string // 標題：發生了什麼。必填，無長度上限
	Trigger    string // 什麼時候要記起來；形狀是「我要做 X」。無長度上限
	Content    string // 內容 — THIS is what enters a context
	RetireWhen string // 什麼時候不需要了（選填，自由文字，非封閉值域）
	Impact     string // 原本想達成什麼、實際變成什麼（選填，但它是主體）

	// ImpactStars 是 impact 的星等，而 0 **不是一個星等**：它是「還沒判」。
	// 既有列的預設值只能是 0，把 0 當成 1（沒弄壞任何東西）等於替它們做了一次
	// 沒有人做過的判定，而 v8 的自檢就再也查不出誰漏填。
	ImpactStars int

	// 🔴 Reviewed 與 ImpactStars 是兩個欄位而不是一個。星等是 agent 的提案，
	// 這一格是別人蓋的章；合成一欄就等於讓 agent 自己蓋自己的章。
	// ⚠️ 這一版沒有任何一條路寫得到它——見 PutLoreEntry 底下的說明。
	Reviewed bool

	Status     string // 'active' | 'superseded' | 'retired' | 'underspecified'
	Supersedes string // id of the entry this replaces; the replaced row is re-statused, never deleted
	EditableBy string // 'agent' | 'owner-gated'
	Origin     string // a subject key — `human:Seth`, `agent:Kyle`. WHO this knowledge came from.
	CreatedTS  float64
	UpdatedTS  float64
}

// 🔴 這裡曾經有一個 IsDegraded()，它被移除了，而它是被**裁定**掉的，不是被清掉的。
//
// 負責人 2026-09-03 在卡 rc-1e32c690018d 裁定「拿掉這個標記 —— 第 1 格的硬擋就夠
// 了，不要第二層」。理由是入口已經有門：第 1 格填不出來的條目**根本寫不進來**
// （loreTriggerError 擋在 PutLoreEntry 這個原始 upsert 縫上），所以再掛一個「寫進
// 來了但品質可疑」的軟標記，是在一道硬擋後面再放一道軟擋。
//
// 連帶移除的東西：LoreEntry.IsDegraded()、線上三個 `degraded` 欄位（entry detail /
// write receipt / search hit）、以及它們在 spec/openapi.json 裡的宣告。
//
// ⚠️ 不要「順手」把它加回來。它在六格時代的判準是「falsify 與 instance 皆空」，
// 五格裡那兩格都不存在；曾經有過一個暫定判準（「problem 為空」，即今天的
// `impact`），而那個暫定值正是這道裁定要收掉的東西。要有第二層品質訊號的話，
// 那是一張新卡。⚠️ v8 的 `impact_stars = 0`（還沒判）**不是**那個標記借屍還魂：
// 它說的是「沒有人判過」，不是「判過而且品質可疑」。

// 🔴 `reviewed` 在這裡，也就是說它會被 PutLoreEntry 的 upsert 覆寫。
//
// ⚠️ 射程要講準（前一版寫寬了）：**沒有任何 HTTP route 帶得出 true**，因為
// `reviewed` 不在任何請求 DTO 裡。但這一層**不是**恆為 false —— 它忠實寫入呼叫者
// 給的值，`dal_lore_t33_test.go` 就有一個呼叫者帶 `Reviewed: true` 並要求讀回
// true。「沒有路由寫得到」跟「這一層寫不進去」是兩件事，把後者寫成前者會讓下一個
// 人以為這裡自帶一道門 —— 它沒有。
//
// 🔴 所以真正的狀況是：**這一欄今天沒有守衛。** v8 要的是一個旗標，「誰能蓋、蓋了
// 要不要留紀錄」還沒有人裁定（卡在 rc-37f10fec50d1）。等有人裁定了，**那道門要有人
// 在這一層或它上面補上**，不能靠「反正沒有路由送得進來」。
const loreEntryColumns = `id, heading, trigger, content, retire_when, impact,
	impact_stars, reviewed,
	status, supersedes, editable_by, origin,
	created_ts, updated_ts`

func scanLoreEntry(row interface{ Scan(...any) error }) (LoreEntry, error) {
	var e LoreEntry
	err := row.Scan(
		&e.ID, &e.Heading, &e.Trigger, &e.Content, &e.RetireWhen, &e.Impact,
		&e.ImpactStars, &e.Reviewed,
		&e.Status, &e.Supersedes, &e.EditableBy, &e.Origin,
		&e.CreatedTS, &e.UpdatedTS,
	)
	return e, err
}

// loreTriggerError enforces 第 1 格必填 —— 空值被拒絕，不是被補一個預設值。
//
// 🔴 這一格取代了舊的 `label`，而且**沒有長度上限**。舊的 40 runes 上限跟著
// `label` 一起走了：負責人自己示範的好例子是把第一格當標題寫的
// （「【什麼時候要記起來】我要確認一個 OffiCraft 前端畫面接的是真後端，還是假
// 資料」），那一行遠超過 40 runes。留著上限就是讓示範用的寫法寫不進來。
// ⚠️ 「第一格兼任標題、因此拿掉 label 與上限」是實作判斷，不是負責人的裁定。
//
// 🔴 舊的 loreLabelError 對**空的** label 是放行的（「條目可以先寫下再命名」）。
// 這裡相反：空的 trigger 是拒絕。差別是這一格不只是名字——「什麼時候要記起來」
// 是這條條目唯一會被撈出來的那一軸，空著的話它躺在表裡，誰都撈不到，而且從外面
// 看起來跟一條寫好的條目一模一樣。
func loreTriggerError(trigger string) error {
	if strings.TrimSpace(trigger) == "" {
		return ErrLoreTriggerBlank
	}
	return nil
}

// loreHeadingError enforces v8 的標題格必填。
//
// 🔴 空值被拒絕，不是被補一個預設值——理由跟 trigger 一模一樣，而且不是同一個
// 理由的複製：trigger 空著的話沒有人撈得到這條，heading 空著的話撈得到、但它在
// 任何一份清單上跟一條寫好的條目長得一模一樣，讀的人不會知道要回來補。
//
// 🔴 檢查在這裡而不在 CHECK constraint 裡，是 migration 00084 明講的判斷：
// SQLite 的 CHECK 只會回一句「CHECK constraint failed」，說不出是哪一格空了。
//
// ⚠️ v8 對標題還有三條這一層擋不住的要求（寫「發生了什麼」、不得是祈使句、
// 標題裡的名詞與數字都要在 content 找得到）。它們是規則，不是約束，而這裡不
// 假裝成一個檢查——一個擋不住的東西被寫成 if，下一個人會以為它擋住了。
func loreHeadingError(heading string) error {
	if strings.TrimSpace(heading) == "" {
		return ErrLoreHeadingBlank
	}
	return nil
}

// loreImpactStarsError 擋 0..3 以外的值。
//
// 🔴 它擋的東西 CHECK 也擋得住，重複是刻意的：CHECK 的失敗會從 d.wdb.Exec 回來
// 成一個 driver 錯誤，上層只能把它報成 500，而送 star=7 的人是可以自己修好的。
// 這一層存在的理由就是讓那個回覆是 422 而且指名是哪一格。
//
// 🔴 0 是合法的，而且它的意思是「還沒判」，不是「最輕」。
func loreImpactStarsError(stars int) error {
	if stars < 0 || stars > 3 {
		return fmt.Errorf("%w: impact_stars=%d", ErrLoreImpactStarsRange, stars)
	}
	return nil
}

// LoreEvent 是第 5 格的一列：一條條目底下的一次事件。時／事／人／地／物。
//
// 🔴 人／地／物空著是合法的，而且「空著」必須看得出來。這三格是空字串，
// **不要**用「未知」「n/a」「unknown」之類的字串把它填滿：「查不出是誰」跟
// 「還沒有人去查」必須長得不一樣，而一旦有人塞了佔位字串進去，這兩件事就永遠
// 分不開了。這一層做得到的是不去填它，而它就是不填。
type LoreEvent struct {
	ID      int64
	EntryID string

	// HappenedTS 是**事件發生的時間**，不是這一列被寫下的時間。這兩個會差好幾
	// 天：一個 agent 今天回頭補記上週撞到的事，這裡是上週。
	HappenedTS float64

	// What 一律**主動語態**，讓「人」永遠是動作者。這一層擋不了被動語態——沒有
	// 任何欄位擋得住——所以它是規則，不是約束，而且它被寫在這裡而不是假裝成
	// 一個檢查。
	What string

	Actor  string // 人：`human:` / `agent:` 前綴。有才填
	Place  string // 地：`machine:` 前綴。語意＝這個動作在哪台機器上發生。有才填
	Object string // 物：`service:` 等前綴。語意＝被動到的是什麼。有才填
}

// loreEventError 驗證一列事件。
//
// 🔴 時與事必填；人／地／物只在**非空**時才檢查前綴。對空字串做前綴檢查就等於
// 把選填變成必填，而那會把寫入者逼去編一個「人」出來——編出來的跟查出來的長得
// 一模一樣，這正是第 5 格最不能出的錯。
//
// 🔴 前綴的值域讀 entity_type，跟 origin／subject 同一份清單。Go 裡再抄一份會在
// 第一個新型別被核准的那天悄悄跟資料庫不一致。
// ⚠️ 「人／地／物非空時必須是 `type:name` 且型別已核准」是實作判斷：規格只說了
// 這三格「有前綴」。不檢查的話前綴就只是裝飾（`Seth` 跟 `human:Seth` 都會進來）。
func (d *DAL) loreEventError(ev LoreEvent) error {
	if ev.HappenedTS <= 0 {
		return fmt.Errorf("%w: happened_ts=%v", ErrLoreEventTimeMissing, ev.HappenedTS)
	}
	if strings.TrimSpace(ev.What) == "" {
		return ErrLoreEventWhatBlank
	}
	for _, cell := range []struct{ name, value string }{
		{"actor", ev.Actor}, {"place", ev.Place}, {"object", ev.Object},
	} {
		if strings.TrimSpace(cell.value) == "" {
			continue // 空著是合法的，而且不會被填滿
		}
		prefix, name, found := strings.Cut(cell.value, ":")
		if !found || prefix == "" || strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: %s=%q is not `type:name`", ErrLoreEventKeyMalformed, cell.name, cell.value)
		}
		var one int
		err := d.rdb.QueryRow(`SELECT 1 FROM entity_type WHERE type = ?`, prefix).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s names %q", ErrLoreEventKeyUnknownType, cell.name, prefix)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ListLoreEvents 回傳一條條目的事件，**按事情發生的順序**（happened_ts，id 只是
// 同一刻的 tie-break）。不是按誰先被寫進來的順序：補記的事件排在它真正發生的
// 位置，否則第 5 格讀起來會是一份寫作順序的紀錄而不是一份事情的紀錄。
func (d *DAL) ListLoreEvents(entryID string) ([]LoreEvent, error) {
	rows, err := d.rdb.Query(`
		SELECT id, entry_id, happened_ts, what, actor, place, object
		FROM lore_event WHERE entry_id = ? ORDER BY happened_ts, id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreEvent
	for rows.Next() {
		var ev LoreEvent
		if err := rows.Scan(&ev.ID, &ev.EntryID, &ev.HappenedTS, &ev.What,
			&ev.Actor, &ev.Place, &ev.Object); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// loreOriginError validates an origin as a subject key.
//
// 🔴 ORIGIN AND SUBJECT ARE THE SAME SHAPE, `type:name`, AND THEY DRAW ON THE
// SAME LIST — the `entity_type` table, read here at run time. That table is the
// ONE copy of the type-prefix vocabulary: the subject side reaches it through
// `entity.type REFERENCES entity_type(type)`, this side reads it directly. A Go slice repeating it
// would be a second copy that drifts the moment a type is approved, and the two
// would then disagree about what is writable, silently, depending on which
// reader you asked.
//
// 🔴 FAIL-CLOSED AND BY NAME. An unrecognised prefix REFUSES the write and says
// which prefix it was; it is never quietly accepted, and never quietly rewritten
// into something that parses. A blank origin is refused too: there is no default
// author, and "unspecified" written as if it were a person would be a claim
// nobody made.
func (d *DAL) loreOriginError(origin string) error {
	if strings.TrimSpace(origin) == "" {
		return ErrLoreOriginBlank
	}
	prefix, name, found := strings.Cut(origin, ":")
	if !found || prefix == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: %q is not `type:name`", ErrLoreOriginMalformed, origin)
	}
	var one int
	err := d.rdb.QueryRow(`SELECT 1 FROM entity_type WHERE type = ?`, prefix).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrLoreOriginUnknownType, prefix)
	}
	return err
}

// ErrLoreEntryIDBlank refuses a write with no id. It is a NAMED
// error rather than a database message because the layer above has to turn it
// into a 400 that says what is wrong, and matching on a driver's wording is
// matching on something nobody promised to keep stable.
var (
	ErrLoreEntryIDBlank = errors.New("lore: the entry id is blank")
	// 🔴 第 1 格必填。舊的 ErrLoreLabelTooLong 沒了：`label` 連同它的 40 runes
	// 上限一起被移除，兩格都不設上限。
	ErrLoreTriggerBlank = errors.New(
		"lore: `trigger` is blank — 什麼時候要記起來？沒有它，這條沒有任何人撈得到")
	// 🔴 標題格必填（v8）。它跟 ErrLoreTriggerBlank 是**兩個**錯誤而不是一個，
	// 因為兩格空著的後果不一樣：trigger 空著是撈不到，heading 空著是撈得到但
	// 看起來已經寫完了。訊息要能告訴寫入者他漏的是哪一種。
	ErrLoreHeadingBlank = errors.New(
		"lore: `heading` is blank — 標題（發生了什麼）？沒有它，這條在清單上跟寫好的一模一樣")
	ErrLoreImpactStarsRange = errors.New(
		"lore: `impact_stars` is outside 0..3 — 0=還沒判、1=沒弄壞任何東西、2=弄壞的只有你動的那個、3=弄壞的包含你沒動的")
	ErrLoreEventTimeMissing = errors.New(
		"lore: an event has no `happened_ts` — 事件發生的時間（不是寫下的時間）")
	ErrLoreEventWhatBlank = errors.New(
		"lore: an event's `what` is blank — 主動語態，讓人永遠是動作者")
	ErrLoreEventKeyMalformed   = errors.New("lore: an event's 人/地/物 is not a `type:name` key")
	ErrLoreEventKeyUnknownType = errors.New("lore: an event's 人/地/物 names an unapproved type prefix")
	ErrLoreOriginBlank         = errors.New("lore: the origin is blank")
	ErrLoreOriginMalformed     = errors.New("lore: the origin is not a `type:name` subject key")
	ErrLoreOriginUnknownType   = errors.New("lore: the origin names an unapproved type prefix")
)

// PutLoreEntry creates or replaces ONE entry.
//
// 🔴 created_ts IS NOT IN THE UPDATE CLAUSE. An edit is not a birth: an entry
// that gets its wording tightened must keep the moment it first appeared,
// because that timestamp is what the staleness judgement reads. The VALUES
// expression still carries a created_ts — SQLite evaluates it before it finds
// the conflict — and it is discarded there, which is why the column is absent
// from DO UPDATE SET rather than being set to itself.
func (d *DAL) PutLoreEntry(e LoreEntry) error {
	if e.ID == "" {
		return ErrLoreEntryIDBlank
	}
	if e.Status == "" {
		e.Status = "active"
	}
	if e.EditableBy == "" {
		e.EditableBy = "agent"
	}
	// 🔴 第 1 格與標題格的必填檢查在**這裡**，不只在 CreateLoreEntry 裡。這是
	// 原始的 upsert 縫，任何繞過寫入路徑的呼叫者都會經過它；只擋在上層等於留一個
	// 側門，而從側門進來的空條目跟正門進來的長得一模一樣。
	if err := loreHeadingError(e.Heading); err != nil {
		return err
	}
	if err := loreTriggerError(e.Trigger); err != nil {
		return err
	}
	if err := loreImpactStarsError(e.ImpactStars); err != nil {
		return err
	}
	if err := d.loreOriginError(e.Origin); err != nil {
		return err
	}
	// 🔴 `reviewed` 出現在 DO UPDATE SET 裡，跟其他每一格一樣，而這是刻意的：
	// 這個函式的語意是「用這一份取代那一列」，把某一欄從取代裡挑掉會讓它在
	// 「整列覆寫」的外表下悄悄保留舊值。
	// ⚠️ 今天沒有 HTTP route 送得進 true，但**這裡不會擋**：呼叫者給 true 就寫
	// true。等有人裁定了誰能蓋章，那道門要加在有身分可看的那一層，這裡的取代語意
	// 不會替它擋。
	_, err := d.wdb.Exec(`
		INSERT INTO lore_entry (`+loreEntryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			heading = excluded.heading,
			trigger = excluded.trigger, content = excluded.content,
			retire_when = excluded.retire_when, impact = excluded.impact,
			impact_stars = excluded.impact_stars, reviewed = excluded.reviewed,
			status = excluded.status,
			supersedes = excluded.supersedes, editable_by = excluded.editable_by,
			origin = excluded.origin, updated_ts = excluded.updated_ts`,
		e.ID, e.Heading, e.Trigger, e.Content, e.RetireWhen, e.Impact,
		e.ImpactStars, e.Reviewed,
		e.Status, e.Supersedes, e.EditableBy, e.Origin,
		e.CreatedTS, e.UpdatedTS)
	return err
}

// GetLoreEntry returns one entry, or nil when no entry carries that
// id. A missing entry is not an error: "does this id exist" is a question the
// callers ask on purpose, and folding it into an error would make them parse one.
func (d *DAL) GetLoreEntry(id string) (*LoreEntry, error) {
	e, err := scanLoreEntry(d.rdb.QueryRow(
		`SELECT `+loreEntryColumns+` FROM lore_entry WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListLoreEntriesBySubject returns the entries filed against one
// entity, oldest→newest with an id tiebreak so the order is deterministic.
//
// 🔴 `retired` ROWS ARE EXCLUDED HERE, AND THAT IS THE RULING, NOT AN
// OPTIMISATION: retired means "no longer retrieved" and nothing else — the row
// stays in the table, readable by the governance surfaces that ask for it by id.
// Filtering in the query rather than at the caller is what stops a future second
// retrieval path from forgetting.
//
// ⚠️ 'superseded' AND 'underspecified' ARE STILL RETURNED. Neither has a ruling
// on retrieval, and silently dropping them here would decide it by accident. The
// ranking layer is where that belongs once somebody decides it.
func (d *DAL) ListLoreEntriesBySubject(entityID string) ([]LoreEntry, error) {
	rows, err := d.rdb.Query(`
		SELECT `+loreEntryColumns+` FROM lore_entry e
		JOIN lore_subject s ON s.entry_id = e.id
		WHERE s.entity_id = ? AND e.status <> 'retired'
		ORDER BY e.created_ts, e.id`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreEntry
	for rows.Next() {
		e, err := scanLoreEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountLoreEntriesBySubject counts what the list above would return.
//
// It exists as its own query because the wake-time subject roster needs "how
// many does this subject have" for every subject and the bodies for none of
// them; loading rows to call len() on them would put the entire memory table on
// a per-wake path. The two share their predicate word for word ON PURPOSE — a
// count that disagrees with its own list is a bug that shows up as a number
// nobody can reconcile.
func (d *DAL) CountLoreEntriesBySubject(entityID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`
		SELECT COUNT(*) FROM lore_entry e
		JOIN lore_subject s ON s.entry_id = e.id
		WHERE s.entity_id = ? AND e.status <> 'retired'`, entityID).Scan(&n)
	return n, err
}

// PutLoreSubject files an entry against one subject.
//
// 🔴 ONE ENTRY CAN CARRY MANY SUBJECTS — that is why this is a join table and
// not a column. A memory about "how the boot context is assembled" is about the
// repo AND about the member who wrote it AND about the machine it ran on, and a
// single-subject column would force a writer to pick one and lose the rest.
//
// Re-filing an existing pair is a no-op rather than an error: the pair IS the
// primary key, so "already filed" is the state the caller asked for.
func (d *DAL) PutLoreSubject(entryID, entityID string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO lore_subject (entry_id, entity_id) VALUES (?, ?)
		ON CONFLICT (entry_id, entity_id) DO NOTHING`, entryID, entityID)
	return err
}

// ListLoreSubjects returns one entry's subject entity ids, sorted so
// the order is a property of the data rather than of the insert history.
func (d *DAL) ListLoreSubjects(entryID string) ([]string, error) {
	return d.loreStrings(`
		SELECT entity_id FROM lore_subject
		WHERE entry_id = ? ORDER BY entity_id`, entryID)
}

// loreStrings is the shared single-column read used by the subject join table
// above. ⚠️ It has ONE caller since the action axis was removed; it is kept
// because inlining it would put rows.Err() back in the caller, and forgetting
// rows.Err() is the failure this helper exists to make impossible.
func (d *DAL) loreStrings(query string, args ...any) ([]string, error) {
	rows, err := d.rdb.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── the subject roster (T-33, the boot-context directory) ───────────────────

// LoreSubjectRosterRow is ONE line of the wake-time 對象目錄: an
// entity that has at least one retrievable entry filed against it, plus how many.
//
// 🔴 THERE IS NO BODY FIELD ON THIS STRUCT, AND THAT IS THE POINT. The boot
// context gets a DIRECTORY — "these subjects exist, this many entries each" —
// and never a `trigger` / `content` / `problem` cell. Carrying a body field here
// would put the entries themselves one careless `+=` away from every boot
// document in the fleet, which is a size decision nobody has made. An agent that
// wants an entry reads it deliberately.
type LoreSubjectRosterRow struct {
	EntityID  string
	Type      string
	Canonical string
	Display   string
	Entries   int
	// HumanOrigin is true when at least one of this subject's entries came from
	// a `human:` origin. It rides on the SAME grouped query rather than a second
	// pass, because the assembler needs it for every row it truncates against —
	// a per-subject follow-up query would turn one boot query into N.
	HumanOrigin bool
}

// ListLoreSubjectRoster returns the whole directory in ONE grouped
// query.
//
// 🔴 COST: THIS IS A BOOT-PATH QUERY — every agent, every wake, forever. It is
// therefore ONE statement with no per-subject follow-up: the count and the
// human-origin flag are both aggregates of the same GROUP BY, never a loop over
// CountLoreEntriesBySubject. Approved as an addition to the per-wake
// query set by the owner on reply card rc-e5a9efbed9da (2026-08-31, option [0]);
// see the COST DISCIPLINE block above resumeFloorParts in api_chat.go, where the
// tree keeps that list.
//
// 🔴 `retired` IS EXCLUDED WITH THE SAME PREDICATE THE LIST AND THE COUNT USE.
// Three readers of one rule is already one too many; they are kept word for word
// identical so a directory that disagrees with the list it indexes is impossible
// rather than merely unlikely.
//
// 🔴 THE ENTITY SIDE IS FILTERED TOO — `pending = 0` AND an EMPTY `merged_into` —
// AND BOTH ARE CORRECTNESS, NOT TIDINESS. `entity` is written by agents without
// a gate (that is deliberate: gating the write is what pushes an agent into
// forcing a near-miss key), so `pending = 1` is the whole review queue. Without
// this predicate every unreviewed name an agent invented is published into EVERY
// agent's boot document the moment one entry is filed against it — the directory
// would be asserting, to the whole fleet, that a name nobody has approved is part
// of the ontology.
//
// `merged_into` is the other half of the same story: a merged-away entity keeps
// existing (nothing in this schema deletes), so it and its merge target would
// each take a line. That is a subject counted twice under two names — and
// because the boot directory is TRUNCATED, the duplicate also eats a slot that a
// real subject needed. A wrong number is worse than no number; a wrong number
// that silently drops a real subject is worse again.
//
// 🔴 THERE IS NO PER-READER FILTER LEFT IN THIS QUERY, AND THAT IS THE RULING,
// NOT AN OVERSIGHT. The private/shared wall is gone (rc-26c1fd0c6b3c, option
// [3]: 「不要私密條目了，全部共享」), so every reader sees the same directory.
// `actorID` is therefore currently unused — it is kept in the signature because
// the callers are the two boot paths and a per-actor axis is the shape this
// query is expected to grow again; removing it would churn them both twice.
func (d *DAL) ListLoreSubjectRoster(actorID string) ([]LoreSubjectRosterRow, error) {
	rows, err := d.rdb.Query(`
		SELECT n.id, n.type, n.canonical, n.display,
		       COUNT(*),
		       MAX(CASE WHEN e.origin LIKE 'human:%' THEN 1 ELSE 0 END)
		FROM lore_entry e
		JOIN lore_subject s ON s.entry_id = e.id
		JOIN entity n ON n.id = s.entity_id
		WHERE e.status <> 'retired'
		  AND n.pending = 0
		  AND n.merged_into = ''
		GROUP BY n.id, n.type, n.canonical, n.display
		ORDER BY n.type, n.canonical, n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoreSubjectRosterRow
	for rows.Next() {
		var r LoreSubjectRosterRow
		var human int
		if err := rows.Scan(&r.EntityID, &r.Type, &r.Canonical, &r.Display, &r.Entries, &human); err != nil {
			return nil, err
		}
		r.HumanOrigin = human == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── the recall journal (T-33, 設計 v29 §3.12.x) ──────────────────────────────

// LoreRecall is ONE row of lore_recall_log: a record that lore was put in front
// of somebody. It is the raw shape of the table, not a view of it — `query` is
// whatever the writing path calls itself, `returned` is whatever that path
// decided is worth being able to count later.
type LoreRecall struct {
	ActorID   string
	Query     string
	SubjectID string
	Hop       int
	Returned  string
	CreatedTS float64

	// SessionBootTS and SessionState are the SESSION ANCHOR, stamped at the
	// moment of the recall rather than joined back to later. member.session_boot_ts
	// is a single cell that the actor's NEXT session overwrites, so a join from
	// an old row answers about the wrong session or about none — see
	// migrations/00082 for the whole argument, and loreRecallSession* for what
	// the three states mean. Nothing in this struct is optional-by-omission: the
	// zero value is `unrecorded`, which is a state and not a default.
	SessionBootTS float64
	SessionState  string
}

// InsertLoreRecall appends one row. It is APPEND-ONLY on purpose: the journal's
// value is that it says what actually happened at a moment in time, so nothing
// here updates or de-duplicates. Two boots of the same member are two events,
// not one event seen twice.
func (d *DAL) InsertLoreRecall(r LoreRecall) error {
	state := r.SessionState
	if state == "" {
		state = loreRecallSessionUnrecorded
	}
	_, err := d.wdb.Exec(`
		INSERT INTO lore_recall_log
			(actor_id, query, subject_id, hop, returned, created_ts,
			 session_boot_ts, session_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ActorID, r.Query, r.SubjectID, r.Hop, r.Returned, r.CreatedTS,
		r.SessionBootTS, state)
	return err
}
