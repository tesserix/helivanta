# Keep weekly template controls within their weekday

Local Staff Admin QA reproduced Remove second shift failing after duplicate
validation. The functional component test passes: the actual browser hit test
shows the Tuesday fieldset covering the center of Monday's Remove button.
A long hospital-specific area/shift label expands Monday's fieldset beyond its
grid track because fieldsets have an intrinsic minimum width. No roster or Staff
allocation was made.

Give the weekday fieldset `min-w-0` so its grid track controls its width. This is
a true one-line layout correction; use actual browser hit-testing and clicking
as the regression proof, since jsdom does not calculate this layout. Preserve
the existing remove/correct/resubmit workflow with a supplementary functional
test. Keep duplicate/overlap/reference checks and first-slot values unchanged.
No API, permission, schema, dependency or migration changes are required.

Verify the same long labels at the original viewport, that Remove receives the
pointer and removes its slot, first-slot choices persist and the template can
save after correction. Check smaller viewport layout without changing the
user's final viewport, then run all web gates and CI. Broader roster acceptance
remains open.
