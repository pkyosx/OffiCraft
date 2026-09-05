// 辦公室 remembers the chat you were last in across a tab switch.
//
// owner 2026-08-20:「切換分頁的時候可以固定住最後的對話視窗」. Before this, the
// open chat lived only in the hash (#office/chat/<id>) and every nav click wrote
// a bare {page}, so stepping into 任務 / 等我回覆 / 監控 / 設定 and back dropped
// you on the roster with the conversation closed.
//
// What is locked here:
//   1. Leaving 辦公室 for another tab and coming back re-opens the SAME peer.
//   2. Only the peer id survives — msgId / composeSeed are one-shot intents
//      (locate this message / seed the composer) and must NOT re-fire on return.
//   3. Clicking 辦公室 while ALREADY in 辦公室 still resets to the roster: that
//      is the only way to close a chat on mobile, and re-opening what the owner
//      just closed would be a regression.
//   4. Nothing to restore (never opened a chat, or the roster was the last
//      office view) leaves the plain roster behaviour untouched — and the
//      roster is the canonical clean root, i.e. an EMPTY hash, not "#office".

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  fireEvent,
  screen,
  waitFor,
  act,
} from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";

vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => ({ open: 0 }) }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({
    orgName: fallback,
    setOrgName: () => {},
  }),
}));
// PARTIAL mock, not a whole-module factory. The real module also exports
// OwnerNameProvider — App wraps its whole tree in it — and useOwnerDisplayName.
// A factory mock returning only `useOwnerName` silently drops the other two, and
// that stays invisible until main starts USING one of them: it did, in ef72b7f
// ("引文畫出收件者，並用 owner 自己設的名字叫他"), and this file then failed with
//   No "OwnerNameProvider" export is defined on the "./hooks/useOwnerName" mock.
// Nothing about this file changed — the module grew underneath it. importOriginal
// keeps every export this test does not deliberately override, so the next export
// added to that module cannot break this file the same way.
vi.mock("./hooks/useOwnerName", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./hooks/useOwnerName")>()),
  useOwnerName: (fallback: string) => ({
    ownerName: fallback,
    setOwnerName: () => {},
  }),
}));
// OfficePage is stubbed, but it now carries the two props App uses to hand a
// vanished restored chat back (C-153d), so the stub records them for the wiring
// tests below. vi.hoisted keeps the box alive above the hoisted vi.mock factory.
const officeStub = vi.hoisted(() => ({
  props: null as null | {
    restoredChatId?: string;
    onRestoredChatGone?: () => void;
  },
}));
vi.mock("./components/OfficePage", () => ({
  OfficePage: (props: {
    restoredChatId?: string;
    onRestoredChatGone?: () => void;
  }) => {
    officeStub.props = props;
    return null;
  },
}));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));
vi.mock("./components/UserGuidePage", () => ({ GuidePage: () => null }));

import App from "./App";

// Must stay in step with App.tsx's own LAST_OFFICE_CHAT_KEY (not exported —
// it is an implementation detail of the module, and the tests below only need
// to aim the private-mode stubs at the same key).
const LAST_OFFICE_CHAT_KEY = "oc_last_office_chat";

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

// The hash is the nav SSOT and `hashchange` is delivered asynchronously, so
// every assertion on a post-click hash awaits the effect that recorded it.
async function clickTab(label: string) {
  fireEvent.click(screen.getByText(label));
  await screen.findByText(label);
}

