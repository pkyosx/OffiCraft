import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AvatarEditor } from "./AvatarEditor";

function renderEditor(
  onUpload = vi.fn(async () => {}),
  onRemove = vi.fn(async () => {}),
  src = "",
) {
  return {
    ...render(
      <I18nProvider>
        <AvatarEditor
          kind="staff"
          src={src}
          onUpload={onUpload}
          onRemove={onRemove}
        />
      </I18nProvider>,
    ),
    onUpload,
    onRemove,
  };
}

describe("AvatarEditor", () => {
  it("uploads an allowed image and removes an existing personal image", async () => {
    const view = renderEditor(undefined, undefined, "blob:personal");
    const input = view.getByLabelText("更換頭像") as HTMLInputElement;
    const file = new File([new Uint8Array([0x89, 0x50])], "me.png", {
      type: "image/png",
    });
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() => expect(view.onUpload).toHaveBeenCalledWith(file));

    fireEvent.click(view.getByRole("button", { name: "移除頭像" }));
    await waitFor(() => expect(view.onRemove).toHaveBeenCalledOnce());
  });

  it("rejects unsupported and oversized files before calling the API", async () => {
    const view = renderEditor();
    const input = view.getByLabelText("更換頭像");
    fireEvent.change(input, {
      target: {
        files: [new File(["<svg/>"], "x.svg", { type: "image/svg+xml" })],
      },
    });
    expect((await view.findByRole("alert")).textContent).toBe(
      "只支援 PNG、JPEG 或 WEBP",
    );
    expect(view.onUpload).not.toHaveBeenCalled();

    fireEvent.change(input, {
      target: {
        files: [
          new File([new Uint8Array(64 * 1024 + 1)], "big.png", {
            type: "image/png",
          }),
        ],
      },
    });
    expect((await view.findByRole("alert")).textContent).toBe(
      "圖片不可超過 64 KiB",
    );
    expect(view.onUpload).not.toHaveBeenCalled();
  });

  it("keeps the visually hidden input out of the keyboard tab order", () => {
    const view = renderEditor();
    expect(view.getByLabelText("更換頭像").getAttribute("tabindex")).toBe("-1");
  });

  it("returns focus to the stable upload action after removal", async () => {
    const view = renderEditor(undefined, undefined, "blob:personal");
    const remove = view.getByRole("button", { name: "移除頭像" });
    remove.focus();
    fireEvent.click(remove);
    await waitFor(() => expect(view.onRemove).toHaveBeenCalledOnce());
    const upload = view.container.querySelector<HTMLButtonElement>(
      "button.avatar-editor__button:not(.avatar-editor__button--remove)",
    );
    await waitFor(() => expect(document.activeElement).toBe(upload));
  });

  it("with a presence, puts the dot on the avatar image and not beside the upload button", () => {
    const view = render(
      <I18nProvider>
        <AvatarEditor
          kind="staff"
          onUpload={vi.fn(async () => {})}
          onRemove={vi.fn(async () => {})}
          presence={{ status: "online", dotTestId: "editor-dot" }}
        />
      </I18nProvider>,
    );
    const corner = view.getByTestId("editor-dot").closest(".presence-avatar")!;
    expect(view.getByTestId("editor-dot").getAttribute("aria-label")).toBe("線上");
    expect(corner.querySelector(".avatar")).not.toBeNull();
    expect(corner.querySelector(".avatar-editor__button")).toBeNull();
  });
});
