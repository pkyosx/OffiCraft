# The `//oc:` directive vocabulary

`spec/openapi.json` is a 100% generated artifact (owner ruling `rc-d6826c70b6ee` [0]).
Its ONE source is `spec/ocapi/ocapi.go`; `spec/ocapi/gen` renders it and
`bin/gen-openapi` is the handle. **Never hand-edit `spec/openapi.json`** — the
`drift-openapi` gate re-renders and diffs, so a hand edit comes back red.

## Why directives at all

A Go type carries the shape and nothing else. Everything the wire contract also
carries — the prose, the titles FastAPI once minted, `format`, `default`,
`enum`, `additionalProperties`, the routes — has nowhere to live in a Go type
declaration, so it lives in comments and the renderer reads them with `go/ast`
(not `reflect`, which cannot see a comment). That is the whole reason this
vocabulary exists; it is not decoration.

## Why the prose is a JSON string and not a plain doc comment

Because `gofmt` REWRITES free prose in doc comments. It turns ``` ``x`` ``` into
smart quotes and inserts blank `//` lines between paragraphs — on this spec's
text, 2,973 changed lines. A doc comment is therefore not a safe place to hold
bytes that must survive verbatim. `gofmt` leaves *directive* lines
(`//word:something`, no space after the colon) alone, so every description is a
`//oc:doc "<JSON string>"` line and the whole file is `gofmt`-stable
byte-for-byte. Verify with `gofmt -l spec/ocapi`.

## Package level (the `package ocapi` doc comment)

| directive | meaning |
|---|---|
| `//oc:openapi <version>` | the `openapi` field |
| `//oc:info <json object>` | the `info` block, verbatim |
| `//oc:errors-std <json object>` | the standard `422`/`4XX`/`5XX` error-envelope response block that `//oc:errors std` expands to. 173 of 183 operations share it byte for byte; spelling it out per operation would be 173 copies of one decision |

## Everywhere

| directive | meaning |
|---|---|
| `//oc:doc "<JSON string>"` | the `description` of the schema / property / operation it sits on. Absent = no `description` key |

## Schema level (a `type` declaration's doc comment)

`//oc:schema <flags…>`:

| tag | meaning |
|---|---|
| `notitle` | the schema has NO `title` key |
| `title="…"` | explicit `title`, when it differs from the Go type name (the default) |
| `ap=true` / `ap=false` | `additionalProperties` |
| `emptyreq` | the schema declares `required: []` — an explicit empty list, which is NOT the same as omitting the key. Five DTOs carry it and it is preserved rather than tidied away |
| `enum=[…]` + `etype=<type>` | an enum schema. The Go declaration is `type X string` |
| `freeroot` | `{"type": "object"}` with no `properties` |

## Field level (a struct field's doc comment)

`//oc:field <flags…>`. The field's Go type carries the shape; these carry the rest.

| tag | meaning |
|---|---|
| `req` | the property is in the schema's `required` |
| `null` | nullable — renders as `anyOf: [<the type>, {"type": "null"}]`, with `title`/`description`/`default` hoisted OUTSIDE the `anyOf`, which is where the spec puts them |
| `notitle` | no `title` key |
| `title="…"` | explicit `title`, when it differs from the default (the JSON name split on `_`, each word capitalised) |
| `default=<json>` | the `default` value. `default=null` is a real default, distinct from having none |
| `fmt=<format>` | `format` on a string |
| `minLength=<n>` | `minLength` on a string |
| `penum=[…]` | `enum` on the property itself |
| `ap=true` / `ap=false` | `additionalProperties` on an object-typed property (`map[string]any` or an inline `struct{…}`) |
| `vtitle="…"` | `title` on the `additionalProperties` VALUE schema of a `map[string]T` |
| `allof` | wrap the `$ref` in a single-member `allOf` — the 3.0 idiom the spec uses where a `$ref` needs a sibling `title`/`default` |
| `union=[…]` | the property is a raw `anyOf` of several members. The Go type is `any` |
| `writeonly` | `writeOnly: true` |
| `xgotype="…"` | the `x-go-type` extension |

`req`/`null` are the four-quadrant part: nullable and required are independent
axes, and all four combinations occur.

### Go type → schema node

`string` · `bool` · `int` · `int64` (`format: int64`) · `float32` (`type:
number`) · `float64` (`format: double`) · `[]T` · `map[string]any` (free object)
· `map[string]T` (typed `additionalProperties`) · `struct{…}` (inline object,
nests) · `any` (`{}` — no `type` at all) · any other identifier → `$ref` to that
schema. A leading `*` is ignored; it is there so the Go file reads the way a Go
file should.

## Operation level (a `func` declaration's doc comment)

| directive | meaning |
|---|---|
| `//oc:route <METHOD> <path>` | where the operation lives |
| `//oc:id <operationId>` | |
| `//oc:summary "<JSON string>"` | |
| `//oc:param <in> <name> [req] [<tags…>]` | one parameter. Its TYPE comes from the handler's Go signature, not from a tag — that is the point of the func being a func. Accepts `title=`/`notitle`, `default=`, `penum=`, `union=`, `fmt=`, `deprecated`, `desc="…"` |
| `//oc:body <content-type> [req] <schema-tag>` | request body |
| `//oc:resp <code> <content-type\|-> <schema-tag> "<description>"` | one response. `-` plus `none` means a response with no content |
| `//oc:errors std` | expand the shared `//oc:errors-std` block |
| `//oc:mcp <json>` | the `x-mcp` extension the MCP catalog is rendered from |

A `<schema-tag>` is one of `ref=<SchemaName>`, `inline=<json>`, `empty`
(`{}`) or `none`.

The handler-argument name for a parameter is the wire name in lowerCamelCase,
with `_` appended when that collides with a Go keyword or with `w`/`r`
(`argName` in `gen/main.go`). A parameter whose argument is missing is a hard
failure, not a silent default.

## Normalisation

The renderer emits keys in sorted order and `required` members sorted, always.
That is a ONE-TIME reshuffle of the committed file, ruled acceptable up front;
it is what makes the output a function of the source alone rather than of the
order somebody happened to type things in. `<`, `>` and `&` are NOT
HTML-escaped — they appear in the prose as prose.

## What is NOT checked here

- This module is not under `cli/*` or `server/*`, so `lint-go-fmt`,
  `lint-go-vet` and `build-go` do NOT reach it (their loops glob those two
  directories). It cannot go there: `build-go` runs `go build -o <binary> ./...`,
  which fails outright the moment a module contains more than one package.
  Run `gofmt -l spec/ocapi` and `go vet ./...` here by hand.
- The renderer proves the spec matches THIS file. It does not prove the file
  matches the server; that is what the conformance suite is for.
