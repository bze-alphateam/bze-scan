import { Link, Text } from "@chakra-ui/react";
import { LuExternalLink } from "react-icons/lu";

import { websiteLink } from "@/lib/links";

import { Account, accountName, coinsText, Coins, Fields, parseCoins, str } from "./common";
import { account, renderer, type BodyProps, type MessageRenderer } from "./types";

const addArticle = renderer({
  category: "other",
  title: (msg) => `Publish an article on CoinTrunk${str(msg, "title") ? ` · ${str(msg, "title")}` : ""}`,
  reasonFor: () => "article fee",
  Body: function AddArticleBody({ msg, ctx }: BodyProps) {
    const link = websiteLink(str(msg, "url"));
    return (
      <Fields
        rows={[
          {
            label: "Publisher",
            value: <Account address={str(msg, "publisher")} ctx={ctx} />,
          },
          {
            label: "Title",
            value: str(msg, "title") && <Text as="span">{str(msg, "title")}</Text>,
          },
          {
            label: "Article",
            value: link && (
              <Link href={link.href} target="_blank" rel="noopener noreferrer nofollow" wordBreak="break-all">
                {link.text} <LuExternalLink aria-hidden />
              </Link>
            ),
          },
        ]}
      />
    );
  },
});

const payRespect = renderer({
  category: "other",
  title: (msg, ctx) => `Pay ${coinsText(parseCoins(str(msg, "amount")), ctx)} of respect to ${accountName(str(msg, "address"), ctx)}`,
  counterparty: (msg) => account(str(msg, "address")),
  reasonFor: (t, msg) => (t.recipient === str(msg, "address") ? "respect paid to the publisher" : "community pool share"),
  Body: function PayRespectBody({ msg, ctx }: BodyProps) {
    return (
      <Fields
        rows={[
          {
            label: "From",
            value: <Account address={str(msg, "creator")} ctx={ctx} />,
          },
          {
            label: "Publisher",
            value: <Account address={str(msg, "address")} ctx={ctx} />,
          },
          {
            label: "Respect",
            value: <Coins list={parseCoins(str(msg, "amount"))} ctx={ctx} />,
          },
        ]}
      />
    );
  },
});

const C = "/bze.cointrunk.";

export const cointrunkRenderers: Record<string, MessageRenderer> = {
  [`${C}MsgAddArticle`]: addArticle,
  [`${C}MsgPayPublisherRespect`]: payRespect,
};
