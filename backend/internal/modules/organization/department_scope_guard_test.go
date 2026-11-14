package organization_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type scopeTree struct {
	r        *gin.Engine
	db       *tenantdb.DB
	ctx      context.Context
	hospital string
	versions map[uuid.UUID]int
	codes    map[uuid.UUID]string
}

func newScopeTree(t *testing.T) *scopeTree {
	t.Helper()
	r, db, ctx, hospital := departmentHarness(t)
	return &scopeTree{r: r, db: db, ctx: ctx, hospital: hospital, versions: map[uuid.UUID]int{}, codes: map[uuid.UUID]string{}}
}

func (s *scopeTree) put(t *testing.T, id uuid.UUID, code string, parent uuid.UUID, status string) {
	t.Helper()
	kind, parentID := "department", ""
	if parent != uuid.Nil {
		kind, parentID = "sub_unit", parent.String()
	}
	if existing, ok := s.codes[id]; ok {
		code = existing
	}
	response := organizationCall(s.r, "PUT", departmentPath(s.hospital, id.String(), uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, departmentIntent(s.versions[id], code, "Synthetic "+code, kind, parentID, status))
	require.Equal(t, 200, response.Code, response.Body.String())
	s.versions[id]++
	s.codes[id] = code
}

func (s *scopeTree) node(t *testing.T, code string, parent uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	s.put(t, id, code, parent, "active")
	return id
}

func (s *scopeTree) hospitalID() uuid.UUID { return uuid.MustParse(s.hospital) }

func (s *scopeTree) within(t *testing.T, guard authz.DepartmentScopeGuard, target, scope uuid.UUID) error {
	t.Helper()
	called := false
	err := s.db.WithGuardedTenantCommand(s.ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithinCurrentDepartment(commandCtx, testutil.TenantA, s.hospitalID(), target, scope, func(context.Context) error { called = true; return nil })
	})
	require.Equal(t, err == nil, called, "the action runs exactly when containment is current")
	return err
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return out
}

