// The attachment's share link (`?sig=`), minted when `enabled` — the address
// an HTML page's frame loads and a 「在新頁面顯示」 anchor opens. Minted ahead
// of any click: a tab opened after an await is eaten by popup blockers.
// `href` is null until the mint lands and stays null when it fails, so no
// caller can render a link that would 404.

import { useEffect, useState } from "react";
import { attachmentShareLinkUrl } from "../lib/shareLink";

export function useShareHref(
  attachmentId: string | undefined,
  enabled: boolean,
  warnAs: string,
): { href: string | null; failed: boolean } {
  const [href, setHref] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    setHref(null);
    setFailed(false);
    if (attachmentId === undefined || attachmentId === "" || !enabled) return;
    let alive = true;
    attachmentShareLinkUrl(attachmentId)
      .then((next) => {
        if (alive) setHref(next);
      })
      .catch((e) => {
        console.warn(`${warnAs}: share link failed`, e);
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [attachmentId, enabled, warnAs]);
  return { href, failed };
}
