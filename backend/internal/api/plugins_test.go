package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

func testChecksum(seed byte) string {
	return "h1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(seed), 32)))
}

func testProvider(t *testing.T, module, id, version string, mutate func(*model.PluginDefinition)) model.PublishedProvider {
	t.Helper()
	definition := model.PluginDefinition{
		ID: id, Name: id, Version: version, Stability: model.Stable,
		Provenance: model.PluginProvenance{Repository: "https://github.com/acme/source", License: "Apache-2.0"},
	}
	if mutate != nil {
		mutate(&definition)
	}
	digest, err := model.DefinitionDigest(definition)
	if err != nil {
		t.Fatalf("definition digest: %v", err)
	}
	return model.PublishedProvider{
		ID: id, ImportPath: module + "/register", Definition: definition, DefinitionDigest: digest,
		Source: model.PluginSource{Module: module, Version: version, Checksum: testChecksum('a')},
	}
}

func storeProviderRelease(t *testing.T, app *App, provider model.PublishedProvider, deprecated bool) {
	t.Helper()
	release := model.Version{Version: provider.Definition.Version, Providers: []model.PublishedProvider{provider}, SourceChecksum: provider.Source.Checksum, Deprecated: deprecated}
	release.ReleaseFingerprint = releaseFingerprint(provider.Source.Module, release)
	key := provider.Source.Module
	pkg, ok := app.Store.GetPackage(key)
	if !ok {
		pkg = model.Package{Name: provider.Source.Module, Short: key}
	}
	pkg.Versions = append(pkg.Versions, release)
	if err := app.Store.UpsertPackage(pkg); err != nil {
		t.Fatalf("store release: %v", err)
	}
}

func storeProviderBundleRelease(t *testing.T, app *App, module, version string, providers []model.PublishedProvider) {
	t.Helper()
	if len(providers) == 0 {
		t.Fatal("store bundle release: no providers")
	}
	release := model.Version{
		Version: version, Providers: append([]model.PublishedProvider(nil), providers...),
		SourceChecksum: providers[0].Source.Checksum,
	}
	release.ReleaseFingerprint = releaseFingerprint(module, release)
	pkg, ok := app.Store.GetPackage(module)
	if !ok {
		pkg = model.Package{Name: module, Short: module}
	}
	pkg.Versions = append(pkg.Versions, release)
	if err := app.Store.UpsertPackage(pkg); err != nil {
		t.Fatalf("store bundle release: %v", err)
	}
}

