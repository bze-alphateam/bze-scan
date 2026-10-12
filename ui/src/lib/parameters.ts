import BigNumber from "bignumber.js";

import type { ChainParams, Stats } from "@/api/types";

import { formatAmount, formatCount, formatPct, shortAddress } from "./format";
import { externalLinks } from "./links";

// The chain parameters dictionary (canvas "Chain parameters — live values,
// plain words"): every value the parameters page shows, with its plain name,
// how its value reads and what it means. Only the words live here; every
// number comes from the live `/params` answer (or `/stats`), so the page can
// never disagree with the chain. Proposals reuse it for parameter changes.

export type ParamGroup =
  | "fees"
  | "staking"
  | "inflation"
  | "penalties"
  | "gov"
  | "dex"
  | "factory"
  | "rewards"
  | "burner"
  | "cointrunk";

/**
 * How a stored value reads: `duration` "1814400s" → "21 days", `ubze` a coin
 * (or base amount) → "100,000 BZE", `decimal_pct` "0.05" → "5%", `count` and
 * `gas` with separators, `address` a module account (or address) by name,
 * `list` "None right now" when empty, `raw` as stored.
 */
export type ParamFormat = "duration" | "ubze" | "decimal_pct" | "count" | "gas" | "address" | "list" | "raw";

/** What an explanation may read besides its own values. */
export interface ExplainContext {
  stats: Stats | null;
  params: ChainParams;
}

/** A row's values, one per path: as stored and as formatted. */
export interface ParamValues {
  raw: unknown[];
  text: string[];
}

export interface ParamDef {
  group: ParamGroup;
  /** The key of the `/params` answer. */
  module: string;
  /** Dotted paths inside the module's params; several for a combined row. */
  paths: string[];
  name: string;
  /** One format for every path, or one per path. */
  format: ParamFormat | ParamFormat[];
  /** The value cell from the values; the formatted values joined by " · " when absent. */
  show?: (v: ParamValues) => string;
  explain: (v: ParamValues, ctx: ExplainContext) => string;
  /** Low-level tuning, flagged as such. */
  advanced?: boolean;
  /** Read from `/stats` (as a decimal) instead of the module's params. */
  fromStats?: (stats: Stats) => string | null;
}

export interface GroupDef {
  group: ParamGroup;
  label: string;
  modules: string[];
  description: string;
  see?: { label: string; href: string };
}

export const PARAM_GROUPS: GroupDef[] = [
  {
    group: "fees",
    label: "Fees",
    modules: ["txfeecollector"],
    description: "What every transaction pays and what happens to fees paid in other tokens.",
  },
  { group: "staking", label: "Staking", modules: ["staking"], description: "How staking BZE with validators works." },
  {
    group: "inflation",
    label: "Rewards & inflation",
    modules: ["mint", "distribution"],
    description: "Where staking rewards come from and how much new BZE is created.",
  },
  {
    group: "penalties",
    label: "Validator penalties",
    modules: ["slashing"],
    description: "What happens to a validator, and to the people staking with it, when it misbehaves.",
  },
  {
    group: "gov",
    label: "Governance",
    modules: ["gov"],
    description: "How proposals are made and decided.",
    see: { label: "the proposals", href: "/proposals" },
  },
  { group: "dex", label: "DEX", modules: ["tradebin"], description: "Fees and limits of the built-in exchange at dex.getbze.com." },
  { group: "factory", label: "Factory", modules: ["tokenfactory"], description: "Creating new tokens on BZE through factory.getbze.com." },
  {
    group: "rewards",
    label: "Rewards programs",
    modules: ["rewards"],
    description: "Programs that projects open to reward people who stake or trade their token.",
  },
  {
    group: "burner",
    label: "Burner",
    modules: ["burner"],
    description: "The account that collects BZE to destroy.",
    see: { label: "burner.getbze.com", href: externalLinks.burner },
  },
  { group: "cointrunk", label: "CoinTrunk", modules: ["cointrunk"], description: "Rules of the CoinTrunk news app at app.cointrunk.io." },
];

/** The module accounts a parameter can name (tradebin's fee destinations). */
export const MODULE_ACCOUNTS: Record<string, { name: string; address: string }> = {
  burner: { name: "Burner module", address: "bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun" },
  community_pool: { name: "Community pool", address: "bze1jv65s3grqf6v6jl3dp4t6c9t9rk99cd86mghmg" },
};

