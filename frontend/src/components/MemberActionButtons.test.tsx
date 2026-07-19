// Force-stop escalation in the *stopping* state.
//
// Once a member is winding down (status "stopping"), its Stop button becomes
// FORCE-STOP: it must invoke onForceStop (the immediate-kill path), NOT onStop
// (the graceful deactivate). An "online-awake" member keeps the graceful Stop.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberActionButtons } from "./MemberActionButtons";

const forceLabel = zh.lifecycle.action["force-stop"];
const stopLabel = zh.lifecycle.action.stop;

describe("MemberActionButtons", () => {
  it("stopping renders Force stop and invokes onForceStop, not onStop", () => {
    const onForceStop = vi.fn();
    const onStop = vi.fn();
    const { getByText } = render(
      <I18nProvider>
        <MemberActionButtons
          status="stopping"
          onStop={onStop}
          onForceStop={onForceStop}
        />
      </I18nProvider>
    );
    fireEvent.click(getByText(forceLabel));
    expect(onForceStop).toHaveBeenCalledTimes(1);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("online-awake keeps the graceful Stop (onStop, no force-stop button)", () => {
    const onStop = vi.fn();
    const onForceStop = vi.fn();
    const { getByText, queryByText } = render(
      <I18nProvider>
        <MemberActionButtons
          status="online-awake"
          onStop={onStop}
          onForceStop={onForceStop}
        />
      </I18nProvider>
    );
    expect(queryByText(forceLabel)).toBeNull();
    fireEvent.click(getByText(stopLabel));
    expect(onStop).toHaveBeenCalledTimes(1);
    expect(onForceStop).not.toHaveBeenCalled();
  });
});
