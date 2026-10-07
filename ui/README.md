# bze-scan ui

React web app of the explorer. The framework and tooling are chosen with the
first UI ticket (the other BZE dapps use Next.js with Chakra UI; a Vite
single-page app is the lighter alternative). Dark and light themes follow
dex.getbze.com.

The app talks only to the backend API, plus direct lazy requests to BZE archive
RPC nodes for the raw JSON shown in the "More details" sections.
