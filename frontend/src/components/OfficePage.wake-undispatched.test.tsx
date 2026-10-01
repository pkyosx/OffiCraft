// OfficePage · the wake verdict survives the CALL SITE (T-7fa1).
//
// 🔴 WHY THIS FILE IS THE MOST IMPORTANT ONE IN THE PACK. Every other test in
// this change can be green while the feature is completely dead, because the
// weakest link is invisible to all of them AND to the type checker:
//
//     onActivate={async (machineId) => {
//       await api.activateMember(detail.id, machineId);   // ← result dropped
//       await refetch();
//     }}
//
// `onActivate` accepts a void-returning handler (it must — not every caller has
// a verdict to give), so deleting the `return` compiles cleanly, keeps every
// panel test green (they inject their own handler), keeps every adapter test
// green (it still reads the wire field), and silently restores the original
// bug end to end. That is EXACTLY the shape of the bug this ticket exists for:
// a real signal, produced correctly, dropped in the middle.
//
// So these drive the WHOLE chain — OfficePage's own wiring → the real mock
// adapter → back into the panel — and assert what the owner would see.
//
// Both OfficePage wake surfaces are covered: the detail panel's 喚醒 and the
// chat room's in-place ⚡喚醒 (they are wired by two SEPARATE handlers, so one
// test cannot stand in for the other).

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __setMockActivationPending,
  __setMockMemberOnline,
  __setMockRelocationDeferred,
  __setMockRelocationPending,
} from "../api/mock";

/** The seeded warden member IS the machine registry row (mock listMachines
 * derives machines from warden members), so this is how the mock gets ONE
 * online machine — without it the wake button is legitimately disabled
 * (「沒有線上的機器」) and the click under test could never fire. */
const SEED_WARDEN = "warden-mbp5";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
}

