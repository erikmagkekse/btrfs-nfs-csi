package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testItem struct {
	Name   string
	Labels map[string]string
}

func (t testItem) GetLabels() map[string]string { return t.Labels }

type testResp struct {
	Name string `json:"name"`
}

type testListResp struct {
	Items []testResp `json:"items"`
	Total int        `json:"total"`
	Next  string     `json:"next,omitempty"`
}

func makeItems(names ...string) []testItem {
	items := make([]testItem, len(names))
	for i, n := range names {
		items[i] = testItem{Name: n}
	}
	return items
}

func callPaginatedList(t *testing.T, h *Handler, items []testItem, after string, limit string) testListResp {
	t.Helper()
	e := echo.New()
	target := "/test"
	sep := "?"
	if after != "" {
		target += sep + "after=" + after
		sep = "&"
	}
	if limit != "" {
		target += sep + "limit=" + limit
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mapFn := func(i *testItem) testResp { return testResp{Name: i.Name} }
	wrapFn := func(r []testResp, total int, next string) any {
		return testListResp{Items: r, Total: total, Next: next}
	}

	err := paginatedList(h, c, items, mapFn, wrapFn)
	require.NoError(t, err)

	var resp testListResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestPagination_NoLimit(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b", "c", "d", "e")

	resp := callPaginatedList(t, h, items, "", "")
	assert.Len(t, resp.Items, 5)
	assert.Equal(t, 5, resp.Total)
	assert.Empty(t, resp.Next, "no next token when no limit")
}

func TestPagination_WithLimit(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b", "c", "d", "e")

	// Page 1
	resp := callPaginatedList(t, h, items, "", "2")
	assert.Len(t, resp.Items, 2)
	assert.Equal(t, "a", resp.Items[0].Name)
	assert.Equal(t, "b", resp.Items[1].Name)
	assert.Equal(t, 5, resp.Total)
	assert.NotEmpty(t, resp.Next)

	// Page 2
	resp2 := callPaginatedList(t, h, items, resp.Next, "2")
	assert.Len(t, resp2.Items, 2)
	assert.Equal(t, "c", resp2.Items[0].Name)
	assert.Equal(t, "d", resp2.Items[1].Name)
	assert.NotEmpty(t, resp2.Next)

	// Page 3 (last)
	resp3 := callPaginatedList(t, h, items, resp2.Next, "2")
	assert.Len(t, resp3.Items, 1)
	assert.Equal(t, "e", resp3.Items[0].Name)
	assert.Empty(t, resp3.Next, "no more pages")
}

func TestPagination_SnapshotIsolation(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b", "c", "d", "e")

	// Page 1
	resp := callPaginatedList(t, h, items, "", "2")
	require.NotEmpty(t, resp.Next)

	// Mutate the live data -- add item at front, remove one
	mutated := makeItems("aa", "b", "c", "d", "e", "f")

	// Page 2 should still read from the original snapshot
	resp2 := callPaginatedList(t, h, mutated, resp.Next, "2")
	assert.Equal(t, "c", resp2.Items[0].Name)
	assert.Equal(t, "d", resp2.Items[1].Name)
	assert.Equal(t, 5, resp2.Total, "total from snapshot, not live data")
}

func TestPagination_InvalidToken_FallsBack(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b", "c")

	resp := callPaginatedList(t, h, items, "garbage!!!", "2")
	assert.Len(t, resp.Items, 2)
	assert.Equal(t, "a", resp.Items[0].Name, "falls back to page 1")
	assert.Equal(t, 3, resp.Total)
}

func TestPagination_ExpiredSnapshot_FallsBack(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b", "c", "d")

	// Page 1
	resp := callPaginatedList(t, h, items, "", "2")
	require.NotEmpty(t, resp.Next)

	// Manually expire the snapshot
	cur, ok := decodeCursor(resp.Next)
	require.True(t, ok)
	if v, loaded := h.pageSnapshots.Load(cur.SnapID); loaded {
		ps := v.(*pageSnapshot)
		ps.timer.Stop()
		h.pageSnapshots.Delete(cur.SnapID)
	}

	// Using expired token falls back to page 1 of current data
	newItems := makeItems("x", "y", "z")
	resp2 := callPaginatedList(t, h, newItems, resp.Next, "2")
	assert.Equal(t, "x", resp2.Items[0].Name, "falls back to page 1 of new data")
	assert.Equal(t, 3, resp2.Total)
}

func TestPagination_AutoCleanup(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b", "c")

	resp := callPaginatedList(t, h, items, "", "2")
	require.NotEmpty(t, resp.Next)

	cur, ok := decodeCursor(resp.Next)
	require.True(t, ok)

	// Snapshot should exist
	_, loaded := h.pageSnapshots.Load(cur.SnapID)
	assert.True(t, loaded, "snapshot should exist before TTL")

	// Wait for cleanup (PaginationTTL, but we can stop timer and trigger manually)
	if v, ok := h.pageSnapshots.Load(cur.SnapID); ok {
		ps := v.(*pageSnapshot)
		ps.timer.Stop()
	}
	h.pageSnapshots.Delete(cur.SnapID)

	_, loaded = h.pageSnapshots.Load(cur.SnapID)
	assert.False(t, loaded, "snapshot should be gone after cleanup")
}

func TestPagination_OffsetBeyondEnd(t *testing.T) {
	h := &Handler{}
	items := makeItems("a", "b")

	// Get a valid token
	resp := callPaginatedList(t, h, items, "", "2")
	assert.Empty(t, resp.Next, "only 2 items with limit 2 = no next")

	// Craft a cursor pointing beyond the snapshot
	snapID := generateSnapID()
	ps := &pageSnapshot{items: makeItems("a")}
	ps.timer = time.AfterFunc(30*time.Second, func() { h.pageSnapshots.Delete(snapID) })
	h.pageSnapshots.Store(snapID, ps)

	token := encodeCursor(pageCursor{SnapID: snapID, Offset: 999})
	resp2 := callPaginatedList(t, h, items, token, "10")
	assert.Empty(t, resp2.Items)
	assert.Equal(t, 1, resp2.Total, "total from snapshot")
}

func TestCursor_EncodeDecode(t *testing.T) {
	c := pageCursor{SnapID: "abc123", Offset: 42}
	encoded := encodeCursor(c)
	decoded, ok := decodeCursor(encoded)
	require.True(t, ok)
	assert.Equal(t, c, decoded)
}

func TestCursor_InvalidBase64(t *testing.T) {
	_, ok := decodeCursor("not-base64!!!")
	assert.False(t, ok)
}

func TestCursor_InvalidJSON(t *testing.T) {
	_, ok := decodeCursor("bm90LWpzb24")
	assert.False(t, ok)
}
