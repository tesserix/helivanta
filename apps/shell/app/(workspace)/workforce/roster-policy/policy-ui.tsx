import { Button } from "@tesserix/web";
import { WEEKDAYS } from "@helivanta/api";

export const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";
export const today = () => new Date().toISOString().slice(0, 10);
export const weekdayOptions = WEEKDAYS.map((label, index) => ({ value: String(index), label }));
export function Failure({ message, retry }: { message: string; retry: () => void }) {
  return (
    <div role="alert" className="rounded-xl border p-6 text-sm">
      {message}{" "}
      <Button variant="outline" onClick={retry}>
        Try again
      </Button>
    </div>
  );
}
export function ReviewBox({
  id,
  error,
  children,
  register,
}: {
  id: string;
  error?: string;
  children: React.ReactNode;
  register: object;
}) {
  return (
    <div className="rounded-xl border bg-muted/30 p-4">
      <label className="flex items-start gap-3 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
          aria-describedby={error ? `${id}-error` : undefined}
          {...register}
        />
        {children}
      </label>
      {error && (
        <p id={`${id}-error`} className="mt-2 text-xs text-destructive">
          {error}
        </p>
      )}
      <p className="mt-3 text-xs text-muted-foreground">
        Changes create a new revision effective on or after the current one. Earlier rosters keep
        the policy they were built on.
      </p>
    </div>
  );
}
