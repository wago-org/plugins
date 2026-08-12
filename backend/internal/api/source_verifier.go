package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/wago-org/registry-backend/internal/model"
)

const (
	sourceCatalogSchema       = "https://wago.sh/v1/providers.schema.json"
	sourceCatalogFilename     = "wago.providers.json"
	sourceManifestFilename    = "wago.json"
	sourceVerificationTimeout = 2 * time.Minute
	downloadOutputLimit       = 1 << 20
	catalogFileLimit          = 2 << 20
	commandErrorOutputLimit   = 64 << 10
)

// SourceVerifier independently proves that submitted package metadata and
// providers came from one exact, checksummed Go module release. Implementations
// must never load or execute plugin packages on the registry host.
type SourceVerifier interface {
	Verify(context.Context, model.PluginSource, string, model.PackageManifest, []model.ProviderManifest) error
}

type goSourceVerifier struct {
	runner  goCommandRunner
	timeout time.Duration
}

func newGoSourceVerifier() SourceVerifier {
	return &goSourceVerifier{runner: execGoCommandRunner{}, timeout: sourceVerificationTimeout}
}

type goCommand struct {
	dir         string
	env         []string
	args        []string
	stdoutLimit int
	stderrLimit int
}

type goCommandResult struct {
	stdout []byte
	stderr []byte
}

type goCommandRunner interface {
	Run(context.Context, goCommand) (goCommandResult, error)
}

type execGoCommandRunner struct{}

func (execGoCommandRunner) Run(ctx context.Context, spec goCommand) (goCommandResult, error) {
	cmd := exec.CommandContext(ctx, "go", spec.args...)
	cmd.Dir = spec.dir
	cmd.Env = spec.env
	var stdout, stderr boundedBuffer
	stdout.limit = spec.stdoutLimit
	stderr.limit = spec.stderrLimit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := goCommandResult{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	if stdout.exceeded || stderr.exceeded {
		return result, errors.New("go command output exceeded its verification limit")
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, err
	}
	return result, nil
}

// boundedBuffer retains at most limit bytes. The process is still bounded by
// the verification deadline when a failed download keeps writing after that.
type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.exceeded = b.exceeded || original != 0
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.exceeded = true
	}
	_, _ = b.Buffer.Write(p)
	return original, nil
}

func canonicalModuleVersion(version string) (string, error) {
	if err := model.ValidateVersion(version); err != nil {
		return "", err
	}
	if strings.HasPrefix(version, "v") {
		return version, nil
	}
	return "v" + version, nil
}

type sourceArtifactCatalog struct {
	Schema    string                   `json:"$schema"`
	Providers []model.ProviderManifest `json:"providers"`
}

