import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import test from "node:test";

const appRequire = createRequire(new URL("../apps/ambulanceconnect/package.json", import.meta.url));
const routerRequire = createRequire(appRequire.resolve("expo-router/package.json"));
const parserPath = routerRequire.resolve("./build/fork/getStateFromPath-forks.js");
const { parseQueryParams } = routerRequire(parserPath);
const { stringifySearchParams } = routerRequire("./build/utils/queryParams.js");
const parser = {
  parse: (query) => parseQueryParams(`/report?${query}`, { name: "report" }),
  stringify: stringifySearchParams,
};

test("Expo's query parser preserves Unicode, arrays, plus signs and malformed escapes", () => {
  assert.deepEqual(
    {
      ...parser.parse(
        "name=%E0%A4%B8%E0%A4%AE%E0%A5%80%E0%A4%B0&tag=a&tag=b&note=care+team&bad=%zz",
      ),
    },
    { name: "समीर", tag: ["a", "b"], note: "care team", bad: "%zz" },
  );
  assert.equal(
    parser.stringify({ name: "समीर", tag: ["a", "b"] }),
    "name=%E0%A4%B8%E0%A4%AE%E0%A5%80%E0%A4%B0&tag=a&tag=b",
  );
});

// GHSA-qr28-fpjm-wq7f's public vector runs in a bounded child process.
test("Expo's actual query parser handles distinct malformed percent runs without blocking", () => {
  const program = `
    const { parseQueryParams } = require(${JSON.stringify(parserPath)});
    const parser = { parse: (query) => parseQueryParams('/report?' + query, { name: 'report' }) };
    let value = '';
    for (let i = 0; i < 32000; i++) {
      value += '%' + Math.floor(i / 256).toString(16).padStart(2, '0') + '%' + (i % 256).toString(16).padStart(2, '0') + 'x';
    }
    const result = parser.parse('report=%E0%A4%B0%E0%A4%BF%E0%A4%AA%E0%A5%8B%E0%A4%B0%E0%A5%8D%E0%A4%9F&value=' + value + '%zz');
    process.stdout.write(JSON.stringify({report:result.report,tail:result.value.slice(-3),keys:Object.keys(result)}));
  `;
  const child = spawnSync(process.execPath, ["-e", program], {
    encoding: "utf8",
    timeout: 5000,
    maxBuffer: 8192,
  });
  assert.equal(
    child.error,
    undefined,
    "malformed query decoding must finish within the isolated five-second budget",
  );
  assert.equal(child.status, 0, child.stderr);
  assert.deepEqual(JSON.parse(child.stdout), {
    report: "रिपोर्ट",
    tail: "%zz",
    keys: ["report", "value"],
  });
});