func TestPluginCandidatesReturnsVersionSpecificResolutionNewestFirst(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/workers"
	for _, version := range []string{"0.9.0", "0.10.0", "0.11.0"} {
		provider := testProvider(t, module, module, version, func(def *model.PluginDefinition) {
			def.Authorities = []model.AuthorityRequest{{
				Name: "instance.manage", Mode: "required", Reason: "Own bounded workers",
				Scope: model.AuthorityScope{MaxInstances: 4, MaxMemoryBytes: 32 << 20},
			}}
			def.Provides = []model.ContractSpec{{ID: "github.com/acme/workers/service", Major: 1}}
		})
		storeProviderRelease(t, app, provider, version == "0.11.0")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/candidates?id="+url.QueryEscape(module)+"&range="+url.QueryEscape(">=0.9.0")+"&range="+url.QueryEscape("<0.11.0"), nil)
	result := httptest.NewRecorder()
	app.NewRouter().ServeHTTP(result, request)
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", result.Code, result.Body.String())
	}
	var body struct {
		Plugins []pluginResolution `json:"plugins"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var exactEnvelope map[string]json.RawMessage
	if err := json.Unmarshal(result.Body.Bytes(), &exactEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(exactEnvelope) != 4 || exactEnvelope["plugins"] == nil || exactEnvelope["total"] == nil || exactEnvelope["offset"] == nil || exactEnvelope["limit"] == nil {
		t.Fatalf("candidate envelope keys = %v", exactEnvelope)
	}
	var exactPlugins []map[string]json.RawMessage
	if err := json.Unmarshal(exactEnvelope["plugins"], &exactPlugins); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"id", "version", "source", "provider", "definition", "definitionDigest", "releaseFingerprint"}
	if len(exactPlugins) == 0 || len(exactPlugins[0]) != len(wantKeys) {
		t.Fatalf("candidate keys = %v, want %v", exactPlugins, wantKeys)
	}
	for _, key := range wantKeys {
		if exactPlugins[0][key] == nil {
			t.Fatalf("candidate omitted %q: %v", key, exactPlugins[0])
		}
	}
	if len(body.Plugins) != 2 || body.Plugins[0].Version != "0.10.0" || body.Plugins[1].Version != "0.9.0" {
		t.Fatalf("candidate order = %+v", body.Plugins)
	}
	first := body.Plugins[0]
	if first.Source.Module != module || first.Source.Checksum == "" || first.Provider["importPath"] != module+"/register" || first.DefinitionDigest == "" || first.ReleaseFingerprint == "" {
		t.Fatalf("resolution omitted immutable source/provider fields: %+v", first)
	}
	if len(first.Definition.Authorities) != 1 || first.Definition.Authorities[0].Scope.MaxInstances != 4 || len(first.Definition.Provides) != 1 {
		t.Fatalf("resolution omitted exact authority/contract metadata: %+v", first)
	}
}

func TestPluginCandidatesReportsVersionConflictAndMissingPlugin(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/workers"
	storeProviderRelease(t, app, testProvider(t, module, module, "1.0.0", nil), false)
	for _, tc := range []struct {
		id, ranges string
		want       int
	}{
		{module, "&range=%5E1.0.0&range=%3E%3D2.0.0", http.StatusUnprocessableEntity},
		{"github.com/acme/missing", "", http.StatusNotFound},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/candidates?id="+url.QueryEscape(tc.id)+tc.ranges, nil)
		result := httptest.NewRecorder()
		app.NewRouter().ServeHTTP(result, request)
		if result.Code != tc.want {
			t.Errorf("%s status = %d, want %d; body = %s", tc.id, result.Code, tc.want, result.Body.String())
		}
	}
}

func TestPluginCatalogQueryFailsClosed(t *testing.T) {
	app := newSessionApp(t)
	for _, path := range []string{
		"/api/v1/plugins/candidates",
		"/api/v1/plugins/candidates?id=github.com%2Facme%2Fplugin&id=github.com%2Facme%2Fother",
		"/api/v1/plugins/candidates?id=github.com%2Facme%2Fplugin&range=",
		"/api/v1/plugins/candidates?id=github.com%2Facme%2Fplugin&future=true",
		"/api/v1/plugins/resolve?id=github.com%2Facme%2Fplugin&limit=1",
	} {
		result := httptest.NewRecorder()
		app.NewRouter().ServeHTTP(result, httptest.NewRequest(http.MethodGet, path, nil))
		if result.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400; body = %s", path, result.Code, result.Body.String())
		}
	}
}

func TestPluginCandidatesExcludesDeprecatedUnlessRequested(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/deprecated"
	storeProviderRelease(t, app, testProvider(t, module, module, "1.0.0", nil), true)
	for query, want := range map[string]int{
		"":                        http.StatusUnprocessableEntity,
		"&includeDeprecated=true": http.StatusOK,
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/candidates?id="+url.QueryEscape(module)+query, nil)
		result := httptest.NewRecorder()
		app.NewRouter().ServeHTTP(result, request)
		if result.Code != want {
			t.Errorf("query %q status = %d, want %d; body = %s", query, result.Code, want, result.Body.String())
		}
	}
}

func TestPublishGraphDoesNotResolveDependenciesThroughDeprecatedReleases(t *testing.T) {
	app := newSessionApp(t)
	dependencyID := "github.com/acme/deprecated-dependency"
	storeProviderRelease(t, app, testProvider(t, dependencyID, dependencyID, "1.0.0", nil), true)
	rootID := "github.com/acme/new-consumer"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: dependencyID, Version: "1.x"}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{root}); err == nil || !strings.Contains(err.Error(), "no version satisfying") {
		t.Fatalf("deprecated-only dependency error = %v", err)
	}
}

func TestPluginCandidatesPaginatesWithoutHidingTotal(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/paged"
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		storeProviderRelease(t, app, testProvider(t, module, module, version, nil), false)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/candidates?id="+url.QueryEscape(module)+"&limit=1&offset=1", nil)
	result := httptest.NewRecorder()
	app.NewRouter().ServeHTTP(result, request)
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", result.Code, result.Body.String())
	}
	var body struct {
		Plugins    []pluginResolution `json:"plugins"`
		Total      int                `json:"total"`
		Offset     int                `json:"offset"`
		Limit      int                `json:"limit"`
		NextOffset int                `json:"nextOffset"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 3 || body.Offset != 1 || body.Limit != 1 || body.NextOffset != 2 || len(body.Plugins) != 1 || body.Plugins[0].Version != "1.1.0" {
		t.Fatalf("page = %+v", body)
	}
}

func TestPluginCandidatesRejectsCorruptedStoredReleaseMetadata(t *testing.T) {
	module := "github.com/acme/corrupted"
	for _, mutate := range []func(*model.Package){
		func(pkg *model.Package) { pkg.Name = "github.com/acme/not-the-module" },
		func(pkg *model.Package) { pkg.Versions[0].SourceChecksum = testChecksum('b') },
		func(pkg *model.Package) { pkg.Versions[0].ReleaseFingerprint = "sha256:" + strings.Repeat("0", 64) },
	} {
		app := newSessionApp(t)
		storeProviderRelease(t, app, testProvider(t, module, module, "1.0.0", nil), false)
		pkg, ok := app.Store.GetPackage(module)
		if !ok {
			t.Fatal("stored fixture is missing")
		}
		mutate(&pkg)
		if err := app.Store.UpsertPackage(pkg); err != nil {
			t.Fatalf("corrupt stored fixture: %v", err)
		}

		request := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/candidates?id="+url.QueryEscape(module), nil)
		result := httptest.NewRecorder()
		app.NewRouter().ServeHTTP(result, request)
		if result.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body = %s", result.Code, result.Body.String())
		}
	}
}

