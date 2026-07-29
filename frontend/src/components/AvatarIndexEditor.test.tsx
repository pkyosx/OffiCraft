import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AvatarIndexEditor } from "./AvatarIndexEditor";

const labels = {
  label: "Avatar index",
  saveLabel: "Save avatar index",
  savingLabel: "Saving…",
  errorLabel: "Avatar index was not saved.",
};

describe("AvatarIndexEditor", () => {
  it("waits for an explicit save and disables duplicate submits", async () => {
    let resolveSave!: () => void;
    const onSave = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveSave = resolve;
        }),
    );
    render(<AvatarIndexEditor value={2} onSave={onSave} {...labels} />);

    fireEvent.change(screen.getByLabelText(labels.label), {
      target: { value: "7" },
    });
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: labels.saveLabel }));
    expect(onSave).toHaveBeenCalledWith(7);
    expect(
      (screen.getByRole("button", {
        name: labels.savingLabel,
      }) as HTMLButtonElement).disabled,
    ).toBe(true);

    resolveSave();
    await waitFor(() => {
      expect(
        (screen.getByRole("button", {
          name: labels.saveLabel,
        }) as HTMLButtonElement).disabled,
      ).toBe(true);
      expect(
        (screen.getByLabelText(labels.label) as HTMLInputElement).value,
      ).toBe("7");
    });
  });

  it("shows an inline error and restores the last confirmed value on failure", async () => {
    const onSave = vi.fn().mockRejectedValue(new Error("wire failed"));
    render(<AvatarIndexEditor value={3} onSave={onSave} {...labels} />);

    fireEvent.change(screen.getByLabelText(labels.label), {
      target: { value: "9" },
    });
    fireEvent.click(screen.getByRole("button", { name: labels.saveLabel }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      labels.errorLabel,
    );
    expect(
      (screen.getByLabelText(labels.label) as HTMLInputElement).value,
    ).toBe("3");
  });

  it("supports Enter to save and Escape to discard", () => {
    const onSave = vi.fn(() => new Promise<void>(() => {}));
    render(<AvatarIndexEditor value={4} onSave={onSave} {...labels} />);
    const input = screen.getByLabelText(labels.label);

    fireEvent.change(input, { target: { value: "8" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect((input as HTMLInputElement).value).toBe("4");
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "6" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onSave).toHaveBeenCalledWith(6);
  });
});
