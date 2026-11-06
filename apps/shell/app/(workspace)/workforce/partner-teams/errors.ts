import { ApiError } from "@helivanta/api";
export function accessChanged(error: Error | null | undefined) {
  return (
    error instanceof ApiError &&
    (error.status === 401 ||
      error.status === 403 ||
      error.code === "session_changed" ||
      error.code === "tenant_changed")
  );
}