func TestDepartmentScopeGuardAnswersCurrentPathAndSubtree(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-SCOPE-ROOT", uuid.Nil)
	child := s.node(t, "SYN-SCOPE-CHILD", root)
	leaf := s.node(t, "SYN-SCOPE-LEAF", child)
	retired := s.node(t, "SYN-SCOPE-RETIRED", child)
	s.put(t, retired, "", child, "retired")
	sibling := s.node(t, "SYN-SCOPE-SIBLING", uuid.Nil)
	moved := s.node(t, "SYN-SCOPE-MOVED", sibling)
	guard := organization.New().DepartmentScopes(s.db)
	hospital := s.hospitalID()

	path, err := guard.CurrentPath(s.ctx, testutil.TenantA, hospital, leaf)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{leaf, child, root}, path, "node first, root last")
	path, err = guard.CurrentPath(s.ctx, testutil.TenantA, hospital, root)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{root}, path)
	for name, tc := range map[string]struct {
		tenant     string
		hospital   uuid.UUID
		department uuid.UUID
		want       error
	}{
		"retired node":          {testutil.TenantA, hospital, retired, authz.ErrDepartmentReferenceChanged},
		"unknown node":          {testutil.TenantA, hospital, uuid.Must(uuid.NewV7()), authz.ErrDepartmentReferenceHidden},
		"unregistered hospital": {testutil.TenantA, uuid.New(), leaf, authz.ErrDepartmentReferenceHidden},
		"foreign tenant":        {testutil.TenantB, hospital, leaf, authz.ErrDepartmentReferenceHidden},
		"non-v7 node":           {testutil.TenantA, hospital, uuid.New(), authz.ErrScopedAuthorizationUnavailable},
		"missing hospital":      {testutil.TenantA, uuid.Nil, leaf, authz.ErrScopedAuthorizationUnavailable},
		"non-canonical tenant":  {"not-a-tenant", hospital, leaf, authz.ErrScopedAuthorizationUnavailable},
	} {
		t.Run("path "+name, func(t *testing.T) {
			path, err := guard.CurrentPath(s.ctx, tc.tenant, tc.hospital, tc.department)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, path)
		})
	}

	subtree := func(t *testing.T, roots ...uuid.UUID) []uuid.UUID {
		t.Helper()
		ids, err := guard.CurrentSubtree(s.ctx, testutil.TenantA, hospital, roots)
		require.NoError(t, err)
		return ids
	}
	require.Equal(t, sortedIDs(root, child, leaf), subtree(t, root), "retired descendants are excluded")
	require.Equal(t, sortedIDs(child, leaf), subtree(t, child), "a sub-unit grant never reaches its parent")
	require.Equal(t, sortedIDs(root, child, leaf), subtree(t, child, root, child), "overlapping roots are counted once")
	require.Equal(t, sortedIDs(sibling, moved), subtree(t, sibling))
	require.Empty(t, subtree(t, retired), "a retired root covers nothing")
	require.Empty(t, subtree(t, uuid.Must(uuid.NewV7())), "an unknown root covers nothing")
	require.Empty(t, subtree(t))
	tooMany := make([]uuid.UUID, authz.DepartmentScopeRootLimit+1)
	for i := range tooMany {
		tooMany[i] = uuid.Must(uuid.NewV7())
	}
	_, err = guard.CurrentSubtree(s.ctx, testutil.TenantA, hospital, tooMany)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = guard.CurrentSubtree(s.ctx, testutil.TenantA, hospital, []uuid.UUID{uuid.New()})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, "non-v7 roots are malformed input")
	_, err = guard.CurrentSubtree(s.ctx, testutil.TenantA, uuid.New(), []uuid.UUID{root})
	require.ErrorIs(t, err, authz.ErrDepartmentReferenceHidden)
	_, err = guard.CurrentSubtree(s.ctx, testutil.TenantB, hospital, []uuid.UUID{root})
	require.ErrorIs(t, err, authz.ErrDepartmentReferenceHidden)

	s.put(t, moved, "", child, "active")
	require.Equal(t, sortedIDs(root, child, leaf, moved), subtree(t, root), "moving a sub-unit in brings it into scope")
	require.Equal(t, []uuid.UUID{sibling}, subtree(t, sibling), "moving it out removes it")
	path, err = guard.CurrentPath(s.ctx, testutil.TenantA, hospital, moved)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{moved, child, root}, path)
}

