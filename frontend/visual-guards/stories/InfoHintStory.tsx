// CT story for the ⓘ that holds a title's explanation. "settings" is the real
// SettingsPage in the app's ancestor chain (walked to 一般設定 or 全域情境 by the
// guard); "menu" is the real profile dropdown right-anchored in the top bar;
// "dialog" is an Esc layer holding an ⓘ, the shape every modal has; "wrap" is
// one Latin and one CJK title at every box width where it wraps near the ⓘ.
import { useEffect, useRef, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { SettingsPage } from "../../src/components/SettingsPage";
import { ProfileDropdown } from "../../src/components/ProfileDropdown";
import { InfoHint, TitleWithInfo } from "../../src/components/InfoHint";
import { useEscapeLayer } from "../../src/lib/useEscapeLayer";
import { __resetMock } from "../../src/api/mock";

function EscDialog() {
  const ref = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(true);
  useEscapeLayer(() => setOpen(false), ref, open);
  if (!open) return <div data-testid="dialog-closed">closed</div>;
  return (
    <div ref={ref} role="dialog" aria-label="對話框" className="param-card" style={{ margin: 40 }}>
      對話框標題
      <InfoHint about="對話框標題" hint="對話框裡的說明" />
    </div>
  );
}

function WrapSweep() {
  return (
    <div>
      {["Reassign handover timeout", "外包交接逾時時間設定", "Secret 請示卡的取得後保留時間"].flatMap((title) =>
        Array.from({ length: 160 }, (_, i) => (
          <div key={`${title}-${i}`} className="param-row__name" style={{ width: 40 + i }}>
            <TitleWithInfo title={title} hint="說明" />
          </div>
        )),
      )}
    </div>
  );
}

export function InfoHintStory({ mode }: { mode: "settings" | "menu" | "dialog" | "wrap" }) {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    __resetMock();
    setReady(true);
  }, []);
  if (!ready) return null;
  return (
    <I18nProvider>
      {mode === "settings" && (
        <div className="app">
          <main className="app__main">
            <SettingsPage />
          </main>
        </div>
      )}
      {mode === "menu" && (
        <header className="topbar">
          <div className="topbar__brand">OffiCraft</div>
          <div className="topbar__actions">
            <div className="profile-menu">
              <button className="profile-pill" type="button">
                <span className="profile-pill__name">使用者</span>
              </button>
              <ProfileDropdown open onClose={() => {}} userName="使用者" setOwnerName={() => {}} />
            </div>
          </div>
        </header>
      )}
      {mode === "dialog" && <EscDialog />}
      {mode === "wrap" && <WrapSweep />}
    </I18nProvider>
  );
}
