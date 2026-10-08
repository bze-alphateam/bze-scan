# bze-scan ui

Next.js web app of the explorer, built like the other BZE dapps (React 19 and
Chakra UI), so shared transaction links get server-rendered previews and the
same container deploy pattern applies. Dark and light themes follow
dex.getbze.com. Node 24; the app is scaffolded by the first UI ticket.

The app talks only to the backend API, plus direct lazy requests to BZE archive
RPC nodes for the raw JSON shown in the "More details" sections.
