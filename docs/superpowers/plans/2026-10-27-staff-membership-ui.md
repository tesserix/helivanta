# Staff membership enrollment plan

1. Add and run a failing real-DB stale-workspace enrollment regression.
2. Reuse expected-tenant verification and canonical generated request/result.
3. Add and run a failing typed-client fixed-role request test; implement it.
4. Add and run failing permission-gate, enrollment and retry component tests;
   implement the Staff access form with shared form/query primitives.
5. Update current API/coverage docs and add browser coverage. Run generators,
   IAM tests with race detection, web lint/typecheck/tests/build and browser suite.
6. Commit, push, merge on passing release checks; use the owning GitOps release
   path. Verify live through Staff Admin and the independent reviewer, retaining
   any user credential/access handoffs and never labelling broader issues done
   from this slice alone.
