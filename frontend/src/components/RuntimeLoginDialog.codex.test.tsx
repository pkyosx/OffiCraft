import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import type { RuntimeLoginView } from "../types";
import { RuntimeLoginDialog } from "./RuntimeLoginDialog";

const startRuntimeLogin = vi.fn();
const getRuntimeLogin = vi.fn();
const submitRuntimeLoginCode = vi.fn();
const cancelRuntimeLogin = vi.fn();
let topicHandler: ((topic: string) => void) | null = null;

vi.mock("../api", () => ({
  api: {
    startRuntimeLogin: (...a: unknown[]) => startRuntimeLogin(...a),
    getRuntimeLogin: (...a: unknown[]) => getRuntimeLogin(...a),
    submitRuntimeLoginCode: (...a: unknown[]) => submitRuntimeLoginCode(...a),
    cancelRuntimeLogin: (...a: unknown[]) => cancelRuntimeLogin(...a),
    subscribeEvents: (cb: (topic: string) => void) => {
      topicHandler = cb;
      return () => {
        topicHandler = null;
      };
    },
  },
}));

const login = (patch: Partial<RuntimeLoginView>): RuntimeLoginView => ({
  loginId: "rl-c",
  machineId: "m-box",
  runtime: "codex",
  state: "starting",
  authUrl: null,
  userCode: null,
  expiresTs: null,
  account: null,
  reason: null,
  updatedTs: 1_800_000_000,
  ...patch,
});

const URL = "https://auth.openai.com/codex/device";

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function emit(next: RuntimeLoginView) {
  getRuntimeLogin.mockResolvedValueOnce(next);
  await act(async () => {
    topicHandler?.("runtime_login");
  });
  await flush();
}

function mount(opts: { loggedIn?: boolean; onClose?: () => void } = {}) {
  return render(
    <I18nProvider>
      <RuntimeLoginDialog
        machineId="m-box"
        machineName="工作站"
        runtime="codex"
        loggedIn={opts.loggedIn ?? false}
        onClose={opts.onClose ?? (() => {})}
      />
    </I18nProvider>
  );
}

const text = (id: string) => screen.getByTestId(id).textContent;

beforeEach(() => {
  startRuntimeLogin.mockReset().mockResolvedValue(login({}));
  getRuntimeLogin.mockReset();
  submitRuntimeLoginCode.mockReset();
  cancelRuntimeLogin.mockReset().mockResolvedValue(login({ state: "cancelled" }));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("RuntimeLoginDialog (codex)", () => {
  it("under a device-code login, it shows the URL, the one-time code with copy, a countdown and Codex's warning, then the account", async () => {
    vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval", "setTimeout", "clearTimeout"] });
    vi.setSystemTime(new Date(1_800_000_000_000));
    mount();
    expect(startRuntimeLogin).toHaveBeenCalledWith("m-box", "codex");
    await flush();
    expect(screen.getByRole("dialog").getAttribute("aria-label")).toBe("登入 Codex");
    expect(text("runtime-login-preparing")).toBe("正在請 工作站 準備登入…");

    await emit(login({ state: "awaiting_authorization", authUrl: URL, userCode: "ABCD-EFGHI", expiresTs: 1_800_000_900 }));
    const link = screen.getByTestId("runtime-login-url") as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe(URL);
    expect(text("runtime-login-user-code")).toBe("ABCD-EFGHI");
    expect(text("runtime-login-remaining")).toBe("剩餘有效時間 15:00");
    expect(text("runtime-login-phishing")).toBe(
      "只有在你自己按下登入時才輸入這組碼；如果是網站或別人給你的碼，請關閉"
    );
    expect(screen.getByTestId("runtime-login-authorize").textContent).toContain(
      "在新分頁開啟網址、登入 ChatGPT 帳號，輸入下面的一次性碼並授權；完成後這裡會自動更新"
    );
    expect(screen.queryByTestId("runtime-login-code")).toBeNull();
    await act(async () => {
      vi.advanceTimersByTime(61_000);
    });
    expect(text("runtime-login-remaining")).toBe("剩餘有效時間 13:59");

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-copy-code"));
    });
    expect(writeText).toHaveBeenCalledWith("ABCD-EFGHI");
    expect(text("runtime-login-copy-code")).toBe("已複製");

    await emit(login({ state: "succeeded", account: { email: "owner@example.test", orgName: null, plan: "team" } }));
    expect(text("runtime-login-succeeded")).toBe("已登入：owner@example.test（team）");
  });

  it("under a codex account with no plan, it shows the email alone", async () => {
    mount();
    await flush();
    await emit(login({ state: "succeeded", account: { email: "owner@example.test", orgName: "ignored", plan: null } }));
    expect(text("runtime-login-succeeded")).toBe("已登入：owner@example.test");
  });

  it("under awaiting authorization for longer than 30s, the no-response give-up never fires", async () => {
    vi.useFakeTimers();
    mount();
    await flush();
    await emit(login({ state: "awaiting_authorization", authUrl: URL, userCode: "ABCD-EFGHI", expiresTs: null }));
    await act(async () => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByTestId("runtime-login-authorize")).toBeTruthy();
    expect(screen.queryByTestId("runtime-login-remaining")).toBeNull();
    expect(screen.queryByTestId("runtime-login-no-response")).toBeNull();
  });

  it("under a failed login, it heads with 登入失敗 and shows Codex's text below", async () => {
    mount();
    await flush();
    await emit(login({ state: "failed", reason: "Codex is not enabled for your workspace." }));
    expect(text("runtime-login-failed-summary")).toBe("登入失敗");
    expect(text("runtime-login-failed-cli")).toBe("Codex 回應：Codex is not enabled for your workspace.");
  });

  it("under a warden that can only log in claude, it says this machine's OffiCraft cannot sign in to Codex", async () => {
    mount();
    await flush();
    await emit(login({ state: "failed", reason: 'this warden cannot log in runtime "codex"' }));
    expect(text("runtime-login-failed-summary")).toBe("這台機器的 OffiCraft 版本不支援 Codex 登入");
  });

  it("under a 400 from codex, it does not borrow claude's wrong-code explanation", async () => {
    mount();
    await flush();
    await emit(login({ state: "failed", reason: "Login failed: Request failed with status code 400" }));
    expect(text("runtime-login-failed-summary")).toBe("登入失敗");
  });

  it("under an expired one-time code, it says so and offers 重新開始", async () => {
    mount();
    await flush();
    await emit(login({ state: "expired", reason: "the one-time code was not approved within 16m0s" }));
    expect(text("runtime-login-failed")).toBe("一次性碼已過期，請按「重新開始」");
    expect(screen.getByTestId("runtime-login-restart")).toBeTruthy();
  });

  it("under a machine already logged in to Codex, it says the account will be replaced", async () => {
    mount({ loggedIn: true });
    await flush();
    expect(text("runtime-login-replace-hint")).toBe("完成後會換成新登入的帳號");
  });

  it("under a close or an unmount while awaiting authorization, the login is cancelled once", async () => {
    const view = mount();
    await flush();
    await emit(login({ state: "awaiting_authorization", authUrl: URL, userCode: "ABCD-EFGHI", expiresTs: null }));
    view.unmount();
    expect(cancelRuntimeLogin).toHaveBeenCalledTimes(1);
    expect(cancelRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-c");
  });
});
