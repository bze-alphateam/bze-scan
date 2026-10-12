import { Badge, Box, Circle, HStack, Heading, Stack, Text } from "@chakra-ui/react";
import type { ReactNode } from "react";
import type { IconType } from "react-icons";
import { LuArrowLeftRight, LuChartCandlestick, LuCoins, LuFlame, LuGift, LuLandmark, LuSend, LuShapes, LuSparkles } from "react-icons/lu";

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

/** One numbered action of a transaction: its icon, category, plain-word title and body. */
export function ActionCard({ index, category, title, notExecuted, children }: ActionCardProps) {
  const { icon: Icon, label, palette } = CATEGORIES[category];
  return (
    <Box
      as="article"
      bg="bg.panel"
      borderWidth="1px"
      borderColor="border.panel"
      rounded="l3"
      p={{ base: "4", md: "5" }}
      data-testid="action-card"
      data-category={category}
    >
      <HStack gap="3" align="start">
        <Circle size="9" bg={`${palette}.subtle`} color={`${palette}.fg`} flexShrink={0} aria-hidden>
          <Icon />
        </Circle>
        <Stack gap="1" minW="0" flex="1">
          <HStack gap="2" wrap="wrap">
            <Text fontSize="xs" color="fg.muted" fontWeight="medium">
              {index}. {label}
            </Text>
            {notExecuted && (
              <Badge colorPalette="red" variant="outline" size="sm">
                not executed
              </Badge>
            )}
          </HStack>
          <Heading as="h3" size="md" wordBreak="break-word">
            {title}
          </Heading>
        </Stack>
      </HStack>
      {children && (
        <Box mt="4" ps={{ base: "0", md: "12" }}>
          {children}
        </Box>
      )}
    </Box>
  );
}
