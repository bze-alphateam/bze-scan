import { obj, text, type Entry, type Reader } from "./actions";

/** `/bze.tradebin.v1.MsgUpdateParams` → `tradebin`, `/cosmos.staking.v1beta1.MsgUpdateParams` → `staking`. */
export function moduleOf(typeUrl: string): string {
  const parts = typeUrl.replace(/^\//, "").split(".");
  const name = parts.length >= 3 && /^v\d/.test(parts[parts.length - 2]) ? parts[parts.length - 3] : parts[parts.length - 2];
  return name ?? typeUrl;
}

/**
 * `parameter_change`: a legacy `ParameterChangeProposal` lists the changed
 * keys (of the first change's subspace; a key of another subspace is shown
 * as `subspace/key`); a module's `MsgUpdateParams` sets all of its
 * parameters at once.
 */
export const readParams: Reader = (type, body) => {
  if (type === "/cosmos.params.v1beta1.ParameterChangeProposal") {
    const changes = Array.isArray(body.changes) ? body.changes.map(obj).filter((c) => c !== null) : [];
    const subspaceOf = changes.length > 0 ? (text(changes[0], "subspace") ?? "?") : "?";
    const entries: Entry[] = changes.map((c) => {
      const subspace = text(c, "subspace") ?? "?";
      const key = text(c, "key") ?? "?";
      return { key: subspace === subspaceOf ? key : `${subspace}/${key}`, value: c.value };
    });
    return { type: "params", module: subspaceOf, entries, whole: false };
  }
  if (type.endsWith(".MsgUpdateParams")) {
    const params = obj(body.params) ?? {};
    return {
      type: "params",
      module: moduleOf(type),
      entries: Object.entries(params).map(([key, value]) => ({ key, value })),
      whole: true,
    };
  }
  return null;
};
