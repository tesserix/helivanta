import { z } from "zod";
import { dutyExchangeInputContract, type DutyExchangeInput } from "./duty-contract";

export * from "./duty-contract";

// Each choice is picked from loaded lists; consent must be attested before filing.
export const dutyExchangeFormSchema = z
  .object({
    kind: dutyExchangeInputContract.shape.kind,
    roster_id: z.string().min(1, "Choose a published roster."),
    requester_staff_id: z.string().min(1, "Choose who gives up the duty."),
    requester_allocation_id: z.string().min(1, "Choose the duty to hand over."),
    counterpart_staff_id: z.string().min(1, "Choose who takes the duty."),
    counterpart_allocation_id: z.string(),
    counterpart_consent: z.boolean(),
    reason: z
      .string()
      .trim()
      .min(1, "Add a reason for the exchange.")
      .max(500, "Keep the reason within 500 characters.")
      .refine((value) => !value.includes("\0"), "Remove invalid characters."),
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: "custom", path: [path], message });
    if (value.counterpart_staff_id && value.counterpart_staff_id === value.requester_staff_id)
      issue("counterpart_staff_id", "Choose someone other than the person giving up the duty.");
    if (value.kind === "swap" && !value.counterpart_allocation_id)
      issue("counterpart_allocation_id", "Choose the duty given in return.");
    if (!value.counterpart_consent)
      issue("counterpart_consent", "Confirm the other person has agreed to the exchange.");
  });
export type DutyExchangeForm = z.infer<typeof dutyExchangeFormSchema>;

export const toDutyExchangeInput = (form: DutyExchangeForm): DutyExchangeInput => ({
  kind: form.kind,
  requester_allocation_id: form.requester_allocation_id,
  counterpart_staff_id: form.counterpart_staff_id,
  ...(form.kind === "swap" && { counterpart_allocation_id: form.counterpart_allocation_id }),
  counterpart_consent: form.counterpart_consent,
  reason: form.reason,
});

// Builds the list path; an empty filter value is not sent.
export const dutyExchangesPath = (
  base: string,
  f: { status?: string; staff_id?: string; from?: string; to?: string },
) => {
  const query = new URLSearchParams();
  if (f.status && f.status !== "all") query.set("status", f.status);
  if (f.staff_id) query.set("staff_id", f.staff_id);
  if (f.from) query.set("from", f.from);
  if (f.to) query.set("to", f.to);
  const q = query.toString();
  return `${base}/hr/duty-exchanges${q ? `?${q}` : ""}`;
};
