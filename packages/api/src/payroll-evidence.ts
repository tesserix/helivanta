import { evidenceClient } from "./workforce-evidence";
import { taxEvidenceInputContract } from "./payroll-contract";

export const taxEvidence = evidenceClient({
  key: "tax-declaration-evidence",
  recordKind: "tax_declaration",
  category: "tax_declaration_evidence",
  input: taxEvidenceInputContract,
  recordParam: "revision_id",
});
