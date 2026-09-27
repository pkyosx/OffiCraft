package main

// The {name} variable mechanism for event-procedure documents:
//   declare  — each document kind lists the names it may use (bootDocReg.Vars)
//   validate — DocVarsUndeclared checks the text being rendered
//   render   — RenderDocVars refuses a SEND whose declared name has no value
//
// 🔴 nil MEANS OFF, EMPTY MEANS ZERO. A kind whose Vars is nil is not validated
// at all — e.g. a system_interaction body may quote JSON like {"id": "..."} that
// this syntax cannot tell from a variable. An empty non-nil slice allows NO
// variable.

import (
	"regexp"
	"strings"
)

var docVarRe = regexp.MustCompile(`\{([^{}]*)\}`)

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

// 🔴 RenderDocVars REFUSES rather than substituting a blank: a missing value is
// a CODE fault, and "" yields a sentence that reads perfectly and names the wrong
// thing. All missing names come back at once.
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

func docVarNameList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, "{"+n+"}")
	}
	return strings.Join(quoted, ", ")
}

func errDocVars(lead string, names []string, tail string) error {
	return &docVarError{msg: "document " + lead + docVarNameList(names) + " " + tail}
}

type docVarError struct{ msg string }

func (e *docVarError) Error() string { return e.msg }
