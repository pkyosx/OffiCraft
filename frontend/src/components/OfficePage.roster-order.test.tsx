// T-ed38 — the roster's grouped rendering, through the REAL OfficePage.
//
// rosterOrder.test.ts already locks the comparator as a pure function. What can
// still go wrong at this level is the WIRING: the sorted list reaching the
// screen, the pinned/unpinned split, and the four boundary rules for the
// divider (Iris 3e):
//
//   1. zero pinned      → no hairline AND no group wrapper (today's roster);
//   2. all pinned       → no hairline AND no group wrapper either (the
//                         condition is "BOTH groups non-empty", not "the
//                         pinned group is non-empty");
//   3. mixed            → hairline on the FIRST UNPINNED card's group;
//   4. inside the group → the stored pin order (contract 2, covered in the
//                         comparator's own suite).

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { api } from "../api";
import {
  __resetMock,
  __injectMockChat,
  __injectMockMember,
  __emitMockTopic,
} from "../api/mock";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
}

const DIVIDED = "office__roster-group--divided";

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage — roster order reaches the screen", () => {
  it("puts an unread member above one the owner merely spoke to recently", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    __injectMockMember({ id: "cara", name: "Cara", role_key: "engineer" });
    // Cara's thread is the freshest, but only Bob is WAITING on an answer.
    __injectMockChat({
      id: "m-cara",
      from: "owner",
      to: "cara",
      body: "剛聊完",
      ts: 9000,
      attachments: [],
      replyCardId: null,
    });
    __injectMockChat({
      id: "m-bob",
      from: "bob",
      to: "owner",
      body: "在等你回",
      ts: 100,
      attachments: [],
      replyCardId: null,
    });

    const { container } = renderOffice();
    await waitFor(() =>
      expect(
        [...container.querySelectorAll(".member-card__name")].map(
          (n) => n.textContent,
        ),
      ).toEqual(["Bob", "Cara", "Mira"]),
    );
  });
});

describe("OfficePage — pinned group boundaries (Iris 3e)", () => {
  it("零置頂: no group wrapper and no hairline — the roster looks unchanged", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    const { container, queryByTestId } = renderOffice();
    await waitFor(() =>
      expect(container.querySelectorAll(".member-card").length).toBe(2),
    );
    expect(queryByTestId("pinned-group")).toBeNull();
    expect(container.querySelector(`.${DIVIDED}`)).toBeNull();

    // Rule 1 to the LETTER: no group wrapper either. Zero pinned is the state
    // of every owner who has never touched this feature, and in it the DOM must
    // be what it was before the feature shipped — so the cards are DIRECT
    // children of the list, not children of a wrapper that happens to be
    // invisible today. Asserting the absent testid alone would not catch a
    // wrapper that merely lost its testid, so assert the parentage instead.
    expect(queryByTestId("unpinned-group")).toBeNull();
    const list = container.querySelector(".office__members-list");
    expect(list).not.toBeNull();
    expect(
      [...list!.children].map((c) => c.classList.contains("member-card")),
    ).toEqual([true, true]);
  });

  it("混合: the hairline sits on the FIRST UNPINNED group, and the pinned group is labelled", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    await api.patchServerSettings({ pinnedMemberIds: ["bob"] });

    const { container, findByTestId, getByTestId } = renderOffice();
    const group = await findByTestId("pinned-group");
    // The group carries the SEMANTICS (the hairline is decorative and must not
    // be announced): role=group + a label, and no duplicate separator role.
    expect(group.getAttribute("role")).toBe("group");
    expect(group.getAttribute("aria-label")).toBe("已置頂");
    expect(container.querySelector('[role="separator"]')).toBeNull();

    // Bob is pinned → he is IN the pinned group and FIRST overall, even though
    // Mira's assistant role would otherwise put her on top.
    expect(
      [...group.querySelectorAll(".member-card__name")].map(
        (n) => n.textContent,
      ),
    ).toEqual(["Bob"]);
    await waitFor(() =>
      expect(
        [...container.querySelectorAll(".member-card__name")].map(
          (n) => n.textContent,
        ),
      ).toEqual(["Bob", "Mira"]),
    );

    // The divider is on the UNPINNED group (its top edge), not on the pinned one.
    expect(getByTestId("unpinned-group").className).toContain(DIVIDED);
    expect(group.className).not.toContain(DIVIDED);
  });

  it("全部置頂: no hairline AND no unpinned wrapper (the group does not exist)", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    await api.patchServerSettings({ pinnedMemberIds: ["bob", "mira"] });

    const { container, findByTestId, queryByTestId } = renderOffice();
    const group = await findByTestId("pinned-group");
    await waitFor(() =>
      expect(group.querySelectorAll(".member-card").length).toBe(2),
    );
    // A trailing line with nothing under it would be the bug here.
    expect(container.querySelector(`.${DIVIDED}`)).toBeNull();

    // Rule 2 to the LETTER, mirroring rule 1 above: no wrapper either. With
    // everyone pinned the "unpinned group" does not exist, and a div that
    // represents nothing makes the DOM lie — the parent's `gap: 8px` merely
    // makes the lie visible (8px of dead space after the last card). So the
    // ONLY child of the list is the pinned group, and the empty container is
    // not there under any testid.
    expect(queryByTestId("unpinned-group")).toBeNull();
    const list = container.querySelector(".office__members-list");
    expect(list).not.toBeNull();
    expect([...list!.children]).toEqual([group]);
  });

  it("a pinned member does NOT get reshuffled when someone else gets unread", async () => {
    // Contract 2 at the wiring level: the pin holds its place even as the
    // automatic rules churn underneath it.
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    __injectMockMember({ id: "cara", name: "Cara", role_key: "engineer" });
    await api.patchServerSettings({ pinnedMemberIds: ["cara"] });

    const { container } = renderOffice();
    const rosterNames = () =>
      [...container.querySelectorAll(".member-card__name")].map(
        (n) => n.textContent,
      );
    await waitFor(() => expect(rosterNames()[0]).toBe("Cara"));

    __injectMockChat({
      id: "m-bob",
      from: "bob",
      to: "owner",
      body: "在等你回",
      ts: Date.now() / 1000,
      attachments: [],
      replyCardId: null,
    });
    await act(async () => {
      __emitMockTopic("chat");
      await Promise.resolve();
    });

    // Bob climbed over Mira (proving the re-sort happened) but never over the
    // pinned Cara.
    await waitFor(() => expect(rosterNames()).toEqual(["Cara", "Bob", "Mira"]));
  });

  it("an ORPHAN pin (a member who is gone) renders no ghost row and no empty group", async () => {
    await api.patchServerSettings({
      pinnedMemberIds: ["m-dismissed-long-ago"],
    });
    const { container, queryByTestId } = renderOffice();
    await waitFor(() =>
      expect(container.querySelectorAll(".member-card").length).toBe(1),
    );
    // Only the live member renders, and the pinned group is not drawn at all.
    expect(
      [...container.querySelectorAll(".member-card__name")].map(
        (n) => n.textContent,
      ),
    ).toEqual(["Mira"]);
    expect(queryByTestId("pinned-group")).toBeNull();
    expect(container.querySelector(`.${DIVIDED}`)).toBeNull();
  });
});
