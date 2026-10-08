# bze-scan ui

Next.js web app of the explorer, built like the other BZE dapps (React 19 and
Chakra UI), so shared transaction links get server-rendered previews and the
same container deploy pattern applies. Dark and light themes follow
dex.getbze.com. Node 24; the app is scaffolded by the first UI ticket.

The app talks only to the backend API, including for the raw JSON shown in the
"More details" sections, which the backend proxies from archive nodes. Live
views (home, the block and transaction lists, the account page) refetch every
7 seconds through React Query and pause in background tabs; a block or a found
transaction is fetched once and never refetched.
