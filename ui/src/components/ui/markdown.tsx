"use client";

import { Box, Code, Heading, List, Table, Text } from "@chakra-ui/react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { ExternalLink } from "@/components/ui/external-link";
import { safeUrl } from "@/lib/markdown";

// Text anyone can write on chain (a proposal's summary), shown as Markdown.
// Raw HTML is dropped, never parsed into elements; links keep only http(s)
// and mailto URLs; images become links, so a summary loads nothing by itself.

const components: Components = {
  h1: ({ children }) => <Heading as="h4" size="md" mt="2">{children}</Heading>,
  h2: ({ children }) => <Heading as="h4" size="sm" mt="2">{children}</Heading>,
  h3: ({ children }) => <Heading as="h5" size="sm" mt="1">{children}</Heading>,
  h4: ({ children }) => <Heading as="h6" size="xs">{children}</Heading>,
  h5: ({ children }) => <Heading as="h6" size="xs">{children}</Heading>,
  h6: ({ children }) => <Heading as="h6" size="xs">{children}</Heading>,
  p: ({ children }) => <Text>{children}</Text>,
  a: ({ href, children }) =>
    href ? (
      <ExternalLink href={href} userContent wordBreak="break-word">
        {children}
      </ExternalLink>
    ) : (
      <>{children}</>
    ),
  img: ({ src, alt }) => {
    const href = typeof src === "string" ? safeUrl(src) : "";
    const text = alt || "image";
    return href ? (
      <ExternalLink href={href} userContent wordBreak="break-word">
        {text}
      </ExternalLink>
    ) : (
      <>{text}</>
    );
  },
  ul: ({ children }) => <List.Root ps="5">{children}</List.Root>,
  ol: ({ children }) => (
    <List.Root as="ol" ps="5" listStyleType="decimal">
      {children}
    </List.Root>
  ),
  li: ({ children }) => <List.Item>{children}</List.Item>,
  blockquote: ({ children }) => (
    <Box borderStartWidth="3px" borderColor="border.emphasized" ps="3" color="fg.muted">
      {children}
    </Box>
  ),
  code: ({ children }) => <Code fontSize="xs" whiteSpace="pre-wrap" wordBreak="break-word">{children}</Code>,
  pre: ({ children }) => (
    <Box as="pre" bg="bg.muted" p="3" rounded="l2" overflowX="auto" fontSize="xs">
      {children}
    </Box>
  ),
  hr: () => <Box as="hr" borderColor="border.panel" />,
  table: ({ children }) => (
    <Box overflowX="auto" maxW="full">
      <Table.Root size="sm" variant="outline">{children}</Table.Root>
    </Box>
  ),
  thead: ({ children }) => <Table.Header>{children}</Table.Header>,
  tbody: ({ children }) => <Table.Body>{children}</Table.Body>,
  tr: ({ children }) => <Table.Row>{children}</Table.Row>,
  th: ({ children }) => <Table.ColumnHeader>{children}</Table.ColumnHeader>,
  td: ({ children }) => <Table.Cell>{children}</Table.Cell>,
};

/** Untrusted Markdown, rendered safely (see above). */
export function Markdown({ text }: { text: string }) {
  return (
    <Box fontSize="sm" wordBreak="break-word" display="flex" flexDirection="column" gap="2" data-testid="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={safeUrl} components={components}>
        {text}
      </ReactMarkdown>
    </Box>
  );
}
