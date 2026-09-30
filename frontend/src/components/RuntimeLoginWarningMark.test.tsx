import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RuntimeLoginWarningMark } from "./RuntimeLoginWarningMark";
import type { RuntimeLoginWarning } from "../types";

function hoverLines(warnings: RuntimeLoginWarning[]) {
  render(
    <I18nProvider>
      <RuntimeLoginWarningMark warnings={warnings} />
    </I18nProvider>,
  );
  const mark = screen.getByTestId("runtime-login-warning");
  fireEvent.mouseEnter(mark);
  const lines = Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent);
  return { lines, label: mark.getAttribute("aria-label") };
}

describe("RuntimeLoginWarningMark", () => {
  it("under a pending pair alone, the hint names its machine exactly like a current pair", () => {
    expect(
      hoverLines([{ machineId: "m1", machineName: "seth-m1", runtime: "claude", pending: true }]),
    ).toEqual({ lines: ["seth-m1 未登入 Claude"], label: "seth-m1 未登入 Claude" });
  });

  it("under a current and a pending pair on different machines, each line names its own machine", () => {
    expect(
      hoverLines([
        { machineId: "m5", machineName: "seth-m5", runtime: "claude", pending: false },
        { machineId: "mb", machineName: "Studio B", runtime: "codex", pending: true },
      ]),
    ).toEqual({
      lines: ["seth-m5 未登入 Claude", "Studio B 未登入 Codex"],
      label: "seth-m5 未登入 Claude\nStudio B 未登入 Codex",
    });
  });

  it("under the same machine and runtime as both current and pending, the hint shows it once", () => {
    expect(
      hoverLines([
        { machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: false },
        { machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: true },
      ]),
    ).toEqual({ lines: ["seth-m1 未登入 Codex"], label: "seth-m1 未登入 Codex" });
  });

  it("under English, a pair reads as its machine signed out of the runtime", () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      expect(
        hoverLines([{ machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: true }]),
      ).toEqual({ lines: ["seth-m1 signed out of Codex"], label: "seth-m1 signed out of Codex" });
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("under no warnings, nothing renders", () => {
    const { container } = render(
      <I18nProvider>
        <RuntimeLoginWarningMark warnings={[]} />
      </I18nProvider>,
    );
    expect(container.innerHTML).toBe("");
  });
});