const BECH32 = /^bze1[02-9ac-hj-np-z]{38,}$/;

/** A module account (or address) value as a chip's address and name; null for anything else. */
export function moduleAccount(value: unknown): { address: string; name: string | null } | null {
  if (typeof value !== "string") return null;
  if (MODULE_ACCOUNTS[value]) return MODULE_ACCOUNTS[value];
  return BECH32.test(value) ? { address: value, name: null } : null;
}

// Formatters.

const trimmed = (v: BigNumber, decimals: number) =>
  v.decimalPlaces(decimals, BigNumber.ROUND_HALF_UP).toFormat({ groupSeparator: ",", groupSize: 3, decimalSeparator: "." });

const plural = (n: string, unit: string) => `${n} ${unit}${n === "1" ? "" : "s"}`;

/** Seconds of a proto duration ("1814400s") or a number; null for anything else. */
export function durationSeconds(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value !== "string") return null;
  const m = /^(\d+(?:\.\d+)?)s$/.exec(value.trim());
  return m ? Number(m[1]) : null;
}

/** Days when at least one day, else hours, minutes or seconds: "21 days", "16.4 hours", "30 minutes". */
export function formatDuration(seconds: number): string {
  const s = new BigNumber(seconds);
  if (s.gte(86400)) return plural(trimmed(s.div(86400), 1), "day");
  if (s.gte(3600)) return plural(trimmed(s.div(3600), 1), "hour");
  if (s.gte(60)) return plural(trimmed(s.div(60), 0), "minute");
  return plural(trimmed(s, 0), "second");
}

/** A rough duration in whole units, for estimates: "about 16 hours". */
export function aboutDuration(seconds: number): string {
  const units: [number, string][] = [
    [86400, "day"],
    [3600, "hour"],
    [60, "minute"],
  ];
  const [size, unit] = units.find(([size]) => seconds >= size) ?? [1, "second"];
  return `about ${plural(String(Math.round(seconds / size)), unit)}`;
}

function coinOf(value: unknown): { denom: string; amount: string } | null {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const c = value as Record<string, unknown>;
    if (typeof c.denom === "string" && (typeof c.amount === "string" || typeof c.amount === "number")) {
      return { denom: c.denom, amount: String(c.amount) };
    }
  }
  return null;
}

/** A coin, a base amount of ubze, or a list of coins in display units: "100,000 BZE". */
export function formatUbze(value: unknown): string | null {
  if (Array.isArray(value)) {
    const parts = value.map(formatUbze);
    return parts.every((p) => p !== null) && parts.length > 0 ? parts.join(", ") : null;
  }
  const coin = coinOf(value) ?? ((typeof value === "string" || typeof value === "number") && new BigNumber(value).isFinite()
    ? { denom: "ubze", amount: String(value) }
    : null);
  if (!coin) return null;
  return coin.denom === "ubze" ? formatAmount(coin.amount, 6, "BZE") : `${formatCount(coin.amount)} ${coin.denom}`;
}

/** "0.334000000000000000" → "33.4%", "0.0001" → "0.01%". */
export function formatDecimalPct(value: unknown): string | null {
  if (typeof value !== "string" && typeof value !== "number") return null;
  const v = new BigNumber(value);
  return v.isFinite() ? `${trimmed(v.times(100), 2)}%` : null;
}

