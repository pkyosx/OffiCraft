import { describe, it, expect, afterEach, vi } from "vitest";
import {
  classifyBrowserError,
  credentialToJSON,
  CredentialWithoutToJSONError,
  isIpHost,
  passkeysSupported,
  toCreationOptions,
  toRequestOptions,
} from "./webauthn";

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "credentials");
});

function stubCredentials() {
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get: vi.fn() },
  });
}

describe("passkeysSupported", () => {
  it("is true only when the browser has the native JSON parsers as well as WebAuthn", () => {
    stubCredentials();
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    expect(passkeysSupported()).toBe(false);

    vi.stubGlobal("PublicKeyCredential", Object.assign(function PublicKeyCredential() {}, {
      parseCreationOptionsFromJSON: vi.fn(),
      parseRequestOptionsFromJSON: vi.fn(),
    }));
    expect(passkeysSupported()).toBe(true);
  });
});

describe("toCreationOptions / toRequestOptions", () => {
  it("hand the server's JSON to the browser's own parsers", () => {
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
    expect(parseRequestOptionsFromJSON).toHaveBeenCalledWith(json);
  });
});

describe("credentialToJSON", () => {
  it("answers the credential's own toJSON, and refuses a credential without one", () => {
    const withToJSON = { toJSON: () => ({ id: "Y3JlZA", type: "public-key" }) } as unknown as PublicKeyCredential;
    expect(credentialToJSON(withToJSON)).toEqual({ id: "Y3JlZA", type: "public-key" });

    const withoutToJSON = { id: "Y3JlZA", type: "public-key", response: {} } as unknown as PublicKeyCredential;
    expect(() => credentialToJSON(withoutToJSON)).toThrow(CredentialWithoutToJSONError);
  });
});

describe("classifyBrowserError / isIpHost", () => {
  it("reads a dismissed dialog as cancelled, an already-registered device as duplicate, and a credential without toJSON as noToJSON", () => {
    expect(classifyBrowserError(new DOMException("x", "NotAllowedError"))).toBe("cancelled");
    expect(classifyBrowserError(new DOMException("x", "InvalidStateError"))).toBe("duplicate");
    expect(classifyBrowserError(new CredentialWithoutToJSONError())).toBe("noToJSON");
    expect(classifyBrowserError(new Error("boom"))).toBe("failed");
  });

  it("treats IPv4 and bracketed IPv6 hosts as IPs and names as hosts", () => {
    expect([isIpHost("127.0.0.1"), isIpHost("[::1]"), isIpHost("localhost"), isIpHost("office.example.com")])
      .toEqual([true, true, false, false]);
  });
});
