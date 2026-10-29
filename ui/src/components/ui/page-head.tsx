import { Box, Flex, Heading, HStack, Link, Text, type FlexProps } from "@chakra-ui/react";
import NextLink from "next/link";
import { Fragment, type ReactNode } from "react";

/** A list page's head: the 24px title and one muted line under it; `children` sit on the right. */
export function PageTitle({ title, intro, children }: { title: ReactNode; intro?: ReactNode; children?: ReactNode }) {
  return (
    <Flex justify="space-between" align="flex-end" gap="4" wrap="wrap" pt="8" pb="5">
      <Box minW="0">
        <Heading as="h1" fontSize="24px" fontWeight="600" letterSpacing="-0.01em" lineHeight="1.25">
          {title}
        </Heading>
        {intro && (
          <Text mt="6px" maxW="640px" color="fg.muted" lineHeight="1.55">
            {intro}
          </Text>
        )}
      </Box>
      {children}
    </Flex>
  );
}

/** "Blocks / 25,120,440": links up the path, the page itself last (mono when `mono`). */
export function Breadcrumb({ path, current, mono }: { path: { label: string; href: string }[]; current: ReactNode; mono?: boolean }) {
  return (
    <HStack as="nav" aria-label="Breadcrumb" gap="1.5" pt="6" fontSize="12px" color="fg.muted" wrap="wrap">
      {path.map((p) => (
        <Fragment key={p.href}>
          <Link asChild color="fg.muted" _hover={{ color: "fg" }}>
            <NextLink href={p.href}>{p.label}</NextLink>
          </Link>
          <Text as="span" aria-hidden>
            /
          </Text>
        </Fragment>
      ))}
      <Text as="span" aria-current="page" fontFamily={mono ? "mono" : undefined}>
        {current}
      </Text>
    </HStack>
  );
}

/** A detail page's title row: the 24px h1, then `children` (arrows, chips). */
export function DetailTitle({ children, title }: { title: ReactNode; children?: ReactNode }) {
  return (
    <HStack gap="3" wrap="wrap" mt="2">
      <Heading as="h1" fontSize="24px" fontWeight="600" letterSpacing="-0.01em" lineHeight="1.25">
        {title}
      </Heading>
      {children}
    </HStack>
  );
}

/** The muted line under a detail title: age, counts, tags. */
export function DetailMeta(props: FlexProps) {
  return <Flex align="center" gap="10px" wrap="wrap" mt="2" fontSize="13px" color="fg.muted" {...props} />;
}

/** A section's head: the small upper-case label (an h2) and an optional link or note on the right. */
export function SectionHead({ id, title, children }: { id?: string; title: ReactNode; children?: ReactNode }) {
  return (
    <HStack justify="space-between" gap="3" mb="10px">
      <Heading as="h2" textStyle="label" id={id}>
        {title}
      </Heading>
      {children}
    </HStack>
  );
}

/** A panel's head row: the small upper-case title, a muted note and an action (an export button) on the right. */
export function PanelHead({ id, title, note, action }: { id: string; title: string; note?: ReactNode; action?: ReactNode }) {
  return (
    <Flex justify="space-between" align="center" gap="3" px="4" py="3" borderBottomWidth="1px" borderColor="border.panel">
      <Heading as="h2" textStyle="label" id={id}>
        {title}
      </Heading>
      {(note || action) && (
        <Flex align="center" gap="3" minW="0">
          {note && (
            <Text fontSize="12px" color="fg.muted" textAlign="right">
              {note}
            </Text>
          )}
          {action}
        </Flex>
      )}
    </Flex>
  );
}
