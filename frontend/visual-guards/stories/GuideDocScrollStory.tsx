// CT story for the 使用說明 doc view's SCROLL POSITION across a doc→doc switch
// (T-68f1 · fixround5 G1).
//
// It mounts the REAL UserGuideDoc against the REAL mock adapter and the REAL
// settings.css, and owns the current slug exactly as GuidePage does in
// production — so a click on a rendered in-app link goes through the real
// resolver → onOpenDoc → new slug, which is the transition under test.
//
// The wrapper is SHORT (200px) on purpose. `.settings` is the scrolling box
// (settings.css gives it `height: 100%` + `min-height: 0` + `overflow-y:
// auto`), and `height: 100%` needs a parent with a definite height to resolve
// against — the app shell supplies that in production via .app/.app__main flex.
// A short box makes even the small mock docs overflow, which is what lets this
// guard scroll for real instead of asserting against a page that never
// scrolled.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { UserGuideDoc } from "../../src/components/UserGuidePage";

/** The mock adapter's doc index (api/mock.ts mockDocs), slug-sorted like the
 * server's listDocsFrom. Kept literal so the story states its own premise. */
const DOCS = [
  { slug: "install", title: "安裝、升級與移除" },
  { slug: "interface", title: "介面說明" },
  { slug: "why", title: "為什麼是 OffiCraft" },
];

export function GuideDocScrollStory({ start = "why" }: { start?: string }) {
  const [slug, setSlug] = useState(start);
  return (
    <I18nProvider>
      <div
        style={{ height: 200, display: "flex", flexDirection: "column" }}
        data-surface="guide-scroll"
      >
        <UserGuideDoc
          slug={slug}
          docs={DOCS}
          crumbs={[{ label: "使用說明" }]}
          onOpenDoc={setSlug}
        />
      </div>
    </I18nProvider>
  );
}
