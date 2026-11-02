import { describe, expect, it } from "vitest";

import fundBurner from "../../test/fixtures/messages/burner-fund-burner.json";
import moveIbc from "../../test/fixtures/messages/burner-move-ibc-locked-coins.json";
import joinRaffle from "../../test/fixtures/messages/burner-join-raffle.json";
import startRaffle from "../../test/fixtures/messages/burner-start-raffle.json";
import send from "../../test/fixtures/messages/bank-send.json";
import deposit from "../../test/fixtures/messages/gov-deposit.json";
import submitV1beta1 from "../../test/fixtures/messages/gov-submit-proposal-v1beta1.json";
import submit from "../../test/fixtures/messages/gov-submit-proposal.json";
import voteWeighted from "../../test/fixtures/messages/gov-vote-weighted.json";
import vote from "../../test/fixtures/messages/gov-vote.json";

import type { RenderMessage } from "./messages/types";
import { appDestinations } from "./tx-apps";

const HONEY = "factory/bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw/uhoney";
const RAFFLE_HREF = `https://burner.getbze.com/coin?coin=${encodeURIComponent(HONEY)}`;

const msg = (m: object, i = 0): RenderMessage => ({ msg_index: i, ...m }) as RenderMessage;
const without = (m: RenderMessage, key: string): RenderMessage => {
  const body = { ...m.body };
  delete body[key];
  return { ...m, body, events: [] };
};

describe("appDestinations", () => {
  it("sends a raffle join or start to the coin's page on the Burner, denom encoded", () => {
    for (const m of [joinRaffle, startRaffle]) {
      expect(appDestinations([msg(m)])).toEqual([{ app: "burner", verb: "Open raffle on", name: "Burner", href: RAFFLE_HREF }]);
    }
  });

  it("sends a vote or a weighted vote to the proposal's governance page on Staking", () => {
    for (const m of [vote, voteWeighted]) {
      expect(appDestinations([msg(m)])).toEqual([
        { app: "staking", verb: "Vote on", name: "Staking", href: "https://staking.getbze.com/governance/14" },
      ]);
    }
  });

  it("reads a submitted proposal's id from its submit_proposal event", () => {
    expect(appDestinations([msg(submit)])).toEqual([
      { app: "staking", verb: "View proposal on", name: "Staking", href: "https://staking.getbze.com/governance/14" },
    ]);
    expect(appDestinations([msg(submitV1beta1)])[0]?.href).toMatch(/\/governance\/\d+$/);
    expect(appDestinations([msg(deposit)])[0]?.verb).toBe("View proposal on");
  });

  it("links nothing without a proposal id or a raffle denom, nor for other messages", () => {
    expect(appDestinations([without(msg(vote), "proposal_id")])).toEqual([]);
    expect(appDestinations([without(msg(submit), "proposal_id")])).toEqual([]);
    expect(appDestinations([without(msg(joinRaffle), "denom")])).toEqual([]);
    expect(appDestinations([msg(send)])).toEqual([]);
    expect(appDestinations([])).toEqual([]);
  });

  it("sends a burner fund to the coin's Burner page, or the Burner home for several or unparsable coins", () => {
    const withAmount = (amount: string) => msg({ ...fundBurner, body: { ...fundBurner.body, amount } });
    expect(appDestinations([msg(fundBurner)])).toEqual([
      { app: "burner", verb: "Burn more on", name: "Burner", href: "https://burner.getbze.com/coin?coin=ubze" },
    ]);
    expect(appDestinations([withAmount("5ubze,7uatom")])[0]?.href).toBe("https://burner.getbze.com");
    expect(appDestinations([withAmount("5ubze,junk")])[0]?.href).toBe("https://burner.getbze.com");
    expect(appDestinations([withAmount("junk")])[0]?.href).toBe("https://burner.getbze.com");
    expect(appDestinations([without(msg(fundBurner), "amount")])[0]?.href).toBe("https://burner.getbze.com");
  });

  it("sends moving IBC-locked coins to that denom's Burner page, or the home without a denom", () => {
    expect(appDestinations([msg(moveIbc)])[0]).toMatchObject({
      app: "burner",
      href: `https://burner.getbze.com/coin?coin=${encodeURIComponent(moveIbc.body.denom)}`,
    });
    expect(appDestinations([without(msg(moveIbc), "denom")])[0]?.href).toBe("https://burner.getbze.com");
  });

  it("gives one link per app, from the first message that has one", () => {
    const other = msg({ ...joinRaffle, body: { ...joinRaffle.body, denom: "ubze" } }, 1);
    const out = appDestinations([msg(send), msg(vote, 1), other, msg(joinRaffle, 2), msg(submit, 3)]);
    expect(out.map((d) => [d.app, d.verb, d.href])).toEqual([
      ["staking", "Vote on", "https://staking.getbze.com/governance/14"],
      ["burner", "Open raffle on", "https://burner.getbze.com/coin?coin=ubze"],
    ]);
  });
});
