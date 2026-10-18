export function isCustomerTimeZone(timeZone: string): boolean {
  if (!timeZone) return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone });
    return true;
  } catch {
    return false;
  }
}

function formatter(timeZone: string) {
  return new Intl.DateTimeFormat("en-GB", {
    timeZone,
    era: "short",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
    timeZoneName: "longOffset",
  });
}

function parts(at: Date, format: Intl.DateTimeFormat) {
  return Object.fromEntries(format.formatToParts(at).map((part) => [part.type, part.value]));
}

function civilTime(
  year: number,
  month: number,
  day: number,
  hour: number,
  minute: number,
  second = 0,
) {
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  date.setUTCHours(hour, minute, second, 0);
  return date.getTime();
}

function inputFromParts(p: Record<string, string>) {
  return `${p.year?.padStart(4, "0")}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
}

export function zonedDateTimeInput(instant: string, timeZone: string): string {
  const date = new Date(instant);
  if (!isCustomerTimeZone(timeZone) || !Number.isFinite(date.getTime())) return "";
  return inputFromParts(parts(date, formatter(timeZone)));
}

// A wall time needs exactly one instant. DST gaps and folds require an explicit
// unambiguous observation time rather than silently changing its meaning.
export function zonedDateTimeISO(input: string, timeZone: string): string | null {
  if (!isCustomerTimeZone(timeZone)) return null;
  const match = /^(\d{4})-(\d{2})-(\d{2})T([01]\d|2[0-3]):([0-5]\d)$/.exec(input);
  if (!match) return null;
  const values = match.slice(1).map(Number);
  const [year, month, day, hour, minute] = values as [number, number, number, number, number];
  if (year < 1 || month < 1 || month > 12 || day < 1 || day > 31) return null;
  const wall = civilTime(year, month, day, hour, minute);
  const check = new Date(wall);
  if (
    check.getUTCFullYear() !== year ||
    check.getUTCMonth() !== month - 1 ||
    check.getUTCDate() !== day
  )
    return null;
  const format = formatter(timeZone);
  const offsets = new Set<number>();
  for (let hours = -48; hours <= 48; hours += 6) {
    const sample = wall + hours * 3600000;
    const p = parts(new Date(sample), format);
    const sampleYear = p.era === "BC" ? 1 - Number(p.year) : Number(p.year);
    offsets.add(
      civilTime(
        sampleYear,
        Number(p.month),
        Number(p.day),
        Number(p.hour),
        Number(p.minute),
        Number(p.second),
      ) - sample,
    );
  }
  const candidates = [...offsets]
    .map((offset) => new Date(wall - offset))
    .filter(
      (date) =>
        date.getUTCFullYear() >= 1 &&
        date.getUTCFullYear() <= 9999 &&
        inputFromParts(parts(date, format)) === input,
    );
  return candidates.length === 1 ? candidates[0]!.toISOString() : null;
}

export function formatZonedDateTime(instant: string, timeZone: string): string {
  const date = new Date(instant);
  if (!isCustomerTimeZone(timeZone) || !Number.isFinite(date.getTime())) return "";
  const p = parts(date, formatter(timeZone));
  const fraction = instant.match(/\.(\d+)/)?.[1];
  return `${inputFromParts(p).replace("T", " ")}:${p.second}${fraction ? "." + fraction : ""} ${p.timeZoneName}`;
}
