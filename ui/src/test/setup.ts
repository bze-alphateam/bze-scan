// Registers the jest-dom matchers (toBeInTheDocument, …).
import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

// jsdom lacks the browser APIs Chakra's overlays and next-themes reach for.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}
if (typeof window.ResizeObserver === "undefined") {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}
if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = () => {};
}

// jsdom cannot parse Chakra's modern CSS (@layer, color-mix…) and prints a
// stack for every injected sheet; drop those lines, keep everything else.
const writeStderr = process.stderr.write.bind(process.stderr);
process.stderr.write = ((chunk: string | Uint8Array, ...rest: unknown[]) => {
  if (typeof chunk === "string" && chunk.includes("Could not parse CSS stylesheet")) {
    return true;
  }
  // @ts-expect-error forwarding the (encoding, callback) overloads verbatim
  return writeStderr(chunk, ...rest);
}) as typeof process.stderr.write;
