package main

// api_bootdocs.go — the boot-context documents the owner may edit (T-791e,
// widened to the lifecycle event procedures by T-3201).
//
// 🔴 THE MECHANISM IS SPECIFIED IN docs/design/boot-documents.md — fold, cap,
// the read-only head, the variable rules, what each write face refuses and why
// reset carries no cap. That document deliberately does NOT list which
// documents exist. bootDocRegistry below is the server's list, and the wire's
// list is the BootDocKind enum in spec/openapi.json — the two are pinned to each
// other by TestBootDocRegistry_MatchesTheBootDocKindEnumInTheFrozenSpec. There
// used to be a listing ENDPOINT instead; the owner replaced it with the enum on
// 2026-08-23, because a listing cannot go stale but also cannot make anything
// fail, and a cockpit that had never heard of a new document just showed
// nothing.
//
// WHY THIS EXISTS (owner, 2026-08-13, verbatim): 「我們可以把系統互動改成可以修改
// 嗎 跟銀月的 insight 一樣是有 history / restore to default」「不用每次都改 code」
// 「啟動程序也是一樣」. Of the three segments the boot context is assembled from,
// only the middle one (使用者自訂) had an owner-editable representation; the other
// two were go:embed seeds, so correcting one sentence cost a release.
//
// 🔴 THREE DOCUMENTS, NOT ONE WITH A VARIANT FIELD — and the reason is NOT that
// the texts differ. Step 3 of the two boot sequences says OPPOSITE things (the
// claude one tells the agent to mount its own `ocagent listen`; the codex one
// forbids exactly that, because the App Server sidecar owns the listener).
// Serving the wrong one leaves the agent unable to come online, and that failure
// is SILENT: nothing that never boots is around to report it. Which is also why
// the runtime→document choice is made in exactly one place
// (bootSequenceSeedName / bootSequenceDocKey, assets.go) and never re-derived.
//
// 🔴 EDITING NEVER TOUCHES THE SEED. The stored edit is an OVERLAY row; the
// factory text stays in the binary's go:embed copy. "Restore to default" is
// therefore answered from a source no write path can reach, and it needs no
// agent, no MCP client and no member identity — the cockpit's owner token alone
// walks the whole way back (api_bootdocs_reset_t791e_test.go pins that: an owner
// token whose sub is on nobody's roster).

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// bootDocSpec addresses ONE editable boot-context block: which document-history
// series it is, which seed file backs it, and which cap judges its writes.
// Resolved once per request and passed around, so the three handlers of a block
// cannot disagree about any of the four.
type bootDocSpec struct {
	Kind     string
	Key      string
	SeedFile string
	Cap      int
	// DocName is what a refusal calls this document. It goes into
	// docCapRefusal, which is the message a caller reads when its write is
	// rejected — naming the wrong document sends the reader to edit the wrong
	// one (the defect insightWriteAuthz exists to avoid on the authz face).
	DocName string
	// Vars are the {name} variables this document may use. nil opts the kind
	// out of variable validation entirely (the three documents that shipped
	// before T-3201 do — see doc_vars.go); a non-nil empty slice means the
	// document is validated and allows none.
	Vars []string
	// Split says this document carries a read-only head above docBodyMarker.
	// Join is what the two halves are joined with when it is rendered — see
	// DocRendered on why it is per document.
	Split bool
	Join  string
	// ReadOnly marks a document the owner may READ but never edit. The owner's
	// ruling, verbatim: 「以前 global context 是固定內容我們也是會顯示 只是不給改」
	// — so these still have a seed, still fold, still reach the cockpit, and
	// only the write faces refuse.
	ReadOnly bool
}

// bootDocReg is ONE row per editable boot-context document kind: everything
// bootDocSpecFor needs to answer for it, in one place.
//
// 🔴 A TABLE, NOT A SWITCH, AND THE REASON IS MEASURED. Adding the offboard
// document (bfe95d1f) meant editing eight hand-maintained switches and lists
// scattered over six files, and four of them have NO gate at all — a missing
// arm compiles, serves 200, and shows the wrong text or none. Kinds resolved
// from one slice cannot be in it for the specFor face and absent from the
// history face, which is the class of defect that produced this comment.
type bootDocReg struct {
	Kind string
	// Keys are the document keys this kind serves, in the order a refusal
	// should name them. Every kind but boot_sequence has exactly one.
	Keys []string
	// SeedFor answers the seed filename for one of Keys. A func rather than a
	// field because boot_sequence's two keys have two different seeds, and the
	// two contradict each other in step 3 — serving the wrong one is a silent
	// failure to boot (see bootSequenceSeedName).
	SeedFor func(key string) string
	DocName func(key string) string
	Cap     func(s *apiServer) int
	// Vars are the {name} variables this kind may use. On a Split kind they
	// are the HEAD's variables: the body declares none, always. nil still
	// means "not validated at all" (doc_vars.go) and is independent of Split —
	// system_interaction's body carries JSON braces this syntax cannot tell
	// from a variable, while its head is immutable all the same.
	Vars []string
	// Split / Join — see bootDocSpec and DocRendered.
	Split    bool
	Join     string
	ReadOnly bool
}

