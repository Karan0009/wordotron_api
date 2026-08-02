package lib_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/models"
)

var testWhitelist = lib.SortWhitelist{
	Allowed: []string{"created_at", "email"},
	Default: "created_at",
}

// parseQuery runs ParsePageParams inside a real request so the query parsing
// path is exercised end to end.
func parseQuery(t *testing.T, query string) (models.PageParams, error) {
	t.Helper()

	app := fiber.New()

	var (
		params   models.PageParams
		parseErr error
	)
	app.Get("/", func(c fiber.Ctx) error {
		params, parseErr = lib.ParsePageParams(c, testWhitelist)
		return c.SendStatus(fiber.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/?"+query, nil))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return params, parseErr
}

func TestParsePageParams_Defaults(t *testing.T) {
	params, err := parseQuery(t, "")
	require.NoError(t, err)

	require.Equal(t, models.DefaultPage, params.Page)
	require.Equal(t, models.DefaultLimit, params.Limit)
	require.Equal(t, "created_at", params.Sort)
	require.Equal(t, models.SortDesc, params.Order)
	require.Empty(t, params.Search)
}

func TestParsePageParams_ExplicitValues(t *testing.T) {
	params, err := parseQuery(t, "page=3&limit=25&sort=email&order=asc&search=jane")
	require.NoError(t, err)

	require.Equal(t, 3, params.Page)
	require.Equal(t, 25, params.Limit)
	require.Equal(t, "email", params.Sort)
	require.Equal(t, models.SortAsc, params.Order)
	require.Equal(t, "jane", params.Search)
	require.Equal(t, 50, params.Offset())
}

func TestParsePageParams_RejectsUnknownSortColumn(t *testing.T) {
	// An unlisted column is the injection vector we are guarding against.
	_, err := parseQuery(t, "sort=password_hash")
	require.Error(t, err)

	appErr, ok := lib.As(err)
	require.True(t, ok)
	require.Equal(t, lib.CodeValidation, appErr.Code)
	require.Len(t, appErr.Fields, 1)
	require.Equal(t, "sort", appErr.Fields[0].Field)
}

func TestParsePageParams_RejectsOutOfRangeLimit(t *testing.T) {
	for _, query := range []string{"limit=0", "limit=101", "limit=-5"} {
		_, err := parseQuery(t, query)
		require.Error(t, err, "query %q must be rejected", query)
	}
}

func TestParsePageParams_RejectsInvalidOrder(t *testing.T) {
	_, err := parseQuery(t, "order=sideways")
	require.Error(t, err)
}

func TestNewPageMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		page      int
		limit     int
		total     int64
		wantPages int
		wantNext  bool
		wantPrev  bool
	}{
		{"first page of three", 1, 10, 25, 3, true, false},
		{"middle page", 2, 10, 25, 3, true, true},
		{"last page", 3, 10, 25, 3, false, true},
		{"empty result", 1, 10, 0, 1, false, false},
		{"exact fit", 2, 10, 20, 2, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta := models.NewPageMeta(models.PageParams{Page: tc.page, Limit: tc.limit}, tc.total)

			require.Equal(t, tc.wantPages, meta.TotalPages)
			require.Equal(t, tc.wantNext, meta.HasNext)
			require.Equal(t, tc.wantPrev, meta.HasPrev)
			require.Equal(t, tc.total, meta.Total)
		})
	}
}

func TestPageParams_NormalizeClampsLimit(t *testing.T) {
	t.Parallel()

	params := models.PageParams{Page: 0, Limit: 5000}.Normalize("created_at")

	require.Equal(t, models.DefaultPage, params.Page)
	require.Equal(t, models.MaxLimit, params.Limit)
	require.Equal(t, "created_at", params.Sort)
	require.Equal(t, models.SortDesc, params.Order)
}
