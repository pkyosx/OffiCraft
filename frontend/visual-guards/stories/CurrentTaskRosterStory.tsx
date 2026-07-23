// CT story: the T-3451 current-task-title line on the Outsource roster ROW.
// An Outsource row (long title) sits in the fixed-width rail (.office__members)
// and must clamp to two lines there. Mounts the REAL component so the loaded
// office.css governs the clamp/overflow the guard measures (jsdom resolves no
// -webkit-line-clamp box).
import { I18nProvider } from "../../src/i18n";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { mkWorker } from "./currentTaskFixtures";

export function CurrentTaskRosterStory() {
  return (
    <I18nProvider>
      <div className="office" style={{ height: 640 }}>
        <aside className="office__members">
          <OutsourcePanel
            workers={[mkWorker({})]}
            error={false}
            maxParallel={10}
            selectedId=""
            onOpenChat={() => {}}
            onOpenDetail={() => {}}
            onOpenTask={() => {}}
          />
        </aside>
        <section className="office__chat" />
      </div>
    </I18nProvider>
  );
}
