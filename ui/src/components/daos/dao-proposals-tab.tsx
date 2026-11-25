"use client";

import { Text } from "@chakra-ui/react";

import type { Dao } from "@/api/types";

/**
 * The Proposals tab's body: the slot the DAO proposals story fills with the
 * DAO's proposals and votes. Until then the tab says so.
 */
export function DaoProposalsTab({ dao }: { dao: Dao }) {
  return (
    <Text fontSize="13px" color="fg.muted" data-testid="dao-proposals-slot" data-dao={dao.id}>
      The proposals of this DAO will be listed here.
    </Text>
  );
}
