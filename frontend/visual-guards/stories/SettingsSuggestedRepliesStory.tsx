// CT story for T-122 — the 建議回覆 EDITOR rows on 設定 › 參數調整, in the real
// app CSS.
//
// WHY THE WHOLE SettingsPage AND NOT A HAND-BUILT ROW: the bug this guard
// exists for is a CSS CASCADE COLLISION between two classes the editor's input
// wears at once (`.param-input` for chrome, `.sugg-edit__input` for width). A
// story that re-declared the markup would carry my copy of the class list, not
// the component's, and would go green while the real page stayed broken. So the
// real page is mounted and walked the way an owner walks it.
//
// Wrapped in `.app__main` for the real ancestor chain (the 22px gutters a bare
// mount omits — frontend/CLAUDE.md).
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { mockApi } from "../../src/api/mock";
import { setToken } from "../../src/api/auth";
import { SettingsPage } from "../../src/components/SettingsPage";

/** A full-length entry — the thing the owner actually types here. At the narrow
 * numeric-field width this shows about seven characters. */
const SENTENCE =
  "收到，這個我看過了。先照你說的做，但倉庫那邊的位子要等他們週一回覆才能確定。";

export function SettingsSuggestedRepliesStory() {
  // 🔴 SEED FIRST, MOUNT SECOND. SettingsPage reads /api/settings once when it
  // mounts and does not re-read; seeding after that leaves the editor showing
  // its empty state and the guard measuring a field that is not on screen.
  const [ready, setReady] = useState(false);
  const seed = async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: [SENTENCE],
      suggestedRepliesTaskMessage: [SENTENCE],
    });
    setToken("ct-owner-token");
    setReady(true);
  };
  return (
    <div>
      <button data-testid="seed" onClick={seed}>
        seed
      </button>
      {ready && (
        <I18nProvider>
          <div className="app__main">
            <SettingsPage />
          </div>
        </I18nProvider>
      )}
    </div>
  );
}
