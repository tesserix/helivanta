import { z } from "zod";
import { apiFetch, apiFetchResponse } from "./client";
import { readAttachmentFile, sha256Hex } from "./attachment-file";
import { staffDocumentSchema } from "./documents-fields";

export const MAX_EVIDENCE = 10;

export type Evidence = Omit<z.infer<typeof staffDocumentSchema>, "record_kind" | "category"> & {
  record_kind: string;
  category: string;
};
export type EvidenceList = { evidence: Evidence[]; uploads_available: boolean };

// Scope pins every file to the staff member and record the screen shows.
export type EvidenceScope = { recordPath: string; staffId: string; recordId: string };

type EvidenceInput = {
  revision_id?: string;
  filename: string;
  content_type: string;
  size: number;
  sha256: string;
};
export type EvidenceUpload = { id: string; recordId: string; file: File; input: EvidenceInput };

export type EvidenceKind = {
  key: string;
  recordKind: string;
  category: string;
  input: z.ZodType<EvidenceInput, z.ZodTypeDef, unknown> & {
    shape: { content_type: z.ZodTypeAny; size: z.ZodTypeAny };
  };
  // Tax names its revision in the query and body; other kinds carry the record in the path.
  recordParam?: "revision_id";
};

const identifier = staffDocumentSchema.shape.id;
const filename = staffDocumentSchema.shape.filename;

export function evidenceClient(kind: EvidenceKind) {
  const schema = staffDocumentSchema.extend({
    record_kind: z.literal(kind.recordKind),
    category: z.literal(kind.category),
  });
  const listSchema = z.object({
    evidence: z.array(schema).max(MAX_EVIDENCE),
    uploads_available: z.boolean(),
  });
  const root = (scope: EvidenceScope) => {
    identifier.parse(scope.staffId);
    identifier.parse(scope.recordId);
    return `${scope.recordPath}/evidence`;
  };
  const receipt = (scope: EvidenceScope, value: unknown, id?: string): Evidence => {
    const saved = schema.parse(value);
    if (
      saved.staff_id !== scope.staffId ||
      saved.record_id !== scope.recordId ||
      (id !== undefined && saved.id !== id)
    )
      throw new Error("Evidence ownership does not match this proposal.");
    return saved;
  };
  return {
    fileSchema: z.object({
      file: z.custom<File | null>().superRefine((file, ctx) => {
        if (!file) {
          ctx.addIssue({ code: "custom", message: "Choose a file first." });
          return;
        }
        const name = filename.safeParse(file.name);
        if (!name.success) ctx.addIssue({ code: "custom", message: name.error.issues[0]!.message });
        else if (!kind.input.shape.content_type.safeParse(file.type).success)
          ctx.addIssue({ code: "custom", message: "Choose a PNG or JPEG image." });
        else if (!kind.input.shape.size.safeParse(file.size).success)
          ctx.addIssue({ code: "custom", message: "Choose an image up to 10 MB." });
      }),
    }),
    key: (scope: EvidenceScope) => ["workforce", kind.key, scope.recordPath, scope.recordId],
    receipt,

    async fetch(scope: EvidenceScope, signal: AbortSignal): Promise<EvidenceList> {
      const query = kind.recordParam ? `?${kind.recordParam}=${scope.recordId}` : "";
      const list = listSchema.parse(
        await apiFetch(root(scope) + query, { signal, cache: "no-store" }),
      );
      list.evidence.forEach((file) => receipt(scope, file));
      return list;
    },

    async prepare(
      scope: EvidenceScope,
      id: string,
      file: File,
      signal: AbortSignal,
    ): Promise<EvidenceUpload> {
      signal.throwIfAborted();
      identifier.parse(id);
      const draft = {
        ...(kind.recordParam && { [kind.recordParam]: scope.recordId }),
        filename: filename.parse(file.name),
        content_type: file.type,
        size: file.size,
      };
      kind.input.parse({ ...draft, sha256: "0".repeat(64) });
      const data = await readAttachmentFile(file, signal);
      signal.throwIfAborted();
      if (data.byteLength !== file.size)
        throw new Error("The selected image could not be read safely.");
      const sha256 = await sha256Hex(data);
      signal.throwIfAborted();
      return { id, recordId: scope.recordId, file, input: kind.input.parse({ ...draft, sha256 }) };
    },

    async submit(
      scope: EvidenceScope,
      upload: EvidenceUpload,
      signal: AbortSignal,
    ): Promise<Evidence> {
      signal.throwIfAborted();
      const input = kind.input.parse(upload.input);
      if (
        upload.recordId !== scope.recordId ||
        (kind.recordParam && input[kind.recordParam] !== scope.recordId)
      )
        throw new Error("The upload belongs to a different proposal.");
      const path = `${root(scope)}/${identifier.parse(upload.id)}`;
      const intent = receipt(
        scope,
        await apiFetch(path, { method: "PUT", body: JSON.stringify(input), signal }),
        upload.id,
      );
      if (
        intent.sha256 !== input.sha256 ||
        intent.filename !== input.filename ||
        intent.size !== input.size ||
        intent.content_type !== input.content_type
      )
        throw new Error("The saved upload request does not match the selected file.");
      signal.throwIfAborted();
      if (intent.status !== "upload_pending") return intent;
      return receipt(
        scope,
        await apiFetch(path + "/content", {
          method: "PUT",
          headers: { "Content-Type": input.content_type },
          body: upload.file,
          signal,
        }),
        upload.id,
      );
    },

    async retry(
      scope: EvidenceScope,
      file: Evidence,
      attempt: number,
      signal: AbortSignal,
    ): Promise<Evidence> {
      signal.throwIfAborted();
      receipt(scope, file, file.id);
      staffDocumentSchema.shape.scan_attempt.parse(attempt);
      return receipt(
        scope,
        await apiFetch(`${root(scope)}/${file.id}/retries/${attempt}`, { method: "PUT", signal }),
        file.id,
      );
    },

    async download(scope: EvidenceScope, file: Evidence, signal: AbortSignal): Promise<Blob> {
      signal.throwIfAborted();
      receipt(scope, file, file.id);
      if (file.status !== "available") throw new Error("This image is not ready for download.");
      const res = await apiFetchResponse(`${root(scope)}/${file.id}/content`, {
        cache: "no-store",
        signal,
      });
      const blob = await res.blob();
      signal.throwIfAborted();
      if (blob.size !== file.size) throw new Error("The evidence download could not be verified.");
      return blob;
    },
  };
}
export type EvidenceClient = ReturnType<typeof evidenceClient>;
