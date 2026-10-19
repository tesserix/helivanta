import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import {
  EXPORT,
  healthID,
  hospital,
  MANAGE,
  mockBenefits,
  otherHospital,
  VIEW,
} from "../../../test/benefits";
import Benefits from "./page";

afterEach(() => {
  cancelTenantTransition();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

type User = Awaited<ReturnType<typeof renderWithProviders>>["user"];
const coverageFile = `/api/v1/workforce/hospitals/${hospital}/hr/benefits/coverage.csv`;

function downloads() {
  const saved: string[] = [];
  const create = vi.fn(() => "blob:synthetic-coverage");
  const revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: revoke }),
  );
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    saved.push(this.download);
  });
  return { saved, click, create, revoke };
}

const exportForm = () => screen.findByRole("form", { name: "Export coverage" });
const setDate = async (user: User, form: HTMLElement, value: string) => {
  const input = within(form).getByLabelText("Coverage in force on");
  await user.clear(input);
  if (value) await user.type(input, value);
};

it("offers the coverage export only with the export permission", async () => {
  mockBenefits({ permissions: [VIEW, MANAGE] });
  renderWithProviders(<Benefits />);
  expect(
    await within(await screen.findByRole("region", { name: "Benefit plans" })).findByText(
      "Synthetic Health · SYN-HEALTH",
    ),
  ).toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Export coverage" })).toBeNull();
});

it("lets a reader export one plan or all plans and reports the verified row count", async () => {
  const { saved, click, revoke } = downloads();
  const { exports } = mockBenefits({ permissions: [VIEW, EXPORT], access: "reader" });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await setDate(user, form, "2026-10-19");
  await chooseOption(
    user,
    within(form).getByRole("combobox", { name: "Plan" }),
    "Synthetic Health · SYN-HEALTH",
  );
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(await screen.findByText("Coverage exported")).toBeInTheDocument();
  expect(
    within(form).getByText("benefits-coverage-2026-10-19.csv · 3 coverage rows"),
  ).toBeInTheDocument();
  expect(saved).toEqual(["benefits-coverage-2026-10-19.csv"]);
  expect(revoke).toHaveBeenCalledWith("blob:synthetic-coverage");
  await chooseOption(user, within(form).getByRole("combobox", { name: "Plan" }), "All plans");
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  await waitFor(() => expect(click).toHaveBeenCalledTimes(2));
  expect(exports.map((e) => e.url)).toEqual([
    `${coverageFile}?as_of=2026-10-19&plan_id=${healthID}`,
    `${coverageFile}?as_of=2026-10-19`,
  ]);
});

it("reports a single row in the singular", async () => {
  downloads();
  mockBenefits({ permissions: [VIEW, EXPORT], exportRows: 1 });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await setDate(user, form, "2026-10-19");
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(
    await within(form).findByText("benefits-coverage-2026-10-19.csv · 1 coverage row"),
  ).toBeInTheDocument();
});

it("checks the date inline before requesting anything", async () => {
  const { click } = downloads();
  const { exports } = mockBenefits({ permissions: [VIEW, EXPORT] });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await setDate(user, form, "");
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(await within(form).findByText("Choose the date the file describes.")).toBeInTheDocument();
  await setDate(user, form, "1999-12-31");
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(
    await within(form).findByText("Choose a date between 2000-01-01 and 2200-12-31."),
  ).toBeInTheDocument();
  expect(exports).toHaveLength(0);
  expect(click).not.toHaveBeenCalled();
});

it("shows why the server refused the export and downloads nothing", async () => {
  const { click } = downloads();
  mockBenefits({
    permissions: [VIEW, EXPORT],
    exportFailure: {
      status: 413,
      body: {
        error: "benefits_export_too_large",
        message: "The export exceeds 1,000 rows or 8 MiB. Narrow it to one plan.",
      },
    },
  });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent(
    "The export exceeds 1,000 rows or 8 MiB. Narrow it to one plan.",
  );
  expect(click).not.toHaveBeenCalled();
  expect(screen.queryByText("Coverage exported")).toBeNull();
});

it("refuses a response that is not the requested coverage file", async () => {
  const { click } = downloads();
  mockBenefits({ permissions: [VIEW, EXPORT], exportHeaders: { "Content-Type": "text/html" } });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent(
    "The server did not return a valid coverage file. Try again.",
  );
  expect(click).not.toHaveBeenCalled();
});

it("cancels the export when the hospital changes and never downloads the late file", async () => {
  const { click } = downloads();
  let release!: () => void;
  const { exports } = mockBenefits({
    permissions: [VIEW, EXPORT],
    hospitals: [{ id: otherHospital, name: "Synthetic Annex", access_level: "reader" }],
    exportHeld: new Promise<void>((resolve) => (release = resolve)),
  });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  await waitFor(() => expect(exports).toHaveLength(1));
  expect(within(form).getByRole("button", { name: "Exporting…" })).toBeDisabled();
  await chooseOption(user, screen.getByRole("combobox", { name: "Hospital" }), "Synthetic Annex");
  expect(exports[0]!.signal?.aborted).toBe(true);
  await act(async () => release());
  expect(await exportForm()).toBeInTheDocument();
  expect(click).not.toHaveBeenCalled();
  expect(screen.queryByText("Coverage exported")).toBeNull();
});

it("cancels the export when the workspace starts switching tenant", async () => {
  const { click } = downloads();
  let release!: () => void;
  const { exports } = mockBenefits({
    permissions: [VIEW, EXPORT],
    exportHeld: new Promise<void>((resolve) => (release = resolve)),
  });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  await waitFor(() => expect(exports).toHaveLength(1));
  act(() => beginTenantTransition());
  expect(exports[0]!.signal?.aborted).toBe(true);
  await act(async () => release());
  expect(await within(form).findByRole("button", { name: "Export coverage" })).toBeInTheDocument();
  expect(click).not.toHaveBeenCalled();
  expect(within(form).queryByRole("alert")).toBeNull();
  expect(screen.queryByText("Coverage exported")).toBeNull();
});

it("starts no export while the workspace is switching tenant", async () => {
  const { click } = downloads();
  const { exports } = mockBenefits({ permissions: [VIEW, EXPORT] });
  const { user } = renderWithProviders(<Benefits />);
  const form = await exportForm();
  act(() => beginTenantTransition());
  await user.click(within(form).getByRole("button", { name: "Export coverage" }));
  expect(await within(form).findByRole("button", { name: "Export coverage" })).toBeEnabled();
  expect(exports).toHaveLength(0);
  expect(click).not.toHaveBeenCalled();
  expect(within(form).queryByRole("alert")).toBeNull();
  expect(screen.queryByText("Coverage exported")).toBeNull();
});
