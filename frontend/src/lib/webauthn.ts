// Browser side of a passkey ceremony: the server's options JSON in, the
// credential JSON out, through the browser's own WebAuthn L3 JSON methods only.
// 🔴 No hand-rolled base64url fallback: a browser without these methods is told
// to update (passkeysSupported is false), and a credential without toJSON (some
// password-manager extensions) is a visible error, never a silent second path.

type Json = Record<string, unknown>;

/** Whether this browser can run a passkey ceremony with the native JSON methods. */
export function passkeysSupported(): boolean {
  if (typeof window === "undefined") return false;
  const PKC = window.PublicKeyCredential as Partial<typeof PublicKeyCredential> | undefined;
  return (
    typeof window.PublicKeyCredential === "function" &&
    typeof PKC?.parseCreationOptionsFromJSON === "function" &&
    typeof PKC?.parseRequestOptionsFromJSON === "function" &&
    typeof navigator.credentials?.create === "function" &&
    typeof navigator.credentials?.get === "function"
  );
}

/** WebAuthn refuses IP-address origins, so an IP host never offers passkeys. */
export function isIpHost(hostname: string): boolean {
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(hostname) || hostname.includes(":") || hostname.startsWith("[");
}

export function toCreationOptions(json: Json): PublicKeyCredentialCreationOptions {
  return PublicKeyCredential.parseCreationOptionsFromJSON(json as unknown as PublicKeyCredentialCreationOptionsJSON);
}

export function toRequestOptions(json: Json): PublicKeyCredentialRequestOptions {
  return PublicKeyCredential.parseRequestOptionsFromJSON(json as unknown as PublicKeyCredentialRequestOptionsJSON);
}

export class CredentialWithoutToJSONError extends Error {
  constructor() {
    super("the credential the browser returned has no toJSON()");
    this.name = "CredentialWithoutToJSONError";
  }
}

export function credentialToJSON(cred: PublicKeyCredential): Json {
  if (typeof cred.toJSON !== "function") throw new CredentialWithoutToJSONError();
  return cred.toJSON() as Json;
}

/** The ways a ceremony ends without a credential, as the browser reports them. */
export type PasskeyBrowserOutcome = "cancelled" | "duplicate" | "noToJSON" | "failed";

export function classifyBrowserError(err: unknown): PasskeyBrowserOutcome {
  const name = (err as { name?: unknown })?.name;
  // NotAllowedError covers both "the owner dismissed the dialog" and a
  // timeout; browsers deliberately do not tell the page which.
  if (name === "NotAllowedError" || name === "AbortError") return "cancelled";
  if (name === "InvalidStateError") return "duplicate";
  if (name === "CredentialWithoutToJSONError") return "noToJSON";
  return "failed";
}

export async function createPasskeyCredential(options: Json): Promise<Json> {
  const cred = (await navigator.credentials.create({
    publicKey: toCreationOptions(options),
  })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("no credential", "NotAllowedError");
  return credentialToJSON(cred);
}

export async function getPasskeyAssertion(options: Json): Promise<Json> {
  const cred = (await navigator.credentials.get({
    publicKey: toRequestOptions(options),
  })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("no credential", "NotAllowedError");
  return credentialToJSON(cred);
}

/** The address the owner should open instead of an IP host, keeping the port. */
export function localhostAlternative(loc: Pick<Location, "port"> = window.location): string {
  return `http://localhost${loc.port ? `:${loc.port}` : ""}`;
}
