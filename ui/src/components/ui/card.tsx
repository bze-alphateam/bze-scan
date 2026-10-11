import { Box, Heading, Stack, Text, type BoxProps } from "@chakra-ui/react";
import type { ReactNode } from "react";

interface CardProps extends Omit<BoxProps, "title"> {
  title?: ReactNode;
  /** The grey line under the title. */
  meta?: ReactNode;
  children?: ReactNode;
}

/** A panel with a title, a meta line and a body. */
export function Card({ title, meta, children, ...props }: CardProps) {
  return (
    <Box as="section" bg="bg.panel" borderWidth="1px" borderColor="border.panel" rounded="l3" p={{ base: "4", md: "5" }} {...props}>
      {(title || meta) && (
        <Stack gap="1" mb={children ? "4" : "0"}>
          {title && (
            <Heading as="h2" size="md">
              {title}
            </Heading>
          )}
          {meta && (
            <Text fontSize="sm" color="fg.muted">
              {meta}
            </Text>
          )}
        </Stack>
      )}
      {children}
    </Box>
  );
}
