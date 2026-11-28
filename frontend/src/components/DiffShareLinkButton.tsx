// The comparison's share button. Drawn only where a session is certain: the
// mint, list and revoke routes are owner-gated, and the reader of an external
// link must not be handed a way to re-mint the link they were sent. Its hosts
// place it only where that is structural — see the note at each call site.

import type { DiffParams } from "../lib/diffLink";
import { diffTargetName } from "../lib/shareLink";
import { ShareLinkMenu } from "./ShareLinkMenu";

export function DiffShareLinkButton({
  params,
  className,
}: {
  params: DiffParams;
  className: string;
}) {
  return (
    <ShareLinkMenu
      target={{ kind: "diff", params }}
      targetName={diffTargetName(params.before, params.after, params.labelBefore, params.labelAfter)}
      className={className}
      testId="diff-share-link"
    />
  );
}
