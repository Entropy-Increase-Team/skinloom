package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing/fstest"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
)

const (
	// MaxPackageBytes caps a Tier 1 JS/theme .slp archive size (20 MiB).
	MaxPackageBytes = 20 * 1024 * 1024
	// MaxUnpackedPackageBytes caps total decompressed bytes extracted from a JS/theme .slp archive (Zip Bomb defense).
	MaxUnpackedPackageBytes = 20 * 1024 * 1024
	// MaxPackageFileBytes caps any individual non-binary file inside an .slp archive (10 MiB).
	MaxPackageFileBytes = 10 * 1024 * 1024
	// MaxServerBundleBytes caps server.js bundle size (5 MiB).
	MaxServerBundleBytes = 5 * 1024 * 1024
	// MaxPackageEntries caps the total number of archive entries.
	MaxPackageEntries = 4096

	// MaxNativeSinglePackageBytes caps a single-platform Tier 2 native .slp archive (50 MiB).
	MaxNativeSinglePackageBytes = 50 * 1024 * 1024
	// MaxNativeFatPackageBytes caps a multi-platform Tier 2 fat .slp archive (150 MiB).
	MaxNativeFatPackageBytes = 150 * 1024 * 1024
	// MaxNativeUnpackedSingleBytes caps decompressed bytes for a single-platform native .slp (100 MiB).
	MaxNativeUnpackedSingleBytes = 100 * 1024 * 1024
	// MaxNativeUnpackedFatBytes caps decompressed bytes for a multi-platform fat native .slp (256 MiB).
	MaxNativeUnpackedFatBytes = 256 * 1024 * 1024
	// MaxNativeBinaryBytes caps an individual compiled Go binary inside a native .slp (64 MiB).
	MaxNativeBinaryBytes = 64 * 1024 * 1024
	// maxManifestEntryBytes caps plugin.json during pre-inspection (1 MiB).
	maxManifestEntryBytes = 1 * 1024 * 1024
)

// OfficialPublicKeyPEM is the built-in Ed25519 SPKI public key used for official packages.
// Its corresponding private key is held offline outside this repository.
const OfficialPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAPD8bUrzAGLNF5McmIr+DEtQ9y+0q/9/FMMUSSiWRcVY=
-----END PUBLIC KEY-----`

var (
	// ErrBadPackage indicates a malformed, oversized, tampered, or unsafe .slp archive.
	ErrBadPackage = errors.New("invalid plugin package")
	// ErrUntrustedPackage indicates an unsigned or unknown-signer .slp archive without allowUnsigned.
	ErrUntrustedPackage = errors.New("plugin package signature is missing or untrusted")
	// ErrUnsignedNativeForbidden indicates an unsigned or untrusted Tier 2 native binary
	// package was rejected by production security policy.
	ErrUnsignedNativeForbidden = errors.New("unsigned or untrusted native plugin is forbidden in production mode")

	checksumLineRe = regexp.MustCompile(`^([0-9a-f]{64}) {2}(.+)$`)
)

// TrustedKey describes an Ed25519 public key trusted to verify .slp packages.
type TrustedKey struct {
	Fingerprint string            `json:"fingerprint"`
	Label       string            `json:"label"`
	Official    bool              `json:"official,omitempty"`
	PublicKey   ed25519.PublicKey `json:"-"`
}

// PackageVerification summarizes integrity and Ed25519 signature verification of an .slp package.
type PackageVerification struct {
	HasChecksums bool     `json:"hasChecksums"`
	Signed       bool     `json:"signed"`
	Verified     bool     `json:"verified"`
	Trusted      bool     `json:"trusted"`
	Official     bool     `json:"official,omitempty"`
	Signer       string   `json:"signer,omitempty"`
	KeyID        string   `json:"keyId,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

// PermissionItem describes one permission requested by a plugin package.
type PermissionItem struct {
	Name        string   `json:"name"`
	Tier        RiskTier `json:"tier"`
	Description string   `json:"description"`
	Granted     bool     `json:"granted,omitempty"`
	Added       bool     `json:"added,omitempty"`
}

