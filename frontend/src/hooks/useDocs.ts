// hooks/useDocs.ts — the 使用說明 nav tab (product guide): the embedded doc list.
// The docs are baked into the binary (server docsdist embed), so there is no
// SSE topic and no mutation — a one-shot fetch of the slug+title index. Per-doc
// content is fetched on selection by the page (api.getDoc), the same read Mira
// makes through the get_doc MCP tool.

import { useEffect, useState } from "react";
import type { DocSummaryView } from "../api/adapter";
import { api } from "../api";

interface UseDocs {
  docs: DocSummaryView[];
  loading: boolean;
  error: boolean;
}

export function useDocs(): UseDocs {
  const [docs, setDocs] = useState<DocSummaryView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .listDocs()
      .then((d) => {
        if (alive) {
          setDocs(d);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useDocs: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, []);

  return { docs, loading, error };
}
