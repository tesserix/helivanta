import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import {
  APPROVE,
  child,
  conflict,
  coverage,
  dependentID,
  enrollment,
  enrollmentID,
  healthID,
  healthPlan,
  hospital,
  lifePlan,
  MANAGE,
  member,
  mockBenefits,
  proposal,
  proposalID,
  spouse,
  staffID,
  stamp,
  VIEW,
} from "@/test/benefits";
import Benefits from "./page";

afterEach(() => vi.unstubAllGlobals());

type User = Awaited<ReturnType<typeof renderWithProviders>>["user"];
const enrollments = `/workforce/hospitals/${hospital}/staff/${staffID}/benefit-enrollments`;
const uuid = "[0-9a-f-]{36}";
const otherEnrollment = "cccccccc-cccc-4ccc-8ccc-cccccccccccd";
const accidentID = "cccccccc-cccc-4ccc-8ccc-ccccccccccce";
const accidentPlan = {
  ...healthPlan,
  id: accidentID,
  code: "SYN-ACCIDENT",
  name: "Synthetic Accident",
  kind: "accident",
  covers_dependents: false,
  covered_relationships: [],
};
const partnerHealth = "Synthetic Health · Synthetic Partner (Synthetic Spouse)";
const waiting = enrollment({
  coverage,
  pending: proposal({
    base_version: 1,
    dependent_id: dependentID,
    coverage_from: "2026-02-01",
    coverage_until: "2026-12-31",
    eligibility_basis: "Synthetic resignation letter",
  }),
});

const openCoverage = async (user: User, label = "Synthetic Nurse · SYN-1") => {
  const panel = await screen.findByRole("region", { name: "Staff benefits" });
  await chooseOption(
    user,
    await within(panel).findByRole("combobox", { name: "Staff member" }),
    label,
  );
  return screen.findByRole("region", { name: "Coverage" });
};
const proposalPart = (url: string) => url.split("/proposals/")[1];
const enrollmentPart = (url: string) => url.split("/benefit-enrollments/")[1]!.split("/")[0];

it("shows approved coverage and a waiting proposal read-only to a reader", async () => {
  mockBenefits({
    permissions: [VIEW],
    subject: "synthetic-reader",
    access: "reader",
    enrollments: [
      enrollment({ coverage }),
      enrollment({
        id: otherEnrollment,
        dependent_id: undefined,
        pending: proposal({ enrollment_id: otherEnrollment }),
      }),
    ],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  expect(await within(section).findByText(partnerHealth)).toBeInTheDocument();
  expect(
    within(section).getByText("Approved · v1 · coverage 2026-02-01 with no end date"),
  ).toBeInTheDocument();
  expect(
    within(section).getByText("Synthetic Health · Synthetic Nurse (employee)"),
  ).toBeInTheDocument();
  expect(within(section).getByText("No approved coverage yet")).toBeInTheDocument();
  expect(
    within(section).getByText(
      "Waiting for approval · coverage 2026-04-01 with no end date · proposed by synthetic-maker",
    ),
  ).toBeInTheDocument();
  for (const name of [/^Add coverage/, /^Change coverage/, /^Review proposal/, /^Withdraw/])
    expect(within(section).queryByRole("button", { name })).toBeNull();
});

it("explains empty coverage and offers a retry when it cannot load", async () => {
  mockBenefits();
  const first = renderWithProviders(<Benefits />);
  const section = await openCoverage(first.user);
  expect(await within(section).findByText("No coverage recorded")).toBeInTheDocument();
  first.unmount();
  mockBenefits({ fail: "enrollments" });
  const second = renderWithProviders(<Benefits />);
  const failed = await openCoverage(second.user);
  expect(await within(failed).findByText("Could not load coverage.")).toBeInTheDocument();
  expect(within(failed).getByRole("button", { name: "Try again" })).toBeInTheDocument();
});

it("proposes coverage only for people the chosen plan covers", async () => {
  const { writes } = mockBenefits({
    plans: [healthPlan, lifePlan, accidentPlan],
    dependents: [spouse, child],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(await within(section).findByRole("button", { name: "Add coverage" }));
  const form = within(section).getByRole("form", { name: "Add coverage" });
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  expect(await within(form).findByText("Choose a valid record.")).toBeInTheDocument();
  expect(within(form).getByText("Use a valid date.")).toBeInTheDocument();
  expect(within(form).getByText("Say what the approver should check.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);

  await user.click(within(form).getByRole("combobox", { name: "Plan" }));
  expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
    "Synthetic Health",
    "Synthetic Accident",
  ]);
  await user.click(screen.getByRole("option", { name: "Synthetic Health" }));
  await user.click(within(form).getByRole("combobox", { name: "Covers" }));
  expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
    "Synthetic Nurse (employee)",
    "Synthetic Partner (Synthetic Spouse)",
  ]);
  await user.click(screen.getByRole("option", { name: "Synthetic Partner (Synthetic Spouse)" }));
  await user.type(within(form).getByLabelText("Coverage from"), "2026-04-01");
  await user.type(
    within(form).getByLabelText("What the approver should check"),
    "Synthetic marriage certificate",
  );
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));

  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.method).toBe("PUT");
  expect(writes[0]!.url).toMatch(new RegExp(`${enrollments}/${uuid}/proposals/${uuid}$`));
  expect(writes[0]!.body).toEqual({
    expected_version: 0,
    plan_id: healthID,
    dependent_id: dependentID,
    coverage_from: "2026-04-01",
    eligibility_basis: "Synthetic marriage certificate",
  });
  expect(await screen.findByText("Coverage proposed")).toBeInTheDocument();
  expect(within(section).queryByRole("form", { name: "Add coverage" })).toBeNull();
});

