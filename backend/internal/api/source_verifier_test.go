package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/registry-backend/internal/auth"
	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/model"
	"github.com/wago-org/registry-backend/internal/store"
)

const verifierModule = "github.com/acme/workers"
const verifierCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func verifierChecksum(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "h1:" + base64.StdEncoding.EncodeToString(digest[:])
}

func verifierProvider(t *testing.T) model.ProviderManifest {
	t.Helper()
	definition := model.PluginDefinition{
		ID:          verifierModule,
		Name:        "Workers",
		Version:     "0.1.0",
		Description: "Bounded workers",
		Stability:   model.Stable,
		Compatibility: model.Compatibility{
			Engines: map[string]string{},
		},
		Provenance: model.PluginProvenance{
			Repository: "https://github.com/acme/workers",
			License:    "Apache-2.0",
			Authors:    []string{"Acme"},
		},
		Authorities: []model.AuthorityRequest{{
			Name: "instance.manage", Mode: "required", Reason: "Own a bounded worker pool",
			Scope: model.AuthorityScope{MaxInstances: 8, MaxMemoryBytes: 64 << 20},
		}},
	}
	digest, err := model.DefinitionDigest(definition)
	if err != nil {
		t.Fatalf("definition digest: %v", err)
	}
	return model.ProviderManifest{
		ImportPath: verifierModule + "/register", Definition: definition, DefinitionDigest: digest,
	}
}

func verifierPackage() model.PackageManifest {
	return model.PackageManifest{
		Module: verifierModule, Version: "0.1.0", Name: "Workers", Description: "Bounded workers",
		Stability: model.Stable, License: "Apache-2.0", Repository: "https://github.com/acme/workers",
		Authors:  []model.PackageAuthor{{Name: "Acme", Github: "acme"}},
		Category: "runtime", Tags: []string{"workers", "bounded"},
		Engines: map[string]string{},
	}
}

type fakeGoCommandRunner struct {
	calls []goCommand
	run   func(goCommand) (goCommandResult, error)
}

func (f *fakeGoCommandRunner) Run(_ context.Context, command goCommand) (goCommandResult, error) {
	f.calls = append(f.calls, command)
	return f.run(command)
}

func environmentValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func artifactDownloadResult(t *testing.T, command goCommand, source model.PluginSource, packageMetadata model.PackageManifest, catalog []model.ProviderManifest, reportedChecksum string) goCommandResult {
	t.Helper()
	moduleCache := environmentValue(command.env, "GOMODCACHE")
	if moduleCache == "" {
		t.Fatal("verification command did not isolate GOMODCACHE")
	}
	moduleDir := filepath.Join(moduleCache, "github.com", "acme", "workers@v0.1.0")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatalf("create fake module directory: %v", err)
	}
	artifact, err := json.Marshal(sourceArtifactCatalog{Schema: sourceCatalogSchema, Providers: catalog})
	if err != nil {
		t.Fatalf("marshal fake source catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, sourceCatalogFilename), artifact, 0o600); err != nil {
		t.Fatalf("write fake source catalog: %v", err)
	}
	manifest, err := json.Marshal(model.Manifest{Schema: model.ManifestSchemaV1, Package: &packageMetadata})
	if err != nil {
		t.Fatalf("marshal fake source manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, sourceManifestFilename), manifest, 0o600); err != nil {
		t.Fatalf("write fake source manifest: %v", err)
	}
	report, err := json.Marshal(map[string]any{
		"Path": source.Module, "Version": source.Version, "Sum": reportedChecksum, "Dir": moduleDir,
		"Origin": map[string]string{
			"VCS": "git", "URL": packageMetadata.Repository, "Subdir": moduleReleaseSubdirectory(source.Module, source.Version),
			"Hash": verifierCommit, "Ref": "refs/tags/" + moduleReleaseTag(source.Module, source.Version),
			"TagPrefix": "", "TagSum": "", "RepoSum": "",
		},
	})
	if err != nil {
		t.Fatalf("marshal download result: %v", err)
	}
	return goCommandResult{stdout: report}
}

func TestGoSourceVerifierReadsExactArtifactWithoutExecutingPluginCode(t *testing.T) {
	checksum := verifierChecksum("exact source")
	source := model.PluginSource{Module: verifierModule, Version: "v0.1.0", Checksum: checksum}
	packageMetadata := verifierPackage()
	providers := []model.ProviderManifest{verifierProvider(t)}
	t.Setenv("WAGO_SOURCE_VERIFIER_TEST_SECRET", "must-not-leak")
	runner := &fakeGoCommandRunner{}
	runner.run = func(command goCommand) (goCommandResult, error) {
		if got, want := command.args, []string{"mod", "download", "-json", verifierModule + "@v0.1.0"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("go command = %v, want %v", got, want)
		}
		if environmentValue(command.env, "GOWORK") != "off" || environmentValue(command.env, "GOSUMDB") != "sum.golang.org" || environmentValue(command.env, "GOVCS") != "*:off" {
			t.Fatalf("verification environment is not fail-closed: %v", command.env)
		}
		for _, entry := range command.env {
			if strings.Contains(entry, "must-not-leak") || strings.HasPrefix(entry, "WAGO_SOURCE_VERIFIER_TEST_SECRET=") {
				t.Fatalf("host secret leaked into verification command: %q", entry)
			}
		}
		return artifactDownloadResult(t, command, source, packageMetadata, providers, checksum), nil
	}
	verifier := &goSourceVerifier{runner: runner, timeout: sourceVerificationTimeout}
	if err := verifier.Verify(context.Background(), source, verifierCommit, packageMetadata, providers); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("verification ran %d commands, want only go mod download", len(runner.calls))
	}
}

