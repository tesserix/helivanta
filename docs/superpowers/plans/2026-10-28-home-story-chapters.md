# Home story chapters plan (#1413)

Spec: `docs/superpowers/specs/2026-10-28-home-story-chapters.md`.

1. **Freeze the content.**
   - Add the D6 inventory test.
   - Generate `landing-content.fixture.json` from the unchanged page and commit
     both.
   - Prove the test fails by deleting one stock register row, then revert.
2. **Container.** Add `homeWidth` and switch `app/page.tsx` to it.
3. **Chapter shell.** Add `StoryChapter` and its test, and wrap the six story
   sections in it with the existing headings and ledes.
4. **Keep the components unchanged.** Stacking was built, measured
   (15,281px against 13,194px today) and dropped (spec D3). The components stay
   as they are.
5. **Acts and chapter 5.** Add the two act headers. Put maintenance and AI under
   chapter 5 as `h3` regions. Adjust only the role-level assertions in
   `landing.test.tsx`.
6. **Chapter bar.** Add `ChapterBar` and its test, and mount it under the
   header.
7. **Verify.**
   - The inventory test is green.
   - The full frontend gate under Node 24 with CI's env:
     `pnpm turbo lint format:check type-check test build`.
   - Before/after screenshots at 1440px, and the `e2e/tests/csp.spec.ts` and
     `smoke.spec.ts` selectors still work.
