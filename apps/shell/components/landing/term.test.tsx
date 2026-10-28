import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { StatusGuide, Term, statusMeaning } from "./term";

it("reads a term inline and opens its definition on click", async () => {
  const user = userEvent.setup();
  renderWithProviders(
    <p>
      Held in <Term definition="Held until a pharmacist releases it.">quarantine</Term>.
    </p>,
  );
  expect(screen.getByText(/Held in/)).toHaveTextContent("Held in quarantine.");
  expect(screen.queryByText("Held until a pharmacist releases it.")).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "quarantine" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("Held until a pharmacist releases it.");
});

it("opens from the keyboard, closes on Escape and returns focus", async () => {
  const user = userEvent.setup();
  renderWithProviders(<Term definition="A definition.">term</Term>);
  const trigger = screen.getByRole("button", { name: "term" });

  await user.tab();
  expect(trigger).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(screen.getByRole("dialog")).toHaveTextContent("A definition.");

  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();
});

it("explains every availability label", async () => {
  const user = userEvent.setup();
  renderWithProviders(<StatusGuide />);
  await user.click(screen.getByRole("button", { name: "What the availability labels mean" }));
  const guide = screen.getByRole("dialog");
  for (const [status, meaning] of Object.entries(statusMeaning)) {
    expect(guide).toHaveTextContent(status);
    expect(guide).toHaveTextContent(meaning);
  }
});
