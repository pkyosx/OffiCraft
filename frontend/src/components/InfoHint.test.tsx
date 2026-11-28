import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { InfoHint, TitleWithInfo } from "./InfoHint";

describe("InfoHint", () => {
  it("is named after the title it explains and shows its text only on hover or focus", () => {
    render(
      <I18nProvider>
        <InfoHint about="工作階段長度" hint="多久之後要重新登入" />
      </I18nProvider>,
    );
    const icon = screen.getByRole("img", { name: "關於 工作階段長度" });
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.focus(icon);
    expect(screen.getByRole("tooltip").textContent).toBe("多久之後要重新登入");
    expect(icon.getAttribute("aria-describedby")).toBe(screen.getByRole("tooltip").id);
  });

  it("a tap inside a row button pins the text and does not activate the row", () => {
    const onRow = vi.fn();
    render(
      <I18nProvider>
        <button type="button" onClick={onRow}>
          偏好設定
          <InfoHint about="偏好設定" hint="外觀、語言、版面" />
        </button>
      </I18nProvider>,
    );
    fireEvent.click(screen.getByTestId("info-hint"));
    expect(screen.getByRole("tooltip").textContent).toBe("外觀、語言、版面");
    expect(onRow).not.toHaveBeenCalled();
    fireEvent.click(document.body);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
});

describe("TitleWithInfo", () => {
  it("glues the last word, or a CJK title's last character, to the icon and keeps the title text whole", () => {
    const { container } = render(
      <I18nProvider>
        <TitleWithInfo title="Session length" hint="How long before you sign in again" />
        <TitleWithInfo title="登入有效期" hint="多久之後要重新登入" />
      </I18nProvider>,
    );
    const titles = Array.from(container.querySelectorAll(".info-hint__title"));
    expect(titles.map((t) => t.textContent)).toEqual(["Session length", "登入有效期"]);
    expect(
      Array.from(container.querySelectorAll(".info-hint__glue")).map((g) => g.textContent),
    ).toEqual(["length", "期"]);
    expect(screen.getByRole("img", { name: "關於 Session length" })).toBeTruthy();
  });
});
