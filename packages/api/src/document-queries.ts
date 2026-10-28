"use client";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { documentPath, documentHeaders, type DocumentScope } from "./documents";
import { documentRecordsSchema, staffDocumentSchema, type StaffDocument } from "./documents-fields";
import type { Page } from "./paged";
import {
  readDocumentClassification,
  readDocumentClassificationHistory,
} from "./document-classification";

export function documentQueryKey(scope: DocumentScope): string[] {
  return ["workforce", "documents", scope.tenantId, scope.hospitalId, scope.staffId];
}
export function useDocumentRecords(scope: DocumentScope, enabled: boolean) {
  return useQuery({
    queryKey: [...documentQueryKey(scope), "records"],
    queryFn: async ({ signal }) =>
      documentRecordsSchema.parse(
        await apiFetch(`${documentPath(scope)}/document-records`, {
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
  data: z.array(staffDocumentSchema),
  page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullish() }),
});
export function useStaffDocuments(scope: DocumentScope, cursor: string | null, enabled: boolean) {
  return useQuery({
    queryKey: [...documentQueryKey(scope), "files", cursor],
    queryFn: async ({ signal }): Promise<Page<StaffDocument>> => {
      const page = pageSchema.parse(
        await apiFetch(
          `${documentPath(scope)}/documents${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
          { headers: documentHeaders(scope), cache: "no-store", signal },
        ),
      );
      if (
        page.data.some(
          (file) => file.hospital_id !== scope.hospitalId || file.staff_id !== scope.staffId,
        )
      )
        throw new Error("Document ownership did not match this workspace.");
      return {
        data: page.data,
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

export function documentClassificationQueryKey(scope: DocumentScope, file: string): string[] {
  return [...documentQueryKey(scope), "classification", file];
}

export function useDocumentClassification(scope: DocumentScope, file: string, enabled: boolean) {
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
  scope: DocumentScope,
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
