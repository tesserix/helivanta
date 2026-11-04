import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyProduct } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { ItemCategories, ProductCategorySection } from "./item-categories";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};

const antibiotics = {
  id: "0199e000-0000-7000-8000-000000000501",
  parent_id: null,
  code: "ANTI",
  name: "Antibiotics",
  retired_at: null,
  created_by: "user-editor",
  created_at: "2026-11-01T05:00:00Z",
};
const oral = {
  ...antibiotics,
  id: "0199e000-0000-7000-8000-000000000502",
  parent_id: antibiotics.id,
  code: "ORAL",
  name: "Oral",
};
const analgesics = {
  ...antibiotics,
  id: "0199e000-0000-7000-8000-000000000503",
  code: "ANAL",
  name: "Analgesics",
  retired_at: "2026-11-02T05:00:00Z",
};

const product = {
  id: "0199e000-0000-7000-8000-000000000101",
  sku: "TEST-AMOX-500",
  brand_name: "TEST Amoxil",
  status: "active",
} as PharmacyProduct;

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function serve(options: { categories?: unknown[] | "fail"; productCategory?: unknown } = {}) {
  const fetcher = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    async (url, init) => {
      if (init?.method === "PUT" || init?.method === "POST")
        return new Response(JSON.stringify({ id: "x" }), { status: 201 });
      if (url.includes("/category"))
        return new Response(
          JSON.stringify(options.productCategory ?? { in_force: null, history: [] }),
        );
      return options.categories === "fail"
        ? new Response(JSON.stringify({ error: "internal", message: "no" }), { status: 500 })
        : new Response(
            JSON.stringify({
              data: options.categories ?? [antibiotics, oral, analgesics],
              page: { next_cursor: null, has_more: false },
            }),
          );
    },
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const writes = (fetcher: ReturnType<typeof serve>) =>
  fetcher.mock.calls.filter(([, init]) => init?.method === "PUT" || init?.method === "POST");

describe("ItemCategories", () => {
  it("lists categories with their subcategories and marks a retired one", async () => {
    serve();
    renderWithProviders(<ItemCategories scope={scope} canEdit={false} />);
    const list = await screen.findByRole("list", { name: "Item categories" });
    const [first, second] = within(list).getAllByRole("listitem");
    expect(first).toHaveTextContent("Antibiotics ANTI");
    expect(first).toHaveTextContent("Oral · ORAL");
    expect(second).toHaveTextContent("Analgesics ANAL · retired");
    expect(screen.queryByRole("button", { name: "Add category" })).toBeNull();
  });

  it("adds a subcategory with its code in capitals, under a category in use", async () => {
    const fetcher = serve();
    const { user } = renderWithProviders(<ItemCategories scope={scope} canEdit />);
    await screen.findByRole("list", { name: "Item categories" });
    await user.type(screen.getByLabelText("Code"), "inj");
    await user.type(screen.getByLabelText("Name"), "Injections");
    const under = screen.getByRole("combobox", { name: "Under" });
    await user.click(under);
    expect(screen.queryByRole("option", { name: /Analgesics/ })).toBeNull();
    await user.keyboard("{Escape}");
    await chooseOption(user, under, "A subcategory of Antibiotics");
    await user.click(screen.getByRole("button", { name: "Add category" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    const [url, init] = writes(fetcher)[0]!;
    expect(url).toMatch(/\/catalogue\/categories\/[0-9a-f-]{36}$/);
    expect(JSON.parse(String(init?.body))).toEqual({
      code: "INJ",
      name: "Injections",
      parent_id: antibiotics.id,
    });
  });

  it("refuses a code that is not a code, and reports a failed load", async () => {
    const fetcher = serve();
    const { user, unmount } = renderWithProviders(<ItemCategories scope={scope} canEdit />);
    await screen.findByRole("list", { name: "Item categories" });
    await user.type(screen.getByLabelText("Code"), "two words");
    await user.type(screen.getByLabelText("Name"), "X");
    await user.click(screen.getByRole("button", { name: "Add category" }));
    expect(await screen.findByText("Up to 20 capital letters, digits, - or _")).toBeVisible();
    expect(writes(fetcher)).toHaveLength(0);
    unmount();
    serve({ categories: "fail" });
    renderWithProviders(<ItemCategories scope={scope} canEdit />);
    expect(await screen.findByText("Could not load the categories.")).toBeVisible();
  });
});

describe("ProductCategorySection", () => {
  const pending = {
    id: "0199e000-0000-7000-8000-000000000601",
    category_id: antibiotics.id,
    subcategory_id: oral.id,
    effective_from: "2026-11-04",
    reason: "TEST filed with the antibiotics",
    declared_by: "user-editor",
    created_at: "2026-11-04T05:00:00Z",
    decision: null,
    decision_reason: null,
    reviewed_by: null,
    in_force_from: null,
  };

  it("shows the category in force and its history, and reviews the pending one", async () => {
    const fetcher = serve({
      productCategory: {
        in_force: {
          declaration_id: "d0",
          category_id: antibiotics.id,
          subcategory_id: null,
          in_force_from: "2026-11-01",
        },
        history: [pending],
      },
    });
    const { user } = renderWithProviders(
      <ProductCategorySection
        scope={scope}
        product={product}
        today="2026-11-04"
        canDeclare={false}
        canReview
      />,
    );
    expect(await screen.findByText("Antibiotics, since 2026-11-01")).toBeVisible();
    const history = screen.getByRole("list", { name: "Category history" });
    expect(history).toHaveTextContent("Antibiotics › Oral");
    expect(history).toHaveTextContent("Awaiting review");
    await user.type(screen.getByLabelText("What you checked"), "checked the pack");
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    const [url, init] = writes(fetcher)[0]!;
    expect(url).toContain(`/category/${pending.id}/review`);
    expect(JSON.parse(String(init?.body))).toEqual({
      decision: "approved",
      reason: "checked the pack",
    });
  });

  it("declares a category from today, never a past date, and offers only live ones", async () => {
    const fetcher = serve();
    const { user } = renderWithProviders(
      <ProductCategorySection
        scope={scope}
        product={product}
        today="2026-11-04"
        canDeclare
        canReview={false}
      />,
    );
    expect(await screen.findByText("No category approved yet")).toBeVisible();
    const category = screen.getByRole("combobox", { name: "Category" });
    await user.click(category);
    expect(screen.queryByRole("option", { name: "Analgesics" })).toBeNull();
    await user.keyboard("{Escape}");
    await chooseOption(user, category, "Antibiotics");
    await chooseOption(user, screen.getByRole("combobox", { name: "Subcategory" }), "Oral");
    await user.type(screen.getByLabelText("Why"), "TEST filed with the antibiotics");
    await user.clear(screen.getByLabelText("In force from"));
    await user.type(screen.getByLabelText("In force from"), "2026-11-03");
    await user.click(screen.getByRole("button", { name: "Declare category" }));
    expect(await screen.findByText("A category takes effect today or later")).toBeVisible();
    expect(writes(fetcher)).toHaveLength(0);
    await user.clear(screen.getByLabelText("In force from"));
    await user.type(screen.getByLabelText("In force from"), "2026-11-04");
    await user.click(screen.getByRole("button", { name: "Declare category" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(writes(fetcher)[0]![1]?.body))).toEqual({
      category_id: antibiotics.id,
      subcategory_id: oral.id,
      effective_from: "2026-11-04",
      reason: "TEST filed with the antibiotics",
    });
  });

  it("drops a subcategory when the category changes", async () => {
    const fetcher = serve({
      categories: [
        antibiotics,
        oral,
        { ...analgesics, retired_at: null, id: "0199e000-0000-7000-8000-000000000504" },
      ],
    });
    const { user } = renderWithProviders(
      <ProductCategorySection
        scope={scope}
        product={product}
        today="2026-11-04"
        canDeclare
        canReview={false}
      />,
    );
    await screen.findByText("No category approved yet");
    await chooseOption(user, screen.getByRole("combobox", { name: "Category" }), "Antibiotics");
    await chooseOption(user, screen.getByRole("combobox", { name: "Subcategory" }), "Oral");
    await chooseOption(user, screen.getByRole("combobox", { name: "Category" }), "Analgesics");
    await user.type(screen.getByLabelText("Why"), "TEST moved to analgesics");
    await user.click(screen.getByRole("button", { name: "Declare category" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(writes(fetcher)[0]![1]?.body)).subcategory_id).toBeUndefined();
  });
});
