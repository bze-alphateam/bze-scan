import { Flex, Grid, Text, type GridProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

// Custom breakpoints are not in Chakra's generated types, so responsive objects using them are hoisted.
const COLUMNS = { base: "minmax(0, 1fr)", c2: "180px minmax(0, 1fr)" };
const LABEL_PB = { base: "0", c2: "10px" };
const LABEL_LINE = { base: "0", c2: "1px" };

/** A panel's label/value list: a 180px label column, a line under every row but the last; one column on phones. */
export function DetailList(props: GridProps) {
  return <Grid as="dl" templateColumns={COLUMNS} m="0" {...props} />;
}

const line = { borderBottomWidth: "1px", borderColor: "border.panel" } as const;

/** One row of a `DetailList`: the muted label, then the value's parts in a wrapping row. */
export function DetailRow({ label, children, last }: { label: string; children: ReactNode; last?: boolean }) {
  return (
    <>
      <Text as="dt" px="4" pt="10px" pb={LABEL_PB} color="fg.muted" {...(last ? {} : { borderBottomWidth: LABEL_LINE, borderColor: "border.panel" })}>
        {label}
      </Text>
      <Flex as="dd" m="0" px="4" py="10px" minW="0" align="center" gap="2" wrap="wrap" overflowWrap="anywhere" {...(last ? {} : line)}>
        {children}
      </Flex>
    </>
  );
}
