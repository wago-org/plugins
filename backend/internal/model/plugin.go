package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
)

const ManifestSchemaV1 = "https://wago.sh/v1/schema.json"

// Manifest is the strict v1 wago.json contract. Project requirements stay at
// the root; everything used to publish a Go module lives below Package.
type Manifest struct {
	Schema   string            `json:"$schema"`
	Plugins  map[string]string `json:"plugins,omitempty"`
	Settings json.RawMessage   `json:"settings,omitempty"`
	Package  *PackageManifest  `json:"package,omitempty"`
}

type manifestSettings struct {
	Features      map[string]bool          `json:"features,omitempty"`
	Optimizations map[string]bool          `json:"optimizations,omitempty"`
	Runtime       *manifestRuntimeSettings `json:"runtime,omitempty"`
}

type manifestRuntimeSettings struct {
	Parallel               *string `json:"parallel,omitempty"`
	DeferredBoundsChecking *bool   `json:"deferredBoundsChecking,omitempty"`
}

// PackageManifest describes one source module. Its provider catalog is snapshotted
// in the exact source artifact and submitted separately so authors never duplicate it here.
type PackageManifest struct {
	Module      string            `json:"module"`
	Version     string            `json:"version,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Stability   Stability         `json:"stability,omitempty"`
	License     string            `json:"license"`
	Repository  string            `json:"repository"`
	Homepage    string            `json:"homepage,omitempty"`
	Authors     []PackageAuthor   `json:"authors,omitempty"`
	Category    string            `json:"category,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Engines     map[string]string `json:"engines,omitempty"`
	Platforms   []string          `json:"platforms,omitempty"`
	Subpackages []PackageSub      `json:"subpackages,omitempty"`
}

type PackageAuthor struct {
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
	URL    string `json:"url,omitempty"`
	Github string `json:"github,omitempty"`
}

type PackageSub struct {
	Module      string            `json:"module"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Stability   Stability         `json:"stability,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Engines     map[string]string `json:"engines,omitempty"`
	Platforms   []string          `json:"platforms,omitempty"`
}

// ProviderManifest is one entry emitted by the publisher's explicit register
// package. DefinitionDigest must match the canonical Definition exactly.
type ProviderManifest struct {
	ImportPath       string           `json:"importPath"`
	Definition       PluginDefinition `json:"definition"`
	DefinitionDigest string           `json:"definitionDigest"`
}

// PluginSource is the exact Go module artifact a consumer must download and
// verify before linking a provider.
type PluginSource struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Checksum string `json:"checksum"`
}

// PublishedProvider is the immutable, source-complete provider record stored on
// a release. Resolution responses never have to reconstruct reviewed metadata.
type PublishedProvider struct {
	ID               string           `json:"id"`
	ImportPath       string           `json:"importPath"`
	Source           PluginSource     `json:"source"`
	Definition       PluginDefinition `json:"definition"`
	DefinitionDigest string           `json:"definitionDigest"`
}

// PluginProvenance identifies the source and license behind a linked provider.
type PluginProvenance struct {
	Homepage   string   `json:"homepage,omitempty"`
	Repository string   `json:"repository,omitempty"`
	License    string   `json:"license,omitempty"`
	Authors    []string `json:"authors,omitempty"`
}

// AuthorityScope is authority-specific. Modules are required only by the three
// definition authorities; resource limits are required only by instance.manage
// and core.instance.instantiate.
type AuthorityScope struct {
	Modules        []string `json:"modules,omitempty"`
	MaxInstances   uint32   `json:"maxInstances,omitempty"`
	MaxMemoryBytes uint64   `json:"maxMemoryBytes,omitempty"`
}

type AuthorityRequest struct {
	Name   string         `json:"name"`
	Mode   string         `json:"mode"`
	Reason string         `json:"reason"`
	Scope  AuthorityScope `json:"scope,omitempty"`
}

type PluginRequirement struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ContractSpec struct {
	ID    string `json:"id"`
	Major uint32 `json:"major"`
}

