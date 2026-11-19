package plugin

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/buildinfo"
)

// Compatibility reason codes returned by the 5-dimensional compatibility engine.
const (
	CompatOK                   = "compatible"
	CompatIncompatiblePlatform = "incompatible_platform"
	CompatIncompatibleSkinloom = "incompatible_skinloom"
	CompatIncompatibleAPI      = "incompatible_api"
	CompatIncompatibleProtocol = "incompatible_protocol"
	CompatIncompatibleGo       = "incompatible_go"
	CompatMissingDependency    = "missing_dependency"
	CompatConflictDependency   = "conflict_dependency"
	CompatCyclicDependency     = "cyclic_dependency"
	CompatYanked               = "yanked"
	CompatBlocked              = "blocked"
)

// Semver represents a parsed semantic version (major.minor.patch[-prerelease][+build]).
type Semver struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Build      string
}

// ParseSemver parses standard SemVer strings as well as Go toolchain strings like
// "go1.26.10", "go1.26", or partial versions like "1.26" (normalized to 1.26.0).
func ParseSemver(raw string) (Semver, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Semver{}, errors.New("empty version")
	}
	s = strings.TrimPrefix(s, "go")
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")

	var build string
	if core, b, ok := strings.Cut(s, "+"); ok {
		s = core
		build = b
	}
	var pre string
	if core, p, ok := strings.Cut(s, "-"); ok {
		s = core
		pre = p
	}
	// Handle Go release candidates like "1.26rc1" or "1.26beta1".
	if pre == "" {
		for _, tag := range []string{"rc", "beta", "alpha"} {
			if idx := strings.Index(s, tag); idx > 0 {
				pre = s[idx:]
				s = s[:idx]
				break
			}
		}
	}

	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return Semver{}, fmt.Errorf("invalid version %q", raw)
	}
	nums := [3]int{}
	for i, part := range parts {
		if part == "" {
			return Semver{}, fmt.Errorf("invalid version %q", raw)
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("invalid version component %q in %q", part, raw)
		}
		nums[i] = n
	}
	return Semver{
		Major:      nums[0],
		Minor:      nums[1],
		Patch:      nums[2],
		Prerelease: pre,
		Build:      build,
	}, nil
}

// String formats v in canonical major.minor.patch[-prerelease] form.
func (v Semver) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		base += "-" + v.Prerelease
	}
	return base
}

// Compare compares v and o according to SemVer 2.0 precedence rules (ignoring build metadata).
// Returns -1 if v < o, 0 if v == o, +1 if v > o.
func (v Semver) Compare(o Semver) int {
	if v.Major != o.Major {
		return cmpInt(v.Major, o.Major)
	}
	if v.Minor != o.Minor {
		return cmpInt(v.Minor, o.Minor)
	}
	if v.Patch != o.Patch {
		return cmpInt(v.Patch, o.Patch)
	}
	if v.Prerelease == o.Prerelease {
		return 0
	}
	if v.Prerelease == "" {
		return 1
	}
	if o.Prerelease == "" {
		return -1
	}
	aParts := strings.Split(v.Prerelease, ".")
	bParts := strings.Split(o.Prerelease, ".")
	n := min(len(aParts), len(bParts))
	for i := range n {
		if aParts[i] == bParts[i] {
			continue
		}
		ai, aErr := strconv.Atoi(aParts[i])
		bi, bErr := strconv.Atoi(bParts[i])
		switch {
		case aErr == nil && bErr == nil:
			return cmpInt(ai, bi)
		case aErr == nil:
			return -1 // numeric identifiers always have lower precedence than non-numeric
		case bErr == nil:
			return 1
		default:
			if aParts[i] < bParts[i] {
				return -1
			}
			return 1
		}
	}
	return cmpInt(len(aParts), len(bParts))
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

type semverComparator struct {
	op  string // "=", "!=", ">", ">=", "<", "<="
	ver Semver
}

func (c semverComparator) match(v Semver) bool {
	cmp := v.Compare(c.ver)
	switch c.op {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	default:
		return false
	}
}

// SemverRange is a parsed disjunction (||) of conjunctions (AND) of comparators.
type SemverRange struct {
	raw    string
	groups [][]semverComparator // empty group matches any version
}

// ParseSemverRange parses npm/Go-style SemVer constraints:
//   - "*", "", "x"
//   - ">=1.0.0 <2.0.0", ">=1.0.0, <2.0.0"
//   - "^1.2.3", "~1.2.3", "1.x", "1.2.*"
//   - "1.0.0 - 2.0.0"
//   - ">=1.0.0 <2.0.0 || >=3.0.0"
func ParseSemverRange(expr string) (*SemverRange, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" || trimmed == "*" || strings.EqualFold(trimmed, "x") {
		return &SemverRange{raw: expr, groups: [][]semverComparator{{}}}, nil
	}
	orParts := strings.Split(trimmed, "||")
	groups := make([][]semverComparator, 0, len(orParts))
	for _, rawGroup := range orParts {
		rawGroup = strings.TrimSpace(rawGroup)
		if rawGroup == "" {
			return nil, fmt.Errorf("empty constraint group in %q", expr)
		}
		comps, err := parseAndGroup(rawGroup)
		if err != nil {
			return nil, fmt.Errorf("invalid range %q: %w", expr, err)
		}
		groups = append(groups, comps)
	}
	return &SemverRange{raw: expr, groups: groups}, nil
}

