import { Account, Fields, str } from "./common";
import { tokenName } from "./tokenfactory";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";
import { under, wholePct } from "./v830-common";

// The IBC rate limits (cosmos/ibc-apps rate-limiting, governance only): a
// token's daily share that may leave or enter over one channel.

const where = (msg: RenderMessage) => str(msg, "channel_or_client_id") ?? "a channel";

function limitText(msg: RenderMessage): string {
  const out = wholePct(str(msg, "max_percent_send"));
  const into = wholePct(str(msg, "max_percent_recv"));
  const hours = str(msg, "duration_hours");
  const per = hours ? ` per ${hours} h` : "";
  return out && into ? `${out} out / ${into} in${per}` : "";
}

function limitBody({ msg, ctx }: BodyProps) {
  return (
    <Fields
      rows={[
        { label: "Decided by", value: <Account address={str(msg, "authority")} ctx={ctx} /> },
        { label: "Token", value: tokenName(str(msg, "denom"), ctx) },
        { label: "Channel", value: where(msg) },
        { label: "Limit", value: limitText(msg) },
      ]}
    />
  );
}

/** `<what> for HONEY on channel-0: 2% out / 2% in per 24 h`. */
const titled = (what: string, withLimit: boolean) => (msg: RenderMessage, ctx: RenderContext) => {
  const limit = withLimit ? limitText(msg) : "";
  return `${what} ${tokenName(str(msg, "denom"), ctx)} on ${where(msg)}${limit ? `: ${limit}` : ""}`;
};

const add = renderer({ category: "governance", title: titled("Added an IBC limit for", true), Body: limitBody });
const update = renderer({ category: "governance", title: titled("Changed the IBC limit for", true), Body: limitBody });
const remove = renderer({ category: "governance", title: titled("Removed the IBC limit for", false), Body: limitBody });
const reset = renderer({ category: "governance", title: titled("Reset the used-up IBC limit for", false), Body: limitBody });

export const ratelimitRenderers: Record<string, MessageRenderer> = under("/ratelimit.v1.", {
  MsgAddRateLimit: add,
  MsgUpdateRateLimit: update,
  MsgRemoveRateLimit: remove,
  MsgResetRateLimit: reset,
});
