package pharmacy_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// #1718: whoever manages an outlet's access sees every grant there.

func grantsPath(pharmacy, outlet string) string {
	return "/v1/pharmacy/pharmacies/" + pharmacy + "/outlets/" + outlet + "/access-grants"
}

type grantPage struct {
	Data []struct {
		ID, Subject, Level string
	} `json:"data"`
	Page struct {
		NextCursor *string `json:"next_cursor"`
	} `json:"page"`
}

func listGrants(t *testing.T, r *gin.Engine, query string) grantPage {
	t.Helper()
	w := do(r, "GET", grantsPath(pharmaOrganization, pharmaOutlet)+query, "steward", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var page grantPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	return page
}

func grantLevels(page grantPage) map[string]string {
	levels := map[string]string{}
	for _, row := range page.Data {
		levels[row.Subject] = row.Level
	}
	return levels
}

func TestOutletGrantsListsEachSubject(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-a", "editor")
	grantCatalogue(t, r, "user-b", "reader")
	grantCatalogue(t, r, "user-c", "editor")
	page := listGrants(t, r, "")
	require.Len(t, page.Data, 3)
	require.Equal(t, map[string]string{"user-a": "editor", "user-b": "reader", "user-c": "editor"}, grantLevels(page))
	for _, row := range page.Data {
		require.NotEmpty(t, row.ID)
	}
}

func TestOutletGrantsReflectChanges(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-a", "editor")
	grantCatalogue(t, r, "user-b", "reader")
	grantCatalogue(t, r, "user-c", "editor")
	grantCatalogue(t, r, "user-b", "editor")
	require.Equal(t, 200, do(r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-c", "editor", "").Code)
	require.Equal(t, map[string]string{"user-a": "editor", "user-b": "editor"}, grantLevels(listGrants(t, r, "")))
}

func TestOutletGrantsStayInTheirScope(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-a", "editor")
	// A second registered pharmacy: the path names it, the outlet is the first's.
	other := "0199e000-0000-7000-8000-0000000000ff"
	w := do(r, "PUT", "/v1/pharmacy/pharmacies/"+other, "editor", `{"code":"TEST-OTHER","name":"TEST Other Pharmacy","ownership_type":"independent"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = do(r, "GET", grantsPath(other, pharmaOutlet), "steward", "")
	require.Equal(t, http.StatusNotFound, w.Code, "another pharmacy's path never lists this outlet: %s", w.Body.String())
	w = do(r, "GET", grantsPath(pharmaOrganization, pharmaOutlet), "registrar", "")
	require.Equal(t, http.StatusNotFound, w.Code, "another tenant never sees the outlet: %s", w.Body.String())
	w = do(r, "GET", grantsPath(pharmaOrganization, pharmaOutlet), "reader", "")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestOutletGrantsPage(t *testing.T) {
	r, _ := catalogueHarness(t)
	for _, subject := range []string{"user-a", "user-b", "user-c"} {
		grantCatalogue(t, r, subject, "editor")
	}
	first := listGrants(t, r, "?limit=2")
	require.Len(t, first.Data, 2)
	require.NotNil(t, first.Page.NextCursor)
	second := listGrants(t, r, "?limit=2&cursor="+*first.Page.NextCursor)
	require.Len(t, second.Data, 1)
	seen := grantLevels(first)
	for subject, level := range grantLevels(second) {
		_, dup := seen[subject]
		require.False(t, dup, subject)
		seen[subject] = level
	}
	require.Len(t, seen, 3)
}
