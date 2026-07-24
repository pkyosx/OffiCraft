// T-3738 CT fixtures — two DISTINCT valid PNG data URIs (1×1, different colour)
// so the avatar-kind guard can assert which kind's image actually painted (the
// real <img src>), not merely that "an image exists".
export const MEMBER_IMG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgqjgBAAHaAUsQ+/EAAAAAAElFTkSuQmCC";
export const OUTSOURCE_IMG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGM4EcUFAAMaAS0191t5AAAAAElFTkSuQmCC";
