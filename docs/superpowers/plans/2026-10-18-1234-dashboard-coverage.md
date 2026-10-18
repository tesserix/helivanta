# Dashboard coverage execution

Issue [#1234](https://github.com/tesserix/helivanta/issues/1234). [Specification and source matrix](../specs/2026-10-18-1234-dashboard-coverage.md).

1. Reproduce the omitted emergency/maternity/birth groups through the dashboard's public rendered interface.
2. Implement the permission-gated clinical coverage panel and explicit tenant-scope wording without inventing unsupported feeds or counts.
3. Exercise permission denial and view navigation; check source failures and real existing values still behave correctly.
4. Run shell tests, repository lint/typecheck/format, production build and a real local browser walkthrough.
5. Publish the reviewed slice with the stock-export work kept separate. Keep #1234/#573 open and list remaining clinical source work. Obtain specific approval before a production rollout; close only when the full required sources and deployed verification exist.