func (v *goSourceVerifier) Verify(ctx context.Context, source model.PluginSource, commit string, submittedPackage model.PackageManifest, submittedProviders []model.ProviderManifest) error {
	if err := model.ValidatePluginID(source.Module); err != nil {
		return fmt.Errorf("source module: %w", err)
	}
	version, err := canonicalModuleVersion(source.Version)
	if err != nil {
		return fmt.Errorf("source version: %w", err)
	}
	if source.Version != version {
		return fmt.Errorf("source version must use canonical Go module form %q", version)
	}
	if err := model.ValidateSourceChecksum(source.Checksum); err != nil {
		return err
	}
	if !fullGitCommit(commit) {
		return errors.New("source commit must be a full lowercase Git SHA")
	}
	if err := submittedPackage.Validate(); err != nil {
		return fmt.Errorf("submitted manifest.package: %w", err)
	}

	timeout := v.timeout
	if timeout <= 0 {
		timeout = sourceVerificationTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	root, err := os.MkdirTemp("", "wago-registry-source-verify-*")
	if err != nil {
		return fmt.Errorf("create source verification workspace: %w", err)
	}
	defer os.RemoveAll(root)
	for _, directory := range []string{"home", "tmp", "gopath", "modcache", "buildcache"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			return fmt.Errorf("create source verification workspace: %w", err)
		}
	}
	env := sourceVerificationEnvironment(root)

	exact := source.Module + "@" + source.Version
	download, err := v.runner.Run(ctx, goCommand{
		dir: root, env: env,
		args:        []string{"mod", "download", "-json", exact},
		stdoutLimit: downloadOutputLimit, stderrLimit: commandErrorOutputLimit,
	})
	if err != nil {
		return commandFailure("download "+exact, download, err)
	}
	var report struct {
		Path     string `json:"Path"`
		Version  string `json:"Version"`
		Query    string `json:"Query"`
		Info     string `json:"Info"`
		GoMod    string `json:"GoMod"`
		Zip      string `json:"Zip"`
		Dir      string `json:"Dir"`
		Sum      string `json:"Sum"`
		GoModSum string `json:"GoModSum"`
		Origin   *struct {
			VCS       string `json:"VCS"`
			URL       string `json:"URL"`
			Subdir    string `json:"Subdir"`
			Hash      string `json:"Hash"`
			Ref       string `json:"Ref"`
			TagPrefix string `json:"TagPrefix"`
			TagSum    string `json:"TagSum"`
			RepoSum   string `json:"RepoSum"`
		} `json:"Origin"`
		Reuse bool   `json:"Reuse"`
		Error string `json:"Error"`
	}
	if err := decodeSingleJSON(download.stdout, &report); err != nil {
		return fmt.Errorf("decode downloaded module report: %w", err)
	}
	if report.Error != "" {
		return fmt.Errorf("download %s: %s", exact, report.Error)
	}
	if report.Path != source.Module || report.Version != source.Version {
		return fmt.Errorf("download returned %s@%s, want %s", report.Path, report.Version, exact)
	}
	if report.Sum != source.Checksum {
		return fmt.Errorf("downloaded source checksum %q does not match submitted checksum %q", report.Sum, source.Checksum)
	}
	if report.Origin == nil {
		return errors.New("downloaded source omits VCS origin metadata")
	}
	wantRef := "refs/tags/" + moduleReleaseTag(source.Module, source.Version)
	wantSubdir := moduleReleaseSubdirectory(source.Module, source.Version)
	if report.Origin.VCS != "git" || !sameRepo(report.Origin.URL, submittedPackage.Repository) ||
		report.Origin.Subdir != wantSubdir || report.Origin.Hash != commit || report.Origin.Ref != wantRef {
		return fmt.Errorf(
			"downloaded source origin %s %s subdir %q %s %s does not match repository %s, subdir %q, commit %s, and tag %s",
			report.Origin.VCS, report.Origin.URL, report.Origin.Subdir, report.Origin.Hash, report.Origin.Ref,
			submittedPackage.Repository, wantSubdir, commit, wantRef,
		)
	}
	moduleDir, err := verifiedModuleDirectory(filepath.Join(root, "modcache"), report.Dir)
	if err != nil {
		return fmt.Errorf("downloaded module directory: %w", err)
	}
	manifestBytes, err := readBoundedRegularFile(filepath.Join(moduleDir, sourceManifestFilename), catalogFileLimit)
	if err != nil {
		return fmt.Errorf("read %s from exact source artifact: %w", sourceManifestFilename, err)
	}
	var artifactManifest model.Manifest
	if err := decodeSingleJSON(manifestBytes, &artifactManifest); err != nil {
		return fmt.Errorf("decode %s from exact source artifact: %w", sourceManifestFilename, err)
	}
	if err := artifactManifest.Validate(); err != nil {
		return fmt.Errorf("validate %s from exact source artifact: %w", sourceManifestFilename, err)
	}
	artifactPackage, err := canonicalPackageManifest(*artifactManifest.Package)
	if err != nil {
		return fmt.Errorf("canonicalize %s manifest.package: %w", sourceManifestFilename, err)
	}
	submittedPackageJSON, err := canonicalPackageManifest(submittedPackage)
	if err != nil {
		return fmt.Errorf("canonicalize submitted manifest.package: %w", err)
	}
	if !bytes.Equal(artifactPackage, submittedPackageJSON) {
		return errors.New("exact source artifact manifest.package does not match submitted manifest.package")
	}
	catalogBytes, err := readBoundedRegularFile(filepath.Join(moduleDir, sourceCatalogFilename), catalogFileLimit)
	if err != nil {
		return fmt.Errorf("read %s from exact source artifact: %w", sourceCatalogFilename, err)
	}
	var artifact sourceArtifactCatalog
	if err := decodeSingleJSON(catalogBytes, &artifact); err != nil {
		return fmt.Errorf("decode %s from exact source artifact: %w", sourceCatalogFilename, err)
	}
	if artifact.Schema != sourceCatalogSchema {
		return fmt.Errorf("%s.$schema must be %q", sourceCatalogFilename, sourceCatalogSchema)
	}
	if err := validateCanonicalArtifactCatalog(source, artifact.Providers); err != nil {
		return fmt.Errorf("%s: %w", sourceCatalogFilename, err)
	}
	if err := model.ValidateProviderCatalog(submittedPackage, source.Version, submittedProviders); err != nil {
		return fmt.Errorf("submitted provider catalog: %w", err)
	}
	if err := model.ValidateProviderCatalog(*artifactManifest.Package, source.Version, artifact.Providers); err != nil {
		return fmt.Errorf("%s provider catalog: %w", sourceCatalogFilename, err)
	}
	if err := compareProviderCatalogs(source, artifact.Providers, submittedProviders); err != nil {
		return fmt.Errorf("exact provider catalog mismatch: %w", err)
	}
	return nil
}

