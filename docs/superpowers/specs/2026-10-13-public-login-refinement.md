# Public entry and login refinement

Issue: #1087. Extends ADR 0011; the public landing remains `/`, authenticated
workspaces remain `/dashboard`, and `/login` preserves the existing OIDC, MFA,
password-change and error recovery contracts.

Use a calm light authentication surface with a contained tenant-accent panel,
clear welcome heading, one primary action and an explicit return to the public
homepage. Tenant logos, approved accent palettes, welcome copy and centered or
split layout remain configurable. The clinical workspace retains its own theme;
system dark mode must not turn the public entry into a mixed dark/light layout.
The Tesserix attribution remains visible in both layouts and mobile widths.

The illustration conveys team structure without patient records, invented
metrics or customer endorsements. Public partner/certification placeholders stay
honest. Self-service provisioning and canonical tenant aliases remain outside
this presentation change, as documented in ADR 0011.

Rejected: merely recolouring the full-bleed split screen retains the excessive
empty space and repeated brand headings. Replacing credential collection would
unnecessarily change the existing authentication boundary.

Validation: tenant branding and home-navigation regression coverage, existing
login/MFA suite, workspace quality gates, desktop/mobile browser inspection,
and production reconciliation plus public-route smoke checks after release.
