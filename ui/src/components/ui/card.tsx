import { Box, Heading, Stack, Text, type BoxProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

interface CardProps extends Omit<BoxProps, "title"> {
  title?: ReactNode;
  /** The grey line under the title. */
  meta?: ReactNode;
  children?: ReactNode;
}

/**
 * A panel: a title row (a small upper-case label, a meta line, a bottom
 * border) over a padded body. `p` sets the body's padding (`p="0"` for a
 * table filling the panel).
 */
export function Card({ title, meta, children, p = { base: "4", md: "5" }, ...props }: CardProps) {
  const head = title || meta;
  return (
    <Box as="section" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" overflow="hidden" {...props}>
      {head && (
        <Stack gap="1" px="4" py="3" borderBottomWidth={children ? "1px" : "0"} borderColor="border.panel">
          {title && (
            <Heading as="h2" textStyle="label">
              {title}
            </Heading>
          )}
          {meta && (
            <Text fontSize="13px" color="fg.muted">
              {meta}
            </Text>
          )}
        </Stack>
      )}
      {children != null && children !== false && <Box p={p}>{children}</Box>}
    </Box>
  );
}
