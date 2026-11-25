// Files that still paint their own card surfaces (#1275 spec D4). They get
// every workspace lint rule except the surface one. surface-legacy.test.mjs
// fails when a listed file no longer paints one, so this list only shrinks:
// each zone tidy-up removes its files, and a new file is never added.
export const SURFACE_LEGACY = [
  // Emergency #1262
  "components/emergency/emergency-base-admin.tsx",
  "components/emergency/emergency-case-detail.tsx",
  "components/emergency/emergency-crew.tsx",
  "components/emergency/emergency-dispatch.tsx",
  "components/emergency/emergency-fleet.tsx",
  "components/emergency/emergency-intake.tsx",
  "components/emergency/emergency-receiving-dispatch.tsx",
  "components/emergency/emergency-receiving.tsx",
  "components/emergency/emergency-review.tsx",
  "components/emergency/emergency-workspace.tsx",
  // Pharmacy #1258
  // Supply #1260
  "components/supply/licence-evidence.tsx",
  "components/supply/licence-renewal-notice.tsx",
  "components/supply/licence-review.tsx",
  "components/supply/organisation-access-limits.tsx",
  "components/supply/organisation-access.tsx",
  "components/supply/organisation-operations.tsx",
  "components/supply/organisation-structure.tsx",
  "components/supply/organisation-support.tsx",
  "components/supply/parties-workspace.tsx",
  "components/supply/party-editor.tsx",
  "components/supply/party-registration.tsx",
  "components/supply/platform-identity.tsx",
  "components/supply/warehouse-bins.tsx",
  // Workforce #1261
  "app/(workspace)/workforce/benefits/benefits-workspace.tsx",
  "app/(workspace)/workforce/benefits/coverage-export.tsx",
  "app/(workspace)/workforce/benefits/plan-editor.tsx",
  "app/(workspace)/workforce/benefits/plans-panel.tsx",
  "app/(workspace)/workforce/benefits/staff-benefits-panel.tsx",
];
