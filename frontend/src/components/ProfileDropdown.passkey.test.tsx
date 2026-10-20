// ProfileDropdown → 帳號安全 → 通行密鑰, against the mock adapter, with the
// browser's WebAuthn surface stubbed (jsdom has none).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ProfileDropdown } from "./ProfileDropdown";
import { __injectMockPasskey, __resetMock } from "../api/mock";
import { api } from "../api";
import { clearToken } from "../api/auth";

const MOCK_PASSWORD = "mock-password";
const jsdom = (globalThis as unknown as { jsdom: { reconfigure(o: { url: string }): void } }).jsdom;
const originalUrl = window.location.href;
const YEAR = new Date().getFullYear();
const at = (mo: number, d: number, h: number, mi: number) => new Date(YEAR, mo - 1, d, h, mi).getTime() / 1000;

const create = vi.fn();
const parseCreationOptionsFromJSON = vi.fn((json: unknown) => ({ parsed: json }));

function stubWebAuthn() {
  vi.stubGlobal("PublicKeyCredential", Object.assign(function PublicKeyCredential() {}, {
    parseCreationOptionsFromJSON,
    parseRequestOptionsFromJSON: vi.fn(),
  }));
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: { create, get: vi.fn() },
  });
}

async function openPasskeys() {
  const utils = render(
    <I18nProvider>
      <ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByText("帳號安全"));
  fireEvent.click(utils.getByText("通行密鑰"));
  return utils;
}

function rowTexts(root: HTMLElement): string[] {
  return Array.from(root.querySelectorAll(".profile-dd__pk")).map((li) =>
    Array.from(li.children).map((c) => c.textContent ?? ""),
  ).map((parts) => parts.join(" | "));
}

function fillAndSubmit(utils: ReturnType<typeof render>, password: string, name: string) {
  fireEvent.change(utils.getByLabelText("目前密碼"), { target: { value: password } });
  fireEvent.change(utils.getByLabelText("裝置名稱（例如 Eva 的 MacBook）"), { target: { value: name } });
  fireEvent.click(utils.getByRole("button", { name: "新增通行密鑰" }));
}

beforeEach(() => {
  __resetMock();
  clearToken();
  localStorage.clear();
  create.mockReset();
  parseCreationOptionsFromJSON.mockClear();
  stubWebAuthn();
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "credentials");
  jsdom.reconfigure({ url: originalUrl });
});

describe("ProfileDropdown · passkeys", () => {
  it("lists each passkey's name, bound host, times and its warnings", async () => {
    __injectMockPasskey({
      id: "pk-000000000001", name: "Eva 的 MacBook", rp_id: "localhost", origin: "http://localhost:3000",
      created_ts: at(1, 2, 9, 5), last_used_ts: null, usable_here: true, clone_warning_ts: null,
    });
    __injectMockPasskey({
      id: "pk-000000000002", name: "YubiKey", rp_id: "office.example.com", origin: "https://office.example.com",
      created_ts: at(3, 4, 10, 0), last_used_ts: at(5, 6, 18, 30), usable_here: false, clone_warning_ts: at(5, 7, 8, 0),
    });
    const utils = await openPasskeys();
    await utils.findByText("YubiKey");

    expect(rowTexts(utils.container)).toEqual([
      "Eva 的 MacBook | 綁定網址 localhost | 建立於 1/2 09:05 | 最後使用 尚未使用 | 刪除",
      "YubiKey | 綁定網址 office.example.com | 建立於 3/4 10:00 | 最後使用 5/6 18:30 | 此網址無法使用 | 可能被複製，建議刪除 | 刪除",
    ]);
  });

  it("adds a passkey: password → browser dialog → it appears in the list", async () => {
    create.mockResolvedValue({ toJSON: () => ({ id: "cred", type: "public-key", response: {} }) });
    const utils = await openPasskeys();
    await utils.findByText("還沒有註冊任何通行密鑰");

    fillAndSubmit(utils, MOCK_PASSWORD, "Eva 的 iPhone");
    await utils.findByText("通行密鑰已新增");

    const options = parseCreationOptionsFromJSON.mock.calls[0][0] as { challenge: string; user: { id: string } };
    expect([options.challenge, options.user.id]).toEqual(["bW9jay1jaGFsbGVuZ2U", "b3duZXI"]);
    expect(create.mock.calls[0][0].publicKey).toEqual({ parsed: options });
    expect(rowTexts(utils.container)).toHaveLength(1);
    expect(rowTexts(utils.container)[0]).toMatch(/^Eva 的 iPhone \| 綁定網址 localhost \| 建立於 /);
    expect((await api.listPasskeys()).map((k) => k.name)).toEqual(["Eva 的 iPhone"]);
  });

  it("a dismissed browser dialog adds nothing and says so calmly", async () => {
    create.mockRejectedValue(new DOMException("The operation either timed out or was not allowed.", "NotAllowedError"));
    const utils = await openPasskeys();
    fillAndSubmit(utils, MOCK_PASSWORD, "Eva 的 iPhone");

    await utils.findByText("已取消，沒有新增通行密鑰。需要時可以再試一次。");
    expect(await api.listPasskeys()).toEqual([]);
  });

  it("a credential without toJSON is named as the reason and adds nothing", async () => {
    create.mockResolvedValue({ id: "cred", type: "public-key", response: {} });
    const utils = await openPasskeys();
    fillAndSubmit(utils, MOCK_PASSWORD, "Eva 的 iPhone");

    await utils.findByText("這個瀏覽器或擴充功能無法建立通行密鑰，請更新後再試。");
    expect(await api.listPasskeys()).toEqual([]);
  });

  it("a browser without the native WebAuthn JSON methods is told to update and offered no add form", async () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    const utils = await openPasskeys();

    await utils.findByText("此瀏覽器版本不支援通行密鑰，請更新瀏覽器");
    expect(utils.queryByRole("button", { name: "新增通行密鑰" })).toBeNull();
  });

  it("a wrong current password never reaches the browser dialog", async () => {
    const utils = await openPasskeys();
    fillAndSubmit(utils, "wrong-password", "Eva 的 iPhone");

    await utils.findByText("目前密碼不對");
    expect(create).not.toHaveBeenCalled();
  });

  it("deletes a passkey after the confirmation", async () => {
    __injectMockPasskey({
      id: "pk-000000000001", name: "舊筆電", rp_id: "localhost", origin: "http://localhost:3000",
      created_ts: at(1, 2, 9, 5), last_used_ts: null, usable_here: true, clone_warning_ts: null,
    });
    const utils = await openPasskeys();
    fireEvent.click(within(await utils.findByTestId("passkey-pk-000000000001")).getByText("刪除"));

    const modal = utils.getByTestId("passkey-delete-confirm");
    expect(modal.querySelector(".confirm-modal__body")?.textContent).toBe(
      "刪除這把通行密鑰？刪除後就不能再用它登入。已經用它登入的分頁會維持登入，直到登入效期到期或改密碼。",
    );
    fireEvent.click(within(modal).getByText("刪除"));

    await utils.findByText("還沒有註冊任何通行密鑰");
    expect(utils.queryByTestId("passkey-delete-confirm")).toBeNull();
    expect(await api.listPasskeys()).toEqual([]);
  });

  it("on 127.0.0.1 points to localhost and offers no add form", async () => {
    jsdom.reconfigure({ url: "http://127.0.0.1:8787/" });
    const utils = await openPasskeys();

    await utils.findByText("要用通行密鑰，請改用 http://localhost:8787 開啟");
    expect(utils.queryByRole("button", { name: "新增通行密鑰" })).toBeNull();
  });
});
