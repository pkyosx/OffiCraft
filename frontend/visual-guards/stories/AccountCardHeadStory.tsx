// CT story: the REAL AccountCard inside the monitor page's real ancestor chain
// (`.app__main` gutters → `.monitor` → section → `.mon-accounts`), so the head
// row gets exactly the width a phone gives it.
//
// The CT runner swaps the seasonal schedule for an empty one, so the halloween
// copy is applied the way a custom theme's 用詞 reaches the screen in
// production: the bundle is stored through the theme resource and named as the
// display theme.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { setToken } from "../../src/api/auth";
import { AccountCard } from "../../src/components/MonitorPage";
import type { MonAccountView } from "../../src/types";
import type { ThemeBundle } from "../../src/lib/themeBundle";
import halloween from "../../../themes/halloween-2026.theme.json";

export type AccountCardHeadWording = "default" | "halloween";

export function AccountCardHeadStory({
  displayName,
  cost,
  wording,
}: {
  displayName: string;
  cost: number;
  wording: AccountCardHeadWording;
}) {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    // Dynamic import: a static one makes Playwright's node-side transform parse
    // mock.ts's `?raw` imports as JavaScript and the spec fails to collect.
    void (async () => {
      const { mockApi, __resetMock } = await import("../../src/api/mock");
      __resetMock();
      if (wording === "halloween") {
        await mockApi.putTheme(halloween as ThemeBundle);
        await mockApi.patchServerSettings({ displayTheme: halloween.id });
        setToken("ct-owner-token");
      }
      setReady(true);
    })();
  }, [wording]);
  if (!ready) return null;

  const account: MonAccountView = {
    account: "seth@example.test/9f8e-uuid",
    accountLabel: "seth@example.test(Example Org)",
    displayName,
    machine: "MBP 5",
    cost,
    fiveHour: null,
    sevenDay: null,
    limitReached: null,
  };
  return (
    <I18nProvider>
      <div className="app__main">
        <div className="monitor">
          <section className="mon-section">
            <div className="mon-accounts">
              <AccountCard
                account={account}
                onRename={() => {}}
                onDetail={() => {}}
                onResetCost={async () => {}}
              />
            </div>
          </section>
        </div>
      </div>
    </I18nProvider>
  );
}
