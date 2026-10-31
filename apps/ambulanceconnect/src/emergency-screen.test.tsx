import { act, render, screen, userEvent, waitFor } from "@testing-library/react-native";
import { Linking } from "react-native";
import { EmergencyScreen } from "./emergency-screen";

afterEach(() => jest.restoreAllMocks());

test("opens the dialler only after the user presses the India call action", async () => {
  const open = jest.spyOn(Linking, "openURL").mockResolvedValue(undefined);
  const user = userEvent.setup();
  await render(<EmergencyScreen />);
  expect(open).not.toHaveBeenCalled();
  expect(screen.getByText(/does not book an ambulance/)).toBeOnTheScreen();
  await user.press(screen.getByRole("button", { name: "Open dialler for 112" }));
  await waitFor(() => expect(open).toHaveBeenCalledWith("tel:112"));
  expect(open).toHaveBeenCalledTimes(1);
  expect(screen.queryByText("Help is on the way")).toBeNull();
  expect(screen.queryByText("Ambulance confirmed")).toBeNull();
});

test("offers manual calling when no dialler is available without claiming success", async () => {
  jest.spyOn(Linking, "openURL").mockRejectedValue(new Error("not supported"));
  const user = userEvent.setup();
  await render(<EmergencyScreen />);
  await user.press(screen.getByRole("button", { name: "Open dialler for 112" }));
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/dial manually: 112/));
  expect(screen.getByRole("button", { name: "Open dialler for 112" })).toBeEnabled();
});

test("removes India call actions outside India and clears a previous dialler failure", async () => {
  jest.spyOn(Linking, "openURL").mockRejectedValue(new Error("not supported"));
  const user = userEvent.setup();
  await render(<EmergencyScreen />);
  await user.press(screen.getByRole("button", { name: "Open dialler for 112" }));
  await waitFor(() => expect(screen.getByRole("alert")).toBeOnTheScreen());
  await user.press(screen.getByRole("button", { name: "Outside India" }));
  expect(screen.queryByRole("button", { name: "Open dialler for 112" })).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByText(/contact the local emergency service/)).toBeOnTheScreen();
  expect(screen.queryByText("108")).toBeNull();
  await user.press(screen.getByRole("button", { name: "India" }));
  expect(screen.getByRole("button", { name: "Open dialler for 112" })).toBeOnTheScreen();
});

test("a pending dialler launch is not opened twice", async () => {
  let finish!: () => void;
  const open = jest.spyOn(Linking, "openURL").mockReturnValue(
    new Promise<void>((resolve) => {
      finish = resolve;
    }),
  );
  const user = userEvent.setup();
  await render(<EmergencyScreen />);
  await user.press(screen.getByRole("button", { name: "Open dialler for 112" }));
  await user.press(screen.getByRole("button", { name: "Open dialler for 112" }));
  expect(open).toHaveBeenCalledTimes(1);
  await act(async () => finish());
  expect(screen.getByRole("button", { name: "Open dialler for 112" })).toBeEnabled();
});

test("an old dialler failure does not reappear after the country changes", async () => {
  let fail!: (reason: Error) => void;
  jest.spyOn(Linking, "openURL").mockReturnValue(
    new Promise<void>((_, reject) => {
      fail = reject;
    }),
  );
  const user = userEvent.setup();
  await render(<EmergencyScreen />);
  await user.press(screen.getByRole("button", { name: "Open dialler for 112" }));
  await user.press(screen.getByRole("button", { name: "Outside India" }));
  await user.press(screen.getByRole("button", { name: "India" }));
  await act(async () => fail(new Error("old launch failed")));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("button", { name: "Open dialler for 112" })).toBeEnabled();
});
