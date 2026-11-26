import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { Input } from "@tesserix/web";
import { Field, useZodForm } from "./form";
import { SelectControl } from "./select";

const schema = z.object({ name: z.string().min(1, "Name is required") });

function Probe({ onValid }: { onValid: (v: { name: string }) => void }) {
  const form = useZodForm(schema, { name: "" });
  return (
    <form noValidate onSubmit={form.handleSubmit(onValid)}>
      <Field id="name" label="Name" error={form.formState.errors.name?.message}>
        <input id="name" {...form.register("name")} />
      </Field>
      <button type="submit">Save</button>
    </form>
  );
}

describe("useZodForm + Field", () => {
  it("shows inline zod errors instead of native validation", async () => {
    const user = userEvent.setup();
    const onValid = vi.fn();
    render(<Probe onValid={onValid} />);
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Name is required");
    expect(onValid).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("Name"), "Asha");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onValid).toHaveBeenCalledWith({ name: "Asha" }, expect.anything());
  });
});

// #1855 D1: Field marks its control, so a screen reader returning to the input
// hears why it failed.
describe("Field marks its control", () => {
  const error = "Enter a name";
  function expectMarked(control: HTMLElement, describedBy = "name-error") {
    expect(control).toHaveAttribute("aria-invalid", "true");
    expect(control).toHaveAttribute("aria-describedby", describedBy);
    expect(document.getElementById("name-error")).toHaveTextContent(error);
  }

  it.each([
    ["Input", <Input key="i" id="name" />],
    ["native input", <input key="n" id="name" />],
    ["textarea", <textarea key="t" id="name" />],
  ])("marks a %s while an error is shown", (_, control) => {
    render(
      <Field id="name" label="Name" error={error}>
        {control}
      </Field>,
    );
    expectMarked(screen.getByLabelText("Name"));
  });

  it("marks SelectControl's trigger", () => {
    render(
      <Field id="name" label="Name" error={error}>
        <SelectControl id="name" onValueChange={() => {}} options={[{ value: "a", label: "A" }]} />
      </Field>,
    );
    expectMarked(screen.getByRole("combobox"));
  });

  it("merges the control's own describedby and keeps its aria-invalid", () => {
    render(
      <Field id="name" label="Name" error={error}>
        <input id="name" aria-describedby="name-hint name-error" aria-invalid="grammar" />
      </Field>,
    );
    const control = screen.getByLabelText("Name");
    expect(control).toHaveAttribute("aria-describedby", "name-hint name-error");
    expect(control).toHaveAttribute("aria-invalid", "grammar");
  });

  it("leaves a child that is not the control untouched", () => {
    render(
      <Field id="name" label="Name" error={error}>
        <div data-testid="wrapper">
          <input id="name" />
        </div>
      </Field>,
    );
    for (const node of [screen.getByTestId("wrapper"), screen.getByLabelText("Name")]) {
      expect(node).not.toHaveAttribute("aria-invalid");
      expect(node).not.toHaveAttribute("aria-describedby");
    }
  });

  it("adds nothing without an error", () => {
    render(
      <Field id="name" label="Name">
        <input id="name" aria-describedby="name-hint" />
      </Field>,
    );
    const control = screen.getByLabelText("Name");
    expect(control).not.toHaveAttribute("aria-invalid");
    expect(control).toHaveAttribute("aria-describedby", "name-hint");
  });
});
