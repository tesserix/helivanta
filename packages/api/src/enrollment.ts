import { z } from "zod";
import { staffSchema, type Staff } from "./workspace";
import {
  enrollmentProfileSchema,
  enrollmentAddressSchema,
  enrollmentContactSchema,
  enrollmentCredentialSchema,
  enrollmentQualificationSchema,
  enrollmentExperienceSchema,
  EMPTY_PROFILE,
  type EnrollmentProfileMastersProjection,
  type EnrollmentQualificationMastersProjection,
} from "./enrollment-fields";
export * from "./enrollment-fields";

export const enrollmentSchema = z
  .object({
    expected_version: z.number().int().min(0),
    staff: staffSchema,
    profile: enrollmentProfileSchema,
    addresses: z.array(enrollmentAddressSchema).max(2),
    emergency_contacts: z.array(enrollmentContactSchema).max(5),
    credentials: z.array(enrollmentCredentialSchema).max(20),
    qualifications: z.array(enrollmentQualificationSchema).max(20),
    experiences: z.array(enrollmentExperienceSchema).max(20),
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: (string | number)[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    if (value.profile.date_of_birth > new Date().toISOString().slice(0, 10))
      issue(["profile", "date_of_birth"], "Date of birth cannot be in the future.");
    for (const end of ["end_date", "probation_end_date", "confirmation_date"] as const) {
      if (
        value.profile[end] &&
        (!value.profile.start_date || value.profile[end] < value.profile.start_date)
      )
        issue(["profile", end], "Choose a date on or after the start date.");
    }
    for (const key of [
      "addresses",
      "emergency_contacts",
      "credentials",
      "qualifications",
      "experiences",
    ] as const) {
      const ids = new Set<string>();
      value[key].forEach((row, i) => {
        if (ids.has(row.id.toLowerCase())) issue([key, i, "id"], "Duplicate record.");
        ids.add(row.id.toLowerCase());
      });
    }
    const addressUses = new Set<string>();
    value.addresses.forEach((row, i) => {
      if (addressUses.has(row.use))
        issue(["addresses", i, "use"], "Keep one address of each type.");
      addressUses.add(row.use);
    });
    const priorities = new Set<number>();
    value.emergency_contacts.forEach((row, i) => {
      if (priorities.has(row.priority))
        issue(["emergency_contacts", i, "priority"], "Choose a different contact priority.");
      priorities.add(row.priority);
    });
    value.credentials.forEach((row, i) => {
      if (row.valid_from && row.valid_until && row.valid_until < row.valid_from)
        issue(["credentials", i, "valid_until"], "Expiry must follow the valid-from date.");
    });
    value.experiences.forEach((row, i) => {
      if (row.end_date && row.end_date < row.start_date)
        issue(["experiences", i, "end_date"], "End date must follow the start date.");
    });
  });

export type EnrollmentInput = z.infer<typeof enrollmentSchema>;
export type StaffEnrollment = Omit<EnrollmentInput, "expected_version" | "staff"> &
  Partial<EnrollmentProfileMastersProjection & EnrollmentQualificationMastersProjection> & {
    staff: Staff;
    version: number;
  };

export const EMPTY_ENROLLMENT: EnrollmentInput = {
  expected_version: 0,
  staff: {
    employee_code: "",
    display_name: "",
    job_category: "other",
    employment_status: "active",
  },
  profile: { ...EMPTY_PROFILE },
  addresses: [],
  emergency_contacts: [],
  credentials: [],
  qualifications: [],
  experiences: [],
};
