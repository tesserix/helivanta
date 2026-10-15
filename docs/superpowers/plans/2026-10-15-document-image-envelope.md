# Staff-document image envelope implementation

Issue: #1105. Design: ../specs/2026-10-15-document-image-envelope.md.

1. Add failing tests using actual complete PNG/JPEG bytes with opaque/private,
   compressed and binary metadata; include valid passive metadata controls.
2. Enforce the supported passive image profile in the shared inspector and give
   a safe actionable upload error. Preserve original bytes and checksums.
3. Verify existing upload and worker paths both call the inspector; run Go lint,
   race tests and coverage plus repository CI. Document exact outcomes.
4. Merge/deploy through the existing release pipeline while acceptance is closed;
   test the actual synthetic UI/API flow and retained audit evidence.
