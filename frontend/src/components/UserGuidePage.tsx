// components/UserGuidePage.tsx — 使用說明 (product guide): the embedded
// docs/guide/ content, read the same way Mira reads it (get_doc). The LIST picks
// a doc; the DOC view renders its markdown via the shared XSS-safe Markdown
// component, with relative image refs (already rewritten server-side to
// /api/docs/assets/…) loaded through the gated ?token= auth a bare <img> needs.
//
// It is a TOP-LEVEL nav tab, to the right of 監控 (owner: 「user guide 改放在
// tab 中,監控的右邊,不要放在 settings 裡」). It used to be a settings
// sub-page, which is why the layout still borrows the `.settings` shell
// classes — the visual container is the same full-width document surface;
// only its place in the navigation changed.
//
// T-68f1: the doc text also carries repo-relative links between docs
// (`[介面說明](interface.md)`). This is the ONE surface that opts in to
// Markdown's `resolveDocLink`, mapping such a reference onto the SAME slug the
// server derives, and only when that slug is actually in the embedded list.

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { DocSummaryView } from "../api/adapter";
import { api } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { useDocs } from "../hooks/useDocs";
import { navigateHash, useHashRoute } from "../lib/hashRoute";
import { Markdown } from "./Markdown";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import { ChevronRightIcon } from "./icons";

export function UserGuideList({
  docs,
  loading,
  error,
  onOpen,
}: {
  docs: DocSummaryView[];
  loading: boolean;
  error: boolean;
  onOpen: (slug: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="settings guide-view">
      {/* NO breadcrumb here, unlike the settings landing. A one-segment trail
          is not navigation — it renders as plain text with nothing to click —
          and on this surface it was the THIRD copy of the word 使用說明 in the
          top 200px, after the active nav tab and the <h1> directly below it.
          The settings landing keeps its single 設定 crumb because Settings is
          an overlay with no tab of its own; this page's tab is always visible
          and always says the same word. The doc view below DOES keep the
          trail: there the first segment is a real button and the only way
          back to this list. */}
      <h1 className="settings__title settings__title--doc">{t.guide.title}</h1>
      {error ? (
        <p className="settings__error">{t.guide.loadError}</p>
      ) : loading ? null : docs.length === 0 ? (
        <p className="settings__empty">{t.guide.empty}</p>
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
 * rule and no other: build-docsdist FLATTENS docs/guide/ (every *.md in the
 * tree — the README used to ride along too, but was pulled; see
 * bin/build-docsdist's SCOPE note) into one directory, and api_docs.go's
 * docSlug is `strings.TrimSuffix(name, ".md")`
 * on that flat filename — so only the BASENAME can carry information. Whatever
 * directory prefix the source used (`docs/guide/why.md` read from the repo
 * root, `../dev/agent-env.md` read from inside docs/guide) is exactly the part
 * the flattening threw away.
 *
 * Deriving a slug is NOT the same as it existing: `../dev/agent-env.md` yields
 * "agent-env", which is deliberately NOT embedded. Existence is checked
 * separately against the list the server actually served, so an unshipped
 * target degrades to the literal-text fallback instead of a 404 button.
 *
 * This basename reduction is also the CONTAINMENT layer, not just a mapping:
 * Markdown's DOC_REL_PATH_RE lets `../../../etc/passwd.md` and `evil.com/x.md`
 * through to the resolver (both match its character class). Taking the
 * basename discards every directory prefix — traversal or host alike — and the
 * `docs.some(...)` check downstream then requires the survivor to be a doc the
 * server really served. The target is NEVER used as a path here; do not start. */
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
  /** 使用說明 › <this doc>. The last crumb's label is filled with the doc
   * title once it loads (the caller passes the trail up to 使用說明). */
  crumbs: Crumb[];
  /** Navigate to another doc (the caller writes #guide/<slug>). */
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

  // Switching docs must land the reader at the TOP of the new one. `.settings`
  // is the scrolling box (settings.css: `height: 100%` + `min-height: 0` +
  // `overflow-y: auto`; nothing above it in the shell scrolls — .app and
  // .app__main are fixed-height flex), and doc→doc keeps that exact DOM node:
  // GuidePage renders the same <UserGuideDoc> element type either way, so React
  // reconciles instead of remounting and the old scroll offset survives. Every
  // in-app link sits in the BODY of a doc, i.e. far down, so without this the
  // reader always arrived mid-page — no title, no breadcrumb, no signal that
  // the document had changed at all.
  //
  // This fires for browser Back/Forward too, and that is deliberate: those
  // change the hash, which changes `slug`, which lands the reader at the top of
  // whatever doc they went back to. Restoring the PREVIOUS offset was the
  // alternative and was rejected — the body arrives asynchronously (the fetch
  // below), so a restore would have to wait for the new content to paint and
  // would visibly jump if it guessed wrong. One rule in every direction beats a
  // rule that is right half the time. (list⇄doc needs nothing: those are
  // different component types, so React remounts and the box starts at 0.)
  const scrollBox = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (scrollBox.current) scrollBox.current.scrollTop = 0;
  }, [slug]);

  return (
    <div className="settings guide-view guide-view--doc" ref={scrollBox}>
      {/* The trail stays — its first segment is the only route back to the doc
          list. The page-level <h1> that used to sit here is GONE: the server
          derives a doc's title from its own first `# ` heading (api_docs.go
          docTitle), so that heading is rendered directly below by <Markdown>
          and the two were the same string BY CONSTRUCTION — the title was
          printed three times (trail, page h1, doc h1) before any prose. A doc
          with no heading at all falls back to its slug and is still named by
          the trail's last segment. */}
      <Breadcrumbs items={[...crumbs, { label: title || slug }]} />
      <div className="doc-card">
        {error ? (
          <p className="settings__error">{t.guide.loadError}</p>
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

/** 使用說明 — the whole tab. Owns the doc index and reads the current view off
 * the hash (`#guide` = list, `#guide/<slug>` = that doc), so the reader's place
 * survives a refresh and any doc is linkable. As a settings sub-page it kept
 * that state in a local `useState` and had no route at all; a top-level tab
 * without one would silently reset to the list on every reload.
 *
 * An unknown slug in the hash self-heals: the doc simply reports a load error
 * and the 使用說明 crumb goes back to the list. */
export function GuidePage() {
  const { t } = useI18n();
  const [route] = useHashRoute();
  const { docs, loading, error } = useDocs();
  const slug = route.page === "guide" ? route.guideSlug : undefined;

  const goList = () => navigateHash({ page: "guide" });
  const openDoc = (next: string) =>
    navigateHash({ page: "guide", guideSlug: next });

  if (slug) {
    return (
      <UserGuideDoc
        slug={slug}
        docs={docs}
        crumbs={[{ label: t.guide.title, onClick: goList }]}
        onOpenDoc={openDoc}
      />
    );
  }
  return (
    <UserGuideList
      docs={docs}
      loading={loading}
      error={error}
      onOpen={openDoc}
    />
  );
}
