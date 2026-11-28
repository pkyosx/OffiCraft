import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SecretsPage } from "./SecretsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { formatAbsolute } from "../lib/dateFormat";

const copyTextMock = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../lib/clipboard", () => ({ copyText: copyTextMock }));

const FAKE_VALUE = "fake-value-1";

function renderPage() {
  return render(
    <I18nProvider>
      <SecretsPage />
    </I18nProvider>,
  );
}

async function firstRow(container: HTMLElement): Promise<HTMLElement> {
  return waitFor(() => {
    const row = container.querySelector<HTMLElement>('[data-testid="secret-row"]');
    if (!row) throw new Error("no secret row yet");
    return row;
  });
}

function valueAppearsAnywhere(container: HTMLElement): boolean {
  if (container.textContent?.includes(FAKE_VALUE)) return true;
  return Array.from(container.querySelectorAll("input, textarea")).some(
    (el) => (el as HTMLInputElement).value.includes(FAKE_VALUE),
  );
}

beforeEach(() => {
  __resetMock();
  copyTextMock.mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SecretsPage", () => {
  it("each secret card shows id, name, note, last access, updated time and the edit / delete icons without any interaction", async () => {
    const { container } = renderPage();
    const row = await firstRow(container);
    const now = Math.floor(Date.now() / 1000);
    expect(row.getAttribute("data-secret-id")).toBe("sec-00000000000a");
    expect(row.getAttribute("role")).toBeNull();
    expect(row.hasAttribute("aria-expanded")).toBe(false);
    expect(row.querySelector('[data-testid="secret-id"]')?.textContent).toContain(
      "sec-00000000000a",
    );
    expect(row.querySelector('[data-testid="secret-name"]')?.textContent).toBe(
      "GitHub deploy token",
    );
    expect(row.querySelector('[data-testid="secret-note"]')?.textContent).toBe(
      "Only for pushing release tags.",
    );
    expect(row.querySelector('[data-testid="secret-last-accessed"]')?.textContent).toBe(
      zh.secrets.colLastAccessed + formatAbsolute(1760000600, now),
    );
    expect(row.querySelector('[data-testid="secret-updated"]')?.textContent).toBe(
      zh.secrets.colUpdated + formatAbsolute(1760000000, now),
    );
    for (const [testId, label] of [
      ["secret-edit", zh.secrets.edit],
      ["secret-delete", zh.secrets.delete],
    ]) {
      const icon = row.querySelector<HTMLElement>(`[data-testid="${testId}"]`)!;
      expect(icon.textContent).toBe("");
      expect(icon.getAttribute("aria-label")).toBe(`${label} GitHub deploy token`);
      expect(icon.getAttribute("title")).toBe(label);
    }
    expect(container.querySelector("h2")).toBeNull();
  });

  it("a note taller than three lines is clamped and the note itself opens and closes it on that card only, by click, Enter or Space, but not by the click that ends a drag-selection; a note that fits is plain text", async () => {
    const longNote = "Rotate quarterly. ".repeat(20).trim();
    await api.createSecret({ name: "Staging DB", value: FAKE_VALUE, note: longNote });
    // jsdom lays nothing out: a clamped note body reports one height unit per
    // character against a three-line box of 40.
    vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(function (this: HTMLElement) {
      return this.dataset.testid === "secret-note-text-body" ? (this.textContent ?? "").length : 0;
    });
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
      return this.dataset.testid === "secret-note-text-body" ? 40 : 0;
    });
    const { container } = renderPage();
    await waitFor(() =>
      expect(container.querySelectorAll('[data-testid="secret-row"]')).toHaveLength(2),
    );
    const [short, long] = Array.from(container.querySelectorAll<HTMLElement>('[data-testid="secret-row"]'));

    const shortBody = short.querySelector<HTMLElement>('[data-testid="secret-note-text-body"]')!;
    expect(shortBody.getAttribute("role")).toBeNull();
    expect(shortBody.hasAttribute("tabindex")).toBe(false);
    expect(shortBody.classList.contains("clamped-text__body--faded")).toBe(false);

    const body = long.querySelector<HTMLElement>('[data-testid="secret-note-text-body"]')!;
    expect(container.querySelector('[data-testid="secret-note-text-toggle"]')).toBeNull();
    expect(body.textContent).toBe(longNote);
    expect([body.getAttribute("role"), body.getAttribute("tabindex"), body.getAttribute("aria-expanded"), body.title]).toEqual([
      "button",
      "0",
      "false",
      zh.common.textShowAll,
    ]);
    expect(body.classList.contains("clamped-text__body--clamped")).toBe(true);
    expect(body.classList.contains("clamped-text__body--faded")).toBe(true);

    fireEvent.click(body);
    expect([body.getAttribute("aria-expanded"), body.title]).toEqual(["true", zh.common.textShowLess]);
    expect(body.classList.contains("clamped-text__body--clamped")).toBe(false);
    expect(body.classList.contains("clamped-text__body--faded")).toBe(false);
    expect(shortBody.classList.contains("clamped-text__body--clamped")).toBe(true);
    expect(screen.queryByTestId("secret-edit-form")).toBeNull();
    expect(screen.queryByTestId("secrets-delete-confirm")).toBeNull();

    fireEvent.click(body);
    expect(body.getAttribute("aria-expanded")).toBe("false");
    expect(body.classList.contains("clamped-text__body--faded")).toBe(true);

    fireEvent.keyDown(body, { key: "Enter" });
    expect(body.getAttribute("aria-expanded")).toBe("true");
    fireEvent.keyDown(body, { key: " " });
    expect(body.getAttribute("aria-expanded")).toBe("false");

    const range = document.createRange();
    range.selectNodeContents(body);
    window.getSelection()!.addRange(range);
    fireEvent.click(body);
    expect(body.getAttribute("aria-expanded")).toBe("false");
    window.getSelection()!.removeAllRanges();
  });

  it("clicking the id copies it and confirms only after the copy succeeded", async () => {
    const { container } = renderPage();
    const row = await firstRow(container);
    expect(
      Array.from(row.querySelector('[data-testid="secret-id"] > svg')!.children).map((el) => el.getAttribute("d") ?? el.tagName),
      "the copy icon leads the id",
    ).toEqual(["rect", "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"]);
    fireEvent.click(row.querySelector('[data-testid="secret-id"]')!);
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledWith("sec-00000000000a"));
    expect(await screen.findByTestId("secret-id-copied")).toBeTruthy();
  });

  it("the toolbar shows no count while the list loads, and none beside the error when it cannot load", async () => {
    let fail: (e: Error) => void = () => {};
    vi.spyOn(api, "listSecrets").mockReturnValue(new Promise((_, reject) => (fail = reject)));
    renderPage();
    expect(screen.getByTestId("secrets-count").textContent).toBe("");
    fail(new Error("boom"));
    await screen.findByText("讀不到密鑰，請稍後再試");
    expect(screen.getByTestId("secrets-count").textContent).toBe("");
  });

  it("the toolbar counts the secrets left of ＋ 新增; creating sends name, value and note, then shows the new row without its value and counts it", async () => {
    const createSpy = vi.spyOn(api, "createSecret");
    const { container } = renderPage();
    await firstRow(container);
    const toolbar = screen.getByTestId("secrets-count").parentElement!;
    expect(toolbar.className).toBe("page-toolbar");
    expect(toolbar.parentElement!.firstElementChild).toBe(toolbar);
    expect([...toolbar.children].map((el) => el.textContent)).toEqual(["1 把密鑰", "+新增"]);
    expect(toolbar.lastElementChild!.getAttribute("data-testid")).toBe("secrets-add");

    fireEvent.click(screen.getByTestId("secrets-add"));
    expect((screen.getByTestId("secrets-add") as HTMLButtonElement).disabled).toBe(true);
    const valueInput = screen.getByTestId("secret-value-input") as HTMLTextAreaElement;
    expect(valueInput.tagName).toBe("TEXTAREA");
    fireEvent.change(screen.getByTestId("secret-name-input"), {
      target: { value: "  Staging DB  " },
    });
    fireEvent.change(valueInput, { target: { value: FAKE_VALUE } });
    fireEvent.change(screen.getByTestId("secret-note-input"), {
      target: { value: "read-only" },
    });
    fireEvent.click(screen.getByTestId("secret-save"));

    await waitFor(() =>
      expect(createSpy).toHaveBeenCalledWith({
        name: "Staging DB",
        value: FAKE_VALUE,
        note: "read-only",
      }),
    );
    await waitFor(() =>
      expect(container.querySelectorAll('[data-testid="secret-row"]')).toHaveLength(2),
    );
    expect(screen.queryByTestId("secret-create-form")).toBeNull();
    expect(valueAppearsAnywhere(container)).toBe(false);
    const created = container.querySelectorAll<HTMLElement>('[data-testid="secret-row"]')[1];
    expect(created.querySelector('[data-testid="secret-last-accessed"]')?.textContent).toBe(
      "最後存取-",
    );
    expect(created.querySelector('[data-testid="secret-note"]')?.textContent).toContain(
      "read-only",
    );
    expect(screen.getByTestId("secrets-count").textContent).toBe("2 把密鑰");
    expect((screen.getByTestId("secrets-add") as HTMLButtonElement).disabled).toBe(false);
  });

  it("a multi-line value is sent byte for byte on create and on edit", async () => {
    const multiLine = "-----BEGIN FAKE KEY-----\nfake-line-1\nfake-line-2\n-----END FAKE KEY-----\n";
    const createSpy = vi.spyOn(api, "createSecret");
    const updateSpy = vi.spyOn(api, "updateSecret");
    const { container } = renderPage();
    const row = await firstRow(container);

    fireEvent.click(screen.getByTestId("secrets-add"));
    fireEvent.change(screen.getByTestId("secret-name-input"), {
      target: { value: "Deploy key" },
    });
    fireEvent.change(screen.getByTestId("secret-value-input"), {
      target: { value: multiLine },
    });
    fireEvent.click(screen.getByTestId("secret-save"));
    await waitFor(() =>
      expect(createSpy).toHaveBeenCalledWith({ name: "Deploy key", value: multiLine, note: "" }),
    );

    fireEvent.click(row.querySelector('[data-testid="secret-edit"]')!);
    fireEvent.change(screen.getByTestId("secret-value-input"), {
      target: { value: multiLine },
    });
    fireEvent.click(screen.getByTestId("secret-save"));
    await waitFor(() =>
      expect(updateSpy).toHaveBeenCalledWith("sec-00000000000a", { value: multiLine }),
    );
    expect(container.textContent).not.toContain("fake-line-1");
  });

  it("a failed create keeps the form open with the server's reason and drops the value", async () => {
    const reason = "value is over the 65536-char limit";
    vi.spyOn(api, "createSecret").mockRejectedValue(
      new ApiError("http 400 for POST /api/secrets", 400, "validation_error", reason),
    );
    const { container } = renderPage();
    await firstRow(container);

    fireEvent.click(screen.getByTestId("secrets-add"));
    fireEvent.change(screen.getByTestId("secret-name-input"), {
      target: { value: "Staging DB" },
    });
    fireEvent.change(screen.getByTestId("secret-value-input"), {
      target: { value: FAKE_VALUE },
    });
    fireEvent.click(screen.getByTestId("secret-save"));

    expect((await screen.findByTestId("secret-form-error")).textContent).toBe(reason);
    expect(valueAppearsAnywhere(container)).toBe(false);
  });

  it("after a failed value change, saving again needs the value re-entered instead of closing with the old value kept", async () => {
    const updateSpy = vi
      .spyOn(api, "updateSecret")
      .mockRejectedValueOnce(new Error("boom"));
    const { container } = renderPage();
    const row = await firstRow(container);

    fireEvent.click(row.querySelector('[data-testid="secret-edit"]')!);
    fireEvent.change(screen.getByTestId("secret-value-input"), {
      target: { value: FAKE_VALUE },
    });
    fireEvent.click(screen.getByTestId("secret-save"));
    expect((await screen.findByTestId("secret-form-error")).textContent).toBe(
      zh.secrets.loadFailed,
    );
    expect(valueAppearsAnywhere(container)).toBe(false);

    const save = screen.getByTestId("secret-save") as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect((screen.getByTestId("secret-value-input") as HTMLTextAreaElement).placeholder).not.toBe(
      zh.secrets.valueKeepPlaceholder,
    );
    fireEvent.submit(screen.getByTestId("secret-edit-form"));
    expect(updateSpy).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("secret-edit-form")).toBeTruthy();

    fireEvent.change(screen.getByTestId("secret-value-input"), {
      target: { value: FAKE_VALUE },
    });
    fireEvent.click(save);
    await waitFor(() => expect(screen.queryByTestId("secret-edit-form")).toBeNull());
    expect(updateSpy).toHaveBeenLastCalledWith("sec-00000000000a", { value: FAKE_VALUE });
  });

  it("editing with a blank value sends only the changed fields, and clearing the note drops its line from the card", async () => {
    const updateSpy = vi.spyOn(api, "updateSecret");
    const { container } = renderPage();
    const row = await firstRow(container);

    fireEvent.click(row.querySelector('[data-testid="secret-edit"]')!);
    const valueInput = screen.getByTestId("secret-value-input") as HTMLInputElement;
    expect(valueInput.value).toBe("");
    expect(valueInput.placeholder).toBe(zh.secrets.valueKeepPlaceholder);
    fireEvent.change(screen.getByTestId("secret-note-input"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByTestId("secret-save"));

    await waitFor(() =>
      expect(updateSpy).toHaveBeenCalledWith("sec-00000000000a", { note: "" }),
    );
    const saved = await firstRow(container);
    expect(saved.querySelector('[data-testid="secret-note"]')).toBeNull();
    expect(saved.querySelector('[data-testid="secret-last-accessed"]')).toBeTruthy();
  });

  it("the value field starts hidden and its eye toggles it without submitting the form", async () => {
    const createSpy = vi.spyOn(api, "createSecret");
    const { container } = renderPage();
    await firstRow(container);

    fireEvent.click(screen.getByTestId("secrets-add"));
    const valueInput = screen.getByTestId("secret-value-input") as HTMLTextAreaElement;
    const eye = screen.getByTestId("secret-reveal-toggle");
    fireEvent.change(screen.getByTestId("secret-name-input"), { target: { value: "Staging DB" } });
    expect(valueInput.classList.contains("secret-value-input__field--masked")).toBe(true);
    expect(eye.getAttribute("aria-label")).toBe("顯示內容");
    fireEvent.change(valueInput, { target: { value: FAKE_VALUE } });
    fireEvent.click(eye);
    expect(valueInput.classList.contains("secret-value-input__field--masked")).toBe(false);
    expect(eye.getAttribute("aria-label")).toBe("隱藏內容");
    expect(valueInput.value).toBe(FAKE_VALUE);
    expect(createSpy).not.toHaveBeenCalled();
    fireEvent.click(eye);
    expect(valueInput.classList.contains("secret-value-input__field--masked")).toBe(true);
  });

  it("deleting goes through the confirmation before calling the API", async () => {
    const deleteSpy = vi.spyOn(api, "deleteSecret");
    const { container } = renderPage();
    const row = await firstRow(container);

    fireEvent.click(row.querySelector('[data-testid="secret-delete"]')!);
    expect(deleteSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId("secrets-delete-confirm").textContent).toContain(
      "GitHub deploy token",
    );
    fireEvent.click(screen.getByTestId("secrets-delete-confirm-btn"));

    await waitFor(() => expect(deleteSpy).toHaveBeenCalledWith("sec-00000000000a"));
    expect(await screen.findByTestId("secrets-empty")).toBeTruthy();
    expect(screen.queryByTestId("secrets-delete-confirm")).toBeNull();
  });
});