// taskEventDocVars name the same facts today's Go string literals interpolate,
// spelled the way the seed files spell them. They are declared per kind rather
// than shared so a name that only makes sense for one event cannot silently be
// used by another.
var bootDocRegistry = []bootDocReg{{
	Kind:    docKindSystemInteraction,
	Keys:    []string{systemInteractionDocKey},
	SeedFor: func(string) string { return systemInteractionSeedMD },
	DocName: func(string) string { return "system interaction block" },
	Cap:     func(s *apiServer) int { return s.systemInteractionCap() },
	// 🔴 NO READ-ONLY HEAD AT ALL (T-6f44). The head used to be this document's
	// OWN title line, promoted; the promotion bought nothing, because the title
	// is a line the owner may as well own — nothing in the body quotes it, the
	// server interpolates nothing into it, and it declared zero variables. The
	// title line simply moved back down into the body, so the RENDERED bytes are
	// byte-for-byte what they were (the old join was "\n\n", which is exactly the
	// blank line that now sits between the title and the rest of the seed).
	//
	// 🔴 SPLIT AND THE SEED'S MARKER MUST GO TOGETHER, and the guard that says so
	// is TestBootDocRegistry_ASeedCarriesAMarkerExactlyWhenItsKindIsSplit. Doing
	// only one of the two is the silent failure: dropping the marker while the
	// kind still declares Split makes every write a 500 and every notice ""; and
	// dropping Split while the seed keeps its marker hands the owner an editable
	// body that CONTAINS the marker and the head — i.e. it turns the half nobody
	// may edit into a half anybody may.
	//
	// Vars stays nil — the body quotes JSON (`{"id": "<attachment id>"}`) that
	// the {name} syntax cannot tell from a variable.
}, {
	Kind: docKindBootSequence,
	Keys: []string{bootSequenceKeyClaude, bootSequenceKeyCodex},
	// bootSequenceSeedName, not a literal map: it is documented as the one
	// place in the tree that decides which runtime gets which sequence, and it
	// holds that title only as long as nobody writes a second one beside it.
	SeedFor: bootSequenceSeedName,
	DocName: func(key string) string { return "boot steps (" + key + ")" },
	Cap:     func(s *apiServer) int { return s.bootSequenceCap() },
	// Same as system_interaction (T-6f44): the promoted title line went back
	// into the body on BOTH runtime seeds, and the read-only head is gone. The
	// two titles still differ per runtime — they are just body text now, which
	// is where a line no code composes belongs.
}, {
	// 〈停止〉 (T-c9c0). A SINGLETON: being collected is the same procedure for
	// every agent and every runtime, so there is deliberately no runtime axis
	// here to get wrong.
	Kind:    docKindOffboard,
	Keys:    []string{offboardDocKey},
	SeedFor: func(string) string { return offboardSeedMD },
	DocName: func(string) string { return "Stop document" },
	Cap:     func(s *apiServer) int { return s.offboardCap() },
	// EMPTY, NOT nil. nil means "not validated at all" (doc_vars.go); this kind
	// declared variables before T-6f44 and must keep being validated — it just
	// allows none now. Letting it fall back to nil would silently retire the
	// write-face refusal that stops the owner typing a slot nothing fills.
	Vars: []string{},
	// 🔴 THE READ-ONLY HEAD IS GONE, AND SO IS {where} (T-6f44, owner's
	// decision 4: 「{where} 不中文化，直接砍掉」). What the head said was 「你在
	// 59%」 — a usage percentage that has nothing to do with how to close out —
	// stapled to an instruction the body already gives. Removing the variable
	// left a head with nothing in it that the body could not say itself, so the
	// head went too: this is the FIRST of the ten documents with no read-only
	// half, and the whole document is now the owner's.
	//
	// Three things had to happen in one commit (see system_interaction's row):
	// the seed lost the marker, this row lost Split, and the seed-vs-Split guard
	// became a biconditional. Doing one of the three would have quietly made the
	// read-only half editable.
	//
	// 🔴 {closer} IS GONE AND report_stopped IS SPELLED OUT. owner, verbatim
	// (c-5b3d8f192a0b): 「我預期是 report_stopped，因為是 server 控制他上下線」,
	// and again (rc-5d044f0c1266): 「下線程序為什麼要看到 restart_self，這個已經
	// 不在下線程序要被呼叫了」. restart_self is a REQUEST an agent makes when it
	// was told to restart itself after finishing something, not a close-out
	// verb; it now lives in 系統互動's tool notes. The Go builder that used to
	// interpolate a per-member closer beside this document is deleted — this
	// head is the only place the sentence exists, on both arms.
}, {
	// 加速停止 (T-3201). Shares the offboard cap on purpose, the same way the
	// two boot sequences share one: it is the same procedure under a shorter
	// clock, and a second ceiling would be a second number to keep in step
	// without a second thing to say about it.
	Kind:    docKindAcceleratedStop,
	Keys:    []string{acceleratedStopDocKey},
	SeedFor: func(string) string { return acceleratedStopSeedMD },
	DocName: func(string) string { return "accelerated stop sequence" },
	Cap:     func(s *apiServer) int { return s.offboardCap() },
	// 🔴 THE ONLY DOCUMENT WHOSE HEAD SURVIVED IN FULL FORCE, DOWN TO ONE
	// SENTENCE (T-6f44): 「你的結束時刻是 {deadline}。」. {where} went the way of
	// 〈停止〉's, and the English 「…offboard now: … Your deadline is …」 went with
	// it. The deadline itself cannot leave: it differs on every send, only the
	// server knows it, and the whole body's behaviour is conditioned on it.
	//
	// 🔴 THE BODY NO LONGER SNIFFS THIS SENTENCE, AND THAT CHANGE LANDED FIRST
	// (owner's decision 5). §1 of both stop procedures used to tell the agent to
	// look for the literal `Your deadline is` and infer hard-vs-soft from it —
	// a string match against ANOTHER document's first line, which the owner may
	// edit. Each document now states outright which one it is. Rewriting this
	// head before that would have disarmed the discrimination with every test
	// still green.
	Split: true,
	Join:  "\n",
	Vars:  []string{"deadline"},
}, {
	Kind:    docKindTaskCloseout,
	Keys:    []string{taskCloseoutDocKey},
	SeedFor: func(string) string { return taskCloseoutSeedMD },
	DocName: func(string) string { return "task close-out procedure" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// 🔴 THE HEAD IS THE TICKET NUMBER AND NOTHING ELSE (T-6f44, owner's
	// decision 3: 「最低限度就是 task id」). {status}, {type_key} and
	// {manual_label} are all facts the agent can READ OFF THE TICKET, and the
	// ticket number is the one fact it cannot: the notice arrives as a single
	// sentence with no context, and an agent closing out two tickets at once has
	// nothing else to tell them apart with.
	//
	// 🔴 THE BODY HAD TO CHANGE IN THE SAME COMMIT, and this is the silent break
	// the ruling names explicitly. The body used to say 「type_key 就用上面那一行
	// 給的值」 and 「送回**上面指名的那本**任務手冊」 — both point at a line that no
	// longer exists once the names leave the head. They now say 「用上一步讀到的
	// 值」 and 「**那本**」, and the body opens by telling the agent to get_task the
	// ticket and read type_key off it. Cutting the variables alone would leave a
	// document that reads perfectly and instructs the agent to look at nothing.
	//
	// 🔴 {closed_by} JOINED THE HEAD IN T-91, AND IT IS NOT A REVERSAL OF THAT
	// RULING — it is the same test applied to a different fact. Decision 3 cut
	// {status}, {type_key} and {manual_label} because every one of them is
	// READ OFF THE TICKET, so carrying them in the head was a second copy that
	// could go stale. WHO CLOSED THE TASK is not on the ticket: there is no such
	// column, and get_task cannot answer it. The head is still "the facts the
	// notice is the only source of", which is now two of them rather than one.
	//
	// It matters because the executor of a task it did NOT close is exactly the
	// reader this notice reaches (T-91 also removed the two gates that used to
	// silence the duplicate and ad-hoc cases), and "my last step report finished
	// it" and "somebody terminated it under me" are opposite situations that the
	// old single sentence rendered identically.
	Split: true,
	Join:  "\n",
	Vars:  []string{"task_no", "closed_by"},
}, {
	Kind:    docKindTaskReassignPredecessor,
	Keys:    []string{taskReassignPredecessorDocKey},
	SeedFor: func(string) string { return taskReassignPredecessorSeedMD },
	DocName: func(string) string { return "task reassignment document (to the predecessor)" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// The cleanest cut of the ten: one sentence of fact, then three of
	// instruction, in that order, inside one paragraph — hence Join "".
	// 🔴 THE SUCCESSOR IS NOT NAMED — ONE VARIABLE, NOT TWO (owner, 2026-08-24,
	// verbatim: 「如果完全不提到接手人是誰呢」「讓他自己去查」「不管是不是
	// outsource」). This SUPERSEDES decision 1 of the same day for THIS document
	// (decision 1 stands for its sibling 〈給接手人〉, which still carries the
	// predecessor's name and id).
	//
	// The criterion is the one he applied across this whole pass: does the
	// READER need the fact to do what the body asks? 〈擋著你手上任務的票解開了〉
	// dropped the blocker's identity on it; 〈任務結案〉 kept only the ticket. Here
	// the body tells the predecessor to write its handover ONTO THE TASK and to
	// treat talking to the successor as nice-to-have — it never asks it to dial
	// anyone, so an id it would not dial is a fact it does not need.
	//
	// 🔴 AND NAMING THE SUCCESSOR WAS WORSE THAN UNNECESSARY: it was the source
	// of a FABRICATED name. An outsource successor is minted LATER by the
	// scheduler, so at reassign time there was nobody to name, and the slot was
	// filled with a hardcoded Chinese status label ("外包（待排程指派）") sitting in
	// the grammatical position of a person. With the name gone the placeholder
	// has nothing left to fill, and the whole branch goes with it.
	Split: true,
	Join:  "",
	Vars:  []string{"task_no"},
}, {
	Kind:    docKindTaskTakeoverWithPredecessor,
	Keys:    []string{taskTakeoverWithPredecessorDocKey},
	SeedFor: func(string) string { return taskTakeoverWithPredecessorSeedMD },
	DocName: func(string) string { return "task reassignment document (to the successor)" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// 🔴 SPLIT ONCE {note} WAS DROPPED (owner, rc-0c36d8739b8f: 「拿掉 —— 交接備註
	// 只留在任務上」). 「交接備註：{note}」 was appended AFTER the instructions, so
	// the facts were not a prefix — and the note it carried was a SECOND COPY:
	// the reassign writes HandoverNote/TS/By onto the task itself and wire.go
	// puts it in the DTO, so the successor reads it with get_task. Dropping the
	// copy leaves one sentence of fact and one of instruction, in that order.
	//
	// Join "" — the two halves run together inside ONE paragraph, exactly like
	// 轉派程序（前任）: today's notice reads 「…（id `x`）。請先跟他確認交接完成…」.
	// 🔴 FOUR NAMES DOWN TO TWO (T-6f44). {title} is on the ticket the number
	// already names, so it goes; {predecessor_label} and {old_executor_id} merge
	// into ONE slot filled 「銀月（mira）」 (owner's decision 1). Neither half of
	// that pair could be dropped: the body's first instruction is to post_chat
	// the predecessor, which needs the id, and a sentence carrying only an id
	// does not tell a reader who it is talking about.
	Split: true,
	Join:  "",
	Vars:  []string{"task_no", "predecessor"},
}, {
	// 🔴 NO LONGER READ-ONLY (T-6f44, owner's decision 2). The reason it was
	// locked was recorded as 「以前 global context 是固定內容 我們也是會顯示 只是
	// 不給改」 — precedent, not a property of this text. 〈新任務〉 and 〈給接手人〉
	// are the two halves of one event, and the owner could edit one and not the
	// other with nothing to say why. The half that SHOULD be locked already is:
	// the read-only head, on all ten.
	//
	// ⚠️ read_only lives in bin/tests/fixtures/boot-doc-registry.tsv as well —
	// the cockpit reads its own copy, and the mirror test on both sides is what
	// makes a one-sided change red instead of invisible.
	Kind:    docKindTaskTakeoverFresh,
	Keys:    []string{taskTakeoverFreshDocKey},
	SeedFor: func(string) string { return taskTakeoverFreshSeedMD },
	DocName: func(string) string { return "new task document" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// Split on the same ruling as its sibling above, and joined the same way:
	// one sentence of fact, then the instructions, inside one paragraph.
	Split: true,
	Join:  "",
	// {title} dropped: the number names the ticket, and the body's first
	// instruction is 「請先讀任務內容」 — it is going to read the title anyway.
	Vars: []string{"task_no"},
}, {
	Kind:    docKindTaskUnblocked,
	Keys:    []string{taskUnblockedDocKey},
	SeedFor: func(string) string { return taskUnblockedSeedMD },
	DocName: func(string) string { return "dependency-released notice" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	Split:   true,
	// A blank line, not "", because the body is a bullet list — the one
	// document of the ten whose body is not today's sentence. owner approved
	// the rewrite on 2026-08-22 (rc-8c0045ef7c38): the old single sentence
	// 「請 get_task 讀內容、submit_plan 規劃步驟後開始執行」 hardcodes the
	// assumption that a blocked ticket has not started, and there is live
	// evidence of it saying so to a ticket already in progress.
	Join: "\n\n",
	// 🔴 ONLY THE BLOCKED TICKET'S NUMBER SURVIVES (T-6f44). What the agent must
	// act on is the ticket that was RELEASED — its own. Which ticket was
	// blocking, what it was called and how it ended change nothing about what to
	// do next, and the dependency is on the ticket for anyone who wants it.
	//
	// Two defects died with them, both visible in the old sentence: {blocker_
	// status} rendered an UNTRANSLATED wire code into Chinese prose (「已經done
	// 了」、「已經terminated了」), and the sentence used a HALFWIDTH comma — the
	// only one in the ten.
	//
	// ⚠️ Not read-only any more — see 〈新任務〉's row above and the shared table.
	Vars: []string{"blocked_task_no"},
}}

// bootDocRegFor finds the row for a kind.
func bootDocRegFor(kind string) (bootDocReg, bool) {
	for _, reg := range bootDocRegistry {
		if reg.Kind == kind {
			return reg, true
		}
	}
	return bootDocReg{}, false
}

func (reg bootDocReg) serves(key string) bool {
	for _, k := range reg.Keys {
		if k == key {
			return true
		}
	}
	return false
}

func (s *apiServer) systemInteractionSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindSystemInteraction, systemInteractionDocKey)
}

func (s *apiServer) offboardSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindOffboard, offboardDocKey)
}

func (s *apiServer) acceleratedStopSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindAcceleratedStop, acceleratedStopDocKey)
}