// PermissionGroups partitions a plugin's requested permissions by risk tier.
type PermissionGroups struct {
	Normal    []PermissionItem `json:"normal"`
	Sensitive []PermissionItem `json:"sensitive"`
	HighRisk  []PermissionItem `json:"high_risk"`
}

// PackageInspectResult contains pre-install inspection metadata returned to administrators or the CLI.
type PackageInspectResult struct {
	ID                      string              `json:"id"`
	Kind                    string              `json:"kind"`
	Runtime                 string              `json:"runtime"`
	Version                 string              `json:"version"`
	Name                    string              `json:"name"`
	Description             string              `json:"description,omitempty"`
	Authors                 []string            `json:"authors,omitempty"`
	Homepage                string              `json:"homepage,omitempty"`
	IsUpgrade               bool                `json:"isUpgrade"`
	CurrentVersion          string              `json:"currentVersion,omitempty"`
	HasServer               bool                `json:"hasServer"`
	HasWeb                  bool                `json:"hasWeb"`
	RoutesCount             int                 `json:"routesCount"`
	HooksCount              int                 `json:"hooksCount"`
	Slots                   []string            `json:"slots,omitempty"`
	SettingsCount           int                 `json:"settingsCount"`
	Dependencies            map[string]string   `json:"dependencies,omitempty"`
	HostPlatform            string              `json:"hostPlatform"`
	SupportedPlatforms      []string            `json:"supportedPlatforms,omitempty"`
	PlatformCompatible      bool                `json:"platformCompatible"`
	AllowUnsignedNative     bool                `json:"allowUnsignedNative,omitempty"`
	NativeInstallBlocked    bool                `json:"nativeInstallBlocked,omitempty"`
	NativeBlockReason       string              `json:"nativeBlockReason,omitempty"`
	ArchiveSHA256           string              `json:"archiveSha256"`
	ArchiveBytes            int                 `json:"archiveBytes"`
	Verification            PackageVerification `json:"verification"`
	Permissions             PermissionGroups    `json:"permissions"`
	RequiresHighRiskConfirm bool                `json:"requiresHighRiskConfirm"`
	Warnings                []string            `json:"warnings,omitempty"`
}

// ParsedPackage holds verified, extracted in-memory files from an .slp archive.
type ParsedPackage struct {
	ArchiveSHA256      string
	ArchiveBytes       []byte
	Manifest           *Manifest
	Files              map[string][]byte // normalized regular files excluding checksums.txt and signature
	ServerJS           []byte
	Verification       PackageVerification
	Warnings           []string
	SupportedPlatforms []string // platforms declared and/or present in archive
	ContainedPlatforms []string // platforms whose executable is actually present in this archive
	NativeExecutables  []string // archive-relative paths of native binaries present
	HostCompatible     bool     // true for JS/themes, or when host GOOS/GOARCH executable is present
}

// ParseEd25519PublicKey decodes a PEM SPKI, base64 SPKI/raw, or 64-hex Ed25519 public key.
func ParseEd25519PublicKey(spec string) (ed25519.PublicKey, error) {
	return settings.ParseEd25519PublicKey(spec)
}

// PublicKeyFingerprint returns a short stable SHA-256 fingerprint for an Ed25519 public key.
func PublicKeyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "sha256:" + hex.EncodeToString(sum[:8])
}

// ResolveTrustedKeys returns the built-in official key plus any valid configured custom keys.
func ResolveTrustedKeys(extraSpecs []string) []TrustedKey {
	keys := make([]TrustedKey, 0, 1+len(extraSpecs))
	seen := map[string]bool{}
	add := func(spec, label string, official bool) {
		pub, err := ParseEd25519PublicKey(spec)
		if err != nil {
			return
		}
		fp := PublicKeyFingerprint(pub)
		if seen[fp] {
			return
		}
		seen[fp] = true
		keys = append(keys, TrustedKey{
			Fingerprint: fp,
			Label:       label,
			Official:    official,
			PublicKey:   pub,
		})
	}
	add(OfficialPublicKeyPEM, "SkinLoom Official", true)
	for i, spec := range extraSpecs {
		add(spec, fmt.Sprintf("Trusted Key #%d", i+1), false)
	}
	return keys
}

