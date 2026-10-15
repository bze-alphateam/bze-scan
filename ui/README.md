# bze-scan ui

The explorer's web app: Next.js 16 (App Router, TypeScript strict), React 19,
Chakra UI v3 with the Terminal system in `src/components/ui/provider.tsx`
(IBM Plex Sans and Mono, the BZE cyan accent, dark by default; components use
its tokens, never a colour), TanStack Query v5, `bignumber.js` and Lucide icons through
`react-icons`. Node 24 (`.nvmrc`), pnpm pinned in `package.json`.

The app talks only to the backend API (`backend/README.md`, "HTTP API"),
including for the raw JSON of the "More details" sections, which the backend
proxies from archive nodes. Never a node URL in `ui/`.

## Structure

| Path | Holds |
| --- | --- |
| `src/app` | routes; `layout.tsx` (header, footer), `providers.tsx` (the composition root), `not-found.tsx`, `error.tsx` |
| `src/api` | `types.ts` (the API's shapes), `client.ts` (the `ExplorerApi` interface, `createHttpApi`, `ApiError`), `provider.tsx` (`ApiProvider`, `useApi`), `hooks.ts` (one React Query hook per route), `policy.ts` (the refetch policies) |
| `src/components/layout` | network strip (the `/stats` summary above the header), header (nav, search box, Apps menu, theme toggle, phone drawer), footer, health pill (Healthy / Stalled and its popover); `search-box.tsx`: an identifier opens its page on submit, other text lists the matching validators and labelled accounts under the box while typing (debounced 250 ms, arrows/Enter/Escape), the form submits to `/search` without JavaScript |
| `src/components/home` | the home page: hero with the search box, six live tiles from `/stats`, the latest blocks and transactions, the app links |
| `src/components/search` | the `/search` results page (rendered on the server by `src/app/search/page.tsx`, which redirects an identifier to its page) and the result row shared with the dropdown |
| `src/components/txs` | the transactions list (status chips, live until an older page is loaded) and `useListRenderContext` (the renderers' context for list rows) |
| `src/components/ui` | the Chakra provider and colour mode, atoms (`Amount`, `AddressLink`, `HashLink`, `TimeAgo`, `CopyButton`, `Avatar`, `Card`, `MoreDetails`, `JsonBlock`, `StatusPill` and `Tag`, `Chip`, `StateBox`, `ListPanel`/`ListHead`/`ListRow`, `ActionCard`, `AccountChip`, `ValidatorChip`, `Tile`) and the skeletons |
| `src/components/funds-flow` | "Where the funds went": `StepList` (one step per transfer, collapsed to a summary line on simple transactions) and `net.ts` (an address's per-denom net) |
| `src/components/blocks` | the blocks list (live until an older page is loaded, then a snapshot) and the block page (header, overview, transactions in one line each, the block's funds flow by group, More details with the raw JSON tabs, the not-found explanations) |
| `src/components/tx` | the transaction page's sections, states and skeleton |
| `src/components/accounts` | the account page: header, tiles, and the Activity (placeholder), Tokens and Staking tabs, the tab kept in `?tab=` |
| `src/components/validators` | the validators list (tiles, Active/Inactive tabs, name filter) and the validator page |
| `src/components/tokens` | the tokens list (tiles, kind chips, name filter) and the token page (supply box, about, top holders, recent activity, More details) |
| `src/components/parameters` | the chain parameters page: last change, anchor nav, one table per group (plain name, human and stored value, what it means), the raw-values box |
| `src/components/proposals` | the proposals list (voting cards, status chips, title filter) and the proposal page (what it does, votes and checks, where to vote, who voted, timeline, More details) |
| `src/render/messages` | the plain-word message registry (below) |
| `src/render/errors.ts` | why a failed transaction failed, from its codespace and code |
| `src/render/blocks.ts`, `src/render/block-flow.ts` | the blocks pages' words and numbers: why a height is not found, fee and gas sums; the block's transfers grouped by the label names the backend seeds (Fees, DEX, Staking, Rewards programs, Burner, Cross-chain, Governance, Payments, Other) with each group's line |
| `src/render/txs.ts` | a transaction in one line (its first message's registry title, "+ N more actions") and its counterparty (its renderer's `counterparty`) |
| `src/render/home.ts` | the home tiles' words and the price change's sign and colour |
| `src/render/account.ts` | the account page's words and numbers: label chips, the tiles' totals, the Tokens tab's join, origin tags, values and sorting, the Staking tab's shares and unlock days |
| `src/render/proposals` | the governance pages' words and numbers: one reader per proposal kind turning `messages` (legacy contents included) into actions, kind and status labels, the tally shares, the chain's three checks and the verdict, the upgrade date estimate, who has not voted, the timeline |
| `src/render/tokens.ts` | the tokens pages' words and numbers: the list's tiles, chips and price cells, the denom in the URL, the supply box's state and mint/burn totals, token events and transfers in plain words, the activity merge |
| `src/render/validators.ts` | the validators pages' words and numbers: active/inactive split, rates, the reward rate after commission, the history in plain words |
| `src/lib` | `env.ts` (settings), `links.ts` (nav and app links, `websiteLink`), `format.ts` (display helpers), `search.ts` (`classifyQuery`: what a query is without a request; result links and order), `use-debounced.ts`, `math.ts` (decimal sums, values and shares on string amounts: the only `bignumber.js` math pages use), `parameters.ts` (the chain parameters dictionary, below) |
| `src/test` | the unit-test setup and `renderWithProviders` |
| `test/mock-api`, `test/fixtures` | the mock API of the end-to-end tests and its JSON answers; `test/fixtures/messages` holds one message per renderer for the unit tests |
| `e2e` | Playwright specs, the `mockApi` helper and `noHorizontalScroll` (`layout.ts`) |

## Scripts

| Script | Does |
| --- | --- |
| `pnpm build` | `next build` |
| `pnpm build:e2e` | `next build` with the browser pointed at the mock API (port 8089), for `pnpm e2e` |
| `pnpm lint` | ESLint (`eslint-config-next`) |
| `pnpm typecheck` | `tsc --noEmit` |
| `pnpm test` | unit tests (vitest) |
| `pnpm e2e` | end-to-end tests (Playwright; starts the mock API and `next start`) |
| `pnpm check` | lint, typecheck and unit tests: what CI runs besides the build and e2e |
| `pnpm dev`, `pnpm start` | the development and production servers |

CI runs three workflows on pull requests touching `ui/`, one check each:
`ui-lint` (lint, typecheck, build), `ui-unit` and `ui-e2e`, plus `ui-image`,
which builds and smoke-tests the container image. On `main`, `ui-image` runs
`ui-unit` and `ui-e2e` first and publishes the image only when both pass.

The development VM never starts the app: build, lint, typecheck and unit
tests only. The end-to-end tests run in CI, or on a developer machine with
`pnpm build:e2e && pnpm exec playwright install chromium && pnpm e2e`.

## Settings

`.env.dist` lists every key with its local value; copy it to `.env.local`.
`NEXT_PUBLIC_*` values are inlined at build time.

| Key | Meaning |
| --- | --- |
| `NEXT_PUBLIC_API_URL` | the API as the browser reaches it, without `/v1` (production `https://explorer.getbze.com/api`) |
| `API_URL` | the API as the Next.js server reaches it; falls back to `NEXT_PUBLIC_API_URL` |
| `NEXT_PUBLIC_CHAIN_ID` | the chain named in the footer (`beezee-1`) |
| `NEXT_PUBLIC_NETWORK_LABEL` | the network tag in the strip above the header (`Mainnet`) |
| `NEXT_PUBLIC_PINGPUB_URL` | the Ping.pub explorer link |
| `NEXT_PUBLIC_SITE_URL` | the site's own URL, base of absolute links and previews |
| `NEXT_PUBLIC_STAKING_URL`, `NEXT_PUBLIC_DEX_URL`, `NEXT_PUBLIC_BURNER_URL`, `NEXT_PUBLIC_FACTORY_URL`, `NEXT_PUBLIC_COMMUNITIES_URL` | the BZE apps the explorer links to (Apps menu, Stake, Trade, Claim on Staking, the DEX's token and market pages); empty means the mainnet hosts, a testnet build sets the `testnet-` ones |
| `NEXT_PUBLIC_TX_PENDING_WINDOW_MS` | how long the transaction page looks for a hash the API does not know yet; empty means two minutes (`pnpm build:e2e` sets 4 s) |

## Data

Only `src/api` calls `fetch`. Components get the API from `useApi()` (or a
hook in `hooks.ts`) as the `ExplorerApi` interface; `providers.tsx` is the
only place `createHttpApi` is called, and tests pass a fake.

Every query picks one policy from `src/api/policy.ts` and never sets the
numbers itself:

| Policy | For | Behaviour |
| --- | --- | --- |
| `live` | home, the first page of a list, accounts, validators, tokens, proposals | stale after 4 s, refetched every 4 s, paused in background tabs |
| `immutable` | a block by height, a found transaction, older list pages, raw JSON | fetched once, kept for an hour, never refetched |
| `status` | the footer's health pill, the parameters page | refetched every 60 s |
| `reference` | lookups a page decorates with: validator monikers, token symbols and prices, the chain parameters | fresh for 60 s, never polled |

A 4xx answer is never retried; other failures are retried twice. The hooks
apply the policies: a list hook with a `cursor` is `immutable`. `useBlock` does the same for a height above the explorer's
latest block (`waitForIt`), with `expired` once the window is over.

`useBlockList` and `useTransactionList(status)` are infinite queries: the
first page is `live`; once an older page is loaded the list is a snapshot,
never refetched, until `latest()` drops the older pages. `useBlocks` and
`useTransactions` read one page (the home page's five). `useBlockTransfers`
follows every page of a block's transfers. `useSearch` is never polled.

A transaction the API answers 404 for is pending, never failed:
`useTransaction` asks again every 4 s for the pending window
(`TX_PENDING_WINDOW_MS`, two minutes), then reports `not_found`; once found it
is `immutable`, except while `isUnsettled`: the order book has not processed
its DEX messages (`dex.pending`) or a cross-chain transfer it created is
`pending` and not expired. It is then refetched every 4 s (`live`).

`useAccountActivity(address, { category })` is the same live-until-older
list over the account's activity. `useAccountIbc(address)` reads the
account's latest 100 cross-chain transfers in both directions (`live`), only
to give activity rows the other chain's name and the transfer's status.

`useAllValidators` follows `next_cursor` to the last page (100 per page, all
statuses) and keeps the first page's `summary`; the list and the validator
page share it.

`useDenoms` pages through the tokens list the same way and returns a
`Map` by denom (`reference` policy); the account page joins balances to it
for symbols, logos, origins and USD values. `useAllTokens` reads the same
pages with the `live` policy for the tokens page, which filters the kinds
itself so the tiles always count every token.

A token's routes take the denom URL-encoded on the path
(`/tokens/factory%2Fbze1…%2Fuhoney`); the client retries a denom with `/` in
the `?denom=` form when the path answers 404, in case a proxy decodes `%2F`.
The page route is a catch-all, so `/tokens/factory/bze1…/uhoney` opens it
too. `useTokenHolders` (the top 10, then 100 a page) and `useTokenTransfers`
(20 a page) are infinite queries the page grows on request;
`useTokenEventsAfter` reads every event older than the token's newest 20, so
the mint and burn totals count them all.

`useProposals(status)` is an infinite query, 20 a page, one cache entry per
status chip (a chip change shows skeleton rows). `useProposal` polls while a
proposal collects deposits or votes and stops once it resolves.
`useValidatorVotes` reads every vote of a validator owner
(`?voter=validators`) for the validators tab and the not-voted list;
`useProposalVotes` pages everyone's votes, by option. Once a tallied proposal
has an `active_set`, the not-voted list is `useProposalNonVoters` (the
validators of the set the tally counted, recorded by the backend, read once);
until then it falls back to today's active set, said so. The checks read the
thresholds from `useParams()` (gov), never constants; an upgrade's date is
estimated from `useStatus()`'s height and `useStats()`'s block time. Where to
vote is data: `voteDestinations()` in `src/lib/links.ts`.

## Chain parameters

`src/lib/parameters.ts` is the dictionary of the parameters page: one
`ParamDef` per row (`group`, the `/params` `module`, the dotted `paths` inside
its params, a plain `name`, a `format` per path, an optional `show` composing
the value cell and `explain(values, ctx)`, which may read `/stats` and the
other modules). Only words live there: every number comes from the live
answer, and "Inflation right now" from `/stats`. A path the answer lacks
renders "not available"; a key no row covers is appended to its group as an
"advanced" row, raw. `useParams({ poll: true })` refetches every minute on
that page; elsewhere it is `reference`. Parameter-change proposals name their
keys with `describeParam` and link to the module's group (`groupOfModule`).

## Messages in plain words

`src/render/messages/registry.ts` maps a type URL to a `MessageRenderer`:
`category` (the action card's icon and label: `send`, `staking`,
`governance`, `cross_chain`, `dex`, `burner`, `tokens`, `rewards`, `other`), `title(msg, ctx)` (the card's
heading, also the page description), `summary(msg, ctx)` (one line for lists,
the title by default), `Body` (the card's content) and an optional
`reasonFor(transfer, msg, ctx)` (why one of its transfers happened, for the
funds flow; the title otherwise) and an optional `counterparty(msg)` (who the
signer dealt with, for "signer → counterparty" on the home page: an account,
a validator or a name). A type without a renderer goes through the
fallback: its type in words and its body as a key/value table.

Renderers read the API's proto JSON body (snake_case) and the message's
events, and look everything else up in the `RenderContext` from
`createRenderContext`: the transaction's transfers, denoms (symbol and
exponent from the transfers, then the tokens list, with its USD price),
validator monikers, the transfers' labels, and on the transaction page the
API's `dex` outcome and `ibc_transfers` (empty in lists). They decode nothing
themselves, and a missing field never throws. One file per module family
(`bank`, `staking`, `distribution`, `gov`, `ibc`, `authz`, and BZE's
`tradebin`, `burner`, `tokenfactory`, `rewards`, `cointrunk`); a new renderer
adds a fixture in `test/fixtures/messages` and its expected title in
`registry.test.tsx`. Every BZE type the backend classifies
(`backend/internal/classify/classify.go`) must have a renderer:
`bze.test.tsx` reads that file and fails on one that falls back.

BZE specifics:

- DEX (`dex.ts`): a market id `base/quote` is split by denom kind (`ibc/…`
  two parts, `factory/…` three); a price is quote base units per base base
  unit, shown × 10^(base exponent − quote exponent); links go to
  `dex.getbze.com/exchange/market?id=`. `outcomeOf` picks the part of `dex`
  that belongs to a message by the queue message ids of its
  `OrderCreateMessageEvent`/`OrderCancelMessageEvent` (chain v8.2.0+), by
  market and owner before that; `DexOutcome` shows pending (amber, never
  failed), the fills, the remainder on the book and cancels. The order's
  held funds are the signer's largest transfer to the DEX in the denom it
  spends; its other transfers to and through the DEX are the order fee. A DEX
  transaction always opens its funds flow with the net box.
- Cross-chain (`ibc-status.tsx`): `ibcRowFor` matches a `MsgTransfer` by its
  source channel and `send_packet` sequence, a relay message by the packet in
  its body; `IbcStatus` is the chip (Pending, Expired, Delivered, Refunded,
  Received, Rejected), the settling transaction and the other chain's
  explorer from the registry's `${accountAddress}` template (https only).

## Account activity

`src/render/activity.tsx` turns an activity row into a sentence from what the
row carries (`kind`, `counterparty`, `deltas`, `details`, `msg_types`,
`success`, `is_signer`); a row is never completed by fetching its
transaction. `activityLine` gives the parts (words, and the counterparty as a
chip: a validator by moniker, a module account dashed) and the chips (the
cross-chain status, Failed). Block-level rows read their `details`: a DEX fill
is summed per market and side (the event's `order_type` is the resting
order's, so the taker did the opposite), a raffle or trading-reward win shows
the amount. `ibcFor` matches a sent transfer by its creating transaction, a
received one by the transaction that brought it in and a refund by the
settling transaction. The amounts column (`deltaLines`) shows the deltas
without the fee and the fee on its own line. An unknown kind reads as its
first message type in words; a unit test walks every kind in
`backend/internal/classify/classify.go` and fails on one without a sentence.

## Loading states

The first load of a data set shows a skeleton with the final layout's size,
never a spinner or an empty page, so nothing shifts when the data lands. The
atoms are in `src/components/ui/skeleton.tsx`: `TileSkeleton`,
`CardSkeleton`, `TableSkeleton({ rows, columns })`, `ListSkeleton({ rows })`
and `JsonSkeleton`.

- A skeleton shows on the first load of a page, of a tab the first time it
  opens, and of a list whose filter changes to a different data set.
- Polling refetches never show one: the previous data stays on screen.
- Paginating appends skeleton rows under the rows already shown.
- A lazily loaded section (raw JSON, holders) shows its own skeleton.
- An error replaces the skeleton with the error state; a skeleton is never
  left on screen.
- The health pill's loading state is its grey dot ("Unknown").

## Container image

`next.config.ts` sets `output: "standalone"`, so `next build` also writes a
self-contained server to `.next/standalone`. `docker/ui.Dockerfile` packs it
into `ghcr.io/bze-alphateam/bze-scan-ui` (see `docker/README.md`). Build it
locally from the repository root, with `.env.dist` (or your own file) as the
build env:

```
docker build -f docker/ui.Dockerfile --secret id=build_env,src=ui/.env.dist --build-arg GIT_SHA=$(git rev-parse --short=8 HEAD) .
```

The build env is read as `.env.production`: its `NEXT_PUBLIC_*` values are
baked into the bundle, and the file itself is left out of the image. `API_URL`
is read by the server at run time, so it comes from the container
environment; unset, the server uses `NEXT_PUBLIC_API_URL`.

## Tests

Unit tests (vitest, jsdom, Testing Library) sit beside the code as
`*.test.ts(x)`. `renderWithProviders(ui, { api })` from `src/test/render.tsx`
renders inside the app's providers; `fakeApi({ …methods })` answers the given
methods and rejects any other call; `pendingApi()` never answers, for
asserting a skeleton. jsdom applies Chakra's base styles only, so components
render their phone layout there.

End-to-end tests (Playwright, Chromium, a 1280 px desktop and a 390 px phone
project) run against the production build. `test/mock-api/server.mjs`, a
dependency-free Node server on port 8089, answers every `GET /api/<path>`
from `test/fixtures/api/<path>.json` (for example
`test/fixtures/api/v1/status.json`) and 404s with the API's error envelope
otherwise. A spec overrides single answers in the browser with
`mockApi(page, { "v1/status": body })` from `e2e/mock-api.ts`
(`respond(status, body)` for another status, `networkError` for a failed
request); alternative answers live in `test/fixtures/variants`.
