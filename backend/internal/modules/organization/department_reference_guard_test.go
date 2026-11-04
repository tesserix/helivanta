package organization_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestDepartmentReferenceGuardPinsCurrentAncestorAndDirection(t *testing.T) {
	r, db, bus, ctx, hospital := departmentHarnessWithBus(t, organization.New())
	create := func(code, kind, parent string) (uuid.UUID, uuid.UUID) {
		t.Helper()
		id, revision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		response := organizationCall(r, "PUT", departmentPath(hospital, id.String(), revision.String()), "manager", testutil.TenantA, departmentIntent(0, code, "Synthetic "+code, kind, parent, "active"))
		require.Equal(t, 200, response.Code, response.Body.String())
		return id, revision
	}
	root, rootRevision := create("SYN-ANCESTOR", "department", "")
	child, _ := create("SYN-CHILD", "sub_unit", root.String())
	leaf, leafRevision := create("SYN-LEAF", "sub_unit", child.String())
	sibling, siblingRevision := create("SYN-SIBLING", "department", "")
	retired, _ := create("SYN-RETIRED", "department", "")
	retiredRevision := uuid.Must(uuid.NewV7())
	response := organizationCall(r, "PUT", departmentPath(hospital, retired.String(), retiredRevision.String()), "manager", testutil.TenantA, departmentIntent(1, "SYN-RETIRED", "Synthetic retired department", "department", "", "retired"))
	require.Equal(t, 200, response.Code, response.Body.String())
	foreignHospital, request := uuid.New(), uuid.Must(uuid.NewV7())
	response = organizationCall(r, "PUT", "/v1/organization/hospitals/"+foreignHospital.String()+"/registrations/"+request.String(), "manager", testutil.TenantA, `{"reason":"Synthetic second hospital"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	for _, consumer := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if consumer.Name != "organization-hospital-observation" {
			continue
		}
		data, err := json.Marshal(map[string]any{"request_id": request, "hospital_id": foreignHospital, "exists": true})
		require.NoError(t, err)
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: data})
		}))
	}
	foreign, foreignRevision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	response = organizationCall(r, "PUT", departmentPath(foreignHospital.String(), foreign.String(), foreignRevision.String()), "manager", testutil.TenantA, departmentIntent(0, "SYN-FOREIGN", "Synthetic sibling hospital department", "department", "", "active"))
	require.Equal(t, 200, response.Code, response.Body.String())
	guard := organization.New().DepartmentReferences(db)
	base := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: leaf, DepartmentRevision: leafRevision}
	check := func(t *testing.T, ref authz.DepartmentReference, want error) {
		t.Helper()
		called := false
		err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithCurrentDepartment(commandCtx, ref, func(context.Context) error { called = true; return nil })
		})
		if want == nil {
			require.NoError(t, err)
			require.True(t, called)
		} else {
			require.ErrorIs(t, err, want)
			require.False(t, called)
		}
	}
	for _, tc := range []struct {
		name               string
		ancestor, revision uuid.UUID
		want               error
	}{
		{"legacy direct reference", uuid.Nil, uuid.Nil, nil},
		{"current ancestor", root, rootRevision, nil},
		{"same department", leaf, leafRevision, nil},
		{"sibling cannot supervise", sibling, siblingRevision, authz.ErrDepartmentReferenceChanged},
		{"stale ancestor revision", root, uuid.Must(uuid.NewV7()), authz.ErrDepartmentReferenceChanged},
		{"retired current ancestor", retired, retiredRevision, authz.ErrDepartmentReferenceChanged},
		{"unknown ancestor", uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), authz.ErrDepartmentReferenceHidden},
		{"sibling hospital ancestor", foreign, foreignRevision, authz.ErrDepartmentReferenceHidden},
		{"missing revision", root, uuid.Nil, authz.ErrScopedAuthorizationUnavailable},
		{"missing ancestor", uuid.Nil, rootRevision, authz.ErrScopedAuthorizationUnavailable},
		{"invalid ancestor version", uuid.New(), rootRevision, authz.ErrScopedAuthorizationUnavailable},
		{"invalid revision version", root, uuid.New(), authz.ErrScopedAuthorizationUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := base
			ref.AncestorID, ref.AncestorRevision = tc.ancestor, tc.revision
			check(t, ref, tc.want)
		})
	}
	upward := base
	upward.DepartmentID, upward.DepartmentRevision = root, rootRevision
	upward.AncestorID, upward.AncestorRevision = leaf, leafRevision
	check(t, upward, authz.ErrDepartmentReferenceChanged)
	newRootRevision := uuid.Must(uuid.NewV7())
	response = organizationCall(r, "PUT", departmentPath(hospital, root.String(), newRootRevision.String()), "manager", testutil.TenantA, departmentIntent(1, "SYN-ANCESTOR", "Synthetic renamed ancestor", "department", "", "active"))
	require.Equal(t, 200, response.Code, response.Body.String())
	current := base
	current.AncestorID, current.AncestorRevision = root, rootRevision
	check(t, current, authz.ErrDepartmentReferenceChanged)
	current.AncestorRevision = newRootRevision
	check(t, current, nil)
	newChildRevision := uuid.Must(uuid.NewV7())
	response = organizationCall(r, "PUT", departmentPath(hospital, child.String(), newChildRevision.String()), "manager", testutil.TenantA, departmentIntent(1, "SYN-CHILD", "Synthetic moved intermediate", "sub_unit", sibling.String(), "active"))
	require.Equal(t, 200, response.Code, response.Body.String())
	check(t, current, authz.ErrDepartmentReferenceChanged)
	current.AncestorID, current.AncestorRevision = sibling, siblingRevision
	check(t, current, nil)
}

func TestDepartmentReferenceGuardLocksAncestorThroughCommitAndRollback(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	root, rootRevision, leaf, leafRevision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, item := range []struct {
		id, revision       uuid.UUID
		code, kind, parent string
	}{{root, rootRevision, "SYN-ROOT-LOCK", "department", ""}, {leaf, leafRevision, "SYN-LEAF-LOCK", "sub_unit", root.String()}} {
		response := organizationCall(r, "PUT", departmentPath(hospital, item.id.String(), item.revision.String()), "manager", testutil.TenantA, departmentIntent(0, item.code, "Synthetic "+item.code, item.kind, item.parent, "active"))
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	guard := organization.New().DepartmentReferences(db)
	ref := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: leaf, DepartmentRevision: leafRevision, AncestorID: root, AncestorRevision: rootRevision}
	tryRootWrite := func() error {
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`SELECT id FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE NOWAIT`, testutil.TenantA, hospital, root).Error
		})
	}
	sentinel := errors.New("synthetic dependent command failure")
	for _, outcome := range []error{nil, sentinel} {
		err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			err := guard.WithCurrentDepartment(commandCtx, ref, func(context.Context) error { require.ErrorContains(t, tryRootWrite(), "55P03"); return nil })
			require.NoError(t, err)
			require.ErrorContains(t, tryRootWrite(), "55P03", "ancestor lock survives returning from the owner callback")
			return outcome
		})
		if outcome == nil {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, outcome)
		}
		require.NoError(t, tryRootWrite(), "commit/rollback releases the exact ancestor row")
	}
}

func TestDepartmentReferenceGuardAcceptsTheFullOwnerDepth(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	var root, rootRevision, leaf, leafRevision uuid.UUID
	for level := 0; level < 8; level++ {
		id, revision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		kind, parent := "sub_unit", leaf.String()
		if level == 0 {
			kind, parent = "department", ""
			root, rootRevision = id, revision
		}
		response := organizationCall(r, "PUT", departmentPath(hospital, id.String(), revision.String()), "manager", testutil.TenantA, departmentIntent(0, fmt.Sprintf("SYN-LEVEL-%d", level), "Synthetic bounded department", kind, parent, "active"))
		require.Equal(t, 200, response.Code, response.Body.String())
		leaf, leafRevision = id, revision
	}
	called := false
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return organization.New().DepartmentReferences(db).WithCurrentDepartment(commandCtx, authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: leaf, DepartmentRevision: leafRevision, AncestorID: root, AncestorRevision: rootRevision}, func(context.Context) error { called = true; return nil })
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestDepartmentReferenceGuardFencesIntermediateMoveUntilOuterCommit(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	root, rootRevision, middle, middleRevision, leaf, leafRevision, sibling, siblingRevision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, row := range []struct {
		id, revision       uuid.UUID
		code, kind, parent string
	}{
		{root, rootRevision, "SYN-FENCE-ROOT", "department", ""},
		{middle, middleRevision, "SYN-FENCE-MIDDLE", "sub_unit", root.String()},
		{leaf, leafRevision, "SYN-FENCE-LEAF", "sub_unit", middle.String()},
		{sibling, siblingRevision, "SYN-FENCE-SIBLING", "department", ""},
	} {
		response := organizationCall(r, "PUT", departmentPath(hospital, row.id.String(), row.revision.String()), "manager", testutil.TenantA, departmentIntent(0, row.code, "Synthetic "+row.code, row.kind, row.parent, "active"))
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	ref := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: leaf, DepartmentRevision: leafRevision, AncestorID: root, AncestorRevision: rootRevision}
	guard := organization.New().DepartmentReferences(db)
	admitted, callbackRelease, ownerReturned, commitRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var callbackOnce, commitOnce sync.Once
	releaseCallback := func() { callbackOnce.Do(func() { close(callbackRelease) }) }
	releaseCommit := func() { commitOnce.Do(func() { close(commitRelease) }) }
	t.Cleanup(func() { releaseCallback(); releaseCommit() })
	command := make(chan error, 1)
	go func() {
		command <- db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(10*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			if err := guard.WithCurrentDepartment(commandCtx, ref, func(context.Context) error { close(admitted); <-callbackRelease; return nil }); err != nil {
				return err
			}
			close(ownerReturned)
			<-commitRelease
			return nil
		})
	}()
	select {
	case <-admitted:
	case err := <-command:
		require.NoError(t, err)
		t.Fatal("source callback was not admitted")
	case <-time.After(10 * time.Second):
		t.Fatal("source admission timed out")
	}
	writer := make(chan int, 1)
	go func() {
		response := organizationCall(r, "PUT", departmentPath(hospital, middle.String(), uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, departmentIntent(1, "SYN-FENCE-MIDDLE", "Synthetic moved middle", "sub_unit", sibling.String(), "active"))
		writer <- response.Code
	}()
	waiting := func() bool {
		var count int64
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND wait_event_type='Lock' AND wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&count).Error
		})
		return err == nil && count > 0
	}
	require.Eventually(t, waiting, 3*time.Second, 10*time.Millisecond, "the actual public writer must wait on the owner gate")
	releaseCallback()
	select {
	case <-ownerReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("source guard did not return")
	}
	require.True(t, waiting(), "returning from the source guard must not release the gate before outer commit")
	releaseCommit()
	require.NoError(t, <-command)
	require.Equal(t, 200, <-writer)
	called := false
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithCurrentDepartment(commandCtx, ref, func(context.Context) error { called = true; return nil })
	})
	require.ErrorIs(t, err, authz.ErrDepartmentReferenceChanged)
	require.False(t, called, "a later command sees the committed move even though the leaf's own revision did not change")
}

func TestDepartmentReferenceGuardHoldsCurrentSourceUntilOuterCommit(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	department, revision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	w := organizationCall(r, "PUT", departmentPath(hospital, department.String(), revision.String()), "manager", testutil.TenantA, departmentIntent(0, "SYN-GUARD", "Synthetic guarded department", "department", "", "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	guard := organization.New().DepartmentReferences(db)
	ref := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: department, DepartmentRevision: revision}
	lockKey := testutil.TenantA + ":organization-departments:" + hospital
	tryRestructure := func() bool {
		var row struct{ Acquired bool }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?,0)) AS acquired`, lockKey).Scan(&row).Error
		}))
		return row.Acquired
	}
	var escaped context.Context
	require.NoError(t, db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		err := guard.WithCurrentDepartment(commandCtx, ref, func(actionCtx context.Context) error {
			escaped = actionCtx
			require.False(t, tryRestructure(), "the owner restructuring gate remains held inside the action")
			err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec(`SELECT id FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE NOWAIT`, testutil.TenantA, hospital, department).Error
			})
			require.ErrorContains(t, err, "55P03", "the exact source row remains locked")
			return nil
		})
		require.NoError(t, err)
		require.False(t, tryRestructure(), "returning from the owner guard must not release its lock before the product commits")
		return nil
	}))
	require.True(t, tryRestructure())
	called := false
	action := func(context.Context) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentDepartment(escaped, ref, action), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	retired := uuid.Must(uuid.NewV7()).String()
	w = organizationCall(r, "PUT", departmentPath(hospital, department.String(), retired), "manager", testutil.TenantA, departmentIntent(1, "SYN-GUARD", "Synthetic guarded department", "department", "", "retired"))
	require.Equal(t, 200, w.Code, w.Body.String())
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithCurrentDepartment(commandCtx, ref, action)
	})
	require.ErrorIs(t, err, authz.ErrDepartmentReferenceChanged)
	require.False(t, called)
}

func TestDepartmentReferenceGuardRejectsUnguardedForeignAndDifferentDatabaseContexts(t *testing.T) {
	_, db, ctx, hospital := departmentHarness(t)
	guard := organization.New().DepartmentReferences(db)
	ref := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: uuid.Must(uuid.NewV7()), DepartmentRevision: uuid.Must(uuid.NewV7())}
	called := false
	action := func(context.Context) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentDepartment(ctx, ref, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		otherDB := organization.New().DepartmentReferences(&tenantdb.DB{})
		require.ErrorIs(t, otherDB.WithCurrentDepartment(commandCtx, ref, action), authz.ErrScopedAuthorizationUnavailable)
		foreign := ref
		foreign.TenantID = testutil.TenantB
		require.ErrorIs(t, guard.WithCurrentDepartment(commandCtx, foreign, action), tenantdb.ErrGuardedCommandScope)
		require.ErrorIs(t, guard.WithCurrentDepartment(commandCtx, ref, action), authz.ErrDepartmentReferenceHidden)
		return nil
	})
	require.NoError(t, err)
	require.False(t, called)
}
