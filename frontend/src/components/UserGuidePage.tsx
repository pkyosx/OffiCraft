// components/UserGuidePage.tsx — 設定 › 使用說明 (product guide): the embedded
// docs/guide/ content, read the same way Mira reads it (get_doc). The LIST picks
// a doc; the DOC view renders its markdown via the shared XSS-safe Markdown
// component, with relative image refs (already rewritten server-side to
// /api/docs/assets/…) loaded through the gated ?token= auth a bare <img> needs.
//
// T-68f1: the doc text also carries repo-relative links between docs
// (`[介面說明](interface.md)`). This is the ONE surface that opts in to
// Markdown's `resolveDocLink`, mapping such a reference onto the SAME slug the
// server derives, and only when that slug is actually in the embedded list.

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

/** Map a repo-relative markdown reference onto a doc slug, by the SERVER's
 * rule and no other: build-docsdist FLATTENS docs/guide/ (and the README) into
 * one directory, and api_docs.go's docSlug is `strings.TrimSuffix(name, ".md")`
 * on that flat filename — so only the BASENAME can carry information. Whatever
 * directory prefix the source used (`docs/guide/why.md` read from the repo
 * root, `../dev/agent-env.md` read from inside docs/guide) is exactly the part
 * the flattening threw away.
 *
 * Deriving a slug is NOT the same as it existing: `../dev/agent-env.md` yields
 * "agent-env", which is deliberately NOT embedded. Existence is checked
 * separately against the list the server actually served, so an unshipped
 * target degrades to the literal-text fallback instead of a 404 button. */
export function docSlugForRef(target: string): string {
  const base = target.slice(target.lastIndexOf("/") + 1);
  return base.endsWith(".md") ? base.slice(0, -3) : base;
}

export function UserGuideDoc({
  slug,
  docs,
  crumbs,
  onOpenDoc,
}: {
  slug: string;
  /** The embedded doc index — the existence check for an in-app link. */
  docs: DocSummaryView[];
  /** 設定 › 使用說明 › <this doc>. The last crumb's label is filled with the
   * doc title once it loads (the caller passes the trail up to 使用說明). */
  crumbs: Crumb[];
  /** Navigate to another doc in place (the page has no hash route). */
  onOpenDoc: (slug: string) => void;
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
            resolveDocLink={(target) => {
              const next = docSlugForRef(target);
              if (next === slug || !docs.some((d) => d.slug === next)) {
                return null;
              }
              return () => onOpenDoc(next);
            }}
          />
        )}
      </div>
    </div>
  );
}
