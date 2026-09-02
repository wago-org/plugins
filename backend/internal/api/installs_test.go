package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

func TestRecordInstallByModulePathUpdatesCounts(t *testing.T) {
	app := newSessionApp(t)
	if err := app.Store.UpsertPackage(model.Package{
		Short: "github.com/wago-org/wasi",
		Name:  "github.com/wago-org/wasi",
	}); err != nil {
		t.Fatalf("upsert package: %v", err)
	}
	router := app.NewRouter()

	record := httptest.NewRequest(
		http.MethodPost,
		"/api/packages/github.com%2Fwago-org%2Fwasi/installs",
		strings.NewReader(`{"version":"v1.2.3"}`),
	)
	record.Header.Set("Content-Type", "application/json")
	recordResult := httptest.NewRecorder()
	router.ServeHTTP(recordResult, record)
	if recordResult.Code != http.StatusOK {
		t.Fatalf("record status = %d, want 200; body = %s", recordResult.Code, recordResult.Body.String())
	}

	var recorded struct {
		Total int `json:"installsTotal"`
		Week  int `json:"installsWeek"`
		Month int `json:"installsMonth"`
	}
	if err := json.Unmarshal(recordResult.Body.Bytes(), &recorded); err != nil {
		t.Fatalf("decode record response: %v", err)
	}
	if recorded.Total != 1 || recorded.Week != 1 || recorded.Month != 1 {
		t.Fatalf("recorded counts = total %d, week %d, month %d; want 1, 1, 1", recorded.Total, recorded.Week, recorded.Month)
	}

	history := httptest.NewRequest(
		http.MethodGet,
		"/api/packages/github.com%2Fwago-org%2Fwasi/installs?days=7",
		nil,
	)
	historyResult := httptest.NewRecorder()
	router.ServeHTTP(historyResult, history)
	if historyResult.Code != http.StatusOK {
		t.Fatalf("history status = %d, want 200; body = %s", historyResult.Code, historyResult.Body.String())
	}
	var loaded struct {
		Total int `json:"total"`
		Week  int `json:"week"`
		Month int `json:"month"`
	}
	if err := json.Unmarshal(historyResult.Body.Bytes(), &loaded); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if loaded.Total != 1 || loaded.Week != 1 || loaded.Month != 1 {
		t.Fatalf("loaded counts = total %d, week %d, month %d; want 1, 1, 1", loaded.Total, loaded.Week, loaded.Month)
	}
}
