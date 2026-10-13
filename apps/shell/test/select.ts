import { screen } from "@testing-library/react";
import type userEvent from "@testing-library/user-event";

export async function chooseOption(
  user: ReturnType<typeof userEvent.setup>,
  trigger: HTMLElement,
  label: string | RegExp,
) {
  await user.click(trigger);
  await user.click(screen.getByRole("option", { name: label }));
}