// mustBootDocSpec resolves a pair this binary is built with. It panics rather
// than returning ok=false because every caller passes a constant pair from the
// registry itself: a false here would mean the binary shipped with a kind whose
// own accessor cannot address it, and degrading that to an empty spec would
// hand a caller a document with no seed, no cap and no name.
func (s *apiServer) mustBootDocSpec(kind, key string) bootDocSpec {
	spec, ok := s.bootDocSpecFor(kind, key)
	if !ok {
		panic("boot document " + kind + "/" + key + " is not in bootDocRegistry")
	}
	return spec
}

// bootSequenceSpecFor resolves the boot-sequence document for a runtime key as
// it arrives on the URL. ok=false means the key names no document this server
// serves — the caller answers 404 rather than quietly falling back to claude,
// because falling back is precisely how a codex reader ends up holding the
// sequence that keeps it from booting.
func (s *apiServer) bootSequenceSpecFor(runtimeKey string) (bootDocSpec, bool) {
	return s.bootDocSpecFor(docKindBootSequence, runtimeKey)
}

// bootDocSpecFor resolves ANY (kind, key) pair naming an editable boot-context
// block — the form the document-history faces address documents in. ok=false
// means the pair names none of them.
func (s *apiServer) bootDocSpecFor(kind, key string) (bootDocSpec, bool) {
	reg, ok := bootDocRegFor(kind)
	if !ok || !reg.serves(key) {
		return bootDocSpec{}, false
	}
	return bootDocSpec{
		Kind:     reg.Kind,
		Key:      key,
		SeedFile: reg.SeedFor(key),
		Cap:      reg.Cap(s),
		DocName:  reg.DocName(key),
		Vars:     reg.Vars,
		Split:    reg.Split,
		Join:     reg.Join,
		ReadOnly: reg.ReadOnly,
	}, true
}

