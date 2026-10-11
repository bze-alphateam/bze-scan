"use client";

import { Text, type TextProps } from "@chakra-ui/react";
import { useEffect, useState } from "react";

import { formatDateTime, relativeTime } from "@/lib/format";

/** Re-renders every `ms` so relative times stay true. */
function useNow(ms: number): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), ms);
    return () => clearInterval(id);
  }, [ms]);
  return now;
}

/** "12 seconds ago", with the absolute UTC time on hover. */
export function TimeAgo({ time, ...props }: { time: string } & TextProps) {
  const now = useNow(10_000);
  const absolute = formatDateTime(time);
  return (
    <Text as="span" title={absolute} suppressHydrationWarning {...props}>
      <time dateTime={time}>{relativeTime(time, now)}</time>
    </Text>
  );
}
