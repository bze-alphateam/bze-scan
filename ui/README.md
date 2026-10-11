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
| `src/components/ui` | the Chakra provider and colour mode, atoms (`Amount`, `AddressLink`, `HashLink`, `TimeAgo`, `CopyButton`, `Avatar`, `Card`, `MoreDetails`, `JsonBlock`) and the skeletons |
| `src/lib` | `env.ts` (settings), `links.ts` (nav and app links), `format.ts` (display helpers) |
| `src/test` | the unit-test setup and `renderWithProviders` |
| `test/mock-api`, `test/fixtures` | the mock API of the end-to-end tests and its JSON answers |
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

A 4xx answer is never retried; other failures are retried twice. The hooks
apply the policies: a list hook with a `cursor` is `immutable`.

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
