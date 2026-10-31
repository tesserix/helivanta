import { z } from "zod";
import { apiFetch } from "./client";
import { withStaffIntakeScope } from "./enrollment";
import { documentPath, documentHeaders, type DocumentScope } from "./documents";
import {
  documentClassificationSchema,
  documentClassificationInputSchema,
  documentClassificationRevisionSchema,
  type DocumentClassification,
  type DocumentClassificationInput,
  type DocumentClassificationRevision,
} from "./documents-fields";
import type { Page } from "./paged";

export function documentClassificationPath(scope: DocumentScope, file: string): string {
  return `${documentPath(scope)}/documents/${documentClassificationSchema.shape.file_id.parse(file)}/classification`;
}

function verifyOwner(
  scope: DocumentScope,
  file: string,
  value: { hospital_id: string; staff_id: string; file_id: string },
): void {
  if (
    value.hospital_id !== scope.hospitalId ||
    value.staff_id !== scope.staffId ||
    value.file_id !== file
  )
    throw new Error("Document classification ownership did not match this workspace.");
}

export async function readDocumentClassification(
  scope: DocumentScope,
  file: string,
  signal: AbortSignal,
): Promise<DocumentClassification> {
  const value = documentClassificationSchema.parse(
    await apiFetch(withStaffIntakeScope(documentClassificationPath(scope, file), scope), {
      headers: documentHeaders(scope),
      cache: "no-store",
      signal,
    }),
  );
  verifyOwner(scope, file, value);
  return value;
}

export async function saveDocumentClassification(
  scope: DocumentScope,
  file: string,
  command: DocumentClassificationInput,
  signal: AbortSignal,
): Promise<DocumentClassification> {
  const input = documentClassificationInputSchema.parse(command);
  input.classification_revision_id = input.classification_revision_id.toLowerCase();
  const value = documentClassificationSchema.parse(
    await apiFetch(withStaffIntakeScope(documentClassificationPath(scope, file), scope), {
      method: "PUT",
      headers: documentHeaders(scope),
      body: JSON.stringify(input),
      cache: "no-store",
      signal,
    }),
  );
  verifyOwner(scope, file, value);
  if (
    value.classification_revision_id !== input.classification_revision_id ||
    (value.version !== input.expected_version && value.version !== input.expected_version + 1)
  )
    throw new Error("Document classification receipt did not match the saved command.");
  return value;
}

const historyPage = z.object({
  data: z.array(documentClassificationRevisionSchema).max(20),
  page: z.object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(2048).nullish() }),
});

export async function readDocumentClassificationHistory(
  scope: DocumentScope,
  file: string,
  cursor: string | null,
  signal: AbortSignal,
): Promise<Page<DocumentClassificationRevision>> {
  const query = new URLSearchParams({ limit: "20" });
  if (cursor) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const value = historyPage.parse(
    await apiFetch(
      withStaffIntakeScope(`${documentClassificationPath(scope, file)}/history?${query}`, scope),
      {
        headers: documentHeaders(scope),
        cache: "no-store",
        signal,
      },
    ),
  );
  if (value.page.has_more !== Boolean(value.page.next_cursor))
    throw new Error("Document classification history could not be continued safely.");
  for (const row of value.data) verifyOwner(scope, file, row);
  return {
    data: value.data,
    page: { has_more: value.page.has_more, next_cursor: value.page.next_cursor ?? null },
  };
}
