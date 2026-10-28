// CT story: the office rail's 外包 worker LIST rows in their REAL layout.
//
// Line 1 of an outsource row lives in the SAME fixed-width rail as the roster
// (.office__members, a 264px grid track). jsdom resolves no grid, so a row that
// spilled past the rail's right edge, or forced a page-level horizontal
// scrollbar, is invisible to the unit suite — this story reproduces the real
// rail skeleton (the classes OfficePage emits) and fills it with the REAL
// <OutsourcePanel>.
import { I18nProvider } from "../../src/i18n";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import type { OutsourceWorkerView } from "../../src/api/adapter";

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  return {
    id: "ow-1",
    codename: "O-30",
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: "t-3ed85291a010",
    taskTitle: "聊天寄件者標籤顯示外包代號",
    taskStatus: "in_progress",
    taskNo: "t-3ed85291a010",
    taskTypeName: "OC 開發",
    presence: "online",
    ...over,
  };
}

const workers: OutsourceWorkerView[] = [
  mkWorker({ id: "ow-1", codename: "O-30" }),
  mkWorker({ id: "ow-2", codename: "H-12", taskNo: "t-9c215291a014", taskTypeName: "review-pr" }),
];

export function OutsourceRowLabelStory() {
  return (
    <I18nProvider>
      <div className="office" style={{ height: 480 }}>
        <aside className="office__members">
          <OutsourcePanel
            workers={workers}
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
