"""Lifecycle face — JWT claims/TTL, the boot-context fold, in-memory honesty
(spec/lifecycle.md).

Third conformance batch. What this file pins, MUST by MUST:

  * §1.1 claim envelope: header exactly {"alg":"HS256","typ":"JWT"}; sub /
        scope / iat / exp integer seconds; machine_id OPTIONAL and OMITTED
        (not null/empty) when there is no placement;
  * §1.1 verification: a tampered signature and an alg-downgrade token are
        both refused 401 (black-box crafts the tokens — no secret needed to
        FORGE, only to verify);
  * §1.3 mint surfaces and TTLs: login (owner scope, token_ttl default
        86400 s), /api/mint (agent scope, min(ttl_days·86400, 400 d) cap),
        machine onboard (90 d default, no machine claim), the one-time
        machine claim-code redemption (same mint), bootstrap-with-
        member (claim = desired_machine_id); wrong password → flat 401;
  * §2  boot-context assembly reproduced BYTE-FOR-BYTE from the seed files
        (language-neutral assets under seeds/ — data, not code)
        plus API-visible overlay state: part order, "\\n\\n" joins, the single
        trailing "\\n", the exact block headers, and the user-custom block
        skipped entirely when blank;
  * §2  bootstrap == the same fold regardless of overlay state (overlay-wins
        exercised via a lessons overlay written through the API);
  * in-memory #2 observed position: agents_on_machine drives the machine
        teardown guard (409 while a claimed agent holds a live SSE; clear
        after disconnect).

Restart honest-empty (§3) and the reconcile timer surface (§4.4) are listed
in DEGRADED below with reasons — the black-box harness cannot restart an
injected OC_TARGET_URL nor afford minutes-scale timer observation.
"""

from __future__ import annotations

import base64
import json
import pathlib
import time
import uuid

from conftest import hire_member
from sse_client import SSEConnection

HERE = pathlib.Path(__file__).resolve().parent
# Seed .md files are LANGUAGE-NEUTRAL ASSETS (spec/lifecycle.md §2.2: "a
# rewrite MUST consume the same files") — reading them as data keeps the suite
# black-box (no server *code* is imported).
SEEDS = HERE.parent / "seeds"

MAX_AGENT_TTL_SECS = 400 * 86400
DEFAULT_TOKEN_TTL = 86400
DEFAULT_MACHINE_TTL_SECS = 90 * 86400
MACHINE_CLAIM_TTL_SECS = 600


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


# ── JWT plumbing (stdlib only — black-box) ────────────────────────────────────


def _b64url_decode(segment: str) -> bytes:
    return base64.urlsafe_b64decode(segment + "=" * (-len(segment) % 4))


