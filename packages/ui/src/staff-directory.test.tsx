import { ApiError } from "@helivanta/api";
import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { Staff } from "@helivanta/api";
import { StaffDirectory } from "./staff-directory";

const staff: Staff = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  hospital_id: "11111111-1111-4111-8111-111111111111",
  employee_code: "SYN-PHARM-001",
  display_name: "Synthetic Pharmacist",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: "2026-11-02T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
};
const props = {
  title: "Synthetic hospital",
  description: "Read-only Staff directory",
  emptyDescription: "No Staff records have been added.",
  scopeDescription: "Showing data from this hospital only",
  rows: [staff],
  isPending: false,
  isError: false,
  error: null,
  onRetry: vi.fn(),
};

it("renders canonical Staff without inventing product authority or management actions", () => {
  render(<StaffDirectory {...props} />);
  expect(screen.getByRole("table", { name: "Staff at Synthetic hospital" })).toBeInTheDocument();
  expect(screen.getByRole("cell", { name: "Synthetic Pharmacist" })).toBeInTheDocument();
  expect(screen.getByRole("cell", { name: "SYN-PHARM-001" })).toBeInTheDocument();
  expect(screen.getByRole("cell", { name: "Pharmacist" })).toBeInTheDocument();
  expect(screen.getByRole("cell", { name: "Active" })).toBeInTheDocument();
  expect(screen.queryByRole("columnheader", { name: "Actions" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(screen.queryByText(/verified|dispensing permitted/i)).not.toBeInTheDocument();
});

it("filters only the supplied page and preserves continuation when no row matches", async () => {
  const user = (await import("@testing-library/user-event")).default.setup();
  const next = vi.fn();
  render(<StaffDirectory {...props} onNextPage={next} />);
  await user.type(screen.getByRole("textbox", { name: "Filter this page" }), "another page");
  expect(screen.getByText("No matching records on this page")).toBeInTheDocument();
  expect(screen.getByText("0 records on this page")).toBeInTheDocument();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Next page" }));
  expect(next).toHaveBeenCalledOnce();
  await user.clear(screen.getByRole("textbox", { name: "Filter this page" }));
  await user.type(screen.getByRole("textbox", { name: "Filter this page" }), "pharm-001");
  expect(screen.getByRole("cell", { name: "Synthetic Pharmacist" })).toBeInTheDocument();
  await user.clear(screen.getByRole("textbox", { name: "Filter this page" }));
  await user.type(screen.getByRole("textbox", { name: "Filter this page" }), "PHARMACIST");
  expect(screen.getByText("1 record on this page")).toBeInTheDocument();
});

it("lets a consumer supply actions for the exact canonical record without copying its profile", async () => {
  const user = (await import("@testing-library/user-event")).default.setup();
  const choose = vi.fn();
  render(
    <StaffDirectory
      {...props}
      headerActions={<button>Add staff</button>}
      rowActions={(row) => <button onClick={() => choose(row)}>Review {row.employee_code}</button>}
    />,
  );
  expect(screen.getByRole("columnheader", { name: "Actions" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Review SYN-PHARM-001" }));
  expect(choose).toHaveBeenCalledExactlyOnceWith(staff);
  expect(screen.getByRole("button", { name: "Add staff" })).toBeInTheDocument();
});

it.each([
  { isPending: true, isError: false },
  { isPending: false, isError: true },
  { isPending: true, isError: true },
])("hides stale rows and their actions while the current page is unverified: %j", (state) => {
  render(
    <StaffDirectory
      {...props}
      {...state}
      headerActions={<button>Add staff</button>}
      rowActions={() => <button>Review private record</button>}
      onFirstPage={vi.fn()}
      onNextPage={vi.fn()}
    />,
  );
  expect(screen.queryByText("Synthetic Pharmacist")).not.toBeInTheDocument();
  expect(screen.queryByText("SYN-PHARM-001")).not.toBeInTheDocument();
  expect(screen.queryByText("Your staff directory starts here")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Review private record" })).not.toBeInTheDocument();
  expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  expect(screen.queryByText(/records? on this page/)).not.toBeInTheDocument();
  if (state.isPending) expect(screen.getByRole("status")).toHaveTextContent("Loading staff");
  else expect(screen.getByRole("alert")).toHaveTextContent("Could not load staff");
});

it("retries a failed page without showing failure as an empty directory", async () => {
  const user = (await import("@testing-library/user-event")).default.setup();
  const retry = vi.fn();
  render(<StaffDirectory {...props} isError onRetry={retry} rows={[]} />);
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(retry).toHaveBeenCalledOnce();
  expect(screen.queryByText("Your staff directory starts here")).not.toBeInTheDocument();
});

it("renders a genuine empty page and follows caller-owned first-page navigation", async () => {
  const user = (await import("@testing-library/user-event")).default.setup();
  const first = vi.fn();
  render(<StaffDirectory {...props} rows={[]} onFirstPage={first} />);
  expect(screen.getByText("Your staff directory starts here")).toBeInTheDocument();
  expect(screen.getByText(props.emptyDescription)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "First page" }));
  expect(first).toHaveBeenCalledOnce();
  expect(screen.queryByRole("button", { name: "Next page" })).not.toBeInTheDocument();
});

it("renders inactive Staff as inactive and escapes the canonical display name", () => {
  render(
    <StaffDirectory
      {...props}
      rows={[
        { ...staff, display_name: "<img src=x onerror=alert(1)>", employment_status: "inactive" },
      ]}
    />,
  );
  expect(screen.getByRole("cell", { name: "Inactive" })).toBeInTheDocument();
  expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
});

it("shows a refused directory as a refusal with no retry", () => {
  render(
    <StaffDirectory {...props} isError error={new ApiError("forbidden", "no", 403)} rows={[]} />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent(
    "This wasn't found, or you don't have access to it.",
  );
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});
