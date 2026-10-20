import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import {
  child,
  childRevision,
  conflict,
  hospital,
  member,
  mockBenefits,
  recordedSpouseRevision,
  spouse,
  staffID,
  VIEW,
} from "@/test/benefits";
import Benefits from "./page";

afterEach(() => vi.unstubAllGlobals());

type User = Awaited<ReturnType<typeof renderWithProviders>>["user"];
const dependents = `/workforce/hospitals/${hospital}/staff/${staffID}/dependents`;

const openDependents = async (user: User, label = "Synthetic Nurse · SYN-1") => {
  const panel = await screen.findByRole("region", { name: "Staff benefits" });
  await chooseOption(
    user,
    await within(panel).findByRole("combobox", { name: "Staff member" }),
    label,
  );
  return screen.findByRole("region", { name: "Dependents" });
};

it("fetches no dependents until a staff member is chosen", async () => {
  const { reads } = mockBenefits();
  renderWithProviders(<Benefits />);
  expect(
    await screen.findByText("Choose a staff member to see their dependents and coverage."),
  ).toBeInTheDocument();
  expect(reads.filter((url) => /dependents|benefit-enrollments/.test(url))).toEqual([]);
});

it("lists dependents read-only to a reader", async () => {
  mockBenefits({ permissions: [VIEW], access: "reader", dependents: [spouse, child] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  expect(await within(section).findByText("Synthetic Partner")).toBeInTheDocument();
  expect(within(section).getByText("Synthetic Spouse · born 1990-05-01")).toBeInTheDocument();
  expect(within(section).getByText("Synthetic Child · born 2020-01-01")).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: "Add dependent" })).toBeNull();
  expect(within(section).queryByRole("button", { name: /^Edit / })).toBeNull();
});

it("refuses an incomplete dependent and adds one from an active relationship with its source", async () => {
  const { writes } = mockBenefits({ dependents: [] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  expect(await within(section).findByText("No dependents recorded")).toBeInTheDocument();
  await user.click(within(section).getByRole("button", { name: "Add dependent" }));
  const form = within(section).getByRole("form", { name: "Add dependent" });
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));
  expect(await within(form).findByText("Enter a name.")).toBeInTheDocument();
  expect(within(form).getByText("Choose a valid record.")).toBeInTheDocument();
  expect(within(form).getByText("Use a valid date.")).toBeInTheDocument();
  expect(within(form).getByText("Say where this comes from.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);

  await user.click(within(form).getByRole("combobox", { name: "Relationship" }));
  expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
    "Synthetic Spouse",
    "Synthetic Child",
  ]);
  await user.click(screen.getByRole("option", { name: "Synthetic Child" }));
  await user.type(within(form).getByLabelText("Full name"), " Synthetic Kid ");
  await user.type(within(form).getByLabelText("Birth date"), "2020-01-01");
  await user.type(within(form).getByLabelText("Phone"), "0400000001");
  await user.type(within(form).getByLabelText("Where this comes from"), "Synthetic certificate");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));
  expect(
    await within(form).findByText("Use an international number, for example +61400000001."),
  ).toBeInTheDocument();
  await user.clear(within(form).getByLabelText("Phone"));
  await user.type(within(form).getByLabelText("Phone"), "+61400000001");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));

  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.url).toMatch(new RegExp(`${dependents}/[0-9a-f-]{36}$`));
  expect(writes[0]!.body).toEqual({
    expected_version: 0,
    full_name: "Synthetic Kid",
    relationship_revision_id: childRevision,
    birth_date: "2020-01-01",
    phone: "+61400000001",
    status: "active",
    reason: "Synthetic certificate",
  });
  expect(await screen.findByText("Dependent saved")).toBeInTheDocument();
  expect(within(section).queryByRole("form", { name: "Add dependent" })).toBeNull();
});

it("replays a failed add under the same dependent id", async () => {
  const { writes } = mockBenefits({
    dependents: [],
    replies: [{ status: 503, body: { error: "unavailable", message: "Try again shortly." } }],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  await user.click(await within(section).findByRole("button", { name: "Add dependent" }));
  const form = within(section).getByRole("form", { name: "Add dependent" });
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Relationship" }),
    "Synthetic Spouse",
  );
  await user.type(within(form).getByLabelText("Full name"), "Synthetic Partner");
  await user.type(within(form).getByLabelText("Birth date"), "1990-05-01");
  await user.type(within(form).getByLabelText("Where this comes from"), "Synthetic form");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));
  expect(await within(form).findByText(/Try again shortly\./)).toBeInTheDocument();
  expect(within(form).getByLabelText("Full name")).toHaveValue("Synthetic Partner");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]!.url).toBe(writes[0]!.url);
  expect(writes[1]!.body).toEqual(writes[0]!.body);
});

