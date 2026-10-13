import { createHMSFixtures } from "./lib/hms-fixtures.mjs";

if (process.argv.length !== 2) {
  console.error(
    "This generator does not accept legacy records or arguments. Run: node scripts/hms-fixtures.mjs",
  );
  process.exitCode = 1;
} else {
  process.stdout.write(`${JSON.stringify(createHMSFixtures(), null, 2)}\n`);
}
