"""REST happy-path face — every manifest row, minimum-viable identity, spec shape.

Second conformance batch. The auth matrix (test_auth_matrix.py) pins WHO may
call each route; this file pins WHAT a permitted call returns: for every row of
``routes_manifest.json`` a concrete happy request is fired as the route's
lowest-friction passing identity (owner for admin/owner routes, a scratch agent
for the self-report rows, anonymous for public rows) and the response is
validated against the committed ``spec/openapi.json`` declaration:

  * the expected success status (200 for every JSON route today);
  * the response body against the spec's declared response schema
    (``schema_check.violations`` — $ref/required/type/anyOf subset);
  * per-row semantic ``check`` hooks for contract points the schema cannot
    express (echoes, token-null rules, catalog equality, binary round-trips).

Coverage has the same teeth as the matrix: ``test_happy_covers_manifest``
fails the run when a manifest row is neither in ``HAPPY`` nor in the explicit
``SKIPPED_HAPPY`` table (reason required), and
``test_openapi_covers_manifest`` pins the manifest row set to the frozen
``spec/openapi.json`` operations. Both of those compare the manifest to another
hand-written list; what carries them back to the server is the Go test
``TestRouteTableCoversSpecSurface``, which pins that same frozen spec against
the route table the mux is built from — so a served route the manifest never
learned about reddens there.

Rows that serve non-JSON bytes (binaries, install.sh, chat attachment blob) or
a non-OpenAPI protocol (MCP JSON-RPC) carry ``nonjson`` with a reason: status
is still asserted and a semantic ``check`` replaces schema validation.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import pathlib
import struct
import time
import urllib.parse
import uuid
from dataclasses import dataclass, field
from typing import Any, Callable

import httpx
import pytest

from conftest import AgentIdentity, hire_member, mint_member_token
from schema_check import violations

HERE = pathlib.Path(__file__).resolve().parent
SPEC = json.loads((HERE.parent / "spec" / "openapi.json").read_text(encoding="utf-8"))
MCP_CATALOG = json.loads(
    (HERE.parent / "spec" / "mcp-catalog.json").read_text(encoding="utf-8")
)

# A 1x1 transparent PNG — a REAL image payload so the attachment round-trip
# also exercises the is_image/gallery face (mime sniffing stays honest).
_PNG_B64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8"
    "z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)
_PNG_BYTES = base64.b64decode(_PNG_B64)


def _auth(token: str | None) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"} if token else {}


# ── TOTP, computed here from the RFC rather than from the server's code ───────
# 🔴 THIS IS THE INTEROP PROOF, and it is why it is hand-rolled from stdlib
# instead of imported: this suite is a language-agnostic BLACK BOX (the run
# script greps for server imports and fails if it finds any). A code computed
# independently from RFC 6238 — HMAC-SHA1, 6 digits, 30s step, the triple every
# authenticator app implements — verifying against this server is the only
# evidence in the tree that a real phone will work. Reusing the server's own
# helper would prove the server agrees with itself.
_TOTP_STEP = 30
_TOTP_DIGITS = 6


def _totp_key(secret: str) -> bytes:
    """Decode a base32 TOTP secret the way an authenticator does (unpadded,
    case-insensitive). Raises on anything an app could not consume."""
    cleaned = secret.strip().replace(" ", "").replace("-", "").upper()
    pad = "=" * (-len(cleaned) % 8)
    key = base64.b32decode(cleaned + pad)
    assert key, f"empty TOTP key from {secret!r}"
    return key


def _totp_align_to_step_start(min_headroom: float = 12.0) -> None:
    """Wait out the tail of the current 30s step when little of it is left.

    🔴 WHY THE CEREMONY NEEDS THIS. The server accepts a code from the current
    step ±1 — exactly THREE steps at any instant — and it SPENDS each step it
    accepts (single-use codes). A ceremony with three code-consuming operations
    (activate, login, disable) therefore needs all three slots, and they only
    stay valid if the window does not slide mid-test. Starting 2s before a step
    boundary would move the window under us and invalidate the earliest slot.
    Waiting a few seconds is far cheaper (and far less flaky) than sleeping a
    whole step between operations.
    """
    remaining = _TOTP_STEP - (time.time() % _TOTP_STEP)
    if remaining < min_headroom:
        time.sleep(remaining + 0.5)


def _totp_code(secret: str, at: float | None = None, step_offset: int = 0) -> str:
    """RFC 6238 code. ``step_offset`` shifts by whole 30s steps — needed because
    the server SPENDS each step it accepts (replay defence), so the code that
    armed the factor cannot also open a session."""
    counter = int((at if at is not None else time.time()) // _TOTP_STEP) + step_offset
    digest = hmac.new(_totp_key(secret), struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    truncated = struct.unpack(">I", digest[offset : offset + 4])[0] & 0x7FFFFFFF
    return str(truncated % (10 ** _TOTP_DIGITS)).zfill(_TOTP_DIGITS)


@dataclass
class HCtx:
    """Everything a happy row needs to build its concrete request."""

    client: httpx.Client
    owner_token: str
    agent: AgentIdentity  # this file's OWN scratch agent (self-report rows)
    machine_id: str  # this file's OWN scratch machine (mutating machine rows)
    fresh_member: Callable[[], str]
    fresh_machine: Callable[[], str]
    fresh_role: Callable[[], str]
    _attachment: tuple[str, bytes] | None = field(default=None, repr=False)
    _put_theme_id: str | None = field(default=None, repr=False)
    _avatar_to_delete_url: str | None = field(default=None, repr=False)

    def token(self, identity: str) -> str | None:
        return {"owner": self.owner_token, "agent": self.agent.token, "none": None}[
            identity
        ]

    def attachment(self) -> tuple[str, bytes]:
        """Lazily seed ONE chat attachment (owner → happy agent); cached so the
        chat rows share a single fixture regardless of execution order."""
        if self._attachment is None:
            r = self.client.post(
                "/api/chat",
                json={
                    "to": self.agent.member_id,
                    "body": "conformance attachment seed",
                    "attachments": [
                        {
                            "data_b64": _PNG_B64,
                            "filename": "conf.png",
                            "mime": "image/png",
                        }
                    ],
                },
                headers=_auth(self.owner_token),
            )
            assert r.status_code == 200, f"attachment seed failed: {r.text}"
            atts = r.json()["attachments"]
            assert len(atts) == 1 and atts[0]["id"], f"bad attachment echo: {atts}"
            self._attachment = (atts[0]["id"], _PNG_BYTES)
        return self._attachment


@pytest.fixture(scope="session")
def hctx(
    client, owner_token, fresh_member, fresh_machine, fresh_role
) -> HCtx:
    member_id = hire_member(client, owner_token, "conf-happy-agent")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return HCtx(
        client=client,
        owner_token=owner_token,
        agent=AgentIdentity(member_id=member_id, token=token, role_key=""),
        machine_id=fresh_machine(),
        fresh_member=fresh_member,
        fresh_machine=fresh_machine,
        fresh_role=fresh_role,
    )


# ── The happy table ──────────────────────────────────────────────────────────

PathLike = str | Callable[[HCtx], str]
CheckFn = Callable[[HCtx, httpx.Response], None]


@dataclass
class Happy:
    """One happy row. ``identity`` is the LOWEST-friction identity that passes
    the route's authz gate; ``status`` the spec success status; ``nonjson`` a
    reason string for rows whose body is not spec-schema JSON."""

    identity: str = "owner"  # "owner" | "agent" | "none"
    path: PathLike | None = None  # default: the manifest template (no params)
    body: Any = None
    status: int = 200
    nonjson: str = ""
    check: CheckFn | None = None


def _check_cost_reset_receipt(_ctx: HCtx, r: httpx.Response) -> None:
    """A fresh member has never reported telemetry and has banked nothing, so
    the receipt honestly says NOTHING WAS DESTROYED — both halves null rather
    than 0. That distinction is the wire contract (null = there was nothing to
    clear; 0 would read as "zero was cleared"), and it is what lets the cockpit
    fall back to the existing "both null -> dash" rule after a reset instead of
    rendering $0."""
    data = r.json()
    assert data["member_id"], data
    assert data["cleared_cost"] is None, data
    assert data["cleared_banked_cost"] is None, data


def _check_account_cost_reset_receipt(_ctx: HCtx, r: httpx.Response) -> None:
    """Nobody has ever reported under a scratch account tag, so the receipt
    honestly says NOTHING WAS DESTROYED — null rather than 0. Two contracts ride
    on that: null means "there was nothing to clear" while 0 would read as "zero
    was cleared" (the same null semantics as CostResetDTO), and an unknown tag is
    a SUCCESS rather than a 404, because an account is a free telemetry string
    with no roster row — "no such account" and "that account has nothing to
    clear" are the same state. Without the second, the owner's second press —
    the likely one, having just cleared it — would look like an error."""
    data = r.json()
    assert data["account"], data
    assert data.get("cleared_cost") is None, data


def _check_version(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["version"] and data["catalog_hash"], data


def _check_login(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["token"] and data["token_type"] == "bearer", data


def _check_signing_keys(_ctx: HCtx, r: httpx.Response) -> None:
    """The ring as the outside may see it: ids, creation times, exactly one
    signer — and NOTHING that could be key material.

    The leak check is deliberately structural rather than a list of field names
    to forbid: it walks every key object and asserts its key set is exactly the
    three documented fields, so a future field carrying a fingerprint, a hash
    prefix or the key itself reddens here without anyone having predicted its
    name. (Why it matters: on an install predating the ring the signing key IS
    SHA-256 over the owner password, so publishing any digest of it is an
    offline dictionary attack on that password.)"""
    data = r.json()
    keys = data["keys"]
    assert keys, data
    assert sum(1 for k in keys if k["is_signing"]) == 1, data
    for k in keys:
        assert set(k) == {"key_id", "created_ts", "is_signing"}, k
        assert isinstance(k["key_id"], str) and k["key_id"], k
        assert isinstance(k["created_ts"], (int, float)), k


def _check_signing_key_rotated(ctx: HCtx, r: httpx.Response) -> None:
    """A rotation puts a BRAND-NEW key in charge: after the call the ring holds
    at least two keys and the one signing is the newest by ``created_ts``.

    ⚠️ What this row does NOT prove is that the rotation dropped nothing — that
    needs a reading taken BEFORE the call, which a response-only check cannot
    take. It is pinned where a before/after IS available:
    TestT62_RotationTakesEffectWithoutRestart and
    TestT62_RetiredKeyVerifiesButNeverSigns
    (server/ocserverd/keyring_rotation_t62_test.go), which mint a token under the
    outgoing key and require it to keep passing the live gate afterwards. Saying
    so here rather than writing a check that cannot see it: a subset assertion
    against a ring re-read after the fact would pass no matter what the route
    did."""
    _check_signing_keys(ctx, r)
    keys = r.json()["keys"]
    assert len(keys) >= 2, keys
    signing = [k for k in keys if k["is_signing"]][0]
    assert signing["created_ts"] == max(k["created_ts"] for k in keys), keys
    assert signing["created_ts"] > 0, signing


def _happy_signing_key_remove_path(ctx: HCtx) -> str:
    """Aim the removal at a key that signed NOTHING anyone is holding.

    Rotate twice: the key created by the first rotation signs only between the
    two calls, and this harness mints no credential in that window. Removing THAT
    key is the route's full semantics with none of the poisoning — removing the
    ORIGINAL key would revoke the very token this request authenticates with,
    and every row after it.

    If the ring cannot produce such a key the row fails rather than silently
    aiming somewhere harmless: a removal probe that never removes anything is
    the failure mode this file exists to prevent."""
    hdr = _auth(ctx.owner_token)
    before = {
        k["key_id"]
        for k in ctx.client.get("/api/auth/signing-keys", headers=hdr).json()["keys"]
    }
    ctx.client.post("/api/auth/signing-keys/rotate", headers=hdr)
    mid = [
        k
        for k in ctx.client.get("/api/auth/signing-keys", headers=hdr).json()["keys"]
        if k["key_id"] not in before
    ]
    assert len(mid) == 1, mid
    ctx.client.post("/api/auth/signing-keys/rotate", headers=hdr)
    return f"/api/auth/signing-keys/{mid[0]['key_id']}/remove"


def _check_signing_key_removed(ctx: HCtx, r: httpx.Response) -> None:
    """The removed key is gone from the ring the call answers with, and the ring
    still has a signer (a removal that left the server unable to mint would be a
    far worse outcome than a refused one)."""
    _check_signing_keys(ctx, r)
    removed = r.request.url.path.split("/")[-2]
    assert removed not in {k["key_id"] for k in r.json()["keys"]}, (removed, r.json())


def _happy_mfa_enroll_path(ctx: HCtx) -> str:
    """Seed the ship-dark flag so enrol answers 200 rather than 403. Inert:
    offering the factor arms nothing, so no later login fixture needs a code."""
    ctx.client.post(
        "/api/auth/mfa/offer",
        json={"offered": True},
        headers=_auth(ctx.owner_token),
    )
    return "/api/auth/mfa/enroll"


def _check_mfa_state(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert isinstance(data["offered"], bool), data
    assert isinstance(data["enrolled"], bool), data
    # A secret is disclosed exactly once, by enrol — never by the state read.
    assert data["secret"] is None and data["otpauth_uri"] is None, data


def _check_mfa_offer(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["offered"] is True, data
    assert data["enrolled"] is False, "offering the feature must not arm anything"


def _check_mfa_enroll(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    # enroll hands back a usable secret AND must NOT claim the factor is armed:
    # a UI that trusted `enrolled` here would tell the owner they are protected
    # while the next login still takes the password alone.
    assert data["enrolled"] is False, data
    assert data["secret"], data
    assert data["otpauth_uri"].startswith("otpauth://totp/"), data
    # The secret must be decodable base32 — an authenticator cannot use anything
    # else, and a malformed one fails only later, on the phone.
    _totp_key(data["secret"])


def _check_install_sh(_ctx: HCtx, r: httpx.Response) -> None:
    # lifecycle.md §5: templates the request base URL + token into text/plain.
    assert r.headers.get("content-type", "").startswith("text/plain"), r.headers
    assert "conf-happy-boot-token" in r.text, "token not templated into script"


def _check_binary(_ctx: HCtx, r: httpx.Response) -> None:
    assert len(r.content) > 0, "binary route served an empty body"


def _check_mcp_tools_list(_ctx: HCtx, r: httpx.Response) -> None:
    # mcp.md: JSON-RPC over HTTP 200; tools/list serves the committed catalog.
    payload = r.json()
    assert payload.get("jsonrpc") == "2.0" and payload.get("id") == 1, payload
    assert "error" not in payload, payload
    tools = payload["result"]["tools"]
    served = {t["name"] for t in tools}
    committed = {t["name"] for t in MCP_CATALOG["tools"]}
    assert served == committed, (
        f"tools/list drifted from spec/mcp-catalog.json: "
        f"served-only={sorted(served - committed)} "
        f"catalog-only={sorted(committed - served)}"
    )
    for tool in tools:
        assert isinstance(tool.get("inputSchema"), dict), f"tool without inputSchema: {tool}"


def _check_upload_ref(ctx: HCtx, r: httpx.Response) -> None:
    # The upload answers the light ref; the stored bytes serve back verbatim.
    ref = r.json()
    assert ref["id"].startswith("att-"), ref
    assert ref["mime"] == "image/png" and ref["filename"] == "conf-upload.png", ref
    served = ctx.client.get(
        f"/api/chat/attachment/{ref['id']}",
        headers=_auth(ctx.token("agent")),
    )
    assert served.status_code == 200 and served.content == _PNG_BYTES


def _check_attachment_roundtrip(ctx: HCtx, r: httpx.Response) -> None:
    _att_id, payload = ctx.attachment()
    assert r.content == payload, "attachment bytes did not round-trip"


def _seeded_avatar_delete_path(ctx: HCtx) -> str:
    """Give the DELETE happy row its own precondition.

    Parametrized route order is not a contract, so the adjacent PUT row cannot
    be the setup: without this seed a broken no-op DELETE still answers the
    same empty avatar_url and the conformance row stays green.
    """
    path = f"/api/members/{ctx.agent.member_id}/avatar"
    seeded = ctx.client.put(
        path + "?filename=conf-delete.png&mime=image/png",
        content=_PNG_BYTES,
        headers=_auth(ctx.owner_token),
    )
    assert seeded.status_code == 200, f"avatar delete seed failed: {seeded.text}"
    url = seeded.json()["avatar_url"]
    assert url.startswith("/api/chat/attachment/ava-"), seeded.json()
    ctx._avatar_to_delete_url = url
    return path


def _check_avatar_delete(ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["member_id"] == ctx.agent.member_id and data["avatar_url"] == "", data
    assert ctx._avatar_to_delete_url is not None, "DELETE row ran without its seed"
    old = ctx.client.get(
        ctx._avatar_to_delete_url,
        headers=_auth(ctx.owner_token),
    )
    assert old.status_code == 404, (
        f"DELETE left its previously referenced blob reachable: "
        f"{old.status_code} {old.text[:200]}"
    )
    ctx._avatar_to_delete_url = None


def _check_bootstrap_preview(_ctx: HCtx, r: httpx.Response) -> None:
    # lifecycle.md §2.3: a UI preview (no member_id) MUST get token: null.
    data = r.json()
    assert data["token"] is None, f"preview bootstrap minted a token: {data}"
    assert data["role"] and data["context"], data


def _check_share_link_shape(ctx: HCtx, r: httpx.Response) -> None:
    att_id, _payload = ctx.attachment()
    url = r.json()["url"]
    assert url.startswith(f"/api/chat/attachment/{att_id}?sig="), url
    sig = url.split("sig=", 1)[1]
    assert sig and "&" not in sig, f"malformed sig segment: {url}"


def _diff_pair_path(ctx: HCtx) -> str:
    att_id, _payload = ctx.attachment()
    return "/api/diff?" + urllib.parse.urlencode({"before": att_id, "after": att_id})


def _check_diff_pair(ctx: HCtx, r: httpx.Response) -> None:
    att_id, _payload = ctx.attachment()
    d = r.json()
    for name in ("before", "after"):
        side = d[name]
        assert side["address"] == att_id, side
        assert side["gone"] is False, side
        # The side carries the RESOLVED content and the stored mime — not a
        # second address the reader would have to fetch. (The fixture is a PNG,
        # so `text` is those bytes as a string; what is pinned here is that the
        # field is present and the mime came along, not the bytes.)
        assert isinstance(side["text"], str) and side["text"], side
        assert side["mime"] == "image/png", side


def _check_diff_share_link(ctx: HCtx, r: httpx.Response) -> None:
    """The minted link is SERVER-RELATIVE, carries the same four parameters, and
    reads the pair with NO credential at all — the whole point of the external
    flavour."""
    att_id, _payload = ctx.attachment()
    url = r.json()["url"]
    assert url.startswith("/diff?"), url
    query = urllib.parse.parse_qs(urllib.parse.urlparse(url).query)
    assert query["before"] == [att_id] and query["after"] == [att_id], query
    assert query.get("sig") and query["sig"][0], f"no signature on the external link: {url}"

    signed = "/api/diff?" + urllib.parse.urlparse(url).query
    anon = ctx.client.get(signed)
    assert anon.status_code == 200, f"credential-less read failed: {anon.status_code} {anon.text}"
    assert anon.json()["before"]["address"] == att_id, anon.text

    # Replacing the last base64url character has to CHANGE it: when the
    # signature already ends in "X" the "tampered" url is the original, the
    # server answers 200 and a healthy build fails the assertion below about
    # once in every 64 runs. The Go twin (api_diff_test.go) handles the same
    # collision; this copy did not.
    sig = query["sig"][0]
    tampered_sig = sig[:-1] + ("Y" if sig.endswith("X") else "X")
    tampered = signed.replace("sig=" + sig, "sig=" + tampered_sig)
    bad = ctx.client.get(tampered)
    assert bad.status_code == 401, f"a tampered sig must be 401: {bad.status_code} {bad.text}"


def _seeded_chat_path(template: str) -> Callable[[HCtx], str]:
    def build(ctx: HCtx) -> str:
        ctx.attachment()  # ensure at least one message/attachment exists
        return f"{template}?with={ctx.agent.member_id}"

    return build


def _nonempty_list(_ctx: HCtx, r: httpx.Response) -> None:
    assert isinstance(r.json(), list) and r.json(), "expected a non-empty list"


def chat_messages(r: httpx.Response) -> list:
    """The rows out of a ``GET /api/chat`` response.

    That route answers an OBJECT since T-48 — ``{messages, next_cursor}`` — on
    EVERY parameter combination, because a bare array had nowhere to say
    "there is more in this direction". Reading the body through here rather
    than indexing it directly means the envelope is re-asserted by every chat
    test in the suite, and a regression to the bare array fails all of them
    with one sentence instead of an IndexError.
    """
    body = r.json()
    assert isinstance(body, dict), f"GET /api/chat must answer an object: {r.text}"
    msgs = body.get("messages")
    assert isinstance(msgs, list), f"the envelope must carry `messages`: {r.text}"
    return msgs


def _nonempty_chat_page(_ctx: HCtx, r: httpx.Response) -> None:
    assert chat_messages(r), "expected a non-empty chat page"


def test_list_answers_carry_sizes_but_never_the_documents(client, owner_token):
    """The three list endpoints answer a DIRECTORY, not the documents (T-1170).

    This is the ticket's core promise and the ONLY thing this test looks at: a
    list row must carry the SIZE of each long document and not the document.
    It lives in the black-box suite because the promise is about the WIRE, and
    the two halves that implement it — the Go handlers and the cockpit's
    generated client — were each green against their own fixtures while
    disagreeing with one another about a field name. A suite that only ever
    asks one side cannot see that.

    Every assertion is written to fail on the two DIFFERENT ways this regresses:
      * the prose comes back (the key is present at all), and
      * the size stops being real (0, or absent) — a size-shaped 0 is worse
        than an honest omission, because it reads as a measurement.

    The corpus is seeded here rather than assumed, so an empty station cannot
    make this pass by having nothing to leak.
    """
    h = _auth(owner_token)
    prose = "conformance directory probe — long enough to be worth omitting\n" * 4
    n = len(prose)

    # ── seed: one manual with both documents, one role definition, one
    # retained revision. Absence has to mean "omitted", never "empty".
    type_key = f"conf-dir-{uuid.uuid4().hex[:8]}"
    r = client.post("/api/task-manuals", json={"type_key": type_key}, headers=h)
    assert r.status_code == 200, r.text
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"sop_md": prose, "learnings": prose},
        headers=h,
    )
    assert r.status_code == 200, r.text

    r = client.post("/api/roles", json={"name": "conformance directory role"}, headers=h)
    assert r.status_code == 200, r.text
    role_key = r.json()["role"]["key"]
    r = client.post(
        f"/api/roles/{role_key}", json={"definition_md": "conf role prose"}, headers=h
    )
    assert r.status_code == 200, r.text

    for text in ("conf directory history v1", "conf directory history v2"):
        r = client.post("/api/global-context", json={"text": text}, headers=h)
        assert r.status_code == 200, r.text

    # ── list_task_manuals ───────────────────────────────────────────────────
    r = client.get("/api/task-manuals", headers=h)
    assert r.status_code == 200, r.text
    rows = {m["type_key"]: m for m in r.json()}
    assert type_key in rows, "the manual just created is not on its own listing"
    m = rows[type_key]
    for absent in ("sop_md", "learnings"):
        assert absent not in m, (
            f"list_task_manuals still carries {absent!r} — the default answer "
            f"is the directory, and the body is GET /api/task-manuals/{{type_key}}"
        )
    assert m["sop_md_chars"] == n, f"sop_md_chars={m['sop_md_chars']!r}, want {n}"
    assert m["learnings_chars"] == n, f"learnings_chars={m['learnings_chars']!r}, want {n}"

    # ── list_roles ──────────────────────────────────────────────────────────
    r = client.get("/api/roles", headers=h)
    assert r.status_code == 200, r.text
    roles = {x["key"]: x for x in r.json()}
    assert role_key in roles, "the role just created is not on the roster"
    role = roles[role_key]
    assert "definition_md" not in role, (
        "list_roles still carries definition_md — the document is GET /api/roles/{role}"
    )
    assert role["size_chars"] == len("conf role prose"), (
        f"size_chars={role['size_chars']!r}, want {len('conf role prose')}"
    )

    # ── list_document_history ───────────────────────────────────────────────
    r = client.get("/api/document-history/global_context/global", headers=h)
    assert r.status_code == 200, r.text
    versions = r.json()
    assert versions, "two writes retained no version — this check would be vacuous"
    v = versions[0]
    assert "content" not in v, (
        "list_document_history still carries content — the body is "
        "GET /api/document-history/{kind}/{key}/{id}"
    )
    # `field_chars` is a MAP keyed by the kind's own field names, and
    # `tombstoned` is its OWN boolean rather than an entry of that map: leaving
    # it in would report the length of the string "true" — a 4 that looks like
    # a measurement — where a reader looks for how long a field was.
    assert isinstance(v["field_chars"], dict) and v["field_chars"], (
        f"field_chars is not a populated map: {v.get('field_chars')!r}"
    )
    assert "tombstoned" not in v["field_chars"], (
        f"tombstoned leaked into field_chars: {v['field_chars']!r}"
    )
    assert isinstance(v["tombstoned"], bool), (
        f"tombstoned is not a boolean: {v['tombstoned']!r}"
    )
    want = len("conf directory history v1")
    assert v["field_chars"]["text"] == want, (
        f"field_chars.text={v['field_chars']['text']!r}, want {want} — a size "
        f"that is not the real length is worse than no size at all"
    )
    # …and the text IS reachable, one revision at a time. Without this the
    # assertions above would also pass against a server that lost the prose.
    r = client.get(
        f"/api/document-history/global_context/global/{v['id']}", headers=h
    )
    assert r.status_code == 200, r.text
    assert r.json()["content"]["text"] == "conf directory history v1", r.text


def test_members_default_includes_outsource_workers_and_light_preserves_kind(
    client, owner_token, hctx, fresh_machine
):
    before = client.get("/api/members", headers=_auth(owner_token))
    assert before.status_code == 200, before.text
    before_ids = {member["id"] for member in before.json()}

    created = client.post(
        "/api/tasks",
        json={
            "title": f"member-list-guard-{uuid.uuid4().hex[:8]}",
            "executor_member_id": hctx.agent.member_id,
        },
        headers=_auth(hctx.agent.token),
    )
    assert created.status_code == 200, created.text
    task_id = created.json()["task"]["id"]
    reassigned = client.post(
        f"/api/tasks/{task_id}/reassign",
        json={"target": {"kind": "outsource", "machine": fresh_machine()}},
        headers=_auth(owner_token),
    )
    assert reassigned.status_code == 200, reassigned.text
    assert reassigned.json()["executor_id"] == ""

    members = client.get("/api/members", headers=_auth(owner_token))
    assert members.status_code == 200, members.text
    workers = [
        member
        for member in members.json()
        if member["id"].startswith("ow-") and member["id"] not in before_ids
    ]
    assert len(workers) == 1
    assert workers[0]["kind"] == "outsource"
    worker_id = workers[0]["id"]

    light = client.get("/api/members?fields=light", headers=_auth(owner_token))
    assert light.status_code == 200, light.text
    light_worker = next(member for member in light.json() if member["id"] == worker_id)
    assert light_worker["kind"] == "outsource"


def _happy_card(ctx: HCtx) -> str:
    """A fresh WAITING reply card opened by the happy agent (the real
    initiator identity: agents open cards, owners answer them)."""
    r = ctx.client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "conf happy card",
              "options": [{"text": "AI pick"}, {"text": "other"}], "linked_task": None},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy card failed: {r.status_code} {r.text}"
    return r.json()["id"]


def _happy_answered_card(ctx: HCtx) -> str:
    card_id = _happy_card(ctx)
    r = ctx.client.post(
        f"/api/reply-cards/{card_id}/answer",
        json={"option_idxs": [0]},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy answer failed: {r.status_code} {r.text}"
    return card_id


def _seeded_reply_cards_path(ctx: HCtx) -> str:
    _happy_card(ctx)  # ensure at least one waiting card exists
    return "/api/reply-cards"


def _onboard_claim(ctx: HCtx) -> dict:
    """Onboard a scratch machine and return the onboard body — the claim rows
    redeem its one-time claim_code."""
    r = ctx.client.post(
        "/api/machines",
        json={"display_name": f"conf-happy-claim-{uuid.uuid4().hex[:8]}"},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"claim-seed onboard failed: {r.status_code} {r.text}"
    return r.json()


def _check_claim_token_authenticates(ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    probe = ctx.client.get("/api/members", headers=_auth(data["token"]))
    assert probe.status_code == 200, "claimed machine token failed to authenticate"


def _happy_task(ctx: HCtx) -> str:
    """A fresh ad-hoc task the happy agent executes (the real initiator
    identity: agents create tasks)."""
    r = ctx.client.post(
        "/api/tasks",
        json={"title": "conf happy task",
              "executor_member_id": ctx.agent.member_id},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy task failed: {r.status_code} {r.text}"
    return r.json()["task"]["id"]


def _happy_task_step(ctx: HCtx) -> tuple[str, str]:
    """A fresh task with one planned PENDING step; (task_id, step_id).

    Task status is DERIVED from the steps (T-9ca5). The `gate=True` variant
    retired with the open_gate route (T-18): its only caller was that route's
    happy row, and a card is now opened through POST /api/reply-cards with an
    explicit linked_task, which has its own row and builds its own fixture."""
    h = _auth(ctx.agent.token)
    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "conf happy step", "dod": "asserted",
                         "is_gate": False}]},
        headers=h,
    )
    assert r.status_code == 200, f"happy plan failed: {r.status_code} {r.text}"
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = ctx.client.get(f"/api/tasks/{task_id}", headers=h).json()["steps"][0]["id"]
    return task_id, step_id


_HAPPY_STEP_NOTE = "conf happy single-step note — 做到哪、下一步接什麼"


def _happy_step_with_note(ctx: HCtx) -> str:
    """A fresh task+step whose note has been WRITTEN through the real write
    face, so the single-step read has something non-empty to prove it serves
    (T-66). Reading back a blank note would pass against a handler that never
    looked at the column."""
    task_id, step_id = _happy_task_step(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/note",
        json={"note": _HAPPY_STEP_NOTE},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy note seed failed: {r.status_code} {r.text}"
    return f"/api/tasks/{task_id}/steps/{step_id}"


def _happy_closed_task(ctx: HCtx) -> str:
    """A fresh DONE task the happy agent executed (close-out targets are
    terminal-only). Task status is DERIVED (T-9ca5): a one-step plan reported
    done auto-derives the task to done and closes it."""
    h = _auth(ctx.agent.token)
    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "conf happy step", "dod": "asserted"}]},
        headers=h,
    )
    assert r.status_code == 200, f"happy plan failed: {r.status_code} {r.text}"
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = ctx.client.get(f"/api/tasks/{task_id}", headers=h).json()["steps"][0]["id"]
    for status in ("in_progress", "done"):
        r = ctx.client.post(
            f"/api/tasks/{task_id}/steps/{step_id}/status",
            json={"status": status}, headers=h,
        )
        assert r.status_code == 200, f"happy step {status} failed: {r.status_code} {r.text}"
    return task_id


def _happy_reassigning_task(ctx: HCtx) -> str:
    """A fresh task under the `reassigning` LOCK whose NEW executor is the happy
    agent — so the happy agent may CLAIM it (the claim endpoint is
    executor-guarded). Created executed by a fresh member, then the owner
    reassigns it (kind=member) to the happy agent → lock=reassigning."""
    r = ctx.client.post(
        "/api/tasks",
        json={"title": "conf happy claim task",
              "executor_member_id": ctx.fresh_member()},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy claim-seed failed: {r.status_code} {r.text}"
    task_id = r.json()["task"]["id"]
    r = ctx.client.post(
        f"/api/tasks/{task_id}/reassign",
        json={"target": {"kind": "member", "member_id": ctx.agent.member_id}},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy reassign failed: {r.status_code} {r.text}"
    assert r.json()["lock"] == "reassigning", r.text
    return task_id


# The artifact id the replace row was aimed at, stashed by its path builder so
# the row's check can assert the write ANSWERED with the same id — a replace
# that minted a new one would otherwise pass on shape alone.
_REPLACE_TARGET: dict[str, str] = {}


def _happy_replaceable_artifact(ctx: HCtx) -> tuple[str, str]:
    """A fresh task with one link artifact pinned; (task_id, artifact_id) — the
    replace target."""
    task_id, artifact_id = _happy_task_artifact(ctx)
    _REPLACE_TARGET["id"] = artifact_id
    return task_id, artifact_id


def _happy_replaced_artifact(ctx: HCtx) -> tuple[str, str]:
    """The same, already replaced once — so its version list has exactly one
    retained entry to list."""
    task_id, artifact_id = _happy_task_artifact(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/artifact/{artifact_id}/replace",
        json={"url": "https://example.com/pr/2"},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy replace failed: {r.status_code} {r.text}"
    return task_id, artifact_id


# The blob the FILE version row was replaced away from, stashed by its path
# builder so the row's check can assert the version's url addresses THAT blob.
_REPLACED_FILE: dict[str, str] = {}


def _happy_replaced_file_artifact(ctx: HCtx) -> tuple[str, str]:
    """A FILE deliverable, already replaced once — the shape the version list
    actually holds (agent-written reports and logs), and the one whose wire the
    row's own `url` column cannot serve: it is empty for file/image, so a version
    projection that copied it left every retained report unreachable.

    Uploaded as `application/octet-stream` under a .md name because that is what
    the agent upload path produces for a report."""
    blobs = []
    for n in (1, 2):
        r = ctx.client.post(
            "/api/chat",
            json={
                "to": ctx.agent.member_id,
                "body": f"conf artifact report {n}",
                "attachments": [
                    {
                        "data_b64": base64.b64encode(
                            f"# conf report {n}\n".encode()
                        ).decode(),
                        "filename": "report.md",
                        "mime": "application/octet-stream",
                    }
                ],
            },
            headers=_auth(ctx.owner_token),
        )
        assert r.status_code == 200, f"happy report seed failed: {r.text}"
        blobs.append(r.json()["attachments"][0]["id"])

    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/artifact",
        json={"kind": "file", "attachment_id": blobs[0]},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy file artifact failed: {r.text}"
    artifact_id = r.json()["artifact_id"]
    r = ctx.client.post(
        f"/api/tasks/{task_id}/artifact/{artifact_id}/replace",
        json={"attachment_id": blobs[1]},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy file replace failed: {r.text}"
    _REPLACED_FILE["attachment_id"] = blobs[0]

    # The link shape stays covered on the real wire even though the row now
    # checks a file: a link version's url IS the row's own external url, which
    # is the control that stops the blob rewrite from applying to every kind.
    link_task, link_art = _happy_replaced_artifact(ctx)
    r = ctx.client.get(
        f"/api/tasks/{link_task}/artifact/{link_art}/history",
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"link history failed: {r.text}"
    link_versions = r.json()
    assert (
        len(link_versions) == 1
        and link_versions[0]["kind"] == "link"
        and link_versions[0]["url"] == "https://example.com/pr/1"
        and link_versions[0]["mime"] == ""
        and link_versions[0]["is_image"] is False
    ), f"a link version keeps its external url and describes no blob: {link_versions}"

    return task_id, artifact_id


def _happy_task_artifact(ctx: HCtx) -> tuple[str, str]:
    """A fresh task (the happy agent executes it) with one link artifact pinned;
    (task_id, artifact_id) — the un-pin (DELETE) target."""
    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/artifact",
        json={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy artifact failed: {r.status_code} {r.text}"
    return task_id, r.json()["artifact_id"]


def _happy_theme(ctx: HCtx) -> str:
    """A saved custom theme (owner-written); returns its id.

    Created through the real PUT rather than seeded, so the rows that READ a
    theme cannot pass against a fixture the product could not have produced."""
    theme_id = f"conf-happy-theme-{uuid.uuid4().hex[:8]}"
    r = ctx.client.put(
        f"/api/themes/{theme_id}",
        json={"id": theme_id, "name": "conf happy theme",
              "colors": {"--color-bg": "#101018"}},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy theme failed: {r.status_code} {r.text}"
    assert r.json()["created"] is True, f"a new theme must report created: {r.text}"
    return theme_id


def _happy_theme_for_put(ctx: HCtx) -> str:
    """The theme the PUT row replaces — created once and CACHED, because that
    row's path and its body have to name the SAME theme and are evaluated
    separately. Deliberately not shared with the other theme rows: DELETE
    consumes the theme it is given, and a shared one would make these rows
    order-dependent."""
    if ctx._put_theme_id is None:
        ctx._put_theme_id = _happy_theme(ctx)
    return ctx._put_theme_id


def _happy_manual(ctx: HCtx) -> str:
    """A fresh task manual (owner-created); returns its type_key.

    Deliberately exercises the LEGACY explicit-type_key create path (T-fa76:
    deprecated but kept for old MCP callers) — the display_name backfill to
    the key is asserted here; the new display_name→minted-tm- flow is the
    happy POST row below."""
    type_key = f"conf-happy-type-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        "/api/task-manuals", json={"type_key": type_key},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy manual failed: {r.status_code} {r.text}"
    assert r.json()["display_name"] == type_key, (
        f"legacy create must backfill display_name=type_key: {r.text}"
    )
    return type_key


def _seeded_tasks_path(ctx: HCtx) -> str:
    _happy_task(ctx)  # ensure at least one task exists
    return "/api/tasks"


def _seeded_task_count_path(ctx: HCtx) -> str:
    _happy_task(ctx)  # ensure at least one OPEN task exists
    return "/api/tasks/count"


def _happy_webhook(ctx: HCtx) -> tuple[str, str]:
    """A fresh webhook endpoint on the happy agent; (member_id, endpoint_id).
    The GET/PATCH/DELETE rows seed one so their faces act on a real endpoint."""
    endpoint_id = f"conf-hook-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        f"/api/members/{ctx.agent.member_id}/webhooks",
        json={"endpoint_id": endpoint_id, "purpose": "conf happy hook"},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy webhook seed failed: {r.status_code} {r.text}"
    return ctx.agent.member_id, endpoint_id


def _happy_webhook_requests_path(ctx: HCtx) -> str:
    """Seed a webhook AND one delivered /in call so the requests ring buffer
    has a row to serve; returns the debug-log path."""
    endpoint_id = f"conf-hook-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        f"/api/members/{ctx.agent.member_id}/webhooks",
        json={"endpoint_id": endpoint_id, "purpose": "conf requests hook"},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"requests-hook seed failed: {r.status_code} {r.text}"
    token = r.json()["token"]
    r = ctx.client.post(f"/in?t={token}", content=b"conf request-log seed")
    assert r.status_code == 200, f"/in seed failed: {r.status_code} {r.text}"
    return f"/api/members/{ctx.agent.member_id}/webhooks/{endpoint_id}/requests"


def _happy_scheduled_message(ctx: HCtx) -> tuple[str, str]:
    """A fresh scheduled message on the happy agent; (member_id, schedule_id).
    The GET/PATCH/DELETE rows seed one so their faces act on a real schedule."""
    r = ctx.client.post(
        f"/api/members/{ctx.agent.member_id}/scheduled-messages",
        json={
            "label": "conf happy schedule",
            "body": "conformance scheduled body",
            "cadence": "daily",
            "hour": 9,
            "minute": 0,
            "timezone": "Asia/Taipei",
        },
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy schedule seed failed: {r.status_code} {r.text}"
    return ctx.agent.member_id, r.json()["id"]


def _happy_restorable_revision(ctx: HCtx) -> str:
    """A restore path pointing at a revision that REALLY EXISTS.

    Two writes to the global context leave the first one retained, so the row
    exercises the real success path (200 + the restored version's DTO) instead
    of a 404 dressed up as coverage. Owner identity throughout: replacing the
    global context is governance, and so is restoring it.
    """
    h = {"Authorization": f"Bearer {ctx.owner_token}"}
    for text in ("conformance happy history v1", "conformance happy history v2"):
        r = ctx.client.post("/api/global-context", json={"text": text}, headers=h)
        assert r.status_code == 200, f"history seed write failed: {r.status_code} {r.text}"
    r = ctx.client.get("/api/document-history/global_context/global", headers=h)
    assert r.status_code == 200, f"history read failed: {r.status_code} {r.text}"
    versions = r.json()
    assert versions, "two writes retained no version — nothing to restore"
    return f"/api/document-history/global_context/global/{versions[0]['id']}/restore"


def _happy_document_version(ctx) -> str:
    """The same retained revision, addressed for a READ of its body.

    It reuses _happy_restorable_revision's seeding so both rows face a version
    that really exists; the listing itself carries no text since T-1170, so the
    id has to come from the listing and the prose from this route.
    """
    restore_path = _happy_restorable_revision(ctx)
    return restore_path.removesuffix("/restore")


_RESET_INSIGHT_OVERLAY = "conformance happy insight overlay to be discarded"


def _reset_insight_path(ctx: HCtx) -> str:
    """Make the reset row SELF-SUFFICIENT: write a distinctive overlay first, so
    the assertions afterwards measure a MOVE rather than a no-op.

    Without this the row would silently depend on whether an earlier row had
    left `assistant` non-default — and a reset that did nothing at all would
    still answer 200 with a spec-shaped body. Same posture as
    _document_history_restore_path above, which seeds its own precondition
    instead of trusting row order.
    """
    h = {"Authorization": f"Bearer {ctx.owner_token}"}
    r = ctx.client.post(
        "/api/insight/assistant", json={"text": _RESET_INSIGHT_OVERLAY}, headers=h
    )
    assert r.status_code == 200, f"reset seed write failed: {r.status_code} {r.text}"
    assert r.json()["is_default"] is False, "seed write did not leave a custom doc"
    return "/api/insight/assistant/reset"


def _check_reset_insight(ctx: HCtx, r: httpx.Response) -> None:
    """The factory seed is back, and the custom doc is provably gone.

    🔴 Deliberately does NOT compare against the seed's TEXT. The suite is
    black-box and must not read seeds/insight_assistant.md; what it can state
    order-independently is that the response is no longer the overlay
    _reset_insight_path just wrote, that is_default flipped back to True, and
    that the doc is non-empty — the assistant is the one role that ships a seed,
    so an empty answer here would mean the fold stopped finding it.
    """
    d = r.json()
    assert d["is_default"] is True, f"reset did not flip is_default: {d}"
    assert d["text"] != _RESET_INSIGHT_OVERLAY, "reset left the custom doc in place"
    assert d["text"].strip(), "reset served an EMPTY doc — the factory seed was not restored"
    assert d["size_chars"] == len(d["text"]), d
    assert d["cap_chars"] >= d["size_chars"], d
    assert d["role_key"] == "assistant", d
    # The precondition for this very route, still true after it ran (T-6501):
    # has_seed is about what SHIPS, so a reset can never consume it.
    assert d["has_seed"] is True, d
    # The READ face agrees — the response is not a one-off projection.
    g = ctx.client.get(
        "/api/insight/assistant",
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert g.status_code == 200, f"{g.status_code} {g.text}"
    assert g.json()["text"] == d["text"], "GET after reset disagrees with the reset response"
    assert g.json()["is_default"] is True, g.text


# ── the two editable boot-context blocks (T-791e) ────────────────────────────

_BOOT_DOC_EDIT = "# conformance edit — 系統互動 / 啟動步驟\n\nnot the factory text\n"

# T-3201 — a boot document has TWO halves on the read face (``read_only_head``
# and ``body``) and ONE on the write face (``body``). The marker that divides
# them on disk used to be spelled out here, because the protocol was "send the
# whole document back with its head verbatim" and a black-box client had to be
# able to build one. It is gone from this suite along with that protocol: the
# server hands over the half a write takes and joins the other one back on
# itself, so a client that knows what a marker is knows something the wire no
# longer asks it to know.


# 🔴 WHICH DOCUMENTS CARRY A READ-ONLY HEAD IS READ FROM THE SHARED TABLE, NOT
# ASSERTED AS A UNIVERSAL (T-6f44). Both checks below used to say
# ``assert d["read_only_head"]`` — every boot document has a non-empty head —
# and that was true right up until the owner ruled that four of them should not
# have one. The rule was written down in FOUR places (the server's registry
# mirror, the cockpit's, the seeds themselves, and here); three were updated and
# this one, in Python, in a suite the person editing the seeds said out loud
# they had not run, went red on documents that were entirely correct.
#
# bin/tests/fixtures/boot-doc-registry.tsv is the one copy the server's and the
# cockpit's guards are already pinned to, so reading it here makes three sides
# agree with ONE table instead of four sides agreeing with each other. Reading a
# repo file is not a black-box violation — the iron rule is about importing
# server IMPLEMENTATION modules, and this suite already reads spec/openapi.json
# and spec/mcp-catalog.json the same way. The table is a spec, not an
# implementation: it says which documents exist and what shape they ship in.
_BOOT_DOC_TABLE_PATH = HERE.parent / "bin" / "tests" / "fixtures" / "boot-doc-registry.tsv"


def _load_boot_doc_table() -> dict[tuple[str, str], bool]:
    """address -> has_head. Missing, malformed or EMPTY is a hard failure at
    import: a guard that goes green because it could not read its fixture is a
    lie, and an empty table would go green by agreeing that nothing exists."""
    out: dict[tuple[str, str], bool] = {}
    text = _BOOT_DOC_TABLE_PATH.read_text(encoding="utf-8")
    for i, line in enumerate(text.split("\n"), start=1):
        if line.startswith("#") or line.strip() == "":
            continue
        cols = line.split("\t")
        if len(cols) != 4:
            raise AssertionError(
                f"{_BOOT_DOC_TABLE_PATH}:{i}: want 4 tab-separated columns, got {len(cols)}"
            )
        if cols[0] == "kind":
            continue  # the header row
        if cols[3] not in ("true", "false"):
            raise AssertionError(
                f"{_BOOT_DOC_TABLE_PATH}:{i}: has_head is {cols[3]!r}, want true or false"
            )
        out[(cols[0], cols[1])] = cols[3] == "true"
    if not out:
        raise AssertionError(f"{_BOOT_DOC_TABLE_PATH} parsed to zero rows")
    return out


_BOOT_DOC_HAS_HEAD = _load_boot_doc_table()


def _check_head(d: dict, kind: str, key: str) -> None:
    """Both directions. A head that vanished is what happened this time; a head
    that APPEARS on a document the table says has none is the same silent drift
    seen from the other side — an agent starts reading a machine-written
    sentence nobody decided to send it."""
    addr = (kind, key)
    assert addr in _BOOT_DOC_HAS_HEAD, (
        f"{kind}/{key} is served but {_BOOT_DOC_TABLE_PATH} does not list it"
    )
    want = _BOOT_DOC_HAS_HEAD[addr]
    got = bool(d["read_only_head"])
    assert got == want, (
        f"{kind}/{key}: the shared table says has_head={want}, this server serves "
        f"read_only_head={d['read_only_head']!r}"
    )
    if want:
        assert d["read_only_head"] in d["text"], d


def _boot_doc_body(ctx: HCtx) -> dict:
    """Body factory: the write face takes the editable half and nothing else."""
    return {"body": _BOOT_DOC_EDIT}


def _boot_doc_written(kind: str, key: str):
    """The edit came back verbatim and the block stopped reading as default.

    ``body`` is compared BYTE FOR BYTE against what was sent — that is the whole
    contract now, and it is a stronger statement than the old one: what a client
    sends is what a client reads back, with no half of the document it has to
    carry along and no rule about carrying it correctly. ``text`` is the whole
    stored document and is checked only for the properties the halves must have
    inside it — including whether there is a head at all, which is now per
    document and read from the shared table.
    """

    def check(_ctx: HCtx, r: httpx.Response) -> None:
        d = r.json()
        assert d["body"] == _BOOT_DOC_EDIT, d
        assert d["text"].endswith(_BOOT_DOC_EDIT), d
        _check_head(d, kind, key)
        assert d["is_default"] is False, d
        assert d["size_chars"] == len(d["text"]), d
        assert d["cap_chars"] >= d["size_chars"], d
        assert d["has_seed"] is True, d

    return check


def _boot_doc_reset(path: str):
    """Reset check: the factory text is back and the edit is provably gone.

    🔴 Deliberately does NOT compare against the seed's TEXT — the suite is
    black-box and must not read seeds/*.md. What it can state order-independently
    is that the answer is no longer the edit, that is_default flipped, and that
    the document is NOT EMPTY: these blocks are the ones every agent boots from,
    so an empty answer would mean the fold stopped finding the shipped seed.
    """

    def check(ctx: HCtx, r: httpx.Response) -> None:
        d = r.json()
        assert d["is_default"] is True, d
        assert d["text"] != _BOOT_DOC_EDIT, "reset left the edit in place"
        assert d["text"].strip(), "reset served an EMPTY block — the shipped seed was not restored"
        assert d["has_seed"] is True, d
        g = ctx.client.get(path, headers={"Authorization": f"Bearer {ctx.owner_token}"})
        assert g.status_code == 200, f"{g.status_code} {g.text}"
        assert g.json()["text"] == d["text"], "GET after reset disagrees with the reset response"

    return check


def _boot_doc_read(kind: str, key: str):
    def check(_ctx: HCtx, r: httpx.Response) -> None:
        d = r.json()
        assert d["kind"] == kind and d["key"] == key, d
        assert d["text"].strip(), "the block is empty — the shipped seed was not folded in"
        # T-3201 — the read face names both halves, and the pair has to describe
        # the document it came with: the half a write takes back is IN the
        # stored text and ends it, and the half nobody may write is in it too.
        # A client that gets this pair never has to know a marker exists.
        assert d["body"] and d["text"].endswith(d["body"]), d
        _check_head(d, kind, key)
        assert d["size_chars"] == len(d["text"]), d
        assert d["cap_chars"] >= d["size_chars"], d
        assert isinstance(d["is_default"], bool), d
        assert d["has_seed"] is True, d

    return check


# ── T-33 lore ────────────────────────────────────────────────────────────────
# 🔴 THIS HELPER IS WHY THE TWO GOVERNANCE ROWS STOPPED BEING SKIPS. They were
# skipped with a reason that said, in as many words, "delete this entry the
# moment a create route lands" — the station served no way to make an entry, so
# every wire-reachable face of retire and revive was a 404 and the only thing
# checking them was a Go test that could reach the DAL directly. A create route
# exists now, so the skip's own condition is gone and the rows are real.
def _lore_entry(ctx: HCtx) -> str:
    """Write one entry as the happy agent and return its id."""
    r = ctx.client.post(
        "/api/lore/entries",
        headers={"Authorization": f"Bearer {ctx.agent.token}"},
        json={
            "trigger": "a route answers 200 and nothing was written",
            "content": "the entry and its original are one transaction",
            "retire_when": "a second route turns out to write entries too",
            "impact": "the conformance suite seeding this very entry",
            # 🔴 ONE EVENT, AND ITS 人／地／物 ARE DELIBERATELY LEFT OFF. 第 5 格
            # only says something if the wire can carry an event whose optional
            # cells nobody knew — the row below asserts they come back EMPTY
            # rather than back-filled, which is the one way this cell could be
            # wrong while every response still validated against the schema.
            "events": [
                {
                    "happened_ts": 1788330000,
                    "what": "the conformance suite wrote this very entry",
                }
            ],
            "origin": f"agent:{ctx.agent.member_id}",
            "subjects": [f"agent:{ctx.agent.member_id}"],
        },
    )
    assert r.status_code == 200, f"seed lore entry: {r.status_code} {r.text}"
    return r.json()["entry_id"]


def _lore_retired_entry(ctx: HCtx) -> str:
    """Write one entry and retire it, so a revival has something to revive."""
    entry_id = _lore_entry(ctx)
    r = ctx.client.post(
        f"/api/lore/entries/{entry_id}/retire",
        headers={"Authorization": f"Bearer {ctx.agent.token}"},
        json={"reason": "expired"},
    )
    assert r.status_code == 200, f"seed retirement: {r.status_code} {r.text}"
    return entry_id


# The subject key the write row files against. It is generated ONCE per session
# and never used by any other row, so "was this minted" is a question about the
# server rather than about which happy row pytest ran first.
_LORE_FRESH_SUBJECT = f"agent:conformance-lore-{uuid.uuid4().hex[:8]}"


def _lore_fresh_subject(_ctx: HCtx) -> str:
    return _LORE_FRESH_SUBJECT


def _lore_revision_path(ctx: HCtx) -> str:
    """Write an entry, read its revision catalogue, and address the one revision
    it has. The id is READ BACK rather than assumed to be 1: revision ids are
    global, so hard-coding one would pass or fail depending on what else ran."""
    entry_id = _lore_entry(ctx)
    r = ctx.client.get(
        f"/api/lore/entries/{entry_id}",
        headers={"Authorization": f"Bearer {ctx.agent.token}"},
    )
    assert r.status_code == 200, f"read back: {r.status_code} {r.text}"
    revs = r.json()["revisions"]
    assert len(revs) == 1, f"a freshly written entry must have exactly one revision: {revs}"
    return f"/api/lore/entries/{entry_id}/revisions/{revs[0]['revision_id']}"


def _check_lore_read(_ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    # 🔴 THE ORIGINAL. `content` (第 2 格) is what enters a boot context and it is
    # lossy on purpose; this field is the whole reason the ticket exists. An entry
    # served with an empty one would look correct in every other respect.
    assert d["original"], f"the entry was served with NO original: {d}"
    # 五格 as the owner ruled it on 2026-09-03. `label` / `falsify` /
    # `residual_risk` are GONE — not renamed, removed — so this list is the four
    # named cells plus the `events:` block, and it is the assertion that would
    # fail first if the renderer ever quietly went back to the old shape.
    for field in ("trigger", "content", "retire_when", "impact", "events"):
        assert f"{field}:" in d["original"], (
            f"the original drops the {field!r} section — a renderer that skips blanks "
            f"cannot tell 'never written' from 'deleted': {d['original']!r}"
        )
    # 🔴 第 5 格 IS INSIDE THE ORIGINAL, therefore inside `sha256`. Without this
    # the events could be served correctly on their own field while being absent
    # from the one text an agent falls back to — and the fallback is the point.
    assert "the conformance suite wrote this very entry" in d["original"], (
        f"the events are not in the L0 original, so 第 5 格 is outside the digest "
        f"an agent verifies against: {d['original']!r}"
    )
    # 🔴 人／地／物 NOBODY KNEW COME BACK EMPTY, NOT BACK-FILLED. A server that
    # helpfully wrote 「未知」 would make 「could not find out who」 and 「nobody has
    # looked yet」 indistinguishable from here on, in the digest as well as on
    # screen, and nothing downstream could separate them again.
    assert len(d["events"]) == 1, d
    ev = d["events"][0]
    assert ev["what"] and ev["happened_ts"] == 1788330000, ev
    assert ev["actor"] == "" and ev["place"] == "" and ev["object"] == "", (
        f"an event's unknown 人／地／物 came back filled in rather than empty: {ev}"
    )
    assert len(d["sha256"]) == 64, d
    assert d["written_by"], d
    assert len(d["revisions"]) == 1, d
    # The catalogue carries NO text: a list is how you choose a revision.
    assert "body" not in d["revisions"][0], d["revisions"][0]


def _check_lore_search(_ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    # 🔴 `applied` IS NOT OPTIONAL AND THIS IS WHERE THAT IS PINNED. The tier
    # labels mean "matched every axis you asked on", which is only interpretable
    # beside the axes that were asked — a tier that travelled alone would be read
    # under the design's older meaning ("both axes intersect") and quietly mean
    # something else.
    applied = d["applied"]
    assert applied["subject"] == _LORE_FRESH_SUBJECT, d
    assert applied["tiered_by"] == ["subject"], d
    assert applied["limit"] == 5, d
    # The kind of matching is a VALUE, not a sentence in a document, so that the
    # day it becomes semantic the answer says so instead of quietly changing.
    assert applied["query_match"] == "literal-substring", d
    assert d["subject_resolved"] is True, d
    assert d["unresolved_subject"] == "", d
    # The subject was minted by the write row, which files one entry under it.
    assert d["total"] >= 1 and d["entries"], d
    first = d["entries"][0]
    assert first["tier"] == "T1", first
    assert first["tier_note"], first
    assert first["trust_scope"] in {"method", "trust", "cognitive"}, first
    assert isinstance(first["trust_fell_back"], bool), first


def _check_lore_write(_ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    assert d["entry_id"], d
    # 🔴 THE ORIGINAL, ASSERTED ON THE WIRE. An entry written with no L0 revision
    # behind it looks identical in every count and every context; revision_id is
    # the only place a caller can see that it was preserved.
    assert d["revision_id"] > 0, f"the write reports no preserved original: {d}"
    assert len(d["sha256"]) == 64, d
    # The subject key was new, so it must come back as a MINT rather than being
    # swallowed — that is what turns a typo into something the writer sees now.
    assert [e["canonical"] for e in d["pending_entities"]] == [
        _LORE_FRESH_SUBJECT
    ], d
    assert d["subject_ids"] and all(d["subject_ids"]), d
    # 🔴 `degraded` IS GONE FROM THIS RECEIPT AND THAT IS ASSERTED, not merely
    # un-asserted. Owner ruling rc-1e32c690018d (2026-09-03) removed the concept:
    # 第 1 格 is refused blank at the upsert seam, so a second, softer 「written but
    # suspect」 flag sat behind a hard gate. An absent key and a key nobody looks
    # at are the same to a reader of this file, so the absence is pinned here.
    assert "degraded" not in d, f"the removed `degraded` flag is back on the wire: {d}"
    assert d["superseded"] == "", d


# ── T-33 lore 對象審核 (the review queue) ──────────────────────────────────────
# Every subject key an agent writes and nothing recognises is MINTED pending, so
# the write route IS the seam that fills this queue — these rows seed through it
# rather than reaching for a fixture the wire does not offer. Each row gets its
# OWN subject key, generated once per session: approving or merging is a state
# change, and a row sharing a key with another would pass or fail on which one
# pytest ran first.
_LORE_QUEUE_SUBJECT = f"repo:conf-queue-{uuid.uuid4().hex[:8]}"
_LORE_APPROVE_SUBJECT = f"repo:conf-approve-{uuid.uuid4().hex[:8]}"
_LORE_MERGE_TARGET_SUBJECT = f"repo:conf-survivor-{uuid.uuid4().hex[:8]}"
_LORE_MERGE_SOURCE_SUBJECT = f"repo:conf-folded-{uuid.uuid4().hex[:8]}"

# The merge row's target id, learned by its path builder (which runs before the
# body builder) and read by the body. It cannot be a constant: the id is minted
# by the server on the seeding write.
_LORE_MERGE_TARGET_ID: dict[str, str] = {}



def _lore_pending_entity(ctx: HCtx, subject: str) -> str:
    """Write one entry under a subject key nothing carries and return the
    entity id the write minted for it."""
    r = ctx.client.post(
        "/api/lore/entries",
        headers=_auth(ctx.agent.token),
        json={
            "trigger": "a subject key is minted and no route can reach it",
            "content": "an unreviewed name is invisible to every agent's boot",
            "retire_when": "the pending entity is listed before anyone approves it",
            "impact": "the conformance suite seeding this very entity",
            "origin": f"agent:{ctx.agent.member_id}",
            "subjects": [subject],
        },
    )
    assert r.status_code == 200, f"seed pending entity: {r.status_code} {r.text}"
    minted = [e for e in r.json()["pending_entities"] if e["canonical"] == subject]
    assert len(minted) == 1, f"{subject} was not minted pending: {r.json()}"
    return minted[0]["entity_id"]


def _lore_queue_path(ctx: HCtx) -> str:
    """Seed one pending entity so the queue answers a row rather than `[]`. An
    empty queue is a legal answer, so a list row with nothing in it would pass
    against a handler that serves a constant."""
    _lore_pending_entity(ctx, _LORE_QUEUE_SUBJECT)
    return "/api/lore/entities/pending"


def _check_lore_queue(_ctx: HCtx, r: httpx.Response) -> None:
    rows = r.json()
    assert isinstance(rows, list)
    seeded = [row for row in rows if row["canonical"] == _LORE_QUEUE_SUBJECT]
    assert len(seeded) == 1, f"the seeded pending entity is not in the queue: {rows}"
    row = seeded[0]
    # 🔴 THE HOMEWORK IS THE ROUTE'S REASON TO EXIST. A row carrying only the
    # key sends the reviewer to two other screens to find out whether it is a
    # typo, so the count and the sample travel WITH it.
    assert row["entries"] >= 1, row
    assert row["sample_short"], row
    assert row["type"] == "repo" and row["name"], row
    # 🔴 ROUND 3 (owner 2026-09-04: 「我根本無從審核起」). The sample answered
    # 「what is ONE of these about」; these three answer the question the review
    # actually asks — was this name ever used, who minted it, and what is filed
    # under it. This fixture writes ONE entry through the real write seam, so
    # all three are determinate rather than merely well-typed:
    #   * `entries_ever` counts retired rows too and nothing here retires, so it
    #     equals `entries` — a handler that hard-coded 0 (the interesting value,
    #     since 0/0 is what marks a never-used name) fails here.
    #   * `created_by` is the VERIFIED token subject of the write, never blank —
    #     CreateLoreEntry refuses a blank actor outright.
    #   * `entry_refs` is the list `entries` counted, so the two must agree, and
    #     each line must identify its entry and carry 第 1 格.
    assert row["entries_ever"] == row["entries"], row
    assert row["created_by"], row
    assert len(row["entry_refs"]) == row["entries"], row
    for ref in row["entry_refs"]:
        assert ref["entry_id"] and ref["trigger"], row
        assert ref["status"] != "retired", row
    # 🔴 `similar` IS PINNED TO THE ONE BRANCH THIS FIXTURE FORCES, not merely
    # well-typed. The fixture makes the branch determinate: the subject is
    # `repo:conf-queue-<random hex>`, a name no approved subject can fold onto
    # (`same_normalized`) and none is within 2 edits / a prefix / a substring
    # of — the suite's own sibling subjects (`conf-approve-`, `conf-survivor-`,
    # `conf-folded-`) diverge at the 6th character and are nowhere near. So
    # `similar` is EMPTY, and that is the only reachable answer here.
    assert row["similar"] == [], row


def _lore_approve_path(ctx: HCtx) -> str:
    return f"/api/lore/entities/{_lore_pending_entity(ctx, _LORE_APPROVE_SUBJECT)}/approve"


def _check_lore_approve(ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    assert d["canonical"] == _LORE_APPROVE_SUBJECT, d
    assert d["kind"] == "entity-approve" and d["actor_id"], d
    # 🔴 THE RECEIPT IS READ BACK, NOT ECHOED, so `pending: false` here is the
    # state the entity is actually in — and leaving the queue is the whole act.
    assert d["pending"] is False and d["merged_into"] == "", d
    left = ctx.client.get(
        "/api/lore/entities/pending", headers=_auth(ctx.owner_token)
    )
    assert left.status_code == 200, left.text
    assert _LORE_APPROVE_SUBJECT not in [
        row["canonical"] for row in left.json()
    ], "an approved entity is still parked in the review queue"


def _lore_merge_path(ctx: HCtx) -> str:
    """A merge needs a survivor that is itself APPROVED — merging into a subject
    the boot directory also hides is the refusal the route names, not the happy
    face — so the target is minted and approved before the source is minted."""
    target = _lore_pending_entity(ctx, _LORE_MERGE_TARGET_SUBJECT)
    approved = ctx.client.post(
        f"/api/lore/entities/{target}/approve",
        headers=_auth(ctx.owner_token),
        json={"reason": "conformance merge target"},
    )
    assert approved.status_code == 200, f"seed merge target: {approved.text}"
    _LORE_MERGE_TARGET_ID["id"] = target
    source = _lore_pending_entity(ctx, _LORE_MERGE_SOURCE_SUBJECT)
    return f"/api/lore/entities/{source}/merge"


def _check_lore_merge(ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    assert d["canonical"] == _LORE_MERGE_SOURCE_SUBJECT, d
    assert d["kind"] == "entity-merge" and d["actor_id"], d
    # The source keeps existing — nothing in this schema deletes — but it stops
    # being pending and now names its survivor.
    assert d["pending"] is False, d
    assert d["merged_into"] == _LORE_MERGE_TARGET_ID["id"], d
    left = ctx.client.get(
        "/api/lore/entities/pending", headers=_auth(ctx.owner_token)
    )
    assert left.status_code == 200, left.text
    assert _LORE_MERGE_SOURCE_SUBJECT not in [
        row["canonical"] for row in left.json()
    ], "a merged-away entity is still parked in the review queue"
# ── T-33 lore 提案 ───────────────────────────────────────────────────────────
# 🔴 THE SEED IS MEMOISED RATHER THAN ORDERED. The POST row needs the entry id in
# its PATH and that entry's digest in its BODY, and a row's path and body are
# resolved by two separate calls. Seeding twice would hand the body the digest of
# a different entry, and the 409 that produced would look exactly like a real
# staleness refusal. Memoising makes the two calls agree without either of them
# knowing which ran first.
_LORE_PROPOSAL_SEED: dict[str, tuple[str, str]] = {}


def _lore_proposal_target(ctx: HCtx, slot: str) -> tuple[str, str]:
    """Write one entry and read its CURRENT digest back off the wire.

    The digest comes from `GET /api/lore/entries/{id}`, which is the path a real
    proposer has. Taking it from the write receipt instead would pass even if the
    read route served a different digest — and that is the one way this feature
    could be wrong while every screen looked right."""
    if slot in _LORE_PROPOSAL_SEED:
        return _LORE_PROPOSAL_SEED[slot]
    entry_id = _lore_entry(ctx)
    r = ctx.client.get(
        f"/api/lore/entries/{entry_id}",
        headers={"Authorization": f"Bearer {ctx.agent.token}"},
    )
    assert r.status_code == 200, f"read back for {slot}: {r.status_code} {r.text}"
    sha = r.json()["sha256"]
    assert len(sha) == 64, sha
    _LORE_PROPOSAL_SEED[slot] = (entry_id, sha)
    return entry_id, sha


def _lore_propose_body(ctx: HCtx) -> dict[str, str]:
    _, sha = _lore_proposal_target(ctx, "post")
    return {
        "kind": "update",
        "base_sha256": sha,
        "encountered": "the conformance suite's own happy row",
        "fault": "stale",
        "evidence": "this entry names the transaction, and the transaction moved file",
        # 🔴 A PROPOSAL CARRIES 四格 AND ITS OWN 第 5 格 — the WHOLE event list
        # as it should stand once accepted, because accepting replaces the
        # entry's events wholesale (owner ruling rc-e5c34500face, 2026-09-03).
        # `events` is REQUIRED on an `update`: omitting it is a 422, never a
        # shorthand for 「維持現狀」, so that one forgotten field cannot clear
        # 第 5 格 where no reviewer would see it.
        "trigger": "a route answers 200 and nothing was written",
        "content": "the entry, its original and its axes are ONE transaction",
        "retire_when": "an entry turns up with no revision behind it",
        "impact": "the conformance suite proposing this very change",
        "events": [
            {
                "happened_ts": 1700000000.0,
                "what": "the conformance suite proposed a whole new version",
            }
        ],
    }


def _check_lore_propose(ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    _, sha = _lore_proposal_target(ctx, "post")
    assert d["proposal_id"], d
    # 🔴 THE BINDING. A receipt that did not name the revision it matched would
    # leave a reviewer unable to tell what this proposal was written against.
    assert d["base_sha256"] == sha, d
    assert d["base_revision_id"] > 0, d
    # The proposed version really was rendered and digested, and it is NOT the
    # base — a proposal that digests to the base changed nothing.
    assert len(d["sha256"]) == 64 and d["sha256"] != sha, d


def _lore_proposal_list_path(ctx: HCtx) -> str:
    """Seed an entry AND a proposal against it, so the list row has something to
    serve. An empty list would satisfy the schema and prove nothing."""
    entry_id, sha = _lore_proposal_target(ctx, "get")
    r = ctx.client.post(
        f"/api/lore/entries/{entry_id}/proposals",
        headers={"Authorization": f"Bearer {ctx.agent.token}"},
        json={
            "kind": "remove",
            "base_sha256": sha,
            "encountered": "the conformance suite's own list row",
            "fault": "misled",
            "evidence": "the entry is retrieved for a situation it does not describe",
        },
    )
    assert r.status_code == 200, f"seed proposal: {r.status_code} {r.text}"
    return f"/api/lore/entries/{entry_id}/proposals"


def _check_lore_proposal_list(ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    _, sha = _lore_proposal_target(ctx, "get")
    assert d["current_sha256"] == sha and d["current_revision_id"] > 0, d
    assert d["proposals"], d
    row = d["proposals"][0]
    # 🔴 `stale` IS A COMPARISON AND BOTH SIDES OF IT ARE ON THE WIRE, so a
    # reviewer can re-derive it instead of trusting it.
    assert row["base_sha256"] == d["current_sha256"], row
    assert row["stale"] is False, row
    # A removal proposes NO version: the body fields are empty, and that is the
    # difference between "he proposed nothing" and "he proposed a blank entry".
    assert row["kind"] == "remove" and row["body"] == "" and row["content"] == "", row
    assert row["fault"] == "misled" and row["encountered"] and row["evidence"], row
    assert row["actor_id"], row
    # 🔴 第 5 格 travels on BOTH sides so the reviewer can recompute the
    # difference rather than trust it — the same rule `current_sha256` follows
    # for `stale`. A `remove` proposes no version at all, so it moves no events:
    # its two difference lists are empty, and that is not the same statement as
    # 「這條不該有事件」, which an `update` makes by sending `events: []`.
    assert isinstance(d["current_events"], list), d
    assert row["events"] == [] and row["events_added"] == [], row
    assert row["events_removed"] == [], row


# ── T-33 lore 提案的核可 ───────────────────────────────────────────────────────
# 🔴 THE ACCEPT ROW SEEDS ITS OWN ENTRY AND ITS OWN PROPOSAL, deliberately NOT
# reusing the "post"/"get" slots above. Accepting REWRITES the entry and bumps
# its digest, so sharing a seed with the propose row would make that row's
# `base_sha256` assertion depend on which of the two pytest happened to run
# first — and the 409 that produced would look exactly like a real staleness
# refusal.
_LORE_ACCEPT: dict[str, str] = {}


def _lore_accept_path(ctx: HCtx) -> str:
    """Write an entry as the agent, file an `update` proposal against it, and
    address the acceptance. The proposer is the AGENT and the accepter will be
    the owner — which is what lets the check below tell the two apart."""
    entry_id, sha = _lore_proposal_target(ctx, "accept")
    r = ctx.client.post(
        f"/api/lore/entries/{entry_id}/proposals",
        headers=_auth(ctx.agent.token),
        json={
            "kind": "update",
            "base_sha256": sha,
            "encountered": "the conformance suite's own acceptance row",
            "fault": "misled",
            "evidence": "the entry is retrieved for a situation it does not describe",
            "trigger": "a proposal is accepted and the entry does not move",
            "content": "accepting writes the proposal's own bytes onto the entry",
            "retire_when": "an accept route turns out to re-render the version",
            "impact": "the conformance suite accepting this very proposal",
            # 🔴 第 5 格 IS REPLACED WHOLESALE, so this ONE event is the entry's
            # whole event list afterwards — the seeded entry carried one of its
            # own, with different text. `events_after` being 1 is therefore not
            # a sum, and the check below reads the entry back to prove which
            # event survived.
            "events": [
                {
                    "happened_ts": 1788440000,
                    "what": "the conformance suite proposed the version it then accepted",
                }
            ],
        },
    )
    assert r.status_code == 200, f"seed proposal to accept: {r.status_code} {r.text}"
    _LORE_ACCEPT["proposal_id"] = r.json()["proposal_id"]
    _LORE_ACCEPT["sha256"] = r.json()["sha256"]
    _LORE_ACCEPT["entry_id"] = entry_id
    return f"/api/lore/entries/{entry_id}/proposals/{_LORE_ACCEPT['proposal_id']}/accept"


def _check_lore_accept(ctx: HCtx, r: httpx.Response) -> None:
    d = r.json()
    assert d["proposal_id"] == _LORE_ACCEPT["proposal_id"], d
    assert d["entry_id"] == _LORE_ACCEPT["entry_id"], d
    assert d["revision_id"] > 0, d
    # 🔴 THE BYTES THAT LANDED ARE THE PROPOSAL'S OWN, not a fresh rendering:
    # the receipt's digest is the one the submission receipt already carried.
    assert d["sha256"] == _LORE_ACCEPT["sha256"], d
    assert d["events_after"] == 1, d

    # Read the entry back: the acceptance has to be visible on the READ face,
    # not only in its own receipt.
    e = ctx.client.get(
        f"/api/lore/entries/{_LORE_ACCEPT['entry_id']}",
        headers=_auth(ctx.agent.token),
    )
    assert e.status_code == 200, f"read back accepted entry: {e.status_code} {e.text}"
    entry = e.json()
    assert entry["sha256"] == d["sha256"], entry
    assert entry["content"] == "accepting writes the proposal's own bytes onto the entry", entry
    # 第 5 格 was replaced wholesale — the seeded entry's own event is GONE.
    assert [ev["what"] for ev in entry["events"]] == [
        "the conformance suite proposed the version it then accepted"
    ], entry
    # 🔴 THE ACCEPTER SIGNS IT, NOT THE PROPOSER. This row ran as the owner and
    # the proposal was filed by the scratch agent; a revision carrying the
    # proposer's id would mean the only record of the verdict names the wrong
    # person.
    newest = max(entry["revisions"], key=lambda row: row["revision_id"])
    assert newest["revision_id"] == d["revision_id"], entry["revisions"]
    assert newest["actor_id"] and newest["actor_id"] != ctx.agent.member_id, entry["revisions"]


HAPPY: dict[str, Happy] = {
    # ── T-33 lore 對象審核 ─────────────────────────────────────────────────────
    # The queue's three faces run as the owner: the floor is admin_agent (owner
    # ruling rc-139a5ab99a19), and the owner is this file's lowest-friction
    # identity at or above it.
    "GET /api/lore/entities/pending": Happy(
        path=_lore_queue_path,
        check=_check_lore_queue,
    ),
    "POST /api/lore/entities/{entity_id}/approve": Happy(
        path=_lore_approve_path,
        body={"reason": "conformance happy approval"},
        check=_check_lore_approve,
    ),
    "POST /api/lore/entities/{entity_id}/merge": Happy(
        path=_lore_merge_path,
        body=lambda _ctx: {
            "into": _LORE_MERGE_TARGET_ID["id"],
            "reason": "conformance happy merge",
        },
        check=_check_lore_merge,
    ),
    # ── T-33 lore ────────────────────────────────────────────────────────────
    "POST /api/lore/entries": Happy(
        identity="agent",
        # 🔴 THE SUBJECT KEY IS FRESH PER RUN, AND THAT IS A FIX, NOT A STYLE
        # CHOICE. The first version filed against `agent:<the happy agent>` and
        # asserted the key came back as a MINT — which passed alone and failed in
        # the suite, because the retire and revive rows seed an entry against
        # that same key first, so by the time this row ran the subject already
        # existed and nothing was minted. An assertion that depends on which
        # rows ran before it is not pinning the server's behaviour, it is
        # pinning the order pytest happened to choose.
        body=lambda ctx: {
            "trigger": "a route answers 200 and nothing was written",
            "content": "the entry and its original are one transaction",
            "retire_when": "a second route turns out to write entries too",
            "impact": "the conformance suite writing this very row",
            "origin": f"agent:{ctx.agent.member_id}",
            "subjects": [_lore_fresh_subject(ctx)],
        },
        check=_check_lore_write,
    ),
    # 🔴 HOP ③ — the route the ticket was opened for. The assertion that matters
    # is `original`: the entry's full text as written, which `content` (第 2 格) is
    # a lossy compression of. Without it, 「原始資訊可以保留」 is true of the database
    # and false of every agent.
    "GET /api/lore/entries/{entry_id}": Happy(
        identity="agent",
        path=lambda ctx: f"/api/lore/entries/{_lore_entry(ctx)}",
        check=_check_lore_read,
    ),
    "GET /api/lore/entries/{entry_id}/revisions/{revision_id}": Happy(
        identity="agent",
        path=_lore_revision_path,
        check=lambda _c, r: _expect(
            r,
            lambda d: d["body"]
            and len(d["sha256"]) == 64
            and d["shrink_chars"] == 0
            and bool(d["actor_id"]),
        ),
    ),
    "POST /api/lore/entries/{entry_id}/retire": Happy(
        identity="agent",
        path=lambda ctx: f"/api/lore/entries/{_lore_entry(ctx)}/retire",
        body={"reason": "expired"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "retired"
            and d["kind"] == "retire"
            and d["reason"] == "expired"
            and bool(d["actor_id"]),
        ),
    ),
    # 🔴 THE ASSERTION THAT MATTERS HERE IS THE REFUSAL, and it is in
    # `test_lore_search_refuses_an_undeclared_condition` below rather than in
    # this row: a happy face proves the route answers, not that it would have
    # objected to a condition it does not implement.
    "POST /api/lore/search": Happy(
        identity="agent",
        body={"subject": _LORE_FRESH_SUBJECT, "limit": 5},
        check=_check_lore_search,
    ),
    "POST /api/lore/entries/{entry_id}/proposals": Happy(
        identity="agent",
        path=lambda ctx: f"/api/lore/entries/{_lore_proposal_target(ctx, 'post')[0]}/proposals",
        body=_lore_propose_body,
        check=_check_lore_propose,
    ),
    "GET /api/lore/entries/{entry_id}/proposals": Happy(
        identity="agent",
        path=_lore_proposal_list_path,
        check=_check_lore_proposal_list,
    ),
    # Runs as the OWNER: the floor is admin_agent (owner ruling rc-a896af93d4f9)
    # and the owner is this file's lowest-friction identity at or above it.
    "POST /api/lore/entries/{entry_id}/proposals/{proposal_id}/accept": Happy(
        path=_lore_accept_path,
        check=_check_lore_accept,
    ),
    "POST /api/lore/entries/{entry_id}/revive": Happy(
        path=lambda ctx: f"/api/lore/entries/{_lore_retired_entry(ctx)}/revive",
        body={"reason": "conformance happy revival"},
        check=lambda _c, r: _expect(
            r, lambda d: d["status"] == "active" and d["kind"] == "revive"
        ),
    ),
    # ── public ───────────────────────────────────────────────────────────────
    "GET /api/health": Happy(identity="none"),
    "GET /api/version": Happy(identity="none", check=_check_version),
    "GET /health": Happy(identity="none"),
    "GET /version": Happy(identity="none", check=_check_version),
    "POST /api/login": Happy(
        identity="none",
        body=lambda _ctx: {"password": os.environ["OC_OWNER_PASSWORD"]},
        check=_check_login,
    ),
    # ── owner second factor (TOTP) ───────────────────────────────────────────
    # enroll is a REAL happy face: it mints an INERT pending secret (nothing is
    # armed until a code proves it), so it neither needs setup nor leaves the
    # shared credential changed.
    #
    # activate / disable are NOT in this table — their positive faces would ARM
    # a factor on the shared install, after which every later login fixture in
    # the run would need a TOTP code. The full ceremony is exercised instead by
    # test_mfa_full_ceremony below, which arms AND disarms inside one test so
    # the install is left exactly as it was found.
    "GET /api/auth/mfa": Happy(check=_check_mfa_state),
    # Turning the flag ON is the precondition for the enrol row below, and is
    # inert on its own — offering the factor arms nothing.
    "POST /api/auth/mfa/offer": Happy(
        body=lambda _ctx: {"offered": True}, check=_check_mfa_offer
    ),
    "GET /api/auth/signing-keys": Happy(check=_check_signing_keys),
    "POST /api/auth/signing-keys/rotate": Happy(check=_check_signing_key_rotated),
    "POST /api/auth/signing-keys/{key_id}/remove": Happy(
        path=_happy_signing_key_remove_path,
        check=_check_signing_key_removed,
    ),
    "POST /api/auth/mfa/enroll": Happy(
        path=_happy_mfa_enroll_path, check=_check_mfa_enroll
    ),
    "GET /install.sh": Happy(
        identity="none",
        path="/install.sh?token=conf-happy-boot-token",
        nonjson="text/plain bootstrap script (lifecycle.md §5), not spec JSON",
        check=_check_install_sh,
    ),
    "GET /api/warden/binary": Happy(
        identity="none",
        nonjson="binary artifact download, not spec JSON",
        check=_check_binary,
    ),
    "GET /api/agent/binary": Happy(
        identity="none",
        nonjson="binary artifact download, not spec JSON",
        check=_check_binary,
    ),
    # ── owner credential + settings (B3) ─────────────────────────────────────
    "GET /api/auth/status": Happy(
        identity="none",
        check=lambda _c, r: _expect(r, lambda d: d["password_set"] is True),
    ),
    "GET /api/settings": Happy(
        check=lambda _c, r: _expect(
            r, lambda d: d["owner_token_ttl"] > 0 and d["agent_token_ttl"] > 0 and 40 <= d["handover_pct"] <= 90
        ),
    ),
    "GET /api/push/public-key": Happy(
        check=lambda _c, r: _expect(r, lambda d: bool(d["public_key"])),
    ),
    "POST /api/push/subscription": Happy(
        body={
            "endpoint": "https://push.example.test/conformance",
            "keys": {"p256dh": "conformance-p256dh", "auth": "conformance-auth"},
        },
        status=204,
        nonjson="204 subscription save has no response body",
        check=lambda _c, _r: None,
    ),
    "DELETE /api/push/subscription": Happy(
        body={"endpoint": "https://push.example.test/conformance"},
        status=204,
        nonjson="204 subscription deletion has no response body",
        check=lambda _c, _r: None,
    ),
    "PATCH /api/settings": Happy(
        # Patch to the defaults: exercises the write path without steering the
        # shared instance away from its expected knobs.
        body={"owner_token_ttl": 86400, "agent_token_ttl": 604800, "handover_pct": 50},
        check=lambda _c, r: _expect(
            r, lambda d: d["owner_token_ttl"] == 86400 and d["agent_token_ttl"] == 604800 and d["handover_pct"] == 50
        ),
    ),
    "GET /api/release/check": Happy(
        # $OC_RELEASE_API_BASE is pinned unroutable (run.sh), so the fresh
        # check deterministically answers the honest degraded verdict: 200
        # {"status":"unknown"} with current_version mirroring /api/version and
        # no fabricated latest tag/link. The reachable-GitHub verdicts are
        # pinned in the server unit tests (update_check_test.go).
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "unknown"
            and d["current_version"]
            and d["latest_tag"] is None
            and d["release_url"] is None,
        ),
    ),
    # ── infra seams ──────────────────────────────────────────────────────────
    "POST /api/mint": Happy(
        body=lambda ctx: {"member_id": ctx.agent.member_id, "ttl_days": 1},
        check=_check_login,
    ),
    "POST /api/mcp": Happy(
        body={"jsonrpc": "2.0", "id": 1, "method": "tools/list"},
        nonjson="JSON-RPC face (spec/mcp.md), not an OpenAPI response schema",
        check=_check_mcp_tools_list,
    ),
    # ── members ──────────────────────────────────────────────────────────────
    "GET /api/members": Happy(check=_nonempty_list),
    "POST /api/members": Happy(
        body=lambda _ctx: {"name": f"conf-happy-hire-{uuid.uuid4().hex[:8]}"},
        check=lambda _c, r: _expect(r, lambda d: d["id"]),
    ),
    "GET /api/members/{member_id}": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}",
        check=lambda ctx, r: _expect(
            r, lambda d: d["id"] == ctx.agent.member_id
        ),
    ),
    "PATCH /api/members/{member_id}": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}",
        body={"name": "conf-happy-renamed"},
        check=lambda _c, r: _expect(r, lambda d: d["name"] == "conf-happy-renamed"),
    ),
    "PUT /api/members/{member_id}/avatar": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/avatar"
        "?filename=conf-avatar.png&mime=image/png",
        body=_PNG_BYTES,
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["member_id"] == ctx.agent.member_id
            and d["avatar_url"].startswith("/api/chat/attachment/ava-")
            and d["mime"] == "image/png"
            and d["filename"] == "conf-avatar.png",
        ),
    ),
    "DELETE /api/members/{member_id}/avatar": Happy(
        path=_seeded_avatar_delete_path,
        check=_check_avatar_delete,
    ),
    "POST /api/members/{member_id}/activate": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/activate",
        body={},
        check=lambda _c, r: _expect(r, lambda d: d["desired_state"] == "online"),
    ),
    "POST /api/members/{member_id}/relocate": Happy(
        # placement-only 改機器: writes desired_machine_id, NEVER touches
        # desired_state (the activate contrast). The pin must name a REAL
        # machine — this file's own onboarded one. The check pins BOTH: the pin
        # landed AND desired_state was NOT flipped online.
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/relocate",
        body=lambda ctx: {"machine_id": ctx.machine_id},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["desired_machine_id"] == ctx.machine_id
            and d.get("desired_state") != "online",
        ),
    ),
    "POST /api/members/{member_id}/deactivate": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/deactivate",
        check=lambda _c, r: _expect(r, lambda d: d["desired_state"] == "offline"),
    ),
    "POST /api/members/{member_id}/force-stop": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/force-stop",
    ),
    "POST /api/members/{member_id}/cost/reset": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/cost/reset",
        check=_check_cost_reset_receipt,
    ),
    "POST /api/accounts/cost/reset": Happy(
        body={"account": "conf-happy-untouched-account"},
        check=_check_account_cost_reset_receipt,
    ),
    "DELETE /api/members/{member_id}": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}",
        check=lambda _c, r: _expect(r, lambda d: d["roster_status"] == "removed"),
    ),
    # ── webhooks (M4) — a member's 回呼端點 config CRUD (admin_agent floor since
    # T-5336; the DTO carries the endpoint's plaintext inlet token) ───────────
    "GET /api/members/{member_id}/webhooks": Happy(
        path=lambda ctx: f"/api/members/{_happy_webhook(ctx)[0]}/webhooks",
        check=_nonempty_list,
    ),
    "POST /api/members/{member_id}/webhooks": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/webhooks",
        body=lambda _ctx: {"endpoint_id": f"conf-hook-{uuid.uuid4().hex[:8]}",
                           "purpose": "conf happy create"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "enabled"
            and d["token"]
            and d["endpoint_id"].startswith("conf-hook-"),
        ),
    ),
    "PATCH /api/members/{member_id}/webhooks/{endpoint_id}": Happy(
        path=lambda ctx: "/api/members/{}/webhooks/{}".format(*_happy_webhook(ctx)),
        body={"status": "disabled", "purpose": "conf happy patched"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "disabled"
            and d["purpose"] == "conf happy patched",
        ),
    ),
    "DELETE /api/members/{member_id}/webhooks/{endpoint_id}": Happy(
        path=lambda ctx: "/api/members/{}/webhooks/{}".format(*_happy_webhook(ctx)),
        check=lambda _c, r: _expect(
            r, lambda d: d["endpoint_id"].startswith("conf-hook-")
        ),
    ),
    "GET /api/members/{member_id}/webhooks/{endpoint_id}/requests": Happy(
        path=_happy_webhook_requests_path,
        check=lambda _c, r: _expect(
            r,
            lambda d: len(d) == 1
            and d[0]["outcome"] == "delivered"
            and d[0]["body"] == "conf request-log seed"
            and d[0]["truncated"] is False,
        ),
    ),
    # ── scheduled messages (T-f059) — the clock-driven twin of the webhook
    # CRUD above; same admin_agent floor, no credential on the DTO ────────────
    "GET /api/members/{member_id}/scheduled-messages": Happy(
        path=lambda ctx: (
            f"/api/members/{_happy_scheduled_message(ctx)[0]}/scheduled-messages"
        ),
        check=_nonempty_list,
    ),
    "POST /api/members/{member_id}/scheduled-messages": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/scheduled-messages",
        body=lambda _ctx: {
            "label": "conf happy create",
            "body": "conformance scheduled create",
            "cadence": "daily",
            "hour": 9,
            "minute": 0,
            "timezone": "Asia/Taipei",
        },
        # last_fired_slot is seeded at creation, which is the wire-visible form
        # of "a new schedule does not fire the slot it was born after". An empty
        # cursor here would mean the next tick delivers immediately.
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "enabled"
            and d["id"].startswith("sch-")
            and d["timezone"] == "Asia/Taipei"
            and d["last_fired_slot"].endswith("T09:00+08:00"),
        ),
    ),
    "PATCH /api/members/{member_id}/scheduled-messages/{schedule_id}": Happy(
        path=lambda ctx: "/api/members/{}/scheduled-messages/{}".format(
            *_happy_scheduled_message(ctx)
        ),
        body={"status": "disabled", "label": "conf happy patched"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "disabled"
            and d["label"] == "conf happy patched",
        ),
    ),
    "DELETE /api/members/{member_id}/scheduled-messages/{schedule_id}": Happy(
        path=lambda ctx: "/api/members/{}/scheduled-messages/{}".format(
            *_happy_scheduled_message(ctx)
        ),
        check=lambda _c, r: _expect(r, lambda d: d["id"].startswith("sch-")),
    ),
    # T-8b0d: the SAME bounded wake snapshot as /api/resume-summary, for a
    # TARGET member (this file's own scratch agent) instead of the caller.
    # The check pins the control-others contract: identity in the body is
    # the TARGET's id, never the owner caller's — that is the whole point of
    # this row over the caller-locked "GET /api/resume-summary" one above.
    "GET /api/members/{member_id}/resume-summary": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/resume-summary",
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["identity"] == ctx.agent.member_id
            and isinstance(d.get("tasks"), list)
            and isinstance(d.get("overview"), dict),
        ),
    ),
    # ── webhook inlet (M4 §2) — PUBLIC, token-only (?t=); silent 200 for every
    # case so it never leaks endpoint existence. The anonymous face (no token)
    # is the lowest-friction happy probe; the accept/ignore delivery semantics
    # are pinned in the server unit tests (api_webhooks_test.go).
    "POST /in": Happy(
        identity="none",
        check=lambda _c, r: _expect(r, lambda d: d["status"] == "ok"),
    ),
    # ── self-report presence (identity from token — agent reports for ITSELF) ─
    "POST /api/self/waking": Happy(
        identity="agent",
        body={},
        check=lambda ctx, r: _expect(r, lambda d: d["id"] == ctx.agent.member_id),
    ),
    "POST /api/self/stopping": Happy(
        identity="agent",
        body={},
        check=lambda ctx, r: _expect(r, lambda d: d["id"] == ctx.agent.member_id),
    ),
    "POST /api/self/stopped": Happy(
        identity="agent",
        body={},
        check=lambda ctx, r: _expect(r, lambda d: d["id"] == ctx.agent.member_id),
    ),
    # ── chat ─────────────────────────────────────────────────────────────────
    "POST /api/chat": Happy(
        body=lambda ctx: {"to": ctx.agent.member_id, "body": "happy ping"},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["from"] == "owner"
            and d["to"] == ctx.agent.member_id
            and d["body"] == "happy ping",
        ),
    ),
    "GET /api/chat": Happy(
        path=_seeded_chat_path("/api/chat"), check=_nonempty_chat_page
    ),
    "GET /api/chat/attachment/{attachment_id}": Happy(
        path=lambda ctx: f"/api/chat/attachment/{ctx.attachment()[0]}",
        nonjson="raw attachment bytes, not spec JSON",
        check=_check_attachment_roundtrip,
    ),
    "GET /api/chat/attachments/{attachment_id}/share-link": Happy(
        path=lambda ctx: f"/api/chat/attachments/{ctx.attachment()[0]}/share-link",
        check=_check_share_link_shape,
    ),
    "GET /api/chat/attachments": Happy(
        path=_seeded_chat_path("/api/chat/attachments"), check=_nonempty_list
    ),
    "GET /api/diff": Happy(path=_diff_pair_path, check=_check_diff_pair),
    "GET /api/diff/share-link": Happy(
        path=lambda ctx: _diff_pair_path(ctx).replace("/api/diff?", "/api/diff/share-link?", 1),
        check=_check_diff_share_link,
    ),
    "POST /api/chat/attachments": Happy(
        identity="agent",
        path="/api/chat/attachments?filename=conf-upload.png",
        body=_PNG_BYTES,
        check=_check_upload_ref,
    ),
    "POST /api/chat/mark-read": Happy(
        body=lambda ctx: {"peer": ctx.agent.member_id, "last_read_ts": 1.0},
    ),
    "GET /api/chat/reads": Happy(),
    "GET /api/chat/unread-count": Happy(
        check=lambda _c, r: _expect(r, lambda d: isinstance(d["unread"], int)),
    ),
    # ── reply cards ──────────────────────────────────────────────────────────
    "POST /api/reply-cards": Happy(
        identity="agent",
        body={"kind": "action", "summary": "conf happy open card",
              "options": [{"text": "done, continue"}], "linked_task": None},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "waiting"
            and d["answer"] is None
            and d["answered_ts"] is None
            and d["chat_message_id"],
        ),
    ),
    "GET /api/reply-cards": Happy(
        path=_seeded_reply_cards_path, check=_nonempty_list
    ),
    "GET /api/reply-cards/count": Happy(
        check=lambda _c, r: _expect(r, lambda d: d["waiting"] >= 1),
    ),
    "GET /api/reply-cards/{card_id}": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_card(ctx)}",
        check=lambda ctx, r: _expect(
            r, lambda d: d["from"] == ctx.agent.member_id
        ),
    ),
    "POST /api/reply-cards/{card_id}/answer": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_card(ctx)}/answer",
        body={"option_idxs": [0]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "answered"
            and d["answer"]["option_idxs"] == [0]
            and d["answered_ts"],
        ),
    ),
    "PUT /api/reply-cards/{card_id}/answer": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_answered_card(ctx)}/answer",
        body={"text": "conf happy revised"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "answered"
            and d["answer"]["text"] == "conf happy revised"
            and d["answer"]["option_idxs"] is None,
        ),
    ),
    "POST /api/reply-cards/{card_id}/expire": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_card(ctx)}/expire",
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "expired"
            and d["expired_ts"]
            and d["answer"] is None
            and d["answered_ts"] is None,
        ),
    ),
    # ── telemetry / monitoring ───────────────────────────────────────────────
    "POST /api/agent/context": Happy(
        identity="agent",
        body={"context_pct": 42},
        check=lambda _c, r: _expect(r, lambda d: d["context_pct"] == 42),
    ),
    "POST /api/monitoring/telemetry": Happy(
        identity="agent", body={"rate_limits": {"primary_used_pct": 1}}
    ),
    "GET /api/monitoring": Happy(),
    # T-da06. The conformance server is a fresh workdir: no SCHEDULED backup has
    # landed, so the honest answer is `unknown`. Asserting membership of the
    # closed vocabulary would be tautological (the server can emit nothing else)
    # — the assertion that carries weight is that a studio with no retreat point
    # does NOT read healthy, which is the entire defect this ticket removed.
    "GET /api/backup-health": Happy(
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "unknown"
            and d["stale_after_secs"] == 43200,
        ),
    ),
    # ── display-name overlays ────────────────────────────────────────────────
    "PATCH /api/accounts/{account_id}": Happy(
        path="/api/accounts/conf-happy-account",
        body={"display_name": "Conf Happy Account"},
        check=lambda _c, r: _expect(
            r, lambda d: d["display_name"] == "Conf Happy Account"
        ),
    ),
    "PATCH /api/machines/{machine_id}": Happy(
        path=lambda ctx: f"/api/machines/{ctx.machine_id}",
        body={"display_name": "Conf Happy Machine"},
        check=lambda _c, r: _expect(
            r, lambda d: d["display_name"] == "Conf Happy Machine"
        ),
    ),
    # ── machines ─────────────────────────────────────────────────────────────
    "GET /api/machines": Happy(check=_nonempty_list),
    "POST /api/machines": Happy(
        body=lambda _ctx: {
            "display_name": f"conf-happy-machine-{uuid.uuid4().hex[:8]}"
        },
        check=lambda _c, r: _expect(
            r,
            lambda d: d["machine_id"]
            and d["claim_code"]
            and d["claim_expires_in"] == 600
            and f"/install.sh?code={d['claim_code']}" in d["boot_command"]
            and d["token"] not in d["boot_command"],
        ),
    ),
    "GET /api/machines/{machine_id}/boot-command": Happy(
        path=lambda ctx: f"/api/machines/{ctx.machine_id}/boot-command",
        check=lambda _c, r: _expect(
            r,
            lambda d: d["claim_code"]
            and d["claim_expires_in"] == 600
            and f"/install.sh?code={d['claim_code']}" in d["boot_command"]
            and d["token"] not in d["boot_command"],
        ),
    ),
    "POST /api/machines/claim": Happy(
        identity="none",
        body=lambda ctx: {"code": _onboard_claim(ctx)["claim_code"]},
        check=_check_claim_token_authenticates,
    ),
    "POST /api/machines/{member_id}/uninstall": Happy(
        path=lambda ctx: f"/api/machines/{ctx.machine_id}/uninstall",
    ),
    "POST /api/machines/{member_id}/upgrade": Happy(
        # The scratch machine's warden is OFFLINE → nothing to command:
        # honest dispatched=false, no durable write (fire-and-forget verb).
        path=lambda ctx: f"/api/machines/{ctx.machine_id}/upgrade",
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["member_id"] == ctx.machine_id
            and d["machine_id"] == ctx.machine_id
            and d["dispatched"] is False,
        ),
    ),
    "DELETE /api/machines/{member_id}": Happy(
        path=lambda ctx: f"/api/machines/{ctx.fresh_machine()}",
    ),
    # ── global context / roles / lessons / bootstrap ─────────────────────────
    # ── document history (T-7d33) ───────────────────────────────────────────
    "GET /api/document-history/{kind}/{key}": Happy(
        path="/api/document-history/global_context/global",
    ),
    "GET /api/document-history/{kind}/{key}/seed": Happy(
        path="/api/document-history/global_context/global/seed",
        # The user-custom block's default IS the empty document, and the reader
        # that compares against it needs the field NAME present (an absent key
        # and an empty string are different documents to a diff).
        check=lambda _c, r: _expect(
            r,
            lambda d: d["kind"] == "global_context"
            and d["key"] == "global"
            and d["content"]["text"] == ""
            and d["content"]["tombstoned"] == "true",
        ),
    ),
    # The BODY of one named revision (T-1170). It reuses the same seeded
    # revision the restore row aims at, so the row exercises a version that
    # REALLY EXISTS rather than a 404 dressed up as coverage — and it asserts
    # the text, which is the whole point of the route: the listing above no
    # longer carries any.
    "GET /api/document-history/{kind}/{key}/{id}": Happy(
        path=_happy_document_version,
        check=lambda _c, r: _expect(
            r,
            lambda d: d["kind"] == "global_context"
            and d["key"] == "global"
            and d["content"]["text"] == "conformance happy history v1",
        ),
    ),
    "POST /api/document-history/{kind}/{key}/{id}/restore": Happy(
        path=_happy_restorable_revision,
        check=lambda _c, r: _expect(r, lambda d: d["id"] and d["content"]),
    ),
    "GET /api/global-context": Happy(),
    "POST /api/global-context": Happy(
        body={"text": "conformance happy user-custom block"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["text"] == "conformance happy user-custom block"
            and d["is_default"] is False,
        ),
    ),
    "POST /api/global-context/reset": Happy(
        check=lambda _c, r: _expect(r, lambda d: d["is_default"] is True),
    ),
    # ── the boot context's other two blocks, editable since T-791e ───────────
    # The reset rows run against a block the replace rows may or may not have
    # edited first: reset is idempotent and replace is a whole document, so
    # neither row depends on the other's order.
    "GET /api/system-interaction": Happy(
        check=lambda _c, r: _boot_doc_read("system_interaction", "global")(_c, r)
    ),
    "POST /api/system-interaction": Happy(
        body=_boot_doc_body, check=_boot_doc_written("system_interaction", "global")
    ),
    "POST /api/system-interaction/reset": Happy(
        check=_boot_doc_reset("/api/system-interaction")
    ),
    "GET /api/boot-sequence/{runtime_key}": Happy(
        path="/api/boot-sequence/claude",
        check=lambda _c, r: _boot_doc_read("boot_sequence", "claude")(_c, r),
    ),
    "POST /api/boot-sequence/{runtime_key}": Happy(
        path="/api/boot-sequence/codex",
        body=_boot_doc_body,
        check=_boot_doc_written("boot_sequence", "codex"),
    ),
    "POST /api/boot-sequence/{runtime_key}/reset": Happy(
        path="/api/boot-sequence/codex/reset",
        check=_boot_doc_reset("/api/boot-sequence/codex"),
    ),
    # ── 〈停止〉, the fourth owner-editable global document (T-c9c0) ──────────
    # A singleton keyed `global`, so it takes the system-interaction shape
    # rather than the boot-sequence one: no runtime on the path.
    "GET /api/offboard": Happy(
        check=lambda _c, r: _boot_doc_read("offboard", "global")(_c, r)
    ),
    "POST /api/offboard": Happy(
        body=_boot_doc_body, check=_boot_doc_written("offboard", "global")
    ),
    "POST /api/offboard/reset": Happy(check=_boot_doc_reset("/api/offboard")),
    # ── the GENERIC face of all of the above, plus the six event procedures
    # that never got named routes (T-3201) ───────────────────────────────────
    # The write rows aim at accelerated_stop because it is the STOP-side document
    # with a read-only head, so one row exercises both halves of the split.
    # ⚠️ This used to say "because it is EDITABLE: two of the ten refuse every
    # caller with 405" — since T-6f44's decision 2 NONE of the ten is read-only,
    # so that reason no longer picks anything out. The 405 branch still exists in
    # the server, it just has no shipped document behind it any more.
    "GET /api/boot-docs/{kind}/{key}": Happy(
        path="/api/boot-docs/task_closeout/global",
        check=lambda _c, r: _boot_doc_read("task_closeout", "global")(_c, r),
    ),
    "POST /api/boot-docs/{kind}/{key}": Happy(
        path="/api/boot-docs/accelerated_stop/global",
        body=_boot_doc_body,
        check=_boot_doc_written("accelerated_stop", "global"),
    ),
    "POST /api/boot-docs/{kind}/{key}/reset": Happy(
        path="/api/boot-docs/accelerated_stop/global/reset",
        check=_boot_doc_reset("/api/boot-docs/accelerated_stop/global"),
    ),
    "GET /api/roles": Happy(check=_nonempty_list),
    "GET /api/doc-sizes": Happy(
        # Size-only overview: every capped document reports its own size and
        # its OWN segment's cap, and NO document text rides along (no
        # definition_md / text / sop_md / learnings anywhere in the payload).
        check=lambda _c, r: _expect(
            r,
            lambda d: isinstance(d.get("roles"), list)
            and isinstance(d.get("task_manuals"), list)
            and d["roles"]
            and all(
                isinstance(row.get(seg), dict)
                and isinstance(row[seg].get("size_chars"), int)
                and isinstance(row[seg].get("cap_chars"), int)
                for row in d["roles"]
                for seg in ("duty", "insight", "lessons")
            )
            and all(
                isinstance(row.get(seg), dict)
                and isinstance(row[seg].get("size_chars"), int)
                and isinstance(row[seg].get("cap_chars"), int)
                for row in d["task_manuals"]
                for seg in ("sop", "learnings")
            )
            # Exact key sets, so a document body cannot ride along under any
            # name — an absence check naming the fields we happen to know about
            # would go green on a payload that renamed them.
            and set(d) == {"roles", "task_manuals"}
            and all(
                set(row) == {"role_key", "duty", "insight", "lessons"}
                and set(row[seg]) == {"size_chars", "cap_chars"}
                for row in d["roles"]
                for seg in ("duty", "insight", "lessons")
            )
            and all(
                set(row) == {"type_key", "sop", "learnings"}
                and set(row[seg]) == {"size_chars", "cap_chars"}
                for row in d["task_manuals"]
                for seg in ("sop", "learnings")
            ),
        ),
    ),
    "POST /api/roles": Happy(
        body=lambda _ctx: {"name": f"Conf Happy Role {uuid.uuid4().hex[:8]}"},
        check=lambda _c, r: _expect(r, lambda d: d["role"]["key"]),
    ),
    "GET /api/roles/{role}": Happy(
        path="/api/roles/assistant",
        check=lambda _c, r: _expect(r, lambda d: d["key"] == "assistant"),
    ),
    "POST /api/roles/{role}": Happy(
        path=lambda ctx: f"/api/roles/{ctx.fresh_role()}",
        body={"name": "Conf Happy Renamed"},
        check=lambda _c, r: _expect(r, lambda d: d["name"] == "Conf Happy Renamed"),
    ),
    "POST /api/roles/{role}/reset": Happy(
        path="/api/roles/assistant/reset",
        check=lambda _c, r: _expect(r, lambda d: d["key"] == "assistant"),
    ),
    "DELETE /api/roles/{role}": Happy(
        path=lambda ctx: f"/api/roles/{ctx.fresh_role()}",
    ),
    "GET /api/lessons/{role_key}": Happy(
        path="/api/lessons/assistant",
    ),
    "POST /api/lessons/{role_key}": Happy(
        path="/api/lessons/assistant",
        body={"text": "conformance happy lessons doc"},
        check=lambda _c, r: _expect(
            r, lambda d: d["text"] == "conformance happy lessons doc"
        ),
    ),
    "POST /api/lessons/{role_key}/patch": Happy(
        # Anchor-addressed patch (T-8327): an APPEND edit (empty old) always
        # lands regardless of the doc's current content; the receipt carries
        # size_chars/cap_chars/sha256 verification anchors instead of the full
        # text. T-3aeb renamed `size` -> `size_chars` (a size field must carry
        # its unit) and added the cap the write was judged against, so a caller
        # can compute its remaining budget without a second request.
        path="/api/lessons/assistant/patch",
        body={"edits": [{"old": "", "new": "conformance happy patch line"}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["applied_edits"] == 1
            and d["size_chars"] > 0
            and d["cap_chars"] >= d["size_chars"]
            and "size" not in d
            and len(d["sha256"]) == 64
            and d["is_default"] is False,
        ),
    ),
    # ── insight (T-3809) ─────────────────────────────────────────────────────
    # The role journal's third block. Its key is the BARE role_key — which
    # since T-2 is also what the lessons trio above uses.
    "GET /api/insight/{role_key}": Happy(
        path="/api/insight/assistant",
        # ⚠️ This row deliberately does NOT assert the empty doc, and the reason
        # is worth keeping. The first version asserted text == "" and
        # is_default is True — "an untouched insight doc reads as genuinely
        # empty", which is the one observable this ticket ships. It failed on
        # the first live run: test_auth_matrix drives the SAME server earlier in
        # the session and its write cells had already put content in
        # assistant's doc. Any "this doc has never been written" assertion is
        # unsound in a suite that shares one server across files — the property
        # is real, but this is not the layer that can see it. It belongs to a
        # server unit test and to the cockpit's mock.
        #
        # What IS order-independent is the size/cap contract, and it is worth
        # more than the lessons GET row (which asserts nothing at all):
        # size_chars must be the CHARACTER count of the served text — Python's
        # len() over a str counts code points, exactly as the server counts
        # runes — and the cap must be at or above it. A server that reported
        # UTF-16 units, or bytes, or a stale cap, would still return 200 here.
        check=lambda _c, r: _expect(
            r,
            lambda d: isinstance(d["text"], str)
            and d["size_chars"] == len(d["text"])
            and d["cap_chars"] >= d["size_chars"]
            and d["role_key"] == "assistant"
            # T-6501. has_seed is order-independent in a way `text` is not: it
            # asks whether seeds/insight_assistant.md SHIPS, which no write in
            # this suite can change. It is also the field the cockpit gates the
            # reset row on, so a server that never set it (false) would silently
            # remove the only path back to the factory doc.
            and d["has_seed"] is True,
        ),
    ),
    "POST /api/insight/{role_key}": Happy(
        path="/api/insight/assistant",
        body={"text": "conformance happy insight doc"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["text"] == "conformance happy insight doc"
            # The write flips is_default off. Since T-e1e3 a role MAY have a
            # factory seed behind it, so this flip is what distinguishes
            # authored text from shipped text — it is no longer interchangeable
            # with "the doc is non-empty".
            and d["is_default"] is False
            and d["size_chars"] == len("conformance happy insight doc")
            and d["cap_chars"] >= d["size_chars"],
        ),
    ),
    "POST /api/insight/{role_key}/patch": Happy(
        # An APPEND edit (empty `old`) always lands regardless of the doc's
        # current content, so this row does not depend on what the replace row
        # above left behind. The receipt carries size_chars / cap_chars / sha256
        # verification anchors instead of the full text — same shape as the
        # lessons patch receipt, and `size` (unitless) must stay absent.
        path="/api/insight/assistant/patch",
        body={"edits": [{"old": "", "new": "conformance happy insight patch"}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["applied_edits"] == 1
            and d["size_chars"] > 0
            and d["cap_chars"] >= d["size_chars"]
            and "size" not in d
            and len(d["sha256"]) == 64
            and d["is_default"] is False,
        ),
    ),
    "POST /api/insight/{role_key}/reset": Happy(
        # T-6501. The way back to the PER-ROLE factory seed — reset_role's
        # counterpart on the Insight block. The path callable writes its own
        # precondition (see _reset_insight_path): a reset that restored nothing
        # would answer 200 with a spec-shaped body, so the row has to know what
        # the document looked like BEFORE it ran.
        path=_reset_insight_path,
        check=_check_reset_insight,
    ),
    "GET /api/resume-summary": Happy(
        check=lambda _c, r: _expect(
            r, lambda d: isinstance(d.get("tasks"), list)
        ),
    ),
    "GET /api/resume-summary-size": Happy(
        # Size-only PEEK: the overview counts/sizes + a derived
        # estimated_total_chars, and NO content keys (no chat/tasks bodies).
        check=lambda _c, r: _expect(
            r,
            lambda d: isinstance(d.get("overview"), dict)
            and isinstance(d["overview"].get("chat_chars"), int)
            and isinstance(d.get("estimated_total_chars"), int)
            and "chat" not in d
            and "tasks" not in d,
        ),
    ),
    "POST /api/bootstrap": Happy(body={}, check=_check_bootstrap_preview),
    # ── tasks (M3) ───────────────────────────────────────────────────────────
    "GET /api/tasks": Happy(path=_seeded_tasks_path, check=_nonempty_list),
    "POST /api/tasks": Happy(
        identity="agent",
        body=lambda ctx: {"title": "conf happy create",
                          "executor_member_id": ctx.agent.member_id},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["deduped"] is False
            and d["task"]["status"] == "not_started"
            and d["task"]["executor_id"] == ctx.agent.member_id
            # task_no IS the id (T-5291) — before that it was a separately
            # derived display value (same wording as test_tasks.py; the old
            # shape is deliberately not named there either).
            and d["task"]["task_no"] == d["task"]["id"],
        ),
    ),
    "GET /api/tasks/count": Happy(
        path=_seeded_task_count_path,
        check=lambda _c, r: _expect(r, lambda d: d["open"] >= 1),
    ),
    "GET /api/tasks/{task_id}": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}",
        check=lambda _c, r: _expect(r, lambda d: d["closed_ts"] is None),
    ),
    "POST /api/tasks/{task_id}/terminate": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/terminate",
        check=lambda _c, r: _expect(
            r, lambda d: d["status"] == "terminated" and d["closed_ts"]
        ),
    ),
    "POST /api/tasks/{task_id}/priority": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/priority",
        body={"priority": "frozen"},
        check=lambda _c, r: _expect(
            r, lambda d: d["priority"] == "frozen" and d["frozen_by"] == "owner"
        ),
    ),
    "POST /api/tasks/{task_id}/message": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/message",
        body={"body": "conf happy task message"},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["from"] == "owner"
            and d["to"] == ctx.agent.member_id
            and d["meta"]["task_id"],
        ),
    ),
    "POST /api/tasks/{task_id}/reassign": Happy(
        # T-35e0: reassign to outsource lands the task UNASSIGNED (発包 → an
        # unassigned outsource task); the scheduler mints the successor later
        # under the global cap, so no worker is bound at reassign time. The
        # task enters the reassigning handover hold with executor_id="".
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/reassign",
        body={"target": {"kind": "outsource", "model": "sonnet",
                         "effort": "low"}},
        # reassigning is a LOCK now (T-9ca5), not a status; status stays DERIVED
        # (the fresh task has no steps → not_started).
        check=lambda _c, r: _expect(
            r,
            lambda d: d["lock"] == "reassigning"
            and d["executor_kind"] == "outsource"
            and d["executor_id"] == "",
        ),
    ),
    "POST /api/tasks/{task_id}/plan": Happy(
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/plan",
        body={"steps": [{"name": "one", "dod": "d1"},
                        {"name": "two", "dod": "d2", "is_gate": True}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["steps_total"] == 2
            and d["progress_total"] == 2
            and d["progress_done"] == 0,
        ),
    ),
    "POST /api/tasks/{task_id}/claim": Happy(
        # T-9ca5 claim (takeover): the NEW executor takes over a reassigned task,
        # clearing the reassigning lock. The task is reassigned TO the happy
        # agent (executor-guarded), so the happy agent claims it → lock cleared.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_reassigning_task(ctx)}/claim",
        check=lambda _c, r: _expect(r, lambda d: d["lock"] == ""),
    ),
    "POST /api/tasks/{task_id}": Happy(
        # T-646a: the executor corrects its own task's title AND description in
        # ONE call — the case its two predecessors could not express, and the
        # reason this route exists. Aimed at the same CLOSED task as the two
        # rows below and for the same reason: a terminal task stays correctable,
        # and a 200 echoing both new values on a card whose artifact set is
        # frozen is the wire statement of that.
        #
        # The check reads BOTH fields back rather than only asserting 200 — a
        # route that accepted the body and wrote nothing, or wrote one field and
        # dropped the other, would pass a status check. It also re-asserts the
        # task is still done and still closed, so a text write that quietly
        # disturbed the terminal state could not pass either.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}",
        body={"title": "one call", "description": "both fields"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["title"] == "one call"
            and d["description"] == "both fields"
            and d["status"] == "done"
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/description": Happy(
        # T-e271: the executor corrects its own task's wording. Aimed at a
        # CLOSED (done) task deliberately — owner ruling 2 says a terminal task
        # stays correctable, and the response echoing the new text on a task
        # whose artifact set is frozen is the wire statement of that. The check
        # reads the description back rather than only asserting 200: a route
        # that accepted the body and wrote nothing would pass a status check.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}/description",
        body={"description": "corrected wording"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["description"] == "corrected wording"
            and d["status"] == "done"
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/title": Happy(
        # T-2ebe: the executor corrects its own task's title. Aimed at the same
        # CLOSED task as its description twin above and for the same reason — a
        # terminal task stays correctable, and a 200 that echoes the new title on
        # a card whose artifact set is frozen is the wire statement of that. The
        # check reads the title back rather than only asserting 200: a route that
        # accepted the body and wrote nothing would pass a status check. It also
        # re-asserts the task is still done and still closed, so a title write
        # that quietly disturbed the task's terminal state could not pass either.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}/title",
        body={"title": "corrected title"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["title"] == "corrected title"
            and d["status"] == "done"
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/duplicate": Happy(
        # T-02c9: mark a fresh task a duplicate of a fresh original — the
        # subject is executed by the happy agent, so the executor guard passes.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/duplicate",
        body=lambda ctx: {"duplicate_of": _happy_task(ctx)},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "duplicated"
            and bool(d["duplicate_of"])
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/status": Happy(
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/steps/{}/status".format(
            *_happy_task_step(ctx)),
        body={"status": "in_progress"},
        check=lambda _c, r: _expect(
            r, lambda d: d["step_status"] == "in_progress"
            and d["task_status"] == "in_progress"
            and d["progress_done"] == 0 and d["progress_total"] == 1
        ),
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/note": Happy(
        # T-cc3e. Written against a PENDING step on purpose: _happy_task_step
        # always leaves the step pending, and the note being writable
        # with no status report first is the ticket's whole claim (waiting_reason
        # is the one bound to a status; this one is not). The check reads the
        # receipt's echoed note, so a handler that 200s without storing anything
        # cannot pass.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/steps/{}/note".format(
            *_happy_task_step(ctx)),
        body={"note": "conf happy note — 做到哪、下一步接什麼"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["note"] == "conf happy note — 做到哪、下一步接什麼"
            and d["step_status"] == "pending"
            and bool(d["task_id"]) and bool(d["step_id"]),
        ),
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/note/patch": Happy(
        # T-1667. Appends onto a step whose note is still empty (each Happy row
        # gets its own scratch task/step), so the check reads BOTH halves of the
        # receipt: applied_edits proves the engine ran, and the echoed note
        # proves what landed — a handler that 200s without storing cannot pass.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/steps/{}/note/patch".format(
            *_happy_task_step(ctx)),
        body={"edits": [{"old": "", "new": "conf happy note patch"}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["applied_edits"] == 1
            and d["note"] == "conf happy note patch",
        ),
    ),
    "GET /api/tasks/{task_id}/steps/{step_id}": Happy(
        # T-66: the single-step read. The check is on the VALUE that came back,
        # not on the shape: a note is written through the real write face first
        # and this row asserts the same text comes out, plus the self-declared
        # detail_level="full" that tells a caller this response is the whole
        # step. A handler that answered the summary projection (no note) or
        # forgot the marker cannot pass.
        identity="agent",
        path=_happy_step_with_note,
        check=lambda _c, r: _expect(
            r,
            lambda d: d["detail_level"] == "full"
            and d["note"] == _HAPPY_STEP_NOTE
            and d["note_size_chars"] == len(_HAPPY_STEP_NOTE)
            and d["note_cap_chars"] > 0,
        ),
    ),
    "POST /api/tasks/{task_id}/deps": Happy(
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/deps",
        body=lambda ctx: {"blocked_by": [_happy_task(ctx)]},
        check=lambda _c, r: _expect(r, lambda d: len(d["deps"]) == 1),
    ),
    "POST /api/tasks/{task_id}/closeout": Happy(
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}/closeout",
        # T-bb70: the close-out answers a BOUNDED receipt, not the whole task.
        # The key-set equality is the point — asserting only that the fields are
        # present would stay green if the route went back to serving the task,
        # because a whole task carries closeout_reported too.
        check=lambda _c, r: _expect(
            r,
            lambda d: set(d) == {
                "task_id", "task_status", "closeout_reported", "closeout_ts"
            }
            and d["closeout_reported"] is True
            and d["task_status"] == "done"
            and d["closeout_ts"] > 0,
        ),
    ),
    "POST /api/tasks/{task_id}/artifact": Happy(
        # T-3dc5: the executing agent pins a deliverable. A link artifact needs
        # no upload, so it is the lowest-friction happy body; the response is a
        # bounded receipt naming the pinned artifact and the resulting count.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/artifact",
        body={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
        check=lambda _c, r: _expect(
            r, lambda d: d["artifact_id"] != "" and d["artifact_count"] == 1
        ),
    ),
    "DELETE /api/tasks/{task_id}/artifact/{artifact_id}": Happy(
        # T-3dc5 (owner ruling 2026-07-18): the executing agent un-pins its own
        # task's artifact — the lowest-friction identity now that remove shares
        # add's agent+executor model. The response is a bounded receipt naming
        # the artifact that went and the count that is left.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/artifact/{}".format(
            *_happy_task_artifact(ctx)),
        check=lambda _c, r: _expect(r, lambda d: d["artifact_count"] == 0),
    ),
    "GET /api/tasks/{task_id}/artifacts": Happy(
        # T-66: the full-artifact read. The check is on the VALUES that came
        # back, not on the shape — the artifact is pinned through the real write
        # face first and this row asserts the same url/label/kind come out,
        # plus the self-declared artifacts_detail_level="full" that tells a
        # caller this response is the whole row. A handler that answered the
        # id+label INDEX the task view carries (no url, no kind) cannot pass,
        # and neither can one that forgot the marker.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/artifacts".format(
            *_happy_task_artifact(ctx)),
        check=lambda _c, r: _expect(
            r,
            lambda d: d["artifacts_detail_level"] == "full"
            and len(d["artifacts"]) == 1
            and d["artifacts"][0]["kind"] == "link"
            and d["artifacts"][0]["url"] == "https://example.com/pr/1"
            and d["artifacts"][0]["label"] == "conf PR"
            and d["artifacts"][0]["created_ts"] > 0,
        ),
    ),
    "POST /api/tasks/{task_id}/artifact/{artifact_id}/replace": Happy(
        # T-60: the executing agent swaps a pinned deliverable's content while
        # its id stays put. The check reads the id back out of the receipt and
        # compares it with the one the fixture pinned — a replace that minted a
        # new artifact (remove+add under another name) would pass a status check
        # and fail here, which is the whole reason the verb exists.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/artifact/{}/replace".format(
            *_happy_replaceable_artifact(ctx)),
        body={"url": "https://example.com/pr/2", "label": "conf PR v2"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["artifact_id"] == _REPLACE_TARGET["id"]
            and d["artifact_count"] == 1
            and d["version_count"] == 2,
        ),
    ),
    "GET /api/tasks/{task_id}/artifact/{artifact_id}/history": Happy(
        # T-60: the version list of an artifact that has just been replaced —
        # exactly one retained version, carrying what the live row held before.
        #
        # The seed is a FILE deliverable on purpose. A link version's url is the
        # row's own column and passes on a projection that copies the row; a
        # file's is NOT — the column is empty for file/image, and the reachable
        # address is the retained blob's serve path. Running this row against a
        # link therefore proved nothing about the class this journal mostly
        # holds, and every retained report read as gone on the real wire.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/artifact/{}/history".format(
            *_happy_replaced_file_artifact(ctx)),
        check=lambda _c, r: _expect(
            r,
            lambda d: len(d) == 1
            and d[0]["kind"] == "file"
            and d[0]["url"]
            == f"/api/chat/attachment/{_REPLACED_FILE['attachment_id']}"
            and d[0]["attachment_id"] == _REPLACED_FILE["attachment_id"]
            and d[0]["mime"] == "application/octet-stream"
            and d[0]["is_image"] is False
            and d[0]["filename"] == "report.md",
        ),
    ),
    # ── outsource panel (M3) ─────────────────────────────────────────────────
    "GET /api/outsource-workers": Happy(
        check=lambda _c, r: _expect(r, lambda d: isinstance(d, list)),
    ),
    # ── task manuals (M3) ────────────────────────────────────────────────────
    "GET /api/task-manuals": Happy(),
    "POST /api/task-manuals": Happy(
        # agent floor (owner ruling 2026-07-13): agents author task types.
        # T-fa76 system-key flow: the caller passes display_name only; the
        # server mints the tm- type_key and returns it (the caller addresses
        # later calls by it). The legacy explicit-type_key path stays pinned
        # via _happy_manual above.
        identity="agent",
        body=lambda _ctx: {"display_name": f"conf 顯示名 {uuid.uuid4().hex[:8]}"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["type_key"].startswith("tm-")
            and len(d["type_key"]) == len("tm-") + 12
            and d["display_name"].startswith("conf 顯示名 ")
            and d["fields"] == []
            and d["assignee"] == {},
        ),
    ),
    "GET /api/task-manuals/{type_key}": Happy(
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}",
    ),
    # ── custom themes (T-83ef) — themes left /api/settings and got their own
    # resource; "change one theme" is now one request instead of re-sending
    # every theme with every embedded image.
    "GET /api/themes": Happy(
        path=lambda ctx: (_happy_theme(ctx), "/api/themes")[1],
        # The list carries id and name ONLY. Asserting the ABSENCE is the point:
        # a list of whole bundles would satisfy any "id and name are present"
        # check, and serving whole bundles is exactly what this resource exists
        # not to do (owner ruling 2026-08-18).
        check=lambda _c, r: _expect(
            r, lambda d: len(d) >= 1 and set(d[0]) == {"id", "name"}
        ),
    ),
    "GET /api/themes/{theme_id}": Happy(
        path=lambda ctx: f"/api/themes/{_happy_theme(ctx)}",
        check=lambda _c, r: _expect(r, lambda d: d["colors"]["--color-bg"] == "#101018"),
    ),
    "PUT /api/themes/{theme_id}": Happy(
        path=lambda ctx: f"/api/themes/{_happy_theme_for_put(ctx)}",
        body=lambda ctx: {
            "id": _happy_theme_for_put(ctx), "name": "conf happy rename",
            "colors": {"--color-bg": "#202028"},
        },
        # created is False: the row already exists, so this is the REPLACE face.
        check=lambda _c, r: _expect(r, lambda d: d["created"] is False),
    ),
    "DELETE /api/themes/{theme_id}": Happy(
        path=lambda ctx: f"/api/themes/{_happy_theme(ctx)}",
        check=lambda _c, r: _expect(
            r, lambda d: d["deleted"] is True and d["display_theme_reset"] is False
        ),
    ),
    "POST /api/task-manuals/{type_key}": Happy(
        # agent floor (owner ruling 2026-07-13): content fields are
        # agent-editable (assignee is governance: owner/admin agent since
        # T-6020 — see test_tasks.py).
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}",
        body={"purpose": "conf happy purpose",
              "fields": [{"name": "pr", "required": True, "is_key": True}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["purpose"] == "conf happy purpose"
            and d["fields"][0]["is_key"] is True,
        ),
    ),
    "DELETE /api/task-manuals/{type_key}": Happy(
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}",
        check=lambda _c, r: _expect(r, lambda d: d["deleted"] is True),
    ),
    "POST /api/task-manuals/{type_key}/learnings": Happy(
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}/learnings",
        body={"text": "conf happy learnings"},
        check=lambda _c, r: _expect(
            r, lambda d: d["learnings"] == "conf happy learnings"
        ),
    ),
    "POST /api/task-manuals/{type_key}/learnings/patch": Happy(
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}/learnings/patch",
        body={"edits": [{"old": "", "new": "conf happy patch"}]},
        check=lambda _c, r: _expect(r, lambda d: d["applied_edits"] == 1),
    ),
    "POST /api/task-manuals/{type_key}/sop/patch": Happy(
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}/sop/patch",
        body={"edits": [{"old": "", "new": "conf happy sop patch"}]},
        check=lambda _c, r: _expect(r, lambda d: d["applied_edits"] == 1),
    ),
    # ── product guide (docs/guide embed) ────────────────────────────────────
    "GET /api/docs": Happy(
        # machine floor — docs/guide/why.md always lists (T-68f1: the repo
        # README no longer ships as slug "readme").
        check=lambda _c, r: _expect(
            r, lambda d: any(row["slug"] == "why" for row in d)
        ),
    ),
    "GET /api/docs/{slug}": Happy(
        path="/api/docs/why",
        check=lambda _c, r: _expect(
            r,
            lambda d: d["slug"] == "why" and len(d["markdown_md"]) > 0,
        ),
    ),
}

# Manifest rows deliberately NOT happy-tested (reason required — the coverage
# tooth enforces the union).
SKIPPED_HAPPY: dict[str, str] = {
    "POST /api/auth/set-password": (
        "the positive face needs an UNSET password + the serve-log claim token; "
        "the harness seeds the password before serve, so no claim token exists. "
        "The already-set 409 is pinned in test_set_password_after_set_conflicts "
        "below; the full first-run flow is pinned in the server unit tests "
        "(api_settings_test.go)."
    ),
    "POST /api/auth/change-password": (
        "the positive face rotates the shared owner credential AND revokes the "
        "session-scoped owner token fixture (password_changed_at iat cut) — it "
        "would poison every later test. The wrong-current 401 face is pinned in "
        "the auth matrix; the full change/revocation semantics in the server "
        "unit tests (api_settings_test.go)."
    ),
    "POST /api/auth/mfa/activate": (
        "the positive face ARMS the owner's second factor on the shared install, "
        "after which every later login fixture in the run would need a TOTP code. "
        "Exercised end-to-end by test_mfa_full_ceremony below, which arms and "
        "then disarms inside one test so the install is left as found; the 409 "
        "and 401 faces are pinned in the auth matrix."
    ),
    "POST /api/auth/mfa/disable": (
        "the positive face needs an ARMED factor (see above) plus a live code. "
        "Exercised by test_mfa_full_ceremony below; its nothing-armed 409 face is "
        "pinned in the auth matrix."
    ),
    "GET /api/events": (
        "SSE stream, not a JSON response — behaviour contract lives in "
        "spec/sse.md; the auth matrix probes its status face."
    ),
    "POST /api/members/{member_id}/refocus": (
        "online-only: the happy face needs a live SSE member (out of black-box "
        "batch scope); its offline 409 is pinned in test_error_envelope.py."
    ),
    "POST /api/self/refocus": (
        "restart_self: the happy face needs a live SSE session with a stamped "
        "boot_ts (online-only + minimum-liveness floor, out of black-box batch "
        "scope); its offline 409 is pinned in the auth matrix, and the online "
        "200 stamp + 429 liveness refusal in the server unit tests "
        "(api_members_restartself_test.go)."
    ),
    "POST /api/machines/renew-credential": (
        "the positive face needs a WARDEN identity: the endpoint names no target "
        "and mints for the caller's own verified sub, so owner/agent — the only "
        "identities this file has — get the 403 refusal, not the renewal. The "
        "warden 200, the machine_id it reports and the 403 for every non-machine "
        "principal are pinned in the auth matrix (which does have that identity), "
        "and the credential's usability plus the removed-machine refusal in the "
        "server unit tests (api_machines_renew_tfc53_test.go)."
    ),
    "POST /api/machines/{machine_id}/bootstrap-here": (
        "positive face runs `ocwarden install` on the HOST under test — a side "
        "effect the black-box harness must not trigger (matrix DEGRADED row)."
    ),
    "POST /api/machines/{machine_id}/teardown-here": (
        "no positive face EXISTS any more (T-42a0): the route refuses every "
        "target — the server-local machine because retiring it revokes "
        "credentials fleet-wide, any other machine because the verb carries no "
        "machine selector and cannot reach it. Both 409s and the ordering "
        "(unknown id still resolves to 404 first) are pinned in the server unit "
        "tests (api_machines_teardown_target_t42a0_test.go); the authz faces are "
        "fully asserted in the matrix."
    ),
    "POST /api/theme/fetch": (
        "the positive face needs an EXTERNAL http origin serving a valid theme "
        "bundle — the black-box harness is deliberately hermetic (same reason "
        "$OC_RELEASE_API_BASE is pinned unroutable), and standing a second "
        "server up for one row would trade that away. The format 422 is pinned "
        "in the auth matrix; the fetch-and-import path end to end, the timeout "
        "and size ceilings, and the theme-shape validation are pinned in the "
        "server unit tests (api_theme_fetch_t29c7_test.go) against a real "
        "httptest origin."
    ),
    "POST /api/update/upgrade": (
        "the positive face needs a reachable GitHub Releases repo holding a "
        "newer published release — the harness pins $OC_RELEASE_API_BASE "
        "unroutable on purpose (hermeticity). The no-newer-known 409 is pinned "
        "in the auth matrix owner cell and test_upgrade_no_newer_conflicts "
        "below; the precondition and execution semantics (pin → download → "
        "sha256 verify via checksums.txt → swap → restart, failures 502 with "
        "the old binary untouched) in the server unit tests "
        "(update_check_test.go / upgrade_test.go)."
    ),
    "GET /api/outsource-workers/{id}": (
        "the positive face needs a LIVE worker row, mintable only by the Phase 2 "
        "assignment scheduler (no black-box mint path). The unknown-404 / "
        "anonymous-401 faces are pinned in the "
        "auth matrix; the projection fold (machine/account/context/cost/"
        "delegated_by) in the server unit tests (api_outsource_test.go, "
        "TestListOutsourceWorkers_RuntimeFold)."
    ),
    "GET /api/outsource-workers/{id}/boot-context": (
        "T-ba6b initial-prompt preview: the positive face needs a LIVE worker "
        "row + its bound task, mintable only by the Phase 2 assignment scheduler "
        "(no black-box mint path — same reasoning as GET /api/outsource-workers/"
        "{id}). The below-owner-403 / owner-404 faces are pinned in the auth "
        "matrix; the re-assembled boot-context fold (codename/task/identity, "
        "never a token, unknown-worker 404) in the server unit tests "
        "(api_outsource_test.go, TestGetWorkerBootContext / "
        "TestGetWorkerBootContext_UnknownWorker404)."
    ),
    "POST /api/outsource-workers/{id}/relocate": (
        "T-f190 owner 改機器: the positive face needs a LIVE worker row + an "
        "online target warden, neither of which the black-box harness can mint. "
        "The below-owner-403 / owner-404 / unknown-machine faces are pinned in "
        "the auth matrix; the full relocate semantics (pin write + old-session "
        "stop + pinned-host start re-spawn — the P5b member verbs, no lifecycle "
        "change) in "
        "the server unit tests (api_outsource_test.go, TestRelocateOutsourceWorker)."
    ),
    "POST /api/outsource-workers/{id}/refocus": (
        "T-32e1 owner 換手: the positive face needs a LIVE, online worker row, "
        "mintable only by the Phase 2 scheduler (no black-box mint path — same "
        "reasoning as relocate). The below-owner-403 / owner-404 faces are pinned "
        "in the auth matrix; the online-only 409, refocus_since stamp, and "
        "kill+respawn in the server unit tests (worker_lifecycle_test.go, "
        "TestRefocusWorker_*)."
    ),
    "POST /api/members/{member_id}/accelerated-stop": (
        "T-ed79 owner 加速停止 (the middle rung): every face of it needs a member "
        "with a LIVE SSE session AND an already-open wind-down — the endpoint 409s "
        "without both, and this black-box suite can mint neither. The 409s "
        "(no session / nothing winding down / a forced epoch), the re-stamped "
        "anchor, the refocus_op=accelerated_stop write and the deadline the tick "
        "then collects on are pinned in the server unit tests "
        "(accelerated_stop_endpoint_ted79_test.go)."
    ),
    "POST /api/outsource-workers/{id}/accelerated-stop": (
        "T-ed79 加速停止 for a worker: the same two prerequisites as the member "
        "twin above (a live worker session and an open wind-down), and a worker row "
        "is mintable only by the Phase 2 scheduler. The below-owner-403 / "
        "owner-404 faces are pinned in the auth matrix; both arms (下線 and 換手), "
        "the 409s and the collect on the deadline in the server unit tests "
        "(worker_graceful_stop_ted79_test.go "
        "TestWorkerStop_AcceleratedStopEscalatesTheStopEpochAndIsHonoured)."
    ),
    "POST /api/outsource-workers/{id}/stop": (
        "T-f190 owner 停止, a GRACEFUL close-out since T-ed79: the positive face "
        "needs a LIVE worker row (no black-box mint path). The below-owner-403 / "
        "owner-404 faces are pinned in the auth matrix; the desired_state=offline "
        "set + refocus clear + 〈停止〉 notice + NO kill + collection on the "
        "worker's own report_stopped in the server unit tests "
        "(worker_graceful_stop_ted79_test.go, TestWorkerStop_* / "
        "TestStoppedWorker_TickNeverRevives)."
    ),
    "POST /api/outsource-workers/{id}/force-stop": (
        "T-ed79 owner 強制停止 (the third rung; the body /stop used to have): the "
        "positive face needs a LIVE worker row (no black-box mint path). The "
        "below-owner-403 / owner-404 faces are pinned in the auth matrix; the "
        "forced anchors + immediate session kill + no-revive + the SILENCE of the "
        "forced arm in the server unit tests (worker_lifecycle_test.go "
        "TestForceStopWorker_KillsAndHoldsDown, "
        "worker_forced_stop_parity_tc996_test.go)."
    ),
    "POST /api/outsource-workers/{id}/restart": (
        "T-f190 owner 重啟: the positive face needs a STOPPED worker row (no "
        "black-box mint path). The below-owner-403 / owner-404 faces are pinned in "
        "the auth matrix; the still-alive-409 (T-7526: the guard is LIVENESS, not "
        "'did anyone press stop' — a worker whose session died on its own IS "
        "restartable) + desired_state=online set + re-dispatch in the server unit "
        "tests (worker_lifecycle_test.go, TestRestartWorker_ClearsAndRedispatches / "
        "TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn)."
    ),
    "POST /api/outsource-workers/{id}/model": (
        "T-f190 owner 換 model: the positive face needs a LIVE worker row (no "
        "black-box mint path). The all-identities-404 faces are pinned in "
        "the auth matrix (T-ed79 dropped this row to the machine floor, so there "
        "is no below-floor 403 face left); the model/effort persist + active-respawn / "
        "assigned-persist-only in the server unit tests (worker_lifecycle_test.go, "
        "TestSetWorkerModel_*)."
    ),
    "GET /api/docs/assets/{name}": (
        "probed with a missing asset name (404 across identities) in the auth "
        "matrix — a positive face would couple this suite to O-46's exact image "
        "filenames under docs/guide/assets. The serve + content-type round-trip "
        "is pinned decoupled in the server unit tests (api_docs_test.go, "
        "TestReadDocAssetFromServesBytesWithContentType)."
    ),
}


def _expect(r: httpx.Response, predicate: Callable[[Any], Any]) -> None:
    data = r.json()
    assert predicate(data), f"semantic check failed on: {json.dumps(data)[:500]}"


# ── plumbing ─────────────────────────────────────────────────────────────────

_MANIFEST: list[dict[str, str]] = json.loads(
    (HERE / "routes_manifest.json").read_text(encoding="utf-8")
)
_MANIFEST_KEYS = [f"{r['method']} {r['path']}" for r in _MANIFEST]


def _response_schema(method: str, template: str, status: int) -> Any:
    op = SPEC["paths"][template][method.lower()]
    resp = op["responses"][str(status)]
    return resp.get("content", {}).get("application/json", {}).get("schema")


_PARAMS = [
    pytest.param(key, id=key.replace(" ", ":"))
    for key in _MANIFEST_KEYS
    if key in HAPPY
]


@pytest.mark.parametrize("route_key", _PARAMS)
def test_rest_happy(hctx: HCtx, route_key: str) -> None:
    method, template = route_key.split(" ", 1)
    row = HAPPY[route_key]
    path = row.path(hctx) if callable(row.path) else (row.path or template)
    assert "{" not in path, f"unresolved path template for {route_key}: {path}"

    body = row.body(hctx) if callable(row.body) else row.body
    kwargs: dict[str, Any] = {"headers": _auth(hctx.token(row.identity))}
    if isinstance(body, (bytes, bytearray)):
        kwargs["content"] = bytes(body)  # raw octet-stream rows (upload)
    elif body is not None:
        kwargs["json"] = body
    r = hctx.client.request(method, path, **kwargs)

    assert r.status_code == row.status, (
        f"{route_key} happy face: expected {row.status}, "
        f"got {r.status_code} {r.text[:300]}"
    )

    if row.nonjson:
        assert row.check is not None, f"{route_key}: nonjson row needs a check"
    else:
        schema = _response_schema(method, template, row.status)
        assert schema is not None and schema != {}, (
            f"{route_key}: spec declares no JSON schema — mark the row nonjson "
            "with a reason instead of silently skipping shape validation"
        )
        problems = violations(r.json(), schema, SPEC)
        assert not problems, (
            f"{route_key}: response does not conform to spec/openapi.json:\n  "
            + "\n  ".join(problems)
        )

    if row.check is not None:
        row.check(hctx, r)


# ── extra semantic pin the table cannot express (two-faced bootstrap) ────────


def test_bootstrap_with_member_mints_token(hctx: HCtx) -> None:
    """lifecycle.md §2.3: bootstrap WITH member_id (a warden spawn) returns a
    freshly minted member JWT — non-null, and it must actually authenticate."""
    member_id = hctx.fresh_member()
    r = hctx.client.post(
        "/api/bootstrap",
        json={"member_id": member_id},
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    token = r.json()["token"]
    assert isinstance(token, str) and token, "spawn bootstrap must mint a token"
    probe = hctx.client.get("/api/members", headers=_auth(token))
    assert probe.status_code == 200, "minted bootstrap token failed to authenticate"


def test_update_task_status_route_is_gone(hctx: HCtx) -> None:
    """T-8449: the retired task-level status report route is REMOVED from the
    wire — the executor's report lands on no handler at all (404), and the task
    never mutates. (The step-level report route stays — the derivation input.)"""
    task_id = _happy_task(hctx)
    h = _auth(hctx.agent.token)
    r = hctx.client.post(
        f"/api/tasks/{task_id}/status", json={"status": "in_progress"}, headers=h)
    assert r.status_code == 404, (
        f"POST /api/tasks/{{id}}/status must be gone (404), got "
        f"{r.status_code} {r.text[:200]}")
    # Nothing mutated — the task is still not_started.
    got = hctx.client.get(f"/api/tasks/{task_id}", headers=_auth(hctx.owner_token))
    assert got.status_code == 200 and got.json()["status"] == "not_started", got.text


def test_claim_code_is_single_use(hctx: HCtx) -> None:
    """A claim code redeems exactly once: onboard → claim 200 (token bound to
    the onboarded machine) → the SAME code again is a flat 401."""
    ob = _onboard_claim(hctx)
    r = hctx.client.post("/api/machines/claim", json={"code": ob["claim_code"]})
    assert r.status_code == 200, r.text
    data = r.json()
    assert data["machine_id"] == ob["machine_id"], data
    again = hctx.client.post("/api/machines/claim", json={"code": ob["claim_code"]})
    assert again.status_code == 401, (
        f"a spent claim code must 401, got {again.status_code} {again.text[:200]}"
    )


def test_install_sh_code_variant_claims_before_download(hctx: HCtx) -> None:
    """install.sh?code= serves the claim-code variant: a HEAD probe of the
    warden binary route runs FIRST (a 503-ing server must not burn the
    one-time code), then the code is templated into a POST /api/machines/claim
    exchange that runs BEFORE the binary download, and sed joins the tool
    precheck. The legacy ?token= variant is pinned untouched by the
    GET /install.sh happy row."""
    r = hctx.client.get("/install.sh?code=conf-happy-claim-code")
    assert r.status_code == 200, r.text
    assert r.headers.get("content-type", "").startswith("text/plain"), r.headers
    body = r.text
    assert '"code":"conf-happy-claim-code"' in body, "code not templated into the claim body"
    assert "for tool in tmux curl sed; do" in body, "sed missing from the precheck"
    probe_at = body.find("curl -fsI ")
    claim_at = body.find("/api/machines/claim")
    # the double quote skips the '# Usage: curl -fsSL ...' comment line
    download_at = body.find('curl -fsSL "')
    assert 0 <= probe_at < claim_at, "the binary probe must precede the claim exchange"
    assert claim_at < download_at, "the claim exchange must precede the binary download"
    assert "/api/warden/binary" in body[probe_at:claim_at], "the probe must hit the binary route"


# ── machine placement: an explicit machine id, or nothing ────────────────────


def _desired_machine(hctx: HCtx, member_id: str) -> str:
    r = hctx.client.get(f"/api/members/{member_id}", headers=_auth(hctx.owner_token))
    assert r.status_code == 200, r.text
    return r.json()["desired_machine_id"]


def test_relocate_requires_a_machine_that_resolves(hctx: HCtx) -> None:
    """Placement is an EXPLICIT decision: relocate takes a real machine id, and
    since the owner ruling of 2026-07-27 it takes NOTHING ELSE. Every other
    non-blank value — the retired "auto" spelling included — is the same honest
    404 a typo'd id has always been, so a member can never be pinned to a
    destination dispatch cannot reach; and "" is no longer the unpin it used to
    be (that made the pin-destroying form the one you got by forgetting a
    field), it is a 400 that leaves the pin exactly where it was."""
    member_id = hctx.fresh_member()
    h = _auth(hctx.owner_token)
    before = _desired_machine(hctx, member_id)
    for bad in ("auto", "warden-nope"):
        r = hctx.client.post(
            f"/api/members/{member_id}/relocate", json={"machine_id": bad}, headers=h)
        assert r.status_code == 404, f"{bad!r}: {r.status_code} {r.text[:200]}"
        assert _desired_machine(hctx, member_id) == before, (
            f"a refused relocate must leave the pin untouched ({bad!r})")
    # Sentinel: a REAL machine still lands, so the refusals above are the rule
    # and not a broken fixture.
    r = hctx.client.post(
        f"/api/members/{member_id}/relocate",
        json={"machine_id": hctx.machine_id}, headers=h)
    assert r.status_code == 200, r.text
    assert r.json()["desired_machine_id"] == hctx.machine_id
    # "" is a semantic refusal (400) and the pin survives; an ABSENT key is the
    # missing-required-field face (422). Both leave the machine just pinned.
    for body, want in (({"machine_id": ""}, 400), ({}, 422)):
        r = hctx.client.post(
            f"/api/members/{member_id}/relocate", json=body, headers=h)
        assert r.status_code == want, f"{body!r}: {r.status_code} {r.text[:200]}"
        assert _desired_machine(hctx, member_id) == hctx.machine_id, (
            f"a refused relocate must leave the pin untouched ({body!r})")


def test_activate_requires_a_machine_that_resolves(hctx: HCtx) -> None:
    """activate's machine bind is held to the SAME rule as relocate: a non-blank
    machine_id must resolve. It matters most here because activate flips
    desired_state online in the same call — a refused bind must leave BOTH the
    pin and the desired_state untouched, never a member that wants to be online
    on a machine that does not exist."""
    member_id = hctx.fresh_member()
    h = _auth(hctx.owner_token)
    before = hctx.client.get(f"/api/members/{member_id}", headers=h).json()
    for bad in ("auto", "warden-nope"):
        r = hctx.client.post(
            f"/api/members/{member_id}/activate", json={"machine_id": bad}, headers=h)
        assert r.status_code == 404, f"{bad!r}: {r.status_code} {r.text[:200]}"
        got = hctx.client.get(f"/api/members/{member_id}", headers=h).json()
        assert got["desired_machine_id"] == before["desired_machine_id"], (
            f"a refused activate must leave the pin untouched ({bad!r})")
        assert got["desired_state"] != "online", (
            f"a refused activate must not wake the member ({bad!r})")
    # Sentinel: a REAL machine activates and pins in one call.
    r = hctx.client.post(
        f"/api/members/{member_id}/activate",
        json={"machine_id": hctx.machine_id}, headers=h)
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["desired_machine_id"] == hctx.machine_id, body
    assert body["desired_state"] == "online", body


def test_outsource_worker_relocate_requires_a_machine_that_resolves(
    hctx: HCtx,
) -> None:
    """The worker twin of the member rule. No black-box path mints a worker, so
    the positive face is DEGRADED to "the machine resolve passed and the WORKER
    is what 404s" — distinguishable because a refused machine names the machine
    in the error message, while a resolved one names the worker."""
    h = _auth(hctx.owner_token)
    for bad in ("auto", "warden-nope"):
        r = hctx.client.post(
            "/api/outsource-workers/ow-nope/relocate",
            json={"machine_id": bad}, headers=h)
        assert r.status_code == 404, f"{bad!r}: {r.status_code} {r.text[:200]}"
        assert f"machine '{bad}' not found" in r.text, r.text
    r = hctx.client.post(
        "/api/outsource-workers/ow-nope/relocate",
        json={"machine_id": hctx.machine_id}, headers=h)
    assert r.status_code == 404, r.text
    assert "machine" not in r.json()["error"]["message"], (
        "a resolvable machine must let the request reach the worker resolve")


# ── share-sig semantics (the ?sig= third auth path on the blob GET) ─────────


def _share_url(hctx: HCtx, att_id: str) -> str:
    r = hctx.client.get(
        f"/api/chat/attachments/{att_id}/share-link",
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    return r.json()["url"]


def _second_attachment(hctx: HCtx) -> str:
    """Seed a SECOND attachment (distinct from ctx.attachment()) so the
    single-file-grant face has a foreign blob to aim the sig at."""
    r = hctx.client.post(
        "/api/chat",
        json={
            "to": hctx.agent.member_id,
            "body": "share-sig foreign blob seed",
            "attachments": [
                {"data_b64": _PNG_B64, "filename": "conf2.png", "mime": "image/png"}
            ],
        },
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    return r.json()["attachments"][0]["id"]


def test_share_sig_serves_blob_without_credentials(hctx: HCtx) -> None:
    """A share link works BARE — no Authorization header, no ?token= — and
    returns the exact stored bytes."""
    att_id, payload = hctx.attachment()
    r = hctx.client.get(_share_url(hctx, att_id))
    assert r.status_code == 200, r.text
    assert r.content == payload, "share-sig fetch did not round-trip the bytes"


def test_share_sig_rejects_tampered_sig(hctx: HCtx) -> None:
    att_id, _ = hctx.attachment()
    url = _share_url(hctx, att_id)
    good_sig = url.split("sig=", 1)[1]
    bad_sig = good_sig[:-1] + ("A" if good_sig[-1] != "A" else "B")
    r = hctx.client.get(f"/api/chat/attachment/{att_id}?sig={bad_sig}")
    assert r.status_code == 401, f"tampered sig must 401, got {r.status_code}"
    assert hctx.client.get(f"/api/chat/attachment/{att_id}?sig=").status_code == 401


def test_share_sig_grants_exactly_one_attachment(hctx: HCtx) -> None:
    """The sig is an HMAC over ONE attachment id: replaying it against any
    other blob id is a 401 — a leaked link never widens into a second file."""
    att_id, _ = hctx.attachment()
    other_id = _second_attachment(hctx)
    assert other_id != att_id
    sig = _share_url(hctx, att_id).split("sig=", 1)[1]
    r = hctx.client.get(f"/api/chat/attachment/{other_id}?sig={sig}")
    assert r.status_code == 401, f"foreign-blob sig must 401, got {r.status_code}"


def test_share_sig_ignored_on_other_routes(hctx: HCtx) -> None:
    """?sig= is a credential ONLY on the blob GET: every other gated route
    stays 401 deny-by-default (the sig never becomes a general token)."""
    att_id, _ = hctx.attachment()
    sig = _share_url(hctx, att_id).split("sig=", 1)[1]
    for path in (
        f"/api/chat/attachments/{att_id}/share-link?sig={sig}",
        f"/api/chat?sig={sig}",
        f"/api/members?sig={sig}",
    ):
        r = hctx.client.get(path)
        assert r.status_code == 401, f"{path}: expected 401, got {r.status_code}"


def test_share_sig_never_shadows_a_bad_bearer(hctx: HCtx) -> None:
    """Precedence pin: a PRESENT bearer credential (header or ?token=) is
    verified as a token and a bad one stays 401 — it never falls through to a
    valid ?sig= riding the same request."""
    att_id, _ = hctx.attachment()
    sig = _share_url(hctx, att_id).split("sig=", 1)[1]
    r = hctx.client.get(
        f"/api/chat/attachment/{att_id}?sig={sig}",
        headers={"Authorization": "Bearer not-a-jwt"},
    )
    assert r.status_code == 401, f"bad bearer + good sig must 401, got {r.status_code}"
    r = hctx.client.get(f"/api/chat/attachment/{att_id}?token=not-a-jwt&sig={sig}")
    assert r.status_code == 401, f"bad ?token= + good sig must 401, got {r.status_code}"


# ── owner second factor: the whole ceremony, over the real wire ──────────────


def test_mfa_full_ceremony(hctx: HCtx) -> None:
    """enroll → activate → login-with-code → replay refused → disable.

    🔴 SELF-CLEANING BY CONSTRUCTION. This test ARMS the owner's second factor
    on the shared install, so it MUST disarm it again — while it is armed, every
    /api/login in the run needs a code. The disable is therefore not an
    afterthought assertion but the thing that keeps the rest of the suite
    honest, and the finally block guarantees it runs even when an assertion
    above it fails. That is also why the positive activate/disable faces are not
    ordinary HAPPY rows: a table row cannot guarantee its own cleanup.

    The codes are computed independently from RFC 6238 (see _totp_code) — this
    is the suite's proof that an ordinary authenticator app interoperates.

    ⚠️ THE SHARED FAILURE BUDGET THIS DOCSTRING USED TO WARN ABOUT NO LONGER
    EXISTS, and the warning is kept in negative form because it is exactly the
    kind of thing someone re-derives from an old memory. There is no attempt
    counter, no free allowance and no backoff any more (T-19 §0): the brake is a
    concurrency cap plus, on the PUBLIC seams only, a fixed per-refusal wait. So
    a refusal here no longer costs a later step anything, and the `finally`
    disable can no longer throttle ITSELF into a 429 — which two earlier versions
    of this test managed to do, leaving the factor ARMED and cascading into every
    later login fixture in the run.

    What a refusal DOES still cost is wall-clock, and only on the two public
    seams: a failed /api/login here waits out the server's refusal floor before
    answering. The mfa activate/disable refusals below are owner-gated and take
    no floor at all, so they are as fast as they ever were.
    """
    owner = _auth(hctx.owner_token)
    password = os.environ["OC_OWNER_PASSWORD"]

    # 🔴 THE STEP BUDGET. Codes are single-use (the server advances a replay
    # floor past every step it accepts) and only the current step ±1 is ever
    # accepted — three slots, and this ceremony consumes exactly three:
    #
    #   activate → step-1     login → step+0     disable → step+1
    #
    # They MUST be strictly increasing (each must clear the floor the previous
    # one left) and all three must stay inside the window, which is why the run
    # aligns to a fresh step first. This is real product behaviour, not a test
    # trick: an owner who logs in and immediately disables MFA must also wait
    # for their authenticator to roll over to a code they have not spent.
    _totp_align_to_step_start()

    # The ship-dark flag gates SET-UP, and its default is OFF — an install that
    # never opts in is untouched by this feature, so enrol would answer 403
    # without this. Offering it arms nothing.
    r = hctx.client.post("/api/auth/mfa/offer", json={"offered": True}, headers=owner)
    assert r.status_code == 200, f"offer: {r.status_code} {r.text}"
    assert r.json()["offered"] is True, r.json()
    assert r.json()["enrolled"] is False, "offering the feature must not arm anything"

    # ── enroll: an INERT pending secret ──────────────────────────────────────
    r = hctx.client.post("/api/auth/mfa/enroll", headers=owner)
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    enrolled = r.json()
    secret = enrolled["secret"]
    assert enrolled["enrolled"] is False, "enroll must not arm the factor"

    # Nothing is armed yet, so the public probe must still say so and a
    # password-only login must still work.
    probe = hctx.client.get("/api/auth/status").json()
    assert probe["mfa_required"] is False, probe
    assert (
        hctx.client.post("/api/login", json={"password": password}).status_code == 200
    ), "a pending (unproven) secret must not gate login"

    # 🔴 The owner token alone must NOT be able to arm a factor. A stolen token
    # could otherwise enrol a secret the attacker controls and activate it,
    # leaving the real owner locked out until someone runs `ocserverd
    # mfa-disable` on the host — strictly worse than the pre-MFA baseline.
    r = hctx.client.post(
        "/api/auth/mfa/activate",
        json={"password": "conf-definitely-wrong", "code": _totp_code(secret, step_offset=-1)},
        headers=owner,
    )
    assert r.status_code == 401, f"activate with a wrong password: {r.status_code} {r.text}"
    assert (
        hctx.client.get("/api/auth/status").json()["mfa_required"] is False
    ), "a factor was armed WITHOUT the password"

    armed = False
    try:
        # ── activate: prove a code, arming the factor ────────────────────────
        activation_code = _totp_code(secret, step_offset=-1)
        r = hctx.client.post(
            "/api/auth/mfa/activate",
            json={"password": password, "code": activation_code},
            headers=owner,
        )
        assert r.status_code == 200, f"activate: {r.status_code} {r.text}"
        armed = True
        assert r.json()["enrolled"] is True
        # The ACTIVE secret must never be echoed back — otherwise a stolen owner
        # token could read out the enrolment and clone the factor.
        assert r.json()["secret"] is None, r.json()
        assert r.json()["otpauth_uri"] is None, r.json()

        # The public probe now tells the login wall to render a code field.
        assert hctx.client.get("/api/auth/status").json()["mfa_required"] is True

        # ── login now REQUIRES the code ─────────────────────────────────────
        assert (
            hctx.client.post("/api/login", json={"password": password}).status_code
            == 401
        ), "password alone must not log in while a factor is armed"

        # The next slot up: activation SPENT step-1, so the code that armed the
        # factor cannot also open a session.
        login_code = _totp_code(secret, step_offset=0)
        r = hctx.client.post(
            "/api/login", json={"password": password, "code": login_code}
        )
        assert r.status_code == 200, f"two-factor login: {r.status_code} {r.text}"
        assert r.json()["token"], r.json()

        # ── the flag is a ROLLOUT switch, not a bypass ───────────────────────
        # Withdraw the feature while the factor is ARMED. Login must still demand
        # the code: if this ever stops holding, anyone holding a stolen owner
        # token could simply turn the feature off and walk past the second factor
        # that exists to stop exactly that.
        r = hctx.client.post("/api/auth/mfa/offer", json={"offered": False}, headers=owner)
        assert r.status_code == 200, f"withdraw: {r.status_code} {r.text}"
        assert r.json()["enrolled"] is True, "withdrawing the feature disarmed the factor"
        assert (
            hctx.client.get("/api/auth/status").json()["mfa_required"] is True
        ), "mfa_required went false while a factor is armed — the wall would hide the code field"
        assert (
            hctx.client.post("/api/login", json={"password": password}).status_code == 401
        ), "password alone logged in while the feature was withdrawn — the flag is a bypass"
        hctx.client.post("/api/auth/mfa/offer", json={"offered": True}, headers=owner)

        # ── the replay guard: that same code must not work twice ─────────────
        r = hctx.client.post(
            "/api/login", json={"password": password, "code": login_code}
        )
        assert r.status_code == 401, (
            f"a REPLAYED code logged in again ({r.status_code}) — the single-use "
            "floor is not holding"
        )

        # A wrong password with a right code is refused the same way, and the
        # refusal must not disclose WHICH half failed.
        # A failed login does NOT spend the step (the password is checked first
        # and short-circuits), so borrowing the disable slot here is safe.
        wrong_pwd = hctx.client.post(
            "/api/login",
            json={
                "password": "conf-definitely-wrong",
                "code": _totp_code(secret, step_offset=1),
            },
        )
        wrong_code = hctx.client.post(
            "/api/login", json={"password": password, "code": "000000"}
        )
        assert wrong_pwd.status_code == wrong_code.status_code == 401
        assert wrong_pwd.json() == wrong_code.json(), (
            "the refusal distinguishes a wrong password from a wrong code, which "
            f"confirms a correct password: {wrong_pwd.json()} vs {wrong_code.json()}"
        )
    finally:
        if armed:
            # Disarm with BOTH factors, using the LAST unspent slot in the
            # window (+1). Still no retry loop: a loop over offsets outside the
            # ±1 window can only fail, so it would burn the remaining window for
            # nothing. (It used to be worse than pointless — each failure spent
            # from a shared credential budget, which is how an earlier version of
            # this test throttled ITSELF into a 429 instead of disarming. That
            # budget is gone; the loop is still wrong.)
            r = hctx.client.post(
                "/api/auth/mfa/disable",
                json={"password": password, "code": _totp_code(secret, step_offset=1)},
                headers=owner,
            )
            assert r.status_code == 200, f"disable: {r.status_code} {r.text}"
            assert r.json()["enrolled"] is False
            # The install must be left EXACTLY as found, or every later login
            # fixture in this run breaks.
            assert hctx.client.get("/api/auth/status").json()["mfa_required"] is False
            assert (
                hctx.client.post(
                    "/api/login", json={"password": password}
                ).status_code
                == 200
            ), "password-only login must work again after the factor is removed"


# ── coverage teeth ───────────────────────────────────────────────────────────


def test_lore_proposal_refuses_a_base_digest_that_is_not_current(hctx: HCtx) -> None:
    """T-33 — 過期提案跟 PR 一模一樣的坑, on the wire.

    A proposal names the version it was written against. If that is not the
    version the entry stands at, the submission is refused 409 rather than
    stored: applying it later would silently discard whoever changed the entry in
    between, and NOTHING about the result would look wrong.

    The 200 at the end is the positive control. Without it a route that refused
    every proposal would satisfy the assertion above."""
    head = {"Authorization": f"Bearer {hctx.agent.token}"}
    entry_id = _lore_entry(hctx)
    sha = hctx.client.get(f"/api/lore/entries/{entry_id}", headers=head).json()["sha256"]

    body = {
        "kind": "remove",
        "base_sha256": "0" * 64,
        "encountered": "the conformance suite probing the staleness refusal",
        "fault": "never-true",
        "evidence": "this proposal names a version of the entry that never existed",
    }
    r = hctx.client.post(
        f"/api/lore/entries/{entry_id}/proposals", headers=head, json=body)
    assert r.status_code == 409, (
        f"a proposal against a version nobody holds must be refused 409, got "
        f"{r.status_code} {r.text[:300]}")
    message = r.json()["error"]["message"]
    # The words carry as much as the number: 409 alone does not tell a proposer
    # whether to re-read the entry or to fix his own body.
    assert "changed while you were reviewing it" in message, message
    assert sha in message and "0" * 64 in message, message

    body["base_sha256"] = sha
    ok = hctx.client.post(
        f"/api/lore/entries/{entry_id}/proposals", headers=head, json=body)
    assert ok.status_code == 200, (
        f"the SAME proposal against the current digest must land: "
        f"{ok.status_code} {ok.text[:300]}")


def test_lore_proposal_carries_its_own_events_and_names_the_ones_it_moves(
    hctx: HCtx,
) -> None:
    """T-33 — 提案帶得動第 5 格，而審核者看得出它動了哪幾筆.

    Owner ruling rc-e5c34500face (2026-09-03): 「改得動 —— 提案就該帶完整的新版本，
    包含所有事件」. Two things have to be true on the wire for that to be usable,
    and each fails silently on its own:

      * `events` is REQUIRED on an `update`. If omitting it meant 「維持現狀」,
        one forgotten field would propose deleting every event — and accepting
        replaces them wholesale, so the deletion would really land.
      * the response says WHICH events move. An addition is visible in the
        proposed list; a DELETION shows up only as an absence, and that is the
        half a reviewer misses.

    The 200 at the end is the positive control: without it, a route that refused
    every proposal would satisfy the first assertion."""
    head = {"Authorization": f"Bearer {hctx.agent.token}"}
    r = hctx.client.post(
        "/api/lore/entries",
        headers=head,
        json={
            "trigger": "I am checking what a proposal may move",
            "content": "a proposal carries the whole version, events included",
            "origin": "agent:conformance",
            "subjects": ["agent:conformance"],
            "events": [
                {"happened_ts": 1700000000.0, "what": "the derivation got this one right"},
                {"happened_ts": 1700000100.0, "what": "the derivation got this one wrong"},
            ],
        },
    )
    assert r.status_code == 200, f"seed entry: {r.status_code} {r.text[:300]}"
    entry_id = r.json()["entry_id"]
    sha = hctx.client.get(f"/api/lore/entries/{entry_id}", headers=head).json()["sha256"]

    body = {
        "kind": "update",
        "base_sha256": sha,
        "encountered": "the conformance suite reading this entry",
        "fault": "never-true",
        "evidence": "the second event names a thing that did not happen",
        "trigger": "I am checking what a proposal may move",
        "content": "a proposal carries the whole version, events included",
    }
    missing = hctx.client.post(
        f"/api/lore/entries/{entry_id}/proposals", headers=head, json=body)
    assert missing.status_code == 422, (
        f"an `update` that never mentions 第 5 格 must be refused 422 — otherwise a "
        f"forgotten field silently proposes deleting every event: "
        f"{missing.status_code} {missing.text[:300]}")

    body["events"] = [
        {"happened_ts": 1700000000.0, "what": "the derivation got this one right"},
        {"happened_ts": 1700000100.0, "what": "a person repaired this one by hand"},
    ]
    ok = hctx.client.post(
        f"/api/lore/entries/{entry_id}/proposals", headers=head, json=body)
    assert ok.status_code == 200, f"file: {ok.status_code} {ok.text[:300]}"

    d = hctx.client.get(
        f"/api/lore/entries/{entry_id}/proposals", headers=head).json()
    row = d["proposals"][0]
    assert [e["what"] for e in row["events"]] == [
        "the derivation got this one right",
        "a person repaired this one by hand",
    ], row
    assert [e["what"] for e in row["events_added"]] == [
        "a person repaired this one by hand"], row
    assert [e["what"] for e in row["events_removed"]] == [
        "the derivation got this one wrong"], row
    # 🔴 The untouched event is in NEITHER list. An id-based comparison would
    # report it as one deletion plus one addition, and that noise is what stops
    # people reading a diff at all.
    assert len(row["events_added"]) == 1 and len(row["events_removed"]) == 1, row
    # Both sides of the comparison are served, so a reviewer recomputes it
    # instead of trusting it — the rule `current_sha256` already follows.
    assert [e["what"] for e in d["current_events"]] == [
        "the derivation got this one right",
        "the derivation got this one wrong",
    ], d


def test_lore_search_refuses_an_undeclared_condition(hctx: HCtx) -> None:
    """🔴 THE ASSERTION THE WHOLE BODY-SIDE DESIGN EXISTS FOR.

    This route's entire value is its selection conditions, and a condition that
    is silently ignored does not raise — it hands back a plausible set of
    memories that is not the set that was asked for, and the symptom of that is
    "somebody forgot something today".

    Two halves, and BOTH are needed. A key the DTO does not declare must be
    REFUSED, naming itself. The same word on the QUERY STRING must be accepted
    and ignored — which is not a bug being pinned as correct, it is the reason
    the conditions had to be put in the body: `POST …?typo=1` is exactly as
    silent as the GET would be, so the verb protects nothing and the SIDE is
    what does. If the second half ever starts failing, the router changed and
    the design note explaining this choice has to be re-read, not deleted.
    """
    token = hctx.agent.token
    head = {"Authorization": f"Bearer {token}"}

    refused = hctx.client.post(
        "/api/lore/search", headers=head, json={"context_labels": ["anything"]}
    )
    assert refused.status_code == 422, f"{refused.status_code} {refused.text}"
    assert "context_labels" in refused.text, (
        "the refusal must name the field, or a caller cannot tell WHICH condition "
        f"was rejected: {refused.text}"
    )

    ignored = hctx.client.post(
        "/api/lore/search?context_labels=anything", headers=head, json={}
    )
    assert ignored.status_code == 200, (
        "an undeclared QUERY parameter is silently ignored on every route this "
        "station serves; if that changed, the body-side rule above needs "
        f"re-justifying rather than deleting: {ignored.status_code} {ignored.text}"
    )
    assert ignored.json()["applied"]["tiered_by"] == [], (
        "the query-string condition must have been ignored, not applied"
    )


def test_set_password_after_set_conflicts(hctx: HCtx) -> None:
    """Once a password is set, set-password is a flat 409 — the claim token is
    never consulted (no oracle for guessing it) and the credential is
    untouched."""
    r = hctx.client.post(
        "/api/auth/set-password",
        json={"password": "conf-stomp-password", "claim_token": "conf-any-token"},
    )
    assert r.status_code == 409, f"{r.status_code} {r.text}"
    login = hctx.client.post(
        "/api/login", json={"password": os.environ["OC_OWNER_PASSWORD"]}
    )
    assert login.status_code == 200, "the credential must be untouched"


def test_upgrade_no_newer_conflicts(hctx: HCtx) -> None:
    """With GitHub unreachable (the harness pins $OC_RELEASE_API_BASE at an
    unroutable loopback) no newer release is ever known, so the owner's
    explicit upgrade trigger is an honest 409 — never a fabricated upgrade."""
    r = hctx.client.post(
        "/api/update/upgrade", headers=_auth(hctx.owner_token)
    )
    assert r.status_code == 409, f"{r.status_code} {r.text}"
    body = r.json()
    assert body["error"]["code"] == "conflict", body


def test_settings_updater_server_fields_retired(hctx: HCtx) -> None:
    """The updater-server pair (updater_url + updater_invite_code) left the
    wire with the ocupdaterd teardown (t-dc68 — updates come from GitHub
    Releases now): reads carry NEITHER field, and a PATCH writing the retired
    keys is rejected as an unknown-field validation error."""
    h = _auth(hctx.owner_token)
    r = hctx.client.get("/api/settings", headers=h)
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    body = r.json()
    assert "updater_url" not in body, body
    assert "updater_invite_code" not in body, body
    assert "updater_invite_code_set" not in body, body

    r = hctx.client.patch(
        "/api/settings",
        json={
            "updater_url": "http://127.0.0.1:59999/",
            "updater_invite_code": "conf-retired-invite-code",
        },
        headers=h,
    )
    assert r.status_code == 422, f"{r.status_code} {r.text}"
    body = r.json()
    assert body["error"]["code"] == "validation_error", body
    assert "conf-retired-invite-code" not in r.text, "retired secret echoed"


def test_settings_updater_channel_toggles_roundtrip(hctx: HCtx) -> None:
    """The two software-update toggles (updater_receive_beta + updater_auto_update):
    both default false, PATCH flips each independently (partial semantics),
    reads reflect the live value. The test restores both OFF so the shared
    instance never runs with auto-update armed."""
    h = _auth(hctx.owner_token)
    r = hctx.client.get("/api/settings", headers=h)
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    body = r.json()
    assert body["updater_receive_beta"] is False, body
    assert body["updater_auto_update"] is False, body
    try:
        r = hctx.client.patch(
            "/api/settings", json={"updater_receive_beta": True}, headers=h
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        body = r.json()
        assert body["updater_receive_beta"] is True, body
        assert body["updater_auto_update"] is False, body  # untouched (PATCH)

        r = hctx.client.patch(
            "/api/settings", json={"updater_auto_update": True}, headers=h
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        body = r.json()
        assert body["updater_receive_beta"] is True, body
        assert body["updater_auto_update"] is True, body

        r = hctx.client.get("/api/settings", headers=h)
        assert r.status_code == 200
        body = r.json()
        assert body["updater_receive_beta"] is True, body
        assert body["updater_auto_update"] is True, body
    finally:
        r = hctx.client.patch(
            "/api/settings",
            json={"updater_receive_beta": False, "updater_auto_update": False},
            headers=h,
        )
        assert r.status_code == 200, f"restore failed: {r.status_code} {r.text}"
        body = r.json()
        assert body["updater_receive_beta"] is False, body
        assert body["updater_auto_update"] is False, body


def test_theme_background_and_mode_round_trip(hctx: HCtx) -> None:
    """The outer-canvas image and its lay-down mode are wire fields (T-081b), so
    the server — not just the client validator — decides what is admissible and
    what comes back. A legal image + mode round-trips durably; the mode
    vocabulary is closed; and a mode naming a zone that carries no image is a
    422 that writes nothing (a mode alone paints nothing, so it is a mistake
    worth naming rather than ignoring).

    T-83ef moved the transport, not the claims: themes left /api/settings, so
    this exercises PUT/GET /api/themes/{id}. One shape genuinely changed — the
    write no longer echoes the bundle back (it answers a receipt, because a
    bundle carries its images), so what the write stored is checked by reading
    it, which was the durability assertion here anyway."""
    h = _auth(hctx.owner_token)
    png = (
        "data:image/png;base64,"
        "iVBORw0KGgoAAAABAAAAAQ=="  # PNG magic + filler; the gate checks magic+size
    )
    bundle = {
        "id": "conf-canvas",
        "name": "Conformance canvas",
        "colors": {"--color-bg": "#101018"},
        "backgrounds": {"canvas": png},
        "backgroundModes": {"canvas": "cover"},
    }
    try:
        r = hctx.client.put("/api/themes/conf-canvas", json=bundle, headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        assert r.json()["created"] is True, r.text

        # Durable across a re-read — an omitted passthrough would empty it here.
        r = hctx.client.get("/api/themes/conf-canvas", headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        b = r.json()
        assert b["backgrounds"] == {"canvas": png}, b
        assert b["backgroundModes"] == {"canvas": "cover"}, b

        # The mode vocabulary is CLOSED — and unlike an unknown wording code,
        # this one is not lenient: the whole bundle is refused.
        for bad in ({"canvas": "contain"}, {"topbar": "tile"}):
            r = hctx.client.put(
                "/api/themes/conf-bad-mode",
                json={
                    "id": "conf-bad-mode",
                    "name": "Bad",
                    "colors": {"--color-bg": "#101018"},
                    "backgrounds": {"canvas": png},
                    "backgroundModes": bad,
                },
                headers=h,
            )
            assert r.status_code == 422, f"{bad}: {r.status_code} {r.text}"
            assert r.json()["error"]["code"] == "validation_error", r.text

        # A mode with no image behind it is refused too.
        r = hctx.client.put(
            "/api/themes/conf-lone-mode",
            json={
                "id": "conf-lone-mode",
                "name": "Lone",
                "colors": {"--color-bg": "#101018"},
                "backgroundModes": {"canvas": "sides"},
            },
            headers=h,
        )
        assert r.status_code == 422, f"{r.status_code} {r.text}"

        # …and none of the refusals stored anything: the refused ids have no row,
        # and the good one is untouched. (The old version asserted this by
        # listing settings' whole array; the id list is where that fact lives
        # now, and it is checked for the refused ids by NAME rather than by
        # counting, so an unrelated theme left by another row cannot mask it.)
        r = hctx.client.get("/api/themes", headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        ids = [b["id"] for b in r.json()]
        assert "conf-canvas" in ids, ids
        assert "conf-bad-mode" not in ids, ids
        assert "conf-lone-mode" not in ids, ids
    finally:
        r = hctx.client.delete("/api/themes/conf-canvas", headers=h)
        assert r.status_code in (200, 404), f"restore failed: {r.status_code} {r.text}"


def test_theme_unknown_wording_code_is_dropped_not_rejected(hctx: HCtx) -> None:
    """A theme bundle has ONE lenient rule inside an otherwise all-or-nothing
    validator: a `wording` code outside the message-key whitelist does not 422
    the bundle — it is dropped and the request succeeds, so an already-imported
    theme pack stays usable when the whitelist shrinks (T-081b). Everything else
    in the overlay stays strict, and the surviving codes are durable.

    T-83ef moved the transport only. The write used to echo the pruned overlay
    back; it now answers a receipt, so the prune is observed where it was always
    the more valuable claim — in what was STORED."""
    h = _auth(hctx.owner_token)
    try:
        r = hctx.client.put(
            "/api/themes/conf-wording",
            json={
                "id": "conf-wording",
                "name": "Conformance wording",
                "colors": {"--color-bg": "#101018"},
                "wording": {
                    "zh": {
                        "nav.tasks": "待辦",
                        "profile.themeOffice": "精靈村",
                        "not.a.real.key": "x",
                    }
                },
            },
            headers=h,
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"

        # Durable: a re-read carries only the surviving code.
        r = hctx.client.get("/api/themes/conf-wording", headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        zh = r.json()["wording"]["zh"]
        assert zh == {"nav.tasks": "待辦"}, zh

        # The leniency is scoped to the CODE. A language outside {zh,en} is
        # still a 422 and still writes nothing.
        r = hctx.client.put(
            "/api/themes/conf-bad-lang",
            json={
                "id": "conf-bad-lang",
                "name": "Bad",
                "colors": {"--color-bg": "#101018"},
                "wording": {"xian": {"nav.tasks": "仙"}},
            },
            headers=h,
        )
        assert r.status_code == 422, f"{r.status_code} {r.text}"
        assert r.json()["error"]["code"] == "validation_error", r.text
        r = hctx.client.get("/api/themes/conf-bad-lang", headers=h)
        assert r.status_code == 404, f"a refused write must store nothing: {r.text}"
    finally:
        r = hctx.client.delete("/api/themes/conf-wording", headers=h)
        assert r.status_code in (200, 404), f"restore failed: {r.status_code} {r.text}"


def test_upload_then_ref_post_roundtrip(hctx: HCtx) -> None:
    """The send-side seam end to end: upload raw bytes → post_chat with the
    light {id} ref → the message stamps the STORED blob's mime/filename (a
    filename/mime alongside the ref is ignored) → the blob serves back
    byte-exact. No base64 ever rides the message body."""
    payload = b"PK\x03\x04 conformance zip payload " * 64
    up = hctx.client.post(
        "/api/chat/attachments?filename=conf-ref.zip&mime=application/zip",
        content=payload,
        headers=_auth(hctx.agent.token),
    )
    assert up.status_code == 200, up.text
    ref = up.json()
    assert ref["mime"] == "application/zip" and ref["filename"] == "conf-ref.zip"

    posted = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [
            {"id": ref["id"], "filename": "spoof.txt", "mime": "text/plain"},
        ]},
        headers=_auth(hctx.agent.token),
    )
    assert posted.status_code == 200, posted.text
    atts = posted.json()["attachments"]
    assert len(atts) == 1 and atts[0]["id"] == ref["id"], atts
    assert atts[0]["mime"] == "application/zip", "stored blob must beat the ref's mime"
    assert atts[0]["filename"] == "conf-ref.zip", "stored blob must beat the ref's filename"

    served = hctx.client.get(
        f"/api/chat/attachment/{ref['id']}", headers=_auth(hctx.agent.token)
    )
    assert served.status_code == 200 and served.content == payload

    # Multi-reference: the same blob rides a second message untouched.
    again = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [{"id": ref["id"]}]},
        headers=_auth(hctx.agent.token),
    )
    assert again.status_code == 200, again.text


def test_chat_reply_to_is_the_servers_link_not_the_callers(hctx: HCtx) -> None:
    """T-4e95 「回覆這則」 over the real wire.

    The repo charter puts the BEHAVIOURAL close-out of a wire change here, and
    this field has four claims worth closing out over HTTP rather than only in
    Go: the link round-trips, a link OUT of this conversation is ACCEPTED and
    brings its quote with it, an id naming nothing is refused, and a
    caller-supplied ``meta.reply_to`` is discarded. The last is the one that
    needs a real request the most — ``meta`` is copied through wholesale, so the
    only thing standing between a caller and an unvalidated link is a deletion
    the handler performs before it validates anything.

    The second REVERSED on 2026-08-21 (owner ruling): this test asserted a 400
    and an "another conversation" message until that date. The refusal is gone
    because quoting a line out of two other people's thread in order to step in
    and ask about it is the use case, and it was the one thing the refusal made
    impossible.
    """
    quoted = hctx.client.post(
        "/api/chat",
        json={"to": hctx.agent.member_id, "body": "reply-to-target"},
        headers=_auth(hctx.owner_token),
    )
    assert quoted.status_code == 200, quoted.text
    quoted_id = quoted.json()["id"]
    assert quoted.json()["reply_to"] == "", "a plain post carries no link"

    # The commonest shape: answering what the other party sent you — a reply
    # travelling the opposite way to the message it quotes.
    reply = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "body": "reply-to-answer", "reply_to": quoted_id},
        headers=_auth(hctx.agent.token),
    )
    assert reply.status_code == 200, reply.text
    assert reply.json()["reply_to"] == quoted_id

    # Read it back off the wire — the POST response is built from the row the
    # handler just made, so it would look right even if nothing were stored.
    served = hctx.client.get(
        f"/api/chat?ids={reply.json()['id']}", headers=_auth(hctx.agent.token)
    )
    assert served.status_code == 200, served.text
    assert chat_messages(served)[0]["reply_to"] == quoted_id
    # …and the QUOTE came with it, built by the server on this read. This is the
    # half that makes the link usable: without it the browser would have to go
    # and fetch what the id names, which is the design this replaced.
    quote = chat_messages(served)[0].get("reply_to_chat")
    assert quote is not None, f"every read must carry the quote: {served.text}"
    assert quote["id"] == quoted_id
    assert quote["from"] == "owner"
    assert quote["to"] == hctx.agent.member_id
    assert quote["content"] == "reply-to-target"

    # A link OUT of this conversation, over the real wire: ACCEPTED, and the
    # quoted text crosses the boundary with it. A THIRD party's line, so this is
    # genuinely another conversation — owner↔agent in the other direction would
    # be the SAME one and would prove nothing.
    third = hctx.fresh_member()
    elsewhere = hctx.client.post(
        "/api/chat",
        json={"to": third, "body": "another-thread"},
        headers=_auth(hctx.owner_token),
    )
    assert elsewhere.status_code == 200, elsewhere.text
    sideways = hctx.client.post(
        "/api/chat",
        json={
            "to": "owner",
            "body": "quoting sideways",
            "reply_to": elsewhere.json()["id"],
        },
        headers=_auth(hctx.agent.token),
    )
    assert sideways.status_code == 200, sideways.text
    assert sideways.json()["reply_to"] == elsewhere.json()["id"]
    sideways_quote = sideways.json().get("reply_to_chat")
    assert sideways_quote is not None, (
        f"a cross-conversation reply must still carry its quote: {sideways.text}"
    )
    assert sideways_quote["content"] == "another-thread"
    # THE ADDRESSEE IS THE QUOTED MESSAGE'S OWN. This reply travels agent→owner
    # while the line it quotes travelled owner→third, so a projection that read
    # the ENCLOSING message's recipient would answer "owner" — the one wrong
    # answer that every same-conversation row above cannot tell apart.
    assert sideways_quote["from"] == "owner"
    assert sideways_quote["to"] == third, (
        "the quote must name the ORIGINAL's addressee, not the recipient of "
        f"the reply carrying it: {sideways.text}"
    )
    # Names are the OTHER half of the convention and are NOT resolved on this
    # door — only the wake snapshot fills them.
    assert sideways_quote.get("from_name", "") == ""
    assert sideways_quote.get("to_name", "") == ""

    # An id that names nothing is a 400 on the FIELD, not a 404 on the route.
    orphan = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "body": "orphan", "reply_to": "c-nosuchmessage"},
        headers=_auth(hctx.agent.token),
    )
    assert orphan.status_code == 400, orphan.text
    assert "c-nosuchmessage" in orphan.text

    # A caller-supplied meta.reply_to is DISCARDED — while the rest of meta,
    # which really is free-form passthrough, survives.
    forged = hctx.client.post(
        "/api/chat",
        json={
            "to": "owner",
            "body": "forged-link",
            "meta": {"reply_to": quoted_id, "keepme": "yes"},
        },
        headers=_auth(hctx.agent.token),
    )
    assert forged.status_code == 200, forged.text
    assert forged.json()["reply_to"] == "", "a meta-supplied link must not stand"
    assert forged.json()["meta"].get("keepme") == "yes"
    assert forged.json().get("reply_to_chat") is None, (
        "no link ⇒ no quote — a forged meta.reply_to must not conjure one either"
    )

    # A reply whose ORIGINAL CANNOT BE READ: the quote is absent, the link is
    # not, and the message is served normally. Reached here the way a real
    # station reaches it — the link was stamped by the server when the target
    # existed, and the read happens against a target that no longer resolves.
    # The POST door refuses an unknown id on purpose (asserted above), so this
    # state cannot be created through it.
    #
    # What is checked over the wire here is the ORDINARY, ALWAYS-PRESENT half:
    # a message that replies to nothing carries no quote key at all.
    plain = hctx.client.get(
        f"/api/chat?ids={quoted_id}", headers=_auth(hctx.agent.token)
    )
    assert plain.status_code == 200, plain.text
    assert chat_messages(plain)[0]["reply_to"] == ""
    assert chat_messages(plain)[0].get("reply_to_chat") is None, (
        "a message that answers nothing must carry no quote: " + plain.text
    )
    assert "reply_to" not in forged.json()["meta"]


def test_chat_recipient_validation_preserves_offline_mailbox(hctx: HCtx) -> None:
    """A valid member remains a durable mailbox while disconnected, but an
    invented recipient is rejected instead of becoming an orphaned chat row."""
    delivered = hctx.client.post(
        "/api/chat",
        json={"to": hctx.agent.member_id, "body": "offline-mailbox-probe"},
        headers=_auth(hctx.owner_token),
    )
    assert delivered.status_code == 200, delivered.text

    # The conformance agent has no SSE listener: this reads the persisted
    # mailbox, not a success that depended on live fan-out.
    mailbox = hctx.client.get(
        "/api/chat?with=owner&limit=-1", headers=_auth(hctx.agent.token)
    )
    assert mailbox.status_code == 200, mailbox.text
    assert any(m["body"] == "offline-mailbox-probe" for m in chat_messages(mailbox))

    rejected = hctx.client.post(
        "/api/chat",
        json={
            "to": "m-conformance-typo",
            "body": "must-not-land",
            "attachments": [{"data_b64": "bm8tb3JwaGFu", "mime": "text/plain"}],
        },
        headers=_auth(hctx.owner_token),
    )
    assert rejected.status_code == 404, rejected.text
    assert "chat recipient 'm-conformance-typo' not found" in rejected.text

    missing_mailbox = hctx.client.get(
        "/api/chat?with=m-conformance-typo&limit=-1", headers=_auth(hctx.owner_token)
    )
    assert missing_mailbox.status_code == 200, missing_mailbox.text
    assert all(
        m["body"] != "must-not-land" for m in chat_messages(missing_mailbox)
    )


def test_upload_ref_rejections(hctx: HCtx) -> None:
    """The ref form's 400 faces: unknown id, id together with data_b64, and
    an over-cap / empty upload body."""
    headers = _auth(hctx.agent.token)
    r = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [{"id": "att-conf-missing"}]},
        headers=headers,
    )
    assert r.status_code == 400 and "not found" in r.text, f"{r.status_code} {r.text}"

    att_id, _ = hctx.attachment()
    r = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [{"id": att_id, "data_b64": "aGk="}]},
        headers=headers,
    )
    assert r.status_code == 400 and "both id and data_b64" in r.text, (
        f"{r.status_code} {r.text}"
    )

    r = hctx.client.post("/api/chat/attachments", content=b"", headers=headers)
    assert r.status_code == 400 and "empty" in r.text, f"{r.status_code} {r.text}"

    r = hctx.client.post(
        "/api/chat/attachments?mime=image/png",
        content=b"x" * (20 * 1024 * 1024 + 1),
        headers=headers,
    )
    assert r.status_code == 400 and "20 MB" in r.text, f"{r.status_code} {r.text}"


def test_chat_answers_the_envelope_and_pages_by_opaque_cursor(hctx: HCtx) -> None:
    """T-48: EVERY path of ``GET /api/chat`` answers ``{messages, next_cursor}``,
    and ``next_cursor`` is the ONLY end-of-walk signal.

    The bare array this replaced had nowhere to say "there is more in this
    direction": a caller could only infer exhaustion from a short page, and a
    page is short for reasons that have nothing to do with exhaustion. The three
    paths that name their own set (``ids``, ``start_id``, ``end_id``) carry NO
    cursor, because there is no defined direction to continue in.
    """
    peer = hctx.agent.member_id
    sent = []
    for i in range(4):
        r = hctx.client.post(
            "/api/chat",
            json={"to": peer, "body": f"envelope seed {uuid.uuid4().hex[:6]} {i}"},
            headers=_auth(hctx.owner_token),
        )
        assert r.status_code == 200, r.text
        sent.append(r.json())

    def page(params: dict) -> tuple[list, str]:
        r = hctx.client.get("/api/chat", params=params, headers=_auth(hctx.owner_token))
        assert r.status_code == 200, r.text
        body = r.json()
        assert isinstance(body, dict), f"must be an object, got {r.text[:200]}"
        assert isinstance(body.get("messages"), list), r.text[:200]
        return body["messages"], body.get("next_cursor", "")

    # The three set-naming paths carry no cursor.
    for params in (
        {"ids": sent[0]["id"]},
        {"start_id": sent[0]["id"], "limit": 2},
        {"end_id": sent[-1]["id"], "limit": 2},
    ):
        msgs, cursor = page(params)
        assert msgs, f"{params} returned nothing"
        assert cursor == "", f"{params} must carry no next_cursor, got {cursor!r}"

    # The newest page DOES, and walking it back reaches every seeded message
    # exactly once. Not asserted as "the whole stream": this suite shares one
    # server, so the only rows this test owns are the ones it posted.
    msgs, cursor = page({"with": peer, "limit": 2})
    assert cursor, "four messages exist behind a limit of 2 — expected a cursor"
    seen = [m["id"] for m in msgs]
    rounds = 0
    while cursor:
        rounds += 1
        assert rounds < 200, "the cursor walk did not terminate"
        prev = cursor
        msgs, cursor = page({"with": peer, "limit": 2, "cursor": prev})
        assert cursor != prev, f"next_cursor did not advance: {cursor!r}"
        seen = [m["id"] for m in msgs] + seen
    assert len(seen) == len(set(seen)), f"the walk re-served a message: {seen}"
    for m in sent:
        assert m["id"] in seen, f"the walk skipped {m['id']}"

    # An opaque token is opaque: one this API never minted is a 422 that says so,
    # rather than a 200 answering from some guessed position.
    r = hctx.client.get(
        "/api/chat", params={"with": peer, "cursor": "not-a-cursor"},
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 422, f"{r.status_code} {r.text}"
    assert "next_cursor" in r.text, r.text


def test_chat_unread_is_per_sender_and_marks_nothing_read(hctx: HCtx) -> None:
    """T-48 ``?unread=true``: the caller's own unread, judged against the
    watermark FOR EACH SENDER, oldest first, and still writing nothing.

    🔴 The per-sender rule is the load-bearing half. Two senders write to the
    agent; the agent marks ONE of them read up to its newest message. Everything
    from the OTHER sender must survive — including messages OLDER than the
    watermark that was just written — because that watermark belongs to a
    different conversation. Folding the two into one number would drop them, and
    would drop them silently: the answer is a shorter page, which is exactly what
    an empty inbox looks like.
    """
    tag = uuid.uuid4().hex[:8]
    reader, reader_tok = hctx.agent.member_id, hctx.agent.token
    other = hctx.fresh_member()
    other_tok = mint_member_token(hctx.client, hctx.owner_token, other)

    def post(token: str, body: str) -> dict:
        r = hctx.client.post(
            "/api/chat", json={"to": reader, "body": body}, headers=_auth(token)
        )
        assert r.status_code == 200, r.text
        return r.json()

    # Interleaved on purpose: `other`'s message is OLDER than the watermark the
    # owner's line is about to get, so a single global watermark would hide it.
    from_other = post(other_tok, f"unread-other-{tag}")
    from_owner = post(hctx.owner_token, f"unread-owner-{tag}")
    assert from_other["ts"] <= from_owner["ts"]

    r = hctx.client.post(
        "/api/chat/mark-read",
        json={"peer": "owner", "last_read_ts": from_owner["ts"]},
        headers=_auth(reader_tok),
    )
    assert r.status_code == 200, r.text

    r = hctx.client.get(
        "/api/chat", params={"unread": "true", "limit": -1}, headers=_auth(reader_tok)
    )
    assert r.status_code == 200, r.text
    unread = {m["id"]: m for m in chat_messages(r)}
    assert from_other["id"] in unread, (
        "the OTHER sender has no watermark at all, so its message is unread even "
        f"though it is older than the one just marked read: {sorted(unread)}"
    )
    assert from_owner["id"] not in unread, (
        "the owner's line was marked read up to this message"
    )
    assert all(m["to"] == reader for m in unread.values()), (
        "unread must only carry messages addressed to the caller"
    )
    assert all(m["from"] != reader for m in unread.values()), (
        "your own messages are never your unread"
    )
    ts = [m["ts"] for m in unread.values()]
    assert ts == sorted(ts), "unread is answered OLDEST FIRST"

    # 🔴 READING IT CLEARS NOTHING. The full backlog is still there afterwards.
    r2 = hctx.client.get(
        "/api/chat", params={"unread": "true", "limit": -1}, headers=_auth(reader_tok)
    )
    assert r2.status_code == 200, r2.text
    assert {m["id"] for m in chat_messages(r2)} == set(unread), (
        "paging unread must not advance any watermark — the backlog is unchanged"
    )

    # `unread` names a set defined by watermarks; the stream-position cursors
    # name a place in the whole stream. Both at once is a 422, not a guess.
    for extra in ({"before_ts": 1, "before_id": "x"}, {"start_id": from_other["id"]}):
        r = hctx.client.get(
            "/api/chat", params={"unread": "true", **extra}, headers=_auth(reader_tok)
        )
        assert r.status_code == 422, f"{extra}: {r.status_code} {r.text}"


def test_chat_refuses_a_query_parameter_it_does_not_declare(hctx: HCtx) -> None:
    """T-48 (owner ruling, THIS ROUTE ONLY): an undeclared query parameter is a
    400 that NAMES it, rather than being silently ignored.

    ``caller_only`` is the case that made this worth having: it was removed in
    the same change, and a caller still sending it would otherwise have received
    a 200 carrying an UNNARROWED listing with nothing said. Now it is told.
    """
    for name, value in (("caller_only", "true"), ("peek", "true"), ("wiht", "owner")):
        r = hctx.client.get(
            "/api/chat",
            params={"with": hctx.agent.member_id, name: value},
            headers=_auth(hctx.owner_token),
        )
        assert r.status_code == 400, f"{name}: {r.status_code} {r.text}"
        assert name in r.text, f"the refusal must name {name}: {r.text}"

    # The scope of that rule is exactly one route: everything else still ignores
    # what it does not know. Without this arm the test would pass just as well
    # against a server that had grown the refusal everywhere.
    r = hctx.client.get(
        "/api/members", params={"wiht": "owner"}, headers=_auth(hctx.owner_token)
    )
    assert r.status_code == 200, (
        f"only GET /api/chat refuses unknown parameters: {r.status_code} {r.text}"
    )

    # …and the `?token=` transport credential is not a typo: it is how a client
    # that cannot set a header authenticates, and it must still be accepted here.
    r = hctx.client.get(
        "/api/chat",
        params={"with": hctx.agent.member_id, "token": hctx.owner_token},
    )
    assert r.status_code == 200, f"?token= must be accepted: {r.status_code} {r.text}"


def test_chat_sender_and_recipient_narrow_one_side_each(hctx: HCtx) -> None:
    """T-48: ``sender``/``recipient`` filter ONE side each and AND together,
    which pins one direction of one line — something ``with`` cannot express,
    because it matches either side."""
    tag = uuid.uuid4().hex[:8]
    peer = hctx.agent.member_id
    outbound = hctx.client.post(
        "/api/chat", json={"to": peer, "body": f"one-sided-out-{tag}"},
        headers=_auth(hctx.owner_token),
    )
    assert outbound.status_code == 200, outbound.text
    inbound = hctx.client.post(
        "/api/chat", json={"to": "owner", "body": f"one-sided-in-{tag}"},
        headers=_auth(hctx.agent.token),
    )
    assert inbound.status_code == 200, inbound.text

    r = hctx.client.get(
        "/api/chat",
        params={"sender": "owner", "recipient": peer, "limit": -1},
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    ids = {m["id"] for m in chat_messages(r)}
    assert outbound.json()["id"] in ids
    assert inbound.json()["id"] not in ids, (
        "sender+recipient pins ONE direction; the reply travels the other way"
    )
    assert all(
        m["from"] == "owner" and m["to"] == peer for m in chat_messages(r)
    ), "every row must match both filters"


def test_chat_scrollback_cursor_page_never_marks_read(hctx: HCtx) -> None:
    """T-bf82 scrollback: ``GET /api/chat?with=&before_ts=&before_id=`` serves
    the strictly-older history page (oldest→newest) and NEVER advances the
    caller's read watermark; a partial cursor is a 422.

    T-48 flipped the last clause of this pin. The cursorless list used to
    auto-mark, and this test asserted it did. It does not any more, on ANY path:
    owner ruled that reading a list must not be able to claim a conversation was
    read (「get_chat 不應該可以標示已讀未讀，這應該要另一隻 API 明確表示有這個
    意圖」), because a member that had only attached its listener — never woken,
    never shown a line — grew a watermark for messages nobody had looked at.
    Marking read is POST /api/chat/mark-read and nothing else."""
    peer = hctx.agent.member_id
    sent = []
    for i in range(3):
        r = hctx.client.post(
            "/api/chat",
            json={"to": peer, "body": f"scrollback seed {i}"},
            headers=_auth(hctx.owner_token),
        )
        assert r.status_code == 200, r.text
        sent.append(r.json())
    newest = sent[-1]

    def agent_watermark() -> float:
        r = hctx.client.get(
            "/api/chat/reads",
            params={"with": "owner"},
            headers=_auth(hctx.agent.token),
        )
        assert r.status_code == 200, r.text
        rows = [x for x in r.json() if x["reader_id"] == peer]
        return rows[0]["last_read_ts"] if rows else 0.0

    marked_before = agent_watermark()
    assert marked_before < newest["ts"], (
        "fixture broken: the agent already read past the fresh seed"
    )

    # The AGENT pages history back from the newest message: the page is
    # strictly older (newest excluded), ascending, ending on the second seed.
    r = hctx.client.get(
        "/api/chat",
        params={
            "with": "owner",
            "before_ts": newest["ts"],
            "before_id": newest["id"],
        },
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, r.text
    page = chat_messages(r)
    ids = [m["id"] for m in page]
    assert newest["id"] not in ids, "a history page must exclude the cursor row"
    assert ids[-2:] == [sent[0]["id"], sent[1]["id"]], (
        f"history page must end on the strictly-older seeds: {ids[-4:]}"
    )
    ts_list = [m["ts"] for m in page]
    assert ts_list == sorted(ts_list), "history page must stay oldest→newest"

    # THE watermark pin: reading old context is not reading the conversation.
    assert agent_watermark() == marked_before, (
        "a before_ts/before_id history page must never advance the watermark"
    )

    # A partial cursor is a 422 — the params travel together.
    for partial in ({"before_ts": newest["ts"]}, {"before_id": newest["id"]}):
        r = hctx.client.get(
            "/api/chat",
            params={"with": "owner", **partial},
            headers=_auth(hctx.agent.token),
        )
        assert r.status_code == 422, f"partial cursor: {r.status_code} {r.text}"

    # The cursorless list does not mark either (T-48). This is the arm that
    # changed, so it is asserted against the watermark captured BEFORE any read
    # in this test — "unchanged", not merely "below the newest ts", which would
    # also pass if the write happened and landed low.
    r = hctx.client.get(
        "/api/chat",
        params={"with": "owner"},
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, r.text
    assert agent_watermark() == marked_before, (
        "a cursorless list must not advance the watermark either — marking a "
        "conversation read is POST /api/chat/mark-read and nothing else"
    )


def test_happy_covers_manifest(routes_manifest: list[dict[str, str]]) -> None:
    rows = {f"{r['method']} {r['path']}" for r in routes_manifest}
    covered = set(HAPPY) | set(SKIPPED_HAPPY)
    overlap = set(HAPPY) & set(SKIPPED_HAPPY)
    missing = rows - covered
    stale = covered - rows
    assert not overlap, f"rows both happy-tested and skipped: {sorted(overlap)}"
    assert not missing, (
        f"manifest rows with NO happy row and NO skip reason: {sorted(missing)}"
    )
    assert not stale, f"happy/skip rows no longer in ROUTE_SPECS: {sorted(stale)}"
    for key, reason in SKIPPED_HAPPY.items():
        assert reason.strip(), f"SKIPPED_HAPPY[{key}] carries no reason"


def test_openapi_covers_manifest(routes_manifest: list[dict[str, str]]) -> None:
    """The frozen spec/openapi.json and routes_manifest.json must describe the
    SAME operation set — a spec freeze update without a manifest update (or
    vice versa) reddens the run here.

    Both sides of THIS comparison are hand-written, so it cannot tell you the
    server serves either set: two lists agreeing prove they were typed the same
    day. The leg that reaches the server is TestRouteTableCoversSpecSurface
    (server/ocserverd/server_test.go), which pins the same frozen spec against
    the route table the mux is built from — chained with this test, the
    manifest and the served routes are held equal."""
    manifest_ops = {f"{r['method']} {r['path']}" for r in routes_manifest}
    spec_ops = {
        f"{m.upper()} {p}"
        for p, ops in SPEC["paths"].items()
        for m in ops
        if m in ("get", "post", "put", "patch", "delete")
    }
    assert spec_ops == manifest_ops, (
        f"spec-only={sorted(spec_ops - manifest_ops)} "
        f"manifest-only={sorted(manifest_ops - spec_ops)}"
    )


# ---------------------------------------------------------------------------
# command_result → member.last_op_reason fold (成員啟動失敗原因全鏈可見).
# The warden's telemetry receipt carries a STRUCTURED refusal cause
# ("<code>: <detail>", e.g. "session_already_exists: ..."); the server must
# persist it on the member SEPARATELY from the free-form log and expose it as
# MemberDTO.last_op_reason. A reason-less receipt (older warden) folds "".
# ---------------------------------------------------------------------------


def _member_row(hctx: HCtx, member_id: str) -> dict:
    r = hctx.client.get("/api/members", headers=_auth(hctx.owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    rows = [m for m in r.json() if m["id"] == member_id]
    assert rows, f"member {member_id} missing from roster"
    return rows[0]


def test_command_result_reason_folds_onto_member(hctx: HCtx) -> None:
    target = hctx.fresh_member()
    reason = (
        'session_already_exists: tmux session "member-x" is already live '
        "(clobber-guard refused to stomp it)"
    )
    r = hctx.client.post(
        "/api/monitoring/telemetry",
        json={
            "command_result": {
                "member_id": target,
                "rpc": "start",
                "ok": False,
                "reason": reason,
                "log": reason,
                "at": "2026-07-13T08:00:00Z",
            }
        },
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    row = _member_row(hctx, target)
    assert row["last_op"] == "start" and row["last_op_ok"] is False, row
    assert row["last_op_reason"] == reason, row


def test_command_result_without_reason_folds_empty(hctx: HCtx) -> None:
    # Old-warden compat: no reason key ⇒ last_op_reason is "" (status-only for
    # the FE), and it OVERWRITES a stale prior reason (the reason always
    # describes THIS op) while the log still folds.
    target = hctx.fresh_member()
    seed = hctx.client.post(
        "/api/monitoring/telemetry",
        json={
            "command_result": {
                "member_id": target,
                "rpc": "start",
                "ok": False,
                "reason": "spawn_exec_failed: stale prior cause",
            }
        },
        headers=_auth(hctx.agent.token),
    )
    assert seed.status_code == 200, f"{seed.status_code} {seed.text}"
    r = hctx.client.post(
        "/api/monitoring/telemetry",
        json={
            "command_result": {
                "member_id": target,
                "rpc": "stop",
                "ok": True,
                "log": "session=member-x: stopped",
            }
        },
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    row = _member_row(hctx, target)
    assert row["last_op"] == "stop" and row["last_op_reason"] == "", row
    assert row["last_op_log"] == "session=member-x: stopped", row
