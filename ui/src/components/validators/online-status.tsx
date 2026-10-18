"use client";

import { Box, Text } from "@chakra-ui/react";
import { useId } from "react";

import type { ChainParams, Validator } from "@/api/types";
import { Callout } from "@/components/ui/callout";
import { useHoverCard } from "@/components/ui/hover-card";
import { Tag } from "@/components/ui/status-pill";
import { downtimeSlashText, jailedInText, onlineLines, onlineState, type OnlineState } from "@/render/validators";

type Signing = Pick<Validator, "offline" | "missed_in_a_row" | "last_signed_height">;

const TONE: Record<OnlineState, { fg: string; label: string }> = {
  online: { fg: "pos.fg", label: "Online" },
  offline: { fg: "neg.fg", label: "Offline" },
};

function Dot({ state, size = "9px" }: { state: OnlineState; size?: string }) {
  return <Box as="span" display="inline-block" flex="none" boxSize={size} rounded="full" bg={TONE[state].fg} aria-hidden />;
}

/**
 * The online / offline dot of an active validator, with its card on hover,
 * keyboard focus and tap. Nothing for a validator outside the active set.
 * `align` is the edge the card hangs from: `end` in the uptime column, `start`
 * after the moniker.
 */
export function OnlineDot({ v, avgBlockMs, align = "end" }: { v: Signing; avgBlockMs?: number | null; align?: "start" | "end" }) {
  const id = useId();
  const { open, toggle, rootProps } = useHoverCard();
  const state = onlineState(v);
  if (!state) return null;
  const tone = TONE[state];
  return (
    <Box as="span" {...rootProps} position="relative" display="inline-flex" verticalAlign="middle" data-testid="online-dot" data-state={state}>
      <Box
        as="button"
        {...{ type: "button" }}
        display="inline-flex"
        alignItems="center"
        justifyContent="center"
        boxSize="18px"
        p="0"
        bg="transparent"
        border="0"
        cursor="help"
        rounded="full"
        _focusVisible={{ outline: "none", boxShadow: "0 0 0 2px {colors.accent.fg}" }}
        aria-label={tone.label}
        aria-describedby={id}
        aria-expanded={open}
        onClick={toggle}
      >
        <Dot state={state} />
      </Box>
      <Box
        as="span"
        id={id}
        role="tooltip"
        hidden={!open}
        position="absolute"
        top="calc(100% + 6px)"
        {...(align === "end" ? { right: "-6px" } : { left: "-6px" })}
        zIndex="popover"
        w="280px"
        maxW="calc(100vw - 32px)"
        px="14px"
        py="3"
        bg="bg.panel"
        borderWidth="1px"
        borderColor="border.panel"
        rounded="l3"
        boxShadow="0 8px 24px rgba(0,0,0,.35)"
        fontFamily="body"
        fontSize="12px"
        lineHeight="1.5"
        fontWeight="400"
        color="fg"
        textAlign="start"
        whiteSpace="normal"
        cursor="default"
        // The gap above the card keeps the hover while the pointer crosses it.
        _before={{ content: '""', position: "absolute", insetX: "0", top: "-8px", h: "8px" }}
        css={{ "&:not([hidden])": { display: "flex", flexDirection: "column", gap: "4px" } }}
        data-testid="online-card"
      >
        <Box as="span" display="flex" alignItems="center" gap="6px" fontWeight="600" fontSize="13px" color={tone.fg}>
          <Dot state={state} size="8px" />
          {tone.label}
        </Box>
        {onlineLines(v, avgBlockMs).map((line, i) => (
          <Text as="span" key={line} color={i === 0 ? "fg" : "fg.muted"}>
            {line}
          </Text>
        ))}
      </Box>
    </Box>
  );
}

/** The header's ONLINE / OFFLINE tag of an active validator; nothing outside the active set. */
export function OnlineTag({ v }: { v: Signing }) {
  const state = onlineState(v);
  if (!state) return null;
  return (
    <Tag tone={state === "online" ? "ok" : "bad"} gap="6px" data-testid="online-tag">
      <Dot state={state} size="7px" />
      {TONE[state].label}
    </Tag>
  );
}

/** The validator page's red note while an active validator is offline: how long, and what jailing would cost. */
export function OfflineCallout({
  v,
  params,
  avgBlockMs,
}: {
  v: Signing & Pick<Validator, "moniker" | "missed_blocks" | "signed_blocks_window">;
  params: ChainParams | undefined;
  avgBlockMs: number | null | undefined;
}) {
  if (!v.offline) return null;
  const [missed, last] = onlineLines(v, avgBlockMs);
  const jailIn = jailedInText(v, params, avgBlockMs);
  const slash = downtimeSlashText(params);
  return (
    <Callout tone="bad" title="Offline: not signing blocks" mt="5" data-testid="offline-callout">
      {v.moniker} {missed.charAt(0).toLowerCase() + missed.slice(1)} {last}
      {jailIn && (
        <Text as="span" display="block" color="fg.muted" mt="1">
          {jailIn === "any moment now"
            ? "It gets jailed any moment now if it stays offline."
            : `If it stays offline it gets jailed in ${jailIn}.`}{" "}
          Stakers then earn nothing from it{slash ? ` and lose ${slash} of their stake` : ""}.
        </Text>
      )}
    </Callout>
  );
}