type ContractRequirement struct {
	ID    string `json:"id"`
	Major uint32 `json:"major"`
	Mode  string `json:"mode"`
}

// PluginDefinition mirrors the runtime's digest-bearing public definition.
type PluginDefinition struct {
	ID            string                `json:"id"`
	Name          string                `json:"name,omitempty"`
	Version       string                `json:"version"`
	Description   string                `json:"description,omitempty"`
	Stability     Stability             `json:"stability,omitempty"`
	Compatibility Compatibility         `json:"compatibility,omitempty"`
	Provenance    PluginProvenance      `json:"provenance,omitempty"`
	Requires      []PluginRequirement   `json:"requires,omitempty"`
	Authorities   []AuthorityRequest    `json:"authorities,omitempty"`
	ConfigSchema  json.RawMessage       `json:"configSchema,omitempty"`
	Provides      []ContractSpec        `json:"provides,omitempty"`
	Consumes      []ContractRequirement `json:"consumes,omitempty"`
}

// UnmarshalJSON makes the registry reject unknown v1 fields at every nested
// struct boundary. There is deliberately no v0 fallback parser.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type plain Manifest
	var decoded plain
	if err := decodeStrict(data, &decoded); err != nil {
		return err
	}
	*m = Manifest(decoded)
	return nil
}

func decodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

// Validate checks the complete publish manifest, including digest equality.
func (m Manifest) Validate() error {
	if m.Schema != ManifestSchemaV1 {
		return fmt.Errorf("manifest.$schema must be %q", ManifestSchemaV1)
	}
	if len(m.Plugins) > 512 {
		return errors.New("manifest.plugins exceeds 512 direct requirements")
	}
	for id, constraint := range m.Plugins {
		if err := ValidatePluginID(id); err != nil {
			return fmt.Errorf("manifest.plugins[%q]: %w", id, err)
		}
		if constraint == "" {
			return fmt.Errorf("manifest.plugins[%q]: version range must not be empty", id)
		}
		if err := ValidateVersionRange(constraint); err != nil {
			return fmt.Errorf("manifest.plugins[%q]: %w", id, err)
		}
	}
	if len(m.Settings) != 0 {
		var settings *manifestSettings
		if err := decodeStrict(m.Settings, &settings); err != nil {
			return fmt.Errorf("manifest.settings: %w", err)
		}
		if settings == nil {
			return errors.New("manifest.settings must be an object")
		}
		if err := settings.Validate(); err != nil {
			return err
		}
	}
	if m.Package == nil {
		return errors.New("manifest.package is required when publishing")
	}
	return m.Package.Validate()
}

func (s manifestSettings) Validate() error {
	for feature := range s.Features {
		if _, ok := manifestFeatures[feature]; !ok {
			return fmt.Errorf("manifest.settings.features[%q] is unknown", feature)
		}
	}
	for optimization := range s.Optimizations {
		if _, ok := manifestOptimizations[optimization]; !ok {
			return fmt.Errorf("manifest.settings.optimizations[%q] is unknown", optimization)
		}
	}
	if s.Runtime != nil && s.Runtime.Parallel != nil {
		parallel := *s.Runtime.Parallel
		if parallel != "auto" {
			if parallel == "" {
				return errors.New("manifest.settings.runtime.parallel must be auto or a non-negative integer")
			}
			for _, r := range parallel {
				if r < '0' || r > '9' {
					return errors.New("manifest.settings.runtime.parallel must be auto or a non-negative integer")
				}
			}
		}
	}
	return nil
}

var manifestFeatures = stringSet(
	"bulk-memory-operations", "exception-handling", "extended-const-expressions",
	"extended-constant-expressions", "gc", "memory64", "multi-memory", "multi-value",
	"mutable-global", "nontrapping-float-to-int-conversion", "reference-types",
	"sign-extension-ops", "simd", "table64", "tail-call", "threads",
	"typed-function-references",
)

