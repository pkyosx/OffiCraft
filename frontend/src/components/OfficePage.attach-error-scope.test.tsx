// T-48 R14-2.1 — the staging refusal is scoped to the CHAT PAGE, the draft is not.
//
// 「圖片太大」/「最多 10 個檔案」 was component state on the composer until the
// draft store took it, so leaving the office page took it with it. Moving it to
// a module-level table gave it one lifetime too many: refuse a 30 MB image,
// walk to 任務, come back ten minutes later, and the red sentence is still on
// screen describing a drop from ten minutes ago.
//
// The draft and its staged files must NOT follow it out — surviving the
// navigation is the whole reason they live outside the composer.
//
// 🔴 EVERY CASE HERE RENDERS UNDER <StrictMode> (R16 D-2). This file used to
// render bare, and the app does not: `main.tsx` wraps everything in StrictMode
// unconditionally, which runs each effect setup → cleanup → setup on the first
// mount. The guard therefore could not see its own subject — the unmount clear
// it was written to protect wiped every peer's notice, so the FIRST of those
// two setups destroyed a refusal that had been raised BEFORE the page mounted.
// Dev and prod disagreed about something the owner can see, and this file was
// green for both. A guard that renders differently from the app is not
// watching the app.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { StrictMode } from "react";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { __resetMock } from "../api/mock";
import {
  getChatAttachError,
  getChatDraft,
  resetChatDrafts,
  setChatAttachError,
  updateChatDraftAttachments,
} from "../lib/chatDraftStore";

// 🔴 THE PEER `OfficePage` ACTUALLY SELECTS (R16 D-2). This file used to key
// every assertion to "m-1", which is not in the mock roster at all — so the
// store assertions were true of a room that was never on screen, and no
// assertion in the file could have noticed the composer had stopped rendering
// the sentence. `mira` is the peer the page lands on with an empty hash.
const PEER = "mira";
const NOTICE = "圖片太大（上限 20 MB）";

/** The app's own shape: StrictMode, exactly as `main.tsx` mounts it. */
function renderOffice() {
  return render(
    <StrictMode>
      <I18nProvider>
        <OfficePage />
      </I18nProvider>
    </StrictMode>,
  );
}

async function officeIsUp(view: { container: HTMLElement }) {
  await waitFor(() => expect(view.container.querySelector(".office")).not.toBeNull());
}

beforeEach(() => {
  __resetMock();
  resetChatDrafts();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage", () => {
  it("drops a staging refusal when the chat page is left, and keeps the draft", async () => {
    const view = renderOffice();
    await officeIsUp(view);

    // Typed into the REAL composer, not written behind its back: `mira` is the
    // room on screen, so its composer owns the draft text and would save its own
    // (empty) box over anything poked into the store.
    const box = view.container.querySelector("textarea")!;
    fireEvent.change(box, { target: { value: "沒送完的字" } });
    act(() => {
      updateChatDraftAttachments(PEER, () => [
        { key: "pa-1", filename: "shot.png" } as never,
      ]);
      setChatAttachError(PEER, NOTICE);
    });

    // 跳到「任務」/「監控」: the whole office page unmounts.
    view.unmount();

    // The close is deferred one microtask past the unmount so StrictMode's
    // synchronous remount can cancel it; nothing paints in that gap.
    await waitFor(() => expect(getChatAttachError(PEER)).toBeNull());
    expect(getChatDraft(PEER)?.text).toBe("沒送完的字");
    expect(getChatDraft(PEER)?.attachments).toHaveLength(1);

    // …and coming back shows no sentence about something ten minutes old.
    const back = renderOffice();
    await officeIsUp(back);
    expect(getChatAttachError(PEER)).toBeNull();
  });

  it("keeps a refusal raised while the page was away, so the owner sees it on their return", async () => {
    // R11-4 / R12-1, the reason the notice is in a module-level table at all:
    // drop a 30 MB image, walk to 任務, and the `FileReader` finishes its refusal
    // while nothing is mounted. It has to be on screen when they come back.
    setChatAttachError(PEER, NOTICE);

    const view = renderOffice();
    await officeIsUp(view);

    expect(getChatAttachError(PEER)).toBe(NOTICE);
    // …and still there after the mount has fully settled, not merely surviving
    // the first frame.
    await Promise.resolve();
    expect(getChatAttachError(PEER)).toBe(NOTICE);

    // 🔴 AND IT IS ON SCREEN, not merely in the table. Every other assertion in
    // this file reads the store, so a composer that stopped rendering the
    // sentence would keep them all green — the owner's side of this contract is
    // the red line under the composer, not a Map entry.
    await waitFor(() =>
      expect(
        view.container.querySelector(".chat__preview-error")?.textContent,
      ).toBe(NOTICE),
    );
  });

  it("drops that same returned-to refusal when the page is left again", async () => {
    // The R14-2.1 lifetime applies to it too once it has been shown: the scope
    // closes on the notices that exist when the last surface goes, whenever they
    // were raised.
    setChatAttachError(PEER, NOTICE);
    const view = renderOffice();
    await officeIsUp(view);
    expect(getChatAttachError(PEER)).toBe(NOTICE);

    view.unmount();
    await waitFor(() => expect(getChatAttachError(PEER)).toBeNull());
  });
});
