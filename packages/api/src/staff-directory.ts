import { z } from "zod";
import { staffSchema } from "./workspace";
import { staffDirectoryReadMetadataContract } from "./staff-directory-contract";

export const staffDirectoryViewSchema = staffSchema
  .extend({
    id: z.string().guid(),
    hospital_id: z.string().guid(),
    created_at: z.string().datetime({ offset: true }),
    updated_at: z.string().datetime({ offset: true }),
    ...staffDirectoryReadMetadataContract.shape,
  })
  .strict();
export type StaffDirectoryView = z.infer<typeof staffDirectoryViewSchema>;
