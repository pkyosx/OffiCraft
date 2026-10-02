import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RuntimeLoginWarningMark } from "./RuntimeLoginWarningMark";
import type { ModelCallWarning, RuntimeLoginWarning } from "../types";

function hoverLines(warnings: RuntimeLoginWarning[], modelCallWarnings: ModelCallWarning[] = []) {
  render(
    <I18nProvider>
      <RuntimeLoginWarningMark warnings={warnings} modelCallWarnings={modelCallWarnings} />
    </I18nProvider>,
  );
  const marks = screen.getAllByTestId("runtime-login-warning");
  fireEvent.mouseEnter(marks[0]);
  const lines = Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent);
  return {
    marks: marks.length,
    lines,
    label: marks[0].getAttribute("aria-label"),
    className: marks[0].className,
  };
}

/** Local-time epoch seconds for today at h:mm, so HH:mm reads the same in any
 * timezone the suite runs in. */
function localTs(h: number, mi: number): number {
  return new Date(2026, 9, 2, h, mi, 0, 0).getTime() / 1000;
}

const call = (over: Partial<ModelCallWarning>): ModelCallWarning => ({
  runtime: "claude",
  kind: "other",
  code: "invalid_request",
  resetsAt: null,
  sinceTs: 1_790_000_000,
  accountWide: false,
  ...over,
});

describe("RuntimeLoginWarningMark", () => {
  it("under a pending pair alone, the hint names its machine exactly like a current pair", () => {
    expect(
      hoverLines([{ machineId: "m1", machineName: "seth-m1", runtime: "claude", pending: true }]),
    ).toEqual({
      marks: 1,
      lines: ["seth-m1 未登入 Claude"],
      label: "seth-m1 未登入 Claude",
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under a current and a pending pair on different machines, each line names its own machine", () => {
    expect(
      hoverLines([
        { machineId: "m5", machineName: "seth-m5", runtime: "claude", pending: false },
        { machineId: "mb", machineName: "Studio B", runtime: "codex", pending: true },
      ]),
    ).toEqual({
      marks: 1,
      lines: ["seth-m5 未登入 Claude", "Studio B 未登入 Codex"],
      label: "seth-m5 未登入 Claude\nStudio B 未登入 Codex",
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under the same machine and runtime as both current and pending, the hint shows it once", () => {
    expect(
      hoverLines([
        { machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: false },
        { machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: true },
      ]),
    ).toEqual({
      marks: 1,
      lines: ["seth-m1 未登入 Codex"],
      label: "seth-m1 未登入 Codex",
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under English, a pair reads as its machine signed out of the runtime", () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      expect(
        hoverLines([{ machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: true }]),
      ).toEqual({
        marks: 1,
        lines: ["seth-m1 signed out of Codex"],
        label: "seth-m1 signed out of Codex",
        className: "runtime-login-warning runtime-login-warning--danger",
      });
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("under every reason at once, one icon lists logged out, then auth, other, rate limit and server, one line each", () => {
    expect(
      hoverLines(
        [{ machineId: "m1", machineName: "seth-m1", runtime: "codex", pending: false }],
        [
          call({ kind: "server", code: "overloaded" }),
          call({ kind: "rate_limit", code: "rate_limit", resetsAt: localTs(14, 5) }),
          call({ kind: "other", code: "invalid_request" }),
          call({ kind: "auth", code: "authentication_failed" }),
        ],
      ),
    ).toEqual({
      marks: 1,
      lines: [
        "seth-m1 未登入 Codex",
        "Claude 登入失效",
        "Claude 模型呼叫失敗（invalid_request）",
        "已達用量上限 · 14:05 重置",
        "Claude 伺服器異常",
      ],
      label:
        "seth-m1 未登入 Codex\nClaude 登入失效\nClaude 模型呼叫失敗（invalid_request）\n已達用量上限 · 14:05 重置\nClaude 伺服器異常",
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under model-call warnings with no logged-out pair, the icon still shows", () => {
    expect(
      hoverLines([], [call({ kind: "rate_limit", code: "usageLimitExceeded", runtime: "codex", accountWide: true })]),
    ).toEqual({
      marks: 1,
      lines: ["已達用量上限"],
      label: "已達用量上限",
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under only server failures, the icon takes the server tone and names each runtime", () => {
    expect(
      hoverLines(
        [],
        [
          call({ kind: "server", code: "server_error", runtime: "claude" }),
          call({ kind: "server", code: "serverOverloaded", runtime: "codex" }),
        ],
      ),
    ).toEqual({
      marks: 1,
      lines: ["Claude 伺服器異常", "Codex 伺服器異常"],
      label: "Claude 伺服器異常\nCodex 伺服器異常",
      className: "runtime-login-warning runtime-login-warning--server",
    });
  });

  it("under English, each model-call reason reads in English", () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      expect(
        hoverLines(
          [],
          [
            call({ kind: "auth", code: "unauthorized", runtime: "codex" }),
            call({ kind: "other", code: "max_output_tokens" }),
            call({ kind: "rate_limit", code: "rate_limit", resetsAt: localTs(9, 30) }),
            call({ kind: "server", code: "overloaded" }),
          ],
        ),
      ).toEqual({
        marks: 1,
        lines: [
          "Codex sign-in expired",
          "Claude model call failed (max_output_tokens)",
          "Usage limit reached · resets 09:30",
          "Claude server error",
        ],
        label:
          "Codex sign-in expired\nClaude model call failed (max_output_tokens)\nUsage limit reached · resets 09:30\nClaude server error",
        className: "runtime-login-warning runtime-login-warning--danger",
      });
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("under no warnings of either kind, nothing renders, while one model-call warning alone renders the icon", () => {
    const empty = render(
      <I18nProvider>
        <RuntimeLoginWarningMark warnings={[]} modelCallWarnings={[]} />
      </I18nProvider>,
    );
    expect(empty.container.innerHTML).toBe("");
    empty.unmount();

    const one = render(
      <I18nProvider>
        <RuntimeLoginWarningMark warnings={[]} modelCallWarnings={[call({ kind: "server" })]} />
      </I18nProvider>,
    );
    expect(one.container.querySelectorAll('[data-testid="runtime-login-warning"]')).toHaveLength(1);
  });
});