// bootDocHistoryKeyKnown is bootDocSpecFor's server-free half: does this
// (kind, key) name one of these documents at all? The document-history faces
// ask this BEFORE they list or restore, so an address this server does not
// serve is refused rather than answered with an empty version list — "you used
// the wrong key" and "this document has no versions yet" must not look the same.
func bootDocHistoryKeyKnown(kind, key string) bool {
	reg, ok := bootDocRegFor(kind)
	return ok && reg.serves(key)
}

// unknownBootDocKeyMsg names the keys that DO exist for this kind, for the same
// reason writeUnknownBootSequence does: a caller holding a typo needs to be able
// to tell it from a document that is simply empty.
//
// 🔴 IT READS THE REGISTRY, NOT A SECOND LIST. It used to be a switch that
// answered the system-interaction key for every kind that was not
// boot_sequence, so a fourth kind was described by a key that did not belong to
// it; the switch was then taught each kind by hand, which is the same defect
// one edit later. A kind nobody registered says so instead of naming a key.
func unknownBootDocKeyMsg(kind, key string) string {
	reg, ok := bootDocRegFor(kind)
	if !ok {
		return "document history kind '" + kind + "' names no editable document on this server"
	}
	quoted := make([]string, 0, len(reg.Keys))
	for _, k := range reg.Keys {
		quoted = append(quoted, "'"+k+"'")
	}
	return "document history key '" + key + "' does not name a " + kind +
		" document — the key is " + strings.Join(quoted, " or ")
}

// 🔴 DocName IS USER-FACING PROSE, NOT AN IDENTIFIER (T-6f44). It is what a
// REFUSAL calls the document — 「the Stop document exceeds…」 — so when the owner
// renamed these on 2026-08-24 this set had to move with the cockpit's, or the
// error would name a document the settings page no longer has. The five that
// were renamed here follow the cockpit's own names (i18n `*Name`); the four that
// keep their wording (system interaction, accelerated stop, task close-out,
// dependency-released notice) do so because the owner did not rename them.
//
// ⚠️ Two other copies of this mapping exist and are NOT identifiers either:
// frontend/src/api/mock.ts (the cockpit's stand-in server) and the OpenAPI
// descriptions. The mock is kept in step in the same commit; the spec text is
// the wider question the owner is deciding separately. Nothing enforces any of
// this — the wire identity is `kind`/`key`, which this ticket does not touch.

// foldBootDocDTO folds one block: overlay ⊕ the embedded seed.
func (s *apiServer) foldBootDocDTO(spec bootDocSpec) (*bootDocDTO, error) {
	overlay, err := s.dal.GetBootDocument(spec.Kind, spec.Key)
	if err != nil {
		return nil, err
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldBootDocument(overlay, seedMD, hasSeed)
	// 🔴 THE READ SPLITS WHAT THE WRITE CANNOT JOIN. Owner's ruling: 「讀取有
	// 這個 key，回寫沒有這個 key」 — so the read face names the read-only half
	// out loud (read_only_head) and hands back the editable half under the SAME
	// name the write face takes (body). A caller that POSTs the `body` it just
	// GET-ed writes the document back unchanged, which is the property
	// TestBootDoc_TheBodyItReadsBackIsTheBodyItTakes pins; there is no
	// separator, no marker and no join rule for a client to know about.
	//
	// `text` stays, and stays the WHOLE stored document: it is what the version
	// history stores and diffs against (bootDocHistorySnapshot), and what
	// size_chars counts against cap_chars. Three keys describing one document is
	// redundancy the READ side is allowed — the ruling is about the write.
	head := ""
	if spec.Split {
		if h, _, split := DocSplitHeadBody(text); split {
			head = h
		}
	}
	return &bootDocDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      spec.Cap,
		Kind:          spec.Kind,
		Key:           spec.Key,
		Text:          text,
		ReadOnlyHead:  head,
		Body:          bootDocBodyOf(spec, text),
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
		HasSeed:       hasSeed,
		ReadOnly:      spec.ReadOnly,
	}, nil
}

// systemInteractionText / bootSequenceText are what the BOOT FOLDS read
// (buildBootContext for staff, worker_sharedcore.go for outsource). They are the
// reason this ticket is worth anything: before T-791e both were bare
// readSeedFile calls, so an edit could exist in the database and still never
// reach an agent.
//
// The runtime→document choice goes through bootSequenceDocKey, which reads the
// answer of bootSequenceSeedName instead of testing the runtime again — one
// decision point for staff and outsource alike.
// 🔴 THE READ SITES TAKE THE RENDERED TEXT, THE COCKPIT TAKES THE STORED ONE
// (T-3201). foldBootDocDTO still answers the whole document — marker line and
// all — because the owner has to SEE the half he cannot edit. A reader must not
// see the marker, so every boot fold goes through DocRendered with this kind's
// own join. An installation whose seed carries no marker renders byte for byte
// what it rendered before.
func (s *apiServer) systemInteractionText() (string, error) {
	spec := s.systemInteractionSpec()
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		return "", err
	}
	return DocRendered(dto.Text, spec.Join), nil
}

