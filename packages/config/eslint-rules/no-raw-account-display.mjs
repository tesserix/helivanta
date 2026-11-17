const IDENTITY =
  /^(subject|.*_subject|actorSubject|ownerSubject|accountSubject|userId|accountId|user_id|issued_by|created_by|reviewed_by|decided_by|proposed_by|approved_by|updated_by|revoked_by|submitted_by|actor_id|declared_by|verified_by|countersigned_by|cancelled_by|reversed_by|received_by|dispatched_by|closed_by|finalized_by|finalised_by)$/;
const TEXT_ATTRIBUTES = new Set([
  "title",
  "label",
  "description",
  "placeholder",
  "aria-label",
  "aria-description",
]);

function rendersIdentity(node) {
  if (!node) return false;
  if (node.type === "Identifier") return IDENTITY.test(node.name);
  if (node.type === "MemberExpression")
    return (
      !node.computed && node.property.type === "Identifier" && IDENTITY.test(node.property.name)
    );
  if (
    node.type === "ChainExpression" ||
    node.type === "TSNonNullExpression" ||
    node.type === "TSAsExpression"
  )
    return rendersIdentity(node.expression);
  if (node.type === "ConditionalExpression")
    return rendersIdentity(node.consequent) || rendersIdentity(node.alternate);
  if (node.type === "LogicalExpression")
    return (node.operator !== "&&" && rendersIdentity(node.left)) || rendersIdentity(node.right);
  if (node.type === "BinaryExpression" && node.operator === "+")
    return rendersIdentity(node.left) || rendersIdentity(node.right);
  if (node.type === "TemplateLiteral") return node.expressions.some(rendersIdentity);
  return false;
}

export const noRawAccountDisplay = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      identity:
        "Render authorized account names with AccountName or a named account selector; backend account identifiers are not UI labels.",
    },
  },
  create(context) {
    return {
      JSXExpressionContainer(node) {
        const visible =
          ["JSXElement", "JSXFragment"].includes(node.parent.type) ||
          (node.parent.type === "JSXAttribute" &&
            (TEXT_ATTRIBUTES.has(node.parent.name.name) ||
              (node.parent.name.name === "value" &&
                node.parent.parent.name.type === "JSXIdentifier" &&
                node.parent.parent.name.name === "Fact")));
        if (visible && rendersIdentity(node.expression))
          context.report({ node, messageId: "identity" });
      },
    };
  },
};
