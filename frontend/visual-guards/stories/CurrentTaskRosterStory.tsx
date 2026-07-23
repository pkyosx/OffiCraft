// CT story: the T-3451 current-task-title line on the roster ROW, both tabs.
// A Staff MemberCard (long title + one empty) AND an Outsource row (long title)
// share the SAME fixed-width rail (.office__members) — both must clamp to two
// lines there. Mounts the REAL components so the loaded office.css governs the
// clamp/overflow the guard measures (jsdom resolves no -webkit-line-clamp box).
import { I18nProvider } from "../../src/i18n";
import { MemberCard } from "../../src/components/MemberCard";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { LONG_TITLE, mkMember, mkWorker } from "./currentTaskFixtures";

export function CurrentTaskRosterStory() {
  return (
    <I18nProvider>
      <div className="office" style={{ height: 640 }}>
        <aside className="office__members">
          <div className="office__members-list">
            <MemberCard
              member={mkMember({ id: "mira", name: "Mira" })}
              selected={false}
              currentTaskTitle={LONG_TITLE}
              onOpenDetail={() => {}}
              onChat={() => {}}
            />
            <MemberCard
              member={mkMember({ id: "kyle", name: "Kyle" })}
              selected={false}
              currentTaskTitle=""
              onOpenDetail={() => {}}
              onChat={() => {}}
            />
          </div>
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
