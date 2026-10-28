import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { expect, it } from "vitest";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";
import { SelectControl } from "./select";

// #1255 final review: the floating panel must be announced, keep focus, stay on
// screen and survive a menu opened inside it.
function Harness() {
  const [value, setValue] = useState("day");
  return (
    <Popover>
      <PopoverTrigger>Custom range</PopoverTrigger>
      <PopoverContent aria-label="Custom range">
        <SelectControl
          aria-label="Group by"
          value={value}
          onValueChange={setValue}
          options={[
            { value: "day", label: "Day" },
            { value: "year", label: "Year" },
          ]}
        />
      </PopoverContent>
    </Popover>
  );
}

it("announces the panel as a dialog the trigger controls", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  const trigger = screen.getByRole("button", { name: "Custom range" });
  await user.click(trigger);
  const dialog = screen.getByRole("dialog", { name: "Custom range" });
  expect(trigger).toHaveAttribute("aria-controls", dialog.id);
  expect(trigger).toHaveAttribute("aria-expanded", "true");
});

it("returns focus to the trigger when closed with Escape", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  const trigger = screen.getByRole("button", { name: "Custom range" });
  await user.click(trigger);
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(trigger).toHaveFocus();
});

it("stays within a phone viewport", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  await user.click(screen.getByRole("button", { name: "Custom range" }));
  expect(screen.getByRole("dialog")).toHaveClass("max-w-[calc(100vw-2rem)]");
});

it("stays open while a menu inside it is used", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  await user.click(screen.getByRole("button", { name: "Custom range" }));
  await user.click(screen.getByRole("combobox", { name: "Group by" }));
  await user.click(screen.getByRole("option", { name: "Year" }));
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "Group by" })).toHaveTextContent("Year");
});
