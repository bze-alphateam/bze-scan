"use client";

import { useEffect, useRef, useState, type FocusEvent, type KeyboardEvent } from "react";

/**
 * The open state of a small card hung under a trigger: open on hover, on
 * keyboard focus and on tap (phones have no hover), until the pointer leaves,
 * focus moves out, a tap lands elsewhere or Escape. Spread `rootProps` on the
 * element holding both the trigger and the card; the trigger's click calls
 * `toggle`.
 */
export function useHoverCard() {
  const root = useRef<HTMLSpanElement>(null);
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  const [tapped, setTapped] = useState(false);

  // A tap opens the card until the next tap elsewhere.
  useEffect(() => {
    if (!tapped) return;
    const away = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setTapped(false);
    };
    document.addEventListener("pointerdown", away);
    return () => document.removeEventListener("pointerdown", away);
  }, [tapped]);

  const close = () => {
    setHover(false);
    setFocus(false);
    setTapped(false);
  };

  return {
    open: hover || focus || tapped,
    close,
    toggle: () => setTapped((t) => !t),
    rootProps: {
      ref: root,
      onMouseEnter: () => setHover(true),
      onMouseLeave: () => setHover(false),
      onFocus: () => setFocus(true),
      onBlur: (e: FocusEvent) => {
        if (!root.current?.contains(e.relatedTarget as Node | null)) {
          setFocus(false);
          setTapped(false);
        }
      },
      onKeyDown: (e: KeyboardEvent) => {
        if (e.key === "Escape") close();
      },
    },
  };
}
