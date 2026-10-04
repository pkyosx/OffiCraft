import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
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

/** Local-time epoch seconds for 2026-10-02 (plus `days`) at h:mm, so HH:mm
 * reads the same in any timezone the suite runs in. */
function localTs(h: number, mi: number, days = 0): number {
  return new Date(2026, 9, 2 + days, h, mi, 0, 0).getTime() / 1000;
}

const SIGN_IN_CLAUDE = "可到「監控」頁的機器資訊，在 Claude 欄按版本號 →「登入」";
const SIGN_IN_CODEX = "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」";

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
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(2026, 9, 2, 10, 0, 0, 0));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("under a pending pair alone, the hint names its machine exactly like a current pair", () => {
    expect(
      hoverLines([{ machineId: "m1", machineName: "seth-m1", runtime: "claude", pending: true }]),
    ).toEqual({
      marks: 1,
      lines: ["seth-m1 未登入 Claude", SIGN_IN_CLAUDE],
      label: `seth-m1 未登入 Claude\n${SIGN_IN_CLAUDE}`,
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under a current and a pending pair on different machines, each line names its own machine and each runtime gets its own sign-in line", () => {
    expect(
      hoverLines([
        { machineId: "m5", machineName: "seth-m5", runtime: "claude", pending: false },
        { machineId: "mb", machineName: "Studio B", runtime: "codex", pending: true },
      ]),
    ).toEqual({
      marks: 1,
      lines: ["seth-m5 未登入 Claude", "Studio B 未登入 Codex", SIGN_IN_CLAUDE, SIGN_IN_CODEX],
      label: `seth-m5 未登入 Claude\nStudio B 未登入 Codex\n${SIGN_IN_CLAUDE}\n${SIGN_IN_CODEX}`,
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
      lines: ["seth-m1 未登入 Codex", SIGN_IN_CODEX],
      label: `seth-m1 未登入 Codex\n${SIGN_IN_CODEX}`,
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
        lines: ["seth-m1 signed out of Codex", "To sign in: Monitor → Machines, click the version in the Codex column → Sign in"],
        label: "seth-m1 signed out of Codex\nTo sign in: Monitor → Machines, click the version in the Codex column → Sign in",
        className: "runtime-login-warning runtime-login-warning--danger",
      });
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("under every reason at once, one icon lists logged out, then auth, other, rate limit and server, one line each, then where to sign in", () => {
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
        "Claude 已達用量上限 · 今天 14:05 重置",
        "Claude 伺服器異常",
        SIGN_IN_CODEX,
        SIGN_IN_CLAUDE,
      ],
      label: `seth-m1 未登入 Codex\nClaude 登入失效\nClaude 模型呼叫失敗（invalid_request）\nClaude 已達用量上限 · 今天 14:05 重置\nClaude 伺服器異常\n${SIGN_IN_CODEX}\n${SIGN_IN_CLAUDE}`,
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under a rate limit alone, the icon still shows and there is no sign-in line", () => {
    expect(
      hoverLines([], [call({ kind: "rate_limit", code: "usageLimitExceeded", runtime: "codex", accountWide: true })]),
    ).toEqual({
      marks: 1,
      lines: ["Codex 已達用量上限"],
      label: "Codex 已達用量上限",
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
          "Claude usage limit reached · resets today 09:30",
          "Claude server error",
          "To sign in: Monitor → Machines, click the version in the Codex column → Sign in",
        ],
        label:
          "Codex sign-in expired\nClaude model call failed (max_output_tokens)\nClaude usage limit reached · resets today 09:30\nClaude server error\nTo sign in: Monitor → Machines, click the version in the Codex column → Sign in",
        className: "runtime-login-warning runtime-login-warning--danger",
      });
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("under the member's own limit without a reset time beside an account-wide one with it, one usage-limit line carries the time", () => {
    expect(
      hoverLines(
        [],
        [
          call({ kind: "rate_limit", code: "usageLimitExceeded", runtime: "codex" }),
          call({ kind: "rate_limit", code: "rate_limit", runtime: "codex", resetsAt: localTs(16, 39), accountWide: true }),
        ],
      ),
    ).toEqual({
      marks: 1,
      lines: ["Codex 已達用量上限 · 今天 16:39 重置"],
      label: "Codex 已達用量上限 · 今天 16:39 重置",
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under several usage limits with reset times, the one line names the latest", () => {
    expect(
      hoverLines(
        [],
        [
          call({ kind: "rate_limit", code: "rate_limit", resetsAt: localTs(23, 0) }),
          call({ kind: "rate_limit", code: "rate_limit", resetsAt: localTs(4, 30, 1), accountWide: true }),
          call({ kind: "rate_limit", code: "usageLimitExceeded" }),
        ],
      ).lines,
    ).toEqual(["Claude 已達用量上限 · 明天 04:30 重置"]);
  });

  it("under an expired sign-in alone, the hint says where to sign in again", () => {
    expect(hoverLines([], [call({ kind: "auth", code: "authentication_failed" })])).toEqual({
      marks: 1,
      lines: ["Claude 登入失效", SIGN_IN_CLAUDE],
      label: `Claude 登入失效\n${SIGN_IN_CLAUDE}`,
      className: "runtime-login-warning runtime-login-warning--danger",
    });
  });

  it("under a logged-out machine and an expired sign-in on the same runtime, the sign-in line appears once", () => {
    expect(
      hoverLines(
        [{ machineId: "m1", machineName: "seth-m1", runtime: "claude", pending: false }],
        [call({ kind: "auth", code: "authentication_failed" })],
      ).lines,
    ).toEqual(["seth-m1 未登入 Claude", "Claude 登入失效", SIGN_IN_CLAUDE]);
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

describe("RuntimeLoginWarningMark across local midnight", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("under nothing else re-rendering, the reset day rolls over at each local midnight", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 9, 2, 23, 50, 0, 0));
    render(
      <I18nProvider>
        <RuntimeLoginWarningMark
          warnings={[]}
          modelCallWarnings={[call({ kind: "rate_limit", code: "rate_limit", resetsAt: localTs(4, 30, 2) })]}
        />
      </I18nProvider>,
    );
    const label = () => screen.getByTestId("runtime-login-warning").getAttribute("aria-label");
    expect(label()).toBe("Claude 已達用量上限 · 10/4 04:30 重置");

    act(() => {
      vi.advanceTimersByTime(11 * 60 * 1000);
    });
    expect(label()).toBe("Claude 已達用量上限 · 明天 04:30 重置");

    act(() => {
      vi.advanceTimersByTime(24 * 60 * 60 * 1000);
    });
    expect(label()).toBe("Claude 已達用量上限 · 今天 04:30 重置");
  });
});

describe("RuntimeLoginWarningMark under a click or tap", () => {
  const tooltipLines = () =>
    screen.queryByRole("tooltip")
      ? Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent)
      : null;

  function renderInRow() {
    const onRowClick = vi.fn();
    render(
      <I18nProvider>
        <div data-testid="row" onClick={onRowClick}>
          <RuntimeLoginWarningMark
            warnings={[{ machineId: "m1", machineName: "seth-m1", runtime: "claude", pending: false }]}
            modelCallWarnings={[]}
          />
        </div>
      </I18nProvider>,
    );
    return { mark: screen.getByTestId("runtime-login-warning"), onRowClick };
  }

  it("under a click on the mark, the hint opens and the enclosing row's click handler is not called", () => {
    const { mark, onRowClick } = renderInRow();
    fireEvent.click(mark);
    expect(tooltipLines()).toEqual(["seth-m1 未登入 Claude", SIGN_IN_CLAUDE]);
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("under a second click on the mark, the hint closes, and the row still is not called", () => {
    const { mark, onRowClick } = renderInRow();
    fireEvent.click(mark);
    fireEvent.click(mark);
    expect(tooltipLines()).toBeNull();
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("under a click outside after the mark was clicked open, the hint closes", () => {
    const { mark } = renderInRow();
    fireEvent.click(mark);
    expect(tooltipLines()).toEqual(["seth-m1 未登入 Claude", SIGN_IN_CLAUDE]);
    fireEvent.click(document.body);
    expect(tooltipLines()).toBeNull();
  });

  it("under a click on the open hint itself, the hint stays open and the row is not called", () => {
    const { mark, onRowClick } = renderInRow();
    fireEvent.click(mark);
    const hint = screen.getByRole("tooltip");
    expect(hint.className).toBe("instant-hint instant-hint--pinned");
    fireEvent.click(hint);
    expect(tooltipLines()).toEqual(["seth-m1 未登入 Claude", SIGN_IN_CLAUDE]);
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("under a tap (mouseenter, focus, then click), the hint stays open, survives mouseleave and blur, and the next tap closes it", () => {
    const { mark, onRowClick } = renderInRow();
    fireEvent.mouseEnter(mark);
    fireEvent.focus(mark);
    fireEvent.click(mark);
    expect(tooltipLines()).toEqual(["seth-m1 未登入 Claude", SIGN_IN_CLAUDE]);
    fireEvent.mouseLeave(mark);
    fireEvent.blur(mark);
    expect(tooltipLines()).toEqual(["seth-m1 未登入 Claude", SIGN_IN_CLAUDE]);
    fireEvent.click(mark);
    expect(tooltipLines()).toBeNull();
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("under a hover with no click, the hint is not pinned and mouseleave closes it", () => {
    const { mark } = renderInRow();
    fireEvent.mouseEnter(mark);
    expect(screen.getByRole("tooltip").className).toBe("instant-hint");
    fireEvent.mouseLeave(mark);
    expect(tooltipLines()).toBeNull();
  });
});
