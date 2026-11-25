import { Text } from "@chakra-ui/react";

import { Account, accountName, coinsText, Coins, Fields, obj, parseCoins, str } from "./common";
import { ratioPct } from "./v830-common";
import { renderer, type BodyProps, type MessageRenderer, type RenderContext, type RenderMessage } from "./types";

/** A factory token's symbol, else its subdenom (`factory/<creator>/<subdenom>`), else the denom. */
export function tokenName(denom: string | null, ctx: RenderContext): string {
  if (!denom) return "a token";
  return ctx.denom(denom).symbol ?? denom.split("/").pop() ?? denom;
}

/** The denom a message's coins string names. */
function coinsDenom(msg: RenderMessage): string | null {
  return parseCoins(str(msg, "coins"))[0]?.denom ?? null;
}

function Denom({ denom }: { denom: string | null }) {
  return denom ? (
    <Text as="span" fontFamily="mono" fontSize="sm" wordBreak="break-all">
      {denom}
    </Text>
  ) : null;
}

const createDenom = renderer({
  category: "tokens",
  title: (msg) => `Create token ${str(msg, "subdenom") ?? ""}`.trim(),
  reasonFor: () => "token creation fee",
  Body: function CreateDenomBody({ msg, ctx }: BodyProps) {
    const creator = str(msg, "creator");
    const sub = str(msg, "subdenom");
    return (
      <Fields
        rows={[
          {
            label: "Created by",
            value: <Account address={creator} ctx={ctx} />,
          },
          {
            label: "Denom",
            value: creator && sub ? <Denom denom={`factory/${creator}/${sub}`} /> : null,
          },
        ]}
      />
    );
  },
});

const mint = renderer({
  category: "tokens",
  title: (msg, ctx) => `Mint ${coinsText(parseCoins(str(msg, "coins")), ctx)} to ${accountName(str(msg, "creator"), ctx)}`,
  reasonFor: () => "minted",
  Body: function MintBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          {
            label: "Minted by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Amount",
            value: <Coins list={parseCoins(str(msg, "coins"))} ctx={ctx} />,
          },
          { label: "Denom", value: <Denom denom={coinsDenom(msg)} /> },
        ]}
      />
    );
  },
});

const burn = renderer({
  category: "tokens",
  title: (msg, ctx) => `Burn ${coinsText(parseCoins(str(msg, "coins")), ctx)}`,
  reasonFor: () => "burned",
  Body: function BurnBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          {
            label: "Burned by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Amount",
            value: <Coins list={parseCoins(str(msg, "coins"))} ctx={ctx} />,
          },
          { label: "Denom", value: <Denom denom={coinsDenom(msg)} /> },
        ]}
      />
    );
  },
});

/** The new admin: the proto field is `newAdmin`. */
const newAdmin = (msg: RenderMessage) => str(msg, "newAdmin") ?? str(msg, "new_admin");

const changeAdmin = renderer({
  category: "tokens",
  title: (msg, ctx) => {
    const name = tokenName(str(msg, "denom"), ctx);
    const to = newAdmin(msg);
    return to ? `Hand ${name}'s admin to ${accountName(to, ctx)}` : `Renounce ${name}'s admin`;
  },
  counterparty: (msg) => {
    const to = newAdmin(msg);
    return to ? { kind: "account", address: to } : null;
  },
  Body: function ChangeAdminBody({ msg, ctx }: BodyProps) {
    const to = newAdmin(msg);
    return (
      <Fields
        rows={[
          { label: "Token", value: <Denom denom={str(msg, "denom")} /> },
          {
            label: "From",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "To",
            value: to ? <Account address={to} ctx={ctx} /> : <Text as="span">nobody: the token has no admin from now on</Text>,
          },
        ]}
      />
    );
  },
});

const setMetadata = renderer({
  category: "tokens",
  title: (msg, ctx) => {
    const base = obj(msg.body?.metadata)?.base;
    return `Update ${tokenName(typeof base === "string" ? base : null, ctx)}'s metadata (name, symbol, decimals)`;
  },
  Body: function SetMetadataBody({ msg, ctx }: BodyProps) {
    const m = obj(msg.body?.metadata);
    const units = Array.isArray(m?.denom_units) ? m.denom_units.map(obj) : [];
    const display = typeof m?.display === "string" ? m.display : null;
    const decimals = units.find((u) => u?.denom === display)?.exponent;
    return (
      <Fields
        rows={[
          {
            label: "Updated by",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Token",
            value: <Denom denom={typeof m?.base === "string" ? m.base : null} />,
          },
          { label: "Name", value: typeof m?.name === "string" ? m.name : null },
          {
            label: "Symbol",
            value: typeof m?.symbol === "string" ? m.symbol : null,
          },
          {
            label: "Decimals",
            value: typeof decimals === "number" || typeof decimals === "string" ? String(decimals) : null,
          },
          {
            label: "Description",
            value: typeof m?.description === "string" ? m.description : null,
          },
        ]}
      />
    );
  },
});