func parseAndGroup(group string) ([]semverComparator, error) {
	// Support hyphen range "A - B" (spaces around '-' distinguish it from prereleases).
	if left, right, ok := strings.Cut(group, " - "); ok {
		lo, err := parsePartialComparatorLower(strings.TrimSpace(left))
		if err != nil {
			return nil, err
		}
		hi, err := parsePartialComparatorUpper(strings.TrimSpace(right))
		if err != nil {
			return nil, err
		}
		return []semverComparator{lo, hi}, nil
	}

	// Normalize commas to spaces and split tokens while keeping operator + space + ver together.
	normalized := strings.ReplaceAll(group, ",", " ")
	rawTokens := strings.Fields(normalized)
	tokens := make([]string, 0, len(rawTokens))
	for i := 0; i < len(rawTokens); i++ {
		tok := rawTokens[i]
		if isStandaloneOp(tok) && i+1 < len(rawTokens) {
			tokens = append(tokens, tok+rawTokens[i+1])
			i++
			continue
		}
		tokens = append(tokens, tok)
	}

	var comps []semverComparator
	for _, tok := range tokens {
		c, err := expandToken(tok)
		if err != nil {
			return nil, err
		}
		comps = append(comps, c...)
	}
	return comps, nil
}

func isStandaloneOp(tok string) bool {
	switch tok {
	case ">=", "<=", "!=", ">", "<", "=", "^", "~":
		return true
	default:
		return false
	}
}

func expandToken(tok string) ([]semverComparator, error) {
	tok = strings.TrimSpace(tok)
	if tok == "" || tok == "*" || strings.EqualFold(tok, "x") {
		return nil, nil
	}
	switch {
	case strings.HasPrefix(tok, "^"):
		return expandCaret(strings.TrimSpace(tok[1:]))
	case strings.HasPrefix(tok, "~"):
		return expandTilde(strings.TrimSpace(tok[1:]))
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(tok, op) {
			rest := strings.TrimSpace(tok[len(op):])
			if isWildcardSpec(rest) {
				if op == "=" {
					return expandWildcard(rest)
				}
				return nil, fmt.Errorf("wildcard not allowed with operator %q in %q", op, tok)
			}
			v, err := ParseSemver(rest)
			if err != nil {
				return nil, err
			}
			return []semverComparator{{op: op, ver: v}}, nil
		}
	}
	if isWildcardSpec(tok) {
		return expandWildcard(tok)
	}
	// Bare partial version like "1" or "1.2" behaves like x-range; exact "1.2.3" matches "="
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(tok, "v"), "go"), ".")
	if len(parts) < 3 && !strings.ContainsAny(tok, "-+") {
		return expandWildcard(tok)
	}
	v, err := ParseSemver(tok)
	if err != nil {
		return nil, err
	}
	return []semverComparator{{op: "=", ver: v}}, nil
}

func isWildcardSpec(s string) bool {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "go")
	for _, p := range strings.Split(s, ".") {
		if p == "*" || strings.EqualFold(p, "x") {
			return true
		}
	}
	return false
}

func parsePartialVersionParts(s string) (major, minor, patch int, count int, pre string, err error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "go")
	s = strings.TrimPrefix(s, "v")
	if core, _, ok := strings.Cut(s, "+"); ok {
		s = core
	}
	if core, p, ok := strings.Cut(s, "-"); ok {
		s = core
		pre = p
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return 0, 0, 0, 0, "", fmt.Errorf("invalid partial version %q", s)
	}
	vals := [3]int{}
	for _, p := range parts {
		if p == "*" || strings.EqualFold(p, "x") {
			break
		}
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 0 {
			return 0, 0, 0, 0, "", fmt.Errorf("invalid partial version %q", s)
		}
		vals[count] = n
		count++
	}
	return vals[0], vals[1], vals[2], count, pre, nil
}

func expandWildcard(spec string) ([]semverComparator, error) {
	maj, minV, pat, cnt, pre, err := parsePartialVersionParts(spec)
	if err != nil {
		return nil, err
	}
	switch cnt {
	case 0:
		return nil, nil
	case 1:
		return []semverComparator{
			{op: ">=", ver: Semver{Major: maj}},
			{op: "<", ver: Semver{Major: maj + 1}},
		}, nil
	case 2:
		return []semverComparator{
			{op: ">=", ver: Semver{Major: maj, Minor: minV}},
			{op: "<", ver: Semver{Major: maj, Minor: minV + 1}},
		}, nil
	default:
		return []semverComparator{{op: "=", ver: Semver{Major: maj, Minor: minV, Patch: pat, Prerelease: pre}}}, nil
	}
}

