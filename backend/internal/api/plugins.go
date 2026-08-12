package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

type catalogProvider struct {
	provider            model.PublishedProvider
	packageKey          string
	packageModule       string
	releaseVersion      string
	releaseChecksum     string
	releaseFingerprint  string
	expectedFingerprint string
	releaseDeprecated   bool
	persisted           bool
}

// pluginResolution is the strict catalog shape consumed by Wago's lock solver.
// Authority, config, dependency, contract, compatibility, and provenance data
// remain inside the exact digest-bearing Definition so there is one source of
// truth and strict clients need no redundant projections.
type pluginResolution struct {
	ID                 string                 `json:"id"`
	Version            string                 `json:"version"`
	Source             model.PluginSource     `json:"source"`
	Provider           map[string]string      `json:"provider"`
	Definition         model.PluginDefinition `json:"definition"`
	DefinitionDigest   string                 `json:"definitionDigest"`
	ReleaseFingerprint string                 `json:"releaseFingerprint"`
}

func (a *App) handleResolvePlugin(w http.ResponseWriter, r *http.Request) {
	if !validatePluginQuery(w, r, map[string]bool{"id": false, "range": true}) {
		return
	}
	id, ranges, ok := parseCandidateQuery(w, r)
	if !ok {
		return
	}
	entries, err := matchingCatalogProviders(a.catalogProviders(), id, ranges, true)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(entries) == 0 {
		if a.catalogContainsPlugin(id) {
			httpx.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("no published version of %s satisfies %s", id, strings.Join(ranges, " and ")))
			return
		}
		httpx.WriteError(w, http.StatusNotFound, "plugin not found")
		return
	}
	entry := entries[0]
	if err := validateCatalogProvider(entry, true); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "stored plugin metadata failed validation: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"plugin": projectResolution(entry)})
}

func (a *App) handlePluginCandidates(w http.ResponseWriter, r *http.Request) {
	if !validatePluginQuery(w, r, map[string]bool{
		"id": false, "range": true, "includeDeprecated": false, "limit": false, "offset": false,
	}) {
		return
	}
	id, ranges, ok := parseCandidateQuery(w, r)
	if !ok {
		return
	}
	includeDeprecated := false
	switch raw := r.URL.Query().Get("includeDeprecated"); raw {
	case "", "false":
	case "true":
		includeDeprecated = true
	default:
		httpx.WriteError(w, http.StatusBadRequest, "includeDeprecated must be true or false")
		return
	}
	limit, offset, ok := parseCandidatePage(w, r)
	if !ok {
		return
	}
	entries, err := matchingCatalogProviders(a.catalogProviders(), id, ranges, includeDeprecated)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(entries) == 0 && a.catalogContainsPlugin(id) {
		httpx.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("no published version of %s satisfies %s", id, strings.Join(ranges, " and ")))
		return
	}
	if len(entries) == 0 {
		httpx.WriteError(w, http.StatusNotFound, "plugin not found")
		return
	}
	total := len(entries)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := entries[offset:end]
	out := make([]pluginResolution, 0, len(page))
	for _, entry := range page {
		if err := validateCatalogProvider(entry, true); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "stored plugin metadata failed validation: "+err.Error())
			return
		}
		out = append(out, projectResolution(entry))
	}
	response := map[string]any{"plugins": out, "total": total, "offset": offset, "limit": limit}
	if end < total {
		response["nextOffset"] = end
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func validatePluginQuery(w http.ResponseWriter, r *http.Request, allowed map[string]bool) bool {
	for key, values := range r.URL.Query() {
		repeated, ok := allowed[key]
		if !ok {
			httpx.WriteError(w, http.StatusBadRequest, "unknown query parameter: "+key)
			return false
		}
		if !repeated && len(values) != 1 {
			httpx.WriteError(w, http.StatusBadRequest, key+" must appear exactly once")
			return false
		}
	}
	return true
}

func parseCandidatePage(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	limit = 256
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 256 {
			httpx.WriteError(w, http.StatusBadRequest, "limit must be an integer from 1 to 256")
			return 0, 0, false
		}
		limit = parsed
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			httpx.WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return 0, 0, false
		}
		offset = parsed
	}
	return limit, offset, true
}