def _b64url_encode(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def _decode_jwt(token: str) -> tuple[dict, dict]:
    parts = token.split(".")
    assert len(parts) == 3, f"compact JWS must be 3 segments, got {len(parts)}"
    header = json.loads(_b64url_decode(parts[0]))
    payload = json.loads(_b64url_decode(parts[1]))
    return header, payload


def _assert_claims(
    token: str,
    *,
    sub: str,
    scope: str,
    ttl: int,
    machine_id: str | None = None,
) -> dict:
    header, payload = _decode_jwt(token)
    assert header == {"alg": "HS256", "typ": "JWT"}, header
    assert payload["sub"] == sub, payload
    assert payload["scope"] == scope, payload
    assert isinstance(payload["iat"], int) and isinstance(payload["exp"], int), payload
    assert payload["exp"] - payload["iat"] == ttl, (
        f"ttl: expected {ttl}, got {payload['exp'] - payload['iat']}"
    )
    if machine_id is None:
        assert "machine_id" not in payload, (
            f"machine_id MUST be omitted entirely when empty: {payload}"
        )
    else:
        assert payload["machine_id"] == machine_id, payload
    return payload


# ── §1.3 mint surfaces and TTL semantics ─────────────────────────────────────


def test_login_token_claims_and_default_ttl(owner_token) -> None:
    _assert_claims(owner_token, sub="owner", scope="owner", ttl=DEFAULT_TOKEN_TTL)


def test_mint_ttl_days_and_400_day_cap(client, owner_token, agent_a) -> None:
    for ttl_days, expected in ((1, 86400), (1000, MAX_AGENT_TTL_SECS)):
        r = client.post(
            "/api/mint",
            json={"member_id": agent_a.member_id, "ttl_days": ttl_days},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, r.text
        _assert_claims(
            r.json()["token"], sub=agent_a.member_id, scope="agent", ttl=expected
        )


def test_machine_onboard_token_90d_no_placement_claim(client, owner_token) -> None:
    r = client.post(
        "/api/machines",
        json={"display_name": f"conf-lc-machine-{uuid.uuid4().hex[:6]}"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    data = r.json()
    assert data["expires_in"] == DEFAULT_MACHINE_TTL_SECS, data["expires_in"]
    # Warden tokens carry NO machine_id claim (§1.3) — the warden IS the machine.
    _assert_claims(
        data["token"],
        sub=data["machine_id"],
        scope="agent",
        ttl=DEFAULT_MACHINE_TTL_SECS,
    )
    # §1.3 machine claim codes: the boot command carries the ONE-TIME code,
    # never the token, and redeeming it mints the SAME shape onboard minted
    # (agent scope, warden sub, 90 d, no placement claim).
    assert data["claim_expires_in"] == MACHINE_CLAIM_TTL_SECS, data["claim_expires_in"]
    assert f"/install.sh?code={data['claim_code']}" in data["boot_command"], (
        data["boot_command"]
    )
    assert data["token"] not in data["boot_command"], (
        "the boot command must never embed the exec-token"
    )
    claimed = client.post("/api/machines/claim", json={"code": data["claim_code"]})
    assert claimed.status_code == 200, claimed.text
    body = claimed.json()
    assert body["machine_id"] == data["machine_id"], body
    assert body["expires_in"] == DEFAULT_MACHINE_TTL_SECS, body["expires_in"]
    _assert_claims(
        body["token"],
        sub=data["machine_id"],
        scope="agent",
        ttl=DEFAULT_MACHINE_TTL_SECS,
    )


def test_bootstrap_token_carries_desired_machine_claim(
    client, owner_token, fresh_machine
) -> None:
    """§1.3: bootstrap-with-member mints machine_id = member.desired_machine_id."""
    machine_id = fresh_machine()
    member_id = hire_member(client, owner_token, f"conf-lc-claim-{uuid.uuid4().hex[:6]}")
    r = client.post(
        f"/api/members/{member_id}/activate",
        json={"machine_id": machine_id},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    r = client.post(
        "/api/bootstrap", json={"member_id": member_id}, headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    _assert_claims(
        r.json()["token"],
        sub=member_id,
        scope="agent",
        ttl=DEFAULT_TOKEN_TTL,
        machine_id=machine_id,
    )
    client.post(f"/api/members/{member_id}/deactivate", headers=_auth(owner_token))


def test_wrong_password_is_flat_401(client) -> None:
    r = client.post("/api/login", json={"password": "conf-definitely-wrong"})
    assert r.status_code == 401, r.text
    assert r.json()["error"]["code"] == "unauthorized"


# ── §1.1 verification hardening (forged tokens — no secret required) ─────────


def test_tampered_signature_is_401(client, owner_token) -> None:
    head, payload, sig = owner_token.split(".")
    flipped = sig[:-2] + ("AA" if sig[-2:] != "AA" else "BB")
    r = client.get("/api/members", headers=_auth(f"{head}.{payload}.{flipped}"))
    assert r.status_code == 401, f"tampered signature accepted: {r.status_code}"


def test_alg_none_downgrade_is_401(client, owner_token) -> None:
    _, payload, _ = owner_token.split(".")
    none_header = _b64url_encode(json.dumps({"alg": "none", "typ": "JWT"}).encode())
    for forged in (f"{none_header}.{payload}.", f"{none_header}.{payload}.AAAA"):
        r = client.get("/api/members", headers=_auth(forged))
        assert r.status_code == 401, f"alg:none token accepted: {r.status_code}"


def test_expired_exp_is_401(client, owner_token) -> None:
    """A well-formed-but-expired payload under ANY signature must be 401 —
    either as an invalid signature (we cannot re-sign) or as expiry; the gate
    answer is 401 regardless (spec §1.1: failures MAP to 401)."""
    head, _, sig = owner_token.split(".")
    expired = _b64url_encode(
        json.dumps(
            {"sub": "owner", "scope": "owner", "iat": 1, "exp": 2}
        ).encode()
    )
    r = client.get("/api/members", headers=_auth(f"{head}.{expired}.{sig}"))
    assert r.status_code == 401


# ── §2 boot-context fold — byte-for-byte reproduction ────────────────────────


def _seed(name: str) -> str:
    # OBSERVED WIRE (spec gap — reported): the seed loader substitutes the
    # `{OWNER_ID}` placeholder with the fixed single-tenant owner id "owner"
    # before the fold; spec/lifecycle.md §2.2 says "byte-for-byte block content
    # equality" over the seed files without mentioning the substitution.
    return (SEEDS / name).read_text(encoding="utf-8").replace("{OWNER_ID}", "owner")


def _expected_context(client, owner_token, role_key: str, task_type: str, user_text: str) -> str:
    role = client.get(f"/api/roles/{role_key}", headers=_auth(owner_token)).json()
    lessons = client.get(
        f"/api/lessons/{role_key}/{task_type}", headers=_auth(owner_token)
    ).json()
    parts = [
        _seed("system_interaction.md").strip(),
        f"# Role: {role['name'] or role['key']}\n\n{role['definition_md'].strip()}",
        f"# Lessons ({role_key} / {task_type})\n\n{lessons['text'].strip()}",
    ]
    if user_text.strip():
        parts.append(f"# 使用者自訂（Owner Additions）\n\n{user_text.strip()}")
    parts.append(_seed("boot_sequence.md").strip())
    return "\n\n".join(parts) + "\n"


def _bootstrap_context(client, owner_token) -> tuple[str, str, str]:
    r = client.post("/api/bootstrap", json={}, headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    data = r.json()
    return data["context"], data["role"], data["task_type"]


def test_boot_fold_bytes_with_owner_additions(client, owner_token) -> None:
    """§2.2 normative assembly, WITH the user-custom block: set a known
    global-context overlay, then reproduce the served context byte-for-byte
    from the seed assets + API-visible overlay state."""
    marker = f"conf boot fold marker {uuid.uuid4().hex[:8]}"
    r = client.post(
        "/api/global-context", json={"text": marker}, headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    context, role_key, task_type = _bootstrap_context(client, owner_token)
    expected = _expected_context(client, owner_token, role_key, task_type, marker)
    assert context == expected, (
        "boot context does not reproduce the §2.2 assembly byte-for-byte "
        f"(len served={len(context)} vs expected={len(expected)})"
    )
    # The recency-authoritative tail: the boot-sequence seed is LAST.
    assert context.rstrip("\n").endswith(_seed("boot_sequence.md").strip())
    assert context.endswith("\n") and not context.endswith("\n\n")


def test_boot_fold_bytes_blank_user_block_skipped(client, owner_token) -> None:
    """§2.2 part 4: a blank owner text drops the ENTIRE user-custom block —
    no noise header — and the fold is byte-identical to the 4-part form."""
    r = client.post("/api/global-context/reset", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    context, role_key, task_type = _bootstrap_context(client, owner_token)
    assert "# 使用者自訂（Owner Additions）" not in context, (
        "blank owner text must skip the user-custom header entirely"
    )
    expected = _expected_context(client, owner_token, role_key, task_type, "")
    assert context == expected


def test_boot_fold_lessons_overlay_wins(client, owner_token) -> None:
    """§2.1: the lessons fold is overlay-wins — an API-written lessons doc for
    (role, default task_type) must appear verbatim inside the boot context."""
    _, role_key, task_type = _bootstrap_context(client, owner_token)
    marker = f"conf lessons overlay {uuid.uuid4().hex[:8]}"
    r = client.post(
        f"/api/lessons/{role_key}/{task_type}",
        json={"text": marker},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    context, role_key2, task_type2 = _bootstrap_context(client, owner_token)
    assert (role_key2, task_type2) == (role_key, task_type)
    assert f"# Lessons ({role_key} / {task_type})\n\n{marker}" in context
    expected = _expected_context(client, owner_token, role_key, task_type, "")
    assert context == expected


def test_bootstrap_unknown_role_404(client, owner_token) -> None:
    """§2.1: neither overlay nor seed for the role → the bootstrap endpoint
    fails closed with 404 (the reconcile producer analogue is no-START)."""
    r = client.post(
        "/api/bootstrap",
        json={"role": f"conf-no-such-role-{uuid.uuid4().hex[:6]}"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 404, r.text
    assert r.json()["error"]["code"] == "not_found"


# ── in-memory #2: observed position drives the machine teardown guard ────────


def test_agents_on_machine_blocks_machine_uninstall(
    base_url, client, owner_token, fresh_machine
) -> None:
    """spec/sse.md §5: agents_on_machine projects LIVE agent listeners by their
    token machine_id claim — the teardown guard's input. While a claimed agent
    holds /api/events, uninstalling its machine is a 409; after the disconnect
    the projection empties and the uninstall proceeds."""
    machine_id = fresh_machine()
    member_id = hire_member(client, owner_token, f"conf-lc-guard-{uuid.uuid4().hex[:6]}")
    r = client.post(
        f"/api/members/{member_id}/activate",
        json={"machine_id": machine_id},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    token = client.post(
        "/api/bootstrap", json={"member_id": member_id}, headers=_auth(owner_token)
    ).json()["token"]
    conn = SSEConnection(base_url, token)
    try:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            f"/api/machines/{machine_id}/uninstall", headers=_auth(owner_token)
        )
        assert r.status_code == 409, (
            f"live claimed agent must block machine uninstall: {r.status_code} {r.text[:200]}"
        )
        assert r.json()["error"]["code"] == "conflict"
    finally:
        conn.close()
    # Projection must clear with the disconnect (poll — teardown is async).
    deadline = time.monotonic() + 5.0
    while time.monotonic() < deadline:
        r = client.post(
            f"/api/machines/{machine_id}/uninstall", headers=_auth(owner_token)
        )
        if r.status_code == 200:
            break
        time.sleep(0.2)
    assert r.status_code == 200, f"guard did not clear after disconnect: {r.text[:200]}"
    assert r.json()["dispatched"] is False  # warden offline → nothing to command
    client.post(f"/api/members/{member_id}/deactivate", headers=_auth(owner_token))


def test_agents_on_machine_blocks_machine_delete(
    base_url, client, owner_token, fresh_machine
) -> None:
    """Machine delete shares the uninstall gate's ACTUAL-ONLINE criterion: an
    agent whose live SSE claims this machine blocks the delete (409); once it
    disconnects — even though its desired_machine_id still BINDS it here — the
    machine deletes directly."""
    machine_id = fresh_machine()
    member_id = hire_member(client, owner_token, f"conf-lc-delguard-{uuid.uuid4().hex[:6]}")
    r = client.post(
        f"/api/members/{member_id}/activate",
        json={"machine_id": machine_id},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    token = client.post(
        "/api/bootstrap", json={"member_id": member_id}, headers=_auth(owner_token)
    ).json()["token"]
    conn = SSEConnection(base_url, token)
    try:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.delete(f"/api/machines/{machine_id}", headers=_auth(owner_token))
        assert r.status_code == 409, (
            f"live claimed agent must block machine delete: {r.status_code} {r.text[:200]}"
        )
        assert r.json()["error"]["code"] == "conflict"
    finally:
        conn.close()
    deadline = time.monotonic() + 5.0
    while time.monotonic() < deadline:
        r = client.delete(f"/api/machines/{machine_id}", headers=_auth(owner_token))
        if r.status_code == 200:
            break
        time.sleep(0.2)
    assert r.status_code == 200, f"guard did not clear after disconnect: {r.text[:200]}"
    assert r.json()["removed"] is True
    client.post(f"/api/members/{member_id}/deactivate", headers=_auth(owner_token))


# ── uninstall one-shot intent (lifecycle §4.3 owner-decided semantics) ────────


def _onboard_machine(client, owner_token) -> tuple[str, str]:
    """Onboard a scratch machine, returning (machine_id, warden_token)."""
    r = client.post(
        "/api/machines",
        json={"display_name": f"conf-lc-oneshot-{uuid.uuid4().hex[:8]}"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    data = r.json()
    return data["machine_id"], data["token"]


def _desired_state_of(client, owner_token, member_id: str) -> str:
    r = client.get(f"/api/members/{member_id}", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    return r.json()["desired_state"]


def test_uninstall_intent_consumed_when_warden_disconnects(
    base_url, client, owner_token
) -> None:
    """The uninstall intent is ONE-SHOT: armed on an online warden it stays
    live, and the moment the server observes the warden actually offline the
    intent is consumed (desired_state folds back to "offline", record kept) —
    a later re-install can never be answered with a stale UNINSTALL (the
    uninstall→re-install loop incident)."""
    machine_id, warden_token = _onboard_machine(client, owner_token)
    conn = SSEConnection(base_url, warden_token)
    try:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            f"/api/machines/{machine_id}/uninstall", headers=_auth(owner_token)
        )
        assert r.status_code == 200, r.text
        assert r.json()["dispatched"] is True
        assert _desired_state_of(client, owner_token, machine_id) == "uninstall"
    finally:
        conn.close()
    deadline = time.monotonic() + 5.0
    state = None
    while time.monotonic() < deadline:
        state = _desired_state_of(client, owner_token, machine_id)
        if state == "offline":
            break
        time.sleep(0.2)
    assert state == "offline", (
        f"disconnect must consume the one-shot uninstall intent, still {state!r}"
    )


def test_boot_command_refetch_clears_residual_uninstall_intent(
    base_url, client, owner_token
) -> None:
    """Every (re-)install path zeroes a residual uninstall intent BEFORE
    installing (先歸零再裝): re-fetching the installer one-liner for a machine
    still carrying desired_state="uninstall" folds the intent back to
    "offline" so the fresh warden never boots into a standing kill order."""
    machine_id, warden_token = _onboard_machine(client, owner_token)
    conn = SSEConnection(base_url, warden_token)
    try:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            f"/api/machines/{machine_id}/uninstall", headers=_auth(owner_token)
        )
        assert r.status_code == 200, r.text
        assert _desired_state_of(client, owner_token, machine_id) == "uninstall"
        r = client.get(
            f"/api/machines/{machine_id}/boot-command", headers=_auth(owner_token)
        )
        assert r.status_code == 200, r.text
        assert _desired_state_of(client, owner_token, machine_id) == "offline", (
            "the install path must zero the residual uninstall intent"
        )
    finally:
        conn.close()


# ── DEGRADED / SKIP honesty table ─────────────────────────────────────────────
# Contract points this black-box batch CANNOT verify safely/affordably, each
# with the reason. Mirrors the matrix's DEGRADED discipline: no silent gaps.

DEGRADED: dict[str, str] = {
    "lifecycle §3 restart honest-empty (inventory #3/#5/#6/#7)": (
        "verifying 'in-memory stores are empty after restart' requires "
        "RESTARTING the target; the suite speaks only to an injected "
        "OC_TARGET_URL and must not manage its process. Needs a run.sh "
        "restart mode (owner call) before it can be pinned black-box."
    ),
    "sse §2.1 seq restart rollback (inventory #8)": (
        "same restart limitation: observing the counter reset to 0 requires "
        "bouncing the server process."
    ),
    "sse §1 15s heartbeat period": (
        "a positive observation costs >=15s of wall clock per run; the suite "
        "keeps second-scale budgets. Headers/preamble/delta liveness are "
        "pinned instead."
    ),
    "sse §4 multi-owner fan-out scoping": (
        "the target is single-tenant (one fixed owner id); no API mints a "
        "second owner, so 'a listener never receives another owner's frame' "
        "has no black-box counterexample to fire."
    ),
    "lifecycle §4.4 reconcile timers/backoff/circuit": (
        "start_timeout 90s / stop_grace 120s / backoff-circuit windows are "
        "minutes-scale; timers are injectable only white-box. The decision "
        "surface is covered at its observable edges instead (event-driven "
        "START dispatch + target-reachability gate in test_sse.py; "
        "uninstall receipt fold is producer-tick-timing dependent)."
    ),
    "lifecycle §1.2 signing-secret tier resolution": (
        "tier choice (oc.toml secret vs password-derived vs var/jwt_secret) "
        "is not observable through HTTP without knowing the secret; the "
        "cross-implementation token-verify equivalence it exists for is "
        "exercised implicitly (every fixture token round-trips)."
    ),
    "sse §5.2 boot_ts stamp (direct value)": (
        "the context gauge is not exposed over REST; the stamp is verified "
        "INDIRECTLY by the stale-pct guard test (test_sse.py) whose semantics "
        "depend on boot_ts being stamped at connect."
    ),
    "sse §7 queue cap / at-most-once loss semantics": (
        "provoking a lost frame requires killing a connection mid-drain "
        "deterministically; only the FIFO happy drain and the never-broadcast "
        "confidentiality face are pinned."
    ),
}


def test_degraded_rows_carry_reasons() -> None:
    for key, reason in DEGRADED.items():
        assert reason.strip(), f"DEGRADED[{key}] carries no reason"
