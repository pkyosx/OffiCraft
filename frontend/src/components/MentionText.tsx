import { splitMentions, type MentionTarget } from "../lib/mentionLinks";
import type { KnownMention, MentionOpener } from "../hooks/useMentionOpener";
import "./chat-mention.css";

export function MentionButton({
  target,
  known,
}: {
  target: MentionTarget;
  known: KnownMention;
}) {
  return (
    <button
      type="button"
      className="md-mention"
      data-mention-kind={target.kind}
      onClick={known.open}
    >
      @{known.name}
    </button>
  );
}

/** Text typed in a composer (not Markdown) that may carry mention links: a link
 * `opener` knows becomes the clickable roster name, any other one its written
 * `@name`. */
export function MentionText({
  text,
  opener,
}: {
  text: string;
  opener: MentionOpener | null;
}) {
  return (
    <>
      {splitMentions(text).map((piece, i) => {
        if (typeof piece === "string") return piece;
        const known = opener?.(piece) ?? null;
        return known ? (
          <MentionButton key={i} target={piece} known={known} />
        ) : (
          piece.label
        );
      })}
    </>
  );
}