it("loads further plans and dependents from inside a new proposal", async () => {
  const { writes } = mockBenefits({
    plans: [accidentPlan],
    dependents: [child],
    secondPage: { plans: [healthPlan], dependents: [spouse] },
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(await within(section).findByRole("button", { name: "Add coverage" }));
  const form = within(section).getByRole("form", { name: "Add coverage" });
  const options = async (name: string) => {
    await user.click(within(form).getByRole("combobox", { name }));
    const shown = screen.getAllByRole("option").map((o) => o.textContent);
    await user.keyboard("{Escape}");
    return shown;
  };
  expect(await options("Plan")).toEqual(["Synthetic Accident"]);

  await user.click(within(form).getByRole("button", { name: "Load more plans" }));
  await waitFor(() =>
    expect(within(form).queryByRole("button", { name: "Load more plans" })).toBeNull(),
  );
  expect(await options("Plan")).toEqual(["Synthetic Accident", "Synthetic Health"]);
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Plan" }),
    "Synthetic Health",
  );
  expect(await options("Covers")).toEqual(["Synthetic Nurse (employee)"]);

  await user.click(within(form).getByRole("button", { name: "Load more dependents" }));
  await waitFor(() =>
    expect(within(form).queryByRole("button", { name: "Load more dependents" })).toBeNull(),
  );
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Covers" }),
    "Synthetic Partner (Synthetic Spouse)",
  );
  await user.type(within(form).getByLabelText("Coverage from"), "2026-04-01");
  await user.type(
    within(form).getByLabelText("What the approver should check"),
    "Synthetic marriage certificate",
  );
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toMatchObject({ plan_id: healthID, dependent_id: dependentID });
});

it("returns the person to the employee when the new plan does not cover them", async () => {
  const { writes } = mockBenefits({ plans: [healthPlan, accidentPlan] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(await within(section).findByRole("button", { name: "Add coverage" }));
  const form = within(section).getByRole("form", { name: "Add coverage" });
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Plan" }),
    "Synthetic Health",
  );
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Covers" }),
    "Synthetic Partner (Synthetic Spouse)",
  );
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Plan" }),
    "Synthetic Accident",
  );
  expect(within(form).getByRole("combobox", { name: "Covers" })).toHaveTextContent(
    "Synthetic Nurse (employee)",
  );
  await user.type(within(form).getByLabelText("Coverage from"), "2026-04-01");
  await user.type(within(form).getByLabelText("What the approver should check"), "Synthetic form");
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toEqual({
    expected_version: 0,
    plan_id: accidentID,
    coverage_from: "2026-04-01",
    eligibility_basis: "Synthetic form",
  });
});

