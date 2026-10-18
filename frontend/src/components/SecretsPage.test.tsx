import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SecretsPage } from "./SecretsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";

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
  it("lists each secret's id, name, note and the access log", async () => {
    const { container } = renderPage();
    const row = await firstRow(container);
    expect(row.getAttribute("data-secret-id")).toBe("sec-00000000000a");
    expect(row.querySelector('[data-testid="secret-id"]')?.textContent).toContain(
      "sec-00000000000a",
    );
    expect(row.querySelector('[data-testid="secret-name"]')?.textContent).toBe(
      "GitHub deploy token",
    );
    expect(row.querySelector('[data-testid="secret-note"]')?.textContent).toContain(
      "Only for pushing release tags.",
    );
    const logRow = await waitFor(() => {
      const el = container.querySelector<HTMLElement>('[data-testid="secret-log-row"]');
      if (!el) throw new Error("no log row yet");
      return el;
    });
    expect(logRow.getAttribute("data-action")).toBe("reveal");
    expect(logRow.textContent).toContain(zh.secrets.actionReveal);
    expect(logRow.textContent).toContain("GitHub deploy token");
    expect(logRow.textContent).toContain("warden-mbp5");
  });

  it("copies the id on click and confirms only after the copy succeeded", async () => {
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
    const valueInput = screen.getByTestId("secret-value-input") as HTMLInputElement;
    expect(valueInput.type).toBe("password");
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

  it("editing with a blank value sends only the changed fields and keeps the stored value", async () => {
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
