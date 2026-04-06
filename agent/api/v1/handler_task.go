package v1

import (
	"net/http"
	"sort"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/labstack/echo/v5"
)

func formatTimeout(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func taskResponseFrom(t *task.Task) models.TaskResponse {
	return models.TaskResponse{
		ID:          t.ID,
		Type:        t.Type,
		CreatedBy:   t.Labels[config.LabelCreatedBy],
		Status:      string(t.Status),
		Progress:    t.Progress,
		Opts:        t.Opts,
		Timeout:     formatTimeout(t.Timeout),
		Error:       t.Error,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
	}
}

func taskDetailResponseFrom(t *task.Task) models.TaskDetailResponse {
	return models.TaskDetailResponse{
		ID:          t.ID,
		Type:        t.Type,
		CreatedBy:   t.Labels[config.LabelCreatedBy],
		Status:      string(t.Status),
		Progress:    t.Progress,
		Opts:        t.Opts,
		Labels:      t.Labels,
		Timeout:     formatTimeout(t.Timeout),
		Result:      t.Result,
		Error:       t.Error,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
	}
}

// CreateTask godoc
// @Summary      Create a background task
// @Description  Creates a background task (scrub or test). Returns 202 Accepted with task ID.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        type    path string true "Task type" Enums(scrub, test)
// @Param        request body models.TaskCreateRequest false "Task options"
// @Success      202 {object} models.TaskCreateResponse
// @Failure      400 {object} models.ErrorResponse
// @Router       /v1/tasks/{type} [post]
// @Security     BearerAuth
func (h *Handler) CreateTask(c *echo.Context) error {
	taskType := c.Param("type")

	var req models.TaskCreateRequest
	if c.Request().ContentLength > 0 || c.Request().ContentLength == -1 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body: " + err.Error(), Code: storage.ErrInvalid})
		}
	}

	var timeout time.Duration
	if req.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(req.Timeout)
		if err != nil {
			return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid timeout: " + req.Timeout, Code: storage.ErrInvalid})
		}
	}

	if req.Labels[config.LabelCreatedBy] == "" {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "label \"" + config.LabelCreatedBy + "\" is required", Code: storage.ErrInvalid})
	}

	var taskID string
	var err error
	switch taskType {
	case models.TaskTypeScrub:
		taskID, err = h.Store.StartScrub(c.Request().Context(), req.Opts, req.Labels, timeout)
	case models.TaskTypeTest:
		taskID, err = h.Store.StartTestTask(c.Request().Context(), req.Opts, req.Labels, timeout)
	default:
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "unknown task type: " + taskType, Code: storage.ErrInvalid})
	}
	if err != nil {
		return StorageError(c, err)
	}
	return c.JSON(http.StatusAccepted, models.TaskCreateResponse{TaskID: taskID, Status: models.TaskStatusPending})
}

// ListTasks godoc
// @Summary      List tasks
// @Description  Returns background tasks. Filter by type. Supports detail, pagination, and label filtering.
// @Tags         tasks
// @Produce      json
// @Param        type   query string false "Filter by task type" Enums(scrub, test)
// @Param        detail query string false "Return full detail" Enums(true)
// @Param        limit  query int    false "Items per page (0 = pagination disabled)"
// @Param        after  query string false "Pagination cursor"
// @Param        label  query []string false "Label filter (key=value), repeatable"
// @Success      200 {object} models.TaskListResponse
// @Router       /v1/tasks [get]
// @Security     BearerAuth
func (h *Handler) ListTasks(c *echo.Context) error {
	tasks := h.Store.Tasks().List(c.QueryParam("type"))
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })

	if c.QueryParam("detail") == "true" {
		return paginatedList(h, c, tasks, taskDetailResponseFrom, func(r []models.TaskDetailResponse, total int, next string) any {
			return models.TaskDetailListResponse{Tasks: r, Total: total, Next: next}
		})
	}
	return paginatedList(h, c, tasks, taskResponseFrom, func(r []models.TaskResponse, total int, next string) any {
		return models.TaskListResponse{Tasks: r, Total: total, Next: next}
	})
}

// GetTask godoc
// @Summary      Get a task
// @Description  Returns detailed metadata for a single background task.
// @Tags         tasks
// @Produce      json
// @Param        id path string true "Task ID"
// @Success      200 {object} models.TaskDetailResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/tasks/{id} [get]
// @Security     BearerAuth
func (h *Handler) GetTask(c *echo.Context) error {
	t, err := h.Store.Tasks().Get(c.Param("id"))
	if err != nil {
		return StorageError(c, err)
	}
	return c.JSON(http.StatusOK, taskDetailResponseFrom(t))
}

// CancelTask godoc
// @Summary      Cancel a task
// @Description  Cancels a running or pending background task.
// @Tags         tasks
// @Param        id path string true "Task ID"
// @Success      204 "No Content"
// @Failure      404 {object} models.ErrorResponse
// @Router       /v1/tasks/{id} [delete]
// @Security     BearerAuth
func (h *Handler) CancelTask(c *echo.Context) error {
	if err := h.Store.Tasks().Cancel(c.Param("id")); err != nil {
		return StorageError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
