package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

func TestFullGitCommit(t *testing.T) {
	if !fullGitCommit(strings.Repeat("a", 40)) {
		t.Fatal("rejected full lowercase commit")
	}
	for _, invalid := range []string{"", strings.Repeat("a", 39), strings.Repeat("A", 40), strings.Repeat("g", 40)} {
		if fullGitCommit(invalid) {
			t.Fatalf("accepted invalid commit %q", invalid)
		}
	}
}

func version(value string) model.Version { return model.Version{Version: value} }

func TestPackageKeyUsesFullCanonicalSourceModule(t *testing.T) {
	for _, module := range []string{
		"github.com/wago-org/wasi",
		"github.com/acme/monorepo/plugins/pool",
	} {
		if got := shortFromModule(module); got != module {
			t.Errorf("shortFromModule(%q) = %q, want full module", module, got)
		}
	}
	for _, invalid := range []string{"wago-org/wasi", "gitlab.com/acme/plugin", "github.com/acme/plugin+bad"} {
		if got := shortFromModule(invalid); got != "" {
			t.Errorf("shortFromModule(%q) = %q, want rejection", invalid, got)
		}
	}
}

func TestModuleReleaseTagFollowsGoModuleTagRules(t *testing.T) {
	for _, test := range []struct {
		module, version, want string
	}{
		{"github.com/acme/workers", "v0.1.0", "v0.1.0"},
		{"github.com/acme/monorepo/plugins/pool", "v1.2.3", "plugins/pool/v1.2.3"},
		{"github.com/acme/monorepo/plugins/pool/v2", "v2.3.4", "plugins/pool/v2.3.4"},
		{"github.com/acme/monorepo/v2", "v2.0.1", "v2.0.1"},
	} {
		if got := moduleReleaseTag(test.module, test.version); got != test.want {
			t.Errorf("moduleReleaseTag(%q, %q) = %q, want %q", test.module, test.version, got, test.want)
		}
	}
}

func TestApplyReleaseIsImmutableForEveryVersion(t *testing.T) {
	versions, conflict := applyRelease(nil, version("0.0.0"))
	if conflict || len(versions) != 1 || !versions[0].Latest {
		t.Fatalf("first release = %+v, conflict %v", versions, conflict)
	}
	if _, conflict = applyRelease(versions, version("0.0.0")); !conflict {
		t.Fatal("0.0.0 must be immutable in v1")
	}
	if _, conflict = applyRelease(versions, version("v0.0.0")); !conflict {
		t.Fatal("v-prefix alias must not bypass version immutability")
	}
	versions, conflict = applyRelease(versions, version("0.1.0"))
	if conflict || len(versions) != 2 || versions[0].Latest || !versions[1].Latest {
		t.Fatalf("second release = %+v, conflict %v", versions, conflict)
	}
	versions, conflict = applyRelease(versions, version("0.0.5"))
	if conflict || len(versions) != 3 || !versions[1].Latest || versions[2].Latest {
		t.Fatalf("older release changed latest = %+v, conflict %v", versions, conflict)
	}
	versions = append(versions, version("0.2.0-beta.1"), version("0.2.0"))
	markLatestVersion(versions)
	if !versions[len(versions)-1].Latest || versions[len(versions)-2].Latest {
		t.Fatalf("semantic latest did not prefer release over prerelease: %+v", versions)
	}
}

func TestProviderDependenciesAreCanonicalAndUnique(t *testing.T) {
	providers := []model.PublishedProvider{
		{Definition: model.PluginDefinition{Requires: []model.PluginRequirement{
			{ID: "github.com/wago-org/workers", Version: "^0.1.0"},
			{ID: "github.com/JairusSW/wide", Version: "1.x"},
		}}},
		{Definition: model.PluginDefinition{Requires: []model.PluginRequirement{
			{ID: "github.com/wago-org/workers", Version: ">=0.1.1"},
		}}},
	}
	want := []string{"github.com/JairusSW/wide", "github.com/wago-org/workers"}
	if got := providerDependencies(providers); !reflect.DeepEqual(got, want) {
		t.Fatalf("providerDependencies = %v, want %v", got, want)
	}
}

func TestOlderReleaseDoesNotBecomePackageMetadataAuthority(t *testing.T) {
	pkg := model.Package{DisplayName: "Current", Versions: []model.Version{{Version: "2.0.0", Latest: true}}}
	versions, conflict := applyRelease(pkg.Versions, model.Version{Version: "1.0.0"})
	if conflict {
		t.Fatal("older release unexpectedly conflicted")
	}
	pkg.Versions = versions
	if packageVersionIsLatest(pkg, "1.0.0") {
		t.Fatal("older release became package metadata authority")
	}
	if !packageVersionIsLatest(pkg, "v2.0.0") {
		t.Fatal("v-prefix normalization lost the latest release")
	}
}

func TestReleaseFingerprintCoversDefinitionDependencyAndSource(t *testing.T) {
	provider := model.PublishedProvider{
		ID:     "github.com/wago-org/pool",
		Source: model.PluginSource{Module: "github.com/wago-org/pool", Version: "0.1.0", Checksum: "h1:source"},
		Definition: model.PluginDefinition{
			ID: "github.com/wago-org/pool", Version: "0.1.0",
			Requires: []model.PluginRequirement{{ID: "github.com/wago-org/workers", Version: "^0.1.0"}},
		},
		DefinitionDigest: "sha256:definition",
	}
	release := model.Version{Version: "0.1.0", SourceChecksum: provider.Source.Checksum, Providers: []model.PublishedProvider{provider}}
	base := releaseFingerprint("github.com/wago-org/pool", release)
	if !strings.HasPrefix(base, "sha256:") || len(base) != 71 {
		t.Fatalf("fingerprint = %q", base)
	}
	release.Providers[0].Definition.Requires[0].Version = "^0.2.0"
	if changed := releaseFingerprint("github.com/wago-org/pool", release); changed == base {
		t.Fatal("dependency change did not change release fingerprint")
	}
	release.Providers[0].Definition.Requires[0].Version = "^0.1.0"
	release.SourceChecksum = "h1:other"
	if changed := releaseFingerprint("github.com/wago-org/pool", release); changed == base {
		t.Fatal("source checksum change did not change release fingerprint")
	}
	release.SourceChecksum = provider.Source.Checksum
	release.Providers = append(release.Providers, model.PublishedProvider{
		ID: "github.com/wago-org/pool/aux", ImportPath: "github.com/wago-org/pool/register/aux",
		DefinitionDigest: "sha256:aux",
	})
	reversed := release
	reversed.Providers = []model.PublishedProvider{release.Providers[1], release.Providers[0]}
	if releaseFingerprint("github.com/wago-org/pool", release) != releaseFingerprint("github.com/wago-org/pool", reversed) {
		t.Fatal("provider catalog order changed the release fingerprint")
	}
}
