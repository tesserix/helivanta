import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import GlobalError from "./global-error";

it("supplies safe standalone document markup before hydration", () => {
  const markup = renderToStaticMarkup(
    <GlobalError error={new Error("synthetic-private-layout-detail")} reset={() => {}} />,
  );
  const document = new DOMParser().parseFromString(markup, "text/html");
  expect(document.documentElement.lang).toBe("en");
  expect(document.body.querySelector("main h1")?.textContent).toBe("Something went wrong");
  expect(document.body.textContent).toContain("Preparing support reference");
  expect(markup).not.toContain("synthetic-private-layout-detail");
});