var manifestOptimizations = stringSet(
	"affine-lea", "assoc-tree", "bmi2-rorx", "bounds-facts", "branch-fold",
	"call-next-use", "commute-self-update", "compact-i32-frame", "deep-fp-pins",
	"entry-arg-pins", "ext-fp-pins", "frame-elide", "frame-elide-reghomed",
	"immutable-poly-fastpath", "immutable-table", "immutable-table-type", "inline",
	"inline-callfree", "inline-loop-callees", "leaf-scratch-pins", "legacy-fp-pins",
	"legacy-gp-pins", "loop-precheck", "loop-region-pins", "multi-bounds-cert",
	"olddest-rhs-sink", "reg-abi", "reg-merge", "small-frame", "st-flags",
	"stack-fence", "stack-reg", "store-forward", "store-load-fwd", "store8-flags",
	"tee-sink", "tee-spill-elide", "three-op-sink", "tree-order", "unary-sink",
	"uxtw-add", "v128-const-cache", "v128-pins", "v128-sink", "vex-float-mem",
	"x8-pin",
)

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func (p PackageManifest) Validate() error {
	if err := ValidatePluginID(p.Module); err != nil {
		return fmt.Errorf("manifest.package.module: %w", err)
	}
	if p.Version != "" {
		if err := ValidateVersion(p.Version); err != nil {
			return fmt.Errorf("manifest.package.version: %w", err)
		}
	}
	if strings.TrimSpace(p.License) == "" || len(p.License) > 100 {
		return errors.New("manifest.package.license is required and must be at most 100 characters")
	}
	if err := validateHTTPSURL(p.Repository); err != nil {
		return fmt.Errorf("manifest.package.repository: %w", err)
	}
	if p.Homepage != "" {
		if err := validateHomepageURL(p.Homepage); err != nil {
			return fmt.Errorf("manifest.package.homepage: %w", err)
		}
	}
	if err := validateStability(p.Stability); err != nil {
		return fmt.Errorf("manifest.package.stability: %w", err)
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Description) == "" || len(p.Name) > 100 || len(p.Description) > 500 || len(p.Category) > 64 {
		return errors.New("manifest.package name and description are required and display metadata must fit its size limit")
	}
	if p.Category != "" && !validSlug(p.Category) {
		return errors.New("manifest.package.category must be a lowercase slug")
	}
	if err := validatePackageAuthors(p.Authors); err != nil {
		return err
	}
	if err := validateUniqueStrings("manifest.package.tags", p.Tags, 32, 64, true); err != nil {
		return err
	}
	if err := validateCompatibility(Compatibility{Engines: p.Engines, Platforms: p.Platforms}); err != nil {
		return fmt.Errorf("manifest.package compatibility: %w", err)
	}
	if len(p.Subpackages) > 127 {
		return errors.New("manifest.package.subpackages exceeds 127 entries")
	}
	seenSubs := map[string]struct{}{}
	for i, sub := range p.Subpackages {
		if !PathBelongsToModule(sub.Module, p.Module) || sub.Module == p.Module {
			return fmt.Errorf("manifest.package.subpackages[%d].module must be a child of package.module", i)
		}
		if strings.TrimSpace(sub.Name) == "" || strings.TrimSpace(sub.Description) == "" || len(sub.Name) > 100 || len(sub.Description) > 500 {
			return fmt.Errorf("manifest.package.subpackages[%d] requires bounded name and description", i)
		}
		if err := validateStability(sub.Stability); err != nil {
			return fmt.Errorf("manifest.package.subpackages[%d].stability: %w", i, err)
		}
		if err := validateUniqueStrings(fmt.Sprintf("manifest.package.subpackages[%d].tags", i), sub.Tags, 32, 64, true); err != nil {
			return err
		}
		if err := validateCompatibility(Compatibility{Engines: sub.Engines, Platforms: sub.Platforms}); err != nil {
			return fmt.Errorf("manifest.package.subpackages[%d] compatibility: %w", i, err)
		}
		if _, duplicate := seenSubs[sub.Module]; duplicate {
			return fmt.Errorf("manifest.package.subpackages[%d].module is duplicated", i)
		}
		seenSubs[sub.Module] = struct{}{}
	}
	return nil
}

