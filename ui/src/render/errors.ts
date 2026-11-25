// Why a transaction failed, in plain words, from its codespace and code. A
// code the map does not know shows the chain's log verbatim.

export interface FailedTx {
  codespace: string | null;
  code: number;
  error_log: string | null;
}

/** The symbol of a denom when known, for "Not enough BZE". */
export type SymbolOf = (denom: string) => string | null;

// `spendable balance 1ubze is smaller than 100ubze: insufficient funds`
const SHORT_DENOM = /is smaller than \d+([a-zA-Z][\w/.-]*)/;

function insufficientFunds(tx: FailedTx, symbolOf: SymbolOf, feeDenom: string | null): string {
  const denom = SHORT_DENOM.exec(tx.error_log ?? "")?.[1] ?? feeDenom;
  const name = denom ? (symbolOf(denom) ?? denom) : "funds";
  return `Not enough ${name} for the amount plus the fee`;
}

const SDK: Record<number, (tx: FailedTx, symbolOf: SymbolOf, feeDenom: string | null) => string> = {
  5: insufficientFunds,
  11: () => "Ran out of gas: the sender set too little gas for what the transaction did",
  13: () => "The fee was too low for the network",
  19: () => "Already sent: the same transaction was waiting to be included",
  32: () => "Sent out of order, try again",
};

// The IBC rate limits (codespace `ratelimit`) and the guardian's channel stop
// (codespace `txfeecollector`) refuse an outgoing transfer: the transaction fails.
const RATE_LIMIT: Record<number, string> = {
  4: "Over the daily limit for this route. Try a smaller amount or later; limits reset at 00:00 UTC.",
  7: "This token cannot cross chains right now (blocked).",
};
const TXFEECOLLECTOR: Record<number, string> = {
  1140: "Transfers on this channel are stopped.",
};

// Chain v8.3.0 modules: x/tokenfactory, x/tradebin and x/dao (codes from each module's `types/errors.go`).
const TOKENFACTORY: Record<number, string> = {
  1112: "Minting of this token is turned off for good.",
};
const TRADEBIN: Record<number, string> = {
  4018: "The pool's price moved too far from its recent average; try again in a few blocks.",
};
const DAO: Record<number, string> = {
  1129: "One of the DAO's actions failed unexpectedly.",
  1133: "This kind of action is not allowed in a DAO execution.",
  1134: "The DAO's actions are nested too deep.",
  1135: "This admin nomination is out of date: control of the nominating DAO changed since it was made.",
};

// x/circuit refuses a disabled message type in the ante handler with a plain error (no registered code).
const CIRCUIT_LOG = /tx type not allowed/;
const CIRCUIT_WORDS = "This action is switched off by an emergency measure.";

function moduleWords(tx: FailedTx): string | undefined {
  switch (tx.codespace) {
    case "ratelimit":
      return RATE_LIMIT[tx.code];
    case "txfeecollector":
      return TXFEECOLLECTOR[tx.code];
    case "tokenfactory":
      return TOKENFACTORY[tx.code];
    case "tradebin":
      return TRADEBIN[tx.code];
    case "dao":
      return DAO[tx.code];
    default:
      return undefined;
  }
}

/**
 * The plain-word reason of a failed transaction; `feeDenom` names the coin
 * when the log does not.
 */
export function failureReason(tx: FailedTx, symbolOf: SymbolOf = () => null, feeDenom: string | null = null): string {
  const known = tx.codespace === "sdk" ? SDK[tx.code] : undefined;
  if (known) return known(tx, symbolOf, feeDenom);
  const words = moduleWords(tx);
  if (words) return words;
  if (tx.error_log && CIRCUIT_LOG.test(tx.error_log)) return CIRCUIT_WORDS;
  if (tx.error_log) return tx.error_log;
  return `Failed with code ${tx.code}${tx.codespace ? ` (${tx.codespace})` : ""}`;
}
