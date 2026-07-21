// CT story for the 使用說明 doc page's IN-APP links (T-68f1). It mounts the
// REAL UserGuideDoc against the REAL mock adapter (api/index defaults to
// mockApi) and the REAL settings.css, and owns the one thing the page's parent
// owns in production: the current slug. So a click on a rendered doc link
// exercises the whole chain — Markdown's resolveDocLink branch → the page's
// slug mapping + existence check → onOpenDoc → a new api.getDoc — in a real
// browser, which is where "is it actually clickable / does it look like a
// link" is decidable at all (jsdom has no layout and no CSS).
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

export function GuideDocLinksStory({ start = "why" }: { start?: string }) {
  const [slug, setSlug] = useState(start);
  return (
    <I18nProvider>
      <div data-surface="guide-doc">
        <UserGuideDoc
          slug={slug}
          docs={DOCS}
          crumbs={[{ label: "設定" }, { label: "使用說明" }]}
          onOpenDoc={setSlug}
        />
      </div>
    </I18nProvider>
  );
}
