package routes

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"

	"github.com/cdknorow/coral/internal/config"
	"github.com/cdknorow/coral/internal/store"
)

// ScheduleHandler handles scheduled jobs API endpoints.
type ScheduleHandler struct {
	sched *store.ScheduleStore
	cfg   *config.Config
}

func NewScheduleHandler(db *store.DB, cfg *config.Config) *ScheduleHandler {
	return &ScheduleHandler{
		sched: store.NewScheduleStore(db),
		cfg:   cfg,
	}
}

// ListJobs returns all scheduled jobs enriched with last_run and next_fire_at.
// GET /api/scheduled/jobs
func (h *ScheduleHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.sched.ListScheduledJobs(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if jobs == nil {
		jobs = []store.ScheduledJob{}
	}

	// Enrich each job with last_run and next_fire_at (matches Python behavior)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	type enrichedJob struct {
		store.ScheduledJob
		LastRun    *store.ScheduledRun `json:"last_run"`
		NextFireAt *string             `json:"next_fire_at"`
	}
	enriched := make([]enrichedJob, 0, len(jobs))
	for _, job := range jobs {
		ej := enrichedJob{ScheduledJob: job}
		lastRun, err := h.sched.GetLastRunForJob(r.Context(), job.ID)
		if err == nil {
			ej.LastRun = lastRun
		}
		s, err := parser.Parse(job.CronExpr)
		if err == nil {
			loc := time.UTC
			if job.Timezone != "" {
				if tz, err := time.LoadLocation(job.Timezone); err == nil {
					loc = tz
				}
			}
			next := s.Next(time.Now().In(loc))
			if !next.IsZero() {
				ts := next.In(loc).Format("2006-01-02T15:04:05+00:00")
				ej.NextFireAt = &ts
			}
		}
		enriched = append(enriched, ej)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": enriched})
}

// GetJob returns a single scheduled job.
// GET /api/scheduled/jobs/{jobID}
func (h *ScheduleHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID, _ := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	job, err := h.sched.GetScheduledJob(r.Context(), jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// CreateJob creates a new scheduled job.
// POST /api/scheduled/jobs
func (h *ScheduleHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	// Use pointer types for int fields so we can distinguish "not provided" (nil)
	// from "explicitly set to 0". Python defaults enabled=1 and cleanup_worktree=1.
	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		CronExpr        string `json:"cron_expr"`
		Timezone        string `json:"timezone"`
		AgentType       string `json:"agent_type"`
		RepoPath        string `json:"repo_path"`
		BaseBranch      string `json:"base_branch"`
		Prompt          string `json:"prompt"`
		Enabled         *int   `json:"enabled"`
		MaxDurationS    *int   `json:"max_duration_s"`
		CleanupWorktree *int   `json:"cleanup_worktree"`
		Flags           string `json:"flags"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Validate required fields (matches Python behavior)
	if body.Name == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "'name' is required"})
		return
	}
	if body.CronExpr == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "'cron_expr' is required"})
		return
	}
	if body.RepoPath == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "'repo_path' is required"})
		return
	}
	if body.Prompt == "" {
		writeJSON(w, http.StatusOK, map[string]string{"error": "'prompt' is required"})
		return
	}

	// Validate cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(body.CronExpr); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Invalid cron expression: " + body.CronExpr})
		return
	}

	// Build ScheduledJob with defaults matching Python
	job := store.ScheduledJob{
		Name:        body.Name,
		Description: body.Description,
		CronExpr:    body.CronExpr,
		Timezone:    body.Timezone,
		AgentType:   body.AgentType,
		RepoPath:    body.RepoPath,
		BaseBranch:  body.BaseBranch,
		Prompt:      body.Prompt,
		Flags:       body.Flags,
		Enabled:     intPtrOr(body.Enabled, 1),
		MaxDurationS:    intPtrOr(body.MaxDurationS, 3600),
		CleanupWorktree: intPtrOr(body.CleanupWorktree, 1),
	}
	if job.Timezone == "" {
		job.Timezone = "UTC"
	}
	if job.AgentType == "" {
		job.AgentType = "claude"
	}
	if job.BaseBranch == "" {
		job.BaseBranch = "main"
	}

	created, err := h.sched.CreateScheduledJob(r.Context(), &job)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// UpdateJob updates an existing scheduled job.
// PUT /api/scheduled/jobs/{jobID}
func (h *ScheduleHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID, _ := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	var fields map[string]interface{}
	if err := decodeJSON(r, &fields); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Validate cron expression if provided (matches Python behavior)
	if cronExpr, ok := fields["cron_expr"]; ok {
		if expr, ok := cronExpr.(string); ok {
			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
			if _, err := parser.Parse(expr); err != nil {
				writeJSON(w, http.StatusOK, map[string]string{"error": "Invalid cron expression: " + expr})
				return
			}
		}
	}

	updated, err := h.sched.UpdateScheduledJob(r.Context(), jobID, fields)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if updated == nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Job not found"})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DeleteJob deletes a scheduled job and its run history.
// DELETE /api/scheduled/jobs/{jobID}
func (h *ScheduleHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID, _ := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err := h.sched.DeleteScheduledJob(r.Context(), jobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ToggleJob pauses or resumes a scheduled job.
// POST /api/scheduled/jobs/{jobID}/toggle
func (h *ScheduleHandler) ToggleJob(w http.ResponseWriter, r *http.Request) {
	jobID, _ := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	job, err := h.sched.GetScheduledJob(r.Context(), jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	newEnabled := 1
	if job.Enabled == 1 {
		newEnabled = 0
	}
	updated, err := h.sched.UpdateScheduledJob(r.Context(), jobID, map[string]interface{}{"enabled": newEnabled})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// GetJobRuns returns run history for a job.
// GET /api/scheduled/jobs/{jobID}/runs
func (h *ScheduleHandler) GetJobRuns(w http.ResponseWriter, r *http.Request) {
	jobID, _ := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	limit := queryInt(r, "limit", 20)
	runs, err := h.sched.GetRunsForJob(r.Context(), jobID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []store.ScheduledRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// GetRecentRuns returns recent runs across all jobs.
// GET /api/scheduled/runs/recent
func (h *ScheduleHandler) GetRecentRuns(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	runs, err := h.sched.ListAllRecentRuns(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []store.ScheduledRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// ValidateCron validates a cron expression and returns next fire times.
// POST /api/scheduled/validate-cron
func (h *ScheduleHandler) ValidateCron(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CronExpr string `json:"cron_expr"`
		Timezone string `json:"timezone"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.CronExpr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cron_expr is required"})
		return
	}

	// Parse the cron expression (5-field format)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(body.CronExpr)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": "Invalid cron expression"})
		return
	}

	// Determine timezone
	loc := time.UTC
	if body.Timezone != "" {
		if tz, err := time.LoadLocation(body.Timezone); err == nil {
			loc = tz
		}
	}

	// Compute next 5 fire times
	now := time.Now().In(loc)
	var nextFires []string
	t := now
	for i := 0; i < 5; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		nextFires = append(nextFires, t.In(loc).Format("2006-01-02T15:04:05+00:00"))
	}

	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "next_fire_times": nextFires})
}
