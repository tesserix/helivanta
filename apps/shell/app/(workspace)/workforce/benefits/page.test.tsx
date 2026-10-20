import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import {
  childMaster,
  conflict,
  healthID,
  hospital,
  lifePlan,
  mockBenefits,
  spouseMaster,
  stamp,
  VIEW,
} from "@/test/benefits";
import Benefits from "./page";

afterEach(() => vi.unstubAllGlobals());

const plansPanel = () => screen.findByRole("region", { name: "Benefit plans" });

it("says benefits are confidential and requests none without the view permission", async () => {
  const { reads } = mockBenefits({ permissions: ["workforce.payroll.view"] });
  renderWithProviders(<Benefits />);
  expect(await screen.findByText(/Benefits are confidential/)).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Benefit plans" })).toBeNull();
  expect(reads.filter((url) => /benefit|dependents/.test(url))).toEqual([]);
});

it("shows plans read-only to a reader with what each one covers", async () => {
  mockBenefits({ permissions: [VIEW], access: "reader" });
  renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  expect(await within(panel).findByText("Synthetic Health · SYN-HEALTH")).toBeInTheDocument();
  expect(
    within(panel).getByText("Health · v2 · effective 2026-01-01 with no end date"),
  ).toBeInTheDocument();
  expect(within(panel).getByText("Covers the employee and Synthetic Spouse")).toBeInTheDocument();
  expect(
    within(panel).getByText("Life · v3 · effective 2026-01-01 to 2026-06-30"),
  ).toBeInTheDocument();
  expect(within(panel).getByText("Covers the employee only")).toBeInTheDocument();
  expect(within(panel).getByText("Retired")).toBeInTheDocument();
  expect(within(panel).getByText(/Read-only access/)).toBeInTheDocument();
  expect(within(panel).queryByRole("button", { name: "Add plan" })).toBeNull();
  expect(within(panel).queryByRole("button", { name: /^Edit / })).toBeNull();
});

it("keeps plans read-only where the hospital grants no manager access", async () => {
  mockBenefits({ access: "reader" });
  renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  expect(await within(panel).findByText("Synthetic Health · SYN-HEALTH")).toBeInTheDocument();
  expect(within(panel).queryByRole("button", { name: "Add plan" })).toBeNull();
});

it("explains an empty plan list and offers a retry when plans cannot load", async () => {
  mockBenefits({ plans: [] });
  const first = renderWithProviders(<Benefits />);
  expect(await screen.findByText("No benefit plans yet")).toBeInTheDocument();
  first.unmount();
  mockBenefits({ fail: "plans" });
  renderWithProviders(<Benefits />);
  expect(await screen.findByText("Could not load benefit plans.")).toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: "Try again" }).length).toBeGreaterThan(0);
});

