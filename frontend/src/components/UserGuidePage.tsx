// components/UserGuidePage.tsx — 設定 › 使用說明 (product guide): the embedded
// docs/guide/ content, read the same way Mira reads it (get_doc). The LIST picks
// a doc; the DOC view renders its markdown via the shared XSS-safe Markdown
// component, with relative image refs (already rewritten server-side to
// /api/docs/assets/…) loaded through the gated ?token= auth a bare <img> needs.

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { DocSummaryView } from "../api/adapter";
import { api } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { Markdown } from "./Markdown";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import { ChevronRightIcon } from "./icons";

export function UserGuideList({
  docs,
  loading,
  error,
  crumbs,
  onOpen,
}: {
  docs: DocSummaryView[];
  loading: boolean;
  error: boolean;
  crumbs: Crumb[];
  onOpen: (slug: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">{t.settings.guide}</h1>
      {error ? (
        <p className="settings__error">{t.settings.guideLoadError}</p>
      ) : loading ? null : docs.length === 0 ? (
        <p className="settings__empty">{t.settings.guideEmpty}</p>
      ) : (
        <div className="set-entries">
          {docs.map((d) => (
            <button
              key={d.slug}
              type="button"
              className="set-entry"
              data-testid="guide-doc-entry"
              onClick={() => onOpen(d.slug)}
            >
              <span className="set-entry__name">{d.title}</span>
              <ChevronRightIcon size={18} className="set-entry__chev" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function UserGuideDoc({
  slug,
  crumbs,
}: {
  slug: string;
  /** 設定 › 使用說明 › <this doc>. The last crumb's label is filled with the
   * doc title once it loads (the caller passes the trail up to 使用說明). */
  crumbs: Crumb[];
}) {
  const { t } = useI18n();
  const [markdown, setMarkdown] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState(false);

  useEffect(() => {
    let alive = true;
    setError(false);
    api
      .getDoc(slug)
      .then((doc) => {
        if (!alive) return;
        setMarkdown(doc.markdownMd);
        setTitle(doc.title);
      })
      .catch((e) => {
        console.warn("UserGuideDoc: load failed", e);
        if (alive) setError(true);
      });
    return () => {
      alive = false;
    };
  }, [slug]);

  return (
    <div className="settings">
      <Breadcrumbs items={[...crumbs, { label: title || slug }]} />
      <h1 className="settings__title settings__title--doc">{title || slug}</h1>
      <div className="doc-card">
        {error ? (
          <p className="settings__error">{t.settings.guideLoadError}</p>
        ) : (
          <Markdown
            source={markdown}
            className="doc-md"
            resolveImageSrc={authedAttachmentUrl}
          />
        )}
      </div>
    </div>
  );
}