// ParsePackage safely unpacks, validates size/path bounds, checks SHA-256 checksums, and
// verifies Ed25519 signatures for an .slp archive against the current host OS/Arch.
func ParsePackage(pkgBytes []byte, extraTrustedKeys []string) (*ParsedPackage, error) {
	return ParsePackageForPlatform(pkgBytes, extraTrustedKeys, runtime.GOOS+"/"+runtime.GOARCH)
}

// ParsePackageForPlatform unpacks and validates an .slp archive while checking
// Tier 2 native binary presence against hostPlatform (e.g. "linux/amd64").
func ParsePackageForPlatform(pkgBytes []byte, extraTrustedKeys []string, hostPlatform string) (*ParsedPackage, error) {
	if len(pkgBytes) == 0 {
		return nil, fmt.Errorf("%w: package archive is empty", ErrBadPackage)
	}
	if len(pkgBytes) > MaxNativeFatPackageBytes {
		return nil, fmt.Errorf("%w: package size %d exceeds %d byte hard limit", ErrBadPackage, len(pkgBytes), MaxNativeFatPackageBytes)
	}

	sum := sha256.Sum256(pkgBytes)
	archiveSHA := hex.EncodeToString(sum[:])

	zr, err := zip.NewReader(bytes.NewReader(pkgBytes), int64(len(pkgBytes)))
	if err != nil {
		return nil, fmt.Errorf("%w: open zip: %v", ErrBadPackage, err)
	}
	if len(zr.File) > MaxPackageEntries {
		return nil, fmt.Errorf("%w: archive has %d entries (limit %d)", ErrBadPackage, len(zr.File), MaxPackageEntries)
	}

	// Pass 1: locate and parse plugin.json before unpacking all entries so we can
	// apply exact Tier 1 (JS/theme) vs Tier 2 (native single/fat) size limits.
	var manifestRaw []byte
	for _, f := range zr.File {
		if f.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if err := validateZipRawPath(f.Name); err != nil {
			return nil, err
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		if path.Clean(f.Name) == "plugin.json" {
			data, rErr := readLimitedZipEntry(f, maxManifestEntryBytes)
			if rErr != nil {
				return nil, rErr
			}
			manifestRaw = data
			break
		}
	}
	if manifestRaw == nil {
		return nil, fmt.Errorf("%w: missing plugin.json", ErrBadPackage)
	}
	mf, err := ParseManifest(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPackage, err)
	}

	isNative := mf.EffectiveRuntime() == RuntimeNative
	maxArchiveBytes := MaxPackageBytes
	maxUnpackedBytes := int64(MaxUnpackedPackageBytes)
	if isNative {
		isFat := isFatNativeArchive(mf, zr.File)
		if isFat {
			maxArchiveBytes = MaxNativeFatPackageBytes
			maxUnpackedBytes = MaxNativeUnpackedFatBytes
		} else {
			maxArchiveBytes = MaxNativeSinglePackageBytes
			maxUnpackedBytes = MaxNativeUnpackedSingleBytes
		}
	}
	if len(pkgBytes) > maxArchiveBytes {
		return nil, fmt.Errorf("%w: package size %d exceeds %d byte limit", ErrBadPackage, len(pkgBytes), maxArchiveBytes)
	}

	files := make(map[string][]byte, len(zr.File))
	lowerFiles := make(map[string]struct{}, len(zr.File))
	var checksumRaw []byte
	var signatureRaw []byte
	var totalUnpacked int64

	for _, f := range zr.File {
		if f.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symbolic link %q is forbidden", ErrBadPackage, f.Name)
		}
		if err := validateZipRawPath(f.Name); err != nil {
			return nil, err
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		rel := path.Clean(f.Name)
		lowerRel := strings.ToLower(rel)
		if _, dup := files[rel]; dup || (rel == "checksums.txt" && checksumRaw != nil) || (rel == "signature" && signatureRaw != nil) {
			return nil, fmt.Errorf("%w: duplicate archive entry %q", ErrBadPackage, rel)
		}
		if _, dupCase := lowerFiles[lowerRel]; dupCase {
			return nil, fmt.Errorf("%w: case-colliding archive entry %q", ErrBadPackage, rel)
		}
		lowerFiles[lowerRel] = struct{}{}

		fileLimit := int64(MaxPackageFileBytes)
		if isNative && isNativeExecutablePath(mf, rel) {
			fileLimit = int64(MaxNativeBinaryBytes)
		}
		if f.UncompressedSize64 > uint64(fileLimit) {
			return nil, fmt.Errorf("%w: file %q uncompressed size exceeds %d byte limit", ErrBadPackage, rel, fileLimit)
		}

		data, err := readLimitedZipEntry(f, fileLimit)
		if err != nil {
			return nil, err
		}
		totalUnpacked += int64(len(data))
		if totalUnpacked > maxUnpackedBytes {
			return nil, fmt.Errorf("%w: unpacked size exceeds %d byte cap", ErrBadPackage, maxUnpackedBytes)
		}

		switch rel {
		case "checksums.txt":
			checksumRaw = data
		case "signature":
			signatureRaw = data
		default:
			files[rel] = data
		}
	}

	var serverJS []byte
	if mf.Server != "" {
		serverKey := path.Clean(mf.Server)
		code, ok := files[serverKey]
		if !ok {
			return nil, fmt.Errorf("%w: declared server entry %q not found in archive", ErrBadPackage, mf.Server)
		}
		if len(code) > MaxServerBundleBytes {
			return nil, fmt.Errorf("%w: server bundle %q (%d bytes) exceeds %d byte limit", ErrBadPackage, mf.Server, len(code), MaxServerBundleBytes)
		}
		serverJS = code
	}
	if mf.Web != "" {
		webKey := path.Clean(mf.Web)
		if _, ok := files[webKey]; !ok {
			return nil, fmt.Errorf("%w: declared web entry %q not found in archive", ErrBadPackage, mf.Web)
		}
	}

	var supportedPlatforms, containedPlatforms, nativeExecs []string
	hostCompatible := true
	if isNative {
		var nErr error
		supportedPlatforms, containedPlatforms, nativeExecs, hostCompatible, nErr = inspectNativeArchiveExecutables(mf, files, hostPlatform)
		if nErr != nil {
			return nil, nErr
		}
	}

	if err := verifyChecksums(checksumRaw, files); err != nil {
		return nil, err
	}

	verification := PackageVerification{HasChecksums: true}
	if len(bytes.TrimSpace(signatureRaw)) == 0 {
		verification.Warnings = append(verification.Warnings,
			"Package is unsigned (missing Ed25519 signature). Only install unsigned packages from trusted local development builds.")
	} else {
		verification.Signed = true
		sigStr := strings.TrimSpace(string(signatureRaw))
		sigBytes, decErr := decodeBase64Flexible(sigStr)
		if decErr != nil || len(sigBytes) != ed25519.SignatureSize {
			return nil, fmt.Errorf("%w: malformed Ed25519 signature", ErrBadPackage)
		}
		keys := ResolveTrustedKeys(extraTrustedKeys)
		for _, k := range keys {
			if ed25519.Verify(k.PublicKey, checksumRaw, sigBytes) {
				verification.Verified = true
				verification.Trusted = true
				verification.Official = k.Official
				verification.Signer = k.Label
				verification.KeyID = k.Fingerprint
				break
			}
		}
		if !verification.Trusted {
			verification.Warnings = append(verification.Warnings,
				"Package signature did not match any trusted Ed25519 public key (unknown signer or modified signature).")
		}
	}

	return &ParsedPackage{
		ArchiveSHA256:      archiveSHA,
		ArchiveBytes:       append([]byte(nil), pkgBytes...),
		Manifest:           mf,
		Files:              files,
		ServerJS:           serverJS,
		Verification:       verification,
		Warnings:           append([]string(nil), verification.Warnings...),
		SupportedPlatforms: supportedPlatforms,
		ContainedPlatforms: containedPlatforms,
		NativeExecutables:  nativeExecs,
		HostCompatible:     hostCompatible,
	}, nil
}

