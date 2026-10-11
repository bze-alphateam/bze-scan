import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderOptions } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";

import type { ExplorerApi } from "@/api/client";
import { ApiProvider } from "@/api/provider";
import { Provider } from "@/components/ui/provider";

/**
 * A fake API: the given methods, every other one rejecting so a test notices
 * a call it did not expect.
 */
export function fakeApi(methods: Partial<ExplorerApi> = {}): ExplorerApi {
  return new Proxy(methods as ExplorerApi, {
    get(target, prop: string) {
      if (prop in target) return target[prop as keyof ExplorerApi];
      return () => Promise.reject(new Error(`fakeApi: unexpected call to ${prop}`));
    },
  });
}

/** An API whose every method never resolves: for asserting skeletons. */
export function pendingApi(): ExplorerApi {
  return new Proxy({} as ExplorerApi, {
    get: () => () => new Promise(() => {}),
  });
}

export function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
}

interface Options extends Omit<RenderOptions, "wrapper"> {
  api?: ExplorerApi;
  queryClient?: QueryClient;
}

/** Renders inside the app's providers with a fake API (by default one that rejects every call). */
export function renderWithProviders(ui: ReactElement, { api = fakeApi(), queryClient = testQueryClient(), ...options }: Options = {}) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <Provider>
        <QueryClientProvider client={queryClient}>
          <ApiProvider api={api}>{children}</ApiProvider>
        </QueryClientProvider>
      </Provider>
    );
  }
  return { queryClient, ...render(ui, { wrapper: Wrapper, ...options }) };
}
