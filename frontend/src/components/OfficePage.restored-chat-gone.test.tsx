// C-153d: the office remembers your last chat — but the peer can be gone by the
// time you come back (member fired, outsource worker released). A cold load then
// used to land on the read-only "已釋出 / 對話已不在" history panel that nobody
// asked for. OfficePage now reports that back to App, which returns to the
// roster and forgets the memory.
//
// 🔴 The whole risk of this feature is ONE confusion: "the roster has not
// loaded yet" and "this peer no longer exists" are the SAME missing-id lookup.
// Acting on the first would erase the memory of a perfectly live conversation —
// silently, and worse than the problem being fixed. So the trigger is
// `releasedPeer`, which is already gated on BOTH the member list and the live
// outsource list having settled (T-661b review finding #1/#2), never a bare
// `roster.find(...)` miss. The "peer that DOES exist" test below is the one that
// holds that line: with an ungated check it fails, because during the loading
// window a live member looks exactly like a departed one.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { __resetMock, __injectMockChat } from "../api/mock";

function renderOffice(props: {
  restoredChatId?: string;
  onRestoredChatGone?: () => void;
}) {
  return render(
    <I18nProvider>
      <OfficePage {...props} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  // jsdom has no scrollIntoView; ChatArea calls it once a thread renders.
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage — a RESTORED chat whose peer is gone", () => {
  it("reports the restored chat as gone once both lists have settled", async () => {
    const goneId = "m-1892d870ded7"; // never on the roster, never a live worker
    __injectMockChat({
      id: "m-old",
      from: goneId,
      to: "owner",
      body: "離職前的最後一則訊息。",
      ts: Date.now() / 1000 - 600,
      attachments: [],
      replyCardId: null,
    });
    window.location.hash = `#office/chat/${goneId}`;
    const onRestoredChatGone = vi.fn();

    renderOffice({ restoredChatId: goneId, onRestoredChatGone });

    await waitFor(() => expect(onRestoredChatGone).toHaveBeenCalled());
  });

  it("stays put when the restored peer DOES exist — a still-loading roster is never read as 'gone'", async () => {
    // `mira` is on the mock roster, but she is NOT there on the first render:
    // the roster arrives asynchronously. An existence check that skips the load
    // gate fires in that window and throws away a live chat's memory.
    window.location.hash = "#office/chat/mira";
    const onRestoredChatGone = vi.fn();

    const { findAllByText } = renderOffice({
      restoredChatId: "mira",
      onRestoredChatGone,
    });

    // Wait for the roster to actually land, so the loading window is genuinely
    // traversed rather than the assertion racing ahead of it.
    // (Mira shows in BOTH the roster rail and the open chat header — hence All.)
    await findAllByText("Mira");
    expect(onRestoredChatGone).not.toHaveBeenCalled();
  });

  it("leaves an EXPLICITLY deep-linked departed peer on its read-only history (T-661b)", async () => {
    // Same departed peer, but reached by 跳到原訊息 rather than restored from
    // memory (restoredChatId is undefined). That conversation must stay open —
    // silently bouncing it to the roster is the T-661b regression coming back.
    const workerId = "ow-353820f2c636";
    __injectMockChat({
      id: "m-orig",
      from: workerId,
      to: "owner",
      body: "外包回報:任務初稿完成,請確認。",
      ts: Date.now() / 1000 - 120,
      attachments: [],
      replyCardId: null,
    });
    window.location.hash = `#office/chat/${workerId}/msg/m-orig`;
    const onRestoredChatGone = vi.fn();

    const { findByText, findByTestId } = renderOffice({ onRestoredChatGone });

    await findByText("外包回報:任務初稿完成,請確認。");
    await findByTestId("released-chat-sub"); // the honest 已釋出 identity
    expect(onRestoredChatGone).not.toHaveBeenCalled();
  });
});