func parseCandidateQuery(w http.ResponseWriter, r *http.Request) (string, []string, bool) {
	ids := r.URL.Query()["id"]
	if len(ids) != 1 {
		httpx.WriteError(w, http.StatusBadRequest, "id must appear exactly once")
		return "", nil, false
	}
	id := ids[0]
	if err := model.ValidatePluginID(id); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid plugin id: "+err.Error())
		return "", nil, false
	}
	ranges := r.URL.Query()["range"]
	if len(ranges) == 0 {
		ranges = []string{"*"}
	}
	if len(ranges) > 128 {
		httpx.WriteError(w, http.StatusBadRequest, "too many version constraints")
		return "", nil, false
	}
	for _, constraint := range ranges {
		if constraint == "" {
			httpx.WriteError(w, http.StatusBadRequest, "version range must not be empty")
			return "", nil, false
		}
		if err := model.ValidateVersionRange(constraint); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return "", nil, false
		}
	}
	return id, ranges, true
}

func projectResolution(entry catalogProvider) pluginResolution {
	definition := entry.provider.Definition
	return pluginResolution{
		ID: definition.ID, Version: definition.Version, Source: entry.provider.Source,
		Provider: map[string]string{"importPath": entry.provider.ImportPath}, Definition: definition,
		DefinitionDigest: entry.provider.DefinitionDigest, ReleaseFingerprint: entry.releaseFingerprint,
	}
}

func (a *App) catalogContainsPlugin(id string) bool {
	for _, entry := range a.catalogProviders() {
		if entry.provider.ID == id || entry.provider.Definition.ID == id {
			return true
		}
	}
	return false
}

func (a *App) catalogProviders() []catalogProvider {
	packages := a.Store.ListPackages()
	var entries []catalogProvider
	for _, pkg := range packages {
		for _, release := range pkg.Versions {
			expectedFingerprint := releaseFingerprint(pkg.Name, release)
			for _, provider := range release.Providers {
				entries = append(entries, catalogProvider{
					provider:   provider,
					packageKey: pkg.Short, packageModule: pkg.Name,
					releaseVersion: release.Version, releaseChecksum: release.SourceChecksum,
					releaseFingerprint: release.ReleaseFingerprint, expectedFingerprint: expectedFingerprint,
					releaseDeprecated: release.Deprecated, persisted: true,
				})
			}
		}
	}
	return entries
}

func matchingCatalogProviders(entries []catalogProvider, id string, ranges []string, includeDeprecated bool) ([]catalogProvider, error) {
	var compatible []catalogProvider
	for _, entry := range entries {
		if entry.provider.ID != id || entry.provider.Definition.ID != id {
			continue
		}
		match, err := model.VersionSatisfiesAll(entry.provider.Definition.Version, ranges)
		if err != nil {
			return nil, err
		}
		if match {
			compatible = append(compatible, entry)
		}
	}
	sort.SliceStable(compatible, func(i, j int) bool {
		leftDeprecated := compatible[i].releaseDeprecated || compatible[i].provider.Definition.Stability == model.Deprecated
		rightDeprecated := compatible[j].releaseDeprecated || compatible[j].provider.Definition.Stability == model.Deprecated
		if leftDeprecated != rightDeprecated {
			return !leftDeprecated
		}
		cmp, _ := model.CompareVersions(compatible[i].provider.Definition.Version, compatible[j].provider.Definition.Version)
		if cmp != 0 {
			return cmp > 0
		}
		if compatible[i].persisted != compatible[j].persisted {
			return !compatible[i].persisted
		}
		return catalogProviderOrderKey(compatible[i]) < catalogProviderOrderKey(compatible[j])
	})
	if !includeDeprecated {
		live := compatible[:0]
		for _, entry := range compatible {
			if !entry.releaseDeprecated && entry.provider.Definition.Stability != model.Deprecated {
				live = append(live, entry)
			}
		}
		compatible = live
	}
	return compatible, nil
}

func catalogProviderOrderKey(entry catalogProvider) string {
	provider := entry.provider
	return strings.Join([]string{
		provider.Source.Module, provider.Source.Version, provider.Source.Checksum,
		provider.ImportPath, entry.releaseFingerprint, provider.DefinitionDigest,
	}, "\x00")
}

