import { z } from "zod";
import { bedInputSchema, type BedClass } from "./medicore-beds";

// Bed-day charges (#197 slice 2): a hospital's timezone, set once, and its
// daily rates per bed class, before tax.

export type BedCharging = {
  hospital_id: string;
  timezone: string;
  created_by: string;
  created_at: string;
};

export type BedRate = {
  id: string;
  hospital_id: string;
  bed_class: BedClass;
  effective_from: string;
  daily_rate_minor: number;
  currency: string;
  created_by: string;
  created_at: string;
};

export const bedChargingSchema = z.object({
  timezone: z
    .string()
    .trim()
    .min(1, "Enter the hospital's timezone, such as Asia/Kolkata")
    .max(64, "At most 64 characters"),
});

export const bedRateSchema = z.object({
  bed_class: bedInputSchema.shape.bed_class,
  effective_from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Choose the date the rate starts"),
  daily_rate_minor: z
    .number({ message: "Enter the daily rate" })
    .int("Enter the daily rate")
    .min(1, "A rate is more than zero")
    .max(1_000_000_000, "At most 1,00,00,000 a day"),
  currency: z.string().regex(/^[A-Z]{3}$/, "Choose the currency"),
});