func TestDepartmentScopeGuardChecksCurrentContainment(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-WITHIN-ROOT", uuid.Nil)
	child := s.node(t, "SYN-WITHIN-CHILD", root)
	leaf := s.node(t, "SYN-WITHIN-LEAF", child)
	retired := s.node(t, "SYN-WITHIN-RETIRED", child)
	s.put(t, retired, "", child, "retired")
	sibling := s.node(t, "SYN-WITHIN-SIBLING", uuid.Nil)
	moving := s.node(t, "SYN-WITHIN-MOVING", sibling)
	guard := organization.New().DepartmentScopes(s.db)

	for name, tc := range map[string]struct {
		target, scope uuid.UUID
		want          error
	}{
		"held department":      {leaf, root, nil},
		"held sub-unit":        {leaf, child, nil},
		"exact node":           {leaf, leaf, nil},
		"root covers itself":   {root, root, nil},
		"never upward":         {root, leaf, authz.ErrDepartmentReferenceChanged},
		"never into a sibling": {leaf, sibling, authz.ErrDepartmentReferenceChanged},
		"unknown scope":        {leaf, uuid.Must(uuid.NewV7()), authz.ErrDepartmentReferenceChanged},
		"retired target":       {retired, child, authz.ErrDepartmentReferenceChanged},
		"retired scope":        {retired, retired, authz.ErrDepartmentReferenceChanged},
		"unknown target":       {uuid.Must(uuid.NewV7()), root, authz.ErrDepartmentReferenceHidden},
		"non-v7 target":        {uuid.New(), root, authz.ErrScopedAuthorizationUnavailable},
		"non-v7 scope":         {leaf, uuid.New(), authz.ErrScopedAuthorizationUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			err := s.within(t, guard, tc.target, tc.scope)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}

	hint, err := guard.CurrentPath(s.ctx, testutil.TenantA, s.hospitalID(), moving)
	require.NoError(t, err)
	require.Contains(t, hint, sibling)
	s.put(t, moving, "", root, "active")
	require.ErrorIs(t, s.within(t, guard, moving, sibling), authz.ErrDepartmentReferenceChanged, "the locked path is authority, not the earlier hint")
	require.NoError(t, s.within(t, guard, moving, root))
}

func TestDepartmentScopeGuardRejectsUnguardedForeignAndDifferentDatabaseContexts(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-CONTEXT-ROOT", uuid.Nil)
	guard := organization.New().DepartmentScopes(s.db)
	called := false
	action := func(context.Context) error { called = true; return nil }
	require.ErrorIs(t, guard.WithinCurrentDepartment(s.ctx, testutil.TenantA, s.hospitalID(), root, root, action), authz.ErrScopedAuthorizationUnavailable, "only inside a guarded command")
	err := s.db.WithGuardedTenantCommand(s.ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		require.ErrorIs(t, guard.WithinCurrentDepartment(commandCtx, testutil.TenantA, s.hospitalID(), root, root, nil), authz.ErrScopedAuthorizationUnavailable)
		other := organization.New().DepartmentScopes(&tenantdb.DB{})
		require.ErrorIs(t, other.WithinCurrentDepartment(commandCtx, testutil.TenantA, s.hospitalID(), root, root, action), authz.ErrScopedAuthorizationUnavailable)
		require.ErrorIs(t, guard.WithinCurrentDepartment(commandCtx, testutil.TenantB, s.hospitalID(), root, root, action), tenantdb.ErrGuardedCommandScope)
		require.ErrorIs(t, guard.WithinCurrentDepartment(commandCtx, testutil.TenantA, uuid.New(), root, root, action), authz.ErrDepartmentReferenceHidden)
		return nil
	})
	require.NoError(t, err)
	require.False(t, called)
	missing := organization.New().DepartmentScopes(nil)
	_, err = missing.CurrentPath(s.ctx, testutil.TenantA, s.hospitalID(), root)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = missing.CurrentSubtree(s.ctx, testutil.TenantA, s.hospitalID(), []uuid.UUID{root})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
}

