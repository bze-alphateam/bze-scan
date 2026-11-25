"use client";

import BigNumber from "bignumber.js";

import { useParams } from "@/api/hooks";

/**
 * The most fills one order can take, from the tradebin parameter, as " (500)"
 * for a sentence; nothing while the parameters are unknown. A client module of
 * its own: the message renderers are also imported on the server, for page titles.
 */
export function FillCap() {
  const params = useParams();
  const raw = params.data?.tradebin?.params.maxFillsPerOrder;
  return typeof raw === "string" && /^\d+$/.test(raw) ? ` (${new BigNumber(raw).toFormat()})` : "";
}