func isFatNativeArchive(mf *Manifest, entries []*zip.File) bool {
	if mf == nil || mf.Native == nil {
		return false
	}
	if len(mf.Native.Platforms) > 1 {
		return true
	}
	execCount := 0
	for _, f := range entries {
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		if isNativeExecutablePath(mf, path.Clean(f.Name)) {
			execCount++
			if execCount > 1 {
				return true
			}
		}
	}
	return false
}

var nativePlatTokenRe = regexp.MustCompile(`^[a-z0-9]+$`)

func isNativeExecutablePath(mf *Manifest, rel string) bool {
	if mf == nil || mf.Native == nil || mf.Native.Executable == "" {
		return false
	}
	base := strings.TrimSuffix(path.Clean(mf.Native.Executable), ".exe")
	clean := path.Clean(rel)
	if clean == base || clean == base+".exe" {
		return true
	}
	if path.Dir(clean) != path.Dir(base) {
		return false
	}
	for _, sep := range []string{"_", "-"} {
		prefix := base + sep
		if !strings.HasPrefix(clean, prefix) {
			continue
		}
		rest := clean[len(prefix):]
		hasExe := strings.HasSuffix(rest, ".exe")
		if hasExe {
			rest = strings.TrimSuffix(rest, ".exe")
		}
		goos, goarch, ok := strings.Cut(rest, sep)
		if !ok || !nativePlatTokenRe.MatchString(goos) || !nativePlatTokenRe.MatchString(goarch) {
			continue
		}
		if hasExe && goos != "windows" {
			continue
		}
		return true
	}
	return false
}

