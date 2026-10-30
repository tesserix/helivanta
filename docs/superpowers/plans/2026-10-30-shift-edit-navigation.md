# Shift edit navigation delivery

1. Reproduce silent unsaved navigation loss in the local administrator UI.
2. Add failing component tests using the real navigation guard provider.
3. Wire dirty/pending registration and guarded cancel/panel/hospital actions;
   preserve the existing save and focus semantics.
4. Verify the local UI and all web gates, document Shift audit evidence, then
   commit/push and merge after CI. Keep production rollout approval separate.
