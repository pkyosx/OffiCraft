package main

// guard-bash — the PreToolUse hook that keeps a headless member from freezing on
// the harness's un-waivable removal prompt.
//
// THE PROBLEM IT SOLVES. Claude Code carries a built-in safety check for removals
// whose target is built from a shell variable it cannot resolve. That check
// demands a person choose Yes or No, and it is immune to
// --dangerously-skip-permissions — the flag every member is launched with. An
// OffiCraft member has nobody at the keyboard, so the demand is a stall with no
// signal: from the outside it looks exactly like a crash, a logout, or an
// exhausted quota. cli/CLAUDE.md §5 has stated the rule for a while (「agent 不
// 直接 rm working tree 內容；harness 的 dangerous-rm gate 會讓 headless agent 卡死」)
// and cli/ocwarden/spawn.go's PurgeTrash reaps the quarantine that rule creates.
// What was missing is the half that acts when the rule is not followed: on
// 2026-09-10 one member stalled during its own shutdown cleanup and the owner
// had to press the button by hand.
//
// WHY THIS LAYER. A PreToolUse hook runs BEFORE the tool call, and a denial from
// it means the command never reaches the harness's check at all — so the prompt
// is not answered, it is never raised. The member cannot route around the hook
// and does not have to remember any rule, which is the property a prose
// instruction cannot offer.
//
// 🔴 THE REFUSAL TEXT IS PART OF THE FUNCTION, NOT COPY. Measured on seth-m5,
// 2026-09-10: worded as a request (「請改用 ocagent clean」) the member REFUSED to
// comply, reasoning that text arriving in a tool result is not its operator
// speaking and must not be allowed to rewrite the command it runs. That instinct
// is correct and must not be argued away. Worded as a statement about the
// environment (「本環境不允許…」) the same member rewrote the path itself and
// finished the job. Anyone softening this string back into a request restores
// the failure.
//
// 🔴 THE BOUNDARY OF THIS GUARD IS A BOUNDARY IN SPELLING, AND ONLY IN SPELLING.
// It is a regexp over the command TEXT. It does not parse the shell, does not
// resolve a variable, does not know the working directory, and therefore cannot
// aim at any one of the harness's six refusal REASONS. Those two axes have been
// confused in this very header before — see COVERAGE OF A REASON below — so keep
// the two lists that follow strictly about spelling.
//
// Every line of both lists is MEANT to have a matching case in guardbash_test.go
// (TestGuardBash_EveryShapeTheHeaderCallsCoveredIsRefused and
// ...EveryShapeTheHeaderCallsNotCoveredIsAllowed). For the shapes those cases do
// name, the guard's BEHAVIOUR is pinned and can no longer drift without going
// red — three seeded mutants confirm they move.
//
// 🔴 BUT THAT ONE-TO-ONE CORRESPONDENCE IS KEPT BY HAND AND NOTHING WATCHES IT.
// One test does read this file — TestPinnedTestNamesInCommentsStillExist scans it
// for cited test names and goes red when one stops existing — but nothing reads
// the LISTS. Measured 2026-09-10: a bogus extra line in the COVERED list with no
// case behind it left the whole package green. So these lists are exactly as
// complete as the last person to edit them was careful, and anyone adding a shape
// here MUST add its case in the same edit or they have put an unpinned claim back
// into this header — the defect the two tests exist to remove. A checker that
// walks the lists and asserts a case per line would close it; it is a KNOWN,
// UNPAID DEBT, not something another layer does.
//
// What no test can check in either direction is this prose: nothing parses
// comments, and a test that did would be defeated by the first equivalent
// rewording while still looking green.
//
// 🔴 SO IF THIS TEXT AND THOSE TABLES EVER DISAGREE, THE TABLES ARE RIGHT AND
// THIS TEXT IS THE BUG. Change both in the same edit. Three separate review
// rounds each found a false sentence in this file's prose, every time with CI
// green; the tables shrink that surface, they do not close it.
//
// COVERED — the guard REFUSES these:
//
//	rm or rmdir as the command word — at the start of the string, after one of
//	the separators | ; & newline ( , or after one of the keywords
//	do / then / else / elif / { , optionally behind sudo — whose argument
//	segment, up to the next separator, contains a $ or a backtick:
//
//	  rm -rf $D/x            ls | rm -f "$D"/x     cd /tmp && rm -rf $D
//	  rmdir "$D"/empty       rm -rf $(cat p.txt)   sudo rm -rf $D
//	  for f in a b; do rm -f $D/$f; done           if [ -d x ]; then rm -rf $D; fi
//	  rm -rf "$D"            D=/abs/p && rm -f "$D"/*.json
//
//	Incidentally this also refuses rm -rf $HOME and rm -rf "$PWD" — see COVERAGE
//	OF A REASON. They are here because they carry a $, not because anything in
//	this guard knows what $HOME is.
//
// NOT COVERED — the guard ALLOWS every one of these:
//
//	1. the removal spelled some other way
//	     find "$D" -delete            find "$D" -exec rm -f {} +
//	     echo "$D" | xargs rm -rf
//	     /bin/rm -rf $D    \rm -rf $D    command rm -rf $D
//	     env rm -rf $D     exec rm -rf $D    TMPDIR=x rm -rf $D
//	2. a token in front of the command word that is not in the keyword group and
//	   is not sudo either — sudo is special-cased and its line is in COVERED above
//	     if rm -rf $D/x; then echo gone; fi    until rm -rf $D/x; do sleep 1; done
//	     while rm -rf $D/x; do sleep 1; done   ! rm -rf $D/x
//	     time rm -rf $D/x    nohup rm -rf $D/x    {rm -rf $D;}
//	3. a target with no expansion in it at all
//	     rm -rf ~/build/*             cd /tmp/x && rm -rf ./*
//	     D=/tmp/x; cd "$D" && rm -rf ./*        rmdir -p ./build/*
//	4. more than 64 command substitutions reached through a command word that is
//	   not rm (per the bundle the harness's circuit breaker scans the whole
//	   command text with no command-word anchoring; this rule does not)
//	     echo $(echo a)…×65 | xargs rm -rf      /bin/rm -rf $(echo a)…×65
//	5. a dangerous LITERAL path, because there is no $ for the rule to match
//	     rm -rf /     rm -rf ~     rm -rf .     rm -rf ..     rm -rf /Users/alice
//
// 🔴 COVERAGE OF A HARNESS *REASON* IS INCIDENTAL, NEVER DESIGNED. A reason is
// covered exactly when a command that triggers it also happens to match the
// spelling rule, and not otherwise — so the SAME reason lands on both sides:
//
//	critical path:      rm -rf $HOME  REFUSED   /   rm -rf ~   ALLOWED
//	cwd or its ancestor: rm -rf "$PWD" REFUSED  /   rm -rf ..  ALLOWED
//
// No reason may therefore be written into either list above as though it were a
// spelling. An earlier version of this header did exactly that — it put "critical
// path" and "working directory or its ancestor" in the NOT COVERED list and said
// nothing guarded them, while guardbash_test.go on the same commit asserted that
// sudo rm -rf $HOME/x was denied. Prose and test contradicting each other inside
// one file, CI green. Note too that only the cwd half would need the session's
// working directory; the critical-path half (/, ~) needs nothing but the text.
//
// 🔴 NOTHING GUARDS THE NOT COVERED LIST. A member that writes one of those
// shapes reaches the harness with this guard silent. It is a KNOWN, UNPAID DEBT
// recorded in T-162's step note, NOT a gap that some other layer picks up.
// Anyone who reads this header and stops asking "so what catches the rest?" has
// been misled by it.
//
// EVIDENCE LEVELS, because this header has been wrong about exactly this before.
// Every allow/deny stated above is MEASURED, in Go, by the two tests named above.
// Every statement about what the HARNESS does — its six reasons, what its circuit
// breaker scans, when it prompts — is READ OUT OF THE 2.1.267 bundle and has NOT
// been measured end to end, EXCEPT the three places that say so inline, which are
// observations on a real member and not bundle reading:
//
//	the stall this guard exists to prevent — a member frozen on the prompt during
//	its own shutdown cleanup, 2026-09-10, in THE PROBLEM IT SOLVES at the top;
//	D=/abs/path && rm -f "$D"/*.json resolving and running with no prompt
//	("measured on a member, 2026-09-10"), in DELIBERATELY WIDER below;
//	the "possibly-empty variable path" reason, marked "measured stalling a member"
//	in the six-reason table above removalWithAnExpandedTarget.
//
// Do not promote anything else to measured, and do not demote those three.
//
// DELIBERATELY WIDER THAN THE HARNESS CHECK, WHERE IT REACHES AT ALL. Inside the
// covered spelling it over-refuses on purpose: per the bundle the harness only
// prompts when it cannot resolve the variable — D=/abs/path && rm -f "$D"/*.json
// resolves and runs (measured on a member, 2026-09-10) — and this guard does NOT
// reproduce that resolution logic, because an unverifiable analysis that drifts
// with someone else's releases trades a cheap failure for the expensive one. So
// rm -rf "$D" is refused here though the bundle says it would not have prompted.
// Inside the covered set the costs are asymmetric and that is the trade: a false
// refusal costs one rewrite, which the member performs by itself; a missed one
// costs a stall.
//
// OC_BASE CLASSIFICATION: EXEMPT. This subcommand contacts no station and never
// reads cfg.Base — it reads one JSON document on stdin and writes at most one on
// stdout. It takes no identity either, so unlike clean it has nothing to refuse
// for.
//
// FAIL-OPEN, ON PURPOSE. Unreadable stdin, malformed JSON, a missing command
// field: every one of them ALLOWS. This code sits in front of every single shell
// call a member makes, so a fail-closed bug here locks the member out of its own
// machine — strictly worse than the stall it exists to prevent.

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