/** A stored value as text, for the grey line under the human value. */
export function rawText(value: unknown): string {
  if (value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  const coin = coinOf(value);
  if (coin) return `${coin.amount} ${coin.denom}`;
  if (Array.isArray(value) && value.length > 0 && value.every((c) => coinOf(c))) return value.map(rawText).join(", ");
  return JSON.stringify(value);
}

/** A value in a format; null when the value does not fit it. */
export function formatParam(format: ParamFormat, value: unknown): string | null {
  if (value === undefined || value === null) return null;
  switch (format) {
    case "duration": {
      const s = durationSeconds(value);
      return s === null ? null : formatDuration(s);
    }
    case "ubze":
      return formatUbze(value);
    case "decimal_pct":
      return formatDecimalPct(value);
    case "count":
    case "gas": {
      if (typeof value !== "string" && typeof value !== "number") return null;
      return new BigNumber(value).isFinite() ? formatCount(value) : null;
    }
    case "address": {
      const account = moduleAccount(value);
      if (account) return account.name ?? shortAddress(account.address);
      return typeof value === "string" ? value : null;
    }
    case "list":
      if (!Array.isArray(value)) return null;
      if (value.length === 0) return "None right now";
      return value
        .map((item) => (item && typeof item === "object" ? Object.values(item).map(String).join(" · ") : String(item)))
        .join(", ");
    case "raw":
      return rawText(value);
  }
}

/** The value at a dotted path of a module's params. */
export function paramAt(params: Record<string, unknown> | undefined, path: string): unknown {
  let at: unknown = params;
  for (const key of path.split(".")) {
    if (!at || typeof at !== "object" || Array.isArray(at)) return undefined;
    at = (at as Record<string, unknown>)[key];
  }
  return at;
}

// The dictionary, in the canvas's order.

const pct = (v: BigNumber.Value) => formatDecimalPct(String(v)) ?? "";
const first = (v: ParamValues) => v.text[0];

export const PARAMETERS: ParamDef[] = [
  // Fees
  {
    group: "fees",
    module: "txfeecollector",
    paths: ["ValidatorMinGasFee"],
    name: "Minimum gas price",
    format: "raw",
    show: (v) => {
      const coin = coinOf(v.raw[0]);
      return coin ? `${trimmed(new BigNumber(coin.amount), 18)} ${coin.denom} per gas` : v.text[0];
    },
    explain: () =>
      "Validators refuse any transaction that pays less than this per unit of work. It keeps spam out while a normal transaction costs less than a cent.",
  },
  {
    group: "fees",
    module: "txfeecollector",
    paths: ["MaxBalanceIterations"],
    name: "Fee tokens converted per pass",
    format: "count",
    show: (v) => `${first(v)} tokens`,
    explain: () =>
      "Fees can be paid in other tokens. When the chain turns them into BZE, it handles at most this many different tokens in one pass.",
  },
  {
    group: "fees",
    module: "txfeecollector",
    paths: ["BlockedIbcInbound"],
    name: "Cross-chain tokens refused",
    format: "list",
    explain: () =>
      "Tokens on this list are bounced back to the sender instead of being created on BZE. Used to retire a token after its issuer moved to another chain.",
  },
  // Staking
  {
    group: "staking",
    module: "staking",
    paths: ["unbonding_time"],
    name: "Unstaking period",
    format: "duration",
    explain: (v) =>
      `Unstaked BZE stays locked for ${first(v)} before it is back in your account. No rewards during that time, and it cannot be moved.`,
  },
  {
    group: "staking",
    module: "staking",
    paths: ["max_validators"],
    name: "Active validator seats",
    format: "count",
    explain: (v, { stats }) => {
      const taken = stats?.validators ? ` ${formatCount(stats.validators.active)} seats are taken right now.` : "";
      return `Only the ${first(v)} validators with the most stake produce blocks and earn rewards.${taken}`;
    },
  },
  {
    group: "staking",
    module: "staking",
    paths: ["max_entries"],
    name: "Unstakings in progress",
    format: "count",
    show: (v) => `${first(v)} per validator`,
    explain: () => "How many separate unstakings you can have running with the same validator at the same time.",
  },
  {
    group: "staking",
    module: "staking",
    paths: ["min_commission_rate"],
    name: "Lowest commission allowed",
    format: "decimal_pct",
    explain: () => "Validators may set any commission from this value up, and never above the limit they set for themselves.",
  },
  // Rewards & inflation
  {
    group: "inflation",
    module: "mint",
    paths: ["inflation"],
    name: "Inflation right now",
    format: "decimal_pct",
    fromStats: (s) => (s.inflation == null ? null : new BigNumber(s.inflation).div(100).toFixed()),
    show: (v) => `${first(v)} per year`,
    explain: () => "New BZE created each year and paid out to stakers. It moves slowly toward the target below.",
  },
  {
    group: "inflation",
    module: "mint",
    paths: ["inflation_min", "inflation_max"],
    name: "Inflation range",
    format: "decimal_pct",
    show: (v) => `${v.text[0]} to ${v.text[1]}`,
    explain: () => "Inflation never goes outside this range, whatever happens to the staked share.",
  },
  {
    group: "inflation",
    module: "mint",
    paths: ["goal_bonded"],
    name: "Target staked share",
    format: "decimal_pct",
    show: (v) => `${first(v)} of all BZE`,
    explain: (v, ctx) => `If less than ${first(v)} of BZE is staked, inflation rises to attract stakers. If more, it falls.${stakedToday(v, ctx)}`,
  },
  {
    group: "inflation",
    module: "mint",
    paths: ["inflation_rate_change"],
    name: "Inflation speed",
    format: "decimal_pct",
    show: (v) => `${first(v).replace(/%$/, "")} points per year`,
    explain: () => "The most inflation can move in a year while adjusting toward the target.",
  },
  {
    group: "inflation",
    module: "distribution",
    paths: ["community_tax"],
    name: "Community pool share",
    format: "decimal_pct",
    show: (v) => `${first(v)} of rewards`,
    explain: (v) => `${first(v)} of every reward goes to the community pool, which only proposals can spend.`,
  },
  {
    group: "inflation",
    module: "mint",
    paths: ["blocks_per_year"],
    name: "Blocks per year",
    format: "count",
    explain: (v, { stats }) => {
      const perYear = new BigNumber(String(v.raw[0]));
      const assumed = perYear.gt(0) ? `, about ${perYear.pow(-1).times(365.25 * 86400).toFixed(1)} seconds per block` : "";
      const now = stats?.avg_block_time_ms ? ` Blocks take ${(stats.avg_block_time_ms / 1000).toFixed(1)} seconds right now.` : "";
      return `The block count the chain assumes when spreading rewards over the year${assumed}.${now}`;
    },
  },
  // Validator penalties
  {
    group: "penalties",
    module: "slashing",
    paths: ["signed_blocks_window"],
    name: "Uptime window",
    format: "count",
    show: (v) => `${first(v)} blocks`,
    explain: (v, { stats }) => {
      const blocks = Number(v.raw[0]);
      const span = stats?.avg_block_time_ms && blocks > 0 ? ` (${aboutDuration((blocks * stats.avg_block_time_ms) / 1000)})` : "";
      return `The chain looks at the last ${first(v)} blocks${span} to judge whether a validator is online.`;
    },
  },
  {
    group: "penalties",
    module: "slashing",
    paths: ["min_signed_per_window"],
    name: "Must sign at least",
    format: "decimal_pct",
    show: (v) => `${first(v)} of them`,
    explain: (v) =>
      `A validator that misses more than ${pct(new BigNumber(1).minus(String(v.raw[0])))} of the blocks in the window is jailed and stops earning.`,
  },
  {
    group: "penalties",
    module: "slashing",
    paths: ["downtime_jail_duration"],
    name: "Jail time for downtime",
    format: "duration",
    explain: () => "The validator can ask to come back after this time. Stakers lose rewards in the meantime.",
  },
  {
    group: "penalties",
    module: "slashing",
    paths: ["slash_fraction_downtime"],
    name: "Penalty for downtime",
    format: "decimal_pct",
    show: (v) => `${first(v)} of the stake`,
    explain: () => "Taken from the validator and from everyone staking with it.",
  },
  {
    group: "penalties",
    module: "slashing",
    paths: ["slash_fraction_double_sign"],
    name: "Penalty for double-signing",
    format: "decimal_pct",
    show: (v) => `${first(v)} of the stake`,
    explain: (v) =>
      `Signing two different blocks at the same height is the worst offence: ${first(v)} is burned and the validator is removed for good.`,
  },
  // Governance
  {
    group: "gov",
    module: "gov",
    paths: ["min_deposit"],
    name: "Deposit to start voting",
    format: "ubze",
    explain: () =>
      "A proposal needs this much deposited before voting starts. The deposit goes back to the depositors unless the proposal is vetoed.",
  },
  {
    group: "gov",
    module: "gov",
    paths: ["max_deposit_period"],
    name: "Time to gather the deposit",
    format: "duration",
    explain: () => "If the deposit is not reached in time, the proposal is dropped.",
  },
  {
    group: "gov",
    module: "gov",
    paths: ["voting_period"],
    name: "Voting period",
    format: "duration",
    explain: () => "How long voting stays open once the deposit is reached.",
  },
  {
    group: "gov",
    module: "gov",
    paths: ["quorum"],
    name: "Quorum",
    format: "decimal_pct",
    show: (v) => `${first(v)} of staked BZE`,
    explain: (v) => `At least ${first(v)} of all staked BZE must vote, or the proposal fails no matter the result.`,
  },
  {
    group: "gov",
    module: "gov",
    paths: ["threshold"],
    name: "Pass threshold",
    format: "decimal_pct",
    show: (v) => `more than ${first(v)} Yes`,
    explain: () => "Counted over Yes, No and No with veto votes. Abstain counts for quorum but not here.",
  },
  {
    group: "gov",
    module: "gov",
    paths: ["veto_threshold"],
    name: "Veto threshold",
    format: "decimal_pct",
    show: (v) => `${first(v)} No with veto`,
    explain: (v) => `If more than ${first(v)} of the votes are No with veto, the proposal fails and the deposit is burned.`,
  },
  {
    group: "gov",
    module: "gov",
    paths: ["expedited_voting_period", "expedited_threshold"],
    name: "Expedited proposals",
    format: ["duration", "decimal_pct"],
    show: (v) => `${v.text[0]} · ${v.text[1]} Yes`,
    explain: (v) => `Urgent proposals can be voted in ${v.text[0]}, but need ${v.text[1]} of Yes votes.`,
  },
  // DEX
  {
    group: "dex",
    module: "tradebin",
    paths: ["createMarketFee"],
    name: "Fee to open a market",
    format: "ubze",
    explain: () => "Paid once by whoever lists a new trading pair.",
  },
  {
    group: "dex",
    module: "tradebin",
    paths: ["marketMakerFee"],
    name: "Maker fee",
    format: "ubze",
    show: (v) => `${first(v)} per order`,
    explain: () => "Paid when your order waits in the order book until someone takes it.",
  },
  {
    group: "dex",
    module: "tradebin",
    paths: ["marketTakerFee"],
    name: "Taker fee",
    format: "ubze",
    show: (v) => `${first(v)} per order`,
    explain: () => "Paid when your order fills right away against orders already in the book.",
  },
  {
    group: "dex",
    module: "tradebin",
    paths: ["makerFeeDestination", "takerFeeDestination"],
    name: "Where the fees go",
    format: "address",
    show: (v) => (v.text[0] === v.text[1] ? v.text[0] : `${v.text[0]} · ${v.text[1]}`),
    explain: (v) => {
      const [maker, taker] = v.raw;
      if (maker === "burner" && taker === "burner") {
        return "Both fees are sent to the Burner and destroyed on the next periodic burn. Trading on BZE reduces the BZE supply.";
      }
      if (maker === taker) return `Both fees are sent to the ${v.text[0]}.`;
      return `Maker fees are sent to the ${v.text[0]}, taker fees to the ${v.text[1]}.`;
    },
  },
  {
    group: "dex",
    module: "tradebin",
    paths: ["orderBookPerBlockMessages"],
    name: "Orders matched per block",
    format: "count",
    explain: (v) =>
      `Orders are matched by the chain itself at the end of each block, at most ${first(v)} per block. In busy moments orders queue for the next block.`,
  },
  {
    group: "dex",
    module: "tradebin",
    paths: ["minNativeLiquidityForModuleSwap"],
    name: "Liquidity needed for fee swaps",
    format: "ubze",
    explain: () => "A market needs this much BZE liquidity before the chain uses it to convert fees paid in that token.",
  },
  {
    group: "dex",
    module: "tradebin",
    paths: ["orderBookExtraGasWindow", "orderBookQueueExtraGas", "fillOrdersExtraGas", "orderBookQueueMessageScanExtraGas"],
    name: "Extra gas for order messages",
    format: "gas",
    advanced: true,
    explain: () => "Gas added to order transactions to pay for the matching work the chain does later, outside your transaction.",
  },
  // Factory
  {
    group: "factory",
    module: "tokenfactory",
    paths: ["create_denom_fee"],
    name: "Fee to create a token",
    format: "ubze",
    explain: () => "Paid once when a new token is created. Minting, burning and branding changes cost only normal gas.",
  },
  // Rewards programs
  {
    group: "rewards",
    module: "rewards",
    paths: ["createStakingRewardFee"],
    name: "Fee to open a staking reward",
    format: "ubze",
    explain: () => "Paid by a project that opens a program rewarding people who lock its token.",
  },
  {
    group: "rewards",
    module: "rewards",
    paths: ["createTradingRewardFee"],
    name: "Fee to open a trading reward",
    format: "ubze",
    explain: () => "Paid by a project that opens a program rewarding traders of one of its markets.",
  },
  {
    group: "rewards",
    module: "rewards",
    paths: ["extraGasForExitStake"],
    name: "Extra gas to leave a staking reward",
    format: "gas",
    explain: () => "Added to the gas of an exit, because the chain releases the locked tokens later, by itself.",
  },
  // Burner
  {
    group: "burner",
    module: "burner",
    paths: ["periodic_burning_weeks"],
    name: "Burn frequency",
    format: "count",
    show: (v) => (first(v) === "1" ? "every week" : `every ${first(v)} weeks`),
    explain: (v) =>
      `The Burner destroys everything it collected, ${first(v) === "1" ? "every week" : `every ${first(v)} weeks`}, by itself. The next burn is shown on the Burner app.`,
  },
  // CoinTrunk
  {
    group: "cointrunk",
    module: "cointrunk",
    paths: ["anon_article_limit"],
    name: "Paid articles per month",
    format: "count",
    explain: (v) =>
      `Accounts that are not approved publishers can post at most ${first(v)} paid articles a month, all of them together.`,
  },
  {
    group: "cointrunk",
    module: "cointrunk",
    paths: ["anon_article_cost"],
    name: "Cost of a paid article",
    format: "ubze",
    explain: () => "Paid by non-publishers for each article. It goes to the community pool.",
  },
  {
    group: "cointrunk",
    module: "cointrunk",
    paths: ["publisher_respect_params.tax", "publisher_respect_params.denom"],
    name: "Respect tax",
    format: ["decimal_pct", "raw"],
    show: (v) => `${v.text[0]}, in ${v.raw[1] === "ubze" ? "BZE" : v.text[1]}`,
    explain: (v) =>
      `When a reader pays respect to a publisher, ${v.text[0]} goes to the community pool and the rest to the publisher.`,
  },
];

/** "61.2% is staked today, so inflation is falling toward the minimum." */
function stakedToday(v: ParamValues, { stats, params }: ExplainContext): string {
  if (stats?.staked_share == null) return "";
  const staked = new BigNumber(stats.staked_share).div(100);
  const goal = new BigNumber(String(v.raw[0]));
  const inflation = stats.inflation == null ? null : new BigNumber(stats.inflation).div(100);
  const mint = params.mint?.params;
  const min = new BigNumber(String(mint?.inflation_min ?? NaN));
  const max = new BigNumber(String(mint?.inflation_max ?? NaN));
  let trend: string;
  if (staked.gt(goal)) {
    trend = inflation && min.isFinite() && inflation.lte(min) ? "sits at the minimum" : "is falling toward the minimum";
  } else if (staked.lt(goal)) {
    trend = inflation && max.isFinite() && inflation.gte(max) ? "sits at the maximum" : "is rising toward the maximum";
  } else {
    trend = "holds steady";
  }
  return ` ${formatPct(stats.staked_share)} is staked today, so inflation ${trend}.`;
}

// The page's rows.

export interface ParamRow {
  key: string;
  name: string;
  /** The chain's names, " · " between the paths of a combined row. */
  chainName: string;
  def: ParamDef | null;
  /** The stored values, one per path. */
  values: unknown[];
  /** The human value; null when the chain did not answer it ("not available"). */
  value: string | null;
  /** The stored values as text, for the grey line. */
  raw: string;
  explain: string;
  advanced: boolean;
}

export interface ParamGroupView extends GroupDef {
  rows: ParamRow[];
  /** The latest change of the group's modules a proposal explains. */
  changedBy: { proposalId: number; time: string } | null;
}

export const NOT_AVAILABLE = "not available";
const UNANSWERED = "The chain did not answer for this value right now.";
const ADVANCED = "An advanced setting, shown as the chain stores it.";

const formatsOf = (def: ParamDef) => def.paths.map((_, i) => (Array.isArray(def.format) ? def.format[i] : def.format));

/** One dictionary row against the live values. */
export function paramRow(def: ParamDef, ctx: ExplainContext): ParamRow {
  const params = ctx.params[def.module]?.params;
  const stat = def.fromStats ? (ctx.stats ? def.fromStats(ctx.stats) : null) : undefined;
  const raw = def.paths.map((p) => (def.fromStats ? (stat ?? undefined) : paramAt(params, p)));
  const formats = formatsOf(def);
  const text = raw.map((value, i) => formatParam(formats[i], value));
  const base = {
    key: `${def.module}:${def.paths.join("+")}`,
    name: def.name,
    chainName: def.paths.join(" · "),
    def,
    values: raw,
    raw: raw.map(rawText).join(" · "),
    advanced: !!def.advanced,
  };
  if (text.some((t) => t === null)) {
    return { ...base, value: null, raw: raw.every((r) => r === undefined) ? "" : base.raw, explain: UNANSWERED };
  }
  const values = { raw, text: text as string[] };
  return { ...base, value: def.show ? def.show(values) : values.text.join(" · "), explain: def.explain(values, ctx) };
}

/** Keys a module answers that no row covers: shown raw, marked advanced, so nothing the chain has is hidden. */
function advancedRows(module: string, params: Record<string, unknown> | undefined): ParamRow[] {
  if (!params) return [];
  const covered = new Set(
    PARAMETERS.filter((d) => d.module === module && !d.fromStats).flatMap((d) => d.paths.map((p) => p.split(".")[0])),
  );
  return Object.entries(params)
    .filter(([key]) => !covered.has(key))
    .map(([key, value]) => ({
      key: `${module}:${key}`,
      name: plainKey(key),
      chainName: key,
      def: null,
      values: [value],
      value: rawText(value) || "—",
      raw: "",
      explain: ADVANCED,
      advanced: true,
    }));
}

/** "historical_entries" → "Historical entries", "createDenomRewardFee" → "Create denom reward fee". */
export function plainKey(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/_/g, " ")
    .toLowerCase()
    .trim();
  return words ? words[0].toUpperCase() + words.slice(1) : key;
}

