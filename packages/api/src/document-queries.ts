"use client";
import { useQuery } from "@tanstack/react-query";
import { staffIntakeScopeKey, withStaffIntakeScope } from "./enrollment";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import {
  documentPath,
  documentHeaders,
  ownedDocument,
  type DocumentOf,
  type DocumentScope,
  type HospitalDocumentScope,
} from "./documents";
import { documentRecordsSchema } from "./documents-fields";
import type { Page } from "./paged";
import {
  readDocumentClassification,
  readDocumentClassificationHistory,
} from "./document-classification";

export function documentQueryKey(scope: DocumentScope): string[] {
  return [
    "workforce",
    "documents",
    "account-v1",
    scope.tenantId,
    scope.subject,
    ...(scope.pharmacyId === undefined
      ? ["hospital", scope.hospitalId]
      : ["pharmacy", scope.pharmacyId]),
    scope.staffId,
    ...(scope.hrIntake === true ? [staffIntakeScopeKey(scope)] : []),
  ];
}
export function useDocumentRecords(scope: DocumentScope, enabled: boolean) {
  return useQuery({
    queryKey: [...documentQueryKey(scope), "records"],
    queryFn: async ({ signal }) =>
      documentRecordsSchema.parse(
        await apiFetch(withStaffIntakeScope(`${documentPath(scope)}/document-records`, scope), {
          headers: documentHeaders(scope),
          cache: "no-store",
          signal,
        }),
      ),
    enabled,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
}
const pageSchema = z.object({
  data: z.array(z.unknown()),
  page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullish() }),
});
export function useStaffDocuments<S extends DocumentScope>(
  scope: S,
  cursor: string | null,
  enabled: boolean,
) {
  return useQuery({
    queryKey: [...documentQueryKey(scope), "files", cursor],
    queryFn: async ({ signal }): Promise<Page<DocumentOf<S>>> => {
      const page = pageSchema.parse(
        await apiFetch(
          withStaffIntakeScope(
            `${documentPath(scope)}/documents${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
            scope,
          ),
          { headers: documentHeaders(scope), cache: "no-store", signal },
        ),
      );
      return {
        data: page.data.map((file) => ownedDocument(scope, file)),
        page: { has_more: page.page.has_more, next_cursor: page.page.next_cursor ?? null },
      };
    },
    enabled,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchInterval: (query) =>
      query.state.data?.data.some(
        (file) =>
          file.status === "scan_pending" ||
          (file.status === "scan_error" && Date.now() - Date.parse(file.updated_at) < 5 * 60_000),
      )
        ? POLL_INTERVAL_MS
        : false,
  });
}

export function documentClassificationQueryKey(
  scope: HospitalDocumentScope,
  file: string,
): string[] {
  return [...documentQueryKey(scope), "classification", file];
}

export function useDocumentClassification(
  scope: HospitalDocumentScope,
  file: string,
  enabled: boolean,
) {
  return useQuery({
    queryKey: documentClassificationQueryKey(scope, file),
    queryFn: ({ signal }) => readDocumentClassification(scope, file, signal),
    enabled,
    gcTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  });
}

export function useDocumentClassificationHistory(
  scope: HospitalDocumentScope,
  file: string,
  cursor: string | null,
  enabled: boolean,
) {
  return useQuery({
    queryKey: [...documentClassificationQueryKey(scope, file), "history", cursor],
    queryFn: ({ signal }) => readDocumentClassificationHistory(scope, file, cursor, signal),
    enabled,
    gcTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