// winddownNoticeText is the WHOLE notice a member being wound down receives:
// the document for this kind, its {variables} filled from the live facts, and
// its two halves joined the way this kind joins them (T-3201). It replaces the
// Go string concatenation that used to build the first line beside a document
// that already carried it — the complaint that opened this ticket was that the
// owner went looking for the words an agent is sent and could not find them.
//
// 🔴 WHICH DOCUMENT IS THE WHOLE OF THE SOFT/HARD DISTINCTION. A soft wind-down
// reads 〈停止〉; the final call reads 〈加速停止〉, and that is the only document
// whose head carries a {deadline} slot.
//
// ⚠️ WHY THIS ARGUMENT WAS REWRITTEN (T-6f44). It used to run: the soft document
// quotes no instant, and 〈停止〉 §1 told an agent to read "no instant" as soft.
// That inference is GONE — decision 5 deleted the sniffing rule, and each
// document now says outright which one it is. The cost of handing the hard arm
// the soft document is therefore no longer a missing hint, it is a FLAT LIE:
// 〈停止〉 §1 reads 「你讀到的是這一份，就代表**沒有人在對你倒數**：收尾照自己的
// 節奏做完」 — an agent under a running clock would be told, in words, that
// nobody is counting, and would let its sub-agents finish inside a window that
// is already closing. The document is more explicit than it was, so sending the
// wrong one is worse than it was.
//
// It answers "" on ANY fault — an unreadable document, an undeclared name, a
// declared name nothing filled — and every caller omits the notice rather than
// sending it. That is the one direction RenderDocVars exists to enforce: a
// sentence reaching an agent with `{deadline}` still in it is worse than no
// sentence, because it reads as a real instant that cannot be parsed.
func (s *apiServer) winddownNoticeText(kind string, deadline float64) string {
	spec := s.offboardSpec()
	// 🔴 {where} IS GONE FROM BOTH DOCUMENTS AND FROM THIS SIGNATURE (T-6f44,
	// owner's decision 4, verbatim: 「這個資訊跟 agent 要怎麼下線完全沒關係吧」).
	// It was measured, not reasoned: an agent that received 「context 55% (your
	// limits: 55% / 65%)」 read it and did nothing differently.
	//
	// The parameter went WITH the slot rather than lingering as an ignored
	// argument, because the arguments were the last thing keeping the code that
	// COMPOSED that clause alive — a gauge read and two branches of English
	// formatting that nothing downstream could read. Left in place they would
	// have kept passing their own tests while having no observable effect,
	// which is the shape this ticket exists to remove.
	values := map[string]string{}
	if kind == offboardKindFinal {
		// Unreachable: offboardKindOf answers final only on a clocked arm, and
		// winddownDeadlineOf is positive on exactly those arms
		// (TestWindDownKind_TheClockAndTheSentenceCannotDisagree pins the online
		// arm, TestOffboardKindOf_AFinalCallAlwaysHasAClock the offline one).
		// Refusing rather than formatting epoch 0 keeps a 1970 deadline out of
		// the one sentence an agent acts on if they ever come apart.
		if deadline <= 0 {
			return ""
		}
		spec = s.acceleratedStopSpec()
		// .UTC() is not cosmetic: the reader is an AGENT that need not run on
		// this host, and the client de-dupes on the whole sentence verbatim, so
		// one epoch has to render to one constant string.
		values["deadline"] = time.Unix(int64(deadline), 0).UTC().Format(time.RFC3339)
	}
	return s.eventNoticeText(spec, values)
}

// taskEventBodyText is the EDITABLE HALF of a task-event document, with no
// read-only head — for a caller that wants the document's INSTRUCTIONS without
// its statement of fact.
//
// 🔴 IT EXISTS BECAUSE THE HEAD MAKES A CLAIM AND THE BODY DOES NOT. 〈任務結案〉
// opens 「任務 {task_no} 已結束。」, which is true when a task closes and FALSE on
// the other path that needs the same instructions: an outsource worker being
// wound down mid-task, whose ticket is still open. Sending it the whole document
// would tell it its task had ended in order to remind it to write its learnings
// back — and the false half is the half it would act on.
//
// The body stands alone by construction: it opens by telling the agent to read
// the ticket and take type_key off it, so it needs neither the head's sentence
// nor any variable. This is deliberately NOT a second copy of those words
// (which is the defect T-6f44 removed) — it is the SAME document, minus a claim
// this caller cannot make.
func (s *apiServer) taskEventBodyText(kind string) string {
	spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
	dto, err := s.foldBootDocDTO(spec)
	if err != nil || dto == nil {
		return ""
	}
	if _, _, split := DocSplitHeadBody(dto.Text); spec.Split && !split {
		return ""
	}
	return strings.TrimSpace(bootDocBodyOf(spec, dto.Text))
}

// taskNoticeText is the WHOLE chat notice one TASK event posts to the executor
// it concerns: the document for this kind, its {variables} filled from the live
// task facts, and its two halves joined the way this kind joins them (T-3201).
// The wind-down pair took this road first; these documents differ only in
// carrying more names and in being addressed by kind rather than by an arm.
//
// 🔴 THE KIND IS THE WHOLE OF WHICH WORDS GO OUT, so passing the wrong constant
// sends an agent another event's instructions. It cannot be caught downstream:
// both documents open with a [任務編號] and read as a coherent notice, so
// a predecessor told to hand over would simply be told instead that something
// stopped blocking it. The send-site tests compare the posted body against the
// whole expected text for that reason — a keyword probe passes on either.
//
// It answers "" on ANY fault, and every caller posts nothing rather than
// posting that: a name nothing filled would otherwise reach an agent as
// `{blocker_title}`, which reads like a real title and names no task.
//
// TrimSpace because a document is a FILE and ends with a newline, while a chat
// row is one message — the same trim buildBootContext does to every block it
// staples. The wind-down notices do not take it: theirs is an SSE field whose
// bytes the client de-dupes against, and trimming it now would change what
// every agent already receives.
func (s *apiServer) taskNoticeText(kind string, values map[string]string) string {
	return strings.TrimSpace(
		s.eventNoticeText(s.mustBootDocSpec(kind, bootDocSingletonKey), values))
}

