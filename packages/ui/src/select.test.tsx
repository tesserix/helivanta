import { useState } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
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

it("preserves the selected value when the browser emits an empty native form event", async () => {
  const user = userEvent.setup();
  const { container } = render(
    <form>
      <Picker />
    </form>,
  );
  await user.click(screen.getByRole("combobox", { name: "Blood group" }));
  await user.click(screen.getByRole("option", { name: "AB" }));
  const native = container.querySelector("select")!;
  fireEvent.change(native, { target: { value: "" } });
  expect(screen.getByLabelText("Selected value")).toHaveTextContent("AB");
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

it("submits null when a nullable form selector is explicitly cleared", async () => {
  const { FormSelectField } = await import("./select");
  const { useZodForm } = await import("./form");
  const { z } = await import("zod");
  const { vi } = await import("vitest");
  const saved = vi.fn();
  function NullablePicker() {
    const form = useZodForm(z.object({ branch: z.string().nullable() }), { branch: "branch-a" });
    return (
      <form noValidate onSubmit={form.handleSubmit(saved)}>
        <FormSelectField
          control={form.control}
          name="branch"
          id="branch"
          label="Branch"
          emptyValue={null}
          options={[
            { value: "", label: "Standalone" },
            { value: "branch-a", label: "Branch A" },
          ]}
        />
        <button type="submit">Save branch</button>
      </form>
    );
  }
  const user = userEvent.setup();
  render(<NullablePicker />);
  await user.click(screen.getByRole("combobox", { name: "Branch" }));
  await user.click(screen.getByRole("option", { name: "Standalone" }));
  await user.click(screen.getByRole("button", { name: "Save branch" }));
  expect(saved).toHaveBeenCalledWith({ branch: null }, expect.anything());
});
