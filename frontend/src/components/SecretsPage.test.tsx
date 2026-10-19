import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SecretsPage } from "./SecretsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
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
      zh.secrets.colNote + "Only for pushing release tags.",
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

  it("clicking the id copies it and confirms only after the copy succeeded", async () => {
    const { container } = renderPage();
    const row = await firstRow(container);
    fireEvent.click(row.querySelector('[data-testid="secret-id"]')!);
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledWith("sec-00000000000a"));
    expect(await screen.findByTestId("secret-id-copied")).toBeTruthy();
  });

  it("creating sends name, value and note, then shows the new row without its value", async () => {
    const createSpy = vi.spyOn(api, "createSecret");
    const { container } = renderPage();
    await firstRow(container);

    fireEvent.click(screen.getByTestId("secrets-add"));
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
      zh.secrets.colLastAccessed + zh.secrets.neverAccessed,
    );
    expect(created.querySelector('[data-testid="secret-note"]')?.textContent).toContain(
      "read-only",
    );
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
    vi.spyOn(api, "createSecret").mockRejectedValue(new Error("boom"));
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

    expect((await screen.findByTestId("secret-form-error")).textContent).toBe(
      zh.secrets.loadFailed,
    );
    expect(valueAppearsAnywhere(container)).toBe(false);
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
