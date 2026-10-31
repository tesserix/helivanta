# Staff template migration history implementation

1. In an isolated source copy, change an existing template permission and prove
   the actual generator currently rewrites the old migration successfully.
2. Introduce the canonical ordered migration catalog and validate/generate all
   SQL/digests before output writes. Preserve current public artifacts exactly.
3. Verify unchanged normal/check runs, rejected historical edits without partial
   writes, duplicate/reordered/unsafe history and additive new-version emission.
4. Run script tests, canonical checks, lint/typecheck/build and migration registry
   checks. Record exact evidence, source revision and remaining full-scope gates.
5. Continue compound scoped permissions and complete HR intake/owner/UI workflows;
   this prerequisite alone activates no new operational access.
