const API_PREFIX = "/api/v1";

export const POLL_INTERVAL_MS = 3000;

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

type Envelope = { error?: string; message?: string };

// Same-origin client for the Go API. Every zone reaches the backend
// through its /api rewrite, so the session cookie flows automatically.
export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(`${API_PREFIX}${path}`, {
    ...init,
    headers,
  });
  if (!res.ok) {
    let envelope: Envelope = {};
    try {
      envelope = (await res.json()) as Envelope;
    } catch {
      // non-JSON error body — fall through to the generic error
    }
    throw new ApiError(
      envelope.error ?? "internal",
      envelope.message ?? "Something went wrong. Try again.",
      res.status,
    );
  }
  return (await res.json()) as T;
}
