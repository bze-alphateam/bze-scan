# bze-scan ui

The explorer's web app: Next.js 16 (App Router, TypeScript strict), React 19,
Chakra UI v3 with the system of dex.getbze.com (Inter, the blue palette, dark
by default), TanStack Query v5, `bignumber.js` and Lucide icons through
`react-icons`. Node 24 (`.nvmrc`), pnpm pinned in `package.json`.

The app talks only to the backend API (`backend/README.md`, "HTTP API"),
including for the raw JSON of the "More details" sections, which the backend
proxies from archive nodes. Never a node URL in `ui/`.

## Structure

| Path | Holds |
| --- | --- |
| `src/app` | routes; `layout.tsx` (header, footer), `providers.tsx` (the composition root), `not-found.tsx`, `error.tsx` |
| `src/api` | `types.ts` (the API's shapes), `client.ts` (the `ExplorerApi` interface, `createHttpApi`, `ApiError`), `provider.tsx` (`ApiProvider`, `useApi`), `hooks.ts` (one React Query hook per route), `policy.ts` (the refetch policies) |
| `src/components/layout` | header (nav, search box, Apps menu, theme toggle, phone drawer), footer, status light |
| `src/components/ui` | the Chakra provider and colour mode, atoms (`Amount`, `AddressLink`, `HashLink`, `TimeAgo`, `CopyButton`, `Avatar`, `Card`, `MoreDetails`, `JsonBlock`, `StatusPill`, `ActionCard`, `AccountChip`, `ValidatorChip`, `Tile`) and the skeletons |
| `src/components/funds-flow` | "Where the funds went": `StepList` (one step per transfer, collapsed to a summary line on simple transactions) and `net.ts` (an address's per-denom net) |
| `src/components/tx` | the transaction page's sections, states and skeleton |
| `src/components/validators` | the validators list (tiles, Active/Inactive tabs, name filter) and the validator page |
| `src/render/messages` | the plain-word message registry (below) |
| `src/render/errors.ts` | why a failed transaction failed, from its codespace and code |
| `src/render/validators.ts` | the validators pages' words and numbers: active/inactive split, rates, the reward rate after commission, the history in plain words |
| `src/lib` | `env.ts` (settings), `links.ts` (nav and app links), `format.ts` (display helpers) |
| `src/test` | the unit-test setup and `renderWithProviders` |
| `test/mock-api`, `test/fixtures` | the mock API of the end-to-end tests and its JSON answers; `test/fixtures/messages` holds one message per renderer for the unit tests |
| `e2e` | Playwright specs and the `mockApi` helper |

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

CI runs three workflows on changes under `ui/`, one check each: `ui-lint`
(lint, typecheck, build), `ui-unit` and `ui-e2e`.

The development VM never starts the app: build, lint, typecheck and unit
tests only. The end-to-end tests run in CI, or on a developer machine with
`pnpm build:e2e && pnpm exec playwright install chromium && pnpm e2e`.

## Settings

`.env.dist` lists every key with its local value; copy it to `.env.local`.
`NEXT_PUBLIC_*` values are inlined at build time.

| Key | Meaning |
| --- | --- |
| `NEXT_PUBLIC_API_URL` | the API as the browser reaches it, without `/v1` (production `https://scan.getbze.com/api`) |
| `API_URL` | the API as the Next.js server reaches it; falls back to `NEXT_PUBLIC_API_URL` |
| `NEXT_PUBLIC_CHAIN_ID` | the chain named in the footer (`beezee-1`) |
| `NEXT_PUBLIC_NETWORK_LABEL` | the header's network badge (`Mainnet`) |
| `NEXT_PUBLIC_PINGPUB_URL` | the Ping.pub explorer link |
| `NEXT_PUBLIC_SITE_URL` | the site's own URL, base of absolute links and previews |
| `NEXT_PUBLIC_TX_PENDING_WINDOW_MS` | how long the transaction page looks for a hash the API does not know yet; empty means two minutes (`pnpm build:e2e` sets 4 s) |

## Data

Only `src/api` calls `fetch`. Components get the API from `useApi()` (or a
hook in `hooks.ts`) as the `ExplorerApi` interface; `providers.tsx` is the
only place `createHttpApi` is called, and tests pass a fake.

Every query picks one policy from `src/api/policy.ts` and never sets the
numbers itself:

| Policy | For | Behaviour |
| --- | --- | --- |
| `live` | home, the first page of a list, accounts, validators, tokens, proposals | stale after 7 s, refetched every 7 s, paused in background tabs |
| `immutable` | a block by height, a found transaction, older list pages, raw JSON | fetched once, kept for an hour, never refetched |
| `status` | the status light | refetched every 60 s |
| `reference` | lookups a page decorates with: validator monikers, token symbols and prices, the chain parameters | fresh for 60 s, never polled |

A 4xx answer is never retried; other failures are retried twice. The hooks
apply the policies: a list hook with a `cursor` is `immutable`.

A transaction the API answers 404 for is pending, never failed:
`useTransaction` asks again every 7 s for the pending window
(`TX_PENDING_WINDOW_MS`, two minutes), then reports `not_found`; once found it
is `immutable`.

`useAllValidators` follows `next_cursor` to the last page (100 per page, all
statuses) and keeps the first page's `summary`; the list and the validator
page share it.

## Messages in plain words

`src/render/messages/registry.ts` maps a type URL to a `MessageRenderer`:
`category` (the action card's icon and label: `send`, `staking`,
`governance`, `cross_chain`, `other`), `title(msg, ctx)` (the card's
heading, also the page description), `summary(msg, ctx)` (one line for lists,
the title by default), `Body` (the card's content) and an optional
`reasonFor(transfer, msg, ctx)` (why one of its transfers happened, for the
funds flow; the title otherwise). A type without a renderer goes through the
fallback: its type in words and its body as a key/value table.

Renderers read the API's proto JSON body (snake_case) and the message's
events, and look everything else up in the `RenderContext` from
`createRenderContext`: the transaction's transfers, denoms (symbol and
exponent from the transfers, then the tokens list, with its USD price),
validator monikers and the transfers' labels. They decode nothing
themselves, and a missing field never throws. One file per module family
(`bank`, `staking`, `distribution`, `gov`, `ibc`, `authz`); a new renderer
adds a fixture in `test/fixtures/messages` and its expected title in
`registry.test.tsx`.

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
- The status light's loading state is its grey dot.

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
