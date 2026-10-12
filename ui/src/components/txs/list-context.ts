"use client";

import { useMemo } from "react";

import { useTokenDirectory, useValidatorDirectory } from "@/api/hooks";
import { createRenderContext } from "@/render/messages/registry";
import type { RenderContext } from "@/render/messages/types";

const NO_TRANSFERS: never[] = [];

/** The renderers' context for list rows: symbols from the tokens list, monikers from the validators list. */
export function useListRenderContext(): RenderContext {
  const tokens = useTokenDirectory();
  const validators = useValidatorDirectory();
  return useMemo(
    () => createRenderContext({ transfers: NO_TRANSFERS, tokens: tokens.data, validators: validators.data }),
    [tokens.data, validators.data],
  );
}
