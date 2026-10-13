"use client"

import { ChakraProvider, createSystem, defaultConfig } from "@chakra-ui/react"
import {
  ColorModeProvider,
  type ColorModeProviderProps,
} from "./color-mode"

// The Terminal design (UI Restyle page, "Design system"): one value per
// theme, dark first. Components name these tokens, never a colour.
const T = {
  bg: { base: "#F5F7F9", _dark: "#090D11" },
  pan: { base: "#FFFFFF", _dark: "#0E141A" },
  pan2: { base: "#F0F3F6", _dark: "#131B23" },
  line: { base: "#DDE3E9", _dark: "#1D2731" },
  tx: { base: "#0E1620", _dark: "#DCE5EC" },
  mut: { base: "#5A6773", _dark: "#82919E" },
  acc: { base: "#0B74A8", _dark: "#22C4E2" },
  acc2: { base: "#1468A8", _dark: "#1694D2" },
  accbg: { base: "rgba(22,148,210,.09)", _dark: "rgba(34,196,226,.10)" },
  pos: { base: "#067647", _dark: "#3DDC97" },
  posbg: { base: "#E3F5EC", _dark: "rgba(61,220,151,.12)" },
  neg: { base: "#B42318", _dark: "#FF7A7A" },
  negbg: { base: "#FDECEA", _dark: "rgba(255,122,122,.12)" },
  warn: { base: "#9A5B00", _dark: "#F5B454" },
  warnbg: { base: "#FFF3DC", _dark: "rgba(245,180,84,.12)" },
} as const

const v = (value: { base: string; _dark: string } | string) => ({ value })

/** A Chakra palette's semantic tokens pointed at one Terminal tone, so `colorPalette="green"` badges follow it. */
function palette(fg: keyof typeof T, subtle: keyof typeof T, contrast: string) {
  return {
    contrast: v(contrast),
    fg: v(T[fg]),
    subtle: v(T[subtle]),
    muted: v(T[subtle]),
    emphasized: v(T[subtle]),
    solid: v(T[fg]),
    focusRing: v(T[fg]),
    border: v(T[fg]),
  }
}

// Text on a filled pos/neg/warn surface: the page background reads on both themes' tones.
const onTone = "{colors.bg.page}"

export const system = createSystem(defaultConfig, {
  globalCss: {
    body: {
      colorPalette: "blue",
      bg: "bg.page",
      color: "fg",
      fontSize: "14px",
      lineHeight: "1.45",
    },
    "code, pre, kbd, samp": { fontFeatureSettings: '"zero"' },
  },
  // Mono text (numbers, hashes, addresses) gets the slashed zero.
  utilities: {
    fontFamily: {
      values: "fonts",
      transform: (value: string, { raw }: { raw: string }) =>
        raw === "mono" ? { fontFamily: value, fontFeatureSettings: '"zero"' } : { fontFamily: value },
    },
  },
  theme: {
    breakpoints: {
      // Column priority in tables: `hideBelow="c3"` and `hideBelow="c2"`.
      c2: "820px",
      c3: "1180px",
    },
    tokens: {
      fonts: {
        body: { value: "var(--font-plex-sans), system-ui, sans-serif" },
        heading: { value: "var(--font-plex-sans), system-ui, sans-serif" },
        mono: { value: "var(--font-plex-mono), ui-monospace, monospace" },
      },
      colors: {
        // The avatar hash palette, the same on both themes.
        avatar: {
          0: { value: "#1468A8" },
          1: { value: "#9A3412" },
          2: { value: "#6D28D9" },
          3: { value: "#0F766E" },
          4: { value: "#A16207" },
          5: { value: "#475569" },
          6: { value: "#BE123C" },
          7: { value: "#15803D" },
        },
        gold: {
          ring: { value: "linear-gradient(135deg,#FBE29A 0%,#E9B64A 45%,#B9800F 100%)" },
        },
      },
    },
    semanticTokens: {
      colors: {
        bg: {
          DEFAULT: v(T.pan),
          page: v(T.bg),
          panel: v(T.pan),
          hover: v(T.pan2),
          subtle: v(T.pan2),
          muted: v(T.pan2),
          emphasized: v(T.line),
        },
        border: {
          DEFAULT: v(T.line),
          panel: v(T.line),
          subtle: v(T.line),
          muted: v(T.line),
          emphasized: v(T.mut),
        },
        fg: {
          DEFAULT: v(T.tx),
          muted: v(T.mut),
          subtle: v(T.mut),
        },
        accent: {
          fg: v(T.acc),
          solid: v(T.acc2),
          subtle: v(T.accbg),
        },
        pos: { fg: v(T.pos), subtle: v(T.posbg) },
        neg: { fg: v(T.neg), subtle: v(T.negbg) },
        warn: { fg: v(T.warn), subtle: v(T.warnbg) },
        blue: {
          contrast: v("white"),
          fg: v(T.acc),
          subtle: v(T.accbg),
          muted: v(T.accbg),
          emphasized: v(T.accbg),
          solid: v(T.acc2),
          focusRing: v(T.acc),
          border: v(T.acc),
        },
        green: palette("pos", "posbg", onTone),
        red: palette("neg", "negbg", onTone),
        orange: palette("warn", "warnbg", onTone),
        yellow: palette("warn", "warnbg", onTone),
        gray: {
          subtle: v(T.pan2),
          muted: v(T.pan2),
          fg: v(T.mut),
          border: v(T.line),
        },
      },
      radii: {
        // Tags, chips and small buttons · inputs and buttons · panels.
        l1: { value: "4px" },
        l2: { value: "6px" },
        l3: { value: "8px" },
      },
    },
    textStyles: {
      label: {
        value: { fontSize: "12px", fontWeight: "600", textTransform: "uppercase", letterSpacing: "0.08em", color: "fg.muted" },
      },
      tileLabel: {
        value: { fontSize: "11px", fontWeight: "500", textTransform: "uppercase", letterSpacing: "0.06em", color: "fg.muted" },
      },
      tileValue: {
        value: { fontFamily: "mono", fontSize: "22px", fontWeight: "500", letterSpacing: "-0.01em", lineHeight: "1.2" },
      },
      tableHead: {
        value: { fontSize: "11px", fontWeight: "500", textTransform: "uppercase", letterSpacing: "0.06em", color: "fg.muted" },
      },
    },
    recipes: {
      // Placeholders in the hover tone.
      skeleton: {
        variants: {
          variant: { pulse: { background: "bg.hover" } },
        },
      },
      button: {
        // Two heights only: 40px and 30px (`size="sm"`); outlined buttons turn accent on hover.
        variants: {
          size: {
            xs: { h: "30px", minW: "30px", fontSize: "13px", px: "2.5" },
            sm: { h: "30px", minW: "30px", fontSize: "13px", px: "3" },
            md: { h: "40px", minW: "40px", fontSize: "13px", px: "4" },
          },
          variant: {
            outline: {
              borderColor: "border.panel",
              color: "fg",
              _hover: { bg: "transparent", borderColor: "accent.fg", color: "accent.fg" },
            },
          },
        },
      },
    },
  },
})

export function Provider({ children, ...props }: ColorModeProviderProps & { children: React.ReactNode }) {
  return (
    <ChakraProvider value={system}>
      <ColorModeProvider {...props}>
        {children}
      </ColorModeProvider>
    </ChakraProvider>
  )
}
