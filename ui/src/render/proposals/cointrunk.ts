import { text, type Reader } from "./actions";

/** `cointrunk_publisher`: a CoinTrunk publisher or accepted domain added (`active`) or removed. */
export const readCointrunk: Reader = (type, body) => {
  const add = body.active !== false;
  switch (type) {
    case "/bze.cointrunk.MsgSavePublisher":
    case "/bze.cointrunk.v1.PublisherProposal":
      return { type: "publisher", add, name: text(body, "name"), address: text(body, "address") };
    case "/bze.cointrunk.MsgAcceptDomain":
    case "/bze.cointrunk.v1.AcceptedDomainProposal":
      return { type: "domain", add, domain: text(body, "domain") };
    default:
      return null;
  }
};
