// gen renders spec/openapi.json from the Go SSOT (spec/ocapi/ocapi.go).
//
// It reads the SSOT with go/ast — NOT reflect — because everything the wire
// contract carries beyond a Go type lives in comments: the //oc: directives and
// the //oc:doc prose. A reflect-based renderer cannot see either.
//
// The directive vocabulary is documented in spec/ocapi/VOCABULARY.md. Anything
// this file cannot express is not expressible in the spec, by construction.
//
// Usage: gen <ssot.go> <out.json>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type obj = map[string]any

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "[gen-openapi] FAIL — "+format+"\n", a...)
	os.Exit(1)
}

// ---------------------------------------------------------------- directives

// dirs is one declaration's //oc: lines plus the //oc:doc prose it carried.
type dirs struct {
	lines  []string
	doc    string
	hasDoc bool
}

func splitDoc(cg *ast.CommentGroup) dirs {
	d := dirs{}
	if cg == nil {
		return d
	}
	for _, c := range cg.List {
		t := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if !strings.HasPrefix(t, "oc:") {
			continue
		}
		if rest, ok := strings.CutPrefix(t, "oc:doc "); ok {
			if err := json.Unmarshal([]byte(rest), &d.doc); err != nil {
				die("//oc:doc is not a JSON string: %s", rest)
			}
			d.hasDoc = true
			continue
		}
		d.lines = append(d.lines, t)
	}
	return d
}

func (d dirs) find(kw string) (string, bool) {
	for _, l := range d.lines {
		if rest, ok := strings.CutPrefix(l, "oc:"+kw); ok {
			if rest == "" {
				return "", true
			}
			if strings.HasPrefix(rest, " ") {
				return strings.TrimSpace(rest), true
			}
		}
	}
	return "", false
}

func (d dirs) all(kw string) []string {
	var out []string
	for _, l := range d.lines {
		if rest, ok := strings.CutPrefix(l, "oc:"+kw+" "); ok {
			out = append(out, strings.TrimSpace(rest))
		}
	}
	return out
}

