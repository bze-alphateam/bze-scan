import { Box, HStack } from "@chakra-ui/react";

interface FilterPillsProps<T> {
  /** What the group filters, for screen readers: "Filter by status". */
  label: string;
  options: { label: string; value: T }[];
  value: T;
  onChange: (value: T) => void;
  /** The space under the pills; 0 when they share a bar with a search box. */
  mb?: string;
}

/** The round filter buttons over a list: the chosen one in the accent tone. */
export function FilterPills<T>({ label, options, value, onChange, mb = "4" }: FilterPillsProps<T>) {
  return (
    <HStack gap="6px" wrap="wrap" mb={mb} role="group" aria-label={label}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <Box
            key={o.label}
            as="button"
            {...{ type: "button" }}
            h="30px"
            px="14px"
            rounded="15px"
            borderWidth="1px"
            borderColor={on ? "accent.fg" : "border.panel"}
            bg={on ? "accent.subtle" : "transparent"}
            color={on ? "accent.fg" : "fg.muted"}
            fontSize="13px"
            fontWeight="500"
            cursor="pointer"
            _hover={on ? undefined : { color: "fg" }}
            aria-pressed={on}
            onClick={() => onChange(o.value)}
          >
            {o.label}
          </Box>
        );
      })}
    </HStack>
  );
}
