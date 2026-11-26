// #1855: native constraint validation shows the browser's own bubble, outside
// the zod-driven inline error (frontend.md §4). These are no-restricted-syntax
// selectors in a rule of their own, because no-restricted-syntax options do not
// merge across config objects: a file that also gets the design rules would
// lose one set or the other.
const FIX = "Validate with useZodForm + Field (frontend.md §4)";
export const NATIVE_VALIDATION_SYNTAX = [
  {
    selector: "JSXAttribute[name.name=/^(required|minLength|maxLength|pattern)$/]",
    message: `${FIX}, not a native required/minLength/maxLength/pattern attribute.`,
  },
  {
    selector: "JSXAttribute[name.name='type'][value.value=/^(email|url)$/]",
    message: `${FIX}; an email or URL input is type="text" with inputMode, not type="email"/"url".`,
  },
  {
    selector: "JSXAttribute[name.name='type'][value.expression.value=/^(email|url)$/]",
    message: `${FIX}; an email or URL input is type="text" with inputMode, not type="email"/"url".`,
  },
  {
    selector: "JSXOpeningElement[name.name='form']:not(:has(JSXAttribute[name.name='noValidate']))",
    message: `${FIX}; every <form> sets noValidate.`,
  },
];

export const noNativeValidation = {
  meta: { type: "problem", schema: [] },
  create(context) {
    return Object.fromEntries(
      NATIVE_VALIDATION_SYNTAX.map(({ selector, message }) => [
        selector,
        (node) => context.report({ node, message }),
      ]),
    );
  },
};
