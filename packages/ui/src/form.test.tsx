import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { Field, useZodForm } from "./form";

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
