import type { Page } from "@playwright/test";

const RESPONSE = Symbol("response");

interface Response {
  [RESPONSE]: true;
  status: number;
  body: unknown;
}

/** An answer with a status: `respond(404, { error: … })`. */
export function respond(status: number, body: unknown): Response {
  return { [RESPONSE]: true, status, body };
}

/** A failed request, as when the API cannot be reached. */
export const networkError = "abort" as const;

type Override = Response | typeof networkError | unknown;

const isResponse = (o: unknown): o is Response => typeof o === "object" && o !== null && RESPONSE in o;

/**
 * Overrides the browser's API answers by route, keyed by the path after
 * `/api/` (`"v1/status"`): a value is answered as a 200 JSON body, a
 * `respond()` with its status, `networkError` aborts the request. Every other
 * route reaches the mock API server and its fixtures. Call before `goto`.
 */
export async function mockApi(page: Page, overrides: Record<string, Override>): Promise<void> {
  await page.route("**/api/v1/**", async (route) => {
    const key = new URL(route.request().url()).pathname.replace(/^.*?\/api\//, "");
    if (!(key in overrides)) {
      return route.fallback();
    }
    const override = overrides[key];
    if (override === networkError) {
      return route.abort("failed");
    }
    const { status, body } = isResponse(override) ? override : { status: 200, body: override };
    return route.fulfill({
      status,
      json: body,
      headers: { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store" },
    });
  });
}