func TestPublishGraphValidatesPoolWorkersAndComponentContracts(t *testing.T) {
	app := newSessionApp(t)
	workersID := "github.com/acme/workers"
	workers := testProvider(t, workersID, workersID, "1.2.0", func(def *model.PluginDefinition) {
		def.Provides = []model.ContractSpec{{ID: workersID + "/service", Major: 1}}
	})
	storeProviderRelease(t, app, workers, false)

	poolID := "github.com/acme/pool"
	pool := testProvider(t, poolID, poolID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: workersID, Version: "^1.0.0"}}
		def.Consumes = []model.ContractRequirement{{ID: workersID + "/service", Major: 1, Mode: "required"}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{pool}); err != nil {
		t.Fatalf("Pool -> Workers graph rejected: %v", err)
	}

	componentID := "github.com/acme/component-model"
	component := testProvider(t, componentID, componentID, "0.2.0", func(def *model.PluginDefinition) {
		def.Provides = []model.ContractSpec{{ID: componentID + "/service", Major: 1}}
	})
	storeProviderRelease(t, app, component, false)
	consumerID := "github.com/acme/component-consumer"
	consumer := testProvider(t, consumerID, consumerID, "0.1.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: componentID, Version: "0.x"}}
		def.Consumes = []model.ContractRequirement{{ID: componentID + "/service", Major: 1, Mode: "required"}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{consumer}); err != nil {
		t.Fatalf("component contract graph rejected: %v", err)
	}
}

func TestPublishGraphResolvesProvidersFromSameNewRelease(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/bundle"
	serviceID := module + "/service"
	service := testProvider(t, module, serviceID, "1.0.0", func(def *model.PluginDefinition) {
		def.Provides = []model.ContractSpec{{ID: serviceID + "/contract", Major: 1}}
	})
	consumer := testProvider(t, module, module+"/consumer", "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: serviceID, Version: "1.x"}}
		def.Consumes = []model.ContractRequirement{{ID: serviceID + "/contract", Major: 1, Mode: "required"}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{service, consumer}); err != nil {
		t.Fatalf("same-release provider dependency rejected: %v", err)
	}
}

func TestPublishGraphAcceptsNormalizedSourceVersionPrefix(t *testing.T) {
	app := newSessionApp(t)
	provider := testProvider(t, "github.com/acme/prefixed", "github.com/acme/prefixed", "1.2.3", nil)
	provider.Source.Version = "v1.2.3"
	if err := app.validateProviderGraph([]model.PublishedProvider{provider}); err != nil {
		t.Fatalf("normalized source version prefix rejected: %v", err)
	}
}

func TestPublishGraphRejectsProviderIDClaimedByAnotherSourceModule(t *testing.T) {
	app := newSessionApp(t)
	id := "github.com/acme/source/plugin"
	existing := testProvider(t, "github.com/acme/source", id, "1.0.0", nil)
	storeProviderRelease(t, app, existing, false)
	claim := testProvider(t, id, id, "1.1.0", nil)
	if err := app.validateProviderGraph([]model.PublishedProvider{claim}); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("cross-source provider claim error = %v", err)
	}
}