// ValidateProviderCatalog checks the publisher-executed catalog against the
// source manifest and exact release version. It stays separate from Manifest so
// wago.json never asks authors to duplicate executable provider metadata.
func ValidateProviderCatalog(p PackageManifest, version string, providers []ProviderManifest) error {
	if err := ValidateVersion(version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if p.Version != "" && normalizeVersion(p.Version) != normalizeVersion(version) {
		return fmt.Errorf("version %q does not match manifest.package.version %q", version, p.Version)
	}
	if len(providers) == 0 || len(providers) > 128 {
		return errors.New("providers must contain 1 to 128 explicit catalog entries")
	}
	expected := map[string]PackageSub{p.Module: {
		Module: p.Module, Name: p.Name, Description: p.Description, Stability: p.Stability,
		Engines: p.Engines, Platforms: p.Platforms,
	}}
	for _, sub := range p.Subpackages {
		metadata := PackageSub{
			Module: sub.Module, Name: sub.Name, Description: sub.Description,
			Stability: p.Stability, Engines: p.Engines, Platforms: p.Platforms,
		}
		if sub.Stability != "" {
			metadata.Stability = sub.Stability
		}
		if sub.Engines != nil {
			metadata.Engines = sub.Engines
		}
		if sub.Platforms != nil {
			metadata.Platforms = sub.Platforms
		}
		expected[sub.Module] = metadata
	}
	authorNames := make([]string, len(p.Authors))
	for i, author := range p.Authors {
		authorNames[i] = author.Name
	}
	seen := map[string]struct{}{}
	for i, provider := range providers {
		path := fmt.Sprintf("providers[%d]", i)
		if provider.ImportPath != p.Module+"/register" {
			return fmt.Errorf("%s.importPath must be %q", path, p.Module+"/register")
		}
		if err := ValidatePluginDefinition(provider.Definition); err != nil {
			return fmt.Errorf("%s.definition: %w", path, err)
		}
		metadata, declared := expected[provider.Definition.ID]
		if !declared {
			return fmt.Errorf("%s.definition.id %q is not declared by package or package.subpackages", path, provider.Definition.ID)
		}
		if normalizeVersion(provider.Definition.Version) != normalizeVersion(version) {
			return fmt.Errorf("%s.definition.version %q must equal release version %q", path, provider.Definition.Version, version)
		}
		if provider.Definition.Name != metadata.Name || provider.Definition.Description != metadata.Description || provider.Definition.Stability != metadata.Stability {
			return fmt.Errorf("%s.definition display metadata must match wago.json", path)
		}
		if !sameStringMap(provider.Definition.Compatibility.Engines, metadata.Engines) || !sameStringSet(provider.Definition.Compatibility.Platforms, metadata.Platforms) {
			return fmt.Errorf("%s.definition.compatibility must match wago.json", path)
		}
		provenance := provider.Definition.Provenance
		if provenance.Repository != p.Repository || provenance.Homepage != p.Homepage || provenance.License != p.License || !sameStringSet(provenance.Authors, authorNames) {
			return fmt.Errorf("%s.definition.provenance must match wago.json", path)
		}
		if _, duplicate := seen[provider.Definition.ID]; duplicate {
			return fmt.Errorf("%s.definition.id %q is duplicated", path, provider.Definition.ID)
		}
		seen[provider.Definition.ID] = struct{}{}
		digest, err := DefinitionDigest(provider.Definition)
		if err != nil {
			return fmt.Errorf("%s.definition: %w", path, err)
		}
		if provider.DefinitionDigest != digest {
			return fmt.Errorf("%s.definitionDigest is %q, want %q", path, provider.DefinitionDigest, digest)
		}
	}
	for id := range expected {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("wago.json declares provider %q but the provider catalog omitted it", id)
		}
	}
	return nil
}

func normalizeVersion(value string) string { return strings.TrimPrefix(value, "v") }

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = sortedStrings(left)
	right = sortedStrings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

