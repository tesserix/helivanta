import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { SelectControl } from "./select";

function Picker() {
  const [value, setValue] = useState("A");
  return (
    <>
      <label htmlFor="group">Blood group</label>
      <SelectControl
        id="group"
        value={value}
        onValueChange={setValue}
        options={[
          { value: "", label: "Not recorded" },
          { value: "A", label: "A" },
          { value: "AB", label: "AB" },
        ]}
      />
      <output aria-label="Selected value">{value || "empty"}</output>
    </>
  );
}

it("selects with the keyboard and supports an explicit empty value", async () => {
  const user = userEvent.setup();
  render(<Picker />);
  await user.tab();
  const trigger = screen.getByRole("combobox", { name: "Blood group" });
  expect(trigger).toHaveFocus();
  await user.keyboard("{ArrowDown}");
  expect(screen.getByRole("listbox")).toBeInTheDocument();
  await user.click(screen.getByRole("option", { name: "AB" }));
  expect(trigger).toHaveTextContent("AB");
  expect(trigger).toHaveFocus();
  await user.click(trigger);
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  expect(trigger).toHaveTextContent("AB");
  await user.click(trigger);
  await user.click(screen.getByRole("option", { name: "Not recorded" }));
  expect(screen.getByLabelText("Selected value")).toHaveTextContent("empty");
});

it("keeps a disabled selector closed and exposes its accessible label", async () => {
  const user = userEvent.setup();
  render(
    <SelectControl
      aria-label="Hospital"
      disabled
      value="hospital"
      onValueChange={() => {}}
      options={[{ value: "hospital", label: "A very long hospital name" }]}
    />,
  );
  const trigger = screen.getByRole("combobox", { name: "Hospital" });
  expect(trigger).toBeDisabled();
  await user.click(trigger);
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});

it("carries the tenant theme into a menu rendered outside the workspace", async () => {
  const { DEFAULT_BRANDING } = await import("@helivanta/api");
  const { renderWithProviders } = await import("@helivanta/api/testing");
  const { HmsShell } = await import("./hms-shell");
  const { vi } = await import("vitest");
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.includes("permissions")
              ? { data: [] }
              : { ...DEFAULT_BRANDING, display_name: "Blue Care", theme: "blue" },
          ),
          { status: 200 },
        ),
    ),
  );
  const { user } = renderWithProviders(
    <HmsShell active="/dashboard">
      <Picker />
    </HmsShell>,
  );
  await screen.findByRole("heading", { name: "Blue Care" });
  await user.click(screen.getByRole("combobox", { name: "Blood group" }));
  expect(screen.getByRole("listbox")).toHaveAttribute("data-brand-theme", "blue");
  vi.unstubAllGlobals();
});