func TestGoSourceVerifierRejectsValidButWrongChecksum(t *testing.T) {
	submittedChecksum := verifierChecksum("submitted source")
	downloadedChecksum := verifierChecksum("downloaded source")
	source := model.PluginSource{Module: verifierModule, Version: "v0.1.0", Checksum: submittedChecksum}
	packageMetadata := verifierPackage()
	providers := []model.ProviderManifest{verifierProvider(t)}
	runner := &fakeGoCommandRunner{}
	runner.run = func(command goCommand) (goCommandResult, error) {
		return artifactDownloadResult(t, command, source, packageMetadata, providers, downloadedChecksum), nil
	}
	verifier := &goSourceVerifier{runner: runner, timeout: sourceVerificationTimeout}
	err := verifier.Verify(context.Background(), source, verifierCommit, packageMetadata, providers)
	if err == nil || !strings.Contains(err.Error(), "does not match submitted checksum") {
		t.Fatalf("Verify error = %v, want checksum mismatch", err)
	}
}

func TestGoSourceVerifierBindsArtifactOriginToResolvedTagCommit(t *testing.T) {
	checksum := verifierChecksum("exact source")
	source := model.PluginSource{Module: verifierModule, Version: "v0.1.0", Checksum: checksum}
	packageMetadata := verifierPackage()
	providers := []model.ProviderManifest{verifierProvider(t)}
	runner := &fakeGoCommandRunner{}
	runner.run = func(command goCommand) (goCommandResult, error) {
		return artifactDownloadResult(t, command, source, packageMetadata, providers, checksum), nil
	}
	verifier := &goSourceVerifier{runner: runner, timeout: sourceVerificationTimeout}
	err := verifier.Verify(context.Background(), source, strings.Repeat("b", 40), packageMetadata, providers)
	if err == nil || !strings.Contains(err.Error(), "downloaded source origin") {
		t.Fatalf("Verify error = %v, want origin commit mismatch", err)
	}
}

func TestGoSourceVerifierAcceptsDocumentedNestedModuleOrigin(t *testing.T) {
	checksum := verifierChecksum("nested exact source")
	source := model.PluginSource{Module: verifierModule + "/tools/v2", Version: "v2.1.0", Checksum: checksum}
	packageMetadata := verifierPackage()
	packageMetadata.Module = source.Module
	packageMetadata.Version = "2.1.0"
	provider := verifierProvider(t)
	provider.ImportPath = source.Module + "/register"
	provider.Definition.ID = source.Module
	provider.Definition.Version = "2.1.0"
	provider.DefinitionDigest, _ = model.DefinitionDigest(provider.Definition)
	runner := &fakeGoCommandRunner{}
	runner.run = func(command goCommand) (goCommandResult, error) {
		return artifactDownloadResult(t, command, source, packageMetadata, []model.ProviderManifest{provider}, checksum), nil
	}
	verifier := &goSourceVerifier{runner: runner, timeout: sourceVerificationTimeout}
	if err := verifier.Verify(context.Background(), source, verifierCommit, packageMetadata, []model.ProviderManifest{provider}); err != nil {
		t.Fatalf("Verify nested module: %v", err)
	}
}