func TestPublishGraphLeavesExactContractChoiceToLockBinding(t *testing.T) {
	app := newSessionApp(t)
	contractID := "github.com/acme/service/contract"
	dependencies := []model.PluginRequirement{}
	for _, name := range []string{"provider-a", "provider-b"} {
		id := "github.com/acme/" + name
		provider := testProvider(t, id, id, "1.0.0", func(def *model.PluginDefinition) {
			def.Provides = []model.ContractSpec{{ID: contractID, Major: 1}}
		})
		storeProviderRelease(t, app, provider, false)
		dependencies = append(dependencies, model.PluginRequirement{ID: id, Version: "1.x"})
	}
	for _, mode := range []string{"required", "optional", "many"} {
		rootID := "github.com/acme/consumer-" + mode
		root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
			def.Requires = append([]model.PluginRequirement(nil), dependencies...)
			def.Consumes = []model.ContractRequirement{{ID: contractID, Major: 1, Mode: mode}}
		})
		if err := app.validateProviderGraph([]model.PublishedProvider{root}); err != nil {
			t.Fatalf("%s definition graph rejected multiple provider candidates before lock binding: %v", mode, err)
		}
	}
}

func TestPublishGraphRejectsMissingContractCycleAndDiamondConflict(t *testing.T) {
	app := newSessionApp(t)
	rootID := "github.com/acme/root"
	missing := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Consumes = []model.ContractRequirement{{ID: "github.com/acme/service", Major: 1, Mode: "required"}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{missing}); err == nil || !strings.Contains(err.Error(), "required contract") {
		t.Fatalf("missing contract error = %v", err)
	}

	aID, bID := "github.com/acme/a", "github.com/acme/b"
	a := testProvider(t, aID, aID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: bID, Version: "1.x"}}
	})
	b := testProvider(t, bID, bID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: aID, Version: "1.x"}}
	})
	storeProviderRelease(t, app, a, false)
	storeProviderRelease(t, app, b, false)
	if err := app.validateProviderGraph([]model.PublishedProvider{a}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}

	sharedID := "github.com/acme/shared"
	for _, version := range []string{"1.0.0", "2.0.0"} {
		storeProviderRelease(t, app, testProvider(t, sharedID, sharedID, version, nil), false)
	}
	leftID, rightID := "github.com/acme/left", "github.com/acme/right"
	left := testProvider(t, leftID, leftID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: sharedID, Version: "1.x"}}
	})
	right := testProvider(t, rightID, rightID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: sharedID, Version: "2.x"}}
	})
	storeProviderRelease(t, app, left, false)
	storeProviderRelease(t, app, right, false)
	diamond := testProvider(t, "github.com/acme/diamond", "github.com/acme/diamond", "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: leftID, Version: "1.x"}, {ID: rightID, Version: "1.x"}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{diamond}); err == nil || !strings.Contains(err.Error(), "no version satisfying") {
		t.Fatalf("diamond conflict error = %v", err)
	}
}

func TestPublishGraphRejectsCycleCreatedByRequiredContractBinding(t *testing.T) {
	app := newSessionApp(t)
	contractID := "github.com/acme/service/contract"
	providerID, consumerID := "github.com/acme/provider", "github.com/acme/consumer"
	provider := testProvider(t, providerID, providerID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: consumerID, Version: "1.x"}}
		def.Provides = []model.ContractSpec{{ID: contractID, Major: 1}}
	})
	consumer := testProvider(t, consumerID, consumerID, "1.0.0", func(def *model.PluginDefinition) {
		def.Consumes = []model.ContractRequirement{{ID: contractID, Major: 1, Mode: "required"}}
	})
	err := app.validateProviderGraph([]model.PublishedProvider{provider, consumer})
	if err == nil || !strings.Contains(err.Error(), "no acyclic exact binding") {
		t.Fatalf("contract binding cycle error = %v", err)
	}
}

func TestPublishGraphBacktracksToAcyclicContractProvider(t *testing.T) {
	app := newSessionApp(t)
	contractID := "github.com/acme/service/contract"
	providerAID := "github.com/acme/a"
	consumerID := "github.com/acme/consumer"
	providerBID := "github.com/acme/b"
	providerA := testProvider(t, providerAID, providerAID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: consumerID, Version: "1.x"}}
		def.Provides = []model.ContractSpec{{ID: contractID, Major: 1}}
	})
	consumer := testProvider(t, consumerID, consumerID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: providerBID, Version: "1.x"}}
		def.Consumes = []model.ContractRequirement{{ID: contractID, Major: 1, Mode: "required"}}
	})
	providerB := testProvider(t, providerBID, providerBID, "1.0.0", func(def *model.PluginDefinition) {
		def.Provides = []model.ContractSpec{{ID: contractID, Major: 1}}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{providerA, consumer, providerB}); err != nil {
		t.Fatalf("acyclic alternate contract provider rejected: %v", err)
	}
}

