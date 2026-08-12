package api

import (
	"net/http"
	"strings"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// handleMyNotifications lists the current user's inbox — publish invites and
// ownership-transfer offers addressed to their GitHub login — newest first.
func (a *App) handleMyNotifications(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ns := a.Store.NotificationsForRecipient(u.Login)
	if ns == nil {
		ns = []model.Notification{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"notifications": ns})
}

// notification resolves the notification in the URL and returns it with the
// current user. Writes the error and returns ok=false on any failure.
func (a *App) notification(w http.ResponseWriter, r *http.Request) (model.Notification, *model.User, bool) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return model.Notification{}, nil, false
	}
	n, ok := a.Store.GetNotification(r.PathValue("id"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "notification not found")
		return model.Notification{}, nil, false
	}
	return n, u, true
}

// handleAcceptNotification accepts a pending invite (recipient only) and applies
// its effect: a publish invite adds the recipient to the package's allowed
// publishers; a transfer sets the recipient as the package owner.
func (a *App) handleAcceptNotification(w http.ResponseWriter, r *http.Request) {
	n, u, ok := a.notification(w, r)
	if !ok {
		return
	}
	// Only the addressed recipient may accept.
	if !strings.EqualFold(n.Recipient, u.Login) {
		httpx.WriteError(w, http.StatusForbidden, "this invite isn't addressed to you")
		return
	}
	if n.Status != model.NotifyPending {
		httpx.WriteError(w, http.StatusConflict, "this invite is no longer pending")
		return
	}
	unlockPackage := a.lockPackageWrite(n.PackageShort)
	defer unlockPackage()
	p, exists := a.Store.GetPackage(n.PackageShort)
	if !exists {
		// The package vanished; retire the invite so it stops showing.
		a.Store.SetNotificationStatus(n.ID, model.NotifyDeclined)
		httpx.WriteError(w, http.StatusNotFound, "that package no longer exists")
		return
	}

	switch n.Kind {
	case model.NotifyPublishInvite:
		if !containsFold(p.AllowedPublishers, u.Login) && !strings.EqualFold(p.OwnerLogin, u.Login) {
			p.AllowedPublishers = append(p.AllowedPublishers, u.Login)
			if err := a.Store.UpsertPackage(p); err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, "store error")
				return
			}
		}
	case model.NotifyTransfer:
		p.OwnerLogin = u.Login
		if err := a.Store.UpsertPackage(p); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "store error")
			return
		}
	}

	updated, _ := a.Store.SetNotificationStatus(n.ID, model.NotifyAccepted)
	httpx.WriteJSON(w, http.StatusOK, updated)
}

// handleDeclineNotification declines a pending invite. The addressed recipient
// declines it; a manager of the referenced package may also decline it to cancel
// an invite they sent.
func (a *App) handleDeclineNotification(w http.ResponseWriter, r *http.Request) {
	n, u, ok := a.notification(w, r)
	if !ok {
		return
	}
	mayDecline := strings.EqualFold(n.Recipient, u.Login)
	if !mayDecline {
		if p, exists := a.Store.GetPackage(n.PackageShort); exists && a.ownsPackage(u, p) {
			mayDecline = true
		}
	}
	if !mayDecline {
		httpx.WriteError(w, http.StatusForbidden, "not allowed to decline this invite")
		return
	}
	if n.Status != model.NotifyPending {
		httpx.WriteError(w, http.StatusConflict, "this invite is no longer pending")
		return
	}
	updated, _ := a.Store.SetNotificationStatus(n.ID, model.NotifyDeclined)
	httpx.WriteJSON(w, http.StatusOK, updated)
}

// inviteRequest is the body of POST /api/packages/{name}/publishers/invite.
type inviteRequest struct {
	Login string `json:"login"`
}

// handleInvitePublisher creates a pending publish invite for a GitHub login. The
// invitee must accept it (in their notifications) before they can publish. Owner
// / admin only.
func (a *App) handleInvitePublisher(w http.ResponseWriter, r *http.Request) {
	unlockPackage := a.lockPackageWrite(r.PathValue("name"))
	defer unlockPackage()
	p, ok := a.ownedPackage(w, r)
	if !ok {
		return
	}
	var req inviteRequest
	if err := decodeJSON(w, r, &req, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	login := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Login), "@"))
	if login == "" {
		httpx.WriteError(w, http.StatusBadRequest, "login is required")
		return
	}
	// Already an owner or accepted publisher — nothing to invite.
	if strings.EqualFold(login, p.OwnerLogin) || containsFold(p.AllowedPublishers, login) {
		httpx.WriteError(w, http.StatusConflict, "@"+login+" can already publish this package")
		return
	}
	// Don't stack duplicate pending invites for the same login.
	for _, n := range a.Store.PendingNotifications(p.Short, model.NotifyPublishInvite) {
		if strings.EqualFold(n.Recipient, login) {
			httpx.WriteError(w, http.StatusConflict, "@"+login+" already has a pending invite")
			return
		}
	}
	u := a.Sessions.CurrentUser(r)
	if _, err := a.Store.AddNotification(model.Notification{
		Recipient:    login,
		Kind:         model.NotifyPublishInvite,
		PackageShort: p.Short,
		PackageName:  p.Name,
		FromLogin:    u.Login,
	}); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decorateForViewer(p, u))
}
