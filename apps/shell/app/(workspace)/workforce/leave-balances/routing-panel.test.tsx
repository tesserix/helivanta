import "@/test/select";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { Hospital } from "@helivanta/api";
import { LeaveRoutingPanel } from "./routing-panel";

const hospital: Hospital = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "Synthetic Hospital",
  access_level: "manager",
};
const saved = {
  version: 2,
  mode: "reporting_line",
  escalation_hours: 48,
  source_reference: "Synthetic HR circular 7",
  reviewed_by_subject: "synthetic-hr",
  created_at: "2026-11-04T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

function mock({
  current = { version: 0, mode: "hospital", source_reference: "" } as object,
  reject = 0,
} = {}) {
  const writes: unknown[] = [];
  let reads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (!String(input).endsWith(`/hospitals/${hospital.id}/hr/leave-routing`))
        throw new Error(`Unexpected request ${String(input)}`);
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return reject
          ? Response.json(
              {
                error: "conflict",
                message: "The leave routing policy changed since you opened it.",
              },
              { status: reject },
            )
          : Response.json({ ...saved, version: 1 });
      }
      reads++;
      return Response.json(current);
    }),
  );
  return { writes, reads: () => reads };
}

it("shows the saved policy to readers without an editor", async () => {
  mock({ current: saved });
  renderWithProviders(<LeaveRoutingPanel hospital={hospital} writable={false} />);
  expect(
    await screen.findByText(
      /Version 2 · Reporting line with escalation · escalates every 48 hours/,
    ),
  ).toBeInTheDocument();
  expect(
    screen.getByText(/Synthetic HR circular 7 · reviewed by synthetic-hr/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Edit leave approval routing" })).toBeNull();
});

it("requires review, an interval and a source before saving reporting-line routing", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<LeaveRoutingPanel hospital={hospital} writable />);
  expect(await screen.findByText(/hospital leave desk decides every request/)).toBeInTheDocument();
  await user.click(screen.getByRole("combobox", { name: "Who decides leave first" }));
  await user.click(screen.getByRole("option", { name: "Reporting line with escalation" }));
  await user.click(screen.getByRole("button", { name: "Save routing policy" }));
  expect(await screen.findAllByText("Enter whole hours from 1 to 720.")).not.toHaveLength(0);
  expect(screen.getByText("Give the circular or order that sets this policy.")).toBeInTheDocument();
  expect(screen.getByText("Confirm that you reviewed the change.")).toBeInTheDocument();
  await user.type(screen.getByLabelText(/Escalate to the next level/), "721");
  await user.type(screen.getByLabelText("Source reference"), "  Synthetic HR circular 9  ");
  await user.click(screen.getByRole("checkbox", { name: /approved leave order/ }));
  await user.click(screen.getByRole("button", { name: "Save routing policy" }));
  expect(await screen.findByText("Enter whole hours from 1 to 720.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(screen.getByLabelText(/Escalate to the next level/));
  await user.type(screen.getByLabelText(/Escalate to the next level/), "24");
  await user.click(screen.getByRole("button", { name: "Save routing policy" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    expected_version: 0,
    mode: "reporting_line",
    escalation_hours: 24,
    source_reference: "Synthetic HR circular 9",
    reviewed: true,
  });
});

it("drops the interval when switching back to the hospital desk", async () => {
  const { writes } = mock({ current: saved });
  const { user } = renderWithProviders(<LeaveRoutingPanel hospital={hospital} writable />);
  await user.click(await screen.findByRole("combobox", { name: "Who decides leave first" }));
  await user.click(screen.getByRole("option", { name: "Hospital leave desk" }));
  expect(screen.queryByLabelText(/Escalate to the next level/)).toBeNull();
  await user.click(screen.getByRole("checkbox", { name: /approved leave order/ }));
  await user.click(screen.getByRole("button", { name: "Save routing policy" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    expected_version: 2,
    mode: "hospital",
    source_reference: "Synthetic HR circular 7",
    reviewed: true,
  });
});

it("keeps the input on a stale version and reloads on request", async () => {
  const { reads } = mock({ current: saved, reject: 409 });
  const { user } = renderWithProviders(<LeaveRoutingPanel hospital={hospital} writable />);
  await user.click(await screen.findByRole("checkbox", { name: /approved leave order/ }));
  await user.click(screen.getByRole("button", { name: "Save routing policy" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "changed since you opened it. Your input is preserved.",
  );
  const before = reads();
  await user.click(screen.getByRole("button", { name: "Reload policy" }));
  await waitFor(() => expect(reads()).toBeGreaterThan(before));
});
