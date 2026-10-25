package testinfra

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

type protocolObject struct {
	bytes       []byte
	generation  int64
	contentType string
	metadata    map[string]string
}

func (o protocolObject) attrs(bucket, key string) map[string]any {
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], crc32.Checksum(o.bytes, crc32.MakeTable(crc32.Castagnoli)))
	return map[string]any{"bucket": bucket, "name": key, "generation": strconv.FormatInt(o.generation, 10), "size": strconv.Itoa(len(o.bytes)), "crc32c": base64.StdEncoding.EncodeToString(checksum[:]), "contentType": o.contentType, "metadata": o.metadata}
}

// GCSProtocol runs the vendor JSON protocol against isolated immutable objects.
type GCSProtocol struct {
	Client               *storage.Client
	Quarantine, Released string
	mu                   sync.Mutex
	objects              map[string]protocolObject
	nextGeneration       int64
	copies               int
	afterCopy            func()
	afterQuarantineWrite func()
	afterReleasedRead    func()
	denyReads            bool
}

func NewGCSProtocol(t *testing.T) *GCSProtocol {
	t.Helper()
	p := &GCSProtocol{Quarantine: "helivanta-test-quarantine", Released: "helivanta-test-released", objects: make(map[string]protocolObject), nextGeneration: 10000}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serve(t, w, r) }))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	p.Client = client
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return p
}

func (p *GCSProtocol) CopyCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.copies }
func (p *GCSProtocol) AfterQuarantineWrite(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterQuarantineWrite = fn
}
func (p *GCSProtocol) AfterCopy(fn func()) { p.mu.Lock(); defer p.mu.Unlock(); p.afterCopy = fn }
func (p *GCSProtocol) AfterReleasedRead(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterReleasedRead = fn
}
func (p *GCSProtocol) DenyReads(deny bool) { p.mu.Lock(); defer p.mu.Unlock(); p.denyReads = deny }
func (p *GCSProtocol) CorruptQuarantine(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, object := range p.objects {
		if strings.HasPrefix(key, p.Quarantine+"/") {
			object.bytes = append([]byte(nil), data...)
			p.objects[key] = object
		}
	}
}

func (p *GCSProtocol) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(r.URL.Path, "/rewriteTo/") {
		source, destination, ok := strings.Cut(r.URL.Path, "/rewriteTo/b/")
		require.True(t, ok)
		key := strings.TrimPrefix(source, "/storage/v1/b/"+p.Quarantine+"/o/")
		require.Equal(t, p.Released+"/o/"+key, destination)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
		p.mu.Lock()
		object, exists := p.objects[p.Quarantine+"/"+key]
		require.True(t, exists)
		require.Equal(t, strconv.FormatInt(object.generation, 10), r.URL.Query().Get("sourceGeneration"))
		require.Equal(t, strconv.FormatInt(object.generation, 10), r.URL.Query().Get("ifSourceGenerationMatch"))
		if _, exists := p.objects[p.Released+"/"+key]; exists {
			p.mu.Unlock()
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		var attrs struct {
			ContentType  string            `json:"contentType"`
			CacheControl string            `json:"cacheControl"`
			Metadata     map[string]string `json:"metadata"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&attrs))
		require.Equal(t, "private, no-store", attrs.CacheControl)
		sum := sha256.Sum256(object.bytes)
		require.Equal(t, hex.EncodeToString(sum[:]), attrs.Metadata["sha256"])
		require.Equal(t, strconv.FormatInt(object.generation, 10), attrs.Metadata["source_generation"])
		p.nextGeneration++
		object.generation = p.nextGeneration
		object.contentType = attrs.ContentType
		object.metadata = attrs.Metadata
		p.objects[p.Released+"/"+key] = object
		p.copies++
		hook := p.afterCopy
		p.mu.Unlock()
		if hook != nil {
			hook()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"done": true, "totalBytesRewritten": strconv.Itoa(len(object.bytes)), "objectSize": strconv.Itoa(len(object.bytes)), "resource": object.attrs(p.Released, key)})
		return
	}
	if r.Method == http.MethodPost {
		require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		parts := multipart.NewReader(r.Body, params["boundary"])
		part, err := parts.NextPart()
		require.NoError(t, err)
		var attrs struct {
			Name         string `json:"name"`
			CacheControl string `json:"cacheControl"`
		}
		require.NoError(t, json.NewDecoder(part).Decode(&attrs))
		require.Equal(t, "private, no-store", attrs.CacheControl)
		part, err = parts.NextPart()
		require.NoError(t, err)
		data, err := io.ReadAll(io.LimitReader(part, 10<<20+1))
		require.NoError(t, err)
		key := p.Quarantine + "/" + attrs.Name
		p.mu.Lock()
		if _, exists := p.objects[key]; exists {
			p.mu.Unlock()
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		p.nextGeneration++
		object := protocolObject{bytes: data, generation: p.nextGeneration}
		p.objects[key] = object
		hook := p.afterQuarantineWrite
		p.mu.Unlock()
		if hook != nil {
			hook()
		}
		_ = json.NewEncoder(w).Encode(object.attrs(p.Quarantine, attrs.Name))
		return
	}
	bucket, key, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/storage/v1/b/"), "/o/")
	require.True(t, ok)
	p.mu.Lock()
	object, exists := p.objects[bucket+"/"+key]
	deny := p.denyReads
	hook := p.afterReleasedRead
	if bucket == p.Released && r.URL.Query().Get("alt") == "media" {
		p.afterReleasedRead = nil
	}
	p.mu.Unlock()
	if deny {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if generation := r.URL.Query().Get("generation"); generation != "" {
		require.Equal(t, strconv.FormatInt(object.generation, 10), generation)
	}
	if r.URL.Query().Get("alt") == "media" {
		if bucket == p.Released && hook != nil {
			hook()
		}
		w.Header().Set("X-Goog-Generation", strconv.FormatInt(object.generation, 10))
		w.Header().Set("X-Goog-Metageneration", "1")
		w.Header().Set("X-Goog-Hash", "crc32c="+object.attrs(bucket, key)["crc32c"].(string))
		_, _ = w.Write(object.bytes)
		return
	}
	_ = json.NewEncoder(w).Encode(object.attrs(bucket, key))
}
