// CT story for T-cd6f: the five per-theme avatar behaviours the owner asked to
// SEE, driven through the REAL AvatarChooser in a real browser.
//
// It holds the state the server holds — a member x theme association plus the
// live pools — so the guard beside it exercises the same resolution order the
// product ships: chosen id -> pool's first image -> built-in glyph.
import { useEffect, useRef, useState } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { TOKEN_KEY } from "../../src/api/auth";
import { AvatarChooser } from "../../src/components/AvatarChooser";
import { Avatar } from "../../src/components/Avatar";
import type { ThemeBundle } from "../../src/lib/themeBundle";

// One-pixel PNGs in distinct colours, so a screenshot shows WHICH image won.
const RED = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGO4Y2MDAANMAVV5GmOeAAAAAElFTkSuQmCC";
const GREEN = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGOw2RIFAAJ6AUt75/BPAAAAAElFTkSuQmCC";
const BLUE = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGNwy7sDAAKOAZEtH1A3AAAAAElFTkSuQmCC";
const AMBER = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGN4tkoDAAQyAbm/AbdRAAAAAElFTkSuQmCC";
// A PNG SIGNATURE with no image data after it. It passes the theme gate — the
// magic bytes are genuine — and the browser still cannot paint it, which is
// exactly the broken-image case the owner asked to see. A payload that failed
// validation would never reach a stored theme, so it could not reproduce this.
const BROKEN = "data:image/png;base64,iVBORw0KGgoAAQID";

const POOLS: Record<string, { id: string; image: string }[]> = {
  full: [
    { id: "icn-red", image: RED },
    { id: "icn-green", image: GREEN },
  ],
  // "the chosen image was removed" — icn-green is gone, icn-red is not.
  pruned: [{ id: "icn-red", image: RED }],
  empty: [],
  broken: [{ id: "icn-broken", image: BROKEN }],
};

function theme(id: string, name: string, pool: { id: string; image: string }[]): ThemeBundle {
  return {
    id,
    name,
    colors: { "--color-bg": "#12131a" },
    avatarPools: { member: pool },
  };
}

export function AvatarChooserScenariosStory() {
  return (
    <I18nProvider>
      <Scenarios />
    </I18nProvider>
  );
}

function Scenarios() {
  const { saveTheme, theme: activeTheme, setTheme } = useI18n();
  // The first commit also SELECTS alpha; later pool edits must not drag the
  // active theme back, or the theme-switch scenario could never leave it.
  const selected = useRef(false);
  const [alphaPool, setAlphaPool] = useState<keyof typeof POOLS>("full");
  // The association: theme id -> chosen icon id. Absent = never chose, which is
  // exactly what the server stores (nothing) and the wire sends (null).
  const [chosen, setChosen] = useState<Record<string, string>>({});

  useEffect(() => {
    // T-83ef: themes are a RESOURCE, and switching to one is token-gated — a
    // signed-out cockpit never fetches a bundle, so there would be no pool.
    localStorage.setItem(TOKEN_KEY, "ct-owner-token");
    void (async () => {
      await saveTheme(theme("alpha", "Alpha", POOLS[alphaPool]));
      await saveTheme(
        theme("beta", "Beta", [
          { id: "icn-blue", image: BLUE },
          { id: "icn-amber", image: AMBER },
        ]),
      );
      // The first pass also SELECTS alpha; later pool edits must not drag the
      // active theme back, or the theme-switch scenario could never leave it.
      if (!selected.current) {
        selected.current = true;
        setTheme("alpha");
      }
    })();
    // The pool set is the only input; the active theme is switched separately.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [alphaPool]);

  return (
    <div style={{ padding: 16, display: "grid", gap: 12, maxWidth: 360 }}>
      <div data-testid="active-theme">{activeTheme}</div>
      <div data-testid="rendered-avatar">
        <Avatar size={52} kind="member" avatarIconId={chosen[activeTheme] ?? null} />
      </div>
      <AvatarChooser
        value={chosen[activeTheme] ?? null}
        kind="member"
        onSave={async (iconId) => {
          setChosen((prev) => ({ ...prev, [activeTheme]: iconId }));
        }}
        label="頭像"
        changeLabel="選擇圖像"
        dialogTitle="選擇頭像"
        closeLabel="關閉"
        emptyLabel="目前主題沒有可用的頭像圖片。到「設定 → 主題」加入圖片後就可以選。"
        brokenLabel="圖片無法顯示"
        savingLabel="儲存中…"
        errorLabel="頭像未儲存，請稍後重試"
      />
      <div style={{ display: "grid", gap: 6 }}>
        <button type="button" data-testid="to-alpha" onClick={() => setTheme("alpha")}>
          theme alpha
        </button>
        <button type="button" data-testid="to-beta" onClick={() => setTheme("beta")}>
          theme beta
        </button>
        <button type="button" data-testid="prune-pool" onClick={() => setAlphaPool("pruned")}>
          remove the chosen image
        </button>
        <button type="button" data-testid="empty-pool" onClick={() => setAlphaPool("empty")}>
          empty the pool
        </button>
        <button type="button" data-testid="break-pool" onClick={() => setAlphaPool("broken")}>
          break the image
        </button>
      </div>
    </div>
  );
}
