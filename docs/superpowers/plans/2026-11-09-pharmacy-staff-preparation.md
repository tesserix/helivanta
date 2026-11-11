# Pharmacy Staff access preparation implementation

1. Reproduce the absent pharmacy preparation and unselectable supplier fulfilment
   templates through the real UI/API client boundary in component tests.
2. Extend the existing preparation form to both owners and the canonical template
   catalogue, retaining immutable recovery and source/account-version checks.
3. Add actionable Staff access preparation guidance to existing record/link views.
4. Cover inactive Staff, stale links, source refusal, pagination, invalid validity,
   wrong scope and lost responses. Run affected/full shell tests, API tests,
   typecheck, lint, formatting and a Node 24 production build; remove build output.
5. Keep contracts/docs aligned. Commit and push the reviewed change, pass CI,
   merge to main, and deploy through the existing protected Kargo release path.
   Verify the deployed UI and retain remaining live acceptance limitations.
