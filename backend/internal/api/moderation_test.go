package api

import (
	"testing"

	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/model"
)

// TestCanModeratePackage covers the branches that don't require a GitHub call:
// nil viewer, site admin, direct owner, and a non-owner without a token.
func TestCanModeratePackage(t *testing.T) {
	app := &App{}
	pkg := model.Package{Short: "wasi", OwnerLogin: "wago-org"}

	if app.canModeratePackage(nil, pkg) {
		t.Fatal("nil viewer must not moderate")
	}
	if !app.canModeratePackage(&model.User{Login: "someone", Admin: true}, pkg) {
		t.Fatal("site admin must moderate any package")
	}
	if !app.canModeratePackage(&model.User{Login: "wago-org"}, pkg) {
		t.Fatal("direct owner must moderate")
	}
	if !app.canModeratePackage(&model.User{Login: "WAGO-ORG"}, pkg) {
		t.Fatal("owner match must be case-insensitive")
	}
	if app.canModeratePackage(&model.User{Login: "stranger"}, pkg) {
		t.Fatal("non-owner without a token must not moderate")
	}
	if app.canModeratePackage(&model.User{Login: "x"}, model.Package{Short: "p"}) {
		t.Fatal("no owner + non-admin must not moderate")
	}
}

// TestIsAdmin verifies the config admin-login parsing/matching.
func TestIsAdmin(t *testing.T) {
	c := config.Config{AdminLogins: []string{"alice", "Bob"}}
	if !c.IsAdmin("alice") || !c.IsAdmin("ALICE") || !c.IsAdmin("bob") {
		t.Fatal("expected listed logins (case-insensitive) to be admins")
	}
	if c.IsAdmin("carol") || c.IsAdmin("") {
		t.Fatal("unlisted login must not be admin")
	}
}
