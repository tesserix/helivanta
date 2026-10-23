// The Dispense queue's Waiting tab is oldest first and paged (#1295), so a
// spec's freshly created patient can sit pages deep in a shared dev
// database. Specs open Waiting searched to their own patient (D9), which is
// also how a pharmacist finds the patient at the counter.
export const waitingFor = (patient: string) =>
  `/pharmacy/dispense?tab=waiting&q=${encodeURIComponent(patient)}`;

// Dispensed is newest first by hand-over time, so a row a spec has just
// dispensed is on its first page.
export const DISPENSED = "/pharmacy/dispense?tab=dispensed";
