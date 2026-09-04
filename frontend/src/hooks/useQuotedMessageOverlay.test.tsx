// useQuotedMessageOverlay — the ONE exit behind the chat bubble's 「看原訊息」
// (T-0b78), and the only place allowed to fetch a quoted message by id.
//
// 🔴 WHAT THIS FILE IS FOR. The chat bubble's quote row fetches the one message
// by id and shows it whole (owner 2026-08-21). T-0b78 briefly routed the 請示
// card and the inline task card through the same hook; owner 2026-08-29 sent
// those two BACK to navigating (#office/chat/<id>/msg/<msgId>), so this hook has
// exactly one caller again. The last test here is what makes a SECOND copy of
// the fetch go red instead of shipping.
//
// 🔴 AND HERE IS WHAT THAT LAST TEST DOES *NOT* CATCH, stated because the
// version before this one claimed the whole property and only enforced part of
// it. It reads SOURCE TEXT. An independent reviewer walked straight through it
// by assembling the method name at runtime ("getChat" + "Message") and shipped
// a full second copy — read, overlay and failure state — with every test green
// and typecheck clean. So it catches the copy someone writes by HAND, which is
// the one that actually happens, and it does not catch deliberate evasion:
// dynamic names, an alias, or going under the adapter to fetch directly.
// Closing that needs an AST-level rule over the whole api surface; it is not
// here, and pretending otherwise is how the previous claim got written.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { api } from "../api";
import type { ChatMessage } from "../api/adapter";
import { useQuotedMessageOverlay } from "./useQuotedMessageOverlay";

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m1",
    to: "owner",
    body: "",
    ts: 1,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    ...over,
  };
}

/** A surface that has a LOADED WINDOW of messages on screen and a control that
 * asks for one message by id. `window` is what the deleted design searched;
 * `targetId` is deliberately not in it. */
function Harness({
  windowMsgs,
  targetId,
}: {
  windowMsgs: ChatMessage[];
  targetId: string;
}) {
  const quoted = useQuotedMessageOverlay(undefined);
  return (
    <div>
      {windowMsgs.map((m) => (
        <div key={m.id} data-msg-id={m.id}>
          {m.body}
        </div>
      ))}
      <button type="button" onClick={() => void quoted.open(targetId)}>
        看原訊息
      </button>
      {quoted.failureNotice(targetId)}
      {quoted.overlay}
    </div>
  );
}

