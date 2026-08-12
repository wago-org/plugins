package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func validDefinition(id, version string) PluginDefinition {
	return PluginDefinition{
		ID:          id,
		Name:        "Workers",
		Version:     version,
		Description: "Bounded workers",
		Stability:   Stable,
		Compatibility: Compatibility{
			Engines: map[string]string{},
		},
		Provenance: PluginProvenance{
			Repository: "https://github.com/wago-org/workers",
			License:    "Apache-2.0",
			Authors:    []string{"Wago"},
		},
		Authorities: []AuthorityRequest{{
			Name: "instance.manage", Mode: "required", Reason: "Own a bounded worker pool",
			Scope: AuthorityScope{MaxInstances: 8, MaxMemoryBytes: 64 << 20},
		}},
		ConfigSchema: json.RawMessage(`{
          "$schema":"https://json-schema.org/draft/2020-12/schema",
          "type":"object",
          "additionalProperties":false,
          "properties":{"workers":{"type":"integer","minimum":1}}
        }`),
		Provides: []ContractSpec{{ID: "github.com/wago-org/workers/service", Major: 1}},
	}
}

func validManifest(t *testing.T) Manifest {
	t.Helper()
	return Manifest{
		Schema: ManifestSchemaV1,
		Package: &PackageManifest{
			Module: "github.com/wago-org/workers", Version: "0.1.0", Name: "Workers",
			Description: "Bounded workers", Stability: Stable, License: "Apache-2.0", Repository: "https://github.com/wago-org/workers",
			Authors: []PackageAuthor{{Name: "Wago", Github: "wago-org"}},
		},
	}
}

func validCatalog(t *testing.T) []ProviderManifest {
	t.Helper()
	definition := validDefinition("github.com/wago-org/workers", "0.1.0")
	digest, err := DefinitionDigest(definition)
	if err != nil {
		t.Fatalf("digest fixture: %v", err)
	}
	return []ProviderManifest{{
		ImportPath: "github.com/wago-org/workers/register", Definition: definition, DefinitionDigest: digest,
	}}
}

func TestManifestV1ValidatesExplicitProviderCatalog(t *testing.T) {
	manifest := validManifest(t)
	manifest.Plugins = map[string]string{"github.com/wago-org/wasi": "^0.1.0"}
	manifest.Settings = json.RawMessage(`{
		"features":{"simd":true},
		"optimizations":{"inline":false},
		"runtime":{"parallel":"auto","deferredBoundsChecking":true}
	}`)
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	catalog := validCatalog(t)
	if err := ValidateProviderCatalog(*manifest.Package, "0.1.0", catalog); err != nil {
		t.Fatalf("ValidateProviderCatalog: %v", err)
	}
	provider := catalog[0]
	if provider.Definition.ID != "github.com/wago-org/workers" {
		t.Fatalf("full plugin ID lost: %q", provider.Definition.ID)
	}
	if !strings.HasPrefix(provider.DefinitionDigest, "sha256:") || len(provider.DefinitionDigest) != 71 {
		t.Fatalf("definition digest = %q, want algorithm-prefixed SHA-256", provider.DefinitionDigest)
	}
}

func TestPluginIDMatchesV1SchemaGrammar(t *testing.T) {
	for _, valid := range []string{
		"github.com/wago-org/wasi",
		"GitHub.COM/Owner/Plugin_Name.v2~beta",
		"x.io/A/b-c_d.e~f9",
		"sub-domain.example.co.uk/path/Segment2",
	} {
		if err := ValidatePluginID(valid); err != nil {
			t.Errorf("ValidatePluginID(%q) = %v, want valid", valid, err)
		}
	}
	for _, invalid := range []string{
		"github.com",
		"localhost/plugin",
		"-github.com/plugin",
		"github-.com/plugin",
		"github..com/plugin",
		"github.com//plugin",
		"github.com/.plugin",
		"github.com/plugin_",
		"github.com/plugin+extra",
		"github.com/plugin@v1",
		"github.com/plugin!extra",
		"github.com/plugin name",
		"github.com/plügïn",
	} {
		if err := ValidatePluginID(invalid); err == nil {
			t.Errorf("ValidatePluginID(%q) accepted schema-invalid ID", invalid)
		}
	}
}

