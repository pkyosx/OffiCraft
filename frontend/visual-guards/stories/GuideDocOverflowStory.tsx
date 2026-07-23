// Story — the product-guide DOC page (使用說明 › <doc>) rendered against the REAL
// app shell + sheets, so a CT can measure it at phone width (T-23df follow-up).
//
// Owner (phone, v0.5.19): opening a guide doc made the text jump huge and the
// page slide sideways; some docs were fine, some not. Root cause is horizontal
// overflow — a doc child wider than the 390px viewport gives the whole page a
// horizontal scrollbar, and iOS Safari's -webkit-text-size-adjust then inflates
// the type unevenly. The offender was the pre-rendered architecture SVG (an
// <img> with no responsive cap), but ANY over-wide child (a raster screenshot,
// a wide table, a long code line) does the same, so the guard renders the real
// doc content driven from the real *.md.
//
// Faithfulness: the markdown text and the referenced assets are passed in as
// props (the spec reads them off disk with Node fs and hands assets over as
// data: URLs, so the images load at their true intrinsic size without a running
// server). The asset-path rewrite mirrors api_docs.go's rewriteDocAssetPaths
// (`](assets/…)` / `](./assets/…)` → the served `/api/docs/assets/…`), and the
// DOM/class chain mirrors UserGuidePage.tsx's UserGuideDoc exactly, wrapped in
// the .app › .app__main column so the measured width matches production.
import { Markdown } from "../../src/components/Markdown";
import { Breadcrumbs } from "../../src/components/Breadcrumbs";
import "../../src/styles/theme.css";
import "../../src/styles/global.css";
import "../../src/components/chrome.css";
import "../../src/components/settings.css";

const ASSET_URL_PREFIX = "/api/docs/assets/";

/** Mirror of api_docs.go rewriteDocAssetPaths: doc-relative image refs → the
 * absolute served asset endpoint (the client never sees the raw `assets/…`). */
function rewriteDocAssetPaths(md: string): string {
  return md
    .split("](./assets/")
    .join("](" + ASSET_URL_PREFIX)
    .split("](assets/")
    .join("](" + ASSET_URL_PREFIX);
}

export function GuideDocOverflowStory({
  title,
  markdown,
  assets,
}: {
  /** The doc's display title (last breadcrumb segment). */
  title: string;
  /** The doc's raw *.md source (pre-rewrite, exactly as it ships). */
  markdown: string;
  /** filename → data: URL for every asset the doc references. */
  assets: Record<string, string>;
}) {
  const source = rewriteDocAssetPaths(markdown);
  const resolveImageSrc = (src: string): string => {
    const name = src.startsWith(ASSET_URL_PREFIX)
      ? src.slice(ASSET_URL_PREFIX.length)
      : src;
    return assets[name] ?? src;
  };
  return (
    <div className="app">
      <main className="app__main">
        <div className="settings guide-view guide-view--doc">
          <Breadcrumbs items={[{ label: "使用說明" }, { label: title }]} />
          <div className="doc-card">
            <Markdown
              source={source}
              className="doc-md"
              resolveImageSrc={resolveImageSrc}
            />
          </div>
        </div>
      </main>
    </div>
  );
}
