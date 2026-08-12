package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"

	"github.com/wago-org/registry-backend/internal/model"
)

// currentStoreSchemaVersion is independent of the public manifest schema. It
// versions persistent keys and records so an incompatible binary never guesses
// how to interpret production data.
const currentStoreSchemaVersion = 1

// quarantinedV0Package retains the exact old package JSON. V0 releases cannot
// participate in strict v1 resolution because they have no source checksum,
// provider catalog, definition digest, or release fingerprint.
type quarantinedV0Package struct {
	LegacyKey   string          `json:"legacyKey"`
	CanonicalID string          `json:"canonicalId"`
	Record      json.RawMessage `json:"record"`
}

type legacyMigration struct {
	PackageKeys []string
}

func (m legacyMigration) report(d doc) *MigrationReport {
	return &MigrationReport{
		FromVersion:         0,
		ToVersion:           currentStoreSchemaVersion,
		CanonicalPackages:   len(m.PackageKeys),
		QuarantinedPackages: len(d.QuarantinedV0Packages),
	}
}

// migrateLegacyDoc performs the complete in-memory v0-to-v1 projection. The
// caller persists the result atomically. No best-effort identity fallback is
// allowed: the legacy key, stored module, and HTTPS GitHub repository must all
// identify the same owner/repository before any data is rewritten.
func migrateLegacyDoc(d *doc, rawPackages map[string]json.RawMessage) (legacyMigration, error) {
	if d.StoreSchemaVersion != 0 {
		return legacyMigration{}, fmt.Errorf("source schema is %d, want 0", d.StoreSchemaVersion)
	}
	keys := make([]string, 0, len(d.Packages))
	for key := range d.Packages {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	packages := make(map[string]model.Package, len(d.Packages))
	remap := make(map[string]string, len(d.Packages)*3)
	canonicalSources := make(map[string]string, len(d.Packages))
	quarantine := cloneQuarantine(d.QuarantinedV0Packages)

	for _, legacyKey := range keys {
		p := d.Packages[legacyKey]
		canonical, err := canonicalGitHubPackageID(legacyKey, p)
		if err != nil {
			return legacyMigration{}, fmt.Errorf("package %q: %w", legacyKey, err)
		}
		folded := strings.ToLower(canonical)
		if previous, duplicate := canonicalSources[folded]; duplicate {
			return legacyMigration{}, fmt.Errorf("package keys %q and %q both identify %q", previous, legacyKey, canonical)
		}
		canonicalSources[folded] = legacyKey

		for _, identity := range []string{legacyKey, p.Short, p.Name} {
			if previous, exists := remap[identity]; exists && previous != canonical {
				return legacyMigration{}, fmt.Errorf("package identity %q maps to both %q and %q", identity, previous, canonical)
			}
			remap[identity] = canonical
		}

		raw := append(json.RawMessage(nil), rawPackages[legacyKey]...)
		if len(raw) == 0 {
			raw, err = json.Marshal(p)
			if err != nil {
				return legacyMigration{}, fmt.Errorf("encode quarantine for %q: %w", legacyKey, err)
			}
		}
		record := quarantinedV0Package{LegacyKey: legacyKey, CanonicalID: canonical, Record: raw}
		if existing, exists := quarantine[legacyKey]; exists && !sameQuarantinedPackage(existing, record) {
			return legacyMigration{}, fmt.Errorf("quarantine key %q already contains a different package", legacyKey)
		}
		quarantine[legacyKey] = record

		p.Short = canonical
		p.Name = canonical
		p.Versions = nil
		p.Dependencies = nil
		p.Subpackages = nil
		packages[canonical] = p
	}

	stars, err := remapStars(d.Stars, remap)
	if err != nil {
		return legacyMigration{}, err
	}
	installs, err := remapInstalls(d.Installs, remap)
	if err != nil {
		return legacyMigration{}, err
	}
	for id, review := range d.Reviews {
		review.PackageShort = remappedIdentity(review.PackageShort, remap)
		d.Reviews[id] = review
	}
	for id, comment := range d.Comments {
		comment.PackageShort = remappedIdentity(comment.PackageShort, remap)
		d.Comments[id] = comment
	}
	for id, report := range d.Reports {
		report.PackageShort = remappedIdentity(report.PackageShort, remap)
		d.Reports[id] = report
	}
	for id, notification := range d.Notifications {
		if canonical, ok := remap[notification.PackageShort]; ok {
			notification.PackageShort = canonical
			notification.PackageName = canonical
		}
		d.Notifications[id] = notification
	}

	d.Packages = packages
	d.Stars = stars
	d.Installs = installs
	d.QuarantinedV0Packages = quarantine
	d.StoreSchemaVersion = currentStoreSchemaVersion
	return legacyMigration{PackageKeys: keys}, nil
}

func canonicalGitHubPackageID(storageKey string, p model.Package) (string, error) {
	u, err := url.Parse(p.Repository)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") ||
		u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return "", errors.New("repository does not prove an exact HTTPS github.com owner/repository identity")
	}
	path := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("repository does not contain exactly one GitHub owner and repository")
	}
	canonical := "github.com/" + parts[0] + "/" + parts[1]
	if err := model.ValidatePluginID(canonical); err != nil {
		return "", fmt.Errorf("repository identity is not a canonical plugin module: %w", err)
	}
	legacy := parts[0] + "/" + parts[1]
	if storageKey != legacy && storageKey != canonical {
		return "", fmt.Errorf("storage key %q does not match repository %q", storageKey, p.Repository)
	}
	if p.Short != storageKey {
		return "", fmt.Errorf("stored short %q does not equal storage key", p.Short)
	}
	if p.Name != canonical {
		return "", fmt.Errorf("stored module %q does not match repository-derived module %q", p.Name, canonical)
	}
	return canonical, nil
}

