import { useState } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { MfaCodeForm } from "./mfa-code-form";

function renderForm(onSubmit = vi.fn()) {
  function Form() {
    const [value, setValue] = useState("");
    return <MfaCodeForm value={value} onValueChange={setValue} onSubmit={onSubmit} />;
  }
  render(<Form />);
  const group = screen.getByRole("group", { name: "Verification code" });
  return { digits: within(group).getAllByRole("textbox"), onSubmit };
}

describe("MfaCodeForm", () => {
  it("advances focus across digits and submits the complete code only once", async () => {
    const user = userEvent.setup();
    const { digits, onSubmit } = renderForm();
    expect(digits).toHaveLength(6);
    expect(digits[0]).toHaveFocus();
    expect(screen.getByRole("button", { name: "Verify" })).toBeDisabled();
    await user.keyboard("12345");
    expect(digits[5]).toHaveFocus();
    expect(onSubmit).not.toHaveBeenCalled();
    await user.keyboard("6");
    expect(digits.map((digit) => (digit as HTMLInputElement).value).join("")).toBe("123456");
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith("123456");
    await user.click(screen.getByRole("button", { name: "Verify" }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("distributes a pasted code including its leading zero", async () => {
    const user = userEvent.setup();
    const { digits, onSubmit } = renderForm();
    await user.paste("012345");
    expect(digits.map((digit) => (digit as HTMLInputElement).value).join("")).toBe("012345");
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith("012345");
  });

  it("supports backspace and arrow navigation without submitting incomplete input", async () => {
    const user = userEvent.setup();
    const { digits, onSubmit } = renderForm();
    await user.keyboard("12{Backspace}");
    expect(digits[1]).toHaveFocus();
    expect(digits[1]).toHaveValue("");
    await user.keyboard("{ArrowLeft}");
    expect(digits[0]).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    expect(digits[1]).toHaveFocus();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("rejects letters and keeps incomplete codes from submitting", async () => {
    const user = userEvent.setup();
    const { digits, onSubmit } = renderForm();
    await user.keyboard("abc12");
    fireEvent.submit(digits[0].closest("form")!);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Verify" })).toBeDisabled();
  });

  it("disables all digits during verification and allows the same code after a cleared refusal", () => {
    const onSubmit = vi.fn();
    const props = { value: "123456", onValueChange: vi.fn(), onSubmit };
    const { rerender } = render(<MfaCodeForm {...props} />);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    rerender(<MfaCodeForm {...props} loading />);
    for (const digit of screen.getAllByRole("textbox")) expect(digit).toBeDisabled();
    rerender(<MfaCodeForm {...props} value="" error="Invalid code. Try again." />);
    expect(screen.getByRole("group", { name: "Verification code" })).toHaveAccessibleDescription(
      /Invalid code/,
    );
    rerender(<MfaCodeForm {...props} />);
    expect(onSubmit).toHaveBeenCalledTimes(2);
  });
});
