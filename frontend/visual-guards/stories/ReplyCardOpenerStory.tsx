// CT story (T-a706): the 請示 card's 開卡人 chip as a click/keyboard target.
//
// RepliesPage owns live hooks (useMembers/useReplyCards/useHashRoute) that
// this story deliberately does NOT wire up — same philosophy as
// OfficeSidebarStory: reproduce the REAL DOM skeleton (.card-meta)
// against the REAL replies.css, but mount the REAL ReplyCardOpenerChip
// component RepliesPage.tsx itself renders (not a hand-copied approximation
// of its markup — a mutant in the real component must be able to redden
// this guard). What this guards is the property jsdom cannot see: does a
// REAL browser actually deliver a click/keyboard activation to this button
// once real CSS/layout is in play (an overlapping element or a stray
// pointer-events:none would pass jsdom's fireEvent.click but fail here), and
// is the focus-visible ring actually painted.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ReplyCardOpenerChip } from "../../src/components/ReplyCardOpenerChip";
import "../../src/components/replies.css";
import "../../src/components/card-meta.css";

export function ReplyCardOpenerStory() {
  const [openCount, setOpenCount] = useState(0);
  return (
    <I18nProvider>
      <div data-testid="open-count">{openCount}</div>
      <div className="reply-card" style={{ width: 360 }}>
        <div className="card-meta">
          <span className="card-meta__label">開卡人</span>
          <ReplyCardOpenerChip onClick={() => setOpenCount((n) => n + 1)} name="Mira" />
        </div>
      </div>
    </I18nProvider>
  );
}
