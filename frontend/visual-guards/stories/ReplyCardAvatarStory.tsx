// CT story (T-a706): the 請示頁卡片 header avatar as a click/keyboard target.
//
// RepliesPage owns live hooks (useMembers/useReplyCards/useHashRoute) that
// this story deliberately does NOT wire up — same philosophy as
// OfficeSidebarStory: reproduce the REAL DOM skeleton (.reply-card__head)
// against the REAL replies.css, but mount the REAL ReplyCardAvatarButton
// component RepliesPage.tsx itself renders (not a hand-copied approximation
// of its markup — a mutant in the real component must be able to redden
// this guard). What this guards is the property jsdom cannot see: does a
// REAL browser actually deliver a click/keyboard activation to this button
// once real CSS/layout is in play (an overlapping element or a stray
// pointer-events:none would pass jsdom's fireEvent.click but fail here), and
// is the focus-visible ring actually painted.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ReplyCardAvatarButton } from "../../src/components/ReplyCardAvatarButton";
import "../../src/components/replies.css";

export function ReplyCardAvatarStory() {
  const [openCount, setOpenCount] = useState(0);
  return (
    <I18nProvider>
      <div data-testid="open-count">{openCount}</div>
      <div className="reply-card" style={{ width: 360 }}>
        <header className="reply-card__head">
          <ReplyCardAvatarButton onClick={() => setOpenCount((n) => n + 1)} />
          <div className="reply-card__who">
            <span className="reply-card__name">Mira</span>
            <span className="reply-card__role">特助</span>
          </div>
        </header>
      </div>
    </I18nProvider>
  );
}
