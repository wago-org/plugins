package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/wago-org/registry-backend/internal/httpx"
)

// handleRecordInstall records one install for today (no auth) and returns the
// updated totals. An optional {"version":"v1.0.0"} body is accepted and ignored
// for now beyond validating JSON.
func (a *App) handleRecordInstall(w http.ResponseWriter, r *http.Request) {
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	// Body is optional; tolerate an empty body.
	if r.ContentLength != 0 {
		var in struct {
			Version string `json:"version"`
		}
		if err := decodeJSON(w, r, &in, 1<<12); err != nil && err.Error() != "EOF" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	today := time.Now().UTC().Format("2006-01-02")
	if err := a.Store.RecordInstall(p.Short, today); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	week := a.Store.InstallWeek(p.Short)
	month := a.Store.InstallMonth(p.Short)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"installsTotal":      a.Store.InstallTotal(p.Short),
		"installsWeek":       week,
		"installsWeekLabel":  compactCount(week),
		"installsMonth":      month,
		"installsMonthLabel": compactCount(month),
	})
}

// handleInstallSeries returns the daily install history for a package.
func (a *App) handleInstallSeries(w http.ResponseWriter, r *http.Request) {
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	days := 90
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}
	week := a.Store.InstallWeek(p.Short)
	month := a.Store.InstallMonth(p.Short)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"series":     a.Store.InstallSeries(p.Short, days),
		"total":      a.Store.InstallTotal(p.Short),
		"week":       week,
		"weekLabel":  compactCount(week),
		"month":      month,
		"monthLabel": compactCount(month),
	})
}
