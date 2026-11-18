// One retained version's content, for the version readers (task artifacts and
// the 檔案庫). The bytes go through ContentView like everywhere else a file is
// shown: markdown rendered, JSON / XML indented, CSV a table, HTML its page in
// the sandboxed frame at its minted share link. Each reader keeps its own
// wording for the states that are about the version rather than the bytes.

import type { ReactNode } from "react";
import { authedAttachmentUrl } from "../api/http";
import { useShareHref } from "../hooks/useShareHref";
import type { ArtifactPayload } from "../hooks/useVersionPayload";
import { attachmentLinkFromHref } from "../lib/attachmentLink";
import { isInlineDisplayableMime } from "./MarkdownPreviewOverlay";
import { ContentView } from "./ContentView";
import "./task-artifact-versions.css";

export type VersionNotices = {
  loading: ReactNode;
  error: ReactNode;
  gone: ReactNode;
  opaque: (mime: string) => ReactNode;
};

function shareableUrl(p: ArtifactPayload): string | undefined {
  if (p.state === "text" && p.kind === "html") return p.url;
  if (p.state === "opaque" && isInlineDisplayableMime(p.mime)) return p.url;
  return undefined;
}

export function VersionContent({
  payload,
  notices,
  noticeClassName,
  label,
  testId,
}: {
  payload: ArtifactPayload;
  notices: VersionNotices;
  noticeClassName: string;
  label?: string;
  testId: string;
}) {
  const shareUrl = shareableUrl(payload);
  const share = useShareHref(
    shareUrl === undefined ? undefined : attachmentLinkFromHref(shareUrl)?.attachmentId,
    shareUrl !== undefined,
    "VersionContent",
  );
  const notice = (text: ReactNode, state: string) => (
    <p className={noticeClassName} data-testid={`${testId}-${state}`}>
      {text}
    </p>
  );
  switch (payload.state) {
    case "loading":
      return notice(notices.loading, "loading");
    case "error":
      return notice(notices.error, "error");
    case "gone":
      return notice(notices.gone, "gone");
    case "link":
      return (
        <a
          className="ta-versions__url"
          data-testid={`${testId}-link`}
          href={payload.url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {payload.url}
        </a>
      );
    case "image":
      return <ContentView kind="image" src={payload.src} label={label} testId={`${testId}-image`} />;
    case "opaque":
      return share.href === null ? (
        notice(notices.opaque(payload.mime), "opaque")
      ) : (
        <ContentView kind="binary" shareHref={share.href} newTabLink testId={`${testId}-opaque`} />
      );
    case "text":
      return (
        <ContentView
          kind={payload.kind}
          text={payload.text}
          label={label}
          shareHref={share.href}
          shareFailed={share.failed}
          resolveImageSrc={authedAttachmentUrl}
          testId={`${testId}-text`}
        />
      );
  }
}
