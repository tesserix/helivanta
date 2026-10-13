import { z } from "zod";

const dateOnly = z
  .string()
  .regex(/^\d{4}-\d{2}-\d{2}$/, "Enter a date of birth.")
  .refine((value) => {
    const date = new Date(value);
    return (
      !Number.isNaN(date.getTime()) &&
      date.toISOString().slice(0, 10) === value &&
      value <= new Date().toISOString().slice(0, 10)
    );
  }, "Enter a valid date that is not in the future.");

export const reportedBloodGroupSchema = z
  .object({
    abo: z.enum(["A", "B", "AB", "O", "unknown"]),
    rh: z.enum(["positive", "negative", "unknown"]),
    source: z
      .enum(["", "patient", "guardian", "document"])
      .refine((value): boolean => value !== "", "Select who or what reported this blood group."),
  })
  .strict();

export const patientRegistrationSchema = z
  .object({
    reported_blood_group: reportedBloodGroupSchema.optional(),
    given_name: z.string().trim().min(1, "Enter a given name.").max(100),
    family_name: z.string().trim().max(100),
    dob: dateOnly,
    dob_estimated: z.boolean(),
    sex: z.enum(["", "male", "female", "other"]),
    mobile: z
      .string()
      .regex(
        /^(\+[1-9][0-9]{6,14}|[0-9]{10})?$/,
        "Use an international number, such as +61491570156.",
      ),
    address_line: z.string().max(300),
    notice_version: z.string().trim().min(1, "Enter the consent notice version.").max(40),
    consented_by: z.enum(["patient", "guardian"]),
    guardian_name: z.string().trim().max(100),
    guardian_relationship: z.string().trim().max(40),
    consent_confirmed: z.boolean().refine((value) => value, "Record consent before registration."),
  })
  .superRefine((value, ctx) => {
    const adult = new Date(value.dob);
    adult.setUTCFullYear(adult.getUTCFullYear() + 18);
    if (
      !Number.isNaN(adult.getTime()) &&
      adult.toISOString() > new Date().toISOString() &&
      value.consented_by !== "guardian"
    )
      ctx.addIssue({
        code: "custom",
        path: ["consented_by"],
        message: "Record guardian consent for a patient under 18.",
      });
    if (value.consented_by === "guardian") {
      if (!value.guardian_name)
        ctx.addIssue({
          code: "custom",
          path: ["guardian_name"],
          message: "Enter the guardian's name.",
        });
      if (!value.guardian_relationship)
        ctx.addIssue({
          code: "custom",
          path: ["guardian_relationship"],
          message: "Enter the guardian's relationship.",
        });
    }
  });
export type PatientRegistrationInput = z.infer<typeof patientRegistrationSchema>;
export type Patient = Pick<
  PatientRegistrationInput,
  "given_name" | "family_name" | "dob" | "dob_estimated" | "sex" | "mobile" | "address_line"
> & { id: string; hospital_id: string; mrn: string; created_at: string };
export type PatientDetail = Patient & {
  reported_blood_group?: z.infer<typeof reportedBloodGroupSchema> & { recorded_at: string };
};
export type PatientMatch = Pick<Patient, "given_name" | "family_name" | "mrn"> & {
  patient_id: string;
  score: number;
};
export type PatientRegistrationResult = { patient: Patient; possible_matches: PatientMatch[] };
