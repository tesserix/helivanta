import { ApiError } from "@helivanta/api";
export const employmentReviewContextChanged = (error: unknown) =>
  error instanceof ApiError && [401, 403, 404, 409].includes(error.status);
