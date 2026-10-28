package emergency_test

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"strings"
	"testing"
)

func TestClosureRequiresScopedSupervisorAndIsIdempotent(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	id := uuid.Must(uuid.NewV7()).String()
	path := f.path() + "/cases/" + id
	w := testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	body := `{"version":1,"reason":"entered_in_error","explanation":"Synthetic report entered in error; supervisor reviewed."}`
	w = testutil.Do(f.r, "POST", path+"/close", "dispatcher", body)
	require.Equal(t, 403, w.Code)
	w = testutil.Do(f.r, "PUT", f.path()+"/access/user-supervisor", "admin", `{"level":"reader"}`)
	require.Equal(t, 200, w.Code)
	w = testutil.Do(f.r, "POST", path+"/close", "supervisor", body)
	require.Equal(t, 404, w.Code, "route-level role alone must not close a case")
	w = testutil.Do(f.r, "PUT", f.path()+"/access/user-supervisor", "admin", `{"level":"supervisor"}`)
	require.Equal(t, 200, w.Code)
	w = testutil.Do(f.r, "POST", path+"/close", "supervisor", strings.Replace(body, `"version":1`, `"version":9`, 1))
	require.Equal(t, 409, w.Code)
	w = testutil.Do(f.r, "POST", path+"/close", "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	first := w.Body.String()
	out := decodeBody(t, w.Body.Bytes())
	require.Equal(t, "closed", out["status"])
	require.EqualValues(t, 2, out["version"])
	w = testutil.Do(f.r, "POST", path+"/close", "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, first, w.Body.String())
	w = testutil.Do(f.r, "POST", path+"/close", "supervisor", strings.Replace(body, "entered_in_error", "caller_withdrew", 1))
	require.Equal(t, 409, w.Code)
	w = testutil.Do(f.r, "GET", path+"/timeline", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "Synthetic report")
	require.Contains(t, w.Body.String(), `"state":"closed"`)
	w = testutil.Do(f.r, "GET", path, "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Synthetic report entered in error")
}
