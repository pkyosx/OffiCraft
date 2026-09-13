# spec/mcp.md — the `/api/mcp` MCP transport contract (M1 wire freeze)

> Status: **frozen** (M1 spec freeze). Behavioural contract for the MCP JSON-RPC surface
> that `spec/openapi.json` cannot express: the JSON-RPC envelope, the `tools/call` argument
> split, the error mappings, the catalog derivation, and the `catalog_hash` algorithm.
> A replacement implementation MUST satisfy every MUST/MUST NOT here.
>
> Source of truth at freeze time: commit `6dd7280`. The frozen tool-catalog snapshot is
> `spec/mcp-catalog.json` (CI-gated, see §5).

## 1. Endpoint and envelope

- `POST /api/mcp` — **gated** (bearer JWT, same gate as every REST route).
- The body is ONE JSON-RPC 2.0 request object. Batch arrays are NOT supported: a non-object
  body MUST be answered with error `-32600`.
- Protocol/transport errors (bad JSON, bad envelope, unknown method/tool, bad params) MUST
  be returned as JSON-RPC `error` objects **carried in an HTTP 200**. Success envelopes are also HTTP 200. The only non-200 from the dispatcher itself
  is the notification 202 (§2).
- Envelope shapes:

```json
{"jsonrpc":"2.0","id":1,"result":{...}}
{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found: 'foo'"}}
```

- Error codes (closed set): `-32700` parse error (body not valid JSON),
  `-32600` invalid request (non-object body, or `method` not a string), `-32601` method not
  found, `-32602` invalid params (non-object `params`, non-string `name`, non-object
  `arguments`, **or unknown tool name**), `-32603` internal error
  (loopback failure). Error `message` wording is not contract; codes and their trigger
  conditions are.
- A parse/invalid-request error MUST carry `id: null` when the request id is unknowable.

## 2. Methods

| method | behaviour |
|---|---|
| `initialize` | result `{protocolVersion, capabilities:{tools:{listChanged:false}}, serverInfo:{name:"officraft", version:<VERSION>}}`. `protocolVersion` MUST echo the client's requested `params.protocolVersion` when it is a non-empty string, else default `"2025-06-18"`. |
| `ping` | result `{}` |
| `tools/list` | result `{"tools":[<descriptor>...]}` in catalog (route-table) order, narrowed to the caller's class (§4.2) |
| `tools/call` | §3 |
| `notifications/*` **or any id-less request** | fire-and-forget: MUST answer HTTP **202 with a JSON `null` body** — no JSON-RPC envelope (a request without an `id` key is a notification even outside the `notifications/` namespace) |
| anything else | error `-32601` |

## 3. `tools/call` — the argument split and result mapping

`params` MUST be an object with a string `name` and an optional object `arguments`
(absent → `{}`); violations are `-32602`.

### 3.1 Splitting the flat `arguments` object

Each tool maps 1:1 to a REST route (§4). The flat `arguments` are split back into
path / query / body:

1. **Path params**: for each `{param}` in the route's path template, pop that key from
   `arguments` and substitute `str(value)` into the path. A missing, `null`, empty, or
   whitespace-only string path value MUST return a tool result with `isError: true` and the
   REST-style 422 `validation_error` envelope (`field required: <param>`); it MUST NOT be
   substituted as an empty segment. Non-empty path values containing `/`, or equal to
   `.` or `..`, MUST return a tool result with `isError: true` and the REST-style 422
   `validation_error` envelope (`invalid path: <param>`); they MUST NOT reach loopback
   dispatch. Other non-empty path values continue to reach the loopback path handling,
   which still applies its existing `path.Clean` before dispatch.
2. **GET routes**: every remaining non-`None` key becomes a query parameter
   (form-urlencoded, `doseq` list expansion); `None` values (unset optionals) MUST be
   dropped.
3. **Non-GET routes**: the remaining keys are serialized as the JSON request body — an
   **empty object** `{}` when nothing remains (a body MUST always be sent for a write
   route). Keys are already wire aliases (§4.1), so DTO validation
   sees them by alias. A mutable tool MUST reject an argument that is not declared by
   its input schema with the loopback route's 422 validation envelope; it MUST NOT
   silently discard the argument. This also applies to nested DTO objects. Explicit
   free-form map fields remain open only where their schema declares
   `additionalProperties`.

### 3.2 In-process loopback

- The call MUST re-enter the implementation's own HTTP stack in-process, **forwarding the
  caller's `Authorization` header** verbatim, so the auth gate, DI, validation, and handler
  guards run exactly as for a direct REST call. Non-GET loopback requests carry
  `Content-Type: application/json`.
