"use client"

import { ChakraProvider, createSystem, defaultConfig } from "@chakra-ui/react"
import {
  ColorModeProvider,
  type ColorModeProviderProps,
} from "./color-mode"

// The DEX app's system (Inter, the blue palette, its radii) plus the
// explorer's surface tokens, all from Chakra's default palette.
export const system = createSystem(defaultConfig, {
  globalCss: {
    body: {
      colorPalette: 'blue',
      bg: 'bg.page',
    },
  },
  theme: {
    tokens: {
      fonts: {
        body: { value: 'var(--font-inter)' },
        heading: { value: 'var(--font-inter)' },
      },
    },
    semanticTokens: {
      colors: {
        bg: {
          page: { value: { base: '{colors.gray.50}', _dark: '{colors.gray.950}' } },
          panel: { value: { base: '{colors.white}', _dark: '{colors.gray.900}' } },
        },
        border: {
          panel: { value: { base: '{colors.gray.200}', _dark: '{colors.gray.800}' } },
        },
      },
      radii: {
        l1: { value: '0.375rem' },
        l2: { value: '0.5rem' },
        l3: { value: '0.75rem' },
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
