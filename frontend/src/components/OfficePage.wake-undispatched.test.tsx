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
        ".member-actions button",
      ) as HTMLButtonElement | null;
      expect(b, "the wake button must exist").not.toBeNull();
      expect(b!.disabled, "the wake button must be enabled").toBe(false);
      return b!;
    });

    fireEvent.click(wake);

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
        ".member-actions button",
      ) as HTMLButtonElement | null;
      expect(b, "the wake button must exist").not.toBeNull();
      expect(b!.disabled).toBe(false);
      return b!;
    });

    fireEvent.click(wake);

    // Give the same async settling the positive case needed, then assert the
    // notice never appeared. (The mock DOES move presence here, so this also
    // pins that a landed wake keeps behaving exactly as it did before.)
    await waitFor(() =>
      expect(
        container.querySelector(".member-actions button"),
      ).not.toBeNull(),
    );
    expect(queryByTestId("mp-wake-undispatched")).toBeNull();
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