// validateCanonicalArtifactCatalog mirrors the runtime decoder's semantic
// canonicality checks. Formatting inside configSchema is normalized, while all
// definition collections and provider IDs must already be in canonical order.
func validateCanonicalArtifactCatalog(source model.PluginSource, providers []model.ProviderManifest) error {
	if len(providers) == 0 || len(providers) > 128 {
		return errors.New("provider catalog must contain 1 to 128 providers")
	}
	previousID := ""
	for index, provider := range providers {
		canonical, err := model.CanonicalPluginDefinition(provider.Definition)
		if err != nil {
			return fmt.Errorf("providers[%d].definition: %w", index, err)
		}
		encoded := provider.Definition
		encoded.ConfigSchema = canonical.ConfigSchema
		if !reflect.DeepEqual(encoded, canonical) {
			return fmt.Errorf("provider %q definition is not canonical", canonical.ID)
		}
		if previousID != "" && previousID >= canonical.ID {
			return errors.New("providers are not in canonical ID order")
		}
		previousID = canonical.ID
		if !model.PathBelongsToModule(canonical.ID, source.Module) {
			return fmt.Errorf("provider %q does not belong to source module %q", canonical.ID, source.Module)
		}
		if provider.ImportPath != source.Module+"/register" {
			return fmt.Errorf("provider %q import path must be %q", canonical.ID, source.Module+"/register")
		}
	}
	return nil
}

func canonicalPackageManifest(pack model.PackageManifest) ([]byte, error) {
	if err := pack.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(pack)
}

func sourceVerificationEnvironment(root string) []string {
	var env []string
	for _, key := range []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "COMSPEC", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = append(env,
		"HOME="+filepath.Join(root, "home"),
		"TMPDIR="+filepath.Join(root, "tmp"),
		"TMP="+filepath.Join(root, "tmp"),
		"TEMP="+filepath.Join(root, "tmp"),
		"GOPATH="+filepath.Join(root, "gopath"),
		"GOMODCACHE="+filepath.Join(root, "modcache"),
		"GOCACHE="+filepath.Join(root, "buildcache"),
		"GOENV=off",
		"GO111MODULE=on",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
		"GOPROXY=https://proxy.golang.org",
		"GOSUMDB=sum.golang.org",
		"GOPRIVATE=",
		"GONOPROXY=",
		"GONOSUMDB=",
		"GOINSECURE=",
		"GOVCS=*:off",
		"GIT_TERMINAL_PROMPT=0",
	)
	return env
}