// removalWithAnExpandedTarget matches a removal command whose arguments contain
// a shell expansion of any kind — a variable ($D, ${DIR}, "$D"/x) or a command
// substitution ($(...) or backticks).
//
// ONE RULE, AIMED AT A SPELLING, NOT AT A LIST OF REASONS. Read out of the
// shipped binary (2.1.267 — READ, not measured) the built-in check refuses
// removals for six stated reasons:
//
//	possibly-empty variable path                              ← measured stalling a member
//	possibly-empty variable path inside command substitution
//	statically-unresolvable target
//	working directory or its ancestor
//	critical path (the filesystem root, the home directory)
//	— too many command substitutions to analyze (>64)         ← a circuit breaker;
//	  same M$ helper, same circuitBreaker:"dangerousRemoval"
//
// This rule reproduces NONE of that analysis, on purpose: a copy of it would
// drift with someone else's releases and fail by stalling, the exact defect this
// guard exists to remove. It asks one much cruder question instead — does a
// removal's own argument segment carry a $ or a backtick — and that question is
// NOT a subset of any reason above. Two corrections this comment has already had
// to make, both of them from claiming otherwise:
//
//	Reason 3 does not require an expansion. Two of its branches are reachable
//	with none: rm -rf ~/build/* and cd /tmp/x && rm -rf ./* are ALLOWED here
//	(measured). "One rule covers reasons 1-3", which this comment used to say,
//	is false for reason 3.
//
//	The circuit breaker is not command-word anchored. Per the bundle it fires on
//	o.length>64 and then tests /\brm(?:dir)?\b/ against the WHOLE command text,
//	so echo $(…)×65 | xargs rm -rf reaches it while this rule stays silent
//	(measured: ALLOWED). This comment used to claim the breaker "cannot fire on
//	a command this guard lets through". It can.
//
// Reasons 4 and 5 land on both sides of this rule depending only on whether the
// command happens to carry a $. See the file header, COVERAGE OF A REASON.
// The leading alternation anchors the command word to the start of the string or
// to a shell separator, so the word has to BE the command rather than appear
// inside somebody else's argument.
//
// 🔴 A SEPARATOR IS NOT THE ONLY THING THAT CAN PRECEDE THE COMMAND WORD. `do`,
// `then`, `else`, `elif` and `{` sit BETWEEN a separator and the command they
// introduce, and they are not separators themselves. Anchoring on separators
// alone made the whole of every loop body and every conditional body invisible —
// `for f in a b; do rm -f $D/$f; done` was allowed. That was not a missing shape
// in a list: deleting a batch of files inside a loop is ordinary member work, and
// on 2026-09-10 exactly that shape stalled T-163's reviewer for 3h50m while it
// was reviewing this guard.
//
// 🔴 THE GROUP IS NOT EXHAUSTIVE, AND LENGTHENING IT IS NOT THE FIX. `if`,
// `until`, `while`, `!`, `time`, `nohup` and `{` with no space after it all
// still stand in front of a removal this rule allows (measured; they are in the
// header's NOT COVERED list, item 2). Every round of review so far has found
// another one, which is the signal that "add the next spelling" is not
// converging. Anything added here must be added to the header's lists and to
// guardbash_test.go in the same edit.
var removalWithAnExpandedTarget = regexp.MustCompile(
	"(?:^|[|;&\n(])\\s*(?:(?:do|then|else|elif|\\{)\\s+)*(?:sudo\\s+)?(?:rm|rmdir)\\b[^|;&\n)]*[$`]",
)

