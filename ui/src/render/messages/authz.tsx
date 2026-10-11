import { Box, Stack, Text } from "@chakra-ui/react";

import { Account, accountName, Fields, humanise, obj, str } from "./common";
import { renderer, type BodyProps, type MessageRenderer, type RenderMessage } from "./types";

const AUTHORIZATIONS: Record<string, string> = {
  "/cosmos.bank.v1beta1.SendAuthorization": "send funds",
  "/cosmos.staking.v1beta1.StakeAuthorization": "manage stake",
};

/** What a grant allows, in words: "vote", "send funds". */
function permission(msg: RenderMessage): string {
  const typeUrl = str(msg, "msg_type_url");
  if (typeUrl) return humanise(typeUrl).toLowerCase();
  const auth = obj(obj(msg.body?.grant)?.authorization);
  const type = typeof auth?.["@type"] === "string" ? auth["@type"] : "";
  if (AUTHORIZATIONS[type]) return AUTHORIZATIONS[type];
  if (typeof auth?.msg === "string") return humanise(auth.msg).toLowerCase();
  return "act for them";
}

function GrantBody({ msg, ctx }: BodyProps) {
  return (
    <Fields
      rows={[
        { label: "Granted by", value: <Account address={str(msg, "granter")} ctx={ctx} /> },
        { label: "Granted to", value: <Account address={str(msg, "grantee")} ctx={ctx} /> },
        { label: "Permission", value: permission(msg) },
      ]}
    />
  );
}

const grant = renderer({
  category: "other",
  title: (msg, ctx) => `Allow ${accountName(str(msg, "grantee"), ctx)} to ${permission(msg)}`,
  Body: GrantBody,
});

const revoke = renderer({
  category: "other",
  title: (msg, ctx) => `Stop allowing ${accountName(str(msg, "grantee"), ctx)} to ${permission(msg)}`,
  Body: GrantBody,
});

/** The messages an authz `MsgExec` runs, as renderable messages (they carry their type in `@type`). */
function inner(msg: RenderMessage): RenderMessage[] {
  const msgs = msg.body?.msgs;
  if (!Array.isArray(msgs)) return [];
  return msgs.map((m) => {
    const { "@type": type, ...body } = obj(m) ?? {};
    return {
      type_url: typeof type === "string" ? type : "",
      sender: null,
      module: null,
      body,
      // The exec's events are its inner messages' events, emitted together.
      events: msg.events,
      msg_index: null,
    };
  });
}

const exec = renderer({
  category: "other",
  title: (msg, ctx) => {
    const list = inner(msg);
    if (list.length === 1) return ctx.rendererFor(list[0].type_url).title(list[0], ctx);
    return `Run ${list.length} actions with a permission`;
  },
  reasonFor: (transfer, msg, ctx) => {
    const list = inner(msg);
    return list.length === 1 ? (ctx.rendererFor(list[0].type_url).reasonFor?.(transfer, list[0], ctx) ?? exec.title(msg, ctx)) : exec.title(msg, ctx);
  },
  Body: function ExecBody({ msg, ctx }: BodyProps) {
    const list = inner(msg);
    return (
      <Stack gap="4">
        <Fields rows={[{ label: "Run by", value: <Account address={str(msg, "grantee")} ctx={ctx} /> }]} />
        {list.map((m, i) => {
          const r = ctx.rendererFor(m.type_url);
          return (
            <Box key={i} borderStartWidth="2px" borderColor="border.panel" ps="4" data-testid="exec-inner">
              <Text fontWeight="semibold" mb="2">
                {list.length > 1 ? `${i + 1}. ` : ""}
                {r.title(m, ctx)}
              </Text>
              <r.Body msg={m} ctx={ctx} />
            </Box>
          );
        })}
      </Stack>
    );
  },
});

function AllowanceBody({ msg, ctx }: BodyProps) {
  return (
    <Fields
      rows={[
        { label: "Paid by", value: <Account address={str(msg, "granter")} ctx={ctx} /> },
        { label: "For", value: <Account address={str(msg, "grantee")} ctx={ctx} /> },
      ]}
    />
  );
}

const grantAllowance = renderer({
  category: "other",
  title: (msg, ctx) => `Pay the fees of ${accountName(str(msg, "grantee"), ctx)}`,
  Body: AllowanceBody,
});

const revokeAllowance = renderer({
  category: "other",
  title: (msg, ctx) => `Stop paying the fees of ${accountName(str(msg, "grantee"), ctx)}`,
  Body: AllowanceBody,
});

export const authzRenderers: Record<string, MessageRenderer> = {
  "/cosmos.authz.v1beta1.MsgGrant": grant,
  "/cosmos.authz.v1beta1.MsgRevoke": revoke,
  "/cosmos.authz.v1beta1.MsgExec": exec,
  "/cosmos.feegrant.v1beta1.MsgGrantAllowance": grantAllowance,
  "/cosmos.feegrant.v1beta1.MsgRevokeAllowance": revokeAllowance,
};
