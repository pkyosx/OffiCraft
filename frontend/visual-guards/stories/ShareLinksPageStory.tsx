// CT story: the REAL ShareLinksPage under the app shell's ancestor chain
// (`.app` → `main.app__main` with its 22px gutters), fed by the mock api.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ShareLinksPage } from "../../src/components/ShareLinksPage";
import { LONG_FILENAME } from "./shareLinksFixtures";

const DAY = 86_400;

export function ShareLinksPageStory() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    // Dynamic import: a static one makes Playwright's node-side transform parse
    // mock.ts's `?raw` imports as JavaScript and the spec fails to collect.
    void (async () => {
      const { mockApi, __resetMock } = await import("../../src/api/mock");
      __resetMock();
      const now = Math.floor(Date.now() / 1000);
      const longId = await mockApi.uploadAttachment(new Blob(["x"]), LONG_FILENAME);
      await mockApi.createAttachmentShareLink(longId, now + 365 * DAY - 60);
      await mockApi.createAttachmentShareLink("att-5d0c11a2b3c4", now + 365 * DAY - 60);
      await mockApi.createDiffShareLink(
        { before: "att-5d0c11a2b3b0", after: "att-5d0c11a2b3c4", labelBefore: "v1", labelAfter: "v2" },
        now + 7 * DAY,
      );
      await mockApi.createAttachmentShareLink("att-5d0c11a2b3b0", null);
      setReady(true);
    })();
  }, []);
  if (!ready) return null;
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <ShareLinksPage />
        </main>
      </div>
    </I18nProvider>
  );
}
