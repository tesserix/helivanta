package attachments

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

var ErrInfected = errors.New("document malware detection prevents release")
var ErrUnsupportedContent = errors.New("document format is unsupported")
var ErrStorageUnavailable = errors.New("document storage unavailable")
var ErrScannerUnavailable = errors.New("document scanner unavailable")

// VerifiedScan can only be constructed by this package after full validation.
type VerifiedScan struct {
	store       *GCS
	scope       ObjectScope
	source      StoredObject
	contentType string
	verdict     ScanVerdict
	scannedAt   time.Time
}

// Matches binds sealed scan evidence to the owning module's immutable intent.
func (s *VerifiedScan) Matches(generation, size int64, sha256 string) bool {
	return s != nil && s.source.Generation == generation && s.source.Size == size && s.source.SHA256 == sha256
}

func (*VerifiedScan) String() string { return "[private verified scan]" }

// Release contains the verified immutable destination and attributable scan.
type Release struct {
	Object           StoredObject
	SourceGeneration int64
	ContentType      string
	Scanner          ScannerStatus
	ScannedAt        time.Time
}

func (g *GCS) ScanQuarantine(ctx context.Context, scope ObjectScope, generation int64, declared string, scanner *ClamAV) (*VerifiedScan, error) {
	if scanner == nil {
		return nil, ErrScannerUnavailable
	}
	content, err := g.ReadQuarantine(ctx, scope, generation)
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	typ, err := InspectContent(content.Bytes, declared)
	if err != nil {
		return nil, ErrUnsupportedContent
	}
	verdict, err := scanner.Scan(ctx, bytes.NewReader(content.Bytes))
	if err != nil {
		return nil, ErrScannerUnavailable
	}
	// Only this constructor can mint promotion evidence; non-clean stays blocked.
	if !verdict.Clean {
		return nil, ErrInfected
	}
	return &VerifiedScan{store: g, scope: scope, source: StoredObject{Generation: generation, SHA256: content.SHA256, Size: content.Size, CRC32C: content.CRC32C}, contentType: typ, verdict: verdict, scannedAt: time.Now().UTC()}, nil
}

func (g *GCS) Promote(ctx context.Context, scan *VerifiedScan) (Release, error) {
	if scan == nil || scan.store != g || !scan.verdict.Clean || scan.source.Generation < 1 {
		return Release{}, errors.New("document promotion requires verified scan evidence")
	}
	key, err := ObjectKey(scan.scope)
	if err != nil {
		return Release{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	source := g.client.Bucket(g.quarantine).Object(key).Generation(scan.source.Generation).If(storage.Conditions{GenerationMatch: scan.source.Generation})
	destination := g.client.Bucket(g.released).Object(key)
	copy := destination.If(storage.Conditions{DoesNotExist: true}).CopierFrom(source)
	copy.ContentType = scan.contentType
	copy.CacheControl = "private, no-store"
	copy.Metadata = map[string]string{"source_generation": strconv.FormatInt(scan.source.Generation, 10), "sha256": scan.source.SHA256}
	attrs, err := copy.Run(ctx)
	if err != nil {
		var apiErr *googleapi.Error
		if !errors.As(err, &apiErr) || apiErr.Code != 412 {
			return Release{}, errors.New("document promotion unavailable")
		}
		// A copy may have committed before the worker crashed; verify, never overwrite.
		attrs, err = destination.Attrs(ctx)
		if err != nil {
			return Release{}, errors.New("document promotion reconciliation unavailable")
		}
	}
	if attrs == nil || attrs.Name != key || attrs.Bucket != g.released || attrs.Generation < 1 || attrs.Size != scan.source.Size || attrs.CRC32C != scan.source.CRC32C || attrs.ContentType != scan.contentType || attrs.ContentEncoding != "" || attrs.Metadata["source_generation"] != strconv.FormatInt(scan.source.Generation, 10) || attrs.Metadata["sha256"] != scan.source.SHA256 {
		return Release{}, errors.New("document promotion receipt conflicts with verified scan")
	}
	content, err := g.read(ctx, g.released, scan.scope, attrs.Generation)
	if err != nil || content.SHA256 != scan.source.SHA256 || content.Size != scan.source.Size || content.CRC32C != scan.source.CRC32C {
		return Release{}, errors.New("document promotion integrity failed")
	}
	return Release{Object: StoredObject{Generation: attrs.Generation, SHA256: content.SHA256, Size: content.Size, CRC32C: content.CRC32C}, SourceGeneration: scan.source.Generation, ContentType: scan.contentType, Scanner: scan.verdict.ScannerStatus, ScannedAt: scan.scannedAt}, nil
}
