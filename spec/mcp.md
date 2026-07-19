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
| `tools/list` | result `{"tools":[<descriptor>...]}` in catalog (route-table) order |
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
   `arguments` and substitute `str(value)` into the path. A missing path key substitutes the
   **empty string** (no error at this layer — the loopback route then 404s/405s naturally).
2. **GET routes**: every remaining non-`None` key becomes a query parameter
   (form-urlencoded, `doseq` list expansion); `None` values (unset optionals) MUST be
   dropped.
3. **Non-GET routes**: the remaining keys are serialized as the JSON request body — an
   **empty object** `{}` when nothing remains (a body MUST always be sent for a write
   route). Keys are already wire aliases (§4.1), so DTO validation
   sees them by alias.

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

## 4. The tool catalog — reflected from the route table

The catalog is NOT a hand-maintained list: it MUST be derived from the implementation's
single route table, keeping every route **not** flagged `mcp_exclude`, in table
order. At freeze this yields **37 tools** (of 54 routes; 17 route rows
are `mcp_exclude` — ops probes, login/mint, the SSE stream, installer/binary, the MCP
endpoint itself).

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
from the route table is strongly recommended to prevent a second drifting list).

## 5. `spec/mcp-catalog.json` — the frozen snapshot

- `bin/dump-mcp-catalog` emits `{"tools":[...]}` — exactly the `tools/list` payload —
  deterministically (sorted keys, 2-space indent, trailing newline; no DB, no server).
- The committed `spec/mcp-catalog.json` is the frozen wire truth. CI (bin/ci.sh step 10)
  MUST fail when a fresh dump differs; changing the tool surface is spec-first: edit the
  snapshot (owner walkthrough) → then the code.
- A live `tools/list` result MUST equal the snapshot's `tools` array (element-wise; JSON key
  order within an object is not significant on the live wire, but the dump normalizes with
  sorted keys for byte-diffs).

## 6. `catalog_hash` — the agent-restart signal

Served in `GET /api/version` (`catalog_hash`) and `GET /version`.
Two independent implementations MUST compute the **identical** value, or agents will
falsely detect a catalog change and restart.

Normative algorithm:

1. Enumerate the route table and keep every route NOT flagged `mcp_exclude` — exactly the
   routes that become MCP tools (the same filter §4 applies, so the hash keys off the
   identical tool surface `tools/list` serves and `spec/mcp-catalog.json` freezes).
2. Render each kept route as the string `"{METHOD} {path}"` — uppercase HTTP method, single
   space, the path template with `{param}` placeholders as written in the table (e.g.
   `"GET /api/members"`, `"POST /api/members/{member_id}/context"`).
3. Sort the strings lexicographically (order-independence: reordering the route table does
   NOT change the hash).
4. Join with `"\n"` (no trailing newline), UTF-8 encode, SHA-256.
5. The hash is the **first 16 lowercase hex chars** of the digest.

Deliberately EXCLUDED from the input: tool descriptions, input schemas (DTO shapes), and
auth requirements — the hash signals "the set of callable tools changed" (add/remove/move a
route), not "a schema field changed". Schema-level drift is caught by the CI wire-freeze
gate over `spec/mcp-catalog.json` instead.

## 7. Not in this contract

- Descriptor caching and reflection mechanics — implementation-free.
- JSON key ordering / whitespace on the live wire.
- JSON-RPC batch support (explicitly absent — a batch array is `-32600`, §1).
