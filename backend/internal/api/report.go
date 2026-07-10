package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// reportRequest is the body of POST /api/packages/{name}/report.
type reportRequest struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// handleReportPackage lets any signed-in user flag a package for moderation. The
// report is logged and emailed to the site admins; a takedown is a separate admin
// action (DELETE /api/packages/{name}).
func (a *App) handleReportPackage(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "sign in to report a package")
		return
	}
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	var req reportRequest
	if err := decodeJSON(w, r, &req, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		httpx.WriteError(w, http.StatusBadRequest, "a reason is required")
		return
	}
	detail := strings.TrimSpace(req.Detail)
	if _, err := a.Store.AddReport(p.Short, u.ID, u.Login, reason, detail); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	a.notifyReport(p, u, reason, detail)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleListReports returns the moderation queue (site admins only).
func (a *App) handleListReports(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil || !u.Admin {
		httpx.WriteError(w, http.StatusForbidden, "admins only")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"reports": a.Store.ListReports()})
}

// handleResolveReport marks a report resolved (site admins only).
func (a *App) handleResolveReport(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil || !u.Admin {
		httpx.WriteError(w, http.StatusForbidden, "admins only")
		return
	}
	rep, ok := a.Store.ResolveReport(r.PathValue("id"), u.Login)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "report not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rep)
}

// notifyReport records a report and best-effort emails every site admin.
func (a *App) notifyReport(p model.Package, reporter *model.User, reason, detail string) {
	log.Printf("package report: %s by @%s — %s %q", p.Short, reporter.Login, reason, detail)
	subject := fmt.Sprintf("[wago] report: %s (%s)", p.Short, reason)
	body := fmt.Sprintf("Package: %s (%s)\nReported by: @%s\nReason: %s\n\n%s\n\n%s/%s",
		p.Short, p.Name, reporter.Login, reason, detail, strings.TrimRight(a.Cfg.FrontendURL, "/"), p.Short)
	for _, login := range a.Cfg.AdminLogins {
		admin, ok := a.Store.GetUserByLogin(login)
		if !ok || admin.Email == "" {
			continue
		}
		if _, err := a.Email.Send(admin.Email, subject, body); err != nil {
			log.Printf("report notify %s: %v", admin.Email, err)
		}
	}
}
