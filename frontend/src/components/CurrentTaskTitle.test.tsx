// T-3451: the shared CurrentTaskTitle presentational contract — the ONE place
// the four surfaces render the current-task line, so truncation/hover/empty
// behave identically everywhere. Locked here:
//   · clamp → the --clamp variant + full text on `title`.
//   · !clamp → no --clamp (header shows the full title, un-truncated).
//   · empty + showEmpty (default) → muted placeholder, no title attr.
//   · empty + showEmpty=false → renders nothing (the chat header omits the line
//     rather than growing a "no task" row).
import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { CurrentTaskTitle } from "./CurrentTaskTitle";

function renderCTT(props: {
  title: string;
  clamp: boolean;
  showEmpty?: boolean;
}) {
  return render(
    <I18nProvider>
      <CurrentTaskTitle {...props} testid="ctt" />
    </I18nProvider>,
  );
}

describe("CurrentTaskTitle (T-3451)", () => {
  it("clamp variant: --clamp class + full text on the title tooltip", () => {
    const { getByTestId } = renderCTT({ title: "一個很長的任務標題", clamp: true });
    const el = getByTestId("ctt");
    expect(el.textContent).toBe("一個很長的任務標題");
    expect(el.getAttribute("title")).toBe("一個很長的任務標題");
    expect(el.className).toContain("current-task-title--clamp");
  });

  it("non-clamp variant (header): full text, no --clamp", () => {
    const { getByTestId } = renderCTT({ title: "完整標題", clamp: false });
    const el = getByTestId("ctt");
    expect(el.textContent).toBe("完整標題");
    expect(el.className).not.toContain("current-task-title--clamp");
  });

  it("empty + showEmpty default: muted placeholder, no title attr", () => {
    const { getByTestId } = renderCTT({ title: "", clamp: true });
    const el = getByTestId("ctt");
    expect(el.textContent).toBe(zh.office.noCurrentTask);
    expect(el.className).toContain("current-task-title--empty");
    expect(el.getAttribute("title")).toBeNull();
  });

  it("empty + showEmpty=false: renders nothing", () => {
    const { queryByTestId } = renderCTT({
      title: "",
      clamp: false,
      showEmpty: false,
    });
    expect(queryByTestId("ctt")).toBeNull();
  });
});
