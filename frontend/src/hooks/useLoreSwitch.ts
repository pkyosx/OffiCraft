// hooks/useLoreSwitch.ts — the station-wide 傳承 switch, for surfaces that need
// to know whether a lore LINK can land anywhere (T-33).
//
// 🔴 IT RIDES GET /api/lore-switch, NOT GET /api/settings. The settings read is
// owner/admin gated and hands back token TTLs, the push contact address and the
// onboarding install log; this route was added (owner ruling rc-2972dcd48782,
// 2026-09-06: 「新一支 tool，只回這個開關（權限半徑就一格）」) precisely so a
// surface that needs this ONE bit does not have to be handed the rest. App.tsx
// still reads it out of settings because it is already loading them for other
// reasons; a card deep inside the member panel is not.
//
// ⚠️ WHAT THIS VALUE IS, EXACTLY: the switch as it stood WHEN THIS HOOK RAN. It
// does not poll, and it does not subscribe. The switch can be flipped while the
// consumer is still on screen — that is the same property a boot context's copy
// of the flag has, and the reason /api/lore-switch exists as a live route at
// all. Consumers must be able to survive being wrong for a while: the lore
// activity card uses it to decide whether to LINK a heading, and the worst case
// of a stale `true` is one click landing on the office page (App.tsx drops a
// `#lore` route through when the feature is off). Polling to close that window
// would spend a request on every open panel to tighten a failure that costs one
// wrong tab.
//
// A failed read resolves to `false` — the same fail-closed choice App.tsx makes,
// and for the same reason: with the switch unknown, NOT offering a link is the
// answer that cannot mislead. It is logged so a broken read and a switched-off
// station are not indistinguishable to whoever is debugging it.

import { useEffect, useState } from "react";
import { api } from "../api";

export function useLoreSwitch(): boolean {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    let alive = true;
    api
      .getLoreSwitch()
      .then((on) => {
        if (alive) setEnabled(on);
      })
      .catch((e) => {
        console.warn(
          "useLoreSwitch: switch read failed — 傳承 links stay off",
          e,
        );
      });
    return () => {
      alive = false;
    };
  }, []);
  return enabled;
}