func TestPublishGraphValidatesRequiredContractsForEverySelectedDependency(t *testing.T) {
	app := newSessionApp(t)
	serviceID := "github.com/acme/transitive-service"
	service := testProvider(t, serviceID, serviceID, "1.0.0", nil)
	storeProviderRelease(t, app, service, false)

	middleID := "github.com/acme/transitive-consumer"
	middle := testProvider(t, middleID, middleID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: serviceID, Version: "1.x"}}
		def.Consumes = []model.ContractRequirement{{ID: serviceID + "/contract", Major: 1, Mode: "required"}}
	})
	storeProviderRelease(t, app, middle, false)

	rootID := "github.com/acme/transitive-root"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: middleID, Version: "1.x"}}
	})
	err := app.validateProviderGraph([]model.PublishedProvider{root})
	if err == nil || !strings.Contains(err.Error(), "plugin "+middleID+" required contract") {
		t.Fatalf("transitive required-contract error = %v", err)
	}
}

func TestPublishGraphDoesNotUseUnselectedCatalogContractProvider(t *testing.T) {
	app := newSessionApp(t)
	contractID := "github.com/acme/unselected-contract"
	unselectedID := "github.com/acme/unselected-provider"
	unselected := testProvider(t, unselectedID, unselectedID, "1.0.0", func(def *model.PluginDefinition) {
		def.Provides = []model.ContractSpec{{ID: contractID, Major: 1}}
	})
	storeProviderRelease(t, app, unselected, false)

	rootID := "github.com/acme/unselected-consumer"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Consumes = []model.ContractRequirement{{ID: contractID, Major: 1, Mode: "required"}}
	})
	err := app.validateProviderGraph([]model.PublishedProvider{root})
	if err == nil || !strings.Contains(err.Error(), "selected dependency graph") {
		t.Fatalf("unselected catalog provider error = %v", err)
	}
}

func TestPublishGraphBacktracksForTransitiveRequiredContract(t *testing.T) {
	app := newSessionApp(t)
	serviceID := "github.com/acme/contract-backtrack-service"
	contractID := serviceID + "/contract"
	older := testProvider(t, serviceID, serviceID, "1.0.0", func(def *model.PluginDefinition) {
		def.Provides = []model.ContractSpec{{ID: contractID, Major: 1}}
	})
	newer := testProvider(t, serviceID, serviceID, "1.1.0", nil)
	storeProviderRelease(t, app, older, false)
	storeProviderRelease(t, app, newer, false)

	middleID := "github.com/acme/contract-backtrack-consumer"
	middle := testProvider(t, middleID, middleID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: serviceID, Version: "1.x"}}
		def.Consumes = []model.ContractRequirement{{ID: contractID, Major: 1, Mode: "required"}}
	})
	storeProviderRelease(t, app, middle, false)

	rootID := "github.com/acme/contract-backtrack-root"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: middleID, Version: "1.x"}}
	})
	selected, err := resolveDefinitionGraph(app.catalogProviders(), root)
	if err != nil {
		t.Fatalf("contract-aware backtracking rejected valid graph: %v", err)
	}
	if got := selected[serviceID].provider.Definition.Version; got != "1.0.0" {
		t.Fatalf("selected service version = %s, want contract-compatible 1.0.0", got)
	}
}

func TestPublishGraphBacktracksFromNewestConflictingDependency(t *testing.T) {
	app := newSessionApp(t)
	sharedID := "github.com/acme/shared-backtrack"
	for _, version := range []string{"1.0.0", "2.0.0"} {
		storeProviderRelease(t, app, testProvider(t, sharedID, sharedID, version, nil), false)
	}

	leftID := "github.com/acme/left-backtrack"
	leftOlder := testProvider(t, leftID, leftID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: sharedID, Version: "1.x"}}
	})
	leftNewest := testProvider(t, leftID, leftID, "1.1.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: sharedID, Version: "2.x"}}
	})
	storeProviderRelease(t, app, leftOlder, false)
	storeProviderRelease(t, app, leftNewest, false)

	rightID := "github.com/acme/right-backtrack"
	right := testProvider(t, rightID, rightID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: sharedID, Version: "1.x"}}
	})
	storeProviderRelease(t, app, right, false)

	rootID := "github.com/acme/root-backtrack"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{
			{ID: leftID, Version: "1.x"},
			{ID: rightID, Version: "1.x"},
		}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{root}); err != nil {
		t.Fatalf("valid graph requiring an older dependency release was rejected: %v", err)
	}
}

