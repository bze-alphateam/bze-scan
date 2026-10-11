// A dependency-free mock of the backend API for the end-to-end tests: every
// GET answers the JSON file at test/fixtures/api/<path>.json (the query
// string ignored), e.g. /api/v1/status → test/fixtures/api/v1/status.json;
// anything else is a 404 with the API's error envelope. Specs override single
// browser-side answers with mockApi() (e2e/mock-api.ts).
//
//   node test/mock-api/server.mjs [port]   (default 8089, or MOCK_API_PORT)

import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../fixtures");
const port = Number(process.argv[2] ?? process.env.MOCK_API_PORT ?? 8089);

const headers = {
  "Content-Type": "application/json",
  "Cache-Control": "no-store",
  "Access-Control-Allow-Origin": "*",
};

function send(res, status, body) {
  res.writeHead(status, headers);
  res.end(typeof body === "string" ? body : JSON.stringify(body));
}

const notFound = (res, message) => send(res, 404, { error: { code: "not_found", message } });

const server = createServer(async (req, res) => {
  if (req.method === "OPTIONS") {
    res.writeHead(204, { ...headers, "Access-Control-Allow-Methods": "GET", "Access-Control-Allow-Headers": "*" });
    return res.end();
  }
  if (req.method !== "GET") {
    return send(res, 405, { error: { code: "bad_request", message: "only GET" } });
  }
  const { pathname } = new URL(req.url ?? "/", "http://mock");
  const file = path.resolve(root, `.${decodeURIComponent(pathname).replace(/\/+$/, "")}.json`);
  if (!file.startsWith(root + path.sep)) {
    return notFound(res, "no fixture");
  }
  try {
    send(res, 200, await readFile(file, "utf8"));
  } catch {
    notFound(res, `no fixture for ${pathname}`);
  }
});

server.listen(port, "127.0.0.1", () => {
  console.log(`mock API on http://127.0.0.1:${port}/api (fixtures in ${path.relative(process.cwd(), root)})`);
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
