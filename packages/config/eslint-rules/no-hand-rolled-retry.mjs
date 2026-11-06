// #1595: a button that re-runs a read by hand bypasses RequestFailure's
// error prop, so a refusal reads as an outage and its retry can only be
// refused again. onRetry/onRefresh/onReload hand the call to a button drawn
// elsewhere; shared components name that hand-off retry or refresh instead.
const HANDLERS = /^on(Click|Retry|Refresh|Reload)$/;
const READS = new Set(["refetch", "retry"]);

function callsRead(node) {
  if (!node || typeof node.type !== "string") return false;
  if (
    node.type === "MemberExpression" &&
    !node.computed &&
    node.property.type === "Identifier" &&
    READS.has(node.property.name)
  )
    return true;
  return Object.entries(node).some(
    ([key, value]) =>
      key !== "parent" &&
      (Array.isArray(value)
        ? value.some(callsRead)
        : value && typeof value === "object" && callsRead(value)),
  );
}

export const noHandRolledRetry = {
  meta: {
    type: "problem",
    messages: {
      retry:
        "A failed read renders RequestFailure or QueryState; a refresh of loaded data is RefreshButton (#1595).",
    },
    schema: [],
  },
  create(context) {
    return {
      JSXAttribute(node) {
        if (
          node.name.type === "JSXIdentifier" &&
          HANDLERS.test(node.name.name) &&
          callsRead(node.value)
        )
          context.report({ node, messageId: "retry" });
      },
    };
  },
};