it("replays a failed proposal under its ids and gives a changed one a new proposal id", async () => {
  const unavailable = {
    status: 503,
    body: { error: "unavailable", message: "Try again shortly." },
  };
  const { writes } = mockBenefits({ replies: [unavailable, unavailable] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(await within(section).findByRole("button", { name: "Add coverage" }));
  const form = within(section).getByRole("form", { name: "Add coverage" });
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Plan" }),
    "Synthetic Health",
  );
  await user.type(within(form).getByLabelText("Coverage from"), "2026-04-01");
  const basis = within(form).getByLabelText("What the approver should check");
  await user.type(basis, "Synthetic form");
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  expect(await within(form).findByText(/Try again shortly\./)).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]!.url).toBe(writes[0]!.url);
  expect(writes[1]!.body).toEqual(writes[0]!.body);

  await user.type(basis, " and payslip");
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  await waitFor(() => expect(writes).toHaveLength(3));
  expect(enrollmentPart(writes[2]!.url)).toBe(enrollmentPart(writes[0]!.url));
  expect(proposalPart(writes[2]!.url)).not.toBe(proposalPart(writes[0]!.url));
  expect(writes[2]!.body).toMatchObject({ eligibility_basis: "Synthetic form and payslip" });
  expect(await screen.findByText("Coverage proposed")).toBeInTheDocument();
});

