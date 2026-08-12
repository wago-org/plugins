package model

import (
	"fmt"
	"strconv"
	"strings"
)

type semVersion struct {
	major, minor, patch uint64
	pre                 []string
}

func ValidateVersion(value string) error {
	if strings.HasPrefix(value, "V") {
		return fmt.Errorf("%q is not a semantic version", value)
	}
	_, err := parseSemVersion(value)
	return err
}

func ValidateVersionRange(value string) error {
	_, err := parseVersionRange(value)
	return err
}

func VersionSatisfiesAll(version string, ranges []string) (bool, error) {
	v, err := parseSemVersion(version)
	if err != nil {
		return false, err
	}
	for _, raw := range ranges {
		rangeSet, err := parseVersionRange(raw)
		if err != nil {
			return false, err
		}
		if !rangeSet.matches(v) {
			return false, nil
		}
	}
	return true, nil
}

func CompareVersions(left, right string) (int, error) {
	a, err := parseSemVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseSemVersion(right)
	if err != nil {
		return 0, err
	}
	return a.compare(b), nil
}

func parseSemVersion(value string) (semVersion, error) {
	original := value
	if strings.HasPrefix(value, "v") || strings.HasPrefix(value, "V") {
		value = value[1:]
	}
	if value == "" || strings.TrimSpace(value) != value {
		return semVersion{}, fmt.Errorf("%q is not a semantic version", original)
	}
	coreAndPre := strings.SplitN(value, "+", 2)
	if len(coreAndPre) == 2 && !validIdentifiers(coreAndPre[1], false) {
		return semVersion{}, fmt.Errorf("%q has invalid build metadata", original)
	}
	core := coreAndPre[0]
	var pre []string
	if split := strings.SplitN(core, "-", 2); len(split) == 2 {
		core = split[0]
		if !validIdentifiers(split[1], true) {
			return semVersion{}, fmt.Errorf("%q has an invalid prerelease", original)
		}
		pre = strings.Split(split[1], ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semVersion{}, fmt.Errorf("%q must contain major.minor.patch", original)
	}
	values := [3]uint64{}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semVersion{}, fmt.Errorf("%q has an invalid numeric component", original)
		}
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return semVersion{}, fmt.Errorf("%q has an invalid numeric component", original)
		}
		values[i] = parsed
	}
	return semVersion{major: values[0], minor: values[1], patch: values[2], pre: pre}, nil
}

