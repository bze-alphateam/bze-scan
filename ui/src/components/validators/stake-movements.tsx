"use client";

import { Box, Button, Grid, Link, Skeleton, Text } from "@chakra-ui/react";
import NextLink from "next/link";
import { useState } from "react";

import { MOVEMENTS_PER_PAGE, useValidatorMovements } from "@/api/hooks";
import type { StakeMovement } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { ListHead, ListPanel } from "@/components/ui/list";
import { PanelHead } from "@/components/ui/page-head";
import { Pager } from "@/components/ui/pager";
import { TimeAgo } from "@/components/ui/time-ago";
import { lastPage } from "@/render/account";
import { movementAdds, movementAmount, movementLabel, movementsRange } from "@/render/validators";

// When · Account · Movement · Amount from c2 up; on phones two lines: the
// movement and the amount, then the account and the time.
// (Custom breakpoints are not in Chakra's generated types, so these objects are hoisted.)
const COLUMNS = { base: "minmax(0, 1fr) auto", c2: "110px 170px minmax(0, 1fr) auto" };
const AREAS = { base: `"what amount" "who when"`, c2: `"when who what amount"` };
const WHEN_ALIGN = { base: "right", c2: "left" } as const;

const validatorHref = (operator: string) => `/validators/${encodeURIComponent(operator)}`;

/** The movements page in the URL (`?movements=3`); 1 when absent or not a page. */
function readPlace(): number {
  if (typeof window === "undefined") return 1;
  const n = Number.parseInt(new URLSearchParams(window.location.search).get("movements") ?? "", 10);
  return Number.isSafeInteger(n) && n > 0 ? n : 1;
}

/** Keeps the movements page in the URL without a navigation; page 1 leaves it out. */
function writePlace(page: number) {
  const url = new URL(window.location.href);
  if (page > 1) url.searchParams.set("movements", String(page));
  else url.searchParams.delete("movements");
  window.history.replaceState(window.history.state, "", url);
}

function MovementRow({ m }: { m: StakeMovement }) {
  const label = movementLabel(m);
  return (
    <Grid
      role="listitem"
      templateColumns={COLUMNS}
      templateAreas={AREAS}
      columnGap="4"
      rowGap="1"
      alignItems="center"
      px="4"
      py="11px"
      borderBottomWidth="1px"
      borderColor="border.panel"
      _hover={{ bg: "bg.hover" }}
      data-testid="stake-movement"
      data-kind={m.kind}
    >
      <Text gridArea="when" fontSize="13px" textAlign={WHEN_ALIGN} color="fg.muted">
        <Link asChild color="inherit" _hover={{ color: "accent.fg" }}>
          <NextLink href={`/txs/${m.tx_hash.toUpperCase()}`} aria-label="See transaction" data-testid="stake-movement-tx">
            <TimeAgo time={m.time} />
          </NextLink>
        </Link>
      </Text>
      <Box gridArea="who" minW="0" fontSize="13px">
        <AccountChip address={m.delegator} />
      </Box>
      <Text gridArea="what" minW="0" wordBreak="break-word" data-testid="stake-movement-what">
        {label.text}
        {label.counterparty && (
          <>
            {" "}
            <Link asChild color="accent.fg">
              <NextLink href={validatorHref(label.counterparty.operator)}>{label.counterparty.name}</NextLink>
            </Link>
          </>
        )}
      </Text>
      <Text
        gridArea="amount"
        textAlign="right"
        fontFamily="mono"
        fontSize="13px"
        whiteSpace="nowrap"
        color={movementAdds(m.kind) ? "pos.fg" : "warn.fg"}
        data-testid="stake-movement-amount"
      >
        {movementAmount(m)}
      </Text>
    </Grid>
  );
}

function RowsSkeleton() {
  return (
    <Box aria-busy data-testid="stake-movements-skeleton">
      {Array.from({ length: 4 }).map((_, i) => (
        <Box key={i} px="4" py="14px" borderBottomWidth="1px" borderColor="border.panel">
          <Skeleton h="4" w="70%" />
        </Box>
      ))}
    </Box>
  );
}

const Note = ({ children, testId }: { children: string; testId: string }) => (
  <Text px="4" py="3" fontSize="13px" color="fg.muted" data-testid={testId}>
    {children}
  </Text>
);

/**
 * "Recent stake movements": the validator's last 7 days of delegations,
 * undelegations and redelegations, 10 a page, newest first. Its own query
 * and loading state: the rest of the page never waits for it.
 */
export function StakeMovements({ operator }: { operator: string }) {
  const [page, setPage] = useState(readPlace);
  const { data, error, refetch } = useValidatorMovements(operator, page);
  const last = data ? lastPage(data.total, MOVEMENTS_PER_PAGE) : null;
  const go = (n: number) => {
    setPage(n);
    writePlace(n);
  };
  return (
    <ListPanel as="section" aria-labelledby="validator-movements" data-testid="validator-movements">
      <PanelHead id="validator-movements" title="Recent stake movements" note="last 7 days" />
      {!data ? (
        error ? (
          <Box px="4" py="3" fontSize="13px" color="fg.muted">
            The movements could not be loaded.{" "}
            <Button variant="plain" size="xs" color="accent.fg" px="0" h="auto" onClick={() => void refetch()}>
              Try again
            </Button>
          </Box>
        ) : (
          <RowsSkeleton />
        )
      ) : (
        <>
          {!data.complete && <Note testId="stake-movements-incomplete">The last 7 days are still being indexed.</Note>}
          {data.total === 0 ? (
            <Note testId="stake-movements-empty">No stake movements in the last 7 days.</Note>
          ) : (
            <>
              <ListHead templateColumns={COLUMNS} hideBelow="c2" aria-hidden>
                <Text>When</Text>
                <Text>Account</Text>
                <Text>Movement</Text>
                <Text textAlign="right">Amount</Text>
              </ListHead>
              <Box role="list" aria-label="Recent stake movements">
                {data.items.map((m, i) => (
                  <MovementRow key={`${m.tx_hash}:${m.kind}:${i}`} m={m} />
                ))}
              </Box>
              {data.items.length === 0 && <Note testId="stake-movements-page-empty">No movements on this page.</Note>}
              <Box mt="-1px">
                <Pager
                  page={page}
                  last={last}
                  hasNext={page < (last ?? 1)}
                  href={(n) => `${validatorHref(operator)}?movements=${n}`}
                  firstHref={validatorHref(operator)}
                  numbered
                  touch
                  onGo={go}
                >
                  <Text as="span" fontFamily="mono" color="fg.muted" data-testid="stake-movements-range">
                    {movementsRange(page, MOVEMENTS_PER_PAGE, data.total)}
                  </Text>
                </Pager>
              </Box>
            </>
          )}
        </>
      )}
    </ListPanel>
  );
}