func TestManifestSettingsAndPackageMetadataFailClosed(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"empty plugin range": func(m *Manifest) {
			m.Plugins = map[string]string{"github.com/acme/dependency": ""}
		},
		"unknown settings field": func(m *Manifest) {
			m.Settings = json.RawMessage(`{"runtime":{"parallel":"auto","futureMode":true}}`)
		},
		"unknown feature": func(m *Manifest) {
			m.Settings = json.RawMessage(`{"features":{"telepathy":true}}`)
		},
		"bad parallel": func(m *Manifest) {
			m.Settings = json.RawMessage(`{"runtime":{"parallel":"four"}}`)
		},
		"open category": func(m *Manifest) {
			m.Package.Category = "Host Imports"
		},
		"bad author": func(m *Manifest) {
			m.Package.Authors[0].Github = "-owner"
		},
		"duplicate author provenance name": func(m *Manifest) {
			m.Package.Authors = append(m.Package.Authors, PackageAuthor{Name: m.Package.Authors[0].Name, Github: "another-owner"})
		},
		"bad subpackage metadata": func(m *Manifest) {
			m.Package.Subpackages = []PackageSub{{
				Module: "github.com/wago-org/workers/addon", Name: "Addon", Description: "Addon",
				Platforms: []string{"linux"},
			}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			manifest := validManifest(t)
			mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("Validate succeeded, want rejection")
			}
		})
	}
}

func TestManifestRejectsV0AndUnknownNestedFields(t *testing.T) {
	for name, raw := range map[string]string{
		"v0":      `{"$schema":"https://wago.sh/v0/schema.json","module":"github.com/acme/plugin"}`,
		"root":    `{"$schema":"https://wago.sh/v1/schema.json","package":{},"subpackages":[]}`,
		"package": `{"$schema":"https://wago.sh/v1/schema.json","package":{"module":"github.com/acme/plugin","version":"1.0.0","name":"Plugin","description":"Plugin","license":"MIT","repository":"https://github.com/acme/plugin","authors":[{"name":"A"}],"mystery":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var manifest Manifest
			err := json.Unmarshal([]byte(raw), &manifest)
			if name == "v0" {
				if err == nil {
					err = manifest.Validate()
				}
				if err == nil {
					t.Fatal("v0 manifest accepted")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v, want unknown-field rejection", err)
			}
		})
	}
	for name, raw := range map[string]string{
		"provider":   `{"importPath":"github.com/acme/plugin/register","definition":{},"definitionDigest":"sha256:00","factory":"New"}`,
		"definition": `{"importPath":"github.com/acme/plugin/register","definition":{"id":"github.com/acme/plugin","version":"1.0.0","futurePower":true},"definitionDigest":"sha256:00"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var provider ProviderManifest
			if err := decodeStrict([]byte(raw), &provider); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v, want strict nested-field rejection", err)
			}
		})
	}
}

func TestDefinitionDigestCanonicalizesSetLikeFieldsAndSchema(t *testing.T) {
	left := validDefinition("github.com/wago-org/workers", "0.1.0")
	right := left
	left.Compatibility.Platforms = []string{"linux/amd64", "darwin/arm64"}
	right.Compatibility.Platforms = []string{"darwin/arm64", "linux/amd64"}
	left.Provenance.Authors = []string{"Wago", "Contributors"}
	right.Provenance.Authors = []string{"Contributors", "Wago"}
	right.ConfigSchema = json.RawMessage(`{"properties":{"workers":{"minimum":1,"type":"integer"}},"additionalProperties":false,"type":"object","$schema":"https://json-schema.org/draft/2020-12/schema"}`)
	leftDigest, err := DefinitionDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := DefinitionDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("canonical digests differ:\nleft  %s\nright %s", leftDigest, rightDigest)
	}
}

