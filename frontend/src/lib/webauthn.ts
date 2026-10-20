// Browser side of a passkey ceremony: the server's options JSON in, the
// credential JSON out. The native WebAuthn L3 JSON methods are used where the
// browser has them; the fallback below covers browsers that predate them
// (they arrived in Chrome/Edge 129, Firefox 119, Safari 18.4) and credential
// objects some password-manager extensions hand back without `toJSON`.

type Json = Record<string, unknown>;

/** Whether this page can run a passkey ceremony at all. */
export function passkeysSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.PublicKeyCredential === "function" &&
    typeof navigator.credentials?.create === "function"
  );
}

/** WebAuthn refuses IP-address origins, so an IP host never offers passkeys. */
export function isIpHost(hostname: string): boolean {
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(hostname) || hostname.includes(":") || hostname.startsWith("[");
}

export function base64urlToBytes(s: string): Uint8Array<ArrayBuffer> {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function bytesToBase64url(buf: ArrayBuffer | ArrayBufferView): string {
  const bytes = ArrayBuffer.isView(buf)
    ? new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
    : new Uint8Array(buf);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function decodeDescriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) return undefined;
  return list.map((d: Json) => ({
    ...(d as object),
    id: base64urlToBytes(String(d.id)),
  })) as unknown as PublicKeyCredentialDescriptor[];
}

function nativeParsers(): {
  creation?: (o: PublicKeyCredentialCreationOptionsJSON) => PublicKeyCredentialCreationOptions;
  request?: (o: PublicKeyCredentialRequestOptionsJSON) => PublicKeyCredentialRequestOptions;
} {
  const PKC = window.PublicKeyCredential as Partial<typeof PublicKeyCredential> | undefined;
  return {
    creation:
      typeof PKC?.parseCreationOptionsFromJSON === "function"
        ? PKC.parseCreationOptionsFromJSON.bind(PKC)
        : undefined,
    request:
      typeof PKC?.parseRequestOptionsFromJSON === "function"
        ? PKC.parseRequestOptionsFromJSON.bind(PKC)
        : undefined,
  };
}

export function toCreationOptions(json: Json): PublicKeyCredentialCreationOptions {
  const native = nativeParsers().creation;
  if (native) return native(json as unknown as PublicKeyCredentialCreationOptionsJSON);
  const user = json.user as Json;
  return {
    ...(json as object),
    challenge: base64urlToBytes(String(json.challenge)),
    user: { ...(user as object), id: base64urlToBytes(String(user.id)) },
    excludeCredentials: decodeDescriptors(json.excludeCredentials),
  } as unknown as PublicKeyCredentialCreationOptions;
}

export function toRequestOptions(json: Json): PublicKeyCredentialRequestOptions {
  const native = nativeParsers().request;
  if (native) return native(json as unknown as PublicKeyCredentialRequestOptionsJSON);
  return {
    ...(json as object),
    challenge: base64urlToBytes(String(json.challenge)),
    allowCredentials: decodeDescriptors(json.allowCredentials),
  } as unknown as PublicKeyCredentialRequestOptions;
}

export function credentialToJSON(cred: PublicKeyCredential): Json {
  if (typeof cred.toJSON === "function") return cred.toJSON() as Json;
  const r = cred.response;
  const response: Json = { clientDataJSON: bytesToBase64url(r.clientDataJSON) };
  if ("attestationObject" in r) {
    const att = r as AuthenticatorAttestationResponse;
    response.attestationObject = bytesToBase64url(att.attestationObject);
    if (typeof att.getTransports === "function") response.transports = att.getTransports();
  } else {
    const asr = r as AuthenticatorAssertionResponse;
    response.authenticatorData = bytesToBase64url(asr.authenticatorData);
    response.signature = bytesToBase64url(asr.signature);
    if (asr.userHandle) response.userHandle = bytesToBase64url(asr.userHandle);
  }
  return {
    id: cred.id,
    rawId: bytesToBase64url(cred.rawId),
    type: cred.type,
    response,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults?.() ?? {},
  };
}

/** The ways a ceremony ends without a credential, as the browser reports them. */
export type PasskeyBrowserOutcome = "cancelled" | "duplicate" | "failed";

export function classifyBrowserError(err: unknown): PasskeyBrowserOutcome {
  const name = (err as { name?: unknown })?.name;
  // NotAllowedError covers both "the owner dismissed the dialog" and a
  // timeout; browsers deliberately do not tell the page which.
  if (name === "NotAllowedError" || name === "AbortError") return "cancelled";
  if (name === "InvalidStateError") return "duplicate";
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
