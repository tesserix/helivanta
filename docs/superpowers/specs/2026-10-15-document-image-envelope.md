# Passive staff-document image envelopes

Issue: #1105, within #979. Status: accepted implementation scope.

## Problem and evidence

Live ClamAV 1.5.4 rejected the EICAR fixture, but returned OK for a valid PNG
containing a private ancillary chunk with an encrypted ZIP and for one with an
archive member above scan limits. Enabling heuristic alerts did not make those
embedded payloads inspectable. Antivirus OK alone cannot establish the supported
static-image contract. Upload acceptance remains closed while this is corrected.

## Decision

Keep the existing immutable original-byte checksum and release contract. Admit a
bounded passive PNG/JPEG profile in the shared inspector used both before
quarantine and immediately before scanning. PNG permits structural/rendering
chunks and bounded uncompressed textual metadata only. JPEG permits structural
segments, bounded plain-text comments, validated JFIF image headers and Adobe
headers. Reject unknown/private chunks, compressed metadata, EXIF/ICC/XMP and
other opaque application segments. Validate header sizes and reject embedded
binary payloads rather than guessing at their content. Full image decoding,
dimension/byte bounds and ClamAV scanning still apply.

The API explains how to export a fresh PNG/JPEG copy if metadata is unsupported.
This is an explicit admission restriction: it does not claim universal archive,
steganography or malicious-pixel detection. Existing unsupported originals are
not released by the worker. No authorization, schema, GCS identity, object naming,
checksum, outbox or Temporal contract changes are required.

## Alternatives

Re-encoding on release would remove metadata, but would change the committed
size/checksum contract and original-evidence semantics. Silently accepting opaque
metadata based only on an antivirus OK repeats the observed unsafe assumption.
An archive-signature blacklist is incomplete and can match ordinary pixel bytes.
Supporting EXIF/ICC/XMP requires a separately validated metadata processing
contract; this slice rejects them explicitly.

## Verification and rollout

Reproduce admission of private ZIP chunks and opaque JPEG segments in regression
tests. Exercise safe metadata, malformed lengths, compressed/binary metadata and
complete plain images. Verify both actual upload admission and worker inspection
call sites. Run lint, race tests, coverage and application CI. Deploy through the
existing reusable release/Kargo/Argo path with uploads disabled. Verify synthetic
uploads/download checksum and unsupported-image rejection before activation is
considered complete. Reverting requires leaving acceptance closed until the
replacement has passed these same checks.