func TestCatalogRequiresExactCompleteManifestProviderSet(t *testing.T) {
	manifest := validManifest(t)
	manifest.Package.Homepage = "https://wago.sh/workers"
	manifest.Package.Engines = map[string]string{"wago": ">=0.1.0 <1.0.0"}
	manifest.Package.Platforms = []string{"linux/amd64", "darwin/arm64"}
	manifest.Package.Subpackages = []PackageSub{{
		Module: "github.com/wago-org/workers/metrics", Name: "Metrics", Description: "Worker metrics",
	}}
	catalog := validCatalog(t)
	catalog[0].Definition.Provenance.Homepage = manifest.Package.Homepage
	catalog[0].Definition.Compatibility = Compatibility{
		Engines: manifest.Package.Engines, Platforms: append([]string(nil), manifest.Package.Platforms...),
	}
	rootDigest, err := DefinitionDigest(catalog[0].Definition)
	if err != nil {
		t.Fatal(err)
	}
	catalog[0].DefinitionDigest = rootDigest
	metrics := validDefinition("github.com/wago-org/workers/metrics", "0.1.0")
	metrics.Name, metrics.Description = "Metrics", "Worker metrics"
	metrics.Stability = manifest.Package.Stability
	metrics.Compatibility = Compatibility{
		Engines: manifest.Package.Engines, Platforms: append([]string(nil), manifest.Package.Platforms...),
	}
	metrics.Provenance.Homepage = manifest.Package.Homepage
	metricsDigest, err := DefinitionDigest(metrics)
	if err != nil {
		t.Fatal(err)
	}
	catalog = append(catalog, ProviderManifest{
		ImportPath: manifest.Package.Module + "/register", Definition: metrics, DefinitionDigest: metricsDigest,
	})
	if err := ValidateProviderCatalog(*manifest.Package, "v0.1.0", catalog); err != nil {
		t.Fatalf("exact catalog rejected: %v", err)
	}

	for name, mutate := range map[string]func([]ProviderManifest){
		"omitted subpackage":  func(c []ProviderManifest) { c[1] = c[0] },
		"undeclared provider": func(c []ProviderManifest) { c[1].Definition.ID = manifest.Package.Module + "/other" },
		"alternate register":  func(c []ProviderManifest) { c[0].ImportPath = manifest.Package.Module + "/internal/register" },
		"display drift":       func(c []ProviderManifest) { c[0].Definition.Name = "Different" },
		"compatibility drift": func(c []ProviderManifest) { c[0].Definition.Compatibility.Platforms = []string{"linux/amd64"} },
		"provenance drift":    func(c []ProviderManifest) { c[0].Definition.Provenance.Homepage = "" },
	} {
		t.Run(name, func(t *testing.T) {
			copyCatalog := append([]ProviderManifest(nil), catalog...)
			mutate(copyCatalog)
			if err := ValidateProviderCatalog(*manifest.Package, "v0.1.0", copyCatalog); err == nil {
				t.Fatal("ValidateProviderCatalog succeeded, want rejection")
			}
		})
	}
}

func TestCatalogRejectsDigestPathDependencyAndScopeViolations(t *testing.T) {
	tests := map[string]func([]ProviderManifest){
		"digest mismatch":         func(c []ProviderManifest) { c[0].DefinitionDigest = "sha256:" + strings.Repeat("0", 64) },
		"provider outside source": func(c []ProviderManifest) { c[0].ImportPath = "github.com/other/plugin/register" },
		"duplicate definition":    func(c []ProviderManifest) { c[0].Definition.ID = "github.com/wago-org/workers" },
		"self dependency": func(c []ProviderManifest) {
			c[0].Definition.Requires = []PluginRequirement{{ID: "github.com/wago-org/workers", Version: "^0.1.0"}}
		},
		"empty dependency range": func(c []ProviderManifest) {
			c[0].Definition.Requires = []PluginRequirement{{ID: "github.com/wago-org/dependency", Version: ""}}
		},
		"unknown authority": func(c []ProviderManifest) { c[0].Definition.Authorities[0].Name = "runtime.*" },
		"scope widening": func(c []ProviderManifest) {
			c[0].Definition.Authorities[0] = AuthorityRequest{Name: "module.compile.observe", Mode: "required", Reason: "Observe", Scope: AuthorityScope{Modules: []string{"env"}}}
		},
		"unbounded manage": func(c []ProviderManifest) { c[0].Definition.Authorities[0].Scope.MaxMemoryBytes = 0 },
		"open config":      func(c []ProviderManifest) { c[0].Definition.ConfigSchema = json.RawMessage(`{"type":"object"}`) },
		"duplicate contract": func(c []ProviderManifest) {
			c[0].Definition.Provides = append(c[0].Definition.Provides, c[0].Definition.Provides[0])
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			manifest := validManifest(t)
			catalog := validCatalog(t)
			if name == "duplicate definition" {
				catalog = append(catalog, catalog[0])
			}
			mutate(catalog)
			if err := ValidateProviderCatalog(*manifest.Package, "0.1.0", catalog); err == nil {
				t.Fatal("Validate succeeded, want rejection")
			}
		})
	}
}