describe("辦公室 記住最後的對話", () => {
  beforeEach(() => {
    history.replaceState(null, "", window.location.pathname);
    localStorage.clear();
    officeStub.props = null;
  });

  it("re-opens the last chat when coming back from another tab", async () => {
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/m-1892d870ded7",
    );
    renderApp();

    await clickTab(zh.nav.tasks);
    expect(window.location.hash).toBe("#tasks");

    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe("#office/chat/m-1892d870ded7");
  });

  it("restores the peer only — not msgId / composeSeed one-shots", async () => {
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/m-1892d870ded7/msg/c-abc123",
    );
    renderApp();

    await clickTab(zh.nav.monitor);
    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe("#office/chat/m-1892d870ded7");
  });

  it("also restores when returning from the Settings overlay", async () => {
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/m-1892d870ded7",
    );
    renderApp();

    fireEvent.click(screen.getByLabelText("settings"));
    expect(window.location.hash).toBe("#settings");

    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe("#office/chat/m-1892d870ded7");
  });

  it("clicking 辦公室 while already there still resets to the roster", async () => {
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/m-1892d870ded7",
    );
    renderApp();

    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe(""); // roster = canonical clean root
  });

  it("leaves the plain roster route alone when no chat was ever opened", async () => {
    renderApp();

    await clickTab(zh.nav.tasks);
    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe(""); // roster = canonical clean root
  });

  // The conductor tab embeds this console as <iframe src="<bare console URL>">
  // and UNMOUNTS it on every conductor tab switch, so coming back is a cold page
  // load with an empty hash — the in-memory ref is already gone. These four pin
  // the localStorage half that survives it (owner 2026-08-20:「在 Conductor 切換
  // iframe 的時候能夠記住嗎?」).
  it("reopens the remembered chat on a cold load of the bare URL", async () => {
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/m-1892d870ded7",
    );
    const first = renderApp();
    await screen.findByText(zh.nav.office);
    first.unmount();

    // A fresh iframe: same origin (localStorage survives), bare URL (no hash).
    history.replaceState(null, "", window.location.pathname);
    renderApp();
    await waitFor(() =>
      expect(window.location.hash).toBe("#office/chat/m-1892d870ded7"),
    );
  });

  it("does NOT hijack an explicit #office on a cold load", async () => {
    localStorage.setItem(LAST_OFFICE_CHAT_KEY, "m-1892d870ded7");
    history.replaceState(null, "", window.location.pathname + "#office");
    renderApp();
    await screen.findByText(zh.nav.office);
    expect(window.location.hash).toBe("#office");
  });

  it("does NOT hijack a deep link into another tab on a cold load", async () => {
    localStorage.setItem(LAST_OFFICE_CHAT_KEY, "m-1892d870ded7");
    history.replaceState(null, "", window.location.pathname + "#tasks");
    renderApp();
    await screen.findByText(zh.nav.tasks);
    expect(window.location.hash).toBe("#tasks");
  });

  // Safari private mode / 3rd-party storage blocking: the Storage object is
  // there, but its methods throw on access. Both halves — the READ at boot and
  // the WRITE on every office route change — must degrade quietly.
  //
  // The stub goes on `globalThis.localStorage` itself, NOT on
  // `Storage.prototype`: test/setup.ts replaces the globals with instances of
  // its own MemoryStorage class, whose methods live on MemoryStorage.prototype,
  // so a Storage.prototype patch is never reached and would silently do nothing
  // (the mistake this pair of tests was rewritten to fix). Scoped to our one
  // key so every other storage user in the app keeps working, and each test
  // asserts the stub was actually hit — an unreached mock is a green test that
  // proves nothing.
  it("degrades to the roster when the localStorage READ throws (private mode)", async () => {
    const getItem = vi
      .spyOn(globalThis.localStorage, "getItem")
      .mockImplementation((key: string) => {
        if (key === LAST_OFFICE_CHAT_KEY) throw new Error("storage blocked");
        return null;
      });
    try {
      renderApp();
      await screen.findByText(zh.nav.office);
      expect(getItem).toHaveBeenCalledWith(LAST_OFFICE_CHAT_KEY);
      expect(window.location.hash).toBe("");
    } finally {
      getItem.mockRestore();
    }
  });

  it("survives a blocked localStorage WRITE and still remembers in-session", async () => {
    const blocked = (key: string) => {
      if (key === LAST_OFFICE_CHAT_KEY) throw new Error("storage blocked");
    };
    const setItem = vi
      .spyOn(globalThis.localStorage, "setItem")
      .mockImplementation(blocked);
    const removeItem = vi
      .spyOn(globalThis.localStorage, "removeItem")
      .mockImplementation(blocked);
    try {
      history.replaceState(
        null,
        "",
        window.location.pathname + "#office/chat/m-1892d870ded7",
      );
      // The recorder effect writes on mount — a throw that escapes it would
      // tear the tree down here, so simply rendering is half the assertion.
      renderApp();
      await screen.findByText(zh.nav.office);
      expect(setItem).toHaveBeenCalledWith(
        LAST_OFFICE_CHAT_KEY,
        "m-1892d870ded7",
      );
      expect(window.location.hash).toBe("#office/chat/m-1892d870ded7");

      // Persistence is gone, but the in-memory ref still carries the session:
      // leaving for another tab and coming back re-opens the same peer.
      await clickTab(zh.nav.tasks);
      expect(window.location.hash).toBe("#tasks");
      await clickTab(zh.nav.office);
      expect(window.location.hash).toBe("#office/chat/m-1892d870ded7");
    } finally {
      setItem.mockRestore();
      removeItem.mockRestore();
    }
  });

  // The remembered id is read at mount and never again. A useRef INITIALIZER
  // ARGUMENT is evaluated on every render, so the obvious spelling
  // `useRef(readLastOfficeChat())` hits localStorage on every repaint of the
  // console — a synchronous storage read per render for a value that can only
  // change through this component's own writer.
  it("reads the remembered chat from localStorage once, not on every render", async () => {
    localStorage.setItem(LAST_OFFICE_CHAT_KEY, "m-1892d870ded7");
    const getItem = vi.spyOn(globalThis.localStorage, "getItem");
    try {
      renderApp();
      await screen.findByText(zh.nav.office);
      // Plenty of re-renders: each nav click re-routes and re-renders the shell.
      await clickTab(zh.nav.tasks);
      await clickTab(zh.nav.monitor);
      await clickTab(zh.nav.office);

      const reads = getItem.mock.calls.filter(
        ([key]) => key === LAST_OFFICE_CHAT_KEY,
      );
      expect(reads).toHaveLength(1);
    } finally {
      getItem.mockRestore();
    }
  });

  // The peer can be gone by the time you come back (member fired / outsource
  // worker released). App cannot tell — it has no roster — so OfficePage raises
  // it, and ONLY for a chat that was restored from memory. See
  // OfficePage.restored-chat-gone.test.tsx for the load-gating that makes that
  // verdict safe to act on.
  it("hands a restored chat to OfficePage so a vanished peer can be reported", async () => {
    localStorage.setItem(LAST_OFFICE_CHAT_KEY, "m-1892d870ded7");
    renderApp();
    await waitFor(() =>
      expect(window.location.hash).toBe("#office/chat/m-1892d870ded7"),
    );
    expect(officeStub.props?.restoredChatId).toBe("m-1892d870ded7");
  });

  it("falls back to the roster and forgets the chat once OfficePage reports it gone", async () => {
    localStorage.setItem(LAST_OFFICE_CHAT_KEY, "m-1892d870ded7");
    renderApp();
    await waitFor(() =>
      expect(window.location.hash).toBe("#office/chat/m-1892d870ded7"),
    );

    await act(async () => {
      officeStub.props!.onRestoredChatGone!();
    });

    expect(window.location.hash).toBe(""); // roster = canonical clean root
    // …and the dead id is not left behind to re-fire on the NEXT cold load.
    expect(localStorage.getItem(LAST_OFFICE_CHAT_KEY)).toBeNull();
  });

  it("never offers an EXPLICITLY deep-linked chat for forgetting (T-661b)", async () => {
    // Reached by hash, not by memory: a departed peer opened this way keeps its
    // read-only history. Marking it restorable would let App bounce it to the
    // roster and undo T-661b.
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/ow-353820f2c636",
    );
    renderApp();
    await screen.findByText(zh.nav.office);
    expect(officeStub.props?.restoredChatId).toBeUndefined();
  });

  it("forgets the chat once the owner closes it back to the roster", async () => {
    history.replaceState(
      null,
      "",
      window.location.pathname + "#office/chat/m-1892d870ded7",
    );
    renderApp();

    // Closing the chat (mobile 返回 / clicking 辦公室 from inside 辦公室).
    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe(""); // roster = canonical clean root

    await clickTab(zh.nav.tasks);
    await clickTab(zh.nav.office);
    expect(window.location.hash).toBe(""); // roster = canonical clean root
  });
});
