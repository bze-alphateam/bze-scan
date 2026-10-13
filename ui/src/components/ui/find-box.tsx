import { HStack, Input, type StackProps } from "@chakra-ui/react";
import { LuSearch } from "react-icons/lu";

interface FindBoxProps extends Omit<StackProps, "onChange"> {
  /** The box's name and placeholder: "Find a token". */
  label: string;
  value: string;
  onChange: (value: string) => void;
}

/** The small search box over a list that filters it as you type. */
export function FindBox({ label, value, onChange, ...props }: FindBoxProps) {
  return (
    <HStack
      as="form"
      role="search"
      onSubmit={(e) => e.preventDefault()}
      gap="2"
      h="34px"
      w={{ base: "full", sm: "260px" }}
      px="3"
      bg="bg.panel"
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l2"
      color="fg.muted"
      _focusWithin={{ borderColor: "accent.fg" }}
      {...props}
    >
      <LuSearch size={14} aria-hidden />
      <Input
        aria-label={label}
        placeholder={label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        unstyled
        flex="1"
        minW="0"
        h="full"
        bg="transparent"
        color="fg"
        fontSize="13px"
        outline="none"
      />
    </HStack>
  );
}
