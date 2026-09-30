// CT story for the identity card's action row (owner 2026-07-31「全部變成左右
// 並排」). The pair 更改 ＋ 停止 used to be a COLUMN (`.mp-identity__actions {
// flex-direction: column }` — a leftover from the era when the action group sat
// stacked over the retired 「更換機器」 button, and 更改 later inherited that
// lower slot). It is a row now, and the narrow-screen media query has to keep it
// one — the old ≤720px rules stretched the COLUMN, which would crush a row into
// two crammed halves if left unadapted.
//
// jsdom can answer none of that: it resolves no flex direction and never
// evaluates @media, so every unit assertion about this is structurally blind
// (the vitest suite pins the DOM shape — same parent, 更改 first — which is the
// other half of the contract).
//
// Uses the REAL MemberActionButtons for the stop half, so the group's own
// `.member-actions` flex/gap participates exactly as it does in the panel; the
// 更改 button is the panel's own markup, verbatim.
//
// 🔴 The row is NOT one shape. Owner 2026-08-21 made the ladder REVEALED, and
// owner 2026-08-22 made it ONE BUTTON THAT UPGRADES (「同一個按鈕 升級的概念 不是
// 不同按鈕」), so the cluster is 更改 ＋ exactly one ladder cell, and every rung
// the one cell can carry has to be measured, because 強制停止 is the longest
// label and a cell that fits 停止 is not evidence that it fits that.
//
// 🔴 BOTH PANELS, from ONE story. 正職 and 外包 render the same
// `.mp-identity__actions > .mp-identity__buttons` shell around the same
// MemberActionButtons; only the panel-owned 更改 button's testid differs. The
// `panel` prop reproduces that difference rather than asserting it away, so a
// geometry regression that only shows up on the outsource panel cannot hide
// behind the member panel's measurements.
import { I18nProvider } from "../../src/i18n";
import {
  MemberActionButtons,
  type StopLadderStage,
} from "../../src/components/MemberActionButtons";
import { CHANGE_TESTID } from "./identityPanelIds";
import "../../src/styles/theme.css";
import "../../src/components/office.css";
import "../../src/components/member-detail.css";

export function IdentityActionsRowStory({
  status = "online-awake",
  stage = "none",
  panel = "member",
}: {
  status?: "online-awake" | "stopping";
  stage?: StopLadderStage;
  panel?: "member" | "worker";
}) {
  return (
    <I18nProvider>
      {/* The real ancestor chain: the identity card is a flex row holding the
          avatar+body and, at its end, the action cluster. Without the card the
          row would have the whole viewport to spread into and the narrow case
          could never reproduce. */}
      <div style={{ width: "100%", padding: 22 }}>
        <div className="mp-card mp-identity" data-testid="story-identity">
          <div className="mp-identity__body">
            <div className="mp-identity__line">
              <span className="mp-identity__name">Mira</span>
              <span className="badge mp-identity__id">m-0001</span>
            </div>
          </div>
          <div className="mp-identity__actions">
            <div className="mp-identity__buttons">
              <button
                type="button"
                className="btn btn--accent-ghost"
                data-testid={CHANGE_TESTID[panel]}
              >
                更改
              </button>
              {/* The ladder cell gets a handler for every rung, so it is never
                  disabled for a reason the panel would not have — a disabled
                  button still lays out, but its title tooltip is the only thing
                  that differs and the measurements must not depend on it.
                  Mounting straight at `stage` also arms the row immediately
                  (LADDER_ARM_MS only fires on a stage CHANGE), so the geometry
                  is measured in its settled state. */}
              <MemberActionButtons
                status={status}
                stage={stage}
                onSpawn={() => {}}
                onStop={() => {}}
                onAcceleratedStop={() => {}}
                onForceStop={() => {}}
              />
            </div>
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}