it("refuses an incomplete plan and creates one covering only chosen, active relationships", async () => {
  const { writes } = mockBenefits();
  const { user } = renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  await user.click(await within(panel).findByRole("button", { name: "Add plan" }));
  const form = within(panel).getByRole("form", { name: "Add plan" });
  await user.click(within(form).getByRole("button", { name: "Save plan" }));
  expect(
    await within(form).findByText("Use letters, numbers, underscores or hyphens."),
  ).toBeInTheDocument();
  expect(within(form).getByText("Enter a name.")).toBeInTheDocument();
  expect(within(form).getByText("Use a valid date.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);

  await user.type(within(form).getByLabelText("Code"), " syn-dental ");
  await user.type(within(form).getByLabelText("Name"), "Synthetic Dental");
  await chooseOption(user, within(form).getByRole("combobox", { name: "Kind" }), "Accident");
  await user.type(within(form).getByLabelText("Effective from"), "2026-04-01");
  await user.click(within(form).getByRole("checkbox", { name: "Covers dependents" }));
  expect(within(form).queryByRole("checkbox", { name: "Synthetic Parent" })).toBeNull();
  await user.click(within(form).getByRole("button", { name: "Save plan" }));
  expect(await within(form).findByText("Choose at least one relationship.")).toBeInTheDocument();
  await user.click(within(form).getByRole("checkbox", { name: "Synthetic Child" }));
  await user.click(within(form).getByRole("button", { name: "Save plan" }));

  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.url).toMatch(
    new RegExp(`/workforce/hospitals/${hospital}/hr/benefits/plans/[0-9a-f-]{36}$`),
  );
  expect(writes[0]!.body).toEqual({
    expected_version: 0,
    code: "SYN-DENTAL",
    name: "Synthetic Dental",
    kind: "accident",
    description: "",
    covers_dependents: true,
    covered_relationships: [childMaster],
    effective_from: "2026-04-01",
    status: "active",
  });
  expect(await screen.findByText("Plan saved")).toBeInTheDocument();
  expect(within(panel).queryByRole("form", { name: "Add plan" })).toBeNull();
});

it("replays a failed create under the same plan id and keeps the input", async () => {
  const { writes } = mockBenefits({
    replies: [{ status: 503, body: { error: "unavailable", message: "Try again shortly." } }],
  });
  const { user } = renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  await user.click(await within(panel).findByRole("button", { name: "Add plan" }));
  const form = within(panel).getByRole("form", { name: "Add plan" });
  await user.type(within(form).getByLabelText("Code"), "SYN-EYE");
  await user.type(within(form).getByLabelText("Name"), "Synthetic Eye");
  await user.type(within(form).getByLabelText("Effective from"), "2026-04-01");
  await user.click(within(form).getByRole("button", { name: "Save plan" }));
  expect(
    await within(form).findByText(/Try again shortly\. Your input is preserved\./),
  ).toBeInTheDocument();
  expect(within(form).getByLabelText("Name")).toHaveValue("Synthetic Eye");
  await user.click(within(form).getByRole("button", { name: "Save plan" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]!.url).toBe(writes[0]!.url);
  expect(writes[1]!.body).toEqual(writes[0]!.body);
});

it("edits a plan with its code fixed and confirms before retiring it", async () => {
  const { writes } = mockBenefits();
  const { user } = renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  expect(within(panel).queryByRole("button", { name: "Edit Synthetic Life" })).toBeNull();
  await user.click(await within(panel).findByRole("button", { name: "Edit Synthetic Health" }));
  const form = within(panel).getByRole("form", { name: "Edit Synthetic Health" });
  expect(within(form).getByLabelText("Code")).toBeDisabled();
  expect(within(form).getByRole("checkbox", { name: "Synthetic Spouse" })).toBeChecked();
  await chooseOption(user, within(form).getByRole("combobox", { name: "Status" }), "Retired");
  await user.click(within(form).getByRole("button", { name: "Save plan" }));
  expect(await within(form).findByText("Enter the date the plan ends.")).toBeInTheDocument();
  await user.type(within(form).getByLabelText("Effective until"), "2026-12-31");
  await user.click(within(form).getByRole("button", { name: "Save plan" }));

  const dialog = await screen.findByRole("dialog", { name: "Retire Synthetic Health?" });
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Retire plan" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.url).toMatch(new RegExp(`/hr/benefits/plans/${healthID}$`));
  expect(writes[0]!.body).toMatchObject({
    expected_version: 2,
    code: "SYN-HEALTH",
    covered_relationships: [spouseMaster],
    effective_until: "2026-12-31",
    status: "retired",
  });
  expect(await screen.findByText("Plan retired")).toBeInTheDocument();
});

it("tells the editor to reopen a plan someone else changed", async () => {
  mockBenefits({
    replies: [conflict("version_conflict", "This record changed since you opened it.")],
  });
  const { user } = renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  await user.click(await within(panel).findByRole("button", { name: "Edit Synthetic Health" }));
  const form = within(panel).getByRole("form", { name: "Edit Synthetic Health" });
  await user.clear(within(form).getByLabelText("Name"));
  await user.type(within(form).getByLabelText("Name"), "Synthetic Health Plus");
  await user.click(within(form).getByRole("button", { name: "Save plan" }));
  expect(
    await within(form).findByText(/This plan changed since you opened it\. Close and reopen it/),
  ).toBeInTheDocument();
});

it("lists a plan's revisions with who changed each one", async () => {
  mockBenefits({
    permissions: [VIEW],
    history: [
      {
        ...lifePlan,
        plan_id: lifePlan.id,
        id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
        changed_by_subject: "synthetic-retirer",
        created_at: stamp,
      },
      {
        ...lifePlan,
        plan_id: lifePlan.id,
        id: "ffffffff-ffff-4fff-8fff-ffffffffffff",
        version: 1,
        status: "active",
        effective_until: undefined,
        changed_by_subject: "synthetic-maker",
        created_at: stamp,
      },
    ],
  });
  const { user } = renderWithProviders(<Benefits />);
  const panel = await plansPanel();
  await user.click(await within(panel).findByRole("button", { name: "History of Synthetic Life" }));
  const history = await screen.findByRole("region", { name: "Synthetic Life history" });
  expect(
    await within(history).findByText("v3 · Retired · effective 2026-01-01 to 2026-06-30"),
  ).toBeInTheDocument();
  expect(within(history).getByText(/changed by synthetic-retirer/)).toBeInTheDocument();
  expect(
    within(history).getByText("v1 · Active · effective 2026-01-01 with no end date"),
  ).toBeInTheDocument();
});
