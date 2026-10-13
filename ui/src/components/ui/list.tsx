import { Box, Grid, type BoxProps, type GridProps } from "@chakra-ui/react";

/** The panel around a list: the Terminal table look for Grid-based lists. */
export function ListPanel(props: BoxProps) {
  return <Box bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden" {...props} />;
}

/** The head row: small upper-case muted labels over a bottom border. Pass the rows' `templateColumns`. */
export function ListHead(props: GridProps) {
  return (
    <Grid
      alignItems="center"
      gap="4"
      px="4"
      py="10px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      textStyle="tableHead"
      whiteSpace="nowrap"
      {...props}
    />
  );
}

/** One row: 11px/16px padding, a separator under every row but the last, the hover tone. */
export function ListRow(props: GridProps) {
  return (
    <Grid
      alignItems="center"
      gap="4"
      px="4"
      py="11px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _last={{ borderBottomWidth: "0" }}
      _hover={{ bg: "bg.hover" }}
      {...props}
    />
  );
}
