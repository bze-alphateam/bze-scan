// Why BZE refused an inbound cross-chain transfer, in plain words, from the
// `ack_error` the backend stores. Other values (the other chain's error, the
// blocked-inbound text) are not refusals of this kind and show as they are.

const REFUSALS: Record<string, string> = {
  "rate limit exceeded": "Refused by BZE: this route's daily limit was reached. The sending chain refunds it.",
  "blocked denom": "Refused by BZE: this token is blocked from crossing chains. The sending chain refunds it.",
  "channel closed": "Refused by BZE: transfers on this channel are stopped. The sending chain refunds it.",
};

/** The sentence of a refusal `ack_error`, null for any other value. */
export function refusalText(ackError: string | null | undefined): string | null {
  return ackError != null && Object.prototype.hasOwnProperty.call(REFUSALS, ackError) ? REFUSALS[ackError] : null;
}
