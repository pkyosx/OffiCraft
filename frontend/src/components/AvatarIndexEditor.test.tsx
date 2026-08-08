import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider, useI18n } from "../i18n";
import { AvatarIndexEditor } from "./AvatarIndexEditor";

const IMG_A = "data:image/png;base64,iVBORw0KGgo=";
const IMG_B = "data:image/png;base64,iVBORw0KGgp=";
const labels = {
  label: "Choose avatar",
  savingLabel: "Saving…",
  errorLabel: "The avatar was not saved.",
};

let themeContext!: ReturnType<typeof useI18n>;
function Capture() {
  themeContext = useI18n();
  return null;
}

function renderEditor(
  onSave: (avatarIndex: number) => Promise<void>,
  value = 0,
) {
  const utils = render(
    <I18nProvider>
      <Capture />
      <AvatarIndexEditor
        value={value}
        kind="member"
        onSave={onSave}
        {...labels}
      />
    </I18nProvider>,
  );
  act(() => {
    themeContext.commitCustomThemes([
      {
        id: "portraits",
        name: "Portraits",
        colors: { "--color-bg": "#101018" },
        avatarPools: { member: [IMG_A, IMG_B] },
      },
    ]);
    themeContext.setTheme("portraits");
  });
  return utils;
}

describe("AvatarIndexEditor", () => {
  it("shows images instead of index text and saves the clicked image", async () => {
    let resolveSave!: () => void;
    const onSave = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveSave = resolve;
        }),
    );
    renderEditor(onSave);

    const choices = screen.getAllByRole("radio");
    expect(choices).toHaveLength(2);
    expect(choices[0].querySelector("img")?.getAttribute("src")).toBe(IMG_A);
    expect(choices[1].querySelector("img")?.getAttribute("src")).toBe(IMG_B);
    expect(screen.queryByRole("spinbutton")).toBeNull();

    fireEvent.click(choices[1]);
    expect(onSave).toHaveBeenCalledWith(1);
    expect(choices.every((choice) => (choice as HTMLButtonElement).disabled)).toBe(
      true,
    );
    expect(screen.getByRole("status").textContent).toBe(labels.savingLabel);

    resolveSave();
    await waitFor(() => expect(choices[1].getAttribute("aria-checked")).toBe("true"));
  });

  it("shows an inline error and restores the last confirmed image on failure", async () => {
    const onSave = vi.fn().mockRejectedValue(new Error("wire failed"));
    renderEditor(onSave);

    const choices = screen.getAllByRole("radio");
    fireEvent.click(choices[1]);

    expect((await screen.findByRole("alert")).textContent).toContain(
      labels.errorLabel,
    );
    expect(choices[0].getAttribute("aria-checked")).toBe("true");
    expect(choices[1].getAttribute("aria-checked")).toBe("false");
  });

  it("renders no technical control when the active theme has no pool", () => {
    render(
      <I18nProvider>
        <AvatarIndexEditor
          value={4}
          kind="member"
          onSave={vi.fn()}
          {...labels}
        />
      </I18nProvider>,
    );
    expect(screen.queryByRole("radiogroup")).toBeNull();
  });
});
