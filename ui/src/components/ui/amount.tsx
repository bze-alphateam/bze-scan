import { Text, type TextProps } from "@chakra-ui/react";

import { formatAmount } from "@/lib/format";

interface AmountProps extends TextProps {
  /** Base units. */
  amount: string;
  denom: string;
  symbol?: string | null;
  exponent?: number | null;
  /** Tiles and list cells only: `14.3M BZE`. */
  compact?: boolean;
}

/**
 * An amount in display units with its symbol; a denom without metadata shows
 * the raw base amount and the denom.
 */
export function Amount({ amount, denom, symbol, exponent, compact, ...props }: AmountProps) {
  const known = symbol != null && exponent != null;
  const text = known ? formatAmount(amount, exponent, symbol, { compact }) : `${amount} ${denom}`;
  const full = known ? formatAmount(amount, exponent, symbol) : text;
  return (
    <Text as="span" fontVariantNumeric="tabular-nums" title={compact ? full : undefined} {...props}>
      {text}
    </Text>
  );
}