var knownNativePlatforms = []string{
	"linux/amd64",
	"linux/arm64",
	"windows/amd64",
	"windows/arm64",
	"darwin/arm64",
	"darwin/amd64",
}

func inspectNativeArchiveExecutables(
	mf *Manifest,
	files map[string][]byte,
	hostPlatform string,
) (supported []string, contained []string, execPaths []string, hostCompat bool, err error) {
	if mf == nil || mf.Native == nil || mf.Native.Executable == "" {
		return nil, nil, nil, false, fmt.Errorf("%w: native plugin missing native.executable", ErrBadPackage)
	}

	execSet := map[string]bool{}
	for rel := range files {
		if isNativeExecutablePath(mf, rel) {
			if len(files[rel]) == 0 {
				return nil, nil, nil, false, fmt.Errorf("%w: native executable %q is empty", ErrBadPackage, rel)
			}
			execSet[rel] = true
			execPaths = append(execPaths, rel)
		}
	}
	sort.Strings(execPaths)
	if len(execPaths) == 0 {
		return nil, nil, nil, false, fmt.Errorf("%w: declared native executable %q not found in archive", ErrBadPackage, mf.Native.Executable)
	}

	// Build set of candidate platforms to probe.
	probePlatforms := make([]string, 0, len(knownNativePlatforms)+len(mf.Native.Platforms)+1)
	probeSeen := map[string]bool{}
	addProbe := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !probeSeen[p] {
			probeSeen[p] = true
			probePlatforms = append(probePlatforms, p)
		}
	}
	for _, p := range mf.Native.Platforms {
		addProbe(p)
	}
	addProbe(hostPlatform)
	for _, p := range knownNativePlatforms {
		addProbe(p)
	}

	hasPlatformBinary := func(plat string) bool {
		goos, goarch, ok := strings.Cut(plat, "/")
		if !ok || goos == "" || goarch == "" {
			return false
		}
		for _, cand := range mf.NativeExecutableCandidates(goos, goarch) {
			if _, exists := files[path.Clean(cand)]; exists {
				return true
			}
		}
		return false
	}

	// Detect explicit platform-suffixed binaries first (e.g. bin/plug_linux_amd64).
	base := strings.TrimSuffix(path.Clean(mf.Native.Executable), ".exe")
	hasGenericBinary := execSet[base] || execSet[base+".exe"]

	containedSet := map[string]bool{}
	for _, plat := range probePlatforms {
		goos, goarch, ok := strings.Cut(plat, "/")
		if !ok {
			continue
		}
		suffixed := []string{
			fmt.Sprintf("%s_%s_%s", base, goos, goarch),
			fmt.Sprintf("%s-%s-%s", base, goos, goarch),
		}
		if goos == "windows" {
			suffixed = append(suffixed,
				fmt.Sprintf("%s_%s_%s.exe", base, goos, goarch),
				fmt.Sprintf("%s-%s-%s.exe", base, goos, goarch),
			)
		}
		for _, cand := range suffixed {
			if _, exists := files[path.Clean(cand)]; exists {
				containedSet[plat] = true
				break
			}
		}
	}
	// If only a generic binary (bin/plug or bin/plug.exe) is packaged alongside
	// a single declared platform in manifest.native.platforms, credit that platform.
	if len(containedSet) == 0 && hasGenericBinary {
		if len(mf.Native.Platforms) == 1 {
			if hasPlatformBinary(mf.Native.Platforms[0]) {
				containedSet[mf.Native.Platforms[0]] = true
			}
		} else if len(mf.Native.Platforms) == 0 && hasPlatformBinary(hostPlatform) {
			containedSet[hostPlatform] = true
		}
	}

	for plat := range containedSet {
		contained = append(contained, plat)
	}
	sort.Strings(contained)

	supportedSet := map[string]bool{}
	for _, p := range mf.Native.Platforms {
		supportedSet[p] = true
	}
	for _, p := range contained {
		supportedSet[p] = true
	}
	for p := range supportedSet {
		supported = append(supported, p)
	}
	sort.Strings(supported)

	// Host is compatible if its platform binary exists AND (if manifest.native.platforms is non-empty)
	// the host platform is declared as supported.
	if len(mf.Native.Platforms) > 0 {
		declared := false
		for _, p := range mf.Native.Platforms {
			if p == hostPlatform {
				declared = true
				break
			}
		}
		hostCompat = declared && hasPlatformBinary(hostPlatform)
	} else {
		hostCompat = hasPlatformBinary(hostPlatform)
	}

	return supported, contained, execPaths, hostCompat, nil
}

