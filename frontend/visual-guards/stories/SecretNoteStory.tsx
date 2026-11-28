// CT story: the REAL SecretsPage against the mock adapter, inside
// `.app > .app__main` as in App.tsx, with one more secret whose note runs well
// past three lines at any width and one with no note; neither has been read.
// The seed's own note is one short line.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { SecretsPage } from "../../src/components/SecretsPage";
import "../../src/components/chrome.css";

export const LONG_NOTE =
  "Rotate every quarter; the staging copy shares nothing with production. ".repeat(8).trim();

export function SecretNoteStory() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    // Dynamic import: a static one makes Playwright's node-side transform parse
    // mock.ts's `?raw` imports as JavaScript.
    void (async () => {
      const { mockApi, __resetMock } = await import("../../src/api/mock");
      __resetMock();
      await mockApi.createSecret({ name: "Staging DB", value: "fake", note: LONG_NOTE });
      await mockApi.createSecret({ name: "SETH_M5_SSH_PASSPHRASE_FOR_THE_STAGING_BASTION_HOST", value: "fake", note: "" });
      setReady(true);
    })();
  }, []);
  if (!ready) return null;
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <SecretsPage />
        </main>
      </div>
    </I18nProvider>
  );
}
