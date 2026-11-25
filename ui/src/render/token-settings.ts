import BigNumber from "bignumber.js";

import type { Token, TokenConfig, TokenEvent } from "@/api/types";

import type { EventLine } from "./tokens";

/** A ratio as a percentage of its own: `0.5` is `50%`, `0.010000000000000000` is `1%`. */
export function ratioPct(ratio: string): string {
  const v = new BigNumber(ratio);
  return v.isFinite() ? `${v.times(100).decimalPlaces(2).toFixed()}%` : ratio;
}

const positive = (ratio: string) => new BigNumber(ratio).gt(0);

/** Whether a factory token's minting was turned off for good while someone still manages it. */
export function isLocked(t: Pick<Token, "kind" | "admin" | "mintable">): boolean {
  return t.kind === "factory" && t.mintable === false && !!t.admin;
}

/** The fee sentences of the settings card, each with where the money goes. */
export interface SettingLine {
  kind: "gas" | "transfer" | "pool";
  /** Words before the destination. */
  text: string;
  /** What the destination is: a community pool address, or burned. */
  toPool: boolean;
}

/** "No special fees" is an empty list. */
export function settingLines(symbol: string, c: TokenConfig): SettingLine[] {
  const toPool = c.community_pool != null;
  const lines: SettingLine[] = [];
  if (positive(c.gas_surcharge)) {
    lines.push({ kind: "gas", text: `Paying fees in ${symbol} costs ${ratioPct(c.gas_surcharge)} more gas; the extra`, toPool });
  }
  if (positive(c.transfer_fee)) {
    lines.push({ kind: "transfer", text: `Fees for BZE features paid in ${symbol} carry a ${ratioPct(c.transfer_fee)} transfer fee, which`, toPool });
  }
  if (toPool) {
    lines.push({ kind: "pool", text: `The community share of pool trading fees paid in ${symbol}`, toPool });
  }
  return lines;
}

/** The sentence ending of a setting line: where the money goes. */
export function destinationText(line: SettingLine): string {
  switch (line.kind) {
    case "gas":
    case "transfer":
      return line.toPool ? "goes to" : "is burned";
    default:
      return "goes to";
  }
}

/** The new token event kinds of chain v8.3.0 in words; null for the older kinds. */
export function v830EventLine(e: TokenEvent): EventLine | null {
  switch (e.kind) {
    case "minting_disabled":
      return { text: "Minting turned off for good", party: null };
    case "admin_set_by_governance": {
      const to = typeof e.details?.to === "string" ? e.details.to : null;
      return to ? { text: "Governance gave control to", party: { address: to, label: null } } : { text: "Governance changed the admin", party: null };
    }
    case "config_changed": {
      const d = e.details ?? {};
      const part = (key: string, name: string) => (typeof d[key] === "string" && positive(d[key]) ? [`${name} ${ratioPct(d[key])}`] : []);
      const pool = typeof d.community_pool === "string" && d.community_pool ? ["community pool set"] : ["no community pool"];
      const parts = [...part("gas_surcharge", "extra gas"), ...part("transfer_fee", "transfer fee"), ...pool];
      return { text: `Settings changed: ${parts.join(", ")}`, party: null };
    }
    default:
      return null;
  }
}
