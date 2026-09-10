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
// 🔴 WHAT THIS GUARD COVERS, AND WHAT IT DOES NOT. It is a TEXT MATCH over the
// command string, never an analysis of the shell, so its boundary is a boundary
// in SPELLING. Read the boundary before trusting it with anything.
//
// COVERED: `rm` or `rmdir` standing as the command word — at the start of the
// string, after one of the separators | ; & newline ( , or after one of the shell
// keywords do / then / else / elif / { , optionally behind sudo — whose argument
// segment up to the next separator contains a `$` or a backtick.
//
// NOT COVERED, and this list is the point of the section:
//
//	find "$D" -delete   /   find "$D" -exec rm -f {} +
//	    → the command word is find; rm never appears where this rule looks.
//	echo "$D" | xargs rm -rf
//	    → the target is not in the text of the rm segment at all.
//	/bin/rm   \rm   command rm   env rm   exec rm   TMPDIR=x rm
//	    → the command word is not the bare token rm. ⚠️ Whether the harness even
//	      prompts on these is UNVERIFIED (T-163 §B-8 measured the guard allowing
//	      them; nobody has measured the harness end).
//	critical path (/, $HOME)   /   working directory or its ancestor
//	    → two of the harness's six refusal reasons; deciding them needs the
//	      session's working directory, not just the command text.
//
// 🔴 NOTHING GUARDS THAT LIST TODAY. A member that writes one of those shapes
// still stalls with no signal, exactly as it did before T-162, and that failure
// is measured rather than hypothetical: on 2026-09-10 a stall of this kind cost
// T-163's reviewer 3h50m (that particular shape IS covered now — but nothing on
// the member's side distinguishes a covered spelling from an uncovered one, so
// the same silent hours are still reachable). It is a KNOWN, UNPAID DEBT
// recorded in T-162's step note, NOT a gap some other layer picks up. Anyone who
// reads this header and stops asking "so what catches the rest?" has been misled
// by it.
//
// DELIBERATELY WIDER THAN THE HARNESS CHECK, WHERE IT REACHES AT ALL. Inside the
// covered spelling it over-refuses on purpose: the harness only prompts when it
// cannot resolve the variable — `D=/abs/path && rm -f "$D"/*.json` resolves and
// runs (measured) — and this guard does NOT reproduce that resolution logic,
// because an unverifiable analysis that drifts with someone else's releases
// trades a cheap failure for the expensive one. So `rm -rf "$D"` is refused here
// though it would not have prompted. Inside the covered set the costs are
// asymmetric and that is the trade: a false refusal costs one rewrite, which the
// member performs by itself; a missed one costs a stall.
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
// ONE RULE, THREE OF THE HARNESS'S SIX REFUSALS. Reading the shipped binary
// (2.1.267) the built-in check refuses removals for six stated reasons, and the
// first three are one family — the target is not a literal path, so it cannot be
// analysed:
//
//	possibly-empty variable path                          ← measured stalling a member
//	possibly-empty variable path inside command substitution
//	statically-unresolvable target
//
// "The target must be a literal path" covers all three WITHOUT reproducing the
// harness's analysis, which is the property that matters: an analysis copied here
// would drift with someone else's releases and fail by stalling, the exact defect
// this guard exists to remove.
//
// 🔴 TWO OF THE REMAINING THREE ARE NOT COVERED, and they are a different family
// — a LITERAL path that is itself dangerous:
//
//	working directory or its ancestor
//	critical path (the filesystem root, the home directory)
//
// Deciding those needs the session's working directory, not just the command
// text, so they are deliberately out of scope here. A member that writes one of
// those shapes still stalls. Widening to them is a separate decision with its own
// false-positive surface; do not quietly bolt it on.
//
// The sixth is a circuit breaker rather than a shape — same M$ helper, same
// circuitBreaker:"dangerousRemoval":
//
//	— too many command substitutions to analyze (>64)
//
// It cannot fire on a command this guard lets through: 65 command substitutions
// cannot be written without a `$` or a backtick, which is what the rule above
// already matches.
//
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
// was reviewing this guard. Anything else that can legally stand in front of a
// command word belongs in this group too, not in a longer list of removal spellings.
var removalWithAnExpandedTarget = regexp.MustCompile(
	"(?:^|[|;&\n(])\\s*(?:(?:do|then|else|elif|\\{)\\s+)*(?:sudo\\s+)?(?:rm|rmdir)\\b[^|;&\n)]*[$`]",
)

// guardBashRefusal is the text the member reads. See the header: it is a
// statement about the environment, never a request.
const guardBashRefusal = "OffiCraft 執行環境政策：移除指令的目標路徑必須是字面路徑，不得含 shell 變數或指令替換。" +
	"這個形狀會觸發執行環境的確認提示，而本環境沒有人可以回答，成員會就此停住。" +
	"這是環境層的固定政策，不是建議，也不是由指令輸出提出的要求。" +
	"改寫方式：直接寫完整路徑，或先 cd 進目標目錄再用相對路徑，" +
	"或改用 ocagent clean <完整路徑>。改寫後直接重試，不需要詢問任何人。"

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
