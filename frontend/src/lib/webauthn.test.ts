import { describe, it, expect, afterEach, vi } from "vitest";
import {
  bytesToBase64url,
  base64urlToBytes,
  classifyBrowserError,
  credentialToJSON,
  isIpHost,
  toCreationOptions,
  toRequestOptions,
} from "./webauthn";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: ArrayBuffer | Uint8Array) => new TextDecoder().decode(b);

afterEach(() => vi.unstubAllGlobals());

describe("toCreationOptions / toRequestOptions", () => {
  it("prefer the browser's own JSON parsers when it has them", () => {
    const parsed = { marker: "native" };
    const parseCreationOptionsFromJSON = vi.fn(() => parsed);
    const parseRequestOptionsFromJSON = vi.fn(() => parsed);
    vi.stubGlobal("PublicKeyCredential", Object.assign(function PublicKeyCredential() {}, {
      parseCreationOptionsFromJSON,
      parseRequestOptionsFromJSON,
    }));
    const json = { challenge: "AAEC", user: { id: "AQ" } };

    expect(toCreationOptions(json)).toBe(parsed);
    expect(toRequestOptions(json)).toBe(parsed);
    expect(parseCreationOptionsFromJSON).toHaveBeenCalledWith(json);
  });

  it("without them, decode every base64url binary field and keep the rest", () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    const creation = toCreationOptions({
      rp: { id: "localhost", name: "OffiCraft" },
      challenge: bytesToBase64url(enc("chal-1")),
      user: { id: bytesToBase64url(enc("handle")), name: "owner", displayName: "owner" },
      excludeCredentials: [{ type: "public-key", id: bytesToBase64url(enc("old-cred")) }],
    });
    expect(dec(creation.challenge as Uint8Array)).toBe("chal-1");
    expect(dec(creation.user.id as Uint8Array)).toBe("handle");
    expect(creation.user.name).toBe("owner");
    expect(creation.rp).toEqual({ id: "localhost", name: "OffiCraft" });
    expect(dec(creation.excludeCredentials![0].id as Uint8Array)).toBe("old-cred");

    const request = toRequestOptions({ challenge: bytesToBase64url(enc("chal-2")), allowCredentials: [], rpId: "localhost" });
    expect(dec(request.challenge as Uint8Array)).toBe("chal-2");
    expect(request.allowCredentials).toEqual([]);
    expect(request.rpId).toBe("localhost");
  });
});

describe("base64url", () => {
  it("round-trips bytes that use the URL-safe alphabet and no padding", () => {
    const bytes = new Uint8Array([0xfb, 0xff, 0xbf, 0x00, 0x01]);
    expect(bytesToBase64url(bytes)).toBe("-_-_AAE");
    expect(Array.from(base64urlToBytes("-_-_AAE"))).toEqual(Array.from(bytes));
  });
});

describe("credentialToJSON", () => {
  it("serialises an assertion by hand when the credential has no toJSON", () => {
    const cred = {
      id: "Y3JlZA",
      rawId: enc("cred").buffer,
      type: "public-key",
      authenticatorAttachment: "platform",
      getClientExtensionResults: () => ({}),
      response: {
        clientDataJSON: enc("cd").buffer,
        authenticatorData: enc("ad").buffer,
        signature: enc("sig").buffer,
        userHandle: enc("uh").buffer,
      },
    } as unknown as PublicKeyCredential;

    expect(credentialToJSON(cred)).toEqual({
      id: "Y3JlZA",
      rawId: "Y3JlZA",
      type: "public-key",
      authenticatorAttachment: "platform",
      clientExtensionResults: {},
      response: { clientDataJSON: "Y2Q", authenticatorData: "YWQ", signature: "c2ln", userHandle: "dWg" },
    });
  });
});

describe("classifyBrowserError / isIpHost", () => {
  it("reads a dismissed dialog as cancelled and an already-registered device as duplicate", () => {
    expect(classifyBrowserError(new DOMException("x", "NotAllowedError"))).toBe("cancelled");
    expect(classifyBrowserError(new DOMException("x", "InvalidStateError"))).toBe("duplicate");
    expect(classifyBrowserError(new Error("boom"))).toBe("failed");
  });

  it("treats IPv4 and bracketed IPv6 hosts as IPs and names as hosts", () => {
    expect([isIpHost("127.0.0.1"), isIpHost("[::1]"), isIpHost("localhost"), isIpHost("office.example.com")])
      .toEqual([true, true, false, false]);
  });
});