var exactAuthorities = map[string]struct{}{
	"host.import.define": {}, "host.caller.identify": {}, "host.caller.invoke": {}, "host.arguments.read": {},
	"runtime.close.observe": {}, "module.source.transform": {}, "module.compile.observe": {},
	"module.close.observe":           {},
	"instance.instantiate.intercept": {}, "instance.instantiate.observe": {},
	"instance.close.observe": {}, "instance.invoke.intercept": {}, "instance.invoke.observe": {},
	"instance.manage": {}, "core.module.compile": {}, "core.instance.instantiate": {},
	"core.funcref.create": {}, "compiler.type.define": {}, "compiler.instruction.define": {},
}

func ValidatePluginDefinition(def PluginDefinition) error {
	if err := ValidatePluginID(def.ID); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if err := ValidateVersion(def.Version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if len(def.Name) > 100 || len(def.Description) > 500 {
		return errors.New("name or description exceeds its size limit")
	}
	if err := validateStability(def.Stability); err != nil {
		return fmt.Errorf("stability: %w", err)
	}
	if err := validateCompatibility(def.Compatibility); err != nil {
		return fmt.Errorf("compatibility: %w", err)
	}
	if strings.TrimSpace(def.Provenance.License) == "" {
		return errors.New("provenance.license is required")
	}
	if err := validateHTTPSURL(def.Provenance.Repository); err != nil {
		return fmt.Errorf("provenance.repository: %w", err)
	}
	if def.Provenance.Homepage != "" {
		if err := validateHomepageURL(def.Provenance.Homepage); err != nil {
			return fmt.Errorf("provenance.homepage: %w", err)
		}
	}
	if err := validateUniqueStrings("provenance.authors", def.Provenance.Authors, 64, 200, false); err != nil {
		return err
	}
	if len(def.Requires) > 128 || len(def.Authorities) > 64 || len(def.Provides) > 128 || len(def.Consumes) > 128 {
		return errors.New("definition exceeds an entry-count limit")
	}
	requires := map[string]struct{}{}
	for i, requirement := range def.Requires {
		if err := ValidatePluginID(requirement.ID); err != nil {
			return fmt.Errorf("requires[%d].id: %w", i, err)
		}
		if requirement.ID == def.ID {
			return fmt.Errorf("requires[%d] cannot depend on itself", i)
		}
		if strings.TrimSpace(requirement.Version) == "" {
			return fmt.Errorf("requires[%d].version must be non-empty", i)
		}
		if err := ValidateVersionRange(requirement.Version); err != nil {
			return fmt.Errorf("requires[%d].version: %w", i, err)
		}
		if _, duplicate := requires[requirement.ID]; duplicate {
			return fmt.Errorf("requires[%d].id %q is duplicated", i, requirement.ID)
		}
		requires[requirement.ID] = struct{}{}
	}
	authorities := map[string]struct{}{}
	for i, request := range def.Authorities {
		if _, ok := exactAuthorities[request.Name]; !ok {
			return fmt.Errorf("authorities[%d].name %q is unknown", i, request.Name)
		}
		if request.Mode != "required" && request.Mode != "optional" {
			return fmt.Errorf("authorities[%d].mode must be required or optional", i)
		}
		if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 500 {
			return fmt.Errorf("authorities[%d].reason is required and must be at most 500 characters", i)
		}
		if _, duplicate := authorities[request.Name]; duplicate {
			return fmt.Errorf("authorities[%d].name %q is duplicated", i, request.Name)
		}
		authorities[request.Name] = struct{}{}
		if err := validateAuthorityScope(request.Name, request.Scope); err != nil {
			return fmt.Errorf("authorities[%d].scope: %w", i, err)
		}
	}
	if len(def.ConfigSchema) > 256<<10 {
		return errors.New("configSchema exceeds 256 KiB")
	}
	if len(def.ConfigSchema) != 0 {
		if err := validateConfigSchema(def.ConfigSchema); err != nil {
			return fmt.Errorf("configSchema: %w", err)
		}
	}
	provided := map[string]struct{}{}
	for i, contract := range def.Provides {
		if err := validateContract(contract.ID, contract.Major); err != nil {
			return fmt.Errorf("provides[%d]: %w", i, err)
		}
		key := contractKey(contract.ID, contract.Major)
		if _, duplicate := provided[key]; duplicate {
			return fmt.Errorf("provides[%d] duplicates contract %s", i, key)
		}
		provided[key] = struct{}{}
	}
	consumed := map[string]struct{}{}
	for i, contract := range def.Consumes {
		if err := validateContract(contract.ID, contract.Major); err != nil {
			return fmt.Errorf("consumes[%d]: %w", i, err)
		}
		if contract.Mode != "required" && contract.Mode != "optional" && contract.Mode != "many" {
			return fmt.Errorf("consumes[%d].mode must be required, optional, or many", i)
		}
		key := contractKey(contract.ID, contract.Major)
		if _, duplicate := consumed[key]; duplicate {
			return fmt.Errorf("consumes[%d] duplicates contract %s", i, key)
		}
		consumed[key] = struct{}{}
	}
	return nil
}

func validateAuthorityScope(name string, scope AuthorityScope) error {
	switch name {
	case "host.import.define", "compiler.type.define", "compiler.instruction.define":
		if scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return errors.New("resource limits are not valid for this authority")
		}
		if len(scope.Modules) == 0 {
			return errors.New("modules must contain at least one exact module name")
		}
		return validateExactModules(scope.Modules)
	case "instance.manage", "core.instance.instantiate":
		if len(scope.Modules) != 0 {
			return fmt.Errorf("modules are not valid for %s", name)
		}
		if scope.MaxInstances == 0 || scope.MaxMemoryBytes == 0 {
			return errors.New("maxInstances and maxMemoryBytes must both be greater than zero")
		}
		return nil
	default:
		if len(scope.Modules) != 0 || scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return errors.New("this authority does not accept a scope")
		}
		return nil
	}
}

