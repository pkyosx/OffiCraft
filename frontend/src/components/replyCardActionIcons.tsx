// The one place a reply-card action picks its glyph: every surface that offers
// the same action draws it from here, so the same action always has the same icon.
export {
  ExternalLinkIcon as JumpToMessageIcon,
  ClockIcon as SnoozeActionIcon,
  UnsnoozeIcon as UnsnoozeActionIcon,
  ExpireIcon as ExpireActionIcon,
} from "./icons";