- The loopback mechanism is an implementation detail; the "same gate + same validation +
  same handler" equivalence is contract.

### 3.3 Result mapping

The sub-response `(status, raw_body)` wraps into a `CallToolResult`:

```json
{"content":[{"type":"text","text":"<raw response body as UTF-8 text>"}],
 "isError":false,
 "structuredContent":{...}}
```

- `content` MUST be a single text item carrying the raw response body (empty string for an
  empty body).
- `isError` MUST be `status >= 400`. A 4xx/5xx from the route (401/403/404/409/422 …) is a
  **successful JSON-RPC result with `isError: true`** — never a JSON-RPC error.
- `structuredContent` MUST be present **iff** the body parses as a JSON **object**. A
  top-level array (the `list_*` tools) or non-JSON body MUST omit it (the full JSON is still
  in `text`).
- An exception escaping the loopback itself maps to JSON-RPC error `-32603`.

## 4. The tool catalog — derived, never hand-listed

The catalog is NOT a hand-maintained list: it MUST be derived from the authoritative
per-operation definitions, keeping every operation **not** excluded, in declared order.
Today that authority is the `x-mcp` block carried by each operation in `spec/openapi.json`:
`include: true` puts the operation on the tool surface and `order` fixes its position;
`include: false` keeps it off. That included set mirrors the implementation's single route
table (the rows **not** flagged `mcp_exclude`).
The following checks hold the three representations together:

- `make drift-mcp-catalog` re-renders `spec/mcp-catalog.json` from `spec/openapi.json` and
  byte-diffs it against the committed file (§5) — openapi → catalog.
- `conformance/test_mcp.py::test_catalog_hash_keys_off_tool_surface_only` asserts the
  manifest's `mcp_tool` column equals the catalog's names, in order — route table → catalog.
- `conformance/test_mcp.py::test_tools_list_equals_frozen_snapshot_elementwise` asserts the
  LIVE list equals the catalog narrowed to the caller's class (see §4.2) — catalog → wire.

**Counts are deliberately not written down here** — how the committed catalog is produced and
pinned is §5.

### 4.2 `tools/list` is narrowed to the caller

The catalog above is the WHOLE tool surface. What a `tools/list` call returns is that catalog
filtered to the tools whose route row the CALLER could get past: for each descriptor, the
route table's `Requires` for that tool name is compared against the caller's principal class
(`machine < agent < admin_agent < owner`; a `public` row has no class requirement and is
listed for every authenticated caller). Catalog order is preserved among the survivors, and
descriptors are served verbatim — the filter only removes.

The filter keys off the ROUTE TABLE and nothing else, so there is no second list to keep in
step. It is **advertising, not authorization**: an unlisted tool called by name still reaches
its own route's 403. Do not turn `tools/list` into the boundary — if it ever becomes one, a
listing bug becomes a security bug.

Each tool descriptor is exactly:

```json
{"name":"<spec.tool_name>","description":"<spec.summary>","inputSchema":{...}}
```

### 4.1 `inputSchema` assembly rules

One flat `{type:"object", properties, required?, $defs?}` object, merged in this order:

1. **Body DTO** — the route's body-DTO JSON schema contributes its properties, required
   list, and `$defs` hoisted to the top level (so `$ref` links resolve). Wire aliases
   (e.g. the chat-message sender field is `"from"` on the wire) MUST be the property names.
2. **Path params** — every `{param}` in the path template becomes a **required**
   `{"type":"string"}` property.
3. **Remaining scalar params** — each becomes an **optional** property under its wire alias
   (query alias respected), typed by JSON-scalar mapping bool→boolean, int→integer,
   float→number, everything else (after unwrapping optional/nullable types) →string.
   Implementation-internal parameters (dependency-injection seams, the raw request object)
   MUST NOT appear as tool arguments.

`required` is emitted only when non-empty; `$defs` only when present.

The frozen implementation reflects handler signatures at runtime; a rewrite MAY make the
schemas explicit/static — **byte-equality of the emitted catalog against
`spec/mcp-catalog.json` is the contract**, not the derivation mechanism (though deriving
from the authoritative definitions — see §5 — is required precisely to prevent a second
drifting list).

## 5. `spec/mcp-catalog.json` — the committed generated snapshot

- **It is generated, not hand-edited.** `bin/gen-mcp-catalog` renders it from the `x-mcp`
  metadata on `spec/openapi.json`'s operations; the committed file is that render checked
  in. Run `bin/gen-mcp-catalog` (it writes `spec/mcp-catalog.json` by default; pass a path
  to render elsewhere) and commit the output in the same batch as the `spec/openapi.json`
  edit that caused it.
