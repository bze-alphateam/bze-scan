/** One side of a pool: its reserve in base units and what the explorer knows to show it. */
export interface PoolAsset {
  denom: string;
  symbol: string | null;
  exponent: number | null;
  reserve: string;
}

/** A pool price: in chain units (quote base units per base base unit) and in display units. */
export interface PoolPriceValue {
  chain: string;
  display: string | null;
}

/**
 * The price state of the pool behind an LP token (`pool` of GET /api/v1/tokens/{denom}). `avg_price`
 * and both flags are null when the node has no average; the flags are also null for a pool without
 * the native denom. `fees_in_bze`: fees paid in the pool's token are charged in BZE.
 * `module_swaps_held`: the fee collector's swap and the burner's liquidity move wait.
 */
export interface PoolPrice {
  pool_id: string;
  reserves: { base: PoolAsset; quote: PoolAsset };
  price: PoolPriceValue | null;
  avg_price: PoolPriceValue | null;
  avg_last_height: number | null;
  /** |price − average| / average in percent, one decimal; null without a price or with an average of 0. */
  drift_pct: string | null;
  half_life_blocks: number | null;
  /** The limits in percent (10, 3 on mainnet). */
  fee_max_drift_pct: string | null;
  module_max_drift_pct: string | null;
  fees_in_bze: boolean | null;
  module_swaps_held: boolean | null;
}
