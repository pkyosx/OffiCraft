// T-ed38 手動置頂 — the pin entry and its failure behaviour.
//
// Driven through the REAL OfficePage so the whole chain is exercised: the
// avatar button that opens the panel, the toggle, the settings write, and the
// roster re-sorting behind it. A panel-only test would prove the button renders
// and nothing about whether pinning works.
//
// Two failure modes get their own cases because both fail SILENTLY otherwise:
//   · a settings READ that rejects must degrade to "nothing pinned" (the
//     roster still renders, it simply has no pins) — never a blank rail;
//   · a settings WRITE that rejects must SNAP BACK, so the owner is never left
//     looking at a pin that was not saved.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { render, waitFor, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { __resetMock, __injectMockMember } from "../api/mock";

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

afterEach(() => {
  vi.restoreAllMocks();
});

/** Open Bob's detail panel through the roster row's avatar button — the same
 * existing, keyboard-reachable entry the design chose to reuse. */
async function openBobPanel() {
  const view = renderOffice();
  await waitFor(() =>
    expect(view.container.querySelectorAll(".member-card").length).toBe(2),
  );
  // Let the pinned-set read settle BEFORE grabbing a row: it re-groups the
  // rows, so a node captured earlier lands detached and the click goes nowhere.
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  const bobCard = [...view.container.querySelectorAll(".member-card")].find(
    (c) => c.querySelector(".member-card__name")?.textContent === "Bob",
  )!;
  fireEvent.click(bobCard.querySelector(".member-card__avatar")!);
  await view.findByTestId("mp-pin-toggle");
  return view;
}

describe("MemberDetailPanel — 置頂 / 取消置頂 (T-ed38)", () => {
  it("pins a member, and the roster lifts them to the top", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    const { container, findByTestId } = await openBobPanel();

    const toggle = await findByTestId("mp-pin-toggle");
    // State rides aria-pressed, so the label can stay a short verb.
    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    expect(toggle.textContent).toBe("置頂");

    await act(async () => {
      fireEvent.click(toggle);
    });
    await waitFor(async () =>
      expect(
        (await findByTestId("mp-pin-toggle")).getAttribute("aria-pressed"),
      ).toBe("true"),
    );
    expect((await findByTestId("mp-pin-toggle")).textContent).toBe("取消置頂");

    // It reached the server (the cross-device promise), not just local state.
    const settings = await api.getServerSettings();
    expect(settings.pinnedMemberIds).toEqual(["bob"]);

    // …and the roster shows it: back out of the panel, Bob is now first even
    // though Mira's assistant role would otherwise win.
    fireEvent.click(container.querySelector(".mp__back")!);
    await waitFor(() =>
      expect(
        [...container.querySelectorAll(".member-card__name")].map(
          (n) => n.textContent,
        ),
      ).toEqual(["Bob", "Mira"]),
    );
  });

  it("a NEW pin goes to the FRONT of the stored set (newest pin on top)", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    await api.patchServerSettings({ pinnedMemberIds: ["mira"] });

    const { findByTestId } = await openBobPanel();
    await act(async () => {
      fireEvent.click(await findByTestId("mp-pin-toggle"));
    });

    await waitFor(async () =>
      expect((await api.getServerSettings()).pinnedMemberIds).toEqual([
        "bob",
        "mira",
      ]),
    );
  });

  it("unpins, leaving the rest of the set (and its order) alone", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    await api.patchServerSettings({ pinnedMemberIds: ["bob", "mira"] });

    const { findByTestId } = await openBobPanel();
    const toggle = await findByTestId("mp-pin-toggle");
    expect(toggle.getAttribute("aria-pressed")).toBe("true");

    await act(async () => {
      fireEvent.click(toggle);
    });
    await waitFor(async () =>
      expect((await api.getServerSettings()).pinnedMemberIds).toEqual(["mira"]),
    );
  });
});

describe("MemberDetailPanel — 置頂 settings failures degrade honestly", () => {
  it("a failed settings READ degrades to no pins — the roster still renders", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "getServerSettings").mockRejectedValue(
      new ApiError("http 500 for GET /api/settings", 500, "internal", "boom"),
    );

    const { container, findByTestId } = renderOffice();
    // The roster renders exactly as it does with nothing pinned — no blank
    // rail, no group wrapper, no divider.
    await waitFor(() =>
      expect(container.querySelectorAll(".member-card").length).toBe(2),
    );
    expect(
      container.querySelector('[data-testid="pinned-group"]'),
    ).toBeNull();
    // And the affordance is still offered (an unreadable pin set is not the
    // same as "pinning is broken").
    const bobCard = [...container.querySelectorAll(".member-card")].find(
      (c) => c.querySelector(".member-card__name")?.textContent === "Bob",
    )!;
    fireEvent.click(bobCard.querySelector(".member-card__avatar")!);
    expect(
      (await findByTestId("mp-pin-toggle")).getAttribute("aria-pressed"),
    ).toBe("false");
  });

  it("a failed settings WRITE snaps the toggle back to the last confirmed value", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    vi.spyOn(console, "warn").mockImplementation(() => {});

    const { findByTestId } = await openBobPanel();
    const toggle = await findByTestId("mp-pin-toggle");
    expect(toggle.getAttribute("aria-pressed")).toBe("false");

    vi.spyOn(api, "patchServerSettings").mockRejectedValue(
      new ApiError("http 500 for PATCH /api/settings", 500, "internal", "boom"),
    );
    await act(async () => {
      fireEvent.click(toggle);
    });

    // Rolled back: the owner must not be shown a pin that was never stored.
    await waitFor(async () =>
      expect(
        (await findByTestId("mp-pin-toggle")).getAttribute("aria-pressed"),
      ).toBe("false"),
    );
  });
});