/** `#1a1a1a` swatches of a light or dark palette, as text. */
function paletteText(value: unknown): string | null {
  const c = obj(value);
  if (!c) return null;
  const parts = (["background", "text", "primary", "secondary"] as const)
    .map((k) => (typeof c[k] === "string" && c[k] !== "" ? `${k} ${c[k]}` : null))
    .filter((x): x is string => x !== null);
  return parts.length ? parts.join(" · ") : null;
}

// On-chain branding is a font and two colour palettes (no logo or website).
const setBranding = renderer({
  category: "tokens",
  title: (msg, ctx) => `Update ${tokenName(str(msg, "denom"), ctx)}'s branding (font and colours)`,
  Body: function SetBrandingBody({ msg, ctx }: BodyProps) {
    const b = obj(msg.body?.branding);
    return (
      <Fields
        rows={[
          { label: "Updated by", value: <Account address={str(msg, "creator")} ctx={ctx} /> },
          { label: "Token", value: <Denom denom={str(msg, "denom")} /> },
          { label: "Font", value: typeof b?.font === "string" ? b.font : null },
          { label: "Light colours", value: paletteText(b?.light) },
          { label: "Dark colours", value: paletteText(b?.dark) },
        ]}
      />
    );
  },
});

const disableMinting = renderer({
  category: "tokens",
  title: (msg, ctx) => `Turned off minting of ${tokenName(str(msg, "denom"), ctx)} for good`,
  Body: function DisableMintingBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Turned off by", value: <Account address={str(msg, "creator")} ctx={ctx} /> },
          { label: "Token", value: <Denom denom={str(msg, "denom")} /> },
          { label: "Can it be undone", value: "No: no new coins of this token can ever be made" },
        ]}
      />
    );
  },
});

/** A ratio as "1%", "none" for zero or missing. */
const ratioOrNone = (ratio: string | null) => {
  const text = ratioPct(ratio);
  return text === null || text === "0%" ? "none" : text;
};

const setDenomConfig = renderer({
  category: "tokens",
  title: (msg, ctx) => {
    const pool = str(msg, "community_pool");
    const pools = pool ? `community pool to ${accountName(pool, ctx)}` : "community pool to none";
    return `Set ${tokenName(str(msg, "denom"), ctx)}'s ${pools}, transfer fee ${ratioOrNone(str(msg, "transfer_fee"))}, gas surcharge ${ratioOrNone(str(msg, "gas_surcharge"))}`;
  },
  Body: function SetDenomConfigBody({ msg, ctx }: BodyProps) {
    const pool = str(msg, "community_pool");
    return (
      <Fields
        rows={[
          { label: "Set by", value: <Account address={str(msg, "creator")} ctx={ctx} /> },
          { label: "Token", value: <Denom denom={str(msg, "denom")} /> },
          { label: "Community pool", value: pool ? <Account address={pool} ctx={ctx} /> : "none" },
          { label: "Transfer fee", value: ratioOrNone(str(msg, "transfer_fee")) },
          { label: "Gas surcharge", value: ratioOrNone(str(msg, "gas_surcharge")) },
        ]}
      />
    );
  },
});

const setDenomAdmin = renderer({
  category: "tokens",
  title: (msg, ctx) => `Governance gave control of ${tokenName(str(msg, "denom"), ctx)} to ${accountName(newAdmin(msg), ctx)}`,
  counterparty: (msg) => (newAdmin(msg) ? { kind: "account", address: newAdmin(msg) as string } : null),
  Body: function SetDenomAdminBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          { label: "Token", value: <Denom denom={str(msg, "denom")} /> },
          { label: "New admin", value: <Account address={newAdmin(msg)} ctx={ctx} /> },
        ]}
      />
    );
  },
});

const F = "/bze.tokenfactory.";

export const tokenfactoryRenderers: Record<string, MessageRenderer> = {
  [`${F}MsgCreateDenom`]: createDenom,
  [`${F}MsgMint`]: mint,
  [`${F}MsgBurn`]: burn,
  [`${F}MsgChangeAdmin`]: changeAdmin,
  [`${F}MsgSetDenomMetadata`]: setMetadata,
  [`${F}MsgSetDenomBranding`]: setBranding,
  [`${F}MsgDisableMinting`]: disableMinting,
  [`${F}MsgSetDenomConfig`]: setDenomConfig,
  [`${F}MsgSetDenomAdmin`]: setDenomAdmin,
};