func expandCaret(spec string) ([]semverComparator, error) {
	maj, minV, pat, cnt, pre, err := parsePartialVersionParts(spec)
	if err != nil || cnt == 0 {
		return nil, fmt.Errorf("invalid caret range ^%s", spec)
	}
	lo := Semver{Major: maj, Minor: minV, Patch: pat, Prerelease: pre}
	var hi Semver
	switch {
	case maj > 0:
		hi = Semver{Major: maj + 1}
	case cnt == 1:
		hi = Semver{Major: 1}
	case minV > 0:
		hi = Semver{Major: 0, Minor: minV + 1}
	case cnt == 2:
		hi = Semver{Major: 0, Minor: 1}
	default:
		hi = Semver{Major: 0, Minor: 0, Patch: pat + 1}
	}
	return []semverComparator{
		{op: ">=", ver: lo},
		{op: "<", ver: hi},
	}, nil
}

func expandTilde(spec string) ([]semverComparator, error) {
	maj, minV, pat, cnt, pre, err := parsePartialVersionParts(spec)
	if err != nil || cnt == 0 {
		return nil, fmt.Errorf("invalid tilde range ~%s", spec)
	}
	lo := Semver{Major: maj, Minor: minV, Patch: pat, Prerelease: pre}
	var hi Semver
	if cnt == 1 {
		hi = Semver{Major: maj + 1}
	} else {
		hi = Semver{Major: maj, Minor: minV + 1}
	}
	return []semverComparator{
		{op: ">=", ver: lo},
		{op: "<", ver: hi},
	}, nil
}

func parsePartialComparatorLower(spec string) (semverComparator, error) {
	v, err := ParseSemver(spec)
	if err != nil {
		return semverComparator{}, err
	}
	return semverComparator{op: ">=", ver: v}, nil
}

func parsePartialComparatorUpper(spec string) (semverComparator, error) {
	maj, minV, pat, cnt, pre, err := parsePartialVersionParts(spec)
	if err != nil || cnt == 0 {
		return semverComparator{}, fmt.Errorf("invalid upper bound %q", spec)
	}
	switch cnt {
	case 1:
		return semverComparator{op: "<", ver: Semver{Major: maj + 1}}, nil
	case 2:
		return semverComparator{op: "<", ver: Semver{Major: maj, Minor: minV + 1}}, nil
	default:
		return semverComparator{op: "<=", ver: Semver{Major: maj, Minor: minV, Patch: pat, Prerelease: pre}}, nil
	}
}

