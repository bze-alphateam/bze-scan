// A dependency-free mock of the backend API for the end-to-end tests: every
// GET answers the JSON file at test/fixtures/api/<path>.json (the query
// string ignored), e.g. /api/v1/status → test/fixtures/api/v1/status.json;
// anything else is a 404 with the API's error envelope. Two computed routes:
// /v1/blocks?before=H answers the `limit` heights below H (down to the status'
// history start), made from blocks.json's rows; /v1/accounts/A/activity?page=n
// answers the account's activity.json rows (of `category`) on every page up to
// the last, with `total` = the account's activity_count (the category's row
// count with one). Specs override single browser-side answers with mockApi()
// (e2e/mock-api.ts).
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

const fixture = async (name) => JSON.parse(await readFile(path.join(root, "api", "v1", `${name}.json`), "utf8"));

/** `/blocks?before=H&limit=n`: blocks.json's rows renumbered as H−1, H−2, … (6 s apart), not below the history start. */
async function blocksBefore(res, before, limit) {
  const [{ items }, status] = await Promise.all([fixture("blocks"), fixture("status")]);
  const oldest = status.back_fill.oldest_height ?? 1;
  const newest = items[0];
  const rows = [];
  for (let h = before - 1; h >= Math.max(oldest, before - limit); h--) {
    const t = items[rows.length % items.length];
    const time = new Date(Date.parse(newest.time) - (newest.height - h) * 6000).toISOString().replace(".000Z", "Z");
    rows.push({ ...t, height: h, time, hash: h.toString(16).toUpperCase().padStart(64, "0") });
  }
  send(res, 200, { items: rows, next_cursor: null });
}

/** `/accounts/A/activity?page=n[&category=]`: a numbered page with its total. */
async function activityPage(res, address, page, limit, category) {
  try {
    const [{ items }, account] = await Promise.all([fixture(`accounts/${address}/activity`), fixture(`accounts/${address}`)]);
    const rows = category ? items.filter((r) => r.category === category) : items;
    const total = category ? rows.length : account.activity_count;
    send(res, 200, { items: page <= Math.ceil(total / limit) ? rows.slice(0, limit) : [], next_cursor: null, total });
  } catch {
    notFound(res, `no activity fixture for ${address}`);
  }
}

const server = createServer(async (req, res) => {
  if (req.method === "OPTIONS") {
    res.writeHead(204, { ...headers, "Access-Control-Allow-Methods": "GET", "Access-Control-Allow-Headers": "*" });
    return res.end();
  }
  if (req.method !== "GET") {
    return send(res, 405, { error: { code: "bad_request", message: "only GET" } });
  }
  const { pathname, searchParams } = new URL(req.url ?? "/", "http://mock");
  const before = Number(searchParams.get("before"));
  if (pathname.replace(/\/+$/, "") === "/api/v1/blocks" && Number.isSafeInteger(before) && before > 0) {
    return blocksBefore(res, before, Number(searchParams.get("limit")) || 25);
  }
  const activity = pathname.match(/^\/api\/v1\/accounts\/(bze1[0-9a-z]+)\/activity\/?$/);
  const page = Number(searchParams.get("page"));
  if (activity && Number.isSafeInteger(page) && page > 0) {
    return activityPage(res, activity[1], page, Number(searchParams.get("limit")) || 25, searchParams.get("category"));
  }
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
