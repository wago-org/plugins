package store

import "github.com/wago-org/registry-backend/internal/model"

// clonePackage gives callers an independent snapshot of every mutable package
// field. Store methods otherwise expose the backing slices and maps held under
// their mutex only while copying the outer struct, allowing an API handler to
// mutate store-owned memory after the lock has been released.
func clonePackage(pack model.Package) model.Package {
	pack.Tags = cloneSlice(pack.Tags)
	pack.Keywords = cloneSlice(pack.Keywords)
	pack.AllowedPublishers = cloneSlice(pack.AllowedPublishers)
	pack.Dependencies = cloneSlice(pack.Dependencies)
	pack.Compat = cloneCompatibility(pack.Compat)
	pack.Authors = cloneSlice(pack.Authors)
	pack.Subpackages = clonePackageSubs(pack.Subpackages)
	pack.Contributors = cloneSlice(pack.Contributors)
	pack.Versions = cloneVersions(pack.Versions)
	pack.Issues = cloneIssues(pack.Issues)
	return pack
}

func cloneCompatibility(compat model.Compatibility) model.Compatibility {
	compat.Engines = cloneMap(compat.Engines)
	compat.Platforms = cloneSlice(compat.Platforms)
	return compat
}

func clonePackageSubs(subpackages []model.PackageSub) []model.PackageSub {
	cloned := cloneSlice(subpackages)
	for index := range cloned {
		cloned[index].Tags = cloneSlice(cloned[index].Tags)
		cloned[index].Engines = cloneMap(cloned[index].Engines)
		cloned[index].Platforms = cloneSlice(cloned[index].Platforms)
	}
	return cloned
}

func cloneVersions(versions []model.Version) []model.Version {
	cloned := cloneSlice(versions)
	for index := range cloned {
		cloned[index].Providers = clonePublishedProviders(cloned[index].Providers)
	}
	return cloned
}

func clonePublishedProviders(providers []model.PublishedProvider) []model.PublishedProvider {
	cloned := cloneSlice(providers)
	for index := range cloned {
		cloned[index].Definition = clonePluginDefinition(cloned[index].Definition)
	}
	return cloned
}

func clonePluginDefinition(definition model.PluginDefinition) model.PluginDefinition {
	definition.Compatibility = cloneCompatibility(definition.Compatibility)
	definition.Provenance.Authors = cloneSlice(definition.Provenance.Authors)
	definition.Requires = cloneSlice(definition.Requires)
	definition.Authorities = cloneSlice(definition.Authorities)
	for index := range definition.Authorities {
		definition.Authorities[index].Scope.Modules = cloneSlice(definition.Authorities[index].Scope.Modules)
	}
	definition.ConfigSchema = cloneSlice(definition.ConfigSchema)
	definition.Provides = cloneSlice(definition.Provides)
	definition.Consumes = cloneSlice(definition.Consumes)
	return definition
}

func cloneIssues(issues []model.Issue) []model.Issue {
	cloned := cloneSlice(issues)
	for index := range cloned {
		cloned[index].Labels = cloneSlice(cloned[index].Labels)
	}
	return cloned
}

func cloneSlice[T any](values []T) []T {
	if values == nil {
		return nil
	}
	cloned := make([]T, len(values))
	copy(cloned, values)
	return cloned
}

func cloneMap[K comparable, V any](values map[K]V) map[K]V {
	if values == nil {
		return nil
	}
	cloned := make(map[K]V, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