func cloneQuarantine(input map[string]quarantinedV0Package) map[string]quarantinedV0Package {
	out := make(map[string]quarantinedV0Package, len(input))
	for key, record := range input {
		record.Record = append(json.RawMessage(nil), record.Record...)
		out[key] = record
	}
	return out
}

func sameQuarantinedPackage(a, b quarantinedV0Package) bool {
	return a.LegacyKey == b.LegacyKey && a.CanonicalID == b.CanonicalID && bytes.Equal(a.Record, b.Record)
}

func remappedIdentity(value string, remap map[string]string) string {
	if canonical, ok := remap[value]; ok {
		return canonical
	}
	return value
}

func remapStars(input map[string][]string, remap map[string]string) (map[string][]string, error) {
	keys := sortedMapKeys(input)
	sets := make(map[string]map[string]struct{}, len(input))
	for _, key := range keys {
		canonical := remappedIdentity(key, remap)
		if sets[canonical] == nil {
			sets[canonical] = map[string]struct{}{}
		}
		for _, userID := range input[key] {
			if userID == "" {
				return nil, fmt.Errorf("stars for %q contain an empty user id", key)
			}
			sets[canonical][userID] = struct{}{}
		}
	}
	out := make(map[string][]string, len(sets))
	for packageID, users := range sets {
		for userID := range users {
			out[packageID] = append(out[packageID], userID)
		}
		sort.Strings(out[packageID])
	}
	return out, nil
}

func remapInstalls(input map[string]map[string]int, remap map[string]string) (map[string]map[string]int, error) {
	out := make(map[string]map[string]int, len(input))
	for _, key := range sortedMapKeys(input) {
		canonical := remappedIdentity(key, remap)
		if out[canonical] == nil {
			out[canonical] = map[string]int{}
		}
		for date, count := range input[key] {
			if count < 0 {
				return nil, fmt.Errorf("install count for %q on %q is negative", key, date)
			}
			current := out[canonical][date]
			if count > math.MaxInt-current {
				return nil, fmt.Errorf("install count for %q on %q overflows int", canonical, date)
			}
			out[canonical][date] = current + count
		}
	}
	return out, nil
}

func sortedMapKeys[V any](input map[string]V) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
