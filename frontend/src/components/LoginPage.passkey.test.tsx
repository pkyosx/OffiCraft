// The login wall's passkey button: when it appears, the begin → browser →
// finish round trip, and the 127.0.0.1 pointer to localhost.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { LoginPage } from "./LoginPage";
import { TOKEN_KEY } from "../api/auth";

const jsdom = (globalThis as unknown as { jsdom: { reconfigure(o: { url: string }): void } }).jsdom;
const originalUrl = window.location.href;
const get = vi.fn();
const parseRequestOptionsFromJSON = vi.fn((json: unknown) => ({ parsed: json }));

function stubWebAuthn() {
  vi.stubGlobal("PublicKeyCredential", Object.assign(function PublicKeyCredential() {}, {
    parseCreationOptionsFromJSON: vi.fn(),
    parseRequestOptionsFromJSON,
  }));
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get },
  });
}

/** Stub the two passkey-login routes; `finish` returns [status, body]. */
function stubPasskeyRoutes(finish: () => [number, unknown]) {
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, body: init?.body ? JSON.parse(String(init.body)) : null });
      const [status, payload] = url.endsWith("/begin")
        ? [200, { ceremony_id: "plc-1", expires_in: 300, options: { challenge: "Y2hhbGxlbmdl", rpId: "localhost", userVerification: "required" } }]
        : finish();
      return new Response(JSON.stringify(payload), { status, headers: { "Content-Type": "application/json" } });
    }),
  );
  return calls;
}

function renderWall(passkeyLoginAvailable: boolean, onSuccess = vi.fn()) {
  const utils = render(
    <I18nProvider>
      <LoginPage onSuccess={onSuccess} passkeyLoginAvailable={passkeyLoginAvailable} />
    </I18nProvider>,
  );
  return { ...utils, onSuccess };
}

const BUTTON = "用通行密鑰登入";
const UNSUPPORTED = "此瀏覽器版本不支援通行密鑰登入，請更新瀏覽器";

beforeEach(() => {
  localStorage.removeItem(TOKEN_KEY);
  get.mockReset();
  parseRequestOptionsFromJSON.mockClear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "credentials");
  jsdom.reconfigure({ url: originalUrl });
  localStorage.removeItem(TOKEN_KEY);
});

describe("LoginPage — passkey sign-in", () => {
  it("shows the button only when the host allows it AND the browser has the native WebAuthn JSON methods, else says to update", () => {
    stubWebAuthn();
    expect(renderWall(false).queryByText(BUTTON)).toBeNull();
    vi.unstubAllGlobals();
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    const older = renderWall(true);
    expect(older.queryByText(BUTTON)).toBeNull();
    expect(older.getAllByText(UNSUPPORTED)).toHaveLength(1);
    older.unmount();
    vi.unstubAllGlobals();
    stubWebAuthn();
    const current = renderWall(true);
    expect(current.getAllByText(BUTTON)).toHaveLength(1);
    expect(current.queryByText(UNSUPPORTED)).toBeNull();
  });

  it("signs in with the passkey and stores the token the way a password login does", async () => {
    stubWebAuthn();
    get.mockResolvedValue({ toJSON: () => ({ id: "cred", type: "public-key", response: {} }) });
    const calls = stubPasskeyRoutes(() => [200, { token: "pk-token", token_type: "bearer", expires_in: 86400, owner_id: "owner" }]);
    const utils = renderWall(true);

    fireEvent.click(utils.getByText(BUTTON));

    await waitFor(() => expect(utils.onSuccess).toHaveBeenCalledTimes(1));
    expect(localStorage.getItem(TOKEN_KEY)).toBe("pk-token");
    expect(parseRequestOptionsFromJSON).toHaveBeenCalledWith({ challenge: "Y2hhbGxlbmdl", rpId: "localhost", userVerification: "required" });
    expect(get.mock.calls[0][0].publicKey).toEqual({
      parsed: { challenge: "Y2hhbGxlbmdl", rpId: "localhost", userVerification: "required" },
    });
    expect(calls).toEqual([
      { url: "/api/auth/passkey-login/begin", body: null },
      {
        url: "/api/auth/passkey-login/finish",
        body: { ceremony_id: "plc-1", credential: { id: "cred", type: "public-key", response: {} } },
      },
    ]);
  });

  it("a dismissed dialog is a calm hint, not a failure", async () => {
    stubWebAuthn();
    get.mockRejectedValue(new DOMException("not allowed", "NotAllowedError"));
    const calls = stubPasskeyRoutes(() => [200, {}]);
    const utils = renderWall(true);

    fireEvent.click(utils.getByText(BUTTON));

    await utils.findByText("已取消通行密鑰登入，可以再試一次或改用密碼。");
    expect(calls.map((c) => c.url)).toEqual(["/api/auth/passkey-login/begin"]);
    expect(utils.onSuccess).not.toHaveBeenCalled();
  });

  it("a refused passkey says sign-in failed and stores nothing", async () => {
    stubWebAuthn();
    get.mockResolvedValue({ toJSON: () => ({ id: "cred" }) });
    stubPasskeyRoutes(() => [401, { error: { code: "unauthorized", message: "passkey sign-in failed" } }]);
    const utils = renderWall(true);

    fireEvent.click(utils.getByText(BUTTON));

    await utils.findByText("通行密鑰登入失敗，請再試一次或改用密碼。");
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(utils.onSuccess).not.toHaveBeenCalled();
  });

  it("a credential without toJSON is named as the reason and nothing is sent", async () => {
    stubWebAuthn();
    get.mockResolvedValue({ id: "cred", type: "public-key", response: {} });
    const calls = stubPasskeyRoutes(() => [200, {}]);
    const utils = renderWall(true);

    fireEvent.click(utils.getByText(BUTTON));

    await utils.findByText("這個瀏覽器或擴充功能無法使用通行密鑰，請更新後再試，或改用密碼登入。");
    expect(calls.map((c) => c.url)).toEqual(["/api/auth/passkey-login/begin"]);
    expect(utils.onSuccess).not.toHaveBeenCalled();
  });

  it("on 127.0.0.1 points to localhost instead of offering the button", () => {
    stubWebAuthn();
    jsdom.reconfigure({ url: "http://127.0.0.1:8787/" });
    const utils = renderWall(false);

    expect(utils.getByText("要用通行密鑰，請改用 http://localhost:8787 開啟")).toBeTruthy();
    expect(utils.queryByText(BUTTON)).toBeNull();
  });

  it("on localhost with passkeys unavailable shows no hint", () => {
    stubWebAuthn();
    jsdom.reconfigure({ url: "http://localhost:8787/" });
    expect(renderWall(false).container.textContent).not.toContain("通行密鑰");
  });
});
