// hooks/useUpgradeInstructions.ts — the 換版交代單 set behind 設定 › 換版交代單
// (T-79): the standing instructions the owner leaves for the assistant, handed
// to her in a chat message at every station upgrade until somebody ticks one
// off.
//
// Read on mount, then MUTATED BY THE CARD'S THREE ACTIONS. Deliberately not a
// poller and deliberately not subscribed: this package ships no SSE topic for
// instructions, so the assistant ticking one off does NOT reach an open
// cockpit — the owner sees it on the next load. That is a stated limit of the
// package, not an oversight, and the card says so on screen rather than letting
// a stale list pass for a live one.
//
// 🔴 A FAILED ACTION MUST NOT LOOK LIKE A DONE ONE. Writing an instruction is
// exactly the kind of thing someone does once, sees no complaint, and believes
// — and the cost of believing it is that the assistant never gets told. `error`
// carries the server's own refusal, and a failure leaves the list exactly as it
// was rather than blanking it (an emptied list would read as "there are none").

import { useCallback, useEffect, useState } from "react";
import type { UpgradeInstructionView } from "../types";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";

interface UseUpgradeInstructions {
  instructions: UpgradeInstructionView[];
  /** How many are still open — the server's own count, never recomputed from
   * the array. It is the number that makes this feature's only failure mode
   * visible: an instruction nobody ever acts on. */
  openCount: number;
  loading: boolean;
  /** True while a create/tick/withdraw is in flight; the controls disable on it
   * so a double press cannot write the same instruction twice. */
  busy: boolean;
  /** The last failure's message, or "" — server text, shown as-is. */
  error: string;
  /** Resolves true when the instruction was written, so the caller knows
   * whether to clear its draft. Clearing on a rejection would lose what the
   * owner typed with nothing to show for it. */
  create: (body: string) => Promise<boolean>;
  markDone: (instructionId: string) => void;
  remove: (instructionId: string) => void;
}

export function useUpgradeInstructions(
  fallbackMessage: string,
): UseUpgradeInstructions {
  const [instructions, setInstructions] = useState<UpgradeInstructionView[]>([]);
  const [openCount, setOpenCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(
    (alive: () => boolean) =>
      api
        .getUpgradeInstructions()
        .then((next) => {
          if (!alive()) return;
          setInstructions(next.instructions);
          setOpenCount(next.openCount);
          setError("");
        })
        .catch((e: unknown) => {
          console.warn("useUpgradeInstructions: load failed", e);
          if (alive()) setError(messageOf(e, fallbackMessage));
        }),
    [fallbackMessage],
  );

  useEffect(() => {
    let alive = true;
    load(() => alive).finally(() => {
      if (alive) setLoading(false);
    });
    return () => {
      alive = false;
    };
  }, [load]);

  // 🔴 EVERY MUTATION REFETCHES INSTEAD OF SPLICING ITS OWN ANSWER IN, and the
  // reason is the tick: the server answers with the row as it stands after
  // whichever call WON the race, and it may also have moved `open_count` for
  // reasons this client did not cause (the assistant ticking from her side).
  // Splicing the response into the old array would keep a count this client
  // computed from a list it no longer knows to be whole.
  const run = useCallback(
    (action: () => Promise<unknown>): Promise<boolean> => {
      setBusy(true);
      setError("");
      return action()
        .then(() => load(() => true).then(() => true))
        .catch((e: unknown) => {
          setError(messageOf(e, fallbackMessage));
          return false;
        })
        .finally(() => setBusy(false));
    },
    [load, fallbackMessage],
  );

  const create = useCallback(
    (body: string) => run(() => api.createUpgradeInstruction(body)),
    [run],
  );
  const markDone = useCallback(
    (instructionId: string) => {
      void run(() => api.markUpgradeInstructionDone(instructionId));
    },
    [run],
  );
  const remove = useCallback(
    (instructionId: string) => {
      void run(() => api.deleteUpgradeInstruction(instructionId));
    },
    [run],
  );

  return { instructions, openCount, loading, busy, error, create, markDone, remove };
}

// 🔴 THE SERVER'S REASON, NOT THE LOG LINE. `ApiError.message` is the
// `http <status> for <METHOD> <path>` format, which api/errors.ts explicitly
// says is "not readable copy": rendering it would show
// `http 422 for POST /api/upgrade-instructions` and throw away the server's
// actual "body must not be blank — a blank instruction is handed over at every
// upgrade while saying nothing". "" means the rejection carried no reason, and
// the caller falls back to its own copy rather than showing an empty line.
function messageOf(e: unknown, fallback: string): string {
  return serverMessageOf(e) || fallback;
}
