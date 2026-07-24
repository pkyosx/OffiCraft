// T-3738 CT fixtures — DISTINCT valid PNG data URIs (1×1, different colour) so
// the avatar-kind guard can assert which kind's image actually painted (the real
// <img src>), not merely that "an image exists". T-ea81 adds ASSISTANT_IMG: with
// the per-role avatar split, a role==="assistant" member (e.g. Mira) now paints
// the assistant image, distinct from the general-正職 member image.
export const MEMBER_IMG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgqjgBAAHaAUsQ+/EAAAAAAElFTkSuQmCC";
export const OUTSOURCE_IMG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGM4EcUFAAMaAS0191t5AAAAAElFTkSuQmCC";
export const ASSISTANT_IMG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPQO1MIAAKXAWy8o8h8AAAAAElFTkSuQmCC";
