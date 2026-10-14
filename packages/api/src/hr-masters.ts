import { z } from "zod";

export const HR_MASTER_LABELS = {
  department: "Departments",
  designation: "Designations",
  grade: "Grades",
  qualification: "Qualifications",
  document_category: "Document categories",
  exit_reason: "Exit reasons",
  relationship: "Relationships",
  language: "Languages",
  contract_type: "Contract types",
} as const;
export type HRMasterKind = keyof typeof HR_MASTER_LABELS;

export const hrMasterSchema = z
  .object({
    expected_version: z.number().int().min(0),
    kind: z.enum([
      "department",
      "designation",
      "grade",
      "qualification",
      "document_category",
      "exit_reason",
      "relationship",
      "language",
      "contract_type",
    ]),
    code: z
      .string()
      .trim()
      .toUpperCase()
      .regex(/^[A-Z0-9_-]{1,40}$/, "Use up to 40 letters, numbers, underscores or hyphens."),
    name: z.string().trim().min(1, "Enter a name.").max(80),
    description: z.string().trim().max(500),
    active: z.boolean(),
  })
  .strict();
export type HRMasterInput = z.infer<typeof hrMasterSchema>;
export type HRMaster = Omit<HRMasterInput, "expected_version"> & {
  id: string;
  revision_id: string;
  version: number;
  created_at: string;
  updated_at: string;
};
export type HRMasterRevision = Omit<HRMaster, "revision_id" | "updated_at"> & { master_id: string };