it("edits a dependent on its recorded relationship even after the master was renamed", async () => {
  const { writes } = mockBenefits({
    dependents: [{ ...spouse, relationship_revision_id: recordedSpouseRevision }],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  await user.click(await within(section).findByRole("button", { name: "Edit Synthetic Partner" }));
  const form = within(section).getByRole("form", { name: "Edit Synthetic Partner" });
  expect(within(form).getByLabelText("Full name")).toHaveValue("Synthetic Partner");
  await user.click(within(form).getByRole("combobox", { name: "Relationship" }));
  expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
    "Synthetic Spouse",
    "Synthetic Child",
  ]);
  await user.keyboard("{Escape}");
  await user.clear(within(form).getByLabelText("Full name"));
  await user.type(within(form).getByLabelText("Full name"), "Synthetic Partner Renamed");
  await user.type(within(form).getByLabelText("Where this comes from"), "Synthetic deed poll");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));

  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.url).toMatch(new RegExp(`${dependents}/${spouse.id}$`));
  expect(writes[0]!.body).toEqual({
    expected_version: 1,
    full_name: "Synthetic Partner Renamed",
    relationship_revision_id: recordedSpouseRevision,
    birth_date: "1990-05-01",
    phone: "",
    status: "active",
    reason: "Synthetic deed poll",
  });
});

it("confirms before removing a dependent and keeps the input when coverage blocks it", async () => {
  const { writes } = mockBenefits({
    replies: [
      conflict(
        "dependent_covered",
        "End the dependent's coverage before removing them or changing their relationship.",
      ),
    ],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  await user.click(await within(section).findByRole("button", { name: "Edit Synthetic Partner" }));
  const form = within(section).getByRole("form", { name: "Edit Synthetic Partner" });
  await chooseOption(user, within(form).getByRole("combobox", { name: "Status" }), "Removed");
  await user.type(within(form).getByLabelText("Where this comes from"), "Synthetic divorce");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));
  const dialog = await screen.findByRole("dialog", { name: "Remove Synthetic Partner?" });
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Remove dependent" }));

  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toMatchObject({ expected_version: 1, status: "removed" });
  expect(
    await within(form).findByText(/End the dependent's coverage before removing them/),
  ).toBeInTheDocument();
  expect(within(form).getByLabelText("Where this comes from")).toHaveValue("Synthetic divorce");
});

it("restores a removed dependent without asking", async () => {
  const { writes } = mockBenefits({ dependents: [{ ...spouse, status: "removed", version: 2 }] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  expect(await within(section).findByText("Removed")).toBeInTheDocument();
  await user.click(within(section).getByRole("button", { name: "Edit Synthetic Partner" }));
  const form = within(section).getByRole("form", { name: "Edit Synthetic Partner" });
  await chooseOption(user, within(form).getByRole("combobox", { name: "Status" }), "Active");
  await user.type(within(form).getByLabelText("Where this comes from"), "Synthetic error fixed");
  await user.click(within(form).getByRole("button", { name: "Save dependent" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toMatchObject({ expected_version: 2, status: "active" });
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(await screen.findByText("Dependent saved")).toBeInTheDocument();
});

it("offers inactive staff no new dependent but keeps existing ones editable", async () => {
  mockBenefits({ staff: { ...member, employment_status: "inactive" } });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user, "Synthetic Nurse · SYN-1 (inactive)");
  expect(await within(section).findByText("Synthetic Partner")).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: "Add dependent" })).toBeNull();
  expect(within(section).getByRole("button", { name: "Edit Synthetic Partner" })).toBeEnabled();
  expect(
    screen.getByText(
      "Inactive staff take no new dependents or coverage. Existing coverage can still be ended.",
    ),
  ).toBeInTheDocument();
});

it("lists a dependent's revisions with their source and author", async () => {
  mockBenefits({
    history: [
      {
        id: "ffffffff-ffff-4fff-8fff-ffffffffffff",
        dependent_id: spouse.id,
        staff_id: staffID,
        version: 2,
        full_name: "Synthetic Partner",
        relationship_revision_id: spouse.relationship_revision_id,
        relationship_master_id: spouse.relationship_master_id,
        relationship_name: "Synthetic Spouse",
        birth_date: "1990-05-01",
        phone: "",
        status: "removed",
        reason: "Synthetic divorce",
        changed_by_subject: "synthetic-maker",
        created_at: spouse.created_at,
      },
    ],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openDependents(user);
  await user.click(
    await within(section).findByRole("button", { name: "History of Synthetic Partner" }),
  );
  const history = await screen.findByRole("region", { name: "Synthetic Partner history" });
  expect(
    await within(history).findByText("v2 · Removed · Synthetic Partner (Synthetic Spouse)"),
  ).toBeInTheDocument();
  expect(
    within(history).getByText("Synthetic divorce · changed by synthetic-maker"),
  ).toBeInTheDocument();
});
