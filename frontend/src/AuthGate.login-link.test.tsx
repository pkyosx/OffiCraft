import { StrictMode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";

// Same reason as AuthGate.mfa.test.tsx: USE_MOCK is read at module-evaluation
// time, so the switch has to be flipped with the hoisted vi.mock.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  USE_MOCK: false,
}));

import { AuthGate } from "./AuthGate";
import { api } from "./api";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";
import { TOKEN_KEY } from "./api/auth";

const BEHIND_THE_WALL = "工作室內容";

// StrictMode as in main.tsx: its doubled effect run must not spend the
// single-use code twice.
function renderGate() {
  return render(
    <StrictMode>
      <I18nProvider>
        <AuthGate authed={<div>{BEHIND_THE_WALL}</div>} />
      </I18nProvider>
    </StrictMode>,
  );
}

function stubLoginLinkAnswer(status: number, body: unknown, headers: Record<string, string> = {}) {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json", ...headers },
      }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function stubProbe() {
  return vi
    .spyOn(api, "getAuthStatus")
    .mockResolvedValue({ passwordSet: true, mfaRequired: false });
}

beforeEach(() => {
  localStorage.clear();
  history.replaceState(null, "", "/?login=link-code-abc&tab=chat");
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
  history.replaceState(null, "", "/");
});

describe("AuthGate", () => {
  it("logs in through a valid login link, stores the token, scrubs the link, and never reuses it", async () => {
    const fetchMock = stubLoginLinkAnswer(200, {
      token: "minted-owner-token",
      token_type: "bearer",
      expires_in: 86400,
      owner_id: "owner",
    });

    renderGate();

    expect(await screen.findByText(BEHIND_THE_WALL)).toBeTruthy();
    expect(localStorage.getItem(TOKEN_KEY)).toBe("minted-owner-token");
    expect(window.location.search).toBe("?tab=chat");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/login-link", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: "link-code-abc" }),
    });

    stubProbe();
    localStorage.removeItem(TOKEN_KEY);
    act(() => {
      window.dispatchEvent(new Event("oc-auth-expired"));
    });

    expect(await screen.findByPlaceholderText(zh.login.passwordPlaceholder)).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("shows the login wall with the expired-link notice when the link is refused", async () => {
    stubLoginLinkAnswer(401, {
      error: { code: "unauthorized", message: "login link is invalid, expired, or already used" },
    });
    stubProbe();

    renderGate();

    expect(
      await screen.findByText(
        "登入連結已失效（已使用、超過 10 分鐘或站台未開啟），請用密碼登入或請人重新產生。",
      ),
    ).toBeTruthy();
    expect(screen.getByPlaceholderText(zh.login.passwordPlaceholder)).toBeTruthy();
    expect(screen.queryByText(BEHIND_THE_WALL)).toBeNull();
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(window.location.search).toBe("?tab=chat");
  });

  it("shows the throttle message instead of the notice when the link attempt is braked", async () => {
    stubLoginLinkAnswer(
      429,
      { error: { code: "client_error", message: "too many failed credential attempts; retry in 1s" } },
      { "Retry-After": "1" },
    );
    stubProbe();

    renderGate();

    expect(await screen.findByText("目前同時處理的登入太多，請於 1 秒後再試。")).toBeTruthy();
    expect(screen.queryByText(zh.login.loginLinkInvalid)).toBeNull();
  });

  it("spends the link over an existing session and replaces its token", async () => {
    localStorage.setItem(TOKEN_KEY, "existing-owner-token");
    const fetchMock = stubLoginLinkAnswer(200, {
      token: "minted-owner-token",
      token_type: "bearer",
      expires_in: 86400,
      owner_id: "owner",
    });

    renderGate();

    expect(await screen.findByText(BEHIND_THE_WALL)).toBeTruthy();
    expect(localStorage.getItem(TOKEN_KEY)).toBe("minted-owner-token");
    expect(window.location.search).toBe("?tab=chat");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/login-link", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code: "link-code-abc" }),
    });
  });

  it.each<{ answer: string; status: number; body: unknown; headers: Record<string, string> }>([
    {
      answer: "refused",
      status: 401,
      body: { error: { code: "unauthorized", message: "login link is invalid, expired, or already used" } },
      headers: {},
    },
    {
      answer: "braked",
      status: 429,
      body: { error: { code: "client_error", message: "too many failed credential attempts; retry in 1s" } },
      headers: { "Retry-After": "1" },
    },
  ])("keeps an existing session and its token when the link is $answer", async ({ status, body, headers }) => {
    localStorage.setItem(TOKEN_KEY, "existing-owner-token");
    const fetchMock = stubLoginLinkAnswer(status, body, headers);
    const probe = stubProbe();

    renderGate();

    expect(await screen.findByText(BEHIND_THE_WALL)).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(TOKEN_KEY)).toBe("existing-owner-token");
    expect(window.location.search).toBe("?tab=chat");
    expect(probe).not.toHaveBeenCalled();
    expect(
      screen.queryByText(
        "登入連結已失效（已使用、超過 10 分鐘或站台未開啟），請用密碼登入或請人重新產生。",
      ),
    ).toBeNull();
    expect(screen.queryByText("目前同時處理的登入太多，請於 1 秒後再試。")).toBeNull();
  });
});
