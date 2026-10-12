import { height, obj, text, type Reader } from "./actions";

/** `software_upgrade`: a planned upgrade, or the cancellation of one. */
export const readUpgrade: Reader = (type, body) => {
  switch (type) {
    case "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade":
    case "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal": {
      const plan = obj(body.plan) ?? {};
      return { type: "upgrade", name: text(plan, "name") ?? "a new version", height: height(plan.height) };
    }
    case "/cosmos.upgrade.v1beta1.MsgCancelUpgrade":
    case "/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal":
      return { type: "cancel_upgrade" };
    default:
      return null;
  }
};
