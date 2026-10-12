import { text, type Reader } from "./actions";

/** `ibc_client_update`: an expired connection to another chain repaired with a substitute client. */
export const readIbc: Reader = (type, body) =>
  type === "/ibc.core.client.v1.MsgRecoverClient" || type === "/ibc.core.client.v1.ClientUpdateProposal"
    ? { type: "ibc_client", subject: text(body, "subject_client_id"), substitute: text(body, "substitute_client_id") }
    : null;