// tokenize splits a directive body on spaces, keeping JSON values (quoted
// strings, {…}, […]) intact.
func tokenize(line string) []string {
	var out []string
	var cur strings.Builder
	depth, inStr, esc := 0, false, false
	for _, r := range line {
		switch {
		case esc:
			esc = false
			cur.WriteRune(r)
		case r == '\\' && inStr:
			esc = true
			cur.WriteRune(r)
		case r == '"':
			inStr = !inStr
			cur.WriteRune(r)
		case inStr:
			cur.WriteRune(r)
		case r == '{' || r == '[':
			depth++
			cur.WriteRune(r)
		case r == '}' || r == ']':
			depth--
			cur.WriteRune(r)
		case r == ' ' && depth == 0:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// tagset is one directive's key=value pairs and bare flags.
type tagset struct {
	kv   map[string]string
	flag map[string]bool
}

func parseTags(toks []string) tagset {
	t := tagset{kv: map[string]string{}, flag: map[string]bool{}}
	for _, tok := range toks {
		if i := strings.Index(tok, "="); i > 0 {
			t.kv[tok[:i]] = tok[i+1:]
		} else {
			t.flag[tok] = true
		}
	}
	return t
}

func (t tagset) str(k string) (string, bool) {
	raw, ok := t.kv[k]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		die("%s= is not a JSON string: %s", k, raw)
	}
	return s, true
}

// jsonVal decodes a directive value with UseNumber so numeric literals survive
// verbatim — a default of 0 must not come back as 0.0.
func jsonVal(raw string) any {
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		die("not valid JSON: %s", raw)
	}
	return v
}

func (t tagset) val(k string) (any, bool) {
	raw, ok := t.kv[k]
	if !ok {
		return nil, false
	}
	return jsonVal(raw), true
}

// ---------------------------------------------------------------- titles

// titleize mirrors the title the spec carries when nobody wrote one: the
// property name split on "_", each word capitalised (already-upper words kept).
func titleize(n string) string {
	parts := strings.Split(n, "_")
	for i, p := range parts {
		if p == "" || p == strings.ToUpper(p) {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func applyTitle(node obj, t tagset, fallback string) {
	if t.flag["notitle"] {
		return
	}
	if s, ok := t.str("title"); ok {
		node["title"] = s
		return
	}
	node["title"] = fallback
}

// ---------------------------------------------------------------- type nodes

// typeNode renders a Go type expression as the schema node it stands for.
// Tags on the same field refine it (format, enum, additionalProperties, …).
func typeNode(e ast.Expr, t tagset) obj {
	switch n := e.(type) {
	case *ast.StarExpr:
		return typeNode(n.X, t)
	case *ast.ArrayType:
		return obj{"type": "array", "items": typeNode(n.Elt, t)}
	case *ast.MapType:
		if key, ok := n.Key.(*ast.Ident); !ok || key.Name != "string" {
			die("only map[string]… is expressible, got %s", exprString(e))
		}
		o := obj{"type": "object"}
		if isAny(n.Value) {
			applyAP(o, t)
			return o
		}
		v := typeNode(n.Value, tagset{kv: map[string]string{}, flag: map[string]bool{}})
		if s, ok := t.str("vtitle"); ok {
			v["title"] = s
		}
		o["additionalProperties"] = v
		return o
	case *ast.StructType:
		o := obj{"type": "object"}
		props, required := fields(n)
		o["properties"] = props
		if len(required) > 0 {
			o["required"] = required
		}
		applyAP(o, t)
		return o
	case *ast.InterfaceType:
		return obj{}
	case *ast.Ident:
		switch n.Name {
		case "any":
			return obj{}
		case "string":
			o := obj{"type": "string"}
			if v, ok := t.val("penum"); ok {
				o["enum"] = v
			}
			if s, ok := t.kv["fmt"]; ok {
				o["format"] = s
			}
			if s, ok := t.kv["minLength"]; ok {
				i, err := strconv.Atoi(s)
				if err != nil {
					die("minLength is not an integer: %s", s)
				}
				o["minLength"] = i
			}
			return o
		case "bool":
			return obj{"type": "boolean"}
		case "int":
			return obj{"type": "integer"}
		case "int64":
			return obj{"type": "integer", "format": "int64"}
		case "float32":
			return obj{"type": "number"}
		case "float64":
			return obj{"type": "number", "format": "double"}
		}
		ref := obj{"$ref": "#/components/schemas/" + n.Name}
		if t.flag["allof"] {
			return obj{"allOf": []any{ref}}
		}
		return ref
	}
	die("unrenderable Go type: %s", exprString(e))
	return nil
}

func isAny(e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok && id.Name == "any" {
		return true
	}
	_, ok := e.(*ast.InterfaceType)
	return ok
}

func applyAP(o obj, t tagset) {
	switch t.kv["ap"] {
	case "true":
		o["additionalProperties"] = true
	case "false":
		o["additionalProperties"] = false
	}
}

func exprString(e ast.Expr) string {
	var b bytes.Buffer
	switch n := e.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.StarExpr:
		return "*" + exprString(n.X)
	case *ast.ArrayType:
		return "[]" + exprString(n.Elt)
	case *ast.MapType:
		return "map[" + exprString(n.Key) + "]" + exprString(n.Value)
	case *ast.StructType:
		return "struct{…}"
	case *ast.InterfaceType:
		return "any"
	}
	fmt.Fprintf(&b, "%T", e)
	return b.String()
}

// ---------------------------------------------------------------- fields

func fields(st *ast.StructType) (obj, []string) {
	props := obj{}
	var required []string
	for _, f := range st.Fields.List {
		if f.Tag == nil {
			die("struct field %s has no json tag", exprString(f.Type))
		}
		raw, err := strconv.Unquote(f.Tag.Value)
		if err != nil {
			die("unparseable struct tag %s", f.Tag.Value)
		}
		name := strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
		if name == "" {
			die("struct field %s has an empty json tag", exprString(f.Type))
		}
		d := splitDoc(f.Doc)
		body, _ := d.find("field")
		t := parseTags(tokenize(body))

		var node obj
		if raw, ok := t.kv["union"]; ok {
			node = obj{"anyOf": jsonVal(raw)}
		} else if t.flag["null"] {
			node = obj{"anyOf": []any{typeNode(f.Type, t), obj{"type": "null"}}}
		} else {
			node = typeNode(f.Type, t)
		}
		if d.hasDoc {
			node["description"] = d.doc
		}
		applyTitle(node, t, titleize(name))
		if v, ok := t.val("default"); ok {
			node["default"] = v
		}
		if t.flag["writeonly"] {
			node["writeOnly"] = true
		}
		if s, ok := t.str("xgotype"); ok {
			node["x-go-type"] = s
		}
		if _, dup := props[name]; dup {
			die("duplicate json property %q", name)
		}
		props[name] = node
		if t.flag["req"] {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	return props, required
}

// ---------------------------------------------------------------- schemas

func schemaDecl(ts *ast.TypeSpec, d dirs) obj {
	name := ts.Name.Name
	body, _ := d.find("schema")
	t := parseTags(tokenize(body))
	s := obj{}
	if d.hasDoc {
		s["description"] = d.doc
	}
	switch {
	case t.kv["enum"] != "":
		v, _ := t.val("enum")
		s["enum"] = v
		if et := t.kv["etype"]; et != "" && et != "None" {
			s["type"] = et
		}
	case t.flag["freeroot"]:
		s["type"] = "object"
	default:
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			die("schema %s is neither an enum, a freeroot nor a struct", name)
		}
		props, required := fields(st)
		s["properties"] = props
		if len(required) > 0 {
			s["required"] = required
		} else if t.flag["emptyreq"] {
			s["required"] = []string{}
		}
		applyAP(s, t)
		s["type"] = "object"
	}
	applyTitle(s, t, name)
	return s
}

// ---------------------------------------------------------------- operations

// contentSchema turns a `ref=` / `inline=` / `empty` / `none` tag into the
// schema node a request body or response carries.
func contentSchema(t tagset) (any, bool) {
	if r, ok := t.kv["ref"]; ok {
		return obj{"$ref": "#/components/schemas/" + r}, true
	}
	if r, ok := t.kv["inline"]; ok {
		return jsonVal(r), true
	}
	if t.flag["empty"] {
		return obj{}, true
	}
	return nil, false
}

func operation(fn *ast.FuncDecl, d dirs, stdErrors obj) (method, path string, op obj) {
	op = obj{}
	route, ok := d.find("route")
	if !ok {
		die("%s has //oc: directives but no //oc:route", fn.Name.Name)
	}
	rt := tokenize(route)
	if len(rt) != 2 {
		die("%s: //oc:route wants METHOD and PATH, got %q", fn.Name.Name, route)
	}
	method, path = strings.ToLower(rt[0]), rt[1]

	id, ok := d.find("id")
	if !ok {
		die("%s: no //oc:id", fn.Name.Name)
	}
	op["operationId"] = id
	if summary, ok := d.find("summary"); ok {
		var s string
		if err := json.Unmarshal([]byte(summary), &s); err != nil {
			die("%s: //oc:summary is not a JSON string", fn.Name.Name)
		}
		op["summary"] = s
	}
	if d.hasDoc {
		op["description"] = d.doc
	}

	// the parameter's Go type comes from the handler SIGNATURE, not a tag
	sig := map[string]ast.Expr{}
	for _, a := range fn.Type.Params.List {
		for _, nm := range a.Names {
			sig[nm.Name] = a.Type
		}
	}

	var params []any
	for _, line := range d.all("param") {
		tk := tokenize(line)
		if len(tk) < 2 {
			die("%s: //oc:param wants at least IN and NAME", fn.Name.Name)
		}
		in, pname := tk[0], tk[1]
		t := parseTags(tk[2:])
		p := obj{"in": in, "name": pname, "required": t.flag["req"]}
		if s, ok := t.str("desc"); ok {
			p["description"] = s
		}
		if t.flag["deprecated"] {
			p["deprecated"] = true
		}
		var sn obj
		if raw, ok := t.kv["union"]; ok {
			sn = obj{"anyOf": jsonVal(raw)}
		} else {
			e, ok := sig[argName(pname)]
			if !ok {
				die("%s: //oc:param %s has no matching handler argument %q — the Go signature is where a parameter's type lives",
					fn.Name.Name, pname, argName(pname))
			}
			sn = typeNode(e, t)
		}
		applyTitle(sn, t, titleize(pname))
		if v, ok := t.val("default"); ok {
			sn["default"] = v
		}
		p["schema"] = sn
		params = append(params, p)
	}
	if len(params) > 0 {
		op["parameters"] = params
	}

	for _, line := range d.all("body") {
		tk := tokenize(line)
		t := parseTags(tk[1:])
		c := obj{}
		if s, ok := contentSchema(t); ok {
			c[tk[0]] = obj{"schema": s}
		} else {
			c[tk[0]] = obj{}
		}
		rb := obj{"content": c}
		if t.flag["req"] {
			rb["required"] = true
		}
		op["requestBody"] = rb
	}

	responses := obj{}
	for _, line := range d.all("resp") {
		tk := tokenize(line)
		if len(tk) < 3 {
			die("%s: //oc:resp wants CODE, CONTENT-TYPE and a description", fn.Name.Name)
		}
		code, ct := tk[0], tk[1]
		var desc string
		if err := json.Unmarshal([]byte(tk[len(tk)-1]), &desc); err != nil {
			die("%s: //oc:resp %s description is not a JSON string", fn.Name.Name, code)
		}
		t := parseTags(tk[2 : len(tk)-1])
		r := obj{"description": desc}
		if ct != "-" {
			s, ok := contentSchema(t)
			if !ok {
				die("%s: //oc:resp %s names a content type but no schema", fn.Name.Name, code)
			}
			r["content"] = obj{ct: obj{"schema": s}}
		}
		responses[code] = r
	}
	if _, ok := d.find("errors"); ok {
		for code, r := range stdErrors {
			if _, dup := responses[code]; dup {
				die("%s: response %s is declared twice (//oc:errors std plus an explicit //oc:resp)", fn.Name.Name, code)
			}
			responses[code] = r
		}
	}
	op["responses"] = responses

	if mcp, ok := d.find("mcp"); ok {
		op["x-mcp"] = jsonVal(mcp)
	}
	return method, path, op
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	"any": true, "w": true, "r": true,
}

// argName maps a wire parameter name to the handler argument that carries its
// type. Keep this identical to arg_name() in the one-shot migration script.
func argName(n string) string {
	parts := strings.Split(n, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	s := b.String()
	if s == "" {
		return "x"
	}
	s = strings.ToLower(s[:1]) + s[1:]
	if goKeywords[s] {
		return s + "_"
	}
	return s
}

// ---------------------------------------------------------------- main

func main() {
	if len(os.Args) != 3 {
		die("usage: gen <ssot.go> <out.json>")
	}
	src, out := os.Args[1], os.Args[2]

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
	if err != nil {
		die("cannot parse %s: %v", src, err)
	}

	pkg := splitDoc(file.Doc)
	version, ok := pkg.find("openapi")
	if !ok {
		die("%s package doc carries no //oc:openapi <version>", src)
	}
	infoRaw, ok := pkg.find("info")
	if !ok {
		die("%s package doc carries no //oc:info <json>", src)
	}
	stdRaw, ok := pkg.find("errors-std")
	if !ok {
		die("%s package doc carries no //oc:errors-std <json>", src)
	}
	stdErrors := obj{}
	for k, v := range jsonVal(stdRaw).(map[string]any) {
		stdErrors[k] = v
	}

	schemas := obj{}
	paths := obj{}
	for _, decl := range file.Decls {
		switch n := decl.(type) {
		case *ast.GenDecl:
			if n.Tok != token.TYPE {
				continue
			}
			for _, spec := range n.Specs {
				ts := spec.(*ast.TypeSpec)
				d := splitDoc(n.Doc)
				if _, dup := schemas[ts.Name.Name]; dup {
					die("duplicate schema %s", ts.Name.Name)
				}
				schemas[ts.Name.Name] = schemaDecl(ts, d)
			}
		case *ast.FuncDecl:
			d := splitDoc(n.Doc)
			if len(d.lines) == 0 {
				continue
			}
			method, path, op := operation(n, d, stdErrors)
			item, ok := paths[path].(obj)
			if !ok {
				item = obj{}
				paths[path] = item
			}
			if _, dup := item[method]; dup {
				die("%s %s is declared twice", strings.ToUpper(method), path)
			}
			item[method] = op
		}
	}
	if len(schemas) == 0 || len(paths) == 0 {
		die("%s yielded %d schemas and %d paths — the SSOT parsed but said nothing", src, len(schemas), len(paths))
	}

	root := obj{
		"openapi":    version,
		"info":       jsonVal(infoRaw),
		"components": obj{"schemas": schemas},
		"paths":      paths,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // the descriptions carry < and & as prose, not markup
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		die("cannot render JSON: %v", err)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		die("cannot write %s: %v", out, err)
	}
	fmt.Printf("[gen-openapi] wrote %s (%d schemas, %d paths, %d bytes)\n",
		out, len(schemas), len(paths), buf.Len())
}
