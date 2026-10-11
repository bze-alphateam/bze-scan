import { Circle, type SquareProps } from "@chakra-ui/react";

import { avatarHue, initials } from "@/lib/format";

interface AvatarProps extends Omit<SquareProps, "children"> {
  /** A moniker, or an address. */
  name: string;
}

/** Two-letter initials on a hue derived from the name. */
export function Avatar({ name, size = "9", ...props }: AvatarProps) {
  const hue = avatarHue(name);
  return (
    <Circle
      size={size}
      flexShrink={0}
      fontSize="xs"
      fontWeight="semibold"
      color="white"
      // A computed hue, so no palette token exists for it.
      style={{ background: `hsl(${hue} 55% 40%)` }}
      aria-hidden
      {...props}
    >
      {initials(name)}
    </Circle>
  );
}
