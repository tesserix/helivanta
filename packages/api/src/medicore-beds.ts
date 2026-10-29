import { z } from "zod";

// Beds, admissions and who occupies a bed (#106 slice 1).
export { v7 as newBedId } from "uuid";

export type BedClass =
  "general" | "semi_private" | "private" | "icu" | "hdu" | "isolation" | "neonatal" | "day_care";
export type BedState = "available" | "occupied" | "cleaning" | "out_of_service";

export const bedClassLabel: Record<BedClass, string> = {
  general: "General",
  semi_private: "Semi-private",
  private: "Private",
  icu: "ICU",
  hdu: "HDU",
  isolation: "Isolation",
  neonatal: "Neonatal",
  day_care: "Day care",
};

export type Ward = {
  id: string;
  hospital_id: string;
  code: string;
  name: string;
  created_at: string;
};
export type Room = {
  id: string;
  hospital_id: string;
  ward_id: string;
  code: string;
  created_at: string;
};

/** A bed on the board, with its current stay if occupied. */
export type Bed = {
  id: string;
  hospital_id: string;
  ward_id: string;
  ward_code: string;
  room_id: string;
  room_code: string;
  code: string;
  bed_class: BedClass;
  state: BedState;
  version: number;
  created_at: string;
  occupancy_id: string | null;
  admission_id: string | null;
  patient_id: string | null;
  occupied_since: string | null;
};

export type ReleaseReason =
  "discharged" | "transferred_out" | "died" | "left_against_advice" | "other";

export const releaseReasonLabel: Record<ReleaseReason, string> = {
  discharged: "Discharged",
  transferred_out: "Transferred to another hospital",
  died: "Died",
  left_against_advice: "Left against medical advice",
  other: "Other",
};

export type Admission = {
  id: string;
  hospital_id: string;
  patient_id: string;
  intent: "elective" | "emergency";
  status: "admitted" | "released";
  admitted_by: string;
  admitted_at: string;
  release_reason: ReleaseReason | null;
  release_note: string | null;
  released_by: string | null;
  released_at: string | null;
  occupancies: {
    id: string;
    bed_id: string;
    started_at: string;
    started_by: string;
    ended_at: string | null;
    ended_by: string | null;
    end: "transfer" | "release" | null;
  }[];
};

const code = z
  .string()
  .trim()
  .toUpperCase()
  .regex(/^[A-Z0-9][A-Z0-9-]{0,19}$/, "Up to 20 letters, digits and dashes");

export const wardInputSchema = z.object({
  code,
  name: z.string().trim().min(1, "Enter the ward name").max(100, "At most 100 characters"),
});
export const roomInputSchema = z.object({ ward_id: z.string().min(1, "Choose the ward"), code });
export const bedInputSchema = z.object({
  room_id: z.string().min(1, "Choose the room"),
  code,
  bed_class: z.enum([
    "general",
    "semi_private",
    "private",
    "icu",
    "hdu",
    "isolation",
    "neonatal",
    "day_care",
  ]),
});

export const admitSchema = z.object({
  patient_id: z.string().min(1, "Choose a patient"),
  bed_id: z.string().min(1, "Choose an available bed"),
  intent: z.enum(["elective", "emergency"]),
});

export const releaseSchema = z
  .object({
    reason: z.enum(["discharged", "transferred_out", "died", "left_against_advice", "other"]),
    note: z.string().trim().max(500, "At most 500 characters"),
  })
  .superRefine((v, ctx) => {
    if (v.reason === "other" && v.note === "")
      ctx.addIssue({ code: "custom", path: ["note"], message: "Say what happened" });
  });

export const bedReasonSchema = z.object({
  reason: z.string().trim().min(1, "Give a reason").max(500, "At most 500 characters"),
});