func TestDepartmentScopeGuardHoldsThePathThroughCommitAndRollback(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-HOLD-ROOT", uuid.Nil)
	leaf := s.node(t, "SYN-HOLD-LEAF", root)
	guard := organization.New().DepartmentScopes(s.db)
	lockKey := testutil.TenantA + ":organization-departments:" + s.hospital
	tryRestructure := func() bool {
		var row struct{ Acquired bool }
		require.NoError(t, s.db.WithTenant(s.ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?,0)) AS acquired`, lockKey).Scan(&row).Error
		}))
		return row.Acquired
	}
	tryRootWrite := func() error {
		return s.db.WithTenant(s.ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`SELECT id FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE NOWAIT`, testutil.TenantA, s.hospital, root).Error
		})
	}
	sentinel := errors.New("synthetic dependent command failure")
	for _, outcome := range []error{nil, sentinel} {
		err := s.db.WithGuardedTenantCommand(s.ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			err := guard.WithinCurrentDepartment(commandCtx, testutil.TenantA, s.hospitalID(), leaf, root, func(context.Context) error {
				require.False(t, tryRestructure(), "the restructuring gate is held inside the action")
				require.ErrorContains(t, tryRootWrite(), "55P03", "the scope ancestor row is locked inside the action")
				return nil
			})
			require.NoError(t, err)
			require.False(t, tryRestructure(), "returning from the guard must not release the gate before the outer commit")
			require.ErrorContains(t, tryRootWrite(), "55P03")
			return outcome
		})
		if outcome == nil {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, outcome)
		}
		require.True(t, tryRestructure(), "commit or rollback releases the gate")
		require.NoError(t, tryRootWrite(), "commit or rollback releases the path rows")
	}
}

func TestDepartmentScopeGuardAdmitsBeforeRetirementAndDeniesAfter(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-RACE-ROOT", uuid.Nil)
	leaf := s.node(t, "SYN-RACE-LEAF", root)
	guard := organization.New().DepartmentScopes(s.db)
	admitted, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseCommand := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseCommand)
	command := make(chan error, 1)
	go func() {
		command <- s.db.WithGuardedTenantCommand(s.ctx, testutil.TenantA, time.Now().Add(10*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithinCurrentDepartment(commandCtx, testutil.TenantA, s.hospitalID(), leaf, root, func(context.Context) error { close(admitted); <-release; return nil })
		})
	}()
	select {
	case <-admitted:
	case err := <-command:
		t.Fatalf("command was not admitted: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("admission timed out")
	}
	writer := make(chan int, 1)
	retiredRevision := uuid.Must(uuid.NewV7())
	go func() {
		response := organizationCall(s.r, "PUT", departmentPath(s.hospital, leaf.String(), retiredRevision.String()), "manager", testutil.TenantA, departmentIntent(1, "SYN-RACE-LEAF", "Synthetic SYN-RACE-LEAF", "sub_unit", root.String(), "retired"))
		writer <- response.Code
	}()
	require.Eventually(t, func() bool {
		var count int64
		err := s.db.WithTenant(s.ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND wait_event_type='Lock' AND wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&count).Error
		})
		return err == nil && count > 0
	}, 3*time.Second, 10*time.Millisecond, "the retirement waits on the owner gate held by the admitted command")
	select {
	case code := <-writer:
		t.Fatalf("retirement finished before the admitted command committed: %d", code)
	default:
	}
	releaseCommand()
	require.NoError(t, <-command, "the admitted command commits")
	require.Equal(t, 200, <-writer)
	require.ErrorIs(t, s.within(t, guard, leaf, root), authz.ErrDepartmentReferenceChanged, "a retired department authorizes nothing from the moment it retires")
}

func TestDepartmentScopeGuardBoundsTheSubtree(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-WIDE-ROOT", uuid.Nil)
	guard := organization.New().DepartmentScopes(s.db)
	nodePrefix, revisionPrefix := uuid.Must(uuid.NewV7()).String()[:24], uuid.Must(uuid.NewV7()).String()[:24]
	seed := func(from, to int) {
		t.Helper()
		require.NoError(t, s.db.WithTenant(tenantdb.WithActor(s.ctx, "user-manager"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO organization_departments(tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,effective_at,created_at,updated_at)
 SELECT ?::uuid,?::uuid,(?||lpad(to_hex(n),12,'0'))::uuid,(?||lpad(to_hex(n),12,'0'))::uuid,'SYN-WIDE-'||n,'sub_unit','Synthetic wide sub-unit',?::uuid,'active',1,
 transaction_timestamp(),transaction_timestamp(),transaction_timestamp() FROM generate_series(?::int,?::int) n`, testutil.TenantA, s.hospital, nodePrefix, revisionPrefix, root, from, to).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO organization_department_revisions(tenant_id,hospital_id,node_id,id,code,kind,name,parent_id,status,version,expected_version,reason,actor_subject,effective_at,node_created_at,created_at,updated_at)
 SELECT tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,0,'Synthetic wide fixture','user-manager',effective_at,created_at,created_at,updated_at
 FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND code LIKE 'SYN-WIDE-%' AND code<>'SYN-WIDE-ROOT' AND created_at=transaction_timestamp()`, testutil.TenantA, s.hospital).Error
		}))
	}
	seed(1, authz.DepartmentSubtreeLimit-1)
	ids, err := guard.CurrentSubtree(s.ctx, testutil.TenantA, s.hospitalID(), []uuid.UUID{root})
	require.NoError(t, err)
	require.Len(t, ids, authz.DepartmentSubtreeLimit, "exactly the limit is answered")
	seed(authz.DepartmentSubtreeLimit, authz.DepartmentSubtreeLimit)
	ids, err = guard.CurrentSubtree(s.ctx, testutil.TenantA, s.hospitalID(), []uuid.UUID{root})
	require.ErrorIs(t, err, authz.ErrDepartmentSubtreeLimit)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, "the limit fails closed as unavailable, never truncated")
	require.Nil(t, ids)
}

func TestDepartmentScopeGuardCoversTheFullPersistedDepth(t *testing.T) {
	s := newScopeTree(t)
	chain := []uuid.UUID{s.node(t, "SYN-DEPTH-1", uuid.Nil)}
	for depth := 2; depth <= 8; depth++ {
		chain = append(chain, s.node(t, "SYN-DEPTH-"+string(rune('0'+depth)), chain[len(chain)-1]))
	}
	guard := organization.New().DepartmentScopes(s.db)
	path, err := guard.CurrentPath(s.ctx, testutil.TenantA, s.hospitalID(), chain[7])
	require.NoError(t, err)
	want := slices.Clone(chain)
	slices.Reverse(want)
	require.Equal(t, want, path)
	ids, err := guard.CurrentSubtree(s.ctx, testutil.TenantA, s.hospitalID(), []uuid.UUID{chain[0]})
	require.NoError(t, err)
	require.Equal(t, sortedIDs(chain...), ids)
	require.NoError(t, s.within(t, guard, chain[7], chain[0]))
	require.NoError(t, s.within(t, guard, chain[7], chain[4]))
	require.ErrorIs(t, s.within(t, guard, chain[3], chain[4]), authz.ErrDepartmentReferenceChanged)
}

func TestDepartmentScopeGuardReadsTheSubtreeInTheGuardedCommand(t *testing.T) {
	s := newScopeTree(t)
	root := s.node(t, "SYN-HELD-ROOT", uuid.Nil)
	guard := organization.New().DepartmentScopes(s.db)
	pending, rollback := uuid.Must(uuid.NewV7()), errors.New("roll back the uncommitted node")
	err := s.db.WithGuardedTenantCommand(tenantdb.WithActor(s.ctx, "user-manager"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		require.NoError(t, s.db.WithTenantCommand(commandCtx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO organization_departments(tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,effective_at,created_at,updated_at)
 VALUES(?,?,?,?,'SYN-HELD-CHILD','sub_unit','Synthetic held child',?,'active',1,transaction_timestamp(),transaction_timestamp(),transaction_timestamp())`, testutil.TenantA, s.hospital, pending, uuid.Must(uuid.NewV7()), root).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO organization_department_revisions(tenant_id,hospital_id,node_id,id,code,kind,name,parent_id,status,version,expected_version,reason,actor_subject,effective_at,node_created_at,created_at,updated_at)
 SELECT tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,0,'Synthetic held fixture','user-manager',effective_at,created_at,created_at,updated_at
 FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, s.hospital, pending).Error
		}))
		ids, err := guard.CurrentSubtree(commandCtx, testutil.TenantA, s.hospitalID(), []uuid.UUID{root})
		require.NoError(t, err)
		require.Equal(t, sortedIDs(root, pending), ids, "the subtree is read in the command transaction that holds the structure lock")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	ids, err := guard.CurrentSubtree(s.ctx, testutil.TenantA, s.hospitalID(), []uuid.UUID{root})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{root}, ids)
}