func TestCanonicalArtifactCatalogRejectsNonCanonicalOrderAndDefinitions(t *testing.T) {
	source := model.PluginSource{Module: verifierModule, Version: "v0.1.0", Checksum: verifierChecksum("catalog")}
	root := verifierProvider(t)
	root.Definition, _ = model.CanonicalPluginDefinition(root.Definition)
	root.DefinitionDigest, _ = model.DefinitionDigest(root.Definition)
	child := root
	child.Definition.ID = verifierModule + "/metrics"
	child.Definition, _ = model.CanonicalPluginDefinition(child.Definition)
	child.DefinitionDigest, _ = model.DefinitionDigest(child.Definition)
	if err := validateCanonicalArtifactCatalog(source, []model.ProviderManifest{child, root}); err == nil || !strings.Contains(err.Error(), "canonical ID order") {
		t.Fatalf("reversed catalog error = %v", err)
	}

	noncanonical := root
	noncanonical.Definition.Provenance.Authors = []string{"Zulu", "Alpha"}
	noncanonical.DefinitionDigest, _ = model.DefinitionDigest(noncanonical.Definition)
	if err := validateCanonicalArtifactCatalog(source, []model.ProviderManifest{noncanonical}); err == nil || !strings.Contains(err.Error(), "definition is not canonical") {
		t.Fatalf("noncanonical definition error = %v", err)
	}
}

func TestGoSourceVerifierRejectsValidButWrongCatalog(t *testing.T) {
	checksum := verifierChecksum("exact source")
	source := model.PluginSource{Module: verifierModule, Version: "v0.1.0", Checksum: checksum}
	packageMetadata := verifierPackage()
	artifactProviders := []model.ProviderManifest{verifierProvider(t)}
	submittedProviders := []model.ProviderManifest{verifierProvider(t)}
	submittedProviders[0].Definition.Authorities[0].Reason = "A different but valid authority reason"
	digest, err := model.DefinitionDigest(submittedProviders[0].Definition)
	if err != nil {
		t.Fatalf("changed definition digest: %v", err)
	}
	submittedProviders[0].DefinitionDigest = digest
	runner := &fakeGoCommandRunner{}
	runner.run = func(command goCommand) (goCommandResult, error) {
		return artifactDownloadResult(t, command, source, packageMetadata, artifactProviders, checksum), nil
	}
	verifier := &goSourceVerifier{runner: runner, timeout: sourceVerificationTimeout}
	err = verifier.Verify(context.Background(), source, verifierCommit, packageMetadata, submittedProviders)
	if err == nil || !strings.Contains(err.Error(), "artifact definition digest") {
		t.Fatalf("Verify error = %v, want catalog mismatch", err)
	}
}

func TestGoSourceVerifierRejectsInventedPackageMetadata(t *testing.T) {
	checksum := verifierChecksum("exact source")
	source := model.PluginSource{Module: verifierModule, Version: "v0.1.0", Checksum: checksum}
	artifactPackage := verifierPackage()
	providers := []model.ProviderManifest{verifierProvider(t)}
	for name, mutate := range map[string]func(*model.PackageManifest){
		"category": func(pack *model.PackageManifest) { pack.Category = "host-imports" },
		"tags":     func(pack *model.PackageManifest) { pack.Tags = []string{"invented"} },
		"authors": func(pack *model.PackageManifest) {
			pack.Authors[0].Github = "invented-publisher"
		},
		"subpackages": func(pack *model.PackageManifest) {
			pack.Subpackages = []model.PackageSub{{
				Module: verifierModule + "/metrics", Name: "Metrics", Description: "Invented discovery entry",
				Stability: model.Stable,
			}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			submittedPackage := verifierPackage()
			mutate(&submittedPackage)
			runner := &fakeGoCommandRunner{}
			runner.run = func(command goCommand) (goCommandResult, error) {
				return artifactDownloadResult(t, command, source, artifactPackage, providers, checksum), nil
			}
			verifier := &goSourceVerifier{runner: runner, timeout: sourceVerificationTimeout}
			err := verifier.Verify(context.Background(), source, verifierCommit, submittedPackage, providers)
			if err == nil || !strings.Contains(err.Error(), "manifest.package does not match") {
				t.Fatalf("Verify error = %v, want exact package metadata mismatch", err)
			}
		})
	}
}

