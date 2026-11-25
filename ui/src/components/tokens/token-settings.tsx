"use client";

import { Box, Popover, Portal, Stack, Text } from "@chakra-ui/react";
import { LuCircleHelp } from "react-icons/lu";

import type { Token } from "@/api/types";
import { AccountChip } from "@/components/ui/account-chip";
import { ListPanel } from "@/components/ui/list";
import { PanelHead } from "@/components/ui/page-head";
import { destinationText, ratioPct, settingLines } from "@/render/token-settings";
import { tokenSymbol } from "@/render/tokens";

/** The "?" pop-up: the raw values as the chain stores them. */
function RawValues({ t }: { t: Token }) {
  const c = t.config;
  if (!c) return null;
  return (
    <Popover.Root positioning={{ placement: "bottom-end" }} lazyMount unmountOnExit>
      <Popover.Trigger asChild>
        <Box asChild cursor="help" color="fg.muted">
          <button type="button" aria-label="Raw settings values" data-testid="settings-raw-trigger">
            <LuCircleHelp size={16} />
          </button>
        </Box>
      </Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content maxW="sm">
            <Popover.Arrow />
            <Popover.Body data-testid="settings-raw">
              <Stack gap="1" fontSize="12px">
                <Text>
                  community_pool: <code>{c.community_pool ?? "none"}</code>
                </Text>
                <Text>
                  transfer_fee: <code>{c.transfer_fee}</code> ({ratioPct(c.transfer_fee)})
                </Text>
                <Text>
                  gas_surcharge: <code>{c.gas_surcharge}</code> ({ratioPct(c.gas_surcharge)})
                </Text>
              </Stack>
            </Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  );
}

/** What a factory token charges for paying fees in it. Nothing for other tokens and for an unknown config. */
export function TokenSettingsCard({ t }: { t: Token }) {
  if (t.kind !== "factory" || !t.config) return null;
  const symbol = tokenSymbol(t);
  const lines = settingLines(symbol, t.config);
  return (
    <Box mt="8" data-testid="token-settings">
      <ListPanel>
        <PanelHead id="token-settings-title" title="Token settings" action={<RawValues t={t} />} />
        <Stack gap="2" p="4" fontSize="13px" lineHeight="1.55">
          {lines.length === 0 && <Text data-testid="settings-none">No special fees</Text>}
          {lines.map((l) => (
            <Text key={l.kind} data-testid={`settings-${l.kind}`}>
              {l.text} {destinationText(l)}
              {l.toPool && t.config?.community_pool && (
                <>
                  {" "}
                  <AccountChip address={t.config.community_pool} label={t.config.community_pool_label} verticalAlign="middle" />
                </>
              )}
              .
            </Text>
          ))}
        </Stack>
      </ListPanel>
    </Box>
  );
}
