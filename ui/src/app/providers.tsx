"use client";

import { QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { createHttpApi } from "@/api/client";
import { createQueryClient } from "@/api/policy";
import { ApiProvider } from "@/api/provider";
import { Provider } from "@/components/ui/provider";
import { env } from "@/lib/env";

// The composition root: the only place the HTTP API is built.
export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);
  const [api] = useState(() => createHttpApi({ baseUrl: env.apiUrl, fetch: globalThis.fetch.bind(globalThis) }));

  return (
    <Provider>
      <QueryClientProvider client={queryClient}>
        <ApiProvider api={api}>{children}</ApiProvider>
      </QueryClientProvider>
    </Provider>
  );
}
