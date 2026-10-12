// Helpers for Markdown that anyone can write on chain.

/**
 * A URL a link may point to: http(s) and mailto only, else "" (the link is
 * dropped). Relative URLs are dropped too: they would resolve against the
 * explorer.
 */
export function safeUrl(url: string): string {
  const u = url.trim();
  return /^(https?:\/\/|mailto:)/i.test(u) && !/[\s<>"]/.test(u) ? u : "";
}

/** Markdown as one line of plain text, for an excerpt: the syntax dropped, the words kept. */
export function markdownToText(md: string): string {
  return md
    .replace(/<[^>]*>/g, " ") // raw HTML
    .replace(/```[\s\S]*?```/g, " ") // fenced code
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1") // images
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1") // links
    .replace(/^\s{0,3}#{1,6}\s+/gm, "") // headings
    .replace(/^\s{0,3}>\s?/gm, "") // quotes
    .replace(/^\s*([-*+]|\d+[.)])\s+/gm, "") // list markers
    .replace(/^\s*\|?(\s*:?-{3,}:?\s*\|)+\s*:?-*:?\s*$/gm, " ") // table rules
    .replace(/\|/g, " ")
    .replace(/(\*\*|__|\*|_|~~|`)/g, "") // emphasis and code
    .replace(/\s+/g, " ")
    .trim();
}