func validateExactModules(modules []string) error {
	if len(modules) > 128 {
		return errors.New("modules exceeds 128 entries")
	}
	seen := map[string]struct{}{}
	for _, module := range modules {
		if module == "" || len(module) > 200 || strings.TrimSpace(module) != module || strings.ContainsAny(module, "*\x00\r\n") {
			return fmt.Errorf("module %q is not an exact module name", module)
		}
		if _, duplicate := seen[module]; duplicate {
			return fmt.Errorf("module %q is duplicated", module)
		}
		seen[module] = struct{}{}
	}
	return nil
}

func validateConfigSchema(raw json.RawMessage) error {
	var schema map[string]any
	if err := decodeJSONValue(raw, &schema); err != nil {
		return err
	}
	if schema == nil {
		return errors.New("must be a JSON object")
	}
	if dialect, ok := schema["$schema"]; ok && dialect != "https://json-schema.org/draft/2020-12/schema" {
		return errors.New("$schema must be JSON Schema draft 2020-12")
	}
	if schema["type"] != "object" {
		return errors.New(`type must be "object"`)
	}
	if additional, ok := schema["additionalProperties"]; !ok || additional != false {
		return errors.New("additionalProperties must be false so unknown config fails closed")
	}
	return nil
}

func validateContract(id string, major uint32) error {
	if err := ValidatePluginID(id); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if major == 0 {
		return errors.New("major must be greater than zero")
	}
	return nil
}

func contractKey(id string, major uint32) string { return fmt.Sprintf("%s@%d", id, major) }

func validateCompatibility(compat Compatibility) error {
	if len(compat.Engines) > 64 || len(compat.Platforms) > 128 {
		return errors.New("too many engines or platforms")
	}
	for engine, constraint := range compat.Engines {
		if !validSlug(engine) {
			return fmt.Errorf("engine name %q is invalid", engine)
		}
		if constraint == "" {
			return fmt.Errorf("engine %q has an empty version range", engine)
		}
		if err := ValidateVersionRange(constraint); err != nil {
			return fmt.Errorf("engine %q: %w", engine, err)
		}
	}
	seen := map[string]struct{}{}
	for _, platform := range compat.Platforms {
		parts := strings.Split(platform, "/")
		if len(parts) != 2 || !validPlatformPart(parts[0]) || !validPlatformPart(parts[1]) {
			return fmt.Errorf("platform %q must be exact GOOS/GOARCH", platform)
		}
		if _, duplicate := seen[platform]; duplicate {
			return fmt.Errorf("platform %q is duplicated", platform)
		}
		seen[platform] = struct{}{}
	}
	return nil
}

