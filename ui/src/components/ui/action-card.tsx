import { Box, Grid, HStack, Heading, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";
import type { IconType } from "react-icons";
import { LuArrowLeftRight, LuChartCandlestick, LuCoins, LuFlame, LuGift, LuLandmark, LuSend, LuShapes, LuSparkles } from "react-icons/lu";

import { Tag } from "./status-pill";

/** What kind of action a message is, for its icon and label. */
export type ActionCategory =
  | "send"
  | "staking"
  | "governance"
  | "cross_chain"
  | "dex"
  | "burner"
  | "tokens"
  | "rewards"
  | "other";

const CATEGORIES: Record<ActionCategory, { icon: IconType; label: string; palette: string }> = {
  send: { icon: LuSend, label: "Send", palette: "blue" },
  staking: { icon: LuCoins, label: "Staking", palette: "purple" },
  governance: { icon: LuLandmark, label: "Governance", palette: "orange" },
  cross_chain: { icon: LuArrowLeftRight, label: "Cross-chain", palette: "teal" },
  dex: { icon: LuChartCandlestick, label: "DEX", palette: "cyan" },
  burner: { icon: LuFlame, label: "Burner", palette: "red" },
  tokens: { icon: LuSparkles, label: "Tokens", palette: "pink" },
  rewards: { icon: LuGift, label: "Rewards", palette: "yellow" },
  other: { icon: LuShapes, label: "Other", palette: "gray" },
};

/** A category's icon, label and colour palette (the action cards and the account's activity rows). */
export function categoryLook(category: ActionCategory): { icon: IconType; label: string; palette: string } {
  return CATEGORIES[category];
}

interface ActionCardProps {
  /** 1-based position among the transaction's actions. */
  index: number;
  category: ActionCategory;
  title: string;
  /** A failed transaction's actions did not happen. */
  notExecuted?: boolean;
  children?: ReactNode;
}

/** One numbered action of a transaction: its icon tile, "1. DEX", the plain-word title and the body. */
export function ActionCard({ index, category, title, notExecuted, children }: ActionCardProps) {
  const { icon: Icon, label } = CATEGORIES[category];
  return (
    <Grid
      as="article"
      templateColumns="36px minmax(0, 1fr)"
      gap="14px"
      p="4"
      bg="bg.panel"
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l3"
      data-testid="action-card"
      data-category={category}
    >
      <Box display="grid" placeItems="center" boxSize="36px" rounded="l2" bg="accent.subtle" color="accent.fg" aria-hidden>
        <Icon size={18} />
      </Box>
      <Box minW="0">
        <HStack gap="2" wrap="wrap">
          <Text fontSize="11px" fontWeight="600" letterSpacing="0.06em" textTransform="uppercase" color="fg.muted">
            {index}. {label}
          </Text>
          {notExecuted && <Tag tone="bad">not executed</Tag>}
        </HStack>
        <Heading as="h3" mt="2px" fontSize="16px" fontWeight="600" lineHeight="1.4" wordBreak="break-word">
          {title}
        </Heading>
        {children && <Box mt="10px">{children}</Box>}
      </Box>
    </Grid>
  );
}