func TestCanonicalModuleVersionRequiresLowercaseVPrefix(t *testing.T) {
	for input, want := range map[string]string{"0.1.0": "v0.1.0", "v1.2.3-beta.1": "v1.2.3-beta.1"} {
		got, err := canonicalModuleVersion(input)
		if err != nil || got != want {
			t.Errorf("canonicalModuleVersion(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := canonicalModuleVersion("V1.2.3"); err == nil {
		t.Fatal("uppercase V version was accepted")
	}
}

func TestNewUsesRealSourceVerifierByDefault(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	app := New(config.Config{SessionSecret: []byte("source-verifier-default-test")}, dataStore)
	verifier, ok := app.sourceVerifier.(*goSourceVerifier)
	if !ok {
		t.Fatalf("New source verifier = %T, want *goSourceVerifier", app.sourceVerifier)
	}
	if _, ok := verifier.runner.(execGoCommandRunner); !ok {
		t.Fatalf("New source verifier runner = %T, want execGoCommandRunner", verifier.runner)
	}
}

type recordingSourceVerifier struct {
	source          model.PluginSource
	commit          string
	packageMetadata model.PackageManifest
	providers       []model.ProviderManifest
	err             error
	calls           int
}

type barrierSourceVerifier struct {
	arrived chan<- struct{}
	release <-chan struct{}
}

func (v barrierSourceVerifier) Verify(ctx context.Context, _ model.PluginSource, _ string, _ model.PackageManifest, _ []model.ProviderManifest) error {
	select {
	case v.arrived <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-v.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pairedCatalogSnapshotStore makes a missing catalog-write critical section
// deterministic: concurrent catalog readers receive the same pre-write
// snapshot. With the critical section in place, the first reader times out of
// the pairing window, persists its claim, and only then can the second read.
type pairedCatalogSnapshotStore struct {
	store.Store
	mu       sync.Mutex
	reads    int
	paired   chan struct{}
	snapshot []model.Package
}

func (s *pairedCatalogSnapshotStore) ListPackages() []model.Package {
	s.mu.Lock()
	read := s.reads
	s.reads++
	switch read {
	case 0:
		paired := s.paired
		s.mu.Unlock()
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-paired:
			s.mu.Lock()
			snapshot := append([]model.Package(nil), s.snapshot...)
			s.mu.Unlock()
			return snapshot
		case <-timer.C:
			return s.Store.ListPackages()
		}
	case 1:
		s.snapshot = s.Store.ListPackages()
		snapshot := append([]model.Package(nil), s.snapshot...)
		close(s.paired)
		s.mu.Unlock()
		return snapshot
	default:
		s.mu.Unlock()
		return s.Store.ListPackages()
	}
}

func (v *recordingSourceVerifier) Verify(_ context.Context, source model.PluginSource, commit string, packageMetadata model.PackageManifest, providers []model.ProviderManifest) error {
	v.calls++
	v.source = source
	v.commit = commit
	v.packageMetadata = packageMetadata
	v.providers = append([]model.ProviderManifest(nil), providers...)
	return v.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func rawPublishRequest(t *testing.T) publishRequest {
	t.Helper()
	provider := verifierProvider(t)
	return publishRequest{
		Manifest: model.Manifest{
			Schema:  model.ManifestSchemaV1,
			Package: func() *model.PackageManifest { pack := verifierPackage(); return &pack }(),
		},
		Version: "0.1.0", Checksum: verifierChecksum("exact source"), Commit: strings.Repeat("a", 40), Providers: []model.ProviderManifest{provider},
	}
}

func providerClaimPublishRequest(t *testing.T, module string, pack model.PackageManifest) publishRequest {
	t.Helper()
	providers := make([]model.ProviderManifest, 0, 1+len(pack.Subpackages))
	for _, metadata := range append([]model.PackageSub{{
		Module: pack.Module, Name: pack.Name, Description: pack.Description, Stability: pack.Stability,
		Engines: pack.Engines, Platforms: pack.Platforms,
	}}, pack.Subpackages...) {
		definition := model.PluginDefinition{
			ID: metadata.Module, Name: metadata.Name, Version: "0.1.0",
			Description: metadata.Description, Stability: metadata.Stability,
			Compatibility: model.Compatibility{Engines: metadata.Engines, Platforms: metadata.Platforms},
			Provenance: model.PluginProvenance{
				Repository: pack.Repository, Homepage: pack.Homepage, License: pack.License,
				Authors: []string{"Acme"},
			},
		}
		digest, err := model.DefinitionDigest(definition)
		if err != nil {
			t.Fatalf("definition digest for %s: %v", metadata.Module, err)
		}
		providers = append(providers, model.ProviderManifest{
			ImportPath: module + "/register", Definition: definition, DefinitionDigest: digest,
		})
	}
	return publishRequest{
		Manifest: model.Manifest{Schema: model.ManifestSchemaV1, Package: &pack},
		Version:  "0.1.0", Checksum: verifierChecksum(module), Commit: verifierCommit, Providers: providers,
	}
}

func serveRawPublish(t *testing.T, verifier SourceVerifier) (*httptest.ResponseRecorder, store.Store) {
	t.Helper()
	return serveRawPublishWithGitHubCommit(t, verifier, http.StatusOK, strings.Repeat("a", 40))
}

func serveRawPublishWithGitHubCommit(t *testing.T, verifier SourceVerifier, commitStatus int, commit string) (*httptest.ResponseRecorder, store.Store) {
	t.Helper()
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	user := model.User{ID: "publisher", Login: "acme", GitHubToken: "github-token"}
	if err := dataStore.UpsertUser(user); err != nil {
		t.Fatalf("store user: %v", err)
	}
	app := NewWithSourceVerifier(config.Config{
		SessionSecret: []byte("publish-source-verifier-test-secret"), DevMode: true, FrontendURL: "http://localhost:8000",
	}, dataStore, verifier)

	commitBody := []byte(commit)
	if commitStatus != http.StatusOK {
		commitBody = []byte(`{"message":"Not Found"}`)
	}
	const repoEndpoint = "https://api.github.com/repos/acme/workers"
	const commitEndpoint = repoEndpoint + "/commits/tags%2Fv0.1.0"
	var githubRequests []string
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		githubRequests = append(githubRequests, request.URL.String())
		if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer github-token" {
			return nil, errors.New("unexpected unauthenticated GitHub request")
		}
		switch request.URL.String() {
		case repoEndpoint:
			if request.Header.Get("Accept") != "application/vnd.github+json" {
				return nil, errors.New("repository authorization did not request GitHub JSON")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"owner":{"type":"User"},
					"permissions":{"admin":true,"maintain":false,"push":false,"triage":false,"pull":true}
				}`)),
				Request: request,
			}, nil
		case commitEndpoint:
			if request.Header.Get("Accept") != "application/vnd.github.sha" {
				return nil, errors.New("release-tag lookup did not request the GitHub SHA media type")
			}
			return &http.Response{
				StatusCode: commitStatus,
				Status:     http.StatusText(commitStatus),
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(commitBody)),
				Request:    request,
			}, nil
		default:
			return nil, errors.New("unexpected GitHub request: " + request.URL.String())
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	body, err := json.Marshal(rawPublishRequest(t))
	if err != nil {
		t.Fatalf("marshal publish request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/publish", bytes.NewReader(body))
	request.AddCookie(app.Sessions.WriteSessionCookie(auth.SessionState{Accounts: []string{user.ID}, Active: user.ID}))
	response := httptest.NewRecorder()
	app.NewRouter().ServeHTTP(response, request)
	if want := []string{repoEndpoint, commitEndpoint}; !reflect.DeepEqual(githubRequests, want) {
		t.Fatalf("GitHub requests = %v, want %v", githubRequests, want)
	}
	return response, dataStore
}

func TestRawPublishCanonicalizesVersionsAndVerifiesBeforePersistence(t *testing.T) {
	verifier := &recordingSourceVerifier{}
	response, dataStore := serveRawPublish(t, verifier)
	if response.Code != http.StatusOK {
		t.Fatalf("publish status = %d, body = %s", response.Code, response.Body.String())
	}
	if verifier.calls != 1 {
		t.Fatalf("source verifier calls = %d, want 1", verifier.calls)
	}
	gotPackage, gotPackageErr := canonicalPackageManifest(verifier.packageMetadata)
	wantPackage, wantPackageErr := canonicalPackageManifest(verifierPackage())
	if verifier.source.Module != verifierModule || verifier.source.Version != "v0.1.0" || verifier.commit != verifierCommit || gotPackageErr != nil || wantPackageErr != nil || !bytes.Equal(gotPackage, wantPackage) || len(verifier.providers) != 1 {
		t.Fatalf("source verifier received source=%+v commit=%s package=%+v providers=%d", verifier.source, verifier.commit, verifier.packageMetadata, len(verifier.providers))
	}
	pack, ok := dataStore.GetPackage(verifierModule)
	if !ok || len(pack.Versions) != 1 {
		t.Fatalf("stored package = %+v, found=%v", pack, ok)
	}
	if pack.Versions[0].Version != "v0.1.0" || pack.Versions[0].Providers[0].Source.Version != "v0.1.0" {
		t.Fatalf("persisted versions are not canonical: %+v", pack.Versions[0])
	}
}

func TestConcurrentPublishesCannotClaimSameProviderIDFromDifferentSourceModules(t *testing.T) {
	baseStore, err := store.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	dataStore := &pairedCatalogSnapshotStore{Store: baseStore, paired: make(chan struct{})}
	user := model.User{ID: "publisher", Login: "acme", GitHubToken: "github-token"}
	if err := dataStore.UpsertUser(user); err != nil {
		t.Fatalf("store user: %v", err)
	}

	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseVerifiers := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseVerifiers()
	app := NewWithSourceVerifier(config.Config{
		SessionSecret: []byte("concurrent-provider-claim-test-secret"), DevMode: true, FrontendURL: "http://localhost:8000",
	}, dataStore, barrierSourceVerifier{arrived: arrived, release: release})

	const repositoryEndpoint = "https://api.github.com/repos/acme/source"
	const rootCommitEndpoint = repositoryEndpoint + "/commits/tags%2Fv0.1.0"
	const nestedCommitEndpoint = repositoryEndpoint + "/commits/tags%2Fplugin%2Fv0.1.0"
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer github-token" {
			return nil, errors.New("unexpected unauthenticated GitHub request")
		}
		body := ""
		switch request.URL.String() {
		case repositoryEndpoint:
			if request.Header.Get("Accept") != "application/vnd.github+json" {
				return nil, errors.New("repository authorization did not request GitHub JSON")
			}
			body = `{"owner":{"type":"User"},"permissions":{"admin":true,"pull":true}}`
		case rootCommitEndpoint, nestedCommitEndpoint:
			if request.Header.Get("Accept") != "application/vnd.github.sha" {
				return nil, errors.New("release-tag lookup did not request the GitHub SHA media type")
			}
			body = verifierCommit
		default:
			return nil, errors.New("unexpected GitHub request: " + request.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(body)), Request: request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	const rootModule = "github.com/acme/source"
	const claimedProviderID = rootModule + "/plugin"
	rootPackage := model.PackageManifest{
		Module: rootModule, Version: "0.1.0", Name: "Source", Description: "Root source plugin",
		Stability: model.Stable, License: "Apache-2.0", Repository: "https://github.com/acme/source",
		Authors: []model.PackageAuthor{{Name: "Acme"}},
		Subpackages: []model.PackageSub{{
			Module: claimedProviderID, Name: "Plugin", Description: "Nested plugin", Stability: model.Stable,
		}},
	}
	nestedPackage := model.PackageManifest{
		Module: claimedProviderID, Version: "0.1.0", Name: "Plugin", Description: "Nested plugin",
		Stability: model.Stable, License: "Apache-2.0", Repository: "https://github.com/acme/source",
		Authors: []model.PackageAuthor{{Name: "Acme"}},
	}
	publishRequests := []publishRequest{
		providerClaimPublishRequest(t, rootModule, rootPackage),
		providerClaimPublishRequest(t, claimedProviderID, nestedPackage),
	}
	publishBodies := make([][]byte, len(publishRequests))
	for i, request := range publishRequests {
		publishBodies[i], err = json.Marshal(request)
		if err != nil {
			t.Fatalf("marshal publish request: %v", err)
		}
	}

	type publishResult struct {
		status int
		body   string
	}
	results := make(chan publishResult, len(publishBodies))
	router := app.NewRouter()
	sessionCookie := app.Sessions.WriteSessionCookie(auth.SessionState{Accounts: []string{user.ID}, Active: user.ID})
	for _, body := range publishBodies {
		body := append([]byte(nil), body...)
		cookie := *sessionCookie
		go func() {
			request := httptest.NewRequest(http.MethodPost, "/api/publish", bytes.NewReader(body))
			request.AddCookie(&cookie)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			results <- publishResult{status: response.Code, body: response.Body.String()}
		}()
	}
	for i := 0; i < len(publishBodies); i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent publishes did not both reach source verification")
		}
	}
	releaseVerifiers()

	responses := make([]publishResult, 0, len(publishBodies))
	for i := 0; i < len(publishBodies); i++ {
		select {
		case result := <-results:
			responses = append(responses, result)
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent publish did not complete")
		}
	}
	succeeded, rejected := 0, 0
	for _, response := range responses {
		switch {
		case response.status == http.StatusOK:
			succeeded++
		case response.status == http.StatusUnprocessableEntity && strings.Contains(response.body, "already published"):
			rejected++
		default:
			t.Errorf("unexpected publish response: status=%d body=%s", response.status, response.body)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent publish outcomes = %+v; want one success and one cross-source rejection", responses)
	}
	packages := baseStore.ListPackages()
	if len(packages) != 1 {
		t.Fatalf("stored packages = %d, want exactly one winning source", len(packages))
	}
	claims := 0
	for _, version := range packages[0].Versions {
		for _, provider := range version.Providers {
			if provider.ID == claimedProviderID {
				claims++
			}
		}
	}
	if claims != 1 {
		t.Fatalf("stored provider claims for %s = %d, want one", claimedProviderID, claims)
	}
}

func TestRawPublishRejectsCommitThatDoesNotMatchReleaseTag(t *testing.T) {
	verifier := &recordingSourceVerifier{}
	response, dataStore := serveRawPublishWithGitHubCommit(t, verifier, http.StatusOK, strings.Repeat("b", 40))
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "does not match release tag v0.1.0") {
		t.Fatalf("publish status = %d, body = %s", response.Code, response.Body.String())
	}
	if verifier.calls != 0 || dataStore.PackageCount() != 0 {
		t.Fatalf("commit mismatch called verifier %d times and persisted %d packages", verifier.calls, dataStore.PackageCount())
	}
}

func TestRawPublishRejectsUnresolvableReleaseTag(t *testing.T) {
	verifier := &recordingSourceVerifier{}
	response, dataStore := serveRawPublishWithGitHubCommit(t, verifier, http.StatusNotFound, "")
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "can't resolve release tag v0.1.0") {
		t.Fatalf("publish status = %d, body = %s", response.Code, response.Body.String())
	}
	if verifier.calls != 0 || dataStore.PackageCount() != 0 {
		t.Fatalf("unresolvable tag called verifier %d times and persisted %d packages", verifier.calls, dataStore.PackageCount())
	}
}

func TestRawPublishRejectsSourceVerifierFailureWithoutPersistence(t *testing.T) {
	verifier := &recordingSourceVerifier{err: errors.New("exact artifact catalog differs")}
	response, dataStore := serveRawPublish(t, verifier)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "source verification failed") {
		t.Fatalf("publish status = %d, body = %s", response.Code, response.Body.String())
	}
	if verifier.calls != 1 {
		t.Fatalf("source verifier calls = %d, want 1", verifier.calls)
	}
	if dataStore.PackageCount() != 0 {
		t.Fatalf("source verification failure persisted %d packages", dataStore.PackageCount())
	}
}