// eventNoticeText is the one road from a document to the bytes an agent reads:
// fold the overlay over the seed, fill the names this kind declares, join the
// halves. "" on any fault — see the two callers above for why every one of them
// omits the notice instead of degrading to a template.
//
// 🔴 A SPLIT KIND WHOSE STORED TEXT HAS NO MARKER IS A FAULT HERE, and refusing
// it is the whole reason this check exists rather than living in DocRendered.
// DocRendered takes a text and a join and cannot know what the kind DECLARED, so
// its no-marker branch returns the text unchanged — correct for the boot folds,
// which staple whole documents together and whose readers lose nothing when a
// document has no head. A NOTICE is the opposite: its read-only head IS the
// sentence, so the same lenient branch ships an agent the instructions with the
// facts sliced off — and it ships them NON-EMPTY, which is worse than "" for two
// different reasons depending on the arm.
//
// ONE arm has a net: the member delta drives cli/ocagent's offboardFallback,
// which arms on an ABSENT notice, so a fragment disarms it — no correct notice
// and no warning either. The other three (the context-high band and the two task
// chat rows) have no equivalent, so there a fragment and "" are equally silent
// and the reason to refuse is simply that the fragment MISLEADS: 轉派程序's body
// says 「請停止推進，改為去跟接手人做交接」 while WHICH task lives only in the
// head, so a predecessor holding several would not know which one to stop.
//
// The 〈加速停止〉 arm is where that costs the most and it is not hypothetical:
// the head is the only place the deadline appears, so a headless notice quotes
// no instant while winddownDeadlineOf is positive and reconcile is already
// counting.
//
// ⚠️ REWRITTEN WITH THE DOCUMENT (T-6f44). The old reason was that 〈停止〉 §1
// told an agent to read "no instant" as soft — that rule is gone (decision 5).
// What replaced it is WORSE for a headless notice, not better: 〈加速停止〉 §1 now
// reads 「你讀到的是這一份，就代表**你在倒數中**：上面那一行的結束時刻就是死線」,
// and 上面那一行 IS the head. Strip it and the body points at a line that is not
// there — the agent is told it is counting down and then told to look at a
// deadline nothing shows it.
//
// 🔴 THE REACHABLE WAY IN IS AN OVERLAY WRITTEN BEFORE THE MARKER EXISTED.
// docBodyMarker arrived with the split and NO migration rewrote the rows that
// were already there, so any installation that had edited one of these documents
// before that release is holding exactly this shape. Nothing can PRODUCE one any
// more: the write face has no field for a head at all and joins the shipped one
// on itself (replaceBootDoc), so a headless document is not something a caller
// is refused, it is something a caller cannot express.
//
// ⚠️ THAT SENTENCE USED TO STOP AT THE WRITE FACE AND WAS WRONG BY OMISSION —
// it read as "nobody can put one back", and one door could. RESTORE IS NOT A
// GENERATOR, IT IS A RE-ARMER: it never invents this shape, but until T-3201 it
// could take a pre-marker version out of the version history, put it straight
// back on the live row without passing any content gate, and write that as a new
// revision — arming a hazard nothing else in the tree could still create. It now
// goes through the same join as every other write (restoreDocumentHistory), so
// what it re-arms comes back WITH the shipped head on it. What is left is only
// the rows already stored, which no write path visits until something writes
// them.
func (s *apiServer) eventNoticeText(spec bootDocSpec, values map[string]string) string {
	dto, err := s.foldBootDocDTO(spec)
	if err != nil || dto == nil {
		return ""
	}
	if _, _, split := DocSplitHeadBody(dto.Text); spec.Split && !split {
		return ""
	}
	text, err := RenderDocVars(dto.Text, spec.Vars, values)
	if err != nil {
		return ""
	}
	return DocRendered(text, spec.Join)
}

func (s *apiServer) bootSequenceText(runtime string) (string, error) {
	spec, ok := s.bootSequenceSpecFor(bootSequenceDocKey(runtime))
	if !ok {
		// Unreachable by construction: bootSequenceDocKey only ever answers a
		// key bootSequenceSeedForKey accepts. Fail closed rather than serve a
		// blank boot sequence, which would look like a successful boot.
		return "", errNotFound
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		return "", err
	}
	return DocRendered(dto.Text, spec.Join), nil
}

// bootDocSnapshotIn is what SaveWithDocumentHistory calls from INSIDE the write
// transaction — the same posture insightSnapshotIn takes, and for the same
// reason: the retained revision must be the state THIS write replaced, not a
// value the handler folded earlier, or two racing writers retain one common
// ancestor and the version written between them becomes unrecoverable.
func bootDocSnapshotIn(kind, key string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getBootDocumentOn(q, kind, key)
		if err != nil {
			return "", err
		}
		return bootDocHistorySnapshot(current)
	}
}

func bootDocHistorySnapshot(current *BootDocument) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// publishBootDoc fans the change on the `global_context` topic.
//
// Deliberately NOT a new topic: the closed vocabulary is enforced at the publish
// seam and a topic outside it is dropped SILENTLY (see sseTopics), so adding one
// means teaching every consumer at once or fanning nothing at all. All three
// blocks are one surface to a reader — the cockpit's 全域情境 pane renders them
// together — so the frame that already means "that surface moved, re-read it" is
// the honest one to send. Same key as the 使用者自訂 writes for the same reason.
func (s *apiServer) publishBootDoc(r *http.Request) {
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil,
		audienceOwnerOnly(), requestTrigger(r))
}

// writeBootDoc is the one write path shared by replace and reset, and the one
// place the no-op rule lives.
//
// 🔴 A WRITE THAT CHANGES NOTHING RETAINS NOTHING (owner's ruling for these three
// documents). These blocks are now editable from a text box, so idle saves are
// expected — and every idle save that retained a revision would push the version
// the owner actually wants ("the one from before I broke it") one step closer to
// falling off the end of the list. The comparison is on the FOLDED text, so
// saving the seed's own bytes back over an untouched document is a no-op too.
// role_def and the task manual already work this way (roleDefHistoryStreams /
// taskManualHistoryStreams); global_context deliberately does NOT, and that is a
// known gap on that document, not a precedent to copy.
//
// Returns whether anything was written.
func (s *apiServer) writeBootDoc(r *http.Request, spec bootDocSpec, current *bootDocDTO, next BootDocument, nextText string) (bool, error) {
	// "Nothing changed" is BOTH halves: the text a reader would get, and whether
	// the document reads as the shipped default afterwards (next.Tombstoned) as
	// it does now (current.IsDefault). Comparing only the text would swallow the
	// one gesture that changes nothing visible but everything about the next
	// reset — adopting the seed's own bytes as an edit — and comparing only the
	// flag would let a genuine rewrite pass as a no-op.
	if current.Text == nextText && current.IsDefault == next.Tombstoned {
		return false, nil
	}
	if err := s.dal.SaveWithDocumentHistory(spec.Kind, spec.Key, currentActor(r),
		bootDocSnapshotIn(spec.Kind, spec.Key), func(ex sqlExecer) error {
			return putBootDocumentOn(ex, next)
		}); err != nil {
		return false, err
	}
	s.publishBootDoc(r)
	return true, nil
}