/** Every group with its rows: the dictionary's, then the advanced ones. */
export function parameterGroups(params: ChainParams, stats: Stats | null): ParamGroupView[] {
  const ctx = { params, stats };
  return PARAM_GROUPS.map((g) => {
    const rows = [
      ...PARAMETERS.filter((d) => d.group === g.group).map((d) => paramRow(d, ctx)),
      ...g.modules.flatMap((m) => advancedRows(m, params[m]?.params)),
    ];
    const changes = g.modules
      .map((m) => params[m]?.last_change)
      .filter((c) => c != null && c.proposal_id != null)
      .sort((a, b) => b!.time.localeCompare(a!.time));
    const changedBy = changes[0] ? { proposalId: changes[0].proposal_id!, time: changes[0].time } : null;
    return { ...g, rows, changedBy };
  });
}

/** The most recent recorded change across modules, with the groups it touched. */
export function latestChange(
  params: ChainParams,
): { proposalId: number | null; time: string; height: number; groups: string[] } | null {
  const changes = Object.entries(params)
    .filter(([, m]) => m?.last_change)
    .map(([module, m]) => ({ module, ...m!.last_change! }));
  if (changes.length === 0) return null;
  const last = changes.reduce((a, b) => (b.time > a.time ? b : a));
  const same = changes.filter((c) =>
    last.proposal_id != null ? c.proposal_id === last.proposal_id : c.height === last.height && c.proposal_id == null,
  );
  const modules = new Set(same.map((c) => c.module));
  const groups = PARAM_GROUPS.filter((g) => g.modules.some((m) => modules.has(m))).map((g) => g.label);
  return { proposalId: last.proposal_id, time: last.time, height: last.height, groups };
}

/** The anchor of a module's group on the parameters page (`/parameters#dex` for tradebin). */
export function groupOfModule(module: string): ParamGroup | null {
  return PARAM_GROUPS.find((g) => g.modules.includes(module))?.group ?? null;
}

/**
 * A changed key of a parameter-change proposal in plain words: the
 * dictionary's name and formatted value for a key a single-value row covers,
 * else null (the caller shows it raw).
 */
export function describeParam(module: string, key: string, value: unknown): { name: string; value: string } | null {
  const def = PARAMETERS.find((d) => d.module === module && !d.fromStats && d.paths.length === 1 && d.paths[0] === key);
  if (!def) return null;
  const text = formatParam(formatsOf(def)[0], value);
  if (text === null) return null;
  return { name: def.name, value: def.show ? def.show({ raw: [value], text: [text] }) : text };
}
