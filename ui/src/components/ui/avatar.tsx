"use client";

import { Box, Image, type BoxProps } from "@chakra-ui/react";
import { useState } from "react";

import { avatarIndex, initials } from "@/lib/format";

interface AvatarProps extends Omit<BoxProps, "children" | "size"> {
  /** A moniker, or an address. */
  name: string;
  /** A picture (a validator's Keybase one); the initials stand in without it or when it fails to load. */
  src?: string | null;
  /** A picture tried when `src` is missing or fails to load (the token placeholder), before the initials. */
  fallbackSrc?: string;
  /** A size token: "5" (20px, chips), "8" (32px, lists, the default) or larger on detail pages. */
  size?: string;
  /** "module": dashed and transparent (an account the chain owns); "token": round, 30px. */
  variant?: "default" | "module" | "token";
  /** "gold": the Hall of Fame ring. */
  ring?: "gold";
}

// A size to its corner radius and initials size.
function shape(size: string) {
  if (size === "5") return { rounded: "l1", fontSize: "9px" };
  if (size === "8" || size === "7" || size === "30px") return { rounded: "l2", fontSize: "11px" };
  return { rounded: "10px", fontSize: "16px" };
}

/**
 * A square avatar: a picture, else the fallback picture, else two initials
 * on a colour of the hash palette derived from the name.
 */
export function Avatar({ name, src, fallbackSrc, size, variant = "default", ring, ...props }: AvatarProps) {
  const [failed, setFailed] = useState<string[]>([]);
  const box = size ?? (variant === "token" ? "30px" : "8");
  const look = shape(box);
  const rounded = variant === "token" ? "full" : look.rounded;
  const isModule = variant === "module";

  const shown = [src, fallbackSrc].find((u): u is string => !!u && !failed.includes(u));
  const face = shown ? (
    <Image
      src={shown}
      alt=""
      boxSize={ring ? "full" : box}
      rounded={rounded}
      flexShrink={0}
      objectFit="cover"
      // Inside the gold ring a round logo would leave the ring's corners floating:
      // the other theme's page colour fills them. Elsewhere nothing shows behind it.
      bg={ring ? "bg.inverse" : "transparent"}
      onError={() => setFailed((f) => [...f, shown])}
      data-testid="avatar-image"
      {...(ring ? {} : props)}
    />
  ) : (
    <Box
      display="inline-grid"
      placeItems="center"
      boxSize={ring ? "full" : box}
      flexShrink={0}
      rounded={rounded}
      fontFamily="mono"
      fontSize={look.fontSize}
      fontWeight="600"
      lineHeight="1"
      color={isModule ? "fg.muted" : "white"}
      bg={isModule ? "transparent" : `avatar.${avatarIndex(name)}`}
      borderWidth={isModule ? "1px" : undefined}
      borderStyle={isModule ? "dashed" : undefined}
      borderColor={isModule ? "fg.muted" : undefined}
      data-avatar={variant}
      aria-hidden
      {...(ring ? {} : props)}
    >
      {initials(name)}
    </Box>
  );
  if (!ring) return face;
  // The gold ring: the gradient behind, the face inset by the ring's width.
  return (
    <Box boxSize={box} flexShrink={0} rounded={rounded} bgImage="{colors.gold.ring}" p="2px" data-ring={ring} {...props}>
      {face}
    </Box>
  );
}
