// One place for clinical timestamp rendering — never call
// toLocaleTimeString directly in components.
const time = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

export function formatTime(iso: string): string {
  return time.format(new Date(iso));
}

export function formatDateTime(iso: string): string {
  return dateTime.format(new Date(iso));
}
