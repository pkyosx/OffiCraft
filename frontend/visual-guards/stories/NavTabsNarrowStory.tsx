// CT story for the nav tab strip at PHONE widths (T-68f1 · RC3-3).
//
// It mounts the REAL `App` behind the same two providers production wires
// above it (main.tsx's I18nProvider, AuthGate's ReplyCardsProvider) against the
// REAL mock adapter, because the thing under test is the strip's own
// horizontal overflow: how many of the five tabs the sheet's paddings leave
// room for at 320–430. A hand-built strip of five <button>s would measure a
// layout that never ships — the widths come from the real labels, the real
// icons, the real `.nav-tabs`/`.nav-tab` paddings, and the real @media block.
import { I18nProvider } from "../../src/i18n";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import App from "../../src/App";

export function NavTabsNarrowStory() {
  return (
    <I18nProvider>
      <ReplyCardsProvider>
        <App onLogout={() => {}} />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}
