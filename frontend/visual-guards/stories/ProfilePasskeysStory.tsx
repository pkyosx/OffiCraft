// CT story for 頭像選單 › 帳號安全 › 通行密鑰. The dropdown is mounted inside the
// production ancestor chain by class (.topbar > .topbar__actions >
// .profile-menu), because it is right-anchored to the profile pill and a bare
// mount would hand it slack the app does not have.
//
// The fixture is the widest row there is: a long tunnel host that is not usable
// here AND carries the clone warning, beside an ordinary one.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ProfileDropdown } from "../../src/components/ProfileDropdown";
import { __injectMockPasskey, __resetMock } from "../../src/api/mock";

export function ProfilePasskeysStory() {
  const [seeded, setSeeded] = useState(false);
  useEffect(() => {
    __resetMock();
    const now = Date.now() / 1000;
    __injectMockPasskey({
      id: "pk-000000000001", name: "Eva 的 MacBook Pro（公司配發，16 吋）", rp_id: "localhost",
      origin: "http://localhost:5241", created_ts: now - 86400 * 40, last_used_ts: now - 3600,
      usable_here: true, clone_warning_ts: null,
    });
    __injectMockPasskey({
      id: "pk-000000000002", name: "YubiKey5NFCSecurityKeyWithAVeryLongUnbrokenName",
      rp_id: "officraft-trial-station-for-the-whole-studio.example-tunnel.trycloudflare.com",
      origin: "https://officraft-trial-station-for-the-whole-studio.example-tunnel.trycloudflare.com",
      created_ts: now - 86400 * 400, last_used_ts: null, usable_here: false, clone_warning_ts: now - 600,
    });
    setSeeded(true);
  }, []);
  if (!seeded) return null;
  return (
    <I18nProvider>
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
    </I18nProvider>
  );
}