func validateStability(stability Stability) error {
	switch stability {
	case "", Experimental, Stable, Deprecated:
		return nil
	default:
		return fmt.Errorf("%q is not experimental, stable, or deprecated", stability)
	}
}

func ValidatePluginID(id string) error {
	if id == "" || len(id) > 300 {
		return errors.New("must be a non-empty canonical Go module or package path of at most 300 bytes")
	}
	parts := strings.Split(id, "/")
	if len(parts) < 2 || !validPluginHost(parts[0]) {
		return errors.New("must begin with a dotted DNS-like source host")
	}
	for _, part := range parts[1:] {
		if !validPluginPathSegment(part) {
			return fmt.Errorf("path segment %q must begin and end with ASCII alphanumeric characters and use only internal . _ ~ - punctuation", part)
		}
	}
	return nil
}

func validPluginHost(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || !asciiAlphaNumeric(label[0]) || !asciiAlphaNumeric(label[len(label)-1]) {
			return false
		}
		for i := 1; i < len(label)-1; i++ {
			if !asciiAlphaNumeric(label[i]) && label[i] != '-' {
				return false
			}
		}
	}
	return true
}

func validPluginPathSegment(segment string) bool {
	if segment == "" || !asciiAlphaNumeric(segment[0]) || !asciiAlphaNumeric(segment[len(segment)-1]) {
		return false
	}
	for i := 1; i < len(segment)-1; i++ {
		if asciiAlphaNumeric(segment[i]) || strings.ContainsRune("._~-", rune(segment[i])) {
			continue
		}
		return false
	}
	return true
}

func asciiAlphaNumeric(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func PathBelongsToModule(path, module string) bool {
	return ValidatePluginID(path) == nil && ValidatePluginID(module) == nil &&
		(path == module || strings.HasPrefix(path, module+"/"))
}

func ValidateSourceChecksum(checksum string) error {
	if !strings.HasPrefix(checksum, "h1:") {
		return errors.New("source checksum must be a Go module h1 checksum")
	}
	digest, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(checksum, "h1:"))
	if err != nil || len(digest) != sha256.Size {
		return errors.New("source checksum must contain one base64-encoded SHA-256 digest")
	}
	return nil
}

func validateHTTPSURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("must be an absolute HTTPS URL without credentials or fragment")
	}
	return nil
}

func validateHomepageURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("must be an absolute HTTPS URL without credentials")
	}
	return nil
}