// replaceBootDoc is the whole-BODY replace shared by every write face.
//
// 🔴 IT TAKES THE BODY, NOT THE DOCUMENT (owner's ruling, 2026-08-23:
// 「唯讀區應該無法回寫，讀取有這個 key，回寫沒有這個 key，沒有人有任何方式可以
// 回寫」). The head is not something a caller may send WRONG — it is something a
// caller cannot send AT ALL, because the wire has no field for it. The server
// puts the shipped head back on here, so "the head came back unchanged" stopped
// being a rule to check and became a property of the only way a document can be
// written. That is the difference between 送錯被擋 and 送不出來, and it is why
// docHeadEditRefusal is gone rather than merely unreachable.
//
// A SIDE EFFECT WORTH KNOWING: this REPAIRS a pre-marker row. An overlay stored
// before docBodyMarker existed has no head; its whole text reads as the body
// (bootDocBodyOf, the same lenient reading DocRendered takes), so the first
// write through this face joins the shipped head back on and the row stops
// being the shape eventNoticeText refuses.
func (s *apiServer) replaceBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec, body string, allowShrink bool) {
	if spec.ReadOnly {
		writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
		return
	}
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	next, err := s.bootDocStoredText(spec, body)
	if err != nil {
		internalError(w, err)
		return
	}
	// 🔴 THE TWO GATES MEASURE TWO DIFFERENT THINGS, AND EACH MEASURES THE ONLY
	// THING ITS QUESTION IS ABOUT. Before the body-only wire they both compared
	// whole document against whole document, because that was the only unit a
	// caller could send; splitting the wire split the question:
	//
	//   WIPE asks "did this write empty the document of everything the caller
	//   can put in it?" — so it judges the BODY. Judging the joined text would
	//   retire the guard by accident: the head survives every write, so no write
	//   can ever produce an empty document again and the gate would answer
	//   "nothing was emptied" for a caller who just erased the whole of his own
	//   half.
	//
	//   THE CAP asks "does the document that gets STORED fit its ceiling?" — so
	//   it judges the joined text. That is the number size_chars reports, the
	//   number the cockpit shows against cap_chars, and the number docCapRefusal
	//   quotes; measuring the body here would make all three say different
	//   things about one document, and the owner would be refused at a length
	//   the surface told him was fine.
	//
	// Pinned by TestReplaceBootDoc_TheWipeGuardJudgesTheBodyAndTheCapJudgesTheStoredDocument.
	if !allowShrink && WholeDocWipeBlocked(bootDocBodyOf(spec, current.Text), body) {
		writeError(w, http.StatusBadRequest,
			docWipeRefusal(spec.DocName, ", or reset it to the shipped default"))
		return
	}
	// Hard cap, checked UNCONDITIONALLY: allow_shrink governs the opposite
	// direction and is not a bypass. The refusal names three numbers (what you
	// wrote, the cap, what is stored) because being refused is otherwise the
	// only way to learn any of them.
	if DocCapBlocked(spec.Cap, current.Text, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(spec.Cap, spec.DocName, current.Text, next))
		return
	}
	// Content validation, last of the gates because it is the only one that
	// judges the CONTENT rather than the size — a caller whose write is both
	// oversized and malformed learns about the size first, which is the one it
	// can act on without re-reading the document.
	if msg := bootDocBodyRefusal(spec, body); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Text: next, Tombstoned: false}, next); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootDocReceiptOf(dto))
}

// bootDocReceiptOf reduces the READ face's fold to the WRITE face's receipt
// (T-91). Both write tails — replaceBootDoc above and resetBootDoc below — go
// through it, so the eight write routes cannot answer with two shapes for one
// document family.
//
// 🔴 IT TAKES THE FOLD, IT DOES NOT REPLACE IT. foldBootDocDTO is still called
// exactly where it was: it is what the wipe guard and the cap gate read BEFORE
// the write (as `current`), and what both tails re-read AFTER it. Reducing the
// answer at the very last step is the only way to leave those guards judging
// the same text they always judged.
//
// size_chars/sha256 are measured on the STORED document (head + body), not the
// body — the same text foldBootDocDTO reports and the same number the cap
// refusal quotes, so the three cannot say different things about one document.
func bootDocReceiptOf(dto *bootDocDTO) bootDocumentReceiptDTO {
	return bootDocumentReceiptDTO{
		Kind:      dto.Kind,
		Key:       dto.Key,
		IsDefault: dto.IsDefault,
		SizeChars: dto.SizeChars,
		CapChars:  dto.CapChars,
		Sha256:    receiptSha256(dto.Text),
	}
}

// bootDocStoredText turns a caller's BODY into the bytes that get stored, by
// joining the SHIPPED head back on.
//
// 🔴 THE HEAD COMES FROM THE SEED, not from what is stored now, and that is the
// same argument the old head COMPARISON was built on: reading the head off the
// stored row would make the wall movable — one row that ever acquired a wrong
// head would hand that head to every write after it. The seed is the one copy
// no write path can reach (see resetBootDoc), so it is the only honest source.
// What changed is that the seed's head is now APPLIED rather than demanded, so
// there is no wall for a caller to be on the wrong side of.
//
// A kind that does not declare Split has no read-only half, so its body IS its
// document and this is the identity. Every kind in bootDocRegistry declares
// Split today; the branch is what keeps that a declaration rather than an
// assumption.
func (s *apiServer) bootDocStoredText(spec bootDocSpec, body string) (string, error) {
	if !spec.Split {
		return body, nil
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		return "", err
	}
	seedHead, _, seedSplit := DocSplitHeadBody(seedMD)
	if !hasSeed || !seedSplit {
		// A split kind whose seed lost its marker cannot say what its head IS.
		// 500 rather than a refusal: nothing the caller typed caused it, and
		// writing the body alone would silently retire the read-only half.
		return "", errors.New("boot document " + spec.Kind + "/" + spec.Key +
			" is declared split but its seed carries no " + docBodyMarker + " line")
	}
	return DocJoinHeadBody(seedHead, body), nil
}

// bootDocBodyOf reads the EDITABLE half out of a stored document.
//
// The no-marker reading is DELIBERATELY LENIENT and matches DocRendered's: a
// row stored before the marker existed is all body as far as anything that
// wants to know what the owner typed is concerned. The strict reading lives in
// eventNoticeText, which holds the spec and refuses to SEND such a row — see
// the asymmetry argued there and in DocRendered.
func bootDocBodyOf(spec bootDocSpec, text string) string {
	if !spec.Split {
		return text
	}
	_, body, split := DocSplitHeadBody(text)
	if !split {
		return text
	}
	return body
}

// bootDocBodyRefusal is the ONE content rule left on the write face: the
// editable half names no variables. It answers "" when the body is acceptable.
//
// 🔴 THE HEAD RULE IS NOT HERE BECAUSE IT NO LONGER EXISTS AS A RULE. It used
// to be half of this function (the head had to come back byte for byte); the
// body-only wire made it structural — see replaceBootDoc — so the refusal that
// went with it was deleted rather than left as a branch nothing can reach.
//
// It returns the sentence rather than writing it, because BOTH write faces need
// it: the REST/MCP replace, which turns it into a 400, and the history restore,
// which has no response body to write into and wraps it in an error instead.
// One gate, two callers — the alternative was the restore path judging content
// by a different rule, which is exactly what it did until T-3201.
//
// nil Vars opts the kind out of variable validation entirely (doc_vars.go) —
// system_interaction quotes JSON in its body — and that opt-out is about the
// SYNTAX, so it has to cover the body rule too.
func bootDocBodyRefusal(spec bootDocSpec, body string) string {
	if !spec.Split {
		// An unsplit kind is judged as ONE text the way it was before the split
		// existed: its declared variables are legal anywhere in it.
		if bad := DocVarsUndeclared(body, spec.Vars); len(bad) > 0 {
			return docVarWriteRefusal(spec.DocName, bad, spec.Vars)
		}
		return ""
	}
	if spec.Vars == nil {
		return ""
	}
	if bad := DocVarsIn(body); len(bad) > 0 {
		return docBodyVarRefusal(spec.DocName, bad)
	}
	return ""
}

