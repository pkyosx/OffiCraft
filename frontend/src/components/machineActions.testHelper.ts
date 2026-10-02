import { fireEvent, screen } from "@testing-library/react";

/** Opens machine row `row`'s ⚙ operations menu (closing any other row's) and
 * answers its item with `testId`. */
export async function machineAction(testId: string, row = 0): Promise<HTMLButtonElement> {
  const gears = await screen.findAllByTestId("mon-actions-menu");
  gears.forEach((gear, i) => {
    if (i !== row && gear.getAttribute("aria-expanded") === "true") fireEvent.click(gear);
  });
  if (gears[row].getAttribute("aria-expanded") !== "true") fireEvent.click(gears[row]);
  return (await screen.findByTestId(testId)) as HTMLButtonElement;
}
