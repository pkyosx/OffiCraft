// hooks/useSigningKeys.ts — the signing-key ring behind 設定 › 簽章金鑰 (T-62).
//
// Read on mount, then MUTATED BY THE TWO BUTTONS. Deliberately not a poller:
// the ring changes only when a person presses something here, and both actions
// answer with the ring as it stands afterwards, so there is never a window
// where the card shows a state the server has moved past.
//
// 🔴 A FAILED ACTION MUST NOT LOOK LIKE A DONE ONE. Rotation and removal are
// exactly the kind of thing someone presses once, sees no complaint, and
// believes: `error` carries the server's refusal and the card renders it, and a
// failure leaves the previous ring in place rather than blanking the list (a
// list that emptied itself would read as "the keys are gone").

import { useCallback, useEffect, useState } from "react";
import type { SigningKeyView } from "../types";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";

interface UseSigningKeys {
  keys: SigningKeyView[];
  loading: boolean;
  /** True while a rotate/remove is in flight — the buttons disable on it so a
   * double press cannot mint two keys. */
  busy: boolean;
  /** The last failure's message, or "" — server text, shown as-is. */
  error: string;
  rotate: () => void;
  remove: (keyId: string) => void;
}

export function useSigningKeys(fallbackMessage: string): UseSigningKeys {
  const [keys, setKeys] = useState<SigningKeyView[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    api
      .getSigningKeys()
      .then((next) => {
        if (alive) {
          setKeys(next);
          setError("");
        }
      })
      .catch((e: unknown) => {
        console.warn("useSigningKeys: load failed", e);
        if (alive) setError(messageOf(e, fallbackMessage));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [fallbackMessage]);

  // Both actions share one runner because they share the property that matters:
  // the answer IS the new ring, and a rejection must leave the old one alone.
  const run = useCallback((action: () => Promise<SigningKeyView[]>) => {
    setBusy(true);
    setError("");
    action()
      .then((next) => setKeys(next))
      .catch((e: unknown) => setError(messageOf(e, fallbackMessage)))
      .finally(() => setBusy(false));
  }, [fallbackMessage]);

  const rotate = useCallback(() => run(() => api.rotateSigningKey()), [run]);
  const remove = useCallback(
    (keyId: string) => run(() => api.removeSigningKey(keyId)),
    [run],
  );

  return { keys, loading, busy, error, rotate, remove };
}

// 🔴 THE SERVER'S REASON, NOT THE LOG LINE. `ApiError.message` is the
// `http <status> for <METHOD> <path>` format, which api/errors.ts explicitly
// says is "not readable copy" — rendering it would show a Chinese cockpit
// `http 409 for POST /api/auth/signing-keys/k-…/remove` and throw away the
// server's actual "rotate first, then remove it". serverMessageOf carries the
// reason; "" means the rejection had none, and the caller falls back to its own
// copy rather than showing an empty line.
//
// (The first version of this hook used `e.message`, and the mock hid it by
// throwing a plain Error whose message WAS the prose — so mock mode looked
// right while the real path was wrong. Found by independent review.)
function messageOf(e: unknown, fallback: string): string {
  return serverMessageOf(e) || fallback;
}