// Match reports whether v satisfies the SemVer range.
// Following SemVer/npm range semantics, a candidate with a prerelease tag only matches
// a non-empty comparator group when at least one comparator in that group explicitly
// targets a prerelease with the same major.minor.patch tuple (preventing e.g. ^1.0.0
// from accidentally matching breaking 2.0.0-alpha.1 releases).
func (r *SemverRange) Match(v Semver) bool {
	if r == nil || len(r.groups) == 0 {
		return true
	}
	for _, group := range r.groups {
		ok := true
		for _, comp := range group {
			if !comp.match(v) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if v.Prerelease != "" && len(group) > 0 {
			preAllowed := false
			for _, comp := range group {
				if comp.ver.Prerelease != "" &&
					comp.ver.Major == v.Major &&
					comp.ver.Minor == v.Minor &&
					comp.ver.Patch == v.Patch {
					preAllowed = true
					break
				}
			}
			if !preAllowed {
				continue
			}
		}
		return true
	}
	return false
}

// MatchString parses raw as a Semver and evaluates Match.
func (r *SemverRange) MatchString(raw string) bool {
	v, err := ParseSemver(raw)
	if err != nil {
		return false
	}
	return r.Match(v)
}

// String returns the original expression.
func (r *SemverRange) String() string {
	if r == nil {
		return ""
	}
	return r.raw
}

// HostEnv captures the running SkinLoom node's 5-dimensional compatibility context.
type HostEnv struct {
	OS                    string            `json:"os"`
	Arch                  string            `json:"arch"`
	SkinloomVersion       string            `json:"skinloomVersion"`
	APIVersion            int               `json:"apiVersion"`
	NativeProtocolVersion int               `json:"nativeProtocolVersion"`
	GoVersion             string            `json:"goVersion"`
	Installed             map[string]string `json:"installed,omitempty"`
	AllowDevSkinloom      bool              `json:"-"`
}

// Platform returns "<os>/<arch>".
func (e HostEnv) Platform() string {
	return e.OS + "/" + e.Arch
}

// CurrentHostEnv snapshots the local process's environment and installed plugins.
func CurrentHostEnv(installed map[string]string) HostEnv {
	inst := make(map[string]string, len(installed))
	for k, v := range installed {
		inst[k] = v
	}
	return HostEnv{
		OS:                    runtime.GOOS,
		Arch:                  runtime.GOARCH,
		SkinloomVersion:       buildinfo.Version,
		APIVersion:            CurrentAPIVersion,
		NativeProtocolVersion: CurrentNativeProtocolVersion,
		GoVersion:             runtime.Version(),
		Installed:             inst,
		AllowDevSkinloom:      strings.HasSuffix(buildinfo.Version, "-dev") || buildinfo.Version == "0.0.0-dev",
	}
}

// RegistryArtifact describes one downloadable platform .slp archive in index.v1.json.
type RegistryArtifact struct {
	URLs      []string `json:"urls"`
	SHA256    string   `json:"sha256"`
	Size      int64    `json:"size"`
	Signature string   `json:"signature,omitempty"`
}

// RegistryVersion describes one published release of a plugin/theme in index.v1.json.
type RegistryVersion struct {
	Version               string                      `json:"version"`
	APIVersion            int                         `json:"apiVersion"`
	NativeProtocolVersion int                         `json:"nativeProtocolVersion,omitempty"`
	Engines               EnginesSpec                 `json:"engines,omitempty"`
	Permissions           []string                    `json:"permissions,omitempty"`
	Dependencies          map[string]string           `json:"dependencies,omitempty"`
	OptionalDependencies  map[string]string           `json:"optionalDependencies,omitempty"`
	PublishedAt           string                      `json:"publishedAt,omitempty"`
	Yanked                bool                        `json:"yanked,omitempty"`
	Changelog             string                      `json:"changelog,omitempty"`
	Artifacts             map[string]RegistryArtifact `json:"artifacts"`
}

// RegistryPackage describes one plugin or theme entry in index.v1.json.
type RegistryPackage struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Runtime     string            `json:"runtime,omitempty"` // "js" (default) | "native"
	Category    string            `json:"category,omitempty"`
	Official    bool              `json:"official,omitempty"`
	Name        LocalizedText     `json:"name"`
	Description LocalizedText     `json:"description,omitempty"`
	Authors     []string          `json:"authors,omitempty"`
	License     string            `json:"license,omitempty"`
	Homepage    string            `json:"homepage,omitempty"`
	Repository  string            `json:"repository,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	Latest      string            `json:"latest,omitempty"`
	Versions    []RegistryVersion `json:"versions"`
}

// EffectiveRuntime returns "native" or "js" (default).
func (p RegistryPackage) EffectiveRuntime() string {
	if p.Runtime == RuntimeNative {
		return RuntimeNative
	}
	return RuntimeJS
}

// RegistryBlocked describes a malicious/revoked package entry in index.v1.json.
type RegistryBlocked struct {
	ID       string `json:"id"`
	Versions string `json:"versions,omitempty"` // SemVer range or "*" / empty for all versions
	Reason   string `json:"reason"`
}

// MatchesVersion reports whether this block rule applies to version.
func (b RegistryBlocked) MatchesVersion(version string) bool {
	if strings.TrimSpace(b.Versions) == "" || strings.TrimSpace(b.Versions) == "*" {
		return true
	}
	rng, err := ParseSemverRange(b.Versions)
	if err != nil {
		return true // fail closed if block expression is malformed
	}
	return rng.MatchString(version)
}

// CompatIssue describes one failed dimension check.
type CompatIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CompatReport summarizes compatibility of a single manifest or version.
type CompatReport struct {
	Compatible bool          `json:"compatible"`
	Status     string        `json:"status"` // CompatOK or primary failure code
	Issues     []CompatIssue `json:"issues,omitempty"`
}

// VersionCompatResult extends CompatReport with resolved platform artifact and dependency plan.
type VersionCompatResult struct {
	Version        string            `json:"version"`
	Compatible     bool              `json:"compatible"`
	Status         string            `json:"status"`
	Issues         []CompatIssue     `json:"issues,omitempty"`
	ArtifactKey    string            `json:"artifactKey,omitempty"`
	Artifact       *RegistryArtifact `json:"artifact,omitempty"`
	DependencyPlan *DependencyPlan   `json:"dependencyPlan,omitempty"`
}

// PackageCompatSummary captures the best installable version and overall compatibility for a package.
type PackageCompatSummary struct {
	Compatible         bool                 `json:"compatible"`
	Status             string               `json:"status"`
	RecommendedVersion string               `json:"recommendedVersion,omitempty"`
	Recommended        *VersionCompatResult `json:"recommended,omitempty"`
	InstalledVersion   string               `json:"installedVersion,omitempty"`
	UpgradeAvailable   bool                 `json:"upgradeAvailable,omitempty"`
	SupportedPlatforms []string             `json:"supportedPlatforms,omitempty"`
	Issues             []CompatIssue        `json:"issues,omitempty"`
}

// DependencyStep is one plugin version to install in topological order.
type DependencyStep struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	ArtifactKey string            `json:"artifactKey,omitempty"`
	Artifact    *RegistryArtifact `json:"artifact,omitempty"`
}

// DependencyPlan holds the topologically ordered cascade-install steps required
// before activating a target plugin.
type DependencyPlan struct {
	// Install lists dependencies that are not yet installed (or need upgrade) in
	// dependency-first order, excluding the root target itself.
	Install []DependencyStep `json:"install,omitempty"`
}

// CheckManifestCompat evaluates a local/uploaded plugin.json manifest against env
// across all 5 dimensions (platform, SkinLoom version, API/protocol version, Go baseline, installed deps).
func CheckManifestCompat(mf *Manifest, env HostEnv) CompatReport {
	if mf == nil {
		return CompatReport{
			Compatible: false,
			Status:     CompatIncompatibleAPI,
			Issues:     []CompatIssue{{Code: CompatIncompatibleAPI, Message: "nil manifest"}},
		}
	}
	var issues []CompatIssue
	rt := mf.EffectiveRuntime()

	// 1. Platform check
	if rt == RuntimeNative && mf.Native != nil && len(mf.Native.Platforms) > 0 {
		target := env.Platform()
		matched := false
		for _, p := range mf.Native.Platforms {
			if p == target {
				matched = true
				break
			}
		}
		if !matched {
			issues = append(issues, CompatIssue{
				Code:    CompatIncompatiblePlatform,
				Message: fmt.Sprintf("package supports %v, host is %s", mf.Native.Platforms, target),
			})
		}
	}

	// 2. SkinLoom version check
	if iss := checkSkinloomVersion(mf.Engines.Skinloom, env); iss != nil {
		issues = append(issues, *iss)
	}

	// 3. API & native protocol version check
	if env.APIVersion > 0 && mf.APIVersion != env.APIVersion {
		issues = append(issues, CompatIssue{
			Code:    CompatIncompatibleAPI,
			Message: fmt.Sprintf("plugin requires apiVersion %d, host supports %d", mf.APIVersion, env.APIVersion),
		})
	}
	if rt == RuntimeNative && mf.Native != nil && env.NativeProtocolVersion > 0 && mf.Native.ProtocolVersion != env.NativeProtocolVersion {
		issues = append(issues, CompatIssue{
			Code:    CompatIncompatibleProtocol,
			Message: fmt.Sprintf("plugin requires native protocolVersion %d, host supports %d", mf.Native.ProtocolVersion, env.NativeProtocolVersion),
		})
	}

	// 4. Go baseline check
	if iss := checkGoVersion(mf.Engines.Go, env); iss != nil {
		issues = append(issues, *iss)
	}

	// 5. Installed dependency satisfaction check
	depIDs := sortedMapKeys(mf.Dependencies)
	for _, depID := range depIDs {
		constraint := mf.Dependencies[depID]
		instVer, ok := env.Installed[depID]
		if !ok || instVer == "" {
			issues = append(issues, CompatIssue{
				Code:    CompatMissingDependency,
				Message: fmt.Sprintf("requires plugin %q (%s) which is not installed", depID, constraint),
			})
			continue
		}
		if constraint != "" {
			rng, err := ParseSemverRange(constraint)
			if err != nil || !rng.MatchString(instVer) {
				issues = append(issues, CompatIssue{
					Code:    CompatConflictDependency,
					Message: fmt.Sprintf("requires %q %s, but installed version is %s", depID, constraint, instVer),
				})
			}
		}
	}

	return buildCompatReport(issues)
}

// CheckVersionCompat runs all 5 compatibility dimensions for one registry release,
// including full transitive dependency tree resolution against installed plugins + catalog.
func CheckVersionCompat(
	pkg RegistryPackage,
	ver RegistryVersion,
	env HostEnv,
	catalog map[string]RegistryPackage,
) VersionCompatResult {
	return CheckVersionCompatWithBlocked(pkg, ver, env, catalog, nil)
}

// CheckVersionCompatWithBlocked runs all 5 compatibility dimensions for one registry release
// while enforcing registry blocklist rules on both the target package and transitive dependencies.
func CheckVersionCompatWithBlocked(
	pkg RegistryPackage,
	ver RegistryVersion,
	env HostEnv,
	catalog map[string]RegistryPackage,
	blocked map[string]RegistryBlocked,
) VersionCompatResult {
	res := VersionCompatResult{Version: ver.Version}
	var issues []CompatIssue

	if rule, hasBlock := blocked[pkg.ID]; hasBlock && rule.MatchesVersion(ver.Version) {
		issues = append(issues, CompatIssue{
			Code:    CompatBlocked,
			Message: fmt.Sprintf("blocked by registry: %s", rule.Reason),
		})
	}
	if ver.Yanked {
		issues = append(issues, CompatIssue{
			Code:    CompatYanked,
			Message: fmt.Sprintf("version %s has been yanked", ver.Version),
		})
	}

	// Dimension 1: Platform matching & artifact selection
	artKey, art, platOK := selectArtifact(pkg.EffectiveRuntime(), ver.Artifacts, env.Platform())
	if !platOK {
		issues = append(issues, CompatIssue{
			Code:    CompatIncompatiblePlatform,
			Message: fmt.Sprintf("no artifact available for %s", env.Platform()),
		})
	} else {
		res.ArtifactKey = artKey
		artCopy := art
		res.Artifact = &artCopy
	}

	// Dimension 2: SkinLoom SemVer compatibility
	if iss := checkSkinloomVersion(ver.Engines.Skinloom, env); iss != nil {
		issues = append(issues, *iss)
	}

	// Dimension 3: API & native protocol version
	if env.APIVersion > 0 && ver.APIVersion != env.APIVersion {
		issues = append(issues, CompatIssue{
			Code:    CompatIncompatibleAPI,
			Message: fmt.Sprintf("requires apiVersion %d, host supports %d", ver.APIVersion, env.APIVersion),
		})
	}
	if pkg.EffectiveRuntime() == RuntimeNative {
		protoVer := ver.NativeProtocolVersion
		if protoVer == 0 {
			protoVer = 1
		}
		if env.NativeProtocolVersion > 0 && protoVer != env.NativeProtocolVersion {
			issues = append(issues, CompatIssue{
				Code:    CompatIncompatibleProtocol,
				Message: fmt.Sprintf("requires nativeProtocolVersion %d, host supports %d", protoVer, env.NativeProtocolVersion),
			})
		}
	}

	// Dimension 4: Go baseline version
	if iss := checkGoVersion(ver.Engines.Go, env); iss != nil {
		issues = append(issues, *iss)
	}

	// Dimension 5: Dependency tree resolution (installed + catalog cascade + conflict/cycle/blocklist detection)
	if len(ver.Dependencies) > 0 {
		plan, depIssue := resolveDepsForVersion(pkg.ID, ver, env, catalog, blocked)
		if depIssue != nil {
			issues = append(issues, *depIssue)
		} else {
			res.DependencyPlan = plan
		}
	}

	rep := buildCompatReport(issues)
	res.Compatible = rep.Compatible
	res.Status = rep.Status
	res.Issues = rep.Issues
	return res
}

// SelectBestVersion evaluates all releases of pkg (highest SemVer first) and returns
// the best compatible version, upgrade availability, and platform summary.
func SelectBestVersion(
	pkg RegistryPackage,
	env HostEnv,
	catalog map[string]RegistryPackage,
	blocked map[string]RegistryBlocked,
) PackageCompatSummary {
	summary := PackageCompatSummary{
		InstalledVersion: env.Installed[pkg.ID],
	}

	// Collect supported platforms across latest or all non-yanked versions.
	platSet := map[string]bool{}
	for _, v := range pkg.Versions {
		if v.Yanked {
			continue
		}
		for k := range v.Artifacts {
			platSet[k] = true
		}
	}
	summary.SupportedPlatforms = sortedBoolKeys(platSet)

	if len(pkg.Versions) == 0 {
		summary.Status = CompatIncompatiblePlatform
		summary.Issues = []CompatIssue{{Code: CompatIncompatiblePlatform, Message: "package has no published versions"}}
		return summary
	}

	// Sort versions descending by SemVer (fallback to lexical).
	sortedVers := append([]RegistryVersion(nil), pkg.Versions...)
	sort.SliceStable(sortedVers, func(i, j int) bool {
		vi, errI := ParseSemver(sortedVers[i].Version)
		vj, errJ := ParseSemver(sortedVers[j].Version)
		if errI == nil && errJ == nil {
			return vi.Compare(vj) > 0
		}
		return sortedVers[i].Version > sortedVers[j].Version
	})

	var fallbackRes *VersionCompatResult
	blockRule, hasBlock := blocked[pkg.ID]

	for _, ver := range sortedVers {
		if hasBlock && blockRule.MatchesVersion(ver.Version) {
			if fallbackRes == nil {
				fallbackRes = &VersionCompatResult{
					Version:    ver.Version,
					Compatible: false,
					Status:     CompatBlocked,
					Issues: []CompatIssue{{
						Code:    CompatBlocked,
						Message: fmt.Sprintf("blocked by registry: %s", blockRule.Reason),
					}},
				}
			}
			continue
		}
		res := CheckVersionCompatWithBlocked(pkg, ver, env, catalog, blocked)
		if res.Compatible {
			resCopy := res
			summary.Compatible = true
			summary.Status = CompatOK
			summary.RecommendedVersion = res.Version
			summary.Recommended = &resCopy
			if summary.InstalledVersion != "" {
				iv, iErr := ParseSemver(summary.InstalledVersion)
				rv, rErr := ParseSemver(res.Version)
				if iErr == nil && rErr == nil {
					summary.UpgradeAvailable = rv.Compare(iv) > 0
				} else {
					summary.UpgradeAvailable = res.Version != summary.InstalledVersion
				}
			}
			return summary
		}
		if fallbackRes == nil || (!ver.Yanked && fallbackRes.Status == CompatYanked) {
			resCopy := res
			fallbackRes = &resCopy
		}
	}

	if fallbackRes != nil {
		summary.Status = fallbackRes.Status
		summary.Issues = fallbackRes.Issues
	}
	return summary
}

// ResolveDependencyPlan resolves the full transitive dependency tree for targetID@targetVersion,
// returning an ordered install plan or an error describing missing, conflicting, or cyclic deps.
func ResolveDependencyPlan(
	targetID, targetVersion string,
	env HostEnv,
	catalog map[string]RegistryPackage,
) (*DependencyPlan, error) {
	return ResolveDependencyPlanWithBlocked(targetID, targetVersion, env, catalog, nil)
}

// ResolveDependencyPlanWithBlocked resolves the transitive dependency tree while enforcing
// registry blocklist rules on the target and all transitive dependencies.
func ResolveDependencyPlanWithBlocked(
	targetID, targetVersion string,
	env HostEnv,
	catalog map[string]RegistryPackage,
	blocked map[string]RegistryBlocked,
) (*DependencyPlan, error) {
	if rule, hasBlock := blocked[targetID]; hasBlock && rule.MatchesVersion(targetVersion) {
		return nil, fmt.Errorf("%s: package %q@%s is blocked by registry: %s", CompatBlocked, targetID, targetVersion, rule.Reason)
	}
	pkg, ok := catalog[targetID]
	if !ok {
		return nil, fmt.Errorf("%s: package %q not found in catalog", CompatMissingDependency, targetID)
	}
	var targetVer *RegistryVersion
	for i := range pkg.Versions {
		if pkg.Versions[i].Version == targetVersion {
			targetVer = &pkg.Versions[i]
			break
		}
	}
	if targetVer == nil {
		return nil, fmt.Errorf("%s: version %q not found for %q", CompatMissingDependency, targetVersion, targetID)
	}
	plan, issue := resolveDepsForVersion(targetID, *targetVer, env, catalog, blocked)
	if issue != nil {
		return nil, fmt.Errorf("%s: %s", issue.Code, issue.Message)
	}
	return plan, nil
}

// resolveDepsForVersion performs DFS dependency resolution with cycle, blocklist & diamond-conflict detection.
func resolveDepsForVersion(
	rootID string,
	rootVer RegistryVersion,
	env HostEnv,
	catalog map[string]RegistryPackage,
	blocked map[string]RegistryBlocked,
) (*DependencyPlan, *CompatIssue) {
	// chosen tracks selected version for each dependency across the graph (detects conflicts).
	chosen := map[string]string{rootID: rootVer.Version}
	constraints := map[string][]*SemverRange{}
	state := map[string]int{rootID: 1} // 0=unvisited, 1=visiting, 2=visited
	var installOrder []DependencyStep

	checkAllConstraints := func(id, ver string) *CompatIssue {
		for _, rng := range constraints[id] {
			if !rng.MatchString(ver) {
				return &CompatIssue{
					Code:    CompatConflictDependency,
					Message: fmt.Sprintf("dependency %q version %s conflicts with required range %q", id, ver, rng.String()),
				}
			}
		}
		return nil
	}

	var visit func(parentID string, deps map[string]string) *CompatIssue
	visit = func(parentID string, deps map[string]string) *CompatIssue {
		for _, depID := range sortedMapKeys(deps) {
			rawRange := deps[depID]
			rng, err := ParseSemverRange(rawRange)
			if err != nil {
				return &CompatIssue{
					Code:    CompatConflictDependency,
					Message: fmt.Sprintf("invalid dependency range %q for %q (required by %q)", rawRange, depID, parentID),
				}
			}
			constraints[depID] = append(constraints[depID], rng)

			if state[depID] == 1 {
				return &CompatIssue{
					Code:    CompatCyclicDependency,
					Message: fmt.Sprintf("circular dependency detected: %q -> %q", parentID, depID),
				}
			}
			if prevVer, already := chosen[depID]; already {
				if iss := checkAllConstraints(depID, prevVer); iss != nil {
					return iss
				}
				continue
			}

			// First check if already installed locally with a compatible (and non-blocked) version.
			if instVer, ok := env.Installed[depID]; ok && instVer != "" {
				instBlocked := false
				if rule, hasBlock := blocked[depID]; hasBlock && rule.MatchesVersion(instVer) {
					instBlocked = true
				}
				if rng.MatchString(instVer) && !instBlocked {
					chosen[depID] = instVer
					state[depID] = 2
					continue
				}
				// Installed version doesn't satisfy constraint (or is blocked): check if catalog offers an upgrade.
				if catalog == nil {
					if instBlocked {
						return &CompatIssue{
							Code:    CompatBlocked,
							Message: fmt.Sprintf("installed dependency %q@%s is blocked by registry", depID, instVer),
						}
					}
					return &CompatIssue{
						Code:    CompatConflictDependency,
						Message: fmt.Sprintf("installed %q@%s does not satisfy %q required by %q", depID, instVer, rawRange, parentID),
					}
				}
			}

			// Look up depID in catalog for cascade installation.
			depPkg, inCat := catalog[depID]
			if !inCat {
				if instVer := env.Installed[depID]; instVer != "" {
					return &CompatIssue{
						Code:    CompatConflictDependency,
						Message: fmt.Sprintf("installed %q@%s does not satisfy %q and is unavailable in market", depID, instVer, rawRange),
					}
				}
				return &CompatIssue{
					Code:    CompatMissingDependency,
					Message: fmt.Sprintf("required dependency %q (%s) is not installed and not found in registry", depID, rawRange),
				}
			}

			// Pick highest non-yanked, non-blocked catalog version satisfying all constraints and host env (dims 1-4).
			cand, artKey, art, selErr := pickCompatibleDepVersion(depPkg, constraints[depID], env, blocked)
			if selErr != nil {
				return selErr
			}

			chosen[depID] = cand.Version
			state[depID] = 1
			if iss := visit(depID, cand.Dependencies); iss != nil {
				return iss
			}
			state[depID] = 2

			artCopy := art
			installOrder = append(installOrder, DependencyStep{
				ID:          depID,
				Version:     cand.Version,
				ArtifactKey: artKey,
				Artifact:    &artCopy,
			})
		}
		return nil
	}

	if iss := visit(rootID, rootVer.Dependencies); iss != nil {
		return nil, iss
	}
	return &DependencyPlan{Install: installOrder}, nil
}

func pickCompatibleDepVersion(
	pkg RegistryPackage,
	ranges []*SemverRange,
	env HostEnv,
	blocked map[string]RegistryBlocked,
) (RegistryVersion, string, RegistryArtifact, *CompatIssue) {
	sortedVers := append([]RegistryVersion(nil), pkg.Versions...)
	sort.SliceStable(sortedVers, func(i, j int) bool {
		vi, errI := ParseSemver(sortedVers[i].Version)
		vj, errJ := ParseSemver(sortedVers[j].Version)
		if errI == nil && errJ == nil {
			return vi.Compare(vj) > 0
		}
		return sortedVers[i].Version > sortedVers[j].Version
	})

	anyRangeMatched := false
	anyBlocked := false
	blockRule, hasBlock := blocked[pkg.ID]
	for _, ver := range sortedVers {
		if ver.Yanked {
			continue
		}
		matchesAll := true
		for _, r := range ranges {
			if !r.MatchString(ver.Version) {
				matchesAll = false
				break
			}
		}
		if !matchesAll {
			continue
		}
		if hasBlock && blockRule.MatchesVersion(ver.Version) {
			anyBlocked = true
			continue
		}
		anyRangeMatched = true

		artKey, art, platOK := selectArtifact(pkg.EffectiveRuntime(), ver.Artifacts, env.Platform())
		if !platOK {
			continue
		}
		if checkSkinloomVersion(ver.Engines.Skinloom, env) != nil {
			continue
		}
		if env.APIVersion > 0 && ver.APIVersion != env.APIVersion {
			continue
		}
		if pkg.EffectiveRuntime() == RuntimeNative {
			protoVer := ver.NativeProtocolVersion
			if protoVer == 0 {
				protoVer = 1
			}
			if env.NativeProtocolVersion > 0 && protoVer != env.NativeProtocolVersion {
				continue
			}
		}
		if checkGoVersion(ver.Engines.Go, env) != nil {
			continue
		}
		return ver, artKey, art, nil
	}

	if anyRangeMatched {
		return RegistryVersion{}, "", RegistryArtifact{}, &CompatIssue{
			Code:    CompatIncompatiblePlatform,
			Message: fmt.Sprintf("dependency %q has matching versions, but none are compatible with host environment %s", pkg.ID, env.Platform()),
		}
	}
	if anyBlocked {
		return RegistryVersion{}, "", RegistryArtifact{}, &CompatIssue{
			Code:    CompatBlocked,
			Message: fmt.Sprintf("all matching versions of dependency %q are blocked by registry: %s", pkg.ID, blockRule.Reason),
		}
	}
	return RegistryVersion{}, "", RegistryArtifact{}, &CompatIssue{
		Code:    CompatConflictDependency,
		Message: fmt.Sprintf("no version of dependency %q satisfies all constraints", pkg.ID),
	}
}

func selectArtifact(rt string, artifacts map[string]RegistryArtifact, platform string) (string, RegistryArtifact, bool) {
	if len(artifacts) == 0 {
		return "", RegistryArtifact{}, false
	}
	if rt == RuntimeJS {
		if a, ok := artifacts["any"]; ok {
			return "any", a, true
		}
		if a, ok := artifacts[platform]; ok {
			return platform, a, true
		}
		return "", RegistryArtifact{}, false
	}
	// Native plugin: exact platform first, then fat package "any" fallback if present.
	if a, ok := artifacts[platform]; ok {
		return platform, a, true
	}
	if a, ok := artifacts["any"]; ok {
		return "any", a, true
	}
	return "", RegistryArtifact{}, false
}

func checkSkinloomVersion(constraint string, env HostEnv) *CompatIssue {
	if strings.TrimSpace(constraint) == "" || strings.TrimSpace(env.SkinloomVersion) == "" {
		return nil
	}
	rng, err := ParseSemverRange(constraint)
	if err != nil {
		return &CompatIssue{
			Code:    CompatIncompatibleSkinloom,
			Message: fmt.Sprintf("invalid engines.skinloom constraint %q: %v", constraint, err),
		}
	}
	if env.AllowDevSkinloom && (strings.HasSuffix(env.SkinloomVersion, "-dev") || env.SkinloomVersion == "dev") {
		return nil
	}
	if !rng.MatchString(env.SkinloomVersion) {
		return &CompatIssue{
			Code:    CompatIncompatibleSkinloom,
			Message: fmt.Sprintf("requires SkinLoom %s, running %s", constraint, env.SkinloomVersion),
		}
	}
	return nil
}

func checkGoVersion(constraint string, env HostEnv) *CompatIssue {
	if strings.TrimSpace(constraint) == "" || strings.TrimSpace(env.GoVersion) == "" {
		return nil
	}
	rng, err := ParseSemverRange(constraint)
	if err != nil {
		return &CompatIssue{
			Code:    CompatIncompatibleGo,
			Message: fmt.Sprintf("invalid engines.go constraint %q: %v", constraint, err),
		}
	}
	if !rng.MatchString(env.GoVersion) {
		return &CompatIssue{
			Code:    CompatIncompatibleGo,
			Message: fmt.Sprintf("requires Go %s, host built with %s", constraint, env.GoVersion),
		}
	}
	return nil
}

func buildCompatReport(issues []CompatIssue) CompatReport {
	if len(issues) == 0 {
		return CompatReport{Compatible: true, Status: CompatOK}
	}
	return CompatReport{
		Compatible: false,
		Status:     issues[0].Code,
		Issues:     issues,
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
