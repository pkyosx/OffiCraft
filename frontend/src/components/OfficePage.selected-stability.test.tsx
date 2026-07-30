// T-ed38 P2 — the sort must never swap the conversation on screen.
//
// This is the ticket's only navigation-semantics change, and the failure it
// prevents was MEASURED, not theorised (docs/T-ed38/verification.md §0.5): with
// only the comparator changed, a probe watched the desktop chat header go
// Mira → Bob after another member sent a message, with zero user input.
//
// Mechanism: `selected` is a derived const recomputed every render, and
// `useMembers` refetches on the `chat` SSE topic. Once the order moves on
// unread/recency, the empty-selection fallback (`roster[0]`) becomes a moving
// target — and the event most likely to move it is "someone sent a message",
// i.e. exactly when the owner is most likely to be looking at the screen.
//
// Scope guard: this locks BOTH halves. The default must hold still, AND the
// T-661b narrowing (an explicit chatId never silently substitutes roster[0])
// must stay intact — a fix that froze more than the empty-selection default
// would show up as the second test here.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
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

/** Land an inbound member→owner message AND fan the `chat` topic, the way the
 * real server does — which is what drives useMembers to refetch the roster. */
async function inboundMessageFrom(from: string, id: string) {
  __injectMockChat({
    id,
    from,
    to: "owner",
    body: `${from} 的訊息`,
    ts: Date.now() / 1000 - 5,
    attachments: [],
    replyCardId: null,
  });
  await act(async () => {
    __emitMockTopic("chat");
    await Promise.resolve();
  });
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage — the shown conversation survives a re-sort (T-ed38 P2)", () => {
  it("keeps the SAME chat open when a re-sort moves another member to the top", async () => {
    // Bob has no assistant role, so he starts BELOW Mira and the desktop chat
    // pane defaults to Mira.
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });

    const { container } = renderOffice();
    const headerName = () =>
      container.querySelector(".chat__header-name")?.textContent ?? "";
    const rosterNames = () =>
      [...container.querySelectorAll(".member-card__name")].map(
        (n) => n.textContent,
      );

    await waitFor(() => expect(headerName()).toBe("Mira"));

    // Bob messages the owner → unread on Bob → Bob sorts to the top.
    await inboundMessageFrom("bob", "m-bob");

    // POSITIVE CONTROL FIRST: the roster really did re-sort. Without this the
    // assertion below could pass simply because nothing happened at all.
    await waitFor(() => expect(rosterNames()[0]).toBe("Bob"));

    // THE CONTRACT: the owner never touched anything, so the conversation on
    // screen must still be Mira's.
    expect(headerName()).toBe("Mira");
  });

  it("still holds after several re-sorts, and lets the owner pick freely", async () => {
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    __injectMockMember({ id: "cara", name: "Cara", role_key: "engineer" });

    const { container } = renderOffice();
    const headerName = () =>
      container.querySelector(".chat__header-name")?.textContent ?? "";
    const rosterNames = () =>
      [...container.querySelectorAll(".member-card__name")].map(
        (n) => n.textContent,
      );

    await waitFor(() => expect(headerName()).toBe("Mira"));

    await inboundMessageFrom("bob", "m-bob");
    await waitFor(() => expect(rosterNames()[0]).toBe("Bob"));
    await inboundMessageFrom("cara", "m-cara");
    await waitFor(() => expect(rosterNames()[0]).toBe("Cara"));

    // Two reorders later the pane has still not moved by itself…
    expect(headerName()).toBe("Mira");

    // …and an EXPLICIT pick still works (the guard froze the default's
    // evaluation, not the selection itself).
    window.location.hash = "#office/chat/bob";
    await waitFor(() => expect(headerName()).toBe("Bob"));
  });

  // Named for what it actually asserts. It was called "does NOT freeze an
  // EXPLICIT chatId onto the default", which over-promised: freezing is only
  // observable after the selection is cleared back to empty, and this test
  // never clears it. The over-freeze direction is covered by the test below;
  // a control-group run found that this one could not detect it at all.
  it("renders an unresolvable EXPLICIT chatId's own history, never the default member's room (T-661b)", async () => {
    // The narrowing this ticket must not touch: an explicit chatId that
    // resolves to no roster member renders its OWN read-only history, never the
    // default member's room.
    const goneId = "member-removed-xyz";
    __injectMockChat({
      id: "m-gone",
      from: goneId,
      to: "owner",
      body: "已離開成員的舊訊息。",
      ts: Date.now() / 1000 - 300,
      attachments: [],
      replyCardId: null,
    });
    window.location.hash = `#office/chat/${goneId}/msg/m-gone`;

    const { container, findByText } = renderOffice();
    await findByText("已離開成員的舊訊息。");
    const headerName =
      container.querySelector(".chat__header-name")?.textContent ?? "";
    expect(headerName).not.toContain("Mira");
  });

  it("clearing an EXPLICIT pick returns to the original default — the pick is never re-anchored onto it", async () => {
    // The over-freeze direction, and the ONLY sequence that can observe it.
    // `defaultChatIdRef` is *written* on every render but *read* only when
    // `selectedId === ""` — so a guard that wrongly captured an explicit
    // chatId onto the ref is completely invisible while that chatId is still
    // selected. It becomes observable one moment and one moment only: after
    // the selection goes back to empty and the default is resolved from the
    // ref again. Hence the pick → CLEAR → assert shape below; drop the clear
    // step and this test asserts nothing about freezing at all.
    //
    // The pick must also resolve to a LIVE roster member. An explicit id that
    // matches nobody would leave `roster.find` missing on the next render and
    // fall back to roster[0] anyway, masking the over-freeze — which is why
    // the T-661b test above, despite its name, does not cover this direction.
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });

    const { container } = renderOffice();
    const headerName = () =>
      container.querySelector(".chat__header-name")?.textContent ?? "";

    // Empty selection resolves the default once: Mira.
    await waitFor(() => expect(headerName()).toBe("Mira"));

    // The owner explicitly picks Bob.
    window.location.hash = "#office/chat/bob";
    // POSITIVE CONTROL: the pick really landed. Without this the assertion
    // below could pass simply because the hash change never took effect.
    await waitFor(() => expect(headerName()).toBe("Bob"));

    // The owner clears the selection — back to the empty-selection default.
    window.location.hash = "#office";

    // THE CONTRACT: the default was resolved ONCE, to Mira, and Bob's
    // explicit visit must not have overwritten it.
    await waitFor(() => expect(headerName()).toBe("Mira"));
  });

  it("falls back to the current first row if the remembered default is dismissed — and is STILL frozen afterwards", async () => {
    // A stale ref must not strand the pane on a member who left the roster.
    __injectMockMember({ id: "bob", name: "Bob", role_key: "engineer" });
    const { container } = renderOffice();
    const headerName = () =>
      container.querySelector(".chat__header-name")?.textContent ?? "";
    const rosterNames = () =>
      [...container.querySelectorAll(".member-card__name")].map(
        (n) => n.textContent,
      );
    await waitFor(() => expect(headerName()).toBe("Mira"));

    const { api } = await import("../api");
    await act(async () => {
      await api.dismissMember("mira");
      __emitMockTopic("member");
      await Promise.resolve();
    });

    // Mira is gone; rather than an empty pane the default resolves again.
    await waitFor(() => expect(headerName()).toBe("Bob"));

    // THE SECOND HALF, and the reason this test is not just about dismissal:
    // the fallback must RE-ANCHOR the ref, not merely paper over one render.
    // If it does not, `find` misses forever after and the default silently
    // reverts to being recomputed as roster[0] — P2's own bug, back for the
    // rest of the session, in the one session where nobody would look for it.
    await act(async () => {
      __injectMockMember({ id: "cara", name: "Cara", role_key: "engineer" });
      __emitMockTopic("member");
      await Promise.resolve();
    });
    await waitFor(() => expect(rosterNames()).toEqual(["Bob", "Cara"]));
    await inboundMessageFrom("cara", "m-cara");

    // POSITIVE CONTROL: the roster really did re-sort under the pane.
    await waitFor(() => expect(rosterNames()[0]).toBe("Cara"));
    // THE CONTRACT: the owner still touched nothing, so Bob's room stays open.
    expect(headerName()).toBe("Bob");
  });
});