func validateCatalogProvider(entry catalogProvider, requireFingerprint bool) error {
	provider := entry.provider
	if provider.ID != provider.Definition.ID {
		return errors.New("provider id does not equal definition id")
	}
	if err := model.ValidatePluginDefinition(provider.Definition); err != nil {
		return err
	}
	if strings.TrimPrefix(provider.Source.Version, "v") != strings.TrimPrefix(provider.Definition.Version, "v") {
		return errors.New("source version does not equal definition version")
	}
	if err := model.ValidateSourceChecksum(provider.Source.Checksum); err != nil {
		return err
	}
	if provider.ImportPath != provider.Source.Module+"/register" {
		return errors.New("provider importPath is not the source module's register package")
	}
	if !model.PathBelongsToModule(provider.ID, provider.Source.Module) {
		return errors.New("plugin id does not belong to source module")
	}
	digest, err := model.DefinitionDigest(provider.Definition)
	if err != nil {
		return err
	}
	if provider.DefinitionDigest != digest {
		return errors.New("definition digest does not match definition")
	}
	if requireFingerprint {
		if err := model.ValidatePluginID(entry.packageModule); err != nil {
			return fmt.Errorf("stored package module: %w", err)
		}
		if entry.packageKey != entry.packageModule {
			return errors.New("stored package key does not equal its canonical module")
		}
		if err := model.ValidateVersion(entry.releaseVersion); err != nil {
			return fmt.Errorf("stored release version: %w", err)
		}
		if err := model.ValidateSourceChecksum(entry.releaseChecksum); err != nil {
			return fmt.Errorf("stored release checksum: %w", err)
		}
		if provider.Source.Module != entry.packageModule {
			return errors.New("provider source module does not equal its stored package module")
		}
		if provider.Source.Version != entry.releaseVersion {
			return errors.New("provider source version does not equal its stored release version")
		}
		if provider.Source.Checksum != entry.releaseChecksum {
			return errors.New("provider source checksum does not equal its stored release checksum")
		}
		if entry.releaseFingerprint == "" || entry.releaseFingerprint != entry.expectedFingerprint {
			return errors.New("release fingerprint does not match immutable release metadata")
		}
	}
	return nil
}

// validateProviderGraph proves each newly published definition has a complete,
// acyclic dependency and contract graph against immutable registry metadata.
func (a *App) validateProviderGraph(providers []model.PublishedProvider) error {
	entries := a.catalogProviders()
	for _, provider := range providers {
		for _, existing := range entries {
			if existing.persisted && existing.provider.ID == provider.ID && existing.provider.Source.Module != provider.Source.Module {
				return fmt.Errorf("plugin %s is already published by source module %s", provider.ID, existing.provider.Source.Module)
			}
		}
		entry := catalogProvider{provider: provider}
		if err := validateCatalogProvider(entry, false); err != nil {
			return fmt.Errorf("plugin %s: %w", provider.ID, err)
		}
		entries = append(entries, entry)
	}
	for _, root := range providers {
		_, err := resolveDefinitionGraph(entries, root)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", root.ID, err)
		}
	}
	return nil
}

const (
	maxDefinitionGraphPlugins = 512
	maxDefinitionSolverSteps  = 100000
)

func resolveDefinitionGraph(entries []catalogProvider, root model.PublishedProvider) (map[string]catalogProvider, error) {
	selected := map[string]catalogProvider{root.ID: {provider: root}}
	constraints := map[string]map[string]string{}
	addDefinitionConstraints(constraints, root.ID, root.Definition.Requires)
	solver := definitionGraphSolver{entries: entries}
	return solver.solve(selected, constraints)
}

type definitionGraphSolver struct {
	entries []catalogProvider
	steps   int
}

