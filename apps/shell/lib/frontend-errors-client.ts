import {
  CLIENT_ERROR_EVENT,
  CLIENT_ERROR_PATH,
  ERROR_AREAS,
  ERROR_KINDS,
  type FrontendErrorReport,
  type FrontendErrorSource,
} from "./frontend-errors-contract";

const observed = new WeakMap<object, Promise<FrontendErrorReport | undefined>>();
let windowStart = 0;
let sent = 0;
let installed = false;

function kindOf(error: unknown): FrontendErrorReport["kind"] {
  try {
    const name =
      typeof error === "object" && error !== null && "name" in error ? error.name : undefined;
    return ERROR_KINDS.find((kind) => kind === name) ?? "unknown";
  } catch {
    return "unknown";
  }
}

function area(): FrontendErrorReport["area"] {
  const prefix = window.location.pathname.split("/")[1];
  return ERROR_AREAS.find((candidate) => candidate === prefix) ?? "other";
}

function maySend(): boolean {
  const now = Date.now();
  if (now - windowStart >= 60_000 || now < windowStart) {
    windowStart = now;
    sent = 0;
  }
  if (sent >= 5) return false;
  sent++;
  return true;
}

async function submit(
  error: unknown,
  source: FrontendErrorSource,
): Promise<FrontendErrorReport | undefined> {
  try {
    const report: FrontendErrorReport = {
      version: 1,
      reference: `HV-${crypto.randomUUID()}`,
      source,
      kind: kindOf(error),
      area: area(),
    };
    let local = false;
    try {
      console.error(JSON.stringify({ severity: "ERROR", event: CLIENT_ERROR_EVENT, ...report }));
      local = true;
    } catch {
      // A failed diagnostic sink must not replace the recovery screen.
    }
    if (!maySend()) return local ? report : undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      const controller = new AbortController();
      timer = setTimeout(() => controller.abort(), 1500);
      const response = await fetch(CLIENT_ERROR_PATH, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(report),
        credentials: "omit",
        // CORS mode preserves Origin while no-referrer omits potentially private paths.
        mode: "cors",
        referrerPolicy: "no-referrer",
        cache: "no-store",
        signal: controller.signal,
      });
      if (response.status === 204) return report;
    } catch {
      // The browser log remains the correlation source when delivery fails.
    } finally {
      if (timer !== undefined) clearTimeout(timer);
    }
    return local ? report : undefined;
  } catch {
    return undefined;
  }
}

export function reportFrontendError(
  error: unknown,
  source: FrontendErrorSource,
): Promise<FrontendErrorReport | undefined> {
  if (typeof error !== "object" || error === null) return submit(error, source);
  const existing = observed.get(error);
  if (existing) return existing;
  const pending = submit(error, source);
  observed.set(error, pending);
  return pending;
}

export function installClientErrorReporting(): () => void {
  if (installed || typeof window === "undefined") return () => {};
  installed = true;
  const onError = (event: ErrorEvent) => {
    try {
      void reportFrontendError(event.error, "runtime");
    } catch {
      // Instrumentation must not cause a second unhandled failure.
    }
  };
  const onRejection = (event: PromiseRejectionEvent) => {
    try {
      void reportFrontendError(event.reason, "unhandled_rejection");
    } catch {
      // Instrumentation must not cause a second unhandled failure.
    }
  };
  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);
  return () => {
    installed = false;
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onRejection);
  };
}
