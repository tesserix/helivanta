# Public platform catalogue

The homepage catalogue preserves the “Different responsibilities. A familiar
place to work.” heading and groups the documented HMS scope into four native,
keyboard-accessible expandable sections. Patient care opens by default; all
categories remain in server-rendered HTML without client-side filtering.

Scope sources are [product ownership](../architecture/01-system-design.md),
[the legacy/backlog mapping](../architecture/06-evidence-and-sources.md), and the
current shell routes and backend modules. Product boundaries are unchanged.

`apps/shell/app/platform-catalog.ts` owns the public copy and availability labels.
“Available” describes the listed staff/hospital slice; “Foundation available”
explicitly separates implemented workflows from their planned extensions.
“Planned” and “Future” do not represent working application modules. Updating
these labels requires implementation evidence, not just a route or menu entry.

Coverage includes patient identity, appointments, OPD/IPD, clinical specialties,
clinician experience, pharmacy, laboratory, imaging, procurement, inventory,
workforce/payroll, billing/insurance, finance, assets/facilities, emergency care,
home care, safety, CRM, patient portal, administration/reporting, tenant branding,
documents/integrations and future AI. Tenant/hospital sharing remains governed
by the architecture's separate permissions; common ownership is not consent.
