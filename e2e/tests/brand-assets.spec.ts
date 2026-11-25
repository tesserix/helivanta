import { test, expect } from "@playwright/test";

// The brand file set (#1835 D6): Next's metadata conventions put the links in
// the head, and every file is fetchable without a session, since a browser
// asks for the manifest and icons without cookies.

test("the home page links the icon set, the manifest and an absolute social image", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.locator('head link[rel="icon"]').first()).toHaveAttribute(
    "href",
    /icon|favicon/,
  );
  await expect(page.locator('head link[rel="apple-touch-icon"]')).toHaveAttribute(
    "href",
    /apple-icon/,
  );
  await expect(page.locator('head link[rel="manifest"]')).toHaveAttribute(
    "href",
    /manifest\.webmanifest/,
  );
  await expect(page.locator('head meta[property="og:image"]').first()).toHaveAttribute(
    "content",
    /^https:\/\/helivanta\.app\/opengraph-image/,
  );
});

test("every brand file loads without a session", async ({ request }) => {
  for (const [path, type] of [
    ["/favicon.ico", /image\//],
    ["/icon.svg", /image\/svg\+xml/],
    ["/apple-icon.png", /image\/png/],
    ["/opengraph-image.png", /image\/png/],
    ["/manifest.webmanifest", /manifest\+json|json/],
    ["/icon-192.png", /image\/png/],
    ["/icon-512.png", /image\/png/],
    ["/maskable-icon-512.png", /image\/png/],
  ] as const) {
    const response = await request.get(path, { maxRedirects: 0 });
    expect(response.status(), path).toBe(200);
    expect(response.headers()["content-type"], path).toMatch(type);
  }
});
