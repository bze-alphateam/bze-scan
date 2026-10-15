"use client";

import { Text, type TextProps } from "@chakra-ui/react";
import { useCallback, useSyncExternalStore } from "react";

import { formatDateTime, relativeTime } from "@/lib/format";

/**
 * One clock for every TimeAgo on the page. A timer per row drifted: rows
 * mounted at different polls refreshed at different moments, so a newer row
 * could read older than the one below it. The shared clock ticks every second
 * while at least one TimeAgo is mounted and stops when the last one unmounts.
 */
const TICK_MS = 1000;

let now = Date.now();
let timer: ReturnType<typeof setInterval> | undefined;
const listeners = new Set<() => void>();

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (timer === undefined) {
    now = Date.now();
    timer = setInterval(() => {
      now = Date.now();
      listeners.forEach((l) => l());
    }, TICK_MS);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}

/** The shared now; read live while the clock is stopped (server render, first mount). */
function currentNow(): number {
  if (timer === undefined) {
    now = Date.now();
  }
  return now;
}

/**
 * The label for a past time. A time slightly ahead of the shared clock (a
 * block that landed between two ticks, or a client clock running late) reads
 * "just now", never "in 1 second".
 */
function pastLabel(time: string, at: number): string {
  return relativeTime(time, new Date(Math.max(at, new Date(time).getTime())));
}

/**
 * "12 seconds ago", with the absolute UTC time on hover. The row subscribes to
 * its label, not to the clock, so it re-renders only when the text changes:
 * every second under a minute old, once a minute after that, and so on.
 */
export function TimeAgo({ time, ...props }: { time: string } & TextProps) {
  const label = useCallback(() => pastLabel(time, currentNow()), [time]);
  const text = useSyncExternalStore(subscribe, label, label);
  return (
    <Text as="span" title={formatDateTime(time)} suppressHydrationWarning {...props}>
      <time dateTime={time}>{text}</time>
    </Text>
  );
}
