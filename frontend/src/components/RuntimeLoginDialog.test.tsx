import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
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
  loginId: "rl-1",
  machineId: "m-box",
  runtime: "claude",
  state: "starting",
  authUrl: null,
  account: null,
  reason: null,
  updatedTs: 1_800_000_000,
  ...patch,
});

const URL = "https://claude.ai/oauth/authorize?state=s1";

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

describe("RuntimeLoginDialog", () => {
  it("under a login the warden carries through, it shows preparing, then the URL and code box, then verifying, then the account", async () => {
    mount();
    expect(startRuntimeLogin).toHaveBeenCalledWith("m-box", "claude");
    await flush();
    expect(text("runtime-login-preparing")).toBe("正在請 工作站 準備登入…");

    await emit(login({ state: "awaiting_code", authUrl: URL }));
    expect(getRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-1");
    expect(text("runtime-login-awaiting")).toContain("在新分頁登入 Claude 並授權，把頁面上顯示的授權碼貼回這裡");
    const link = screen.getByTestId("runtime-login-url") as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe(URL);
    expect(link.getAttribute("target")).toBe("_blank");
    expect((screen.getByTestId("runtime-login-submit") as HTMLButtonElement).disabled).toBe(true);

    submitRuntimeLoginCode.mockResolvedValue(login({ state: "verifying", authUrl: URL }));
    fireEvent.change(screen.getByTestId("runtime-login-code"), { target: { value: "  abc#s1 " } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-submit"));
    });
    await flush();
    expect(submitRuntimeLoginCode).toHaveBeenCalledWith("m-box", "rl-1", "abc#s1");
    expect(text("runtime-login-verifying")).toBe("登入中…");

    await emit(login({ state: "succeeded", authUrl: URL, account: { email: "owner@example.test", orgName: "Example Org" } }));
    expect(text("runtime-login-succeeded")).toBe("已登入：owner@example.test（Example Org）");
    expect(screen.queryByTestId("runtime-login-restart")).toBeNull();
  });

  it("under a partial code (422), it says the code is incomplete and keeps the code box for another paste", async () => {
    mount();
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    submitRuntimeLoginCode.mockRejectedValueOnce(
      new ApiError("http 422 for POST", 422, codeForStatus(422), "code is incomplete: copy the whole code the sign-in page shows (two parts joined by '#')")
    );
    fireEvent.change(screen.getByTestId("runtime-login-code"), { target: { value: "abc" } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-submit"));
    });
    await flush();
    expect(text("runtime-login-code-refused")).toBe("授權碼不完整，請複製完整的授權碼");
    expect(screen.getByTestId("runtime-login-awaiting")).toBeTruthy();
    expect(screen.queryByTestId("runtime-login-failed")).toBeNull();

    submitRuntimeLoginCode.mockResolvedValueOnce(login({ state: "verifying", authUrl: URL }));
    fireEvent.change(screen.getByTestId("runtime-login-code"), { target: { value: "abc#s1" } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-submit"));
    });
    await flush();
    expect(submitRuntimeLoginCode).toHaveBeenLastCalledWith("m-box", "rl-1", "abc#s1");
    expect(text("runtime-login-verifying")).toBe("登入中…");
  });

  it("under a login the machine's CLI sent back to awaiting_code with a reason, it asks for the whole code again", async () => {
    mount();
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL, reason: "Invalid code. Please make sure the full code was copied." }));
    expect(text("runtime-login-code-refused")).toBe("授權碼不完整，請複製完整的授權碼");
    expect((screen.getByTestId("runtime-login-code") as HTMLInputElement).disabled).toBe(false);
  });

  it("under a 409 on the code, it shows the Chinese reason rather than the server's English", async () => {
    mount();
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    submitRuntimeLoginCode.mockRejectedValueOnce(
      new ApiError("http 409 for POST", 409, codeForStatus(409), "machine is offline; its warden cannot run a login")
    );
    fireEvent.change(screen.getByTestId("runtime-login-code"), { target: { value: "abc#s1" } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-submit"));
    });
    await flush();
    expect(text("runtime-login-failed")).toBe(
      "登入失敗：這次登入已無法接收授權碼（機器可能已離線，或登入已結束），請重新開始"
    );
  });

  it("under a machine that is already logged in, it says the account will be replaced until the login ends", async () => {
    mount({ loggedIn: true });
    await flush();
    expect(text("runtime-login-replace-hint")).toBe("完成後會換成新登入的帳號");
    await emit(login({ state: "succeeded", account: { email: "a@b.test", orgName: null } }));
    expect(text("runtime-login-succeeded")).toBe("已登入：a@b.test");
    expect(screen.queryByTestId("runtime-login-replace-hint")).toBeNull();
  });

  it("under a machine that is not logged in, there is no replacement hint", async () => {
    mount({ loggedIn: false });
    await flush();
    expect(screen.getByTestId("runtime-login-preparing")).toBeTruthy();
    expect(screen.queryByTestId("runtime-login-replace-hint")).toBeNull();
  });

  it("under a failed login, it shows the CLI's reason, and 重新開始 starts a new login", async () => {
    mount();
    await flush();
    await emit(login({ state: "failed", reason: "Login failed: Request failed with status code 400" }));
    expect(text("runtime-login-failed-summary")).toBe("授權碼錯誤或已過期，請按「重新開始」取得新的授權網址");
    expect(text("runtime-login-failed-cli")).toBe("Claude 回應：Login failed: Request failed with status code 400");

    startRuntimeLogin.mockResolvedValue(login({ loginId: "rl-2" }));
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-restart"));
    });
    await flush();
    expect(startRuntimeLogin).toHaveBeenCalledTimes(2);
    expect(cancelRuntimeLogin).not.toHaveBeenCalled();
    expect(text("runtime-login-preparing")).toBe("正在請 工作站 準備登入…");
  });

  it("under 重新開始 while a login is still in flight, the new start waits until the cancel has landed", async () => {
    mount();
    await flush();
    let landCancel: () => void = () => {};
    cancelRuntimeLogin.mockReturnValueOnce(
      new Promise((resolve) => {
        landCancel = () => resolve(login({ state: "cancelled" }));
      })
    );
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    submitRuntimeLoginCode.mockRejectedValueOnce(
      new ApiError("http 409 for POST", 409, codeForStatus(409), "x")
    );
    fireEvent.change(screen.getByTestId("runtime-login-code"), { target: { value: "abc#s1" } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-submit"));
    });
    await flush();
    startRuntimeLogin.mockResolvedValue(login({ loginId: "rl-2" }));
    await act(async () => {
      fireEvent.click(screen.getByTestId("runtime-login-restart"));
    });
    await flush();
    expect(cancelRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-1");
    expect(startRuntimeLogin).toHaveBeenCalledTimes(1);
    await act(async () => {
      landCancel();
    });
    await flush();
    expect(startRuntimeLogin).toHaveBeenCalledTimes(2);
  });

  it("under the dialog unmounting with a login in flight (page navigation), the login is cancelled once", async () => {
    const view = mount();
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    expect(cancelRuntimeLogin).not.toHaveBeenCalled();
    view.unmount();
    expect(cancelRuntimeLogin).toHaveBeenCalledTimes(1);
    expect(cancelRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-1");
  });

  it("under a close followed by the unmount it causes, the login is cancelled only once", async () => {
    let view: ReturnType<typeof mount> | null = null;
    view = mount({ onClose: () => view?.unmount() });
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    fireEvent.click(screen.getByTestId("runtime-login-close"));
    expect(cancelRuntimeLogin).toHaveBeenCalledTimes(1);
  });

  it("under a 401 from the token exchange, it also explains a wrong or expired code", async () => {
    mount();
    await flush();
    await emit(login({ state: "failed", reason: "Login failed: Request failed with status code 401" }));
    expect(text("runtime-login-failed-summary")).toBe("授權碼錯誤或已過期，請按「重新開始」取得新的授權網址");
    expect(text("runtime-login-failed-cli")).toBe("Claude 回應：Login failed: Request failed with status code 401");
  });

  it("under any other CLI failure, it heads with 登入失敗 and shows the CLI's text below", async () => {
    mount();
    await flush();
    await emit(login({ state: "failed", reason: "Login failed: Request failed with status code 500" }));
    expect(text("runtime-login-failed-summary")).toBe("登入失敗");
    expect(text("runtime-login-failed-cli")).toBe("Claude 回應：Login failed: Request failed with status code 500");
  });

  it("under the incomplete-code message, it sits outside the code row so the submit button keeps its place", async () => {
    mount();
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL, reason: "Invalid code. Please make sure the full code was copied." }));
    const error = screen.getByTestId("runtime-login-code-refused");
    const row = screen.getByTestId("runtime-login-submit").closest("form")!;
    expect(row.contains(error)).toBe(false);
    expect(row.nextElementSibling).toBe(error);
  });

  it("under an offline warden (409 on start), it says the machine did not answer and why", async () => {
    startRuntimeLogin.mockRejectedValue(
      new ApiError("http 409 for POST", 409, codeForStatus(409), "machine is offline; its warden cannot run a login")
    );
    mount();
    await flush();
    expect(text("runtime-login-no-response")).toBe(
      "工作站 沒有回應登入要求可能原因：機器離線，或這台機器上的 warden 版本太舊、還不支援從這裡登入。"
    );
    expect(screen.getByTestId("runtime-login-restart")).toBeTruthy();
  });

  it("under a login still starting after 30s, it gives up with the no-response message and cancels the login", async () => {
    vi.useFakeTimers();
    mount();
    await flush();
    await act(async () => {
      vi.advanceTimersByTime(29_999);
    });
    expect(screen.getByTestId("runtime-login-preparing")).toBeTruthy();
    expect(cancelRuntimeLogin).not.toHaveBeenCalled();
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(text("runtime-login-no-response")).toContain("工作站 沒有回應登入要求");
    expect(cancelRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-1");
  });

  it("under a login that moved past starting, the 30s give-up never fires", async () => {
    vi.useFakeTimers();
    mount();
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    await act(async () => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByTestId("runtime-login-awaiting")).toBeTruthy();
    expect(screen.queryByTestId("runtime-login-no-response")).toBeNull();
  });

  it("under a close while the login is in flight, it cancels the login and closes", async () => {
    const onClose = vi.fn();
    mount({ onClose });
    await flush();
    await emit(login({ state: "awaiting_code", authUrl: URL }));
    fireEvent.click(screen.getByTestId("runtime-login-close"));
    expect(cancelRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-1");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("under a close after the login ended, it closes without cancelling", async () => {
    const onClose = vi.fn();
    mount({ onClose });
    await flush();
    await emit(login({ state: "succeeded", account: { email: "a@b.test", orgName: null } }));
    fireEvent.click(screen.getByTestId("runtime-login-close"));
    expect(cancelRuntimeLogin).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("under Esc while the login is in flight, it cancels and closes", async () => {
    const onClose = vi.fn();
    mount({ onClose });
    await flush();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(cancelRuntimeLogin).toHaveBeenCalledWith("m-box", "rl-1");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
