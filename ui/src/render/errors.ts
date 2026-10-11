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

/**
 * The plain-word reason of a failed transaction; `feeDenom` names the coin
 * when the log does not.
 */
export function failureReason(tx: FailedTx, symbolOf: SymbolOf = () => null, feeDenom: string | null = null): string {
  const known = tx.codespace === "sdk" ? SDK[tx.code] : undefined;
  if (known) return known(tx, symbolOf, feeDenom);
  if (tx.error_log) return tx.error_log;
  return `Failed with code ${tx.code}${tx.codespace ? ` (${tx.codespace})` : ""}`;
}