func (solver *definitionGraphSolver) solve(selected map[string]catalogProvider, constraints map[string]map[string]string) (map[string]catalogProvider, error) {
	if definitionGraphSize(selected, constraints) > maxDefinitionGraphPlugins {
		return nil, fmt.Errorf("dependency graph exceeds %d plugins", maxDefinitionGraphPlugins)
	}

	id := firstUnselectedDependency(selected, constraints)
	if id == "" {
		if err := validateCombinedPluginGraph(selected); err != nil {
			return nil, err
		}
		return selected, nil
	}
	ranges := sortedConstraintValues(constraints[id])
	candidates, err := matchingCatalogProviders(solver.entries, id, ranges, false)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("dependency %s has no version satisfying %s", id, strings.Join(ranges, " and "))
	}

	var lastErr error
	for _, candidate := range candidates {
		solver.steps++
		if solver.steps > maxDefinitionSolverSteps {
			return nil, fmt.Errorf("dependency resolution exceeded %d candidate steps", maxDefinitionSolverSteps)
		}
		if err := validateCatalogProvider(candidate, candidate.persisted); err != nil {
			return nil, fmt.Errorf("dependency %s metadata: %w", id, err)
		}
		if err := sharedSourceReleaseConflict(selected, candidate); err != nil {
			lastErr = err
			continue
		}

		nextSelected := cloneDefinitionSelections(selected)
		nextConstraints := cloneDefinitionConstraints(constraints)
		nextSelected[id] = candidate
		addDefinitionConstraints(nextConstraints, id, candidate.provider.Definition.Requires)
		if err := validateSelectedConstraints(nextSelected, nextConstraints); err != nil {
			lastErr = err
			continue
		}
		resolved, err := solver.solve(nextSelected, nextConstraints)
		if err == nil {
			return resolved, nil
		}
		if solver.steps > maxDefinitionSolverSteps {
			return nil, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("dependency %s has no release completing the graph: %w", id, lastErr)
	}
	return nil, fmt.Errorf("dependency %s has no release completing the graph", id)
}

func addDefinitionConstraints(constraints map[string]map[string]string, source string, requirements []model.PluginRequirement) {
	for _, requirement := range requirements {
		if constraints[requirement.ID] == nil {
			constraints[requirement.ID] = map[string]string{}
		}
		constraints[requirement.ID][source] = requirement.Version
	}
}

func definitionGraphSize(selected map[string]catalogProvider, constraints map[string]map[string]string) int {
	ids := make(map[string]struct{}, len(selected)+len(constraints))
	for id := range selected {
		ids[id] = struct{}{}
	}
	for id := range constraints {
		ids[id] = struct{}{}
	}
	return len(ids)
}

