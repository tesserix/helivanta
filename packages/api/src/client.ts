const API_PREFIX = "/api/v1";

export const POLL_INTERVAL_MS = 3000;

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  // Extra problem fields such as conflicting record IDs; never trusted for authorization.
  readonly detail: Readonly<Record<string, unknown>>;

  constructor(code: string, message: string, status: number, detail: Record<string, unknown> = {}) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.detail = detail;
  }
}

type Envelope = { error?: string; message?: string; [field: string]: unknown };

// Same-origin client for the Go API. Every zone reaches the backend
// through its /api rewrite, so the session cookie flows automatically.
export async function apiFetchResponse(path: string, init: RequestInit = {}): Promise<Response> {
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
      const body: unknown = await res.json();
      if (body && typeof body === "object" && !Array.isArray(body)) envelope = body as Envelope;
    } catch {
      // non-JSON error body — fall through to the generic error
    }
    const { error, message, ...detail } = envelope;
    throw new ApiError(
      error ?? "internal",
      message ?? "Something went wrong. Try again.",
      res.status,
      detail,
    );
  }
  return res;
}

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  return (await (await apiFetchResponse(path, init)).json()) as T;
}