async function confirmSettings() {
  const confirm = document.querySelector<HTMLButtonElement>(".machine-picker__actions .btn--accent")!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

/** The OTHER online machine in the mock registry (the seeded server-self warden)
 * — the member is pinned to SEED_WARDEN, so picking this one is what makes the
 * unified submit actually dispatch a relocate. */
const SELF_MACHINE = "m-server-self";
const SELF_MACHINE_NAME = "伺服器這一台";

/** Drive a live member's 更改 → pick the other machine → confirm, and wait for
 * the relocate verdict to settle. `pending` stages whether the mock reports the
 * relocate as undispatched. */
async function relocateThroughChange(
  pending: boolean,
  opts: { deferred?: boolean } = {},
) {
  __setMockMemberOnline(SEED_WARDEN, true);
  __setMockMemberOnline(SELF_MACHINE, true);
  __setMockMemberOnline("mira", true);
  __setMockRelocationPending(pending);
  __setMockRelocationDeferred(opts.deferred === true);
  window.location.hash = "#office/member/mira";
  const utils = renderOffice();
  await utils.findByText("Mira");

  const change = (await utils.findByTestId("mp-change")) as HTMLButtonElement;
  await waitFor(() => expect(change.disabled).toBe(false));
  fireEvent.click(change);

  const select = document.querySelector<HTMLSelectElement>(
    "select.machine-picker__select",
  )!;
  await waitFor(() =>
    expect(
      Array.from(select.options).some((o) => o.value === SELF_MACHINE),
    ).toBe(true),
  );
  fireEvent.change(select, { target: { value: SELF_MACHINE } });
  await confirmSettings();

  // The notice is expected for the FAILURE half only; a deferred move settles
  // into the transition indicator instead, so the caller asserts that itself.
  if (pending && opts.deferred !== true) {
    await waitFor(() =>
      expect(utils.queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );
  } else {
    await waitFor(() =>
      expect(document.querySelector(".machine-picker")).toBeNull(),
    );
  }
  return utils;
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage · an undispatched wake reaches the UI (T-7fa1)", () => {
  it("member detail: pressing 喚醒 surfaces the notice instead of a permanent 喚醒中…", async () => {
    __setMockMemberOnline(SEED_WARDEN, true);
    __setMockActivationPending(true);
    window.location.hash = "#office/member/mira";
    const { container, findByText, queryByTestId } = renderOffice();
    await findByText("Mira");

    const wake = await waitFor(() => {
      const b = container.querySelector(
        '[data-testid="member-action-spawn"]',
      ) as HTMLButtonElement | null;
      expect(b, "the wake button must exist").not.toBeNull();
      expect(b!.disabled, "the wake button must be enabled").toBe(false);
      return b!;
    });

    fireEvent.click(wake);
    await confirmSettings();

    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );
  });

  it("member detail: a wake that WAS dispatched shows no notice (negative control)", async () => {
    // Same path, same click, only the server verdict differs. Without this the
    // positive test alone would pass a mutant that shows the notice
    // unconditionally — which would be its own, louder lie.
    __setMockMemberOnline(SEED_WARDEN, true);
    __setMockActivationPending(false);
    window.location.hash = "#office/member/mira";
    const { container, findByText, queryByTestId } = renderOffice();
    await findByText("Mira");

    const wake = await waitFor(() => {
      const b = container.querySelector(
        '[data-testid="member-action-spawn"]',
      ) as HTMLButtonElement | null;
      expect(b, "the wake button must exist").not.toBeNull();
      expect(b!.disabled).toBe(false);
      return b!;
    });

    fireEvent.click(wake);
    await confirmSettings();

    // Give the same async settling the positive case needed, then assert the
    // notice never appeared. The mock DOES move presence here, so the row is
    // the waking one: 更改 ＋ 停止.
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="member-action-stop"]'),
      ).not.toBeNull(),
    );
    expect(queryByTestId("mp-wake-undispatched")).toBeNull();
  });

  it("member detail: an online member exposes Change, not the retired relocate control", async () => {
    __setMockMemberOnline(SEED_WARDEN, true);
    __setMockMemberOnline("mira", true);
    window.location.hash = "#office/member/mira";
    const { findByText, findByTestId, queryByTestId } = renderOffice();
    await findByText("Mira");
    expect(await findByTestId("mp-change")).toBeTruthy();
    expect(queryByTestId("mp-relocate")).toBeNull();
  });

  it("member detail: 更改 surfaces the relocate notice too (mutant MB)", async () => {
    // 🔴 THIS ONE WAS FOUND BY THE REVIEWER, NOT BY ME. Four call sites of the
    // identical shape drop-a-return; round 1 guarded three and left this one
    // bare — the reviewer's mutant MB deleted its `return result` and all 900
    // tests stayed green. Exactly the class of silent drop the commit message
    // names as the original bug, reproduced inside the fix for it.
    //
    // The entry moved (改機器 → the unified 更改 dialog), so this drives the
    // NEW flow — but the chain under test is the same one, and it has to stay
    // covered at the CALL SITE: OfficePage wires `onRelocate` itself, and
    // asserting only that the retired button is gone would have retired the
    // guarantee with it.
    await relocateThroughChange(true);
  });

  it("member detail: a relocate that LANDED shows no notice (negative control)", async () => {
    const { queryByTestId, findByTestId } = await relocateThroughChange(false);
    expect(queryByTestId("mp-relocate-undispatched")).toBeNull();
    // Positive control: the relocate really went out and re-pinned the member —
    // without this, a submit that silently did nothing would satisfy the
    // assertion above just as well.
    expect((await findByTestId("mp-machine-pending")).textContent).toContain(
      SELF_MACHINE_NAME,
    );
  });

  it("member detail: a DEFERRED move is not alarmed about — the wind-down is normal", async () => {
    // 🔴 T-927a, the other direction of the same wire pair. `relocation_pending`
    // is also true when the server deliberately held the move back behind a
    // graceful wind-down: nothing was dispatched, but nothing went wrong. Raising
    // the "nothing was sent" alert there fires it on ROUTINE operation, and an
    // alert that cries on normal days is one the owner learns to ignore — which
    // costs more than having no alert at all.
    const { queryByTestId, findByTestId } = await relocateThroughChange(true, {
      deferred: true,
    });
    expect(queryByTestId("mp-relocate-undispatched")).toBeNull();
    // Positive control: the move really was requested and the owner can still
    // see where it is going — the pending destination, not an alarm.
    expect((await findByTestId("mp-machine-pending")).textContent).toContain(
      SELF_MACHINE_NAME,
    );
  });

  it("chat room: the in-place ⚡喚醒 surfaces the notice too (separate handler)", async () => {
    __setMockMemberOnline(SEED_WARDEN, true);
    __setMockActivationPending(true);
    window.location.hash = "#office/chat/mira";
    const { container, queryByTestId } = renderOffice();

    const wake = await waitFor(() => {
      const b = container.querySelector(
        "button.chat__wake-btn",
      ) as HTMLButtonElement | null;
      expect(b, "the in-chat wake button must exist").not.toBeNull();
      return b!;
    });

    fireEvent.click(wake);

    await waitFor(() =>
      expect(queryByTestId("chat-wake-undispatched")).not.toBeNull(),
    );
  });
});
