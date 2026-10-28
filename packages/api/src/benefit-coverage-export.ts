import { z } from "zod";
import { apiFetchResponse } from "./client";
import { readAttachmentFile, sha256Hex } from "./attachment-file";
import { benefitCoveragePath, benefitPlanContract } from "./benefits";

export const benefitCoverageExportFormSchema = z.object({
  as_of: z
    .string()
    .min(1, "Choose the date the file describes.")
    .date("Use a valid date.")
    .refine(
      (value) => value >= "2000-01-01" && value <= "2200-12-31",
      "Choose a date between 2000-01-01 and 2200-12-31.",
    ),
  plan_id: z.literal("").or(benefitPlanContract.shape.id),
});
export type BenefitCoverageExportForm = z.infer<typeof benefitCoverageExportFormSchema>;

// The row count is reported only for bytes whose digest matches the one the export was audited with.
export async function fetchBenefitCoverageCSV(
  hospitalId: string,
  filters: BenefitCoverageExportForm,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string; rows: number }> {
  signal.throwIfAborted();
  const { as_of, plan_id } = benefitCoverageExportFormSchema.parse(filters);
  const query = new URLSearchParams({ as_of });
  if (plan_id) query.set("plan_id", plan_id);
  const response = await apiFetchResponse(`${benefitCoveragePath(hospitalId)}?${query}`, {
    cache: "no-store",
    signal,
  });
  const filename = `benefits-coverage-${as_of}.csv`;
  const rows = response.headers.get("X-Export-Row-Count") ?? "";
  const digest = response.headers.get("X-Export-Sha256") ?? "";
  if (
    response.headers.get("Content-Disposition") !== `attachment; filename="${filename}"` ||
    !/^text\/csv(?:;|$)/i.test(response.headers.get("Content-Type") ?? "") ||
    !/^(?:0|[1-9]\d{0,2}|1000)$/.test(rows) ||
    !/^[0-9a-f]{64}$/.test(digest)
  )
    throw new Error("The server did not return a valid coverage file. Try again.");
  const blob = await response.blob();
  const actual = await sha256Hex(await readAttachmentFile(blob, signal));
  signal.throwIfAborted();
  if (actual !== digest) throw new Error("The coverage file could not be verified. Try again.");
  return { blob, filename, rows: Number(rows) };
}