func firstUnselectedDependency(selected map[string]catalogProvider, constraints map[string]map[string]string) string {
	ids := make([]string, 0, len(constraints))
	for id := range constraints {
		if _, ok := selected[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func sortedConstraintValues(contributions map[string]string) []string {
	sources := make([]string, 0, len(contributions))
	for source := range contributions {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	values := make([]string, len(sources))
	for index, source := range sources {
		values[index] = contributions[source]
	}
	return values
}

func cloneDefinitionSelections(input map[string]catalogProvider) map[string]catalogProvider {
	output := make(map[string]catalogProvider, len(input)+1)
	for id, entry := range input {
		output[id] = entry
	}
	return output
}

func cloneDefinitionConstraints(input map[string]map[string]string) map[string]map[string]string {
	output := make(map[string]map[string]string, len(input)+1)
	for id, contributions := range input {
		copy := make(map[string]string, len(contributions)+1)
		for source, constraint := range contributions {
			copy[source] = constraint
		}
		output[id] = copy
	}
	return output
}

func validateSelectedConstraints(selected map[string]catalogProvider, constraints map[string]map[string]string) error {
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := selected[id]
		contributions, constrained := constraints[id]
		if !constrained {
			continue
		}
		ranges := sortedConstraintValues(contributions)
		matches, err := model.VersionSatisfiesAll(entry.provider.Definition.Version, ranges)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("dependency %s version %s does not satisfy %s", id, entry.provider.Definition.Version, strings.Join(ranges, " and "))
		}
	}
	return nil
}

func sharedSourceReleaseConflict(selected map[string]catalogProvider, candidate catalogProvider) error {
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		other := selected[id]
		if other.provider.Source.Module != candidate.provider.Source.Module {
			continue
		}
		if sameSourceRelease(other, candidate) {
			continue
		}
		return fmt.Errorf(
			"plugins %s and %s select conflicting releases for source module %s: %s versus %s",
			other.provider.ID, candidate.provider.ID, candidate.provider.Source.Module,
			sourceReleaseDescription(other), sourceReleaseDescription(candidate),
		)
	}
	return nil
}

func sameSourceRelease(left, right catalogProvider) bool {
	if left.provider.Source != right.provider.Source || left.provider.ImportPath != right.provider.ImportPath {
		return false
	}
	if left.persisted != right.persisted {
		return false
	}
	if !left.persisted {
		return true
	}
	return left.releaseFingerprint != "" && left.releaseFingerprint == right.releaseFingerprint
}

func sourceReleaseDescription(entry catalogProvider) string {
	fingerprint := entry.releaseFingerprint
	if fingerprint == "" {
		fingerprint = "pending-release"
	}
	return fmt.Sprintf("%s %s %s %s", entry.provider.Source.Version, entry.provider.Source.Checksum, entry.provider.ImportPath, fingerprint)
}

type contractBindingChoice struct {
	consumer  string
	contract  string
	providers []string
}

// validateCombinedPluginGraph proves that the selected dependency closure has
// at least one exact, acyclic contract binding. Required contracts choose one
// selected provider, optional contracts may remain deliberately unbound, and
// many contracts bind every selected provider. These are the same edges the
// CLI records in the reviewed lockfile, so publish validation cannot accept a
// graph that every consumer would later reject.
func validateCombinedPluginGraph(selected map[string]catalogProvider) error {
	edges := make(map[string][]string, len(selected))
	providers := map[string][]string{}
	ids := make([]string, 0, len(selected))
	for id, entry := range selected {
		ids = append(ids, id)
		for _, dependency := range entry.provider.Definition.Requires {
			if _, ok := selected[dependency.ID]; ok {
				edges[id] = append(edges[id], dependency.ID)
			}
		}
		for _, provided := range entry.provider.Definition.Provides {
			key := fmt.Sprintf("%s@%d", provided.ID, provided.Major)
			providers[key] = append(providers[key], id)
		}
	}
	sort.Strings(ids)
	for key := range providers {
		sort.Strings(providers[key])
	}

	var choices []contractBindingChoice
	for _, consumerID := range ids {
		consumed := append([]model.ContractRequirement(nil), selected[consumerID].provider.Definition.Consumes...)
		sort.Slice(consumed, func(i, j int) bool {
			if consumed[i].ID != consumed[j].ID {
				return consumed[i].ID < consumed[j].ID
			}
			return consumed[i].Major < consumed[j].Major
		})
		for _, requirement := range consumed {
			key := fmt.Sprintf("%s@%d", requirement.ID, requirement.Major)
			matches := providers[key]
			switch requirement.Mode {
			case "required":
				if len(matches) == 0 {
					return fmt.Errorf("plugin %s required contract %s has no provider candidate in the selected dependency graph", consumerID, key)
				}
				choices = append(choices, contractBindingChoice{
					consumer: consumerID, contract: key, providers: append([]string(nil), matches...),
				})
			case "optional":
				// An explicit empty binding is always a valid safe choice.
			case "many":
				edges[consumerID] = append(edges[consumerID], matches...)
			}
		}
	}
	for id := range edges {
		sort.Strings(edges[id])
	}
	if err := detectPluginCycles(edges, ids); err != nil {
		return err
	}
	steps := 0
	if err := chooseAcyclicContractBindings(edges, ids, choices, 0, &steps); err != nil {
		return err
	}
	return nil
}

func chooseAcyclicContractBindings(edges map[string][]string, ids []string, choices []contractBindingChoice, index int, steps *int) error {
	if index == len(choices) {
		return nil
	}
	choice := choices[index]
	var lastErr error
	for _, provider := range choice.providers {
		*steps++
		if *steps > maxDefinitionSolverSteps {
			return fmt.Errorf("contract binding resolution exceeded %d candidate steps", maxDefinitionSolverSteps)
		}
		before := len(edges[choice.consumer])
		edges[choice.consumer] = append(edges[choice.consumer], provider)
		if err := detectPluginCycles(edges, ids); err == nil {
			if err := chooseAcyclicContractBindings(edges, ids, choices, index+1, steps); err == nil {
				return nil
			} else {
				lastErr = err
			}
		} else {
			lastErr = err
		}
		edges[choice.consumer] = edges[choice.consumer][:before]
	}
	if lastErr == nil {
		lastErr = errors.New("no provider candidate")
	}
	return fmt.Errorf("required contract %s for plugin %s has no acyclic exact binding: %w", choice.contract, choice.consumer, lastErr)
}

func detectPluginCycles(edges map[string][]string, ids []string) error {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string, []string) error
	visit = func(id string, path []string) error {
		if visiting[id] {
			return fmt.Errorf("dependency cycle: %s", strings.Join(append(path, id), " -> "))
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		dependencies := append([]string(nil), edges[id]...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if err := visit(dependency, append(path, id)); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for _, id := range ids {
		if err := visit(id, nil); err != nil {
			return err
		}
	}
	return nil
}
