package attachments_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/attachments"
	"google.golang.org/api/option"
)

func TestGCSPromotionPinsScannedGenerationAndCannotOverwrite(t *testing.T) {
	for _, kind := range []string{"staff", "organisation", "practitioner"} {
		t.Run(kind, func(t *testing.T) {
			scope := fileScope()
			if kind != "staff" {
				scope.OwnerKind = kind
				scope.HospitalID = uuid.Nil
			}
			key, err := attachments.ObjectKey(scope)
			require.NoError(t, err)
			data := syntheticPNG(t)
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			var copies atomic.Int64
			var conflicting atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					require.Equal(t, "/storage/v1/b/helivanta-test-quarantine/o/"+key+"/rewriteTo/b/helivanta-test-released/o/"+key, r.URL.Path)
					require.Equal(t, "12345", r.URL.Query().Get("sourceGeneration"))
					require.Equal(t, "12345", r.URL.Query().Get("ifSourceGenerationMatch"))
					require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
					if copies.Add(1) > 1 {
						w.WriteHeader(http.StatusPreconditionFailed)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"done": true, "totalBytesRewritten": strconv.Itoa(len(data)), "objectSize": strconv.Itoa(len(data)), "resource": map[string]any{"name": key, "bucket": "helivanta-test-released", "generation": "45678", "size": strconv.Itoa(len(data)), "crc32c": strings.TrimPrefix(crcHeader(string(data)), "crc32c="), "contentType": "image/png", "metadata": map[string]string{"source_generation": "12345", "sha256": hash}}})
					return
				}
				bucket, generation := "helivanta-test-quarantine", "12345"
				if strings.Contains(r.URL.Path, "helivanta-test-released") {
					bucket, generation = "helivanta-test-released", "45678"
				}
				if r.URL.Query().Get("generation") != "" {
					require.Equal(t, generation, r.URL.Query().Get("generation"))
				}
				if r.URL.Query().Get("alt") == "media" {
					w.Header().Set("Content-Type", "image/png")
					w.Header().Set("X-Goog-Hash", crcHeader(string(data)))
					w.Header().Set("X-Goog-Generation", generation)
					_, _ = w.Write(data)
					return
				}
				sourceGeneration := "12345"
				if conflicting.Load() && bucket == "helivanta-test-released" {
					sourceGeneration = "99999"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"name": key, "bucket": bucket, "generation": generation, "size": strconv.Itoa(len(data)), "crc32c": strings.TrimPrefix(crcHeader(string(data)), "crc32c="), "contentType": "image/png", "metadata": map[string]string{"source_generation": sourceGeneration, "sha256": hash}})
			}))
			t.Cleanup(server.Close)
			client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			store, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
			require.NoError(t, err)
			scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
			require.NoError(t, err)
			scan, err := store.ScanQuarantine(t.Context(), scope, 12345, "image/png", scanner)
			require.NoError(t, err)
			release, err := store.Promote(t.Context(), scan)
			require.NoError(t, err)
			require.EqualValues(t, 1, copies.Load())
			require.EqualValues(t, 12345, release.SourceGeneration)
			require.EqualValues(t, 45678, release.Object.Generation)
			require.Equal(t, "image/png", release.ContentType)
			require.Equal(t, "1.5.1", release.Scanner.EngineVersion)
			require.NotZero(t, release.ScannedAt)
			repeated, err := store.Promote(t.Context(), scan)
			require.NoError(t, err)
			require.Equal(t, release, repeated, "replay verifies an existing immutable copy")
			require.EqualValues(t, 2, copies.Load())
			conflicting.Store(true)
			conflict, err := store.Promote(t.Context(), scan)
			require.Error(t, err, "an orphan from a different source cannot be reconciled as this release")
			require.Zero(t, conflict.Object.Generation)
			_, err = store.Promote(t.Context(), nil)
			require.Error(t, err)
			_, err = store.Promote(t.Context(), &attachments.VerifiedScan{})
			require.Error(t, err)
		})
	}
}

func TestScanCannotMintPromotionAuthorityFromRejectedContent(t *testing.T) {
	for _, kind := range []string{"staff", "organisation", "practitioner"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				name, mime, reply string
				scanner           bool
			}{
				{"infected", "image/png", "stream: Eicar-Test-Signature FOUND\x00", true},
				{"incomplete_verdict", "image/png", "stream: OK", true},
				{"unsupported_type", "application/pdf", "stream: OK\x00", true},
				{"scanner_missing", "image/png", "", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					scope := fileScope()
					if kind != "staff" {
						scope.OwnerKind = kind
						scope.HospitalID = uuid.Nil
					}
					key, err := attachments.ObjectKey(scope)
					require.NoError(t, err)
					data := syntheticPNG(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, http.MethodGet, r.Method, "rejection never writes the released bucket")
						require.NotContains(t, r.URL.Path, "helivanta-test-released")
						w.Header().Set("Content-Type", "application/json")
						if r.URL.Query().Get("alt") == "media" {
							w.Header().Set("Content-Type", "image/png")
							w.Header().Set("X-Goog-Hash", crcHeader(string(data)))
							w.Header().Set("X-Goog-Generation", "12345")
							_, _ = w.Write(data)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"bucket": "helivanta-test-quarantine", "name": key, "generation": "12345", "size": strconv.Itoa(len(data)), "crc32c": strings.TrimPrefix(crcHeader(string(data)), "crc32c=")})
					}))
					t.Cleanup(server.Close)
					client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
					require.NoError(t, err)
					t.Cleanup(func() { require.NoError(t, client.Close()) })
					store, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
					require.NoError(t, err)
					var scanner *attachments.ClamAV
					if tc.scanner {
						scanner, err = attachments.NewClamAV(testinfra.ClamAVProtocol(t, tc.reply), time.Second, attachments.MaxFileBytes)
						require.NoError(t, err)
					}
					scan, err := store.ScanQuarantine(t.Context(), scope, 12345, tc.mime, scanner)
					require.Error(t, err)
					require.Nil(t, scan)
					_, err = store.Promote(t.Context(), scan)
					require.Error(t, err)
				})
			}
		})
	}
}
