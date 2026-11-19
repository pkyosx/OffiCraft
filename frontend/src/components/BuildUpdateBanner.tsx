import { useEffect, useState, useSyncExternalStore } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import { isWindowActive } from "../hooks/useWindowActive";
import { hasAnyChatDraft } from "../lib/chatDraftStore";
import { createStaleBuildWatch } from "../lib/staleBuild";
import { trackUnsavedTextFields } from "../lib/unsavedInput";
import "./status-banner.css";

export function BuildUpdateBanner(): JSX.Element | null {
  const { t } = useI18n();
  const [fields] = useState(() => trackUnsavedTextFields(document));
  const [watch] = useState(() =>
    createStaleBuildWatch({
      readServerSha: () => api.getVersion().then((v) => v.gitSha),
      subscribeConnection: (cb) => api.subscribeConnection(cb),
      isWatched: isWindowActive,
      hasUnsavedInput: () => hasAnyChatDraft() || fields.hasEditedField(),
      reload: () => window.location.reload(),
      storage: {
        getItem: (key) => window.sessionStorage.getItem(key),
        setItem: (key, value) => window.sessionStorage.setItem(key, value),
      },
    })
  );
  useEffect(() => watch.start(), [watch]);
  useEffect(() => fields.stop, [fields]);
  const newerSha = useSyncExternalStore(watch.subscribe, watch.getNewerSha);

  if (newerSha === null) return null;

  return (
    <div className="status-banner" role="status" aria-live="polite" aria-label={t.buildUpdate.ariaLabel}>
      <span className="status-banner__dot" aria-hidden="true" />
      <span className="status-banner__title">{t.buildUpdate.title}</span>
      <span className="status-banner__body">{t.buildUpdate.body}</span>
      <button type="button" className="status-banner__reload" onClick={() => window.location.reload()}>
        {t.buildUpdate.reload}
      </button>
    </div>
  );
}
