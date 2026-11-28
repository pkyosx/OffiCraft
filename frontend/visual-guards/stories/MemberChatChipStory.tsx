// CT story: the member chip that opens a chat (負責人、建立者、前任、開卡人、撰寫人
// on every card), mounted as the REAL MemberChatChip inside a card's label/value
// rows with the real stylesheets, so a mutant in the component or in
// card-meta.css can redden the guard.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { MemberChatChip } from "../../src/components/MemberChip";
import "../../src/components/replies.css";
import "../../src/components/card-meta.css";

export function MemberChatChipStory() {
  const [openCount, setOpenCount] = useState(0);
  return (
    <I18nProvider>
      <div data-testid="open-count">{openCount}</div>
      <div className="reply-card" style={{ width: 360 }}>
        <div className="card-meta">
          <span className="card-meta__label">開卡人</span>
          <MemberChatChip
            name="Mira"
            avatar={{ kind: "staff" }}
            label="傳訊息給開卡人"
            onOpen={() => setOpenCount((n) => n + 1)}
            testId="member-chip"
          />
        </div>
      </div>
    </I18nProvider>
  );
}
