import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import type { SseConnectionState } from "../api/adapter";
import { resetChatDrafts, saveChatDraftText } from "../lib/chatDraftStore";
import { STALE_BUILD_RELOADED_KEY } from "../lib/staleBuild";

const subscribers = new Set<(s: SseConnectionState) => void>();
let serverSha = "aaa1111";

vi.mock("../api", () => ({
  api: {
    getVersion: () => Promise.resolve({ gitSha: serverSha }),
    subscribeConnection(cb: (s: SseConnectionState) => void) {
      subscribers.add(cb);
      cb("live");
      return () => {
        subscribers.delete(cb);
      };
    },
  },
}));

let writing = false;
vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  hasWritesInFlight: () => writing,
}));

import { BuildUpdateBanner } from "./BuildUpdateBanner";

const reload = vi.fn();
const originalLocation = window.location;
let focused = true;

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function mount() {
  const view = render(
    <StrictMode>
      <I18nProvider>
        <BuildUpdateBanner />
      </I18nProvider>
    </StrictMode>
  );
  await flush();
  return view;
}

async function serverUpdatedTo(sha: string) {
  serverSha = sha;
  act(() => {
    for (const cb of [...subscribers]) cb("connecting");
  });
  act(() => {
    for (const cb of [...subscribers]) cb("live");
  });
  await flush();
}

beforeEach(() => {
  serverSha = "aaa1111";
  focused = true;
  writing = false;
  subscribers.clear();
  resetChatDrafts();
  window.sessionStorage.clear();
  reload.mockClear();
  Object.defineProperty(window, "location", { configurable: true, value: { ...originalLocation, reload } });
  vi.spyOn(document, "hasFocus").mockImplementation(() => focused);
});

afterEach(() => {
  Object.defineProperty(window, "location", { configurable: true, value: originalLocation });
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("BuildUpdateBanner", () => {
  it("shows nothing while the server runs the page's own build", async () => {
    const { container } = await mount();
    await serverUpdatedTo("aaa1111");
    expect(container.querySelector(".status-banner")).toBe(null);
  });

  it("asks the person looking at the page to reload once the server runs a newer build, and reloads on click", async () => {
    const { container, getByRole } = await mount();
    await serverUpdatedTo("bbb2222");

    const bar = container.querySelector(".status-banner");
    expect(bar?.getAttribute("aria-label")).toBe("控制台版本狀態");
    expect(bar?.textContent).toBe("控制台有新版本站台已更新，重新整理後才會套用。重新整理");
    expect(reload).not.toHaveBeenCalled();

    fireEvent.click(getByRole("button", { name: "重新整理" }));
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("reloads a page whose window is not focused without showing anything first", async () => {
    focused = false;
    await mount();
    await serverUpdatedTo("bbb2222");
    expect(reload).toHaveBeenCalledTimes(1);
    expect(window.sessionStorage.getItem(STALE_BUILD_RELOADED_KEY)).toBe("bbb2222");
  });

  it("keeps an unsent chat draft by not reloading on its own", async () => {
    focused = false;
    saveChatDraftText("m-aaa", "還沒送出");
    const { container } = await mount();
    await serverUpdatedTo("bbb2222");

    expect(reload).not.toHaveBeenCalled();
    expect(container.querySelector(".status-banner")).not.toBe(null);
  });

  it("keeps a text field typed into by not reloading on its own", async () => {
    focused = false;
    await mount();
    const field = document.createElement("textarea");
    document.body.append(field);
    field.focus();
    field.value = "編輯中";
    await serverUpdatedTo("bbb2222");

    expect(reload).not.toHaveBeenCalled();
  });

  it("does not reload while a sent message is still on its way", async () => {
    focused = false;
    writing = true;
    await mount();
    await serverUpdatedTo("bbb2222");
    expect(reload).not.toHaveBeenCalled();
  });
});
