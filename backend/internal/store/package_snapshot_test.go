package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

func TestPackageSnapshotsOwnMutableState(t *testing.T) {
	forEachPackageStore(t, func(t *testing.T, dataStore Store) {
		want := packageSnapshotFixture()
		input := packageSnapshotFixture()
		if err := dataStore.UpsertPackage(input); err != nil {
			t.Fatalf("upsert package: %v", err)
		}

		mutatePackageSnapshot(&input, "caller")
		assertStoredPackage(t, dataStore, want)

		read, ok := dataStore.GetPackage(want.Short)
		if !ok {
			t.Fatal("get package: package not found")
		}
		mutatePackageSnapshot(&read, "get")
		assertStoredPackage(t, dataStore, want)

		listed := dataStore.ListPackages()
		if len(listed) != 1 {
			t.Fatalf("list packages returned %d packages, want 1", len(listed))
		}
		mutatePackageSnapshot(&listed[0], "list")
		assertStoredPackage(t, dataStore, want)
	})
}

func TestPackageSnapshotsAreRaceIsolated(t *testing.T) {
	forEachPackageStore(t, func(t *testing.T, dataStore Store) {
		want := packageSnapshotFixture()
		if err := dataStore.UpsertPackage(want); err != nil {
			t.Fatalf("upsert package: %v", err)
		}

		const workers = 8
		const iterations = 100
		var group sync.WaitGroup
		group.Add(workers)
		for worker := 0; worker < workers; worker++ {
			worker := worker
			go func() {
				defer group.Done()
				marker := strconv.Itoa(worker)
				for iteration := 0; iteration < iterations; iteration++ {
					read, ok := dataStore.GetPackage(want.Short)
					if !ok {
						t.Errorf("get package: package not found")
						return
					}
					mutatePackageSnapshot(&read, marker)
					listed := dataStore.ListPackages()
					if len(listed) != 1 {
						t.Errorf("list packages returned %d packages, want 1", len(listed))
						return
					}
					mutatePackageSnapshot(&listed[0], marker)
				}
			}()
		}
		group.Wait()
		assertStoredPackage(t, dataStore, want)
	})
}

func forEachPackageStore(t *testing.T, run func(*testing.T, Store)) {
	t.Helper()
	t.Run("json", func(t *testing.T) {
		dataStore, err := Open(filepath.Join(t.TempDir(), "store.json"))
		if err != nil {
			t.Fatalf("open JSON store: %v", err)
		}
		run(t, dataStore)
	})
	t.Run("pebble", func(t *testing.T) {
		dataStore, err := OpenPebble(t.TempDir())
		if err != nil {
			t.Fatalf("open Pebble store: %v", err)
		}
		t.Cleanup(func() {
			if err := dataStore.Close(); err != nil {
				t.Errorf("close Pebble store: %v", err)
			}
		})
		run(t, dataStore)
	})
}

func packageSnapshotFixture() model.Package {
	return model.Package{
		Name: "github.com/acme/plugin", Short: "github.com/acme/plugin",
		Tags: []string{"runtime"}, Keywords: []string{"wasm"},
		AllowedPublishers: []string{"publisher"}, Dependencies: []string{"github.com/acme/dependency"},
		Compat: model.Compatibility{
			Engines: map[string]string{"wago": "^1.0.0"}, Platforms: []string{"linux/amd64"},
		},
		Authors: []model.Author{{Name: "Author"}},
		Subpackages: []model.PackageSub{{
			Module: "github.com/acme/plugin/child", Name: "Child", Description: "Child provider",
			Tags: []string{"child"}, Engines: map[string]string{"wago": "^1.0.0"}, Platforms: []string{"linux/amd64"},
		}},
		Contributors: []string{"contributor"},
		Versions: []model.Version{{
			Version: "v1.0.0", Latest: true,
			Providers: []model.PublishedProvider{{
				ID: "github.com/acme/plugin", Definition: model.PluginDefinition{
					ID: "github.com/acme/plugin", Version: "1.0.0",
					Compatibility: model.Compatibility{
						Engines: map[string]string{"wago": "^1.0.0"}, Platforms: []string{"linux/amd64"},
					},
					Provenance: model.PluginProvenance{Authors: []string{"Author"}},
					Requires:   []model.PluginRequirement{{ID: "github.com/acme/dependency", Version: "^1.0.0"}},
					Authorities: []model.AuthorityRequest{{
						Name: "host.import.define", Scope: model.AuthorityScope{Modules: []string{"acme"}},
					}},
					ConfigSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
					Provides:     []model.ContractSpec{{ID: "github.com/acme/contract", Major: 1}},
					Consumes:     []model.ContractRequirement{{ID: "github.com/acme/input", Major: 1, Mode: "required"}},
				},
			}},
		}},
		Issues: []model.Issue{{Num: 1, Labels: []string{"bug"}}},
	}
}

func mutatePackageSnapshot(pack *model.Package, marker string) {
	pack.Tags[0] = marker
	pack.Keywords[0] = marker
	pack.AllowedPublishers[0] = marker
	pack.Dependencies[0] = marker
	pack.Compat.Engines["wago"] = marker
	pack.Compat.Platforms[0] = marker
	pack.Authors[0].Name = marker
	pack.Subpackages[0].Tags[0] = marker
	pack.Subpackages[0].Engines["wago"] = marker
	pack.Subpackages[0].Platforms[0] = marker
	pack.Contributors[0] = marker
	pack.Versions[0].Deprecated = true
	definition := &pack.Versions[0].Providers[0].Definition
	definition.Compatibility.Engines["wago"] = marker
	definition.Compatibility.Platforms[0] = marker
	definition.Provenance.Authors[0] = marker
	definition.Requires[0].Version = marker
	definition.Authorities[0].Scope.Modules[0] = marker
	definition.ConfigSchema[0] = ' '
	definition.Provides[0].Major++
	definition.Consumes[0].Mode = marker
	pack.Issues[0].Labels[0] = marker
}

func assertStoredPackage(t *testing.T, dataStore Store, want model.Package) {
	t.Helper()
	got, ok := dataStore.GetPackage(want.Short)
	if !ok {
		t.Fatal("stored package not found")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored package changed through an aliased snapshot\n got: %#v\nwant: %#v", got, want)
	}
}