function renderHarness(windowMsgs: ChatMessage[], targetId: string) {
  return render(
    <I18nProvider>
      <Harness windowMsgs={windowMsgs} targetId={targetId} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("useQuotedMessageOverlay", () => {
  it("shows the message whole, by id, and never touches the route", async () => {
    const full = "原訊息開頭" + "，還有後面很長的一整段".repeat(10);
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue(mkMsg({ id: "c-1", from: "m1", body: full }));
    window.location.hash = "";

    const { getByText } = renderHarness([], "c-1");
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });

    expect(get).toHaveBeenCalledWith("c-1");
    expect(document.querySelector(".md-preview")?.textContent).toContain(
      "後面很長的一整段",
    );
    expect(window.location.hash).toBe("");
  });

  // 🔴 DENSITY, NOT AGE. The variable that pushes a message out of a loaded
  // window is how much traffic came AFTER it, not how old it is: a message sent
  // seconds ago is already out of a 30-row window on a busy line. A fixture
  // built out of "a very old message" tests the wrong thing and would still
  // pass under a design that resolved the target from the window whenever the
  // window happened to be deep enough. Here the target is the SECOND-newest
  // message on the line by timestamp, and it is out of the window anyway,
  // because 40 messages landed on that line in the same minute.
  it("shows the right message when a busy line has pushed it out of the loaded window", async () => {
    const targetTs = 10_000;
    const loaded = Array.from({ length: 40 }, (_, i) =>
      mkMsg({
        id: `c-noise-${i}`,
        body: `後續洗版 ${i}`,
        // Every one of these landed AFTER the target — same minute, same line.
        ts: targetTs + 1 + i,
      }),
    );
    // The target is NOT old: only one of the 40 above is newer than it by more
    // than a minute, and it is younger than nothing on screen by age alone.
    const target = mkMsg({
      id: "c-target",
      body: "被洗掉的那一則的全文",
      ts: targetTs,
    });
    const get = vi.spyOn(api, "getChatMessage").mockResolvedValue(target);

    const { container, getByText } = renderHarness(loaded, "c-target");
    // Precondition: it really is absent from everything on screen.
    expect(container.querySelector("[data-msg-id='c-target']")).toBeNull();

    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });

    expect(get).toHaveBeenCalledWith("c-target");
    const overlay = document.querySelector(".md-preview")!;
    expect(overlay, "the window must have no say in this").toBeTruthy();
    expect(overlay.textContent).toContain("被洗掉的那一則的全文");
    // and it did NOT fall back to whatever the window happened to hold.
    expect(overlay.textContent).not.toContain("後續洗版");
  });

  it("says the read failed, on screen, and opens nothing", async () => {
    vi.spyOn(api, "getChatMessage").mockRejectedValue(new Error("boom"));

    const { getByTestId, getByText } = renderHarness([], "c-1");
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });

    expect(getByTestId("msg-quote-error").textContent).toBe(
      zh.chat.replyQuoteOpenFailed,
    );
    expect(document.querySelector(".md-preview")).toBeNull();
  });

  it("is a single request per click, however impatiently the button is pressed", async () => {
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue(mkMsg({ id: "c-1", body: "全文" }));

    const { getByText } = renderHarness([], "c-1");
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
      fireEvent.click(getByText("看原訊息"));
    });

    expect(get).toHaveBeenCalledTimes(1);
  });

  it("切走再切回,上一趟點開的引用原文不准蓋在這一趟的房間上", async () => {
    // 🔴 T-48 R8-3。在 A 按下「看原訊息」、讀取慢、切到 B,讀取落地時不准把 A
    // 的那則訊息以全螢幕 overlay 蓋在 B 的房間上,也不准把焦點交給一顆已經隨 A
    // 的列消失的按鈕。「overlay 蓋住整頁所以切不了對話」這個豁免在這裡不成立:
    // 讀取期間 overlay 根本還沒開,roster 完全點得動。
    //
    // 🔴 換房就是換一份 hook(R13-5):`ChatArea` 掛在 `key={peerId}` 底下,所以
    // 下面用 key 換掉整個 Harness 來驅動,跟 app 走同一條路。這支 hook 以前為此
    // 收一個 visit token 參數,那個參數隨著 remount 一起退場。
    let land!: (m: ChatMessage) => void;
    vi.spyOn(api, "getChatMessage").mockReturnValue(
      new Promise<ChatMessage>((r) => {
        land = r;
      }),
    );

    const { getByText, rerender } = render(
      <I18nProvider>
        <Harness key="visit-1" windowMsgs={[]} targetId="c-1" />
      </I18nProvider>,
    );
    act(() => {
      fireEvent.click(getByText("看原訊息"));
    });
    expect(
      document.querySelector(".md-preview"),
      "前提:讀取還在路上,overlay 還沒開,切換手勢是暢通的",
    ).toBeNull();

    rerender(
      <I18nProvider>
        <Harness key="visit-2" windowMsgs={[]} targetId="c-1" />
      </I18nProvider>,
    );
    await act(async () => {
      land(mkMsg({ id: "c-1", body: "上一趟點開的原文" }));
      await Promise.resolve();
    });

    expect(
      document.querySelector(".md-preview"),
      "上一趟的引用原文不准蓋在這一趟的房間上",
    ).toBeNull();

    // …而這一趟自己按的那一次照樣開得起來(換房沒有把這條路整個關掉)。
    vi.spyOn(api, "getChatMessage").mockResolvedValue(
      mkMsg({ id: "c-1", body: "這一趟自己按的原文" }),
    );
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });
    expect(document.querySelector(".md-preview")).not.toBeNull();
  });

  // 🔴 THE ANTI-SECOND-COPY CLAUSE. Behaviour tests cannot see a duplicate:
  // a call site that grew its OWN api.getChatMessage + its own error state
  // would draw the same pixels and pass every test above. This reads SOURCE
  // TEXT instead. Its exact reach — and what walks through it — is written at
  // the top of this file; the name of this test says only what it enforces.
  it("no component names getChatMessage — the hook is the only one that does", () => {
    // vitest runs with the frontend package as cwd.
    const root = resolve(process.cwd(), "src");
    // The ONE known call site must actually take the shared exit. Named, so it
    // quietly dropping the hook goes red here rather than only in a behaviour
    // test that a second copy would keep green.
    //
    // 🔴 WHY ONLY ONE, when this list used to name three. RepliesPage.tsx and
    // TaskReplyCard.tsx were removed on owner's 2026-08-29 ruling (「1 跟 2 變回
    // 去原本那樣」): those two controls NAVIGATE now — they write
    // #office/chat/<id>/msg/<msgId> and never read a message themselves — so
    // requiring them to import this hook would be requiring a call they must not
    // make. They are not exempt from the sweep below: if either one ever grows
    // its own api.getChatMessage, the whole-tree sweep still catches it. What is
    // gone is only the "must take the shared exit" obligation, and only because
    // they have no fetch to share.
    for (const rel of ["src/components/ChatArea.tsx"]) {
      const src = readFileSync(resolve(process.cwd(), rel), "utf8");
      expect(src, `${rel} must take the shared exit`).toContain(
        "useQuotedMessageOverlay(",
      );
    }
    // 🔴 AND THE SWEEP IS THE WHOLE TREE, not those three paths. The version
    // before this one checked only the three files it already knew about, so a
    // FOURTH component with its own copy — a new screen, a new card — passed
    // untouched. That is the copy that actually happens: nobody edits the three
    // guarded files to add a duplicate, they write a new one.
    const offenders: string[] = [];
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const full = resolve(dir, e.name);
        if (e.isDirectory()) {
          walk(full);
          continue;
        }
        if (!/\.(ts|tsx)$/.test(e.name)) continue;
        // Tests may name it (they mock it). The api layer DEFINES it. The hook
        // is the one place allowed to CALL it.
        if (/\.test\.tsx?$/.test(e.name)) continue;
        const rel = full.slice(root.length + 1);
        if (rel.startsWith("api/")) continue;
        if (rel === "hooks/useQuotedMessageOverlay.tsx") continue;
        const code = readFileSync(full, "utf8")
          .split("\n")
          .filter((l) => !/^\s*(\/\/|\*|\/\*)/.test(l))
          .join("\n");
        if (code.includes("getChatMessage")) offenders.push(rel);
      }
    };
    walk(root);
    expect(
      offenders,
      "these files fetch the quoted message themselves instead of taking the " +
        "shared exit (hooks/useQuotedMessageOverlay)",
    ).toEqual([]);
  });
});
