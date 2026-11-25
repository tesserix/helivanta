"use client";
import { useState } from "react";
import { Button, cn } from "@tesserix/web";
import {
  formatMinutes,
  overtimeSummaryPath,
  useApiPagedQuery,
  type Hospital,
  type OvertimeSummaryRow,
} from "@helivanta/api";
import { Field, Panel, RequestFailure, controlClass, tableStyles } from "@helivanta/ui";
import { today } from "../roster-policy/policy-ui";

const cell = "px-4 py-3 align-top";
const head = cn(tableStyles.head, "px-4 py-3");
const numberHead = cn(head, tableStyles.number);
const numberCell = cn(cell, tableStyles.number);
const validMonth = (m: string) => /^(2[01]\d\d)-(0[1-9]|1[0-2])$/.test(m);

export function OvertimeSummary({ hospital }: { hospital: Hospital }) {
  const [month, setMonth] = useState(() => today().slice(0, 7));
  const base = `/workforce/hospitals/${hospital.id}`;
  const valid = validMonth(month);
  const rows = useApiPagedQuery<OvertimeSummaryRow>(
    ["workforce", "overtime-summary", hospital.id, month],
    overtimeSummaryPath(base, month),
    { enabled: valid },
  );
  return (
    <Panel flush aria-label="Overtime by month">
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Overtime by month</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Payable minutes count approved overtime only; rejected, withdrawn and revoked overtime
            counts nothing.
          </p>
        </div>
        <div className="w-48">
          <Field
            id="overtime-month"
            label="Month"
            error={valid ? undefined : "Choose a month between 2000 and 2199."}
          >
            <input
              id="overtime-month"
              type="month"
              className={controlClass}
              aria-invalid={!valid}
              value={month}
              onChange={(e) => setMonth(e.target.value)}
            />
          </Field>
        </div>
      </div>
      {!valid ? null : rows.isPending ? (
        <p role="status" className="p-5">
          Loading monthly overtime…
        </p>
      ) : rows.isError ? (
        <div className="p-5">
          <RequestFailure
            message="Could not load monthly overtime."
            retry={rows.retry}
            error={rows.error}
          />
        </div>
      ) : rows.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No overtime was recorded in {month}.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr>
                <th scope="col" className={head}>
                  Staff
                </th>
                <th scope="col" className={numberHead}>
                  Pending
                </th>
                <th scope="col" className={numberHead}>
                  Approved requested
                </th>
                <th scope="col" className={numberHead}>
                  Payable
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.items.map((r) => (
                <tr key={r.staff_id} className={tableStyles.row}>
                  <td className={cell}>
                    <span className="font-medium">{r.display_name}</span>
                    <span className="block text-xs text-muted-foreground">{r.employee_code}</span>
                  </td>
                  <td className={numberCell}>{formatMinutes(r.pending_minutes)}</td>
                  <td className={numberCell}>{formatMinutes(r.approved_requested_minutes)}</td>
                  <td className={cn(numberCell, "font-medium")}>
                    {formatMinutes(r.payable_minutes)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {valid && rows.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={rows.isFetchingMore} onClick={rows.loadMore}>
            Load more people
          </Button>
        </div>
      )}
    </Panel>
  );
}
