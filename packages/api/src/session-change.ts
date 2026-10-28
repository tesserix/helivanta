export const SESSION_CHANGE_KEY = "hms.session-change.v1";
let source: string | undefined;

export function sessionChangeSource(): string {
  return (source ??= crypto.randomUUID());
}

export function notifySessionChanged(): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SESSION_CHANGE_KEY, crypto.randomUUID());
  } catch {
    // BroadcastChannel also reaches tabs when browser storage is disabled.
  }
  try {
    const channel = new BroadcastChannel(SESSION_CHANGE_KEY);
    try {
      channel.postMessage({ type: "changed", source: sessionChangeSource() });
    } finally {
      channel.close();
    }
  } catch {
    // Both transports can be unavailable; server checks remain authoritative.
  }
}