// resetBootDoc tombstones the overlay so the folded read falls back to the
// SHIPPED seed.
//
// 🔴 NO CAP IS CHECKED HERE, matching reset_role and reset_insight. The factory
// text is part of the product, not something the caller wrote, so a ceiling the
// owner set afterwards must never be able to block the way back to it. This is
// the path that has to work when a bad edit has stopped agents booting, and at
// that moment there is nobody online to ask.
//
// 404 when the seed file is missing: there must be a factory version to reset
// TO, and the same 404 is what GET .../seed answers, so a surface offering the
// reset and a surface offering the comparison agree.
func (s *apiServer) resetBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec) {
	// 🔴 A READ-ONLY DOCUMENT IS REFUSED HERE TOO, and 405 rather than a
	// success is the deliberate choice. "Restore to default" on a document that
	// can never leave the default is not a harmless no-op: this path is a
	// WRITE — it tombstones an overlay row, retains a history revision and fans
	// a global_context frame — so answering 200 would put a revision and a
	// refresh on every surface for a document nothing changed, and would tell
	// a caller that a reset face exists for it. There is nothing to restore TO
	// that is not already what is being read.
	if spec.ReadOnly {
		writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
		return
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		internalError(w, err)
		return
	}
	if !hasSeed {
		writeError(w, http.StatusNotFound,
			"document '"+spec.Kind+"/"+spec.Key+"' has no shipped default to reset to")
		return
	}
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Tombstoned: true}, seedMD); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootDocReceiptOf(dto))
}

// GET /api/system-interaction — the folded 系統互動 block.
func (s *apiServer) HandleGetSystemInteractionApiSystemInteractionGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.systemInteractionSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/system-interaction — replace the editable BODY ({body}).
func (s *apiServer) HandleReplaceSystemInteractionApiSystemInteractionPost(w http.ResponseWriter, r *http.Request) {
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, s.systemInteractionSpec(), in.Body,
		in.AllowShrink != nil && *in.AllowShrink)
}

// POST /api/system-interaction/reset — back to the shipped seed.
func (s *apiServer) HandleResetSystemInteractionApiSystemInteractionResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.systemInteractionSpec())
}

// GET /api/offboard — the folded 〈停止〉 block.
func (s *apiServer) HandleGetOffboardApiOffboardGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.offboardSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/offboard — replace the editable BODY ({body}).
func (s *apiServer) HandleReplaceOffboardApiOffboardPost(w http.ResponseWriter, r *http.Request) {
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, s.offboardSpec(), in.Body,
		in.AllowShrink != nil && *in.AllowShrink)
}

// POST /api/offboard/reset — back to the shipped seed.
func (s *apiServer) HandleResetOffboardApiOffboardResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.offboardSpec())
}

// GET /api/boot-sequence/{runtime_key} — the folded 啟動步驟 block for ONE runtime.
func (s *apiServer) HandleGetBootSequenceApiBootSequenceRuntimeKeyGet(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/boot-sequence/{runtime_key} — replace the editable BODY ({body}).
func (s *apiServer) HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, spec, in.Body, in.AllowShrink != nil && *in.AllowShrink)
}

// POST /api/boot-sequence/{runtime_key}/reset — back to that runtime's shipped seed.
func (s *apiServer) HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	s.resetBootDoc(w, r, spec)
}

// writeUnknownBootSequence NAMES the runtimes that exist. A bare "not found"
// would leave a caller that typed "Codex" or passed an empty string unable to
// tell a typo from a server that has no boot sequences at all.
func writeUnknownBootSequence(w http.ResponseWriter, runtimeKey string) {
	writeError(w, http.StatusNotFound,
		"no boot sequence for runtime '"+runtimeKey+"' — the runtimes with their own boot sequence are '"+
			bootSequenceKeyClaude+"' and '"+bootSequenceKeyCodex+"'")
}

// bootDocReadOnlyRefusal is the ONE sentence every write face answers for a
// read-only document, the way docCapRefusal and docWipeRefusal are the one text
// behind their gates. It says what the document IS rather than that the caller
// lacks a permission: no principal can edit it, so pointing at authz would send
// an owner looking for a role to grant.
func bootDocReadOnlyRefusal(spec bootDocSpec) string {
	return "the " + spec.DocName + " is a read-only document — it is shown so you can " +
		"see what agents are told, but no caller may edit it and there is no version " +
		"of it other than the shipped one; nothing was written"
}

// genericBootDocSpec resolves the {kind}/{key} pair the three generic faces take,
// answering the SAME refusal all three times: a kind nobody registered says so,
// and a key that kind does not serve is told which keys it does.
//
// 404 rather than 400 on purpose, and it is the one place these routes differ
// from their document-history siblings: there the pair addresses a version
// series and a bad kind is a malformed request; here it addresses A DOCUMENT,
// and a document this server does not have is not found — the same answer
// writeUnknownBootSequence has given the named boot-sequence routes since T-791e.
func (s *apiServer) genericBootDocSpec(w http.ResponseWriter, kind, key string) (bootDocSpec, bool) {
	spec, ok := s.bootDocSpecFor(kind, key)
	if !ok {
		writeError(w, http.StatusNotFound, unknownBootDocKeyMsg(kind, key))
		return bootDocSpec{}, false
	}
	return spec, true
}

// GET /api/boot-docs/{kind}/{key} — the folded document, marker line and all.
func (s *apiServer) HandleGetBootDocApiBootDocsKindKeyGet(w http.ResponseWriter, r *http.Request, kind BootDocKind, key string) {
	spec, ok := s.genericBootDocSpec(w, string(kind), key)
	if !ok {
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/boot-docs/{kind}/{key} — replace the editable BODY ({body}).
func (s *apiServer) HandleReplaceBootDocApiBootDocsKindKeyPost(w http.ResponseWriter, r *http.Request, kind BootDocKind, key string) {
	spec, ok := s.genericBootDocSpec(w, string(kind), key)
	if !ok {
		return
	}
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, spec, in.Body, in.AllowShrink != nil && *in.AllowShrink)
}

// POST /api/boot-docs/{kind}/{key}/reset — back to the shipped seed.
func (s *apiServer) HandleResetBootDocApiBootDocsKindKeyResetPost(w http.ResponseWriter, r *http.Request, kind BootDocKind, key string) {
	spec, ok := s.genericBootDocSpec(w, string(kind), key)
	if !ok {
		return
	}
	s.resetBootDoc(w, r, spec)
}