it("changes approved coverage keeping its plan and person", async () => {
  const { writes } = mockBenefits({ enrollments: [enrollment({ coverage })] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Change coverage for ${partnerHealth}` }),
  );
  const form = within(section).getByRole("form", { name: `Change coverage for ${partnerHealth}` });
  expect(within(form).queryByRole("combobox", { name: "Plan" })).toBeNull();
  expect(within(form).queryByRole("combobox", { name: "Covers" })).toBeNull();
  expect(within(form).getByLabelText("Coverage from")).toHaveValue("2026-02-01");
  await user.type(within(form).getByLabelText("Coverage until"), "2026-12-31");
  await user.type(
    within(form).getByLabelText("What the approver should check"),
    "Synthetic resignation letter",
  );
  await user.click(within(form).getByRole("button", { name: "Propose coverage" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.url).toMatch(new RegExp(`${enrollments}/${enrollmentID}/proposals/${uuid}$`));
  expect(writes[0]!.body).toEqual({
    expected_version: 1,
    plan_id: healthID,
    dependent_id: dependentID,
    coverage_from: "2026-02-01",
    coverage_until: "2026-12-31",
    eligibility_basis: "Synthetic resignation letter",
  });
});

it("lets inactive staff have existing coverage ended but takes no new coverage", async () => {
  mockBenefits({
    staff: { ...member, employment_status: "inactive" },
    enrollments: [enrollment({ coverage })],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user, "Synthetic Nurse · SYN-1 (inactive)");
  expect(
    await within(section).findByRole("button", { name: `Change coverage for ${partnerHealth}` }),
  ).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: "Add coverage" })).toBeNull();
});

it("lets a second manager approve after seeing the basis and what is in force", async () => {
  const { writes } = mockBenefits({
    permissions: [VIEW, MANAGE, APPROVE],
    subject: "synthetic-checker",
    enrollments: [waiting],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Review proposal for ${partnerHealth}` }),
  );
  expect(within(section).queryByRole("button", { name: /^Change coverage/ })).toBeNull();
  expect(within(section).queryByRole("button", { name: /^Withdraw/ })).toBeNull();
  const form = within(section).getByRole("form", { name: "Review coverage proposal" });
  const fact = (label: string) => within(form).getByText(label).nextElementSibling?.textContent;
  expect(fact("Proposed coverage")).toBe("2026-02-01 to 2026-12-31");
  expect(fact("In force now")).toBe("2026-02-01 with no end date");
  expect(fact("Eligibility basis")).toBe("Synthetic resignation letter");
  expect(fact("Proposed by")).toBe("synthetic-maker");
  await user.click(within(form).getByRole("button", { name: "Approve coverage" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.method).toBe("POST");
  expect(writes[0]!.url).toMatch(
    new RegExp(`${enrollments}/${enrollmentID}/proposals/${proposalID}/decision$`),
  );
  expect(writes[0]!.body).toEqual({ expected_version: 1, decision: "approve", note: "" });
  expect(await screen.findByText("Coverage approved")).toBeInTheDocument();
});

it("requires a note to reject and keeps the form open on a version conflict", async () => {
  const { writes } = mockBenefits({
    permissions: [VIEW, APPROVE],
    subject: "synthetic-checker",
    enrollments: [waiting],
    replies: [conflict("version_conflict", "The enrollment changed since you opened it.")],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Review proposal for ${partnerHealth}` }),
  );
  const form = within(section).getByRole("form", { name: "Review coverage proposal" });
  await user.click(within(form).getByRole("button", { name: "Reject coverage" }));
  expect(await within(form).findByText("Say why you reject it.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(form).getByLabelText("Note (required to reject)"), "Synthetic unsigned");
  await user.click(within(form).getByRole("button", { name: "Reject coverage" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toEqual({
    expected_version: 1,
    decision: "reject",
    note: "Synthetic unsigned",
  });
  expect(
    await within(form).findByText(
      /^This enrollment changed since you opened it\. Close and reopen it to see the latest version\./,
    ),
  ).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Reject coverage" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(await screen.findByText("Coverage rejected")).toBeInTheDocument();
});

it("hides review from the proposer and lets only them withdraw", async () => {
  const { writes } = mockBenefits({
    permissions: [VIEW, MANAGE, APPROVE],
    subject: "synthetic-maker",
    enrollments: [waiting],
  });
  const first = renderWithProviders(<Benefits />);
  const section = await openCoverage(first.user);
  expect(
    await within(section).findByText(/proposed by you$/, { selector: "p" }),
  ).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /^Review proposal/ })).toBeNull();
  await first.user.click(
    within(section).getByRole("button", { name: `Withdraw proposal for ${partnerHealth}` }),
  );
  const form = within(section).getByRole("form", { name: "Withdraw coverage proposal" });
  await first.user.type(
    within(form).getByLabelText("Reason for withdrawing (optional)"),
    "Synthetic duplicate",
  );
  await first.user.click(within(form).getByRole("button", { name: "Withdraw proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.url).toMatch(
    new RegExp(`${enrollments}/${enrollmentID}/proposals/${proposalID}/withdraw$`),
  );
  expect(writes[0]!.body).toEqual({ note: "Synthetic duplicate" });
  expect(await screen.findByText("Proposal withdrawn")).toBeInTheDocument();
  first.unmount();

  mockBenefits({ subject: "synthetic-other", enrollments: [waiting] });
  const second = renderWithProviders(<Benefits />);
  const other = await openCoverage(second.user);
  expect(await within(other).findByText(partnerHealth)).toBeInTheDocument();
  expect(within(other).queryByRole("button", { name: /^Review proposal/ })).toBeNull();
  expect(within(other).queryByRole("button", { name: /^Withdraw/ })).toBeNull();
});

it("lists an enrollment's proposals and decisions in its history", async () => {
  mockBenefits({
    enrollments: [enrollment({ coverage })],
    history: [
      proposal({
        dependent_id: dependentID,
        status: "approved",
        approved_version: 1,
        decided_by_subject: "synthetic-checker",
        decided_at: stamp,
        decision_note: "Synthetic certificate seen",
      }),
    ],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `History of ${partnerHealth}` }),
  );
  const history = await screen.findByRole("region", { name: `${partnerHealth} history` });
  expect(
    await within(history).findByText("Approved · v1 · coverage 2026-04-01 with no end date"),
  ).toBeInTheDocument();
  expect(
    within(history).getByText(
      "Synthetic declaration form · proposed by synthetic-maker · decided by synthetic-checker · “Synthetic certificate seen”",
    ),
  ).toBeInTheDocument();
});