func validIdentifiers(value string, prerelease bool) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" || (prerelease && numeric(part) && len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func numeric(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func (v semVersion) compare(other semVersion) int {
	for _, pair := range [][2]uint64{{v.major, other.major}, {v.minor, other.minor}, {v.patch, other.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(v.pre) == 0 && len(other.pre) == 0 {
		return 0
	}
	if len(v.pre) == 0 {
		return 1
	}
	if len(other.pre) == 0 {
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(other.pre); i++ {
		left, right := v.pre[i], other.pre[i]
		if left == right {
			continue
		}
		leftNumeric, rightNumeric := numeric(left), numeric(right)
		switch {
		case leftNumeric && rightNumeric:
			ln, _ := strconv.ParseUint(left, 10, 64)
			rn, _ := strconv.ParseUint(right, 10, 64)
			if ln < rn {
				return -1
			}
			return 1
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case left < right:
			return -1
		default:
			return 1
		}
	}
	if len(v.pre) < len(other.pre) {
		return -1
	}
	if len(v.pre) > len(other.pre) {
		return 1
	}
	return 0
}

type comparator struct {
	op      string
	version semVersion
}

// versionRange is a disjunction of AND-groups, matching Wago's core semver
// grammar and node-semver prerelease admission rule.
type versionRange [][]comparator

func parseVersionRange(raw string) (versionRange, error) {
	if len(raw) > 200 {
		return nil, fmt.Errorf("version range exceeds 200 characters")
	}
	var out versionRange
	for _, part := range strings.Split(raw, "||") {
		group, err := parseRange(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	return out, nil
}

func (r versionRange) matches(version semVersion) bool {
	for _, group := range r {
		if groupMatches(group, version) {
			return true
		}
	}
	return false
}

func groupMatches(group []comparator, version semVersion) bool {
	for _, comparator := range group {
		cmp := version.compare(comparator.version)
		switch comparator.op {
		case "=":
			if cmp != 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case ">=":
			if cmp < 0 {
				return false
			}
		case "<":
			if cmp >= 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		}
	}
	if len(version.pre) != 0 {
		for _, comparator := range group {
			candidate := comparator.version
			if len(candidate.pre) != 0 && candidate.major == version.major && candidate.minor == version.minor && candidate.patch == version.patch {
				return true
			}
		}
		return false
	}
	return true
}

func parseRange(raw string) ([]comparator, error) {
	if raw == "" || raw == "*" || raw == "x" || raw == "X" {
		return anyRange(), nil
	}
	tokens := tokenizeRange(raw)
	if len(tokens) == 3 && tokens[1] == "-" {
		return hyphenRange(tokens[0], tokens[2])
	}
	var out []comparator
	for _, token := range tokens {
		if token == "" || token == "-" {
			return nil, fmt.Errorf("semver: malformed range %q", raw)
		}
		expanded, err := expandRangeToken(token)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
	}
	return out, nil
}

func tokenizeRange(raw string) []string {
	var tokens []string
	for i := 0; i < len(raw); {
		for i < len(raw) && raw[i] == ' ' {
			i++
		}
		if i >= len(raw) {
			break
		}
		if raw[i] == '-' {
			tokens = append(tokens, "-")
			i++
			continue
		}
		start := i
		for i < len(raw) && strings.ContainsRune("<>=~^", rune(raw[i])) {
			i++
		}
		op := raw[start:i]
		for i < len(raw) && raw[i] == ' ' {
			i++
		}
		start = i
		for i < len(raw) && raw[i] != ' ' {
			i++
		}
		tokens = append(tokens, op+raw[start:i])
	}
	return tokens
}

func expandRangeToken(token string) ([]comparator, error) {
	op := ""
	for _, candidate := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(token, candidate) {
			op, token = candidate, strings.TrimPrefix(token, candidate)
			break
		}
	}
	if op != "" && token == "" {
		return nil, fmt.Errorf("semver: operator %q without a version", op)
	}
	partial, err := parsePartial(token)
	if err != nil {
		return nil, err
	}
	switch op {
	case "^":
		return caretRange(partial), nil
	case "~":
		return tildeRange(partial), nil
	case ">", ">=", "<", "<=":
		return comparatorRange(op, partial), nil
	default:
		return equalRange(partial), nil
	}
}

type partialVersion struct {
	major, minor, patch uint64
	pre                 []string
	components          int
}

func (p partialVersion) version() semVersion {
	return semVersion{major: p.major, minor: p.minor, patch: p.patch, pre: p.pre}
}

func parsePartial(raw string) (partialVersion, error) {
	if raw == "" || raw == "*" || raw == "x" || raw == "X" {
		return partialVersion{}, nil
	}
	main := raw
	if index := strings.IndexByte(main, '+'); index >= 0 {
		if !validIdentifiers(main[index+1:], false) {
			return partialVersion{}, fmt.Errorf("semver: invalid build metadata in %q", raw)
		}
		main = main[:index]
	}
	var pre []string
	if index := strings.IndexByte(main, '-'); index >= 0 {
		if !validIdentifiers(main[index+1:], true) {
			return partialVersion{}, fmt.Errorf("semver: invalid prerelease in %q", raw)
		}
		pre = strings.Split(main[index+1:], ".")
		main = main[:index]
	}
	parts := strings.Split(main, ".")
	if len(parts) > 3 {
		return partialVersion{}, fmt.Errorf("semver: too many components in %q", raw)
	}
	var out partialVersion
	for index, part := range parts {
		if part == "" {
			return partialVersion{}, fmt.Errorf("semver: empty component in %q", raw)
		}
		if part == "*" || part == "x" || part == "X" {
			break
		}
		if len(part) > 1 && part[0] == '0' {
			return partialVersion{}, fmt.Errorf("semver: leading zero in %q", part)
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return partialVersion{}, fmt.Errorf("semver: invalid numeric identifier %q", part)
		}
		switch index {
		case 0:
			out.major = value
		case 1:
			out.minor = value
		case 2:
			out.patch = value
		}
		out.components = index + 1
	}
	if len(pre) != 0 && out.components != 3 {
		return partialVersion{}, fmt.Errorf("semver: prerelease requires a full version in %q", raw)
	}
	out.pre = pre
	return out, nil
}

func anyRange() []comparator { return []comparator{{op: ">=", version: semVersion{}}} }

func equalRange(p partialVersion) []comparator {
	switch p.components {
	case 0:
		return anyRange()
	case 1:
		return []comparator{{">=", p.version()}, {"<", semVersion{major: p.major + 1}}}
	case 2:
		return []comparator{{">=", p.version()}, {"<", semVersion{major: p.major, minor: p.minor + 1}}}
	default:
		return []comparator{{"=", p.version()}}
	}
}

func caretRange(p partialVersion) []comparator {
	if p.components == 0 {
		return anyRange()
	}
	var upper semVersion
	switch {
	case p.major != 0:
		upper.major = p.major + 1
	case p.components == 1:
		upper.major = 1
	case p.minor != 0:
		upper.minor = p.minor + 1
	case p.components == 2:
		upper.minor = 1
	default:
		upper.patch = p.patch + 1
	}
	return []comparator{{">=", p.version()}, {"<", upper}}
}

func tildeRange(p partialVersion) []comparator {
	if p.components == 0 {
		return anyRange()
	}
	upper := semVersion{major: p.major + 1}
	if p.components >= 2 {
		upper = semVersion{major: p.major, minor: p.minor + 1}
	}
	return []comparator{{">=", p.version()}, {"<", upper}}
}

func comparatorRange(op string, p partialVersion) []comparator {
	switch op {
	case ">=":
		return []comparator{{">=", p.version()}}
	case "<":
		return []comparator{{"<", p.version()}}
	case ">":
		switch p.components {
		case 0:
			return []comparator{{"<", semVersion{}}}
		case 3:
			return []comparator{{">", p.version()}}
		case 2:
			return []comparator{{">=", semVersion{major: p.major, minor: p.minor + 1}}}
		default:
			return []comparator{{">=", semVersion{major: p.major + 1}}}
		}
	case "<=":
		switch p.components {
		case 0:
			return anyRange()
		case 3:
			return []comparator{{"<=", p.version()}}
		case 2:
			return []comparator{{"<", semVersion{major: p.major, minor: p.minor + 1}}}
		default:
			return []comparator{{"<", semVersion{major: p.major + 1}}}
		}
	}
	return nil
}

func hyphenRange(left, right string) ([]comparator, error) {
	lower, err := parsePartial(left)
	if err != nil {
		return nil, err
	}
	upper, err := parsePartial(right)
	if err != nil {
		return nil, err
	}
	out := []comparator{{">=", lower.version()}}
	switch upper.components {
	case 0:
	case 3:
		out = append(out, comparator{"<=", upper.version()})
	case 2:
		out = append(out, comparator{"<", semVersion{major: upper.major, minor: upper.minor + 1}})
	default:
		out = append(out, comparator{"<", semVersion{major: upper.major + 1}})
	}
	return out, nil
}
