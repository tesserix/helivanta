# Issue #1688 implementation

1. Reproduce the missing barcode route with an HTTP integration assertion.
2. Share GS1 validation with drug master; add audited immutable outlet links and canonical link input schema. Verify scope, concurrency, replay and invalid/inactive cases.
3. Add a reusable focused keyboard-scanner field with exact lookup, paged unknown-product linking, cancellation and actionable recovery.
4. Connect counter scans to basket/availability/prescription gates and receiving scans to the guarded product chooser. Add behavioral regression assertions before implementation.
5. Regenerate contracts/inventory, update the domain specification and run affected tests, required local checks and builds. Review all acceptance criteria and report unverified browser/hardware behavior explicitly.
