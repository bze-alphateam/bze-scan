import { Badge, type BadgeProps } from "@chakra-ui/react";
import { LuCircleCheck, LuCircleX } from "react-icons/lu";

interface StatusPillProps extends Omit<BadgeProps, "children"> {
  success: boolean;
}

/** "Success" in green or "Failed" in red. */
export function StatusPill({ success, ...props }: StatusPillProps) {
  return (
    <Badge colorPalette={success ? "green" : "red"} variant="subtle" size="lg" rounded="full" gap="1" {...props}>
      {success ? <LuCircleCheck aria-hidden /> : <LuCircleX aria-hidden />}
      {success ? "Success" : "Failed"}
    </Badge>
  );
}
