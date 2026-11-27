package iam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSecurityKeyRegistryContainsRequiresEveryKey(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	keys := &securityKeys{db: store.db}
	require.NoError(t, keys.Add(ctx, "user-a", "key-1", "helivanta.test"))
	require.NoError(t, keys.Add(ctx, "user-a", "key-2", "helivanta.test"))
	require.NoError(t, keys.Add(ctx, "user-b", "key-3", "helivanta.test"))

	for _, tc := range []struct {
		name    string
		subject string
		ids     []string
		want    bool
	}{
		{"every key registered", "user-a", []string{"key-1", "key-2"}, true},
		{"duplicates count once", "user-a", []string{"key-1", "key-1"}, true},
		{"one key missing", "user-a", []string{"key-1", "key-9"}, false},
		{"another person's key", "user-a", []string{"key-3"}, false},
		{"no keys is never all keys", "user-a", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := keys.Contains(ctx, tc.subject, tc.ids)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSecurityKeyRegistryAddIsIdempotent(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	keys := &securityKeys{db: store.db}
	require.NoError(t, keys.Add(ctx, "user-a", "key-1", "helivanta.test"))
	require.NoError(t, keys.Add(ctx, "user-a", "key-1", "helivanta.test"), "a retried record must not fail on its own earlier success")
	var n int64
	require.NoError(t, store.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Table("iam_security_keys").Where("subject = ? AND key_id = ?", "user-a", "key-1").Count(&n).Error
	}))
	require.Equal(t, int64(1), n)
	require.Error(t, keys.Add(ctx, "", "key-2", "helivanta.test"), "an empty subject must be refused by the schema")
}

func TestLoginAttemptStoresSecurityKeyIDAsNullWhenAbsent(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{AuthRequestID: "with-key", SessionID: "s", SessionToken: "t", Subject: "u", ExpiresAt: time.Now().Add(time.Minute), Enrolling: true, Stage: stageFactor, SecurityKeyID: "key-1"}))
	require.NoError(t, store.Put(ctx, loginAttempt{AuthRequestID: "without-key", SessionID: "s", SessionToken: "t", Subject: "u", ExpiresAt: time.Now().Add(time.Minute), Stage: stageFactor}))
	with, err := store.Get(ctx, "with-key", stageFactor)
	require.NoError(t, err)
	require.Equal(t, "key-1", with.SecurityKeyID)
	bumped, err := store.BumpAndGet(ctx, "with-key")
	require.NoError(t, err)
	require.Equal(t, "key-1", bumped.SecurityKeyID, "the bump's RETURNING must carry the key id")
	var nulls int64
	require.NoError(t, store.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Table("login_attempt").Where("auth_request_id = ? AND security_key_id IS NULL", "without-key").Count(&nulls).Error
	}))
	require.Equal(t, int64(1), nulls)
}
