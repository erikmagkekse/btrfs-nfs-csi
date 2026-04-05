package v1

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"

	"github.com/labstack/echo/v5"
)

type pageSnapshot struct {
	items any
	timer *time.Timer
}

type pageCursor struct {
	SnapID string
	Offset int
}

func encodeCursor(c pageCursor) string {
	raw := fmt.Sprintf("%s:%d", c.SnapID, c.Offset)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (pageCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return pageCursor{}, false
	}
	id, offsetStr, ok := strings.Cut(string(b), ":")
	if !ok {
		return pageCursor{}, false
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		return pageCursor{}, false
	}
	return pageCursor{SnapID: id, Offset: offset}, true
}

func generateSnapID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (h *Handler) paginationSnapshotTTL() time.Duration {
	if h.PaginationSnapshotTTL > 0 {
		return h.PaginationSnapshotTTL
	}
	return 30 * time.Second
}

func (h *Handler) paginationMaxSnapshots() int {
	if h.PaginationMaxSnapshots > 0 {
		return h.PaginationMaxSnapshots
	}
	return 100
}

func (h *Handler) deleteSnapshot(id string) {
	if _, loaded := h.pageSnapshots.LoadAndDelete(id); loaded {
		h.snapshotCount.Add(-1)
	}
}

// paginatedList applies label filtering, snapshot-based pagination, and response mapping.
func paginatedList[T labeled, R any](h *Handler, c *echo.Context, items []T, mapFn func(*T) R, wrapFn func([]R, int, string) any) error {
	after := c.QueryParam("after")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = h.DefaultPageLimit
	}

	if filters := c.QueryParams()["label"]; len(filters) > 0 {
		items = filterByLabels(items, filters)
	}

	// No pagination needed -- return everything without creating a snapshot.
	if limit <= 0 && after == "" {
		resp := make([]R, len(items))
		for i := range items {
			resp[i] = mapFn(&items[i])
		}
		return c.JSON(http.StatusOK, wrapFn(resp, len(items), ""))
	}

	var snap []T
	var offset int
	var snapID string
	cur, ok := decodeCursor(after)
	if ok {
		if v, loaded := h.pageSnapshots.Load(cur.SnapID); loaded {
			ps := v.(*pageSnapshot)
			if typed, ok := ps.items.([]T); ok {
				snap = typed
				offset = cur.Offset
				snapID = cur.SnapID
				ps.timer.Reset(h.paginationSnapshotTTL())
			}
		}
	}

	// No valid snapshot -- use items directly or create a snapshot if multi-page.
	if snap == nil {
		offset = 0
		// Single page -- no snapshot needed.
		if limit <= 0 || len(items) <= limit {
			resp := make([]R, len(items))
			for i := range items {
				resp[i] = mapFn(&items[i])
			}
			return c.JSON(http.StatusOK, wrapFn(resp, len(items), ""))
		}
		if int(h.snapshotCount.Load()) >= h.paginationMaxSnapshots() {
			resp := make([]R, len(items))
			for i := range items {
				resp[i] = mapFn(&items[i])
			}
			return c.JSON(http.StatusOK, wrapFn(resp, len(items), ""))
		}
		snapID = generateSnapID()
		snap = make([]T, len(items))
		copy(snap, items)
		ps := &pageSnapshot{items: snap}
		ps.timer = time.AfterFunc(h.paginationSnapshotTTL(), func() {
			h.deleteSnapshot(snapID)
		})
		h.pageSnapshots.Store(snapID, ps)
		h.snapshotCount.Add(1)
	}

	total := len(snap)
	if offset >= total {
		resp := make([]R, 0)
		return c.JSON(http.StatusOK, wrapFn(resp, total, ""))
	}

	remaining := snap[offset:]
	var next string
	if limit > 0 && len(remaining) > limit {
		remaining = remaining[:limit]
		next = encodeCursor(pageCursor{SnapID: snapID, Offset: offset + limit})
	}

	resp := make([]R, len(remaining))
	for i := range remaining {
		resp[i] = mapFn(&remaining[i])
	}
	return c.JSON(http.StatusOK, wrapFn(resp, total, next))
}

// Handler serves the agent REST API.
type Handler struct {
	Store                  *storage.Storage
	DefaultPageLimit       int
	PaginationSnapshotTTL  time.Duration
	PaginationMaxSnapshots int
	pageSnapshots          sync.Map
	snapshotCount          atomic.Int32
}

// ServeLanding returns a handler that serves a plain-text landing page at /.
func ServeLanding(version, commit string, swaggerEnabled bool) echo.HandlerFunc {
	swagger := ""
	if swaggerEnabled {
		swagger = "\n  /swagger.json   OpenAPI spec"
	}

	body := fmt.Sprintf(`
  _____ _____ _____ _____      _   _ _____ _____     _____ _____ _____
 | __  |_   _| __  |   __|   | \ | |   __|   __|   |     |   __|     |
 | __ -| | | |    -|   __|   |  \| |   __|__   |   |   --|__   |-   -|
 |_____| |_| |__|__|__|      |_|\__|__|  |_____|   |_____|_____|_____|

  %s (%s)

  CSI driver for dynamic btrfs volume provisioning
  with NFS export, snapshot, and clone support.

  https://github.com/erikmagkekse/btrfs-nfs-csi

  /healthz          Health check%s
`, version, commit, swagger)

	return func(c *echo.Context) error {
		return c.String(http.StatusOK, body)
	}
}
