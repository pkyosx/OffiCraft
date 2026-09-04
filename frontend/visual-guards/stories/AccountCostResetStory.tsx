// CT stories for T-53 帳號歸零 (owner ruling rc-5c5d7c7c6dcd): the destructive
// button on the ACCOUNT card, next to the account's own figure.
//
// jsdom cannot answer either question these exist for. (1) The pill is placed
// INLINE on the cost line — a flex row whose cost side is `flex: none` — so
// whether it stays inside the card instead of pushing the head open needs a real
// layout engine; a unit test sees every box at x=0 with no width. (2) The
// confirm is `position: fixed` over the page, and whether it actually COVERS the
// figure it is about to destroy is likewise a real-browser question.
//
// The stories feed AccountCard a hand-built view directly (no hooks, no api), so
// what is measured is that component's own layout.
import { I18nProvider } from "../../src/i18n";
import { AccountCard } from "../../src/components/MonitorPage";
import type { MonAccountView } from "../../src/types";

const baseAccount: MonAccountView = {
  account: "eva@example.test/9f8e-uuid",
  accountLabel: "eva@example.test(Example Org)",
  displayName: "Eva 的帳號",
  machine: "MBP 5",
  cost: 37,
  fiveHour: null,
  sevenDay: null,
};

/** The steady state: an account with spend on the clock, so the button is live. */
export function AccountCostResetButtonStory() {
  return (
    <I18nProvider>
      <div style={{ width: 420, padding: 16 }}>
        <AccountCard
          account={baseAccount}
          onRename={() => {}}
          onDetail={() => {}}
          onResetCost={async () => {}}
        />
      </div>
    </I18nProvider>
  );
}

/** Nothing measured: the figure is the dash and the button must be dead, which
 * is the same condition rendered two ways. */
export function AccountCostResetNothingToClearStory() {
  return (
    <I18nProvider>
      <div style={{ width: 420, padding: 16 }}>
        <AccountCard
          account={{ ...baseAccount, cost: null }}
          onRename={() => {}}
          onDetail={() => {}}
          onResetCost={async () => {}}
        />
      </div>
    </I18nProvider>
  );
}