// ValidateTheme validates a KindTheme parsed package against theme.Check without touching disk.
func (p *ParsedPackage) ValidateTheme(base *theme.Registry) ([]string, error) {
	if p == nil || p.Manifest == nil || p.Manifest.Kind != KindTheme {
		return nil, nil
	}
	if theme.IsBuiltinID(p.Manifest.ID) || (base != nil && base.IsBuiltin(p.Manifest.ID)) {
		return nil, fmt.Errorf("%w: cannot override built-in theme %q", ErrBadPackage, p.Manifest.ID)
	}
	mfs := make(fstest.MapFS, len(p.Files))
	for name, data := range p.Files {
		mfs[name] = &fstest.MapFile{Data: data, Mode: 0o644}
	}
	_, warnings, err := theme.Check(mfs, base)
	if err != nil {
		return warnings, fmt.Errorf("%w: invalid theme bundle: %v", ErrBadPackage, err)
	}
	return warnings, nil
}

// ClassifyPermissions groups requested manifest permissions by risk tier and upgrade state.
func ClassifyPermissions(declared []string, prevDeclared []string, prevGranted []string, isUpgrade bool) (PermissionGroups, bool) {
	prevDeclSet := make(map[string]bool, len(prevDeclared))
	for _, p := range prevDeclared {
		prevDeclSet[p] = true
	}
	prevGrantSet := make(map[string]bool, len(prevGranted))
	for _, p := range prevGranted {
		prevGrantSet[p] = true
	}

	sorted := append([]string(nil), declared...)
	sort.Strings(sorted)

	groups := PermissionGroups{
		Normal:    []PermissionItem{},
		Sensitive: []PermissionItem{},
		HighRisk:  []PermissionItem{},
	}
	for _, perm := range sorted {
		def, ok := LookupPermission(perm)
		if !ok {
			def = PermissionDef{Name: perm, Tier: RiskHighRisk, Description: "Unknown capability permission."}
		}
		granted := prevGrantSet[perm]
		if prevGranted == nil && !isUpgrade {
			granted = def.Tier == RiskNormal
		}
		item := PermissionItem{
			Name:        def.Name,
			Tier:        def.Tier,
			Description: def.Description,
			Granted:     granted,
			Added:       isUpgrade && !prevDeclSet[perm],
		}
		switch def.Tier {
		case RiskNormal:
			groups.Normal = append(groups.Normal, item)
		case RiskSensitive:
			groups.Sensitive = append(groups.Sensitive, item)
		default:
			groups.HighRisk = append(groups.HighRisk, item)
		}
	}
	return groups, len(groups.HighRisk) > 0
}

