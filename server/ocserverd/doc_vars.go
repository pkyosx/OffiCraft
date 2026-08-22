package main

// doc_vars.go — the {name} variable mechanism for event-procedure documents
// (T-3201).
//
// 🔴 WHY THIS EXISTS. Before this file the tree interpolated NOTHING into an
// owner-editable document and validated nothing: FoldBootDocument hands the
// overlay back untouched, and the only substitution anywhere on the server is
// assets.go's {OWNER_ID} ReplaceAll — which reads the go:embed seed and never
// sees an overlay. So a document that named a variable the code does not fill
// went out to an agent with the literal braces still in it, and nothing on any
// surface said a word. The whole point of the three functions below is that
// each of those three failures now has somewhere to go red.
//
// The three duties, in the order a name travels:
//   declare  — each document kind lists the names it may use (bootDocReg.Vars)
//   validate — DocVarsUndeclared refuses a WRITE that names anything else
//   render   — RenderDocVars refuses a SEND whose declared name has no value
//
// 🔴 nil MEANS OFF, EMPTY MEANS ZERO. A kind whose Vars is nil predates this
// mechanism and is not validated at all — system_interaction's seed carries
// JSON examples like {"id": "<attachment id>"} that this syntax cannot tell
// from a variable, and breaking a document that ships today to introduce a
// guard for documents that do not is the wrong trade. A kind whose Vars is an
// empty non-nil slice is validated and allows NO variable, which is exactly
// what the owner-editable BODY half will declare once the read-only head is
// split off from it.

import (
	"regexp"
	"strings"
)

// docVarRe matches one {name} slot. Non-greedy by construction — the character
// class excludes both braces — so `{a} and {b}` is two slots, not one spanning
// the whole line, and an unbalanced `{` is simply not a slot.
var docVarRe = regexp.MustCompile(`\{([^{}]*)\}`)

// DocVarsIn lists the variable names text uses, in first-appearance order and
// deduped. Order is first-appearance rather than sorted so a refusal reads in
// the order the author's eye scans the document.
func DocVarsIn(text string) []string {
	var names []string
	seen := map[string]bool{}
	for _, m := range docVarRe.FindAllStringSubmatch(text, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		names = append(names, m[1])
	}
	return names
}

// DocVarsUndeclared lists the names text uses that declared does not allow.
// declared==nil means the kind opts out of validation entirely (see the file
// header) and the answer is always empty.
func DocVarsUndeclared(text string, declared []string) []string {
	if declared == nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, d := range declared {
		allowed[d] = true
	}
	var bad []string
	for _, n := range DocVarsIn(text) {
		if !allowed[n] {
			bad = append(bad, n)
		}
	}
	return bad
}

// RenderDocVars substitutes values into text's {name} slots.
//
// 🔴 IT REFUSES RATHER THAN SUBSTITUTING A BLANK. A declared name the caller
// did not supply is a fault in the CODE, not in the document, and the failure
// it produces if left alone is the one this whole mechanism exists to stop: a
// sentence that reaches an agent with `{task_no}` still in it, or — worse, once
// someone "fixes" it by substituting "" — a sentence that reads perfectly and
// names the wrong thing. Missing names come back in the error so the caller
// learns all of them at once instead of one per run.
func RenderDocVars(text string, declared []string, values map[string]string) (string, error) {
	if bad := DocVarsUndeclared(text, declared); len(bad) > 0 {
		return "", errDocVars("cannot be rendered: it uses ", bad,
			"which this document does not declare")
	}
	var missing []string
	for _, n := range DocVarsIn(text) {
		if _, ok := values[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return "", errDocVars("cannot be rendered: no value was supplied for ", missing,
			"— nothing was sent")
	}
	return docVarRe.ReplaceAllStringFunc(text, func(slot string) string {
		return values[docVarRe.FindStringSubmatch(slot)[1]]
	}), nil
}

// docVarNameList renders names the way both the refusal and the error quote
// them, so a reader comparing a rejection with a document sees the same shape.
func docVarNameList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, "{"+n+"}")
	}
	return strings.Join(quoted, ", ")
}

// docVarWriteRefusal is the ONE refusal text behind the write-face validation,
// the same way docCapRefusal is the one text behind the cap. It names the
// offending variables AND the ones this document does allow, because being
// refused is otherwise the only way to learn either — and it says outright that
// nothing was written, which is the sentence the owner needs before deciding
// whether to retype the whole document.
func docVarWriteRefusal(docName string, bad, declared []string) string {
	allowed := "this document declares no variables at all"
	if len(declared) > 0 {
		allowed = "the variables this document declares are " + docVarNameList(declared)
	}
	return "the " + docName + " you are writing uses " + docVarNameList(bad) +
		", which is not one of its variables — nothing was written. " + allowed +
		". A variable nothing fills reaches an agent with the braces still in it."
}

// errDocVars builds the render-time error. Separate from docVarWriteRefusal
// because the two have different readers: that one answers an owner who is
// editing, this one answers a developer whose call site is wrong.
func errDocVars(lead string, names []string, tail string) error {
	return &docVarError{msg: "document " + lead + docVarNameList(names) + " " + tail}
}

type docVarError struct{ msg string }

func (e *docVarError) Error() string { return e.msg }
