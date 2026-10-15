package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"io"
	"regexp"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

const MaxFileBytes int64 = 10 << 20

var ErrObjectExists = errors.New("document object already exists")

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// GCS stores only private, scope-derived immutable attachment objects.
type GCS struct {
	client               *storage.Client
	quarantine, released string
}

func NewGCS(client *storage.Client, quarantine, released string) (*GCS, error) {
	if client == nil || !bucketName.MatchString(quarantine) || !bucketName.MatchString(released) || quarantine == released {
		return nil, errors.New("private document storage configuration is invalid")
	}
	return &GCS{client: client, quarantine: quarantine, released: released}, nil
}

// StoredContent is bounded private content and its exact storage provenance.
type StoredContent struct {
	Bytes      []byte `json:"-"`
	Generation int64
	SHA256     string
	Size       int64
	CRC32C     uint32
}

func (StoredContent) String() string { return "[private attachment content]" }

// StoredObject identifies immutable bytes without carrying protected content.
type StoredObject struct {
	Generation int64
	SHA256     string
	Size       int64
	CRC32C     uint32
}

func (g *GCS) PutQuarantine(ctx context.Context, scope ObjectScope, data []byte) (StoredObject, error) {
	key, err := ObjectKey(scope)
	if err != nil || len(data) < 1 || int64(len(data)) > MaxFileBytes {
		return StoredObject{}, errors.New("document upload is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	hash := sha256.Sum256(data)
	crc := crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
	w := g.client.Bucket(g.quarantine).Object(key).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	w.ChunkSize = 0
	w.ContentType = "application/octet-stream"
	w.CacheControl = "private, no-store"
	w.CRC32C = crc
	w.SendCRC32C = true
	if _, err := w.Write(data); err != nil {
		cancel()
		_ = w.Close()
		return StoredObject{}, errors.New("document upload unavailable")
	}
	if err := w.Close(); err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == 412 {
			return StoredObject{}, ErrObjectExists
		}
		return StoredObject{}, errors.New("document upload unavailable")
	}
	attrs := w.Attrs()
	if attrs == nil || attrs.Name != key || attrs.Bucket != g.quarantine || attrs.Generation < 1 || attrs.Size != int64(len(data)) || attrs.CRC32C != crc {
		return StoredObject{}, errors.New("document upload receipt is invalid")
	}
	return StoredObject{Generation: attrs.Generation, SHA256: hex.EncodeToString(hash[:]), Size: attrs.Size, CRC32C: crc}, nil
}

func (g *GCS) ReadQuarantine(ctx context.Context, scope ObjectScope, generation int64) (StoredContent, error) {
	return g.read(ctx, g.quarantine, scope, generation)
}

// ReadReleased returns only bytes from the caller's exact authorized generation.
func (g *GCS) ReadReleased(ctx context.Context, scope ObjectScope, generation int64) (StoredContent, error) {
	return g.read(ctx, g.released, scope, generation)
}

// EnsureQuarantine reconciles a lost create receipt without replacing any bytes.
func (g *GCS) EnsureQuarantine(ctx context.Context, scope ObjectScope, data []byte) (StoredObject, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	receipt, err := g.PutQuarantine(ctx, scope, data)
	if !errors.Is(err, ErrObjectExists) {
		return receipt, err
	}
	key, err := ObjectKey(scope)
	if err != nil {
		return StoredObject{}, err
	}
	attrs, err := g.client.Bucket(g.quarantine).Object(key).Attrs(ctx)
	if err != nil || attrs == nil || attrs.Name != key || attrs.Bucket != g.quarantine || attrs.Generation < 1 {
		return StoredObject{}, errors.New("document upload reconciliation unavailable")
	}
	content, err := g.ReadQuarantine(ctx, scope, attrs.Generation)
	if err != nil {
		return StoredObject{}, err
	}
	hash := sha256.Sum256(data)
	if content.Size != int64(len(data)) || content.SHA256 != hex.EncodeToString(hash[:]) {
		return StoredObject{}, ErrObjectExists
	}
	return StoredObject{Generation: content.Generation, SHA256: content.SHA256, Size: content.Size, CRC32C: content.CRC32C}, nil
}

func (g *GCS) read(ctx context.Context, bucket string, scope ObjectScope, generation int64) (StoredContent, error) {
	key, err := ObjectKey(scope)
	if err != nil || generation < 1 {
		return StoredContent{}, errors.New("document generation scope is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	object := g.client.Bucket(bucket).Object(key).Generation(generation)
	attrs, err := object.Attrs(ctx)
	if err != nil {
		return StoredContent{}, errors.New("document storage metadata unavailable")
	}
	if attrs.Name != key || attrs.Bucket != bucket || attrs.Generation != generation || attrs.ContentEncoding != "" || attrs.Size < 1 || attrs.Size > MaxFileBytes {
		return StoredContent{}, errors.New("document stored metadata is invalid")
	}
	r, err := object.NewReader(ctx)
	if err != nil {
		return StoredContent{}, errors.New("document storage read unavailable")
	}
	defer func() { _ = r.Close() }()
	// Encoded, transcoded or mismatched bytes cannot inherit a generation's verdict.
	if r.Attrs.Generation != generation || r.Attrs.ContentEncoding != "" || r.Attrs.Decompressed || r.Attrs.Size != attrs.Size {
		return StoredContent{}, errors.New("document stored content is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil || int64(len(data)) != attrs.Size || int64(len(data)) > MaxFileBytes || ctx.Err() != nil || crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)) != attrs.CRC32C {
		return StoredContent{}, errors.New("document stored content is incomplete")
	}
	hash := sha256.Sum256(data)
	return StoredContent{Bytes: data, Generation: generation, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data)), CRC32C: attrs.CRC32C}, nil
}
