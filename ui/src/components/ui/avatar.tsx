"use client";

import { Circle, Image, type SquareProps } from "@chakra-ui/react";
import { useState } from "react";

import { avatarHue, initials } from "@/lib/format";

interface AvatarProps extends Omit<SquareProps, "children"> {
  /** A moniker, or an address. */
  name: string;
  /** A picture (a validator's Keybase one); the initials stand in without it or when it fails to load. */
  src?: string | null;
  /** A picture tried when `src` is missing or fails to load (the token placeholder), before the initials. */
  fallbackSrc?: string;
}

/** A picture, else the fallback picture, else two-letter initials on a hue derived from the name. */
export function Avatar({ name, src, fallbackSrc, size = "9", ...props }: AvatarProps) {
  const [failed, setFailed] = useState<string[]>([]);
  const shown = [src, fallbackSrc].find((u): u is string => !!u && !failed.includes(u));
  if (shown) {
    return (
      <Image
        src={shown}
        alt=""
        boxSize={size}
        rounded="full"
        flexShrink={0}
        objectFit="cover"
        onError={() => setFailed((f) => [...f, shown])}
        data-testid="avatar-image"
      />
    );
  }
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
