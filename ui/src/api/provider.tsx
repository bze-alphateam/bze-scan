"use client";

import { createContext, useContext, type ReactNode } from "react";

import type { ExplorerApi } from "./client";

const ApiContext = createContext<ExplorerApi | null>(null);

/** Puts the API every component reads in context. */
export function ApiProvider({ api, children }: { api: ExplorerApi; children: ReactNode }) {
  return <ApiContext.Provider value={api}>{children}</ApiContext.Provider>;
}

export function useApi(): ExplorerApi {
  const api = useContext(ApiContext);
  if (!api) {
    throw new Error("useApi must be used inside an ApiProvider");
  }
  return api;
}