- **A byte-diff gate stops the committed file going stale.** `make drift-mcp-catalog`
  re-renders into a temp file and `diff -u`s it against the committed bytes — wired into
  `bin/ci.sh` and into the drift cell of `.github/workflows/ci.yml`. A **separate** guard,
  `bin/tests/mcp-catalog-generator.sh` (dispatched from `bin/tests/run.sh`), exercises the
  generator itself against mutated inputs. The two answer different
  questions — *has the committed file drifted from its source* vs *is the generator still
  honest* — and neither substitutes for the other: a provably correct generator still leaves
  a stale catalog on disk if nobody re-runs it, and that stale file is what the wire serves.
- The other direction is pinned as well: ocserverd serves `tools/list` straight out of the
  committed snapshot (`server/ocserverd/assets.go` + `mcp.go`) — descriptors verbatim, only
  narrowed per §4.2 — so the descriptor surface cannot drift from the file by construction,
  and `conformance/test_mcp.py` asserts a LIVE `tools/list` equals the snapshot's `tools`
  array element-wise once per principal class.
- Changing the tool surface therefore stays spec-first, and **the first edit is no longer
  this file**: edit the operation's `x-mcp` in `spec/openapi.json` (owner walkthrough) →
  `bin/gen-mcp-catalog` → commit both → then the code.
- ⚠️ **Transitional (T-2590):** `x-mcp.legacy.descriptor` still carries each tool's
  descriptor as a verbatim JSON fragment, which is what makes the render byte-identical to
  the catalog frozen at M1. Until those fragments are unfolded into real derivation from the
  DTO/param definitions, changing a tool's wire shape means editing that fragment. The
  generator cross-checks the fragment's `name` and `description` against `x-mcp` and refuses
  when those disagree; it does **not** look inside the fragment's `inputSchema`.
- JSON key order within an object is not significant on the live wire; the committed file
  is kept sorted-key/2-space/trailing-newline so it byte-diffs cleanly by hand.

## 6. `catalog_hash` — the agent-restart signal

Served in `GET /api/version` (`catalog_hash`) and `GET /version`.
Two independent implementations MUST compute the **identical** value, or agents will
falsely detect a catalog change and restart.

> ⚠️ **The name promises a consumer that does not exist today.** Nothing under `cli/`
> reads this field — verified 2026-08-07 (T-77b4): `catalog_hash|CatalogHash` matches
> **0** lines under `cli/`, against **23** under `server/` as a positive control, so the
> zero is the search working rather than the search being broken. So "agents will
> falsely detect a catalog change and restart" describes an intended contract, **not an
> observable behaviour of the shipped agents** — no agent restarts on this value.
> This matters when sizing the blast radius of a catalog change: T-77b4 moved this value
> (it added `get_version` to the surface) and, on the evidence above, nothing restarted.
> Left as an intent statement rather than deleted, because the two-implementation
> equality requirement above IS still enforced (`conformance/test_mcp.py` recomputes it).

Normative algorithm:

1. Enumerate the route table and keep every route NOT flagged `mcp_exclude` — exactly the
   routes that become MCP tools (the same filter §4 applies, so the hash keys off the
   identical tool surface `spec/mcp-catalog.json` freezes).
2. Render each kept route as the string `"{METHOD} {path}"` — uppercase HTTP method, single
   space, the path template with `{param}` placeholders as written in the table (e.g.
   `"GET /api/members"`, `"POST /api/members/{member_id}/context"`).
3. Sort the strings lexicographically (order-independence: reordering the route table does
   NOT change the hash).
4. Join with `"\n"` (no trailing newline), UTF-8 encode, SHA-256.
5. The hash is the **first 16 lowercase hex chars** of the digest.

Deliberately EXCLUDED from the input: tool descriptions, input schemas (DTO shapes), and
auth requirements — the hash signals "the STATION'S tool surface changed" (add/remove/move a
route), not "a schema field changed". Schema-level drift is caught by the CI wire-freeze
gate over `spec/mcp-catalog.json` instead.

This hash describes the station's complete tool surface, not one caller's visible subset.
Changing only a route's authorization floor does not change the hash because authorization
requirements are excluded from the input. A per-caller change signal would require a separate
value on an authenticated route.

## 7. Not in this contract

- Descriptor caching and reflection mechanics — implementation-free.
- JSON key ordering / whitespace on the live wire.
- JSON-RPC batch support (explicitly absent — a batch array is `-32600`, §1).