// ExtractPackageFiles atomically writes a parsed package's regular files into destDir.
func ExtractPackageFiles(pkg *ParsedPackage, destDir string) error {
	if pkg == nil {
		return errors.New("nil package")
	}
	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(parent, ".extract-"+pkg.Manifest.ID+"-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := validateZipRawPath(name); err != nil {
			return err
		}
		target := filepath.Join(tmpDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if pkg.Manifest != nil && pkg.Manifest.EffectiveRuntime() == RuntimeNative && isNativeExecutablePath(pkg.Manifest, name) {
			mode = 0o755
		}
		if err := os.WriteFile(target, pkg.Files[name], mode); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".package.sha256"), []byte(pkg.ArchiveSHA256+"\n"), 0o644); err != nil {
		return err
	}

	var backupDir string
	if _, statErr := os.Stat(destDir); statErr == nil {
		candBackup := filepath.Join(parent, ".bak-"+filepath.Base(tmpDir))
		if err := os.Rename(destDir, candBackup); err != nil {
			return err
		}
		backupDir = candBackup
	}
	if err := os.Rename(tmpDir, destDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, destDir)
		}
		return err
	}
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	cleanup = false
	return nil
}

func validateZipRawPath(raw string) error {
	if raw == "" || strings.ContainsRune(raw, 0) || strings.ContainsRune(raw, '\\') || strings.HasPrefix(raw, "/") {
		return fmt.Errorf("%w: unsafe archive entry path %q", ErrBadPackage, raw)
	}
	trimmed := strings.TrimSuffix(raw, "/")
	if trimmed == "" {
		return fmt.Errorf("%w: unsafe archive entry path %q", ErrBadPackage, raw)
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: unsafe archive entry path %q", ErrBadPackage, raw)
		}
	}
	return nil
}

func readLimitedZipEntry(f *zip.File, maxBytes int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open entry %q: %v", ErrBadPackage, f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read entry %q: %v", ErrBadPackage, f.Name, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: file %q exceeds %d byte limit", ErrBadPackage, f.Name, maxBytes)
	}
	return data, nil
}

func verifyChecksums(checksumRaw []byte, files map[string][]byte) error {
	if checksumRaw == nil {
		return fmt.Errorf("%w: missing checksums.txt", ErrBadPackage)
	}
	expected := make(map[string]string, len(files))
	lines := strings.Split(string(checksumRaw), "\n")
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := checksumLineRe.FindStringSubmatch(line)
		if m == nil {
			return fmt.Errorf("%w: malformed line in checksums.txt: %q", ErrBadPackage, line)
		}
		rel := m[2]
		if err := validateZipRawPath(rel); err != nil {
			return err
		}
		clean := path.Clean(rel)
		if _, dup := expected[clean]; dup {
			return fmt.Errorf("%w: duplicate checksum entry %q", ErrBadPackage, clean)
		}
		expected[clean] = m[1]
	}

	for name, data := range files {
		wantHex, ok := expected[name]
		if !ok {
			return fmt.Errorf("%w: file %q missing from checksums.txt", ErrBadPackage, name)
		}
		sum := sha256.Sum256(data)
		gotHex := hex.EncodeToString(sum[:])
		if subtle.ConstantTimeCompare([]byte(gotHex), []byte(wantHex)) != 1 {
			return fmt.Errorf("%w: checksum mismatch for %q", ErrBadPackage, name)
		}
		delete(expected, name)
	}
	if len(expected) > 0 {
		for missing := range expected {
			return fmt.Errorf("%w: checksum entry %q is missing from archive", ErrBadPackage, missing)
		}
	}
	return nil
}

func decodeBase64Flexible(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("invalid base64")
}