func validateUniqueStrings(path string, values []string, maxItems, maxLength int, slug bool) error {
	if len(values) > maxItems {
		return fmt.Errorf("%s exceeds %d entries", path, maxItems)
	}
	seen := map[string]struct{}{}
	for i, value := range values {
		if value == "" || len(value) > maxLength || strings.TrimSpace(value) != value || (slug && !validSlug(value)) {
			return fmt.Errorf("%s[%d] is invalid", path, i)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s[%d] %q is duplicated", path, i, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validatePackageAuthors(authors []PackageAuthor) error {
	if len(authors) == 0 || len(authors) > 64 {
		return errors.New("manifest.package.authors must contain 1 to 64 authors")
	}
	seen := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	for i, author := range authors {
		if strings.TrimSpace(author.Name) == "" || strings.TrimSpace(author.Name) != author.Name || len(author.Name) > 120 ||
			len(author.Email) > 254 || (author.Email != "" && !validEmail(author.Email)) ||
			(author.Github != "" && !validGitHubLogin(author.Github)) {
			return fmt.Errorf("manifest.package.authors[%d] is invalid", i)
		}
		if author.URL != "" {
			if err := validateHTTPSURL(author.URL); err != nil {
				return fmt.Errorf("manifest.package.authors[%d].url: %w", i, err)
			}
		}
		key := author.Name + "\x00" + author.Email + "\x00" + author.URL + "\x00" + author.Github
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("manifest.package.authors[%d] is duplicated", i)
		}
		seen[key] = struct{}{}
		if _, duplicate := seenNames[author.Name]; duplicate {
			return fmt.Errorf("manifest.package.authors[%d].name %q is duplicated", i, author.Name)
		}
		seenNames[author.Name] = struct{}{}
	}
	return nil
}

func validEmail(value string) bool {
	if strings.TrimSpace(value) != value || strings.Count(value, "@") != 1 {
		return false
	}
	parts := strings.SplitN(value, "@", 2)
	return parts[0] != "" && strings.Contains(parts[1], ".") &&
		!strings.ContainsAny(value, "<>(),;:\\[] \t\r\n")
}

func validGitHubLogin(value string) bool {
	if len(value) == 0 || len(value) > 39 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validSlug(value string) bool {
	if value == "" || len(value) > 64 || !asciiLowerOrDigit(rune(value[0])) {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func asciiLowerOrDigit(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

func validPlatformPart(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !asciiLowerOrDigit(r) {
			return false
		}
	}
	return true
}

func decodeJSONValue(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid trailing JSON")
	}
	return nil
}

// DefinitionDigest returns the runtime-compatible SHA-256 digest of the
// canonical definition. Set-like lists and embedded schema JSON are normalized.
func DefinitionDigest(def PluginDefinition) (string, error) {
	canonical, err := CanonicalPluginDefinition(def)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical plugin definition: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func CanonicalPluginDefinition(def PluginDefinition) (PluginDefinition, error) {
	if err := ValidatePluginDefinition(def); err != nil {
		return PluginDefinition{}, err
	}
	def.Compatibility.Engines = cloneStringMap(def.Compatibility.Engines)
	def.Compatibility.Platforms = sortedStrings(def.Compatibility.Platforms)
	def.Provenance.Authors = sortedStrings(def.Provenance.Authors)
	def.Requires = append([]PluginRequirement(nil), def.Requires...)
	sort.Slice(def.Requires, func(i, j int) bool {
		if def.Requires[i].ID == def.Requires[j].ID {
			return def.Requires[i].Version < def.Requires[j].Version
		}
		return def.Requires[i].ID < def.Requires[j].ID
	})
	def.Authorities = append([]AuthorityRequest(nil), def.Authorities...)
	for i := range def.Authorities {
		def.Authorities[i].Scope.Modules = sortedStrings(def.Authorities[i].Scope.Modules)
	}
	sort.Slice(def.Authorities, func(i, j int) bool { return def.Authorities[i].Name < def.Authorities[j].Name })
	def.Provides = append([]ContractSpec(nil), def.Provides...)
	sort.Slice(def.Provides, func(i, j int) bool {
		if def.Provides[i].ID == def.Provides[j].ID {
			return def.Provides[i].Major < def.Provides[j].Major
		}
		return def.Provides[i].ID < def.Provides[j].ID
	})
	def.Consumes = append([]ContractRequirement(nil), def.Consumes...)
	sort.Slice(def.Consumes, func(i, j int) bool {
		if def.Consumes[i].ID != def.Consumes[j].ID {
			return def.Consumes[i].ID < def.Consumes[j].ID
		}
		if def.Consumes[i].Major != def.Consumes[j].Major {
			return def.Consumes[i].Major < def.Consumes[j].Major
		}
		return def.Consumes[i].Mode < def.Consumes[j].Mode
	})
	if len(def.ConfigSchema) != 0 {
		var value any
		if err := decodeJSONValue(def.ConfigSchema, &value); err != nil {
			return PluginDefinition{}, err
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return PluginDefinition{}, err
		}
		def.ConfigSchema = canonical
	}
	return def, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func sortedStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := append([]string(nil), input...)
	sort.Strings(out)
	return out
}