// guardBashRefusal is the text the member reads. See the header: it is a
// statement about the environment, never a request.
const guardBashRefusal = "OffiCraft 執行環境政策：移除指令的目標路徑必須是字面路徑，不得含 shell 變數或指令替換。" +
	"這個形狀會觸發執行環境的確認提示，而本環境沒有人可以回答，成員會就此停住。" +
	"這是環境層的固定政策，不是建議，也不是由指令輸出提出的要求。" +
	"改寫方式：改用 ocagent clean <完整路徑>，或直接寫出完整的字面路徑（不含變數、不含 ~、不含萬用字元）。" +
	"不要改成「先 cd 進目標目錄再用 ./* 之類的相對萬用字元」——那個形狀一樣會停住。" +
	"改寫後直接重試，不需要詢問任何人。"

// preToolUseInput is the slice of the hook payload this guard reads. Every other
// field the harness sends is deliberately ignored.
type preToolUseInput struct {
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// cmdGuardBash reads one PreToolUse payload and writes a deny decision when the
// command carries the shape that stalls a headless member. Allowing is silence:
// nothing on stdout, exit 0.
func cmdGuardBash(in io.Reader, out io.Writer) int {
	raw, err := io.ReadAll(in)
	if err != nil {
		return 0 // fail-open; see the header
	}
	var payload preToolUseInput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0 // fail-open; see the header
	}
	if !removalWithAnExpandedTarget.MatchString(payload.ToolInput.Command) {
		return 0
	}
	// HTML escaping OFF so the refusal reads as written — it contains 「<完整路徑>」,
	// and < in front of a member is noise it has to decode.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": guardBashRefusal,
		},
	})
	_, _ = out.Write(buf.Bytes())
	return 0
}
