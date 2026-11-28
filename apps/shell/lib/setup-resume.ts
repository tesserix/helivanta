// A non-secret note, in this tab only, that a password-setup page has just
// asked the server to park a sign-in session (#1778). It holds no handle or
// credential: the handle is an HttpOnly cookie. /login uses it to decide
// whether to ask the server to resume, and consumes it either way.
const KEY = "helivanta.setupResume";

export function markSetupResume(): void {
  try {
    window.sessionStorage.setItem(KEY, "1");
  } catch {
    // Storage refused: sign-in simply asks for the password again.
  }
}

// peekSetupResume reads without consuming, so a render may call it safely.
export function peekSetupResume(): boolean {
  try {
    return typeof window !== "undefined" && window.sessionStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

export function takeSetupResume(): boolean {
  try {
    const marked = window.sessionStorage.getItem(KEY) === "1";
    window.sessionStorage.removeItem(KEY);
    return marked;
  } catch {
    return false;
  }
}
