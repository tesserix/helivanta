import { z } from "zod";

export const staffSchema = z.object({
  employee_code: z
    .string()
    .regex(/^[A-Za-z0-9_-]{1,40}$/, "Use up to 40 letters, numbers, dashes or underscores."),
  display_name: z.string().trim().min(1, "Enter a full name.").max(150),
  job_category: z.enum([
    "doctor",
    "nurse",
    "pharmacist",
    "lab_technician",
    "receptionist",
    "administration",
    "other",
  ]),
  employment_status: z.enum(["active", "inactive"]),
});
export type StaffInput = z.infer<typeof staffSchema>;
export type Staff = StaffInput & {
  id: string;
  hospital_id: string;
  created_at: string;
  updated_at: string;
};
export type Hospital = { id: string; name: string; access_level: "reader" | "manager" };
export const JOB_LABELS: Record<StaffInput["job_category"], string> = {
  doctor: "Doctor",
  nurse: "Nurse",
  pharmacist: "Pharmacist",
  lab_technician: "Laboratory technician",
  receptionist: "Receptionist",
  administration: "Administration",
  other: "Other",
};
export const brandingSchema = z.object({
  display_name: z.string().trim().min(1, "Enter an organization name.").max(100),
  welcome_text: z.string().max(240),
  logo_url: z
    .string()
    .max(1000)
    .refine((value) => {
      if (!value) return true;
      try {
        const u = new URL(value);
        return (
          u.protocol === "https:" &&
          !u.username &&
          !u.password &&
          !u.port &&
          !u.hash &&
          !u.search &&
          u.hostname.includes(".") &&
          !/^[\d.]+$/.test(u.hostname) &&
          !/\.(local|internal|localhost)$/.test(u.hostname)
        );
      } catch {
        return false;
      }
    }, "Use a public HTTPS image URL without credentials."),
  theme: z.enum(["teal", "blue", "indigo"]),
  login_layout: z.enum(["centered", "split"]),
});
export type Branding = z.infer<typeof brandingSchema>;
export const DEFAULT_BRANDING: Branding = {
  display_name: "Helivanta",
  welcome_text: "A connected workspace for exceptional care.",
  logo_url: "",
  theme: "teal",
  login_layout: "split",
};
