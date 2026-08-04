// One place for clinical timestamp rendering — never call
// toLocaleTimeString directly in components.
const time = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

export function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return time.format(d);
}

export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return dateTime.format(d);
}
