import { chromium } from "@playwright/test";
const b = await chromium.launch();
const page = await b.newPage({ viewport: { width: 1440, height: 900 } });
await page.goto("http://localhost:4301/login");
await page.getByLabel("Email").fill("test@hms.dev");
await page.getByLabel("Password").fill("password123");
await page.getByRole("button", { name: "Sign in" }).click();
await page.waitForURL(/\/$/);
await page.waitForLoadState("networkidle");
await page.screenshot({ path: "/tmp/shot-dashboard.png" });
// expand rail
await page.getByRole("button", { name: "Expand zone rail" }).click();
await page.waitForTimeout(400);
await page.screenshot({ path: "/tmp/shot-dashboard-expanded.png" });
// collapse panel
await page.getByRole("button", { name: "Collapse page panel" }).click();
await page.waitForTimeout(400);
await page.screenshot({ path: "/tmp/shot-dashboard-collapsed-panel.png" });
await page.goto("http://localhost:4301/medicore/opd");
await page.waitForLoadState("networkidle");
await page.screenshot({ path: "/tmp/shot-opd.png" });
await page.goto("http://localhost:4301/pharmacy");
await page.waitForLoadState("networkidle");
await page.screenshot({ path: "/tmp/shot-pharmacy.png" });
await page.goto("http://localhost:4301/lab");
await page.waitForLoadState("networkidle");
await page.screenshot({ path: "/tmp/shot-lab.png" });
await b.close();
console.log("done");
