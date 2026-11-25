// The one class for a text-like control that is not a @tesserix/web component
// (#1261 D1): a control radius, so the surface rule does not read it as a
// card; a 44px target; and the design system's --ring focus, never the tenant
// accent, which is not guaranteed to show.
export const controlClass =
  "w-full min-h-11 rounded-md border border-input bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background";

// Table parts (#1261 D2): a header that stays in view, 44px rows, and numbers
// that line up by place value.
export const tableStyles = {
  head: "sticky top-0 z-10 bg-background text-left text-xs font-medium text-muted-foreground",
  row: "h-11 border-b border-border hover:bg-muted/50",
  number: "text-right tabular-nums",
} as const;