func TestPublishGraphRejectsImpossibleGraphAfterTryingEveryCandidate(t *testing.T) {
	app := newSessionApp(t)
	sharedID := "github.com/acme/shared-impossible"
	for _, version := range []string{"1.0.0", "2.0.0"} {
		storeProviderRelease(t, app, testProvider(t, sharedID, sharedID, version, nil), false)
	}

	leftID := "github.com/acme/left-impossible"
	for _, test := range []struct {
		version, sharedRange string
	}{{"1.0.0", "1.x"}, {"2.0.0", "2.x"}} {
		provider := testProvider(t, leftID, leftID, test.version, func(def *model.PluginDefinition) {
			def.Requires = []model.PluginRequirement{{ID: sharedID, Version: test.sharedRange}}
		})
		storeProviderRelease(t, app, provider, false)
	}
	rightID := "github.com/acme/right-impossible"
	right := testProvider(t, rightID, rightID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{{ID: sharedID, Version: "3.x"}}
	})
	storeProviderRelease(t, app, right, false)

	rootID := "github.com/acme/root-impossible"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{
			{ID: leftID, Version: "*"},
			{ID: rightID, Version: "1.x"},
		}
	})
	err := app.validateProviderGraph([]model.PublishedProvider{root})
	if err == nil || !strings.Contains(err.Error(), "no version satisfying") {
		t.Fatalf("impossible graph error = %v", err)
	}
}

func TestPublishGraphRequiresOneReleasePerSourceModule(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/bundle-conflict"
	leftID, rightID := module+"/left", module+"/right"
	for _, version := range []string{"1.0.0", "2.0.0"} {
		left := testProvider(t, module, leftID, version, nil)
		right := testProvider(t, module, rightID, version, nil)
		storeProviderBundleRelease(t, app, module, version, []model.PublishedProvider{left, right})
	}

	rootID := "github.com/acme/root-source-conflict"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{
			{ID: leftID, Version: "1.x"},
			{ID: rightID, Version: "2.x"},
		}
	})
	err := app.validateProviderGraph([]model.PublishedProvider{root})
	if err == nil || !strings.Contains(err.Error(), "conflicting releases for source module "+module) {
		t.Fatalf("same-source release conflict error = %v", err)
	}
}

func TestPublishGraphAcceptsProvidersFromOnePersistedSourceRelease(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/bundle-one-release"
	leftID, rightID := module+"/left", module+"/right"
	left := testProvider(t, module, leftID, "1.0.0", nil)
	right := testProvider(t, module, rightID, "1.0.0", nil)
	storeProviderBundleRelease(t, app, module, "1.0.0", []model.PublishedProvider{left, right})

	rootID := "github.com/acme/root-one-release"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{
			{ID: leftID, Version: "1.x"},
			{ID: rightID, Version: "1.x"},
		}
	})
	if err := app.validateProviderGraph([]model.PublishedProvider{root}); err != nil {
		t.Fatalf("providers from one immutable source release were rejected: %v", err)
	}
}

func TestPublishGraphRejectsSameSourceReleaseFingerprintMismatch(t *testing.T) {
	app := newSessionApp(t)
	module := "github.com/acme/bundle-fingerprint-conflict"
	leftID, rightID := module+"/left", module+"/right"
	storeProviderRelease(t, app, testProvider(t, module, leftID, "1.0.0", nil), false)
	storeProviderRelease(t, app, testProvider(t, module, rightID, "1.0.0", nil), false)

	rootID := "github.com/acme/root-fingerprint-conflict"
	root := testProvider(t, rootID, rootID, "1.0.0", func(def *model.PluginDefinition) {
		def.Requires = []model.PluginRequirement{
			{ID: leftID, Version: "1.x"},
			{ID: rightID, Version: "1.x"},
		}
	})
	err := app.validateProviderGraph([]model.PublishedProvider{root})
	if err == nil || !strings.Contains(err.Error(), "conflicting releases for source module "+module) {
		t.Fatalf("same-source fingerprint conflict error = %v", err)
	}
}