func verifiedModuleDirectory(modCache, downloaded string) (string, error) {
	if downloaded == "" {
		return "", errors.New("go mod download returned no module directory")
	}
	realCache, err := filepath.EvalSymlinks(modCache)
	if err != nil {
		return "", err
	}
	realDownloaded, err := filepath.EvalSymlinks(downloaded)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(realCache, realDownloaded)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("go mod download returned a directory outside the isolated module cache")
	}
	return realDownloaded, nil
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("catalog is not a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("catalog exceeds %d bytes", limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("catalog exceeds %d bytes", limit)
	}
	return data, nil
}

func commandFailure(action string, result goCommandResult, err error) error {
	detail := strings.TrimSpace(string(result.stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(result.stdout))
	}
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}

func decodeSingleJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

type comparableProvider struct {
	id         string
	importPath string
	digest     string
	definition []byte
}

func compareProviderCatalogs(source model.PluginSource, artifact, submitted []model.ProviderManifest) error {
	actual, err := comparableCatalog("artifact", source, artifact)
	if err != nil {
		return err
	}
	expected, err := comparableCatalog("submitted", source, submitted)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("artifact catalog has %d providers; submitted catalog has %d", len(actual), len(expected))
	}
	for index := range expected {
		want, got := expected[index], actual[index]
		if want.id != got.id {
			return fmt.Errorf("artifact provider %q does not match submitted provider %q", got.id, want.id)
		}
		if want.importPath != got.importPath {
			return fmt.Errorf("provider %q artifact import path is %q, submitted %q", want.id, got.importPath, want.importPath)
		}
		if want.digest != got.digest || !bytes.Equal(want.definition, got.definition) {
			return fmt.Errorf("provider %q artifact definition digest is %q, submitted %q", want.id, got.digest, want.digest)
		}
	}
	return nil
}

func comparableCatalog(label string, source model.PluginSource, catalog []model.ProviderManifest) ([]comparableProvider, error) {
	if len(catalog) == 0 || len(catalog) > 128 {
		return nil, fmt.Errorf("%s catalog must contain 1 to 128 providers", label)
	}
	result := make([]comparableProvider, 0, len(catalog))
	seen := make(map[string]struct{}, len(catalog))
	for index, provider := range catalog {
		canonical, err := model.CanonicalPluginDefinition(provider.Definition)
		if err != nil {
			return nil, fmt.Errorf("%s provider %d definition: %w", label, index, err)
		}
		if !model.PathBelongsToModule(canonical.ID, source.Module) {
			return nil, fmt.Errorf("%s provider %q does not belong to source module %q", label, canonical.ID, source.Module)
		}
		if strings.TrimPrefix(canonical.Version, "v") != strings.TrimPrefix(source.Version, "v") {
			return nil, fmt.Errorf("%s provider %q version %q does not match source version %q", label, canonical.ID, canonical.Version, source.Version)
		}
		expectedImport := source.Module + "/register"
		if provider.ImportPath != expectedImport {
			return nil, fmt.Errorf("%s provider %q import path must be %q", label, canonical.ID, expectedImport)
		}
		if _, duplicate := seen[canonical.ID]; duplicate {
			return nil, fmt.Errorf("%s provider %q is duplicated", label, canonical.ID)
		}
		seen[canonical.ID] = struct{}{}
		digest, err := model.DefinitionDigest(canonical)
		if err != nil {
			return nil, fmt.Errorf("%s provider %q digest: %w", label, canonical.ID, err)
		}
		if provider.DefinitionDigest != digest {
			return nil, fmt.Errorf("%s provider %q definition digest is %q, want %q", label, canonical.ID, provider.DefinitionDigest, digest)
		}
		definition, err := json.Marshal(canonical)
		if err != nil {
			return nil, fmt.Errorf("marshal %s provider %q: %w", label, canonical.ID, err)
		}
		result = append(result, comparableProvider{
			id: canonical.ID, importPath: provider.ImportPath, digest: digest, definition: definition,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, nil
}
