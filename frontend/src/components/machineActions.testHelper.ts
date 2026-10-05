import { fireEvent, screen } from "@testing-library/react";

/** Opens machine row `row`'s name menu (closing any other row's) and answers
 * its item with `testId`. */
export async function machineAction(testId: string, row = 0): Promise<HTMLButtonElement> {
  const triggers = await screen.findAllByTestId("mon-machine-menu");
  triggers.forEach((trigger, i) => {
    if (i !== row && trigger.getAttribute("aria-expanded") === "true") fireEvent.click(trigger);
  });
  if (triggers[row].getAttribute("aria-expanded") !== "true") fireEvent.click(triggers[row]);
  return (await screen.findByTestId(testId)) as HTMLButtonElement;
}