func TestManifestAndDefinitionAllowHTTPSHomepageFragments(t *testing.T) {
	manifest := validManifest(t)
	manifest.Package.Homepage = "https://github.com/wago-org/workers#readme"
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest homepage fragment: %v", err)
	}
	definition := validDefinition("github.com/wago-org/workers", "0.1.0")
	definition.Provenance.Homepage = manifest.Package.Homepage
	if err := ValidatePluginDefinition(definition); err != nil {
		t.Fatalf("definition homepage fragment: %v", err)
	}
}

func TestCatalogAcceptsModuleCloseObserverAuthority(t *testing.T) {
	manifest := validManifest(t)
	catalog := validCatalog(t)
	catalog[0].Definition.Authorities = []AuthorityRequest{{
		Name: "module.close.observe", Mode: "required", Reason: "Release module state",
	}}
	digest, err := DefinitionDigest(catalog[0].Definition)
	if err != nil {
		t.Fatal(err)
	}
	catalog[0].DefinitionDigest = digest
	if err := ValidateProviderCatalog(*manifest.Package, "0.1.0", catalog); err != nil {
		t.Fatalf("module close observer authority rejected: %v", err)
	}
}

func TestVersionRangesIntersectAndCompareSemantically(t *testing.T) {
	match, err := VersionSatisfiesAll("0.3.5", []string{"^0.3.0", ">=0.3.4 <0.4.0"})
	if err != nil || !match {
		t.Fatalf("intersection = %v, %v; want match", match, err)
	}
	match, err = VersionSatisfiesAll("0.4.0", []string{"^0.3.0", ">=0.3.4"})
	if err != nil || match {
		t.Fatalf("conflicting intersection = %v, %v; want no match", match, err)
	}
	cmp, err := CompareVersions("0.10.0", "0.9.0")
	if err != nil || cmp <= 0 {
		t.Fatalf("CompareVersions = %d, %v; want 0.10.0 newer", cmp, err)
	}
}

func TestVersionGrammarMatchesWago(t *testing.T) {
	for _, valid := range []string{"0.0.0", "v1.2.3", "1.2.3-beta.1+build.7"} {
		if err := ValidateVersion(valid); err != nil {
			t.Errorf("ValidateVersion(%q) = %v, want valid", valid, err)
		}
	}
	for _, invalid := range []string{"V1.2.3", "1.2", "01.2.3", "1.2.3+", "1.2.3+bad!"} {
		if err := ValidateVersion(invalid); err == nil {
			t.Errorf("ValidateVersion(%q) accepted invalid version", invalid)
		}
	}
}

func TestVersionRangeGrammarMatchesWagoForms(t *testing.T) {
	for _, tc := range []struct {
		version, constraint string
		want                bool
	}{
		{"1.0.0", "1.0.0 || 2.0.0", true},
		{"1.2.9", "1.2.x", true},
		{"1.3.0", "1.2.x", false},
		{"0.2.9", "^0.2.3", true},
		{"0.3.0", "^0.2.3", false},
		{"1.2.9", "~1.2.3", true},
		{"1.3.0", "~1.2.3", false},
		{"2.3.4", "1.2.3 - 2.3.4", true},
		{"2.3.5", "1.2.3 - 2.3.4", false},
		{"1.2.3-beta.2", ">=1.2.3-beta.1 <1.3.0", true},
		{"1.2.4-beta", ">=1.2.3-alpha <1.3.0", false},
	} {
		got, err := VersionSatisfiesAll(tc.version, []string{tc.constraint})
		if err != nil || got != tc.want {
			t.Errorf("VersionSatisfiesAll(%q, %q) = %v, %v; want %v, nil", tc.version, tc.constraint, got, err, tc.want)
		}
	}
	for _, invalid := range []string{"1.2.3.4", ">=", "^1.2.3.4", ">=abc", "v1.2.3", "1.2.3 - - 2.0.0"} {
		if err := ValidateVersionRange(invalid); err == nil {
			t.Errorf("ValidateVersionRange(%q) accepted malformed range", invalid)
		}
	}
}
