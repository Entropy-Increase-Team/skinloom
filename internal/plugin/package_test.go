package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
)

func buildTestSLP(
	t *testing.T,
	files map[string][]byte,
	priv ed25519.PrivateKey,
	corruptChecksums bool,
	omitChecksums bool,
	rawEntries ...func(*zip.Writer) error,
) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)

	var checksums strings.Builder
	for _, name := range names {
		data := files[name]
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&checksums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}

	if !omitChecksums {
		csText := checksums.String()
		if corruptChecksums {
			csText = strings.Replace(csText, "0", "1", 1)
			if csText == checksums.String() {
				csText = strings.Replace(csText, "a", "b", 1)
			}
		}
		w, err := zw.Create("checksums.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(csText)); err != nil {
			t.Fatal(err)
		}
		if priv != nil {
			sig := ed25519.Sign(priv, []byte(csText))
			sw, err := zw.Create("signature")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sw.Write([]byte(base64.StdEncoding.EncodeToString(sig) + "\n")); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, fn := range rawEntries {
		if err := fn(zw); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testKeypairPEM(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return priv, string(pemBytes)
}

func minimalValidPluginFiles(serverBytes []byte) map[string][]byte {
	files := map[string][]byte{
		"plugin.json": []byte(`{
			"id": "demo-pkg",
			"kind": "plugin",
			"version": "1.2.0",
			"name": {"en": "Demo Package"},
			"apiVersion": 1,
			"server": "server.js",
			"permissions": ["storage.kv", "users.ban"]
		}`),
		"server.js": serverBytes,
	}
	return files
}

func TestParsePackage_SignedValid(t *testing.T) {
	priv, pubPEM := testKeypairPEM(t)
	slp := buildTestSLP(t, minimalValidPluginFiles([]byte(`export default { setup() {} };`)), priv, false, false)

	pkg, err := ParsePackage(slp, []string{pubPEM})
	if err != nil {
		t.Fatalf("ParsePackage: %v", err)
	}
	if pkg.Manifest.ID != "demo-pkg" || pkg.Manifest.Version != "1.2.0" {
		t.Fatalf("unexpected manifest: %+v", pkg.Manifest)
	}
	if !pkg.Verification.HasChecksums || !pkg.Verification.Signed || !pkg.Verification.Verified || !pkg.Verification.Trusted {
		t.Fatalf("expected verified & trusted signature, got %+v", pkg.Verification)
	}
	if len(pkg.ArchiveSHA256) != 64 {
		t.Fatalf("bad archive hash: %q", pkg.ArchiveSHA256)
	}
}

func TestParsePackage_UnsignedAndUnknownSigner(t *testing.T) {
	files := minimalValidPluginFiles([]byte(`export default { setup() {} };`))
	unsigned := buildTestSLP(t, files, nil, false, false)
	pkg, err := ParsePackage(unsigned, nil)
	if err != nil {
		t.Fatalf("ParsePackage unsigned should inspect cleanly: %v", err)
	}
	if pkg.Verification.Signed || pkg.Verification.Trusted || len(pkg.Verification.Warnings) == 0 {
		t.Fatalf("expected unsigned warning, got %+v", pkg.Verification)
	}

	unknownPriv, _ := testKeypairPEM(t)
	untrusted := buildTestSLP(t, files, unknownPriv, false, false)
	pkg2, err := ParsePackage(untrusted, nil)
	if err != nil {
		t.Fatalf("ParsePackage unknown key should inspect with warning: %v", err)
	}
	if !pkg2.Verification.Signed || pkg2.Verification.Trusted || pkg2.Verification.Verified {
		t.Fatalf("expected signed but untrusted, got %+v", pkg2.Verification)
	}
}

func TestParsePackage_TamperChecksumsAndBadEntries(t *testing.T) {
	priv, pubPEM := testKeypairPEM(t)
	files := minimalValidPluginFiles([]byte(`export default { setup() {} };`))

	tampered := buildTestSLP(t, files, priv, true, false)
	if _, err := ParsePackage(tampered, []string{pubPEM}); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage on tampered checksum, got %v", err)
	}

	noCS := buildTestSLP(t, files, nil, false, true)
	if _, err := ParsePackage(noCS, nil); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage when checksums.txt omitted, got %v", err)
	}

	zipSlip := buildTestSLP(t, files, nil, false, false, func(zw *zip.Writer) error {
		w, err := zw.Create("../escape.txt")
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("pwn"))
		return err
	})
	if _, err := ParsePackage(zipSlip, nil); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage on zip-slip path, got %v", err)
	}

	symlinkSLP := buildTestSLP(t, files, nil, false, false, func(zw *zip.Writer) error {
		fh := &zip.FileHeader{Name: "link.txt"}
		fh.SetMode(0o777 | 0o120000) // ModeSymlink
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("/etc/passwd"))
		return err
	})
	if _, err := ParsePackage(symlinkSLP, nil); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage on symlink entry, got %v", err)
	}

	oversizeServer := bytes.Repeat([]byte("a"), MaxServerBundleBytes+1)
	tooBigServer := buildTestSLP(t, minimalValidPluginFiles(oversizeServer), priv, false, false)
	if _, err := ParsePackage(tooBigServer, []string{pubPEM}); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage on oversized server.js, got %v", err)
	}
}

func TestClassifyPermissions_UpgradeDiff(t *testing.T) {
	groups, needsConfirm := ClassifyPermissions(
		[]string{"storage.kv", "http.fetch", "users.ban"},
		[]string{"storage.kv"},
		[]string{"storage.kv"},
		true,
	)
	if !needsConfirm {
		t.Fatal("expected high_risk confirmation required")
	}
	if len(groups.Normal) != 1 || groups.Normal[0].Added || !groups.Normal[0].Granted {
		t.Fatalf("unexpected normal group: %+v", groups.Normal)
	}
	if len(groups.Sensitive) != 1 || !groups.Sensitive[0].Added || groups.Sensitive[0].Granted {
		t.Fatalf("unexpected sensitive group: %+v", groups.Sensitive)
	}
	if len(groups.HighRisk) != 1 || !groups.HighRisk[0].Added {
		t.Fatalf("unexpected high_risk group: %+v", groups.HighRisk)
	}
}

func TestPackageHardeningRegressions(t *testing.T) {
	// 1. manifest.version rejects path traversal and unsafe directory characters.
	for _, badVer := range []string{"../secrets", `1.0\..\x`, "..", ".", ".hidden", "1.0:bad", strings.Repeat("a", 65)} {
		raw := []byte(fmt.Sprintf(`{"id":"safe-plug","kind":"plugin","version":%q,"name":"Safe","apiVersion":1}`, badVer))
		if _, err := ParseManifest(raw); !errors.Is(err, ErrBadManifest) {
			t.Fatalf("version %q: expected ErrBadManifest, got %v", badVer, err)
		}
	}

	// 2. Archive rejects case-colliding entries (e.g., server.js + Server.js).
	pkgCollision := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{"id":"case-plug","kind":"plugin","version":"1.0.0","name":"Case","apiVersion":1,"server":"server.js"}`),
		"server.js":   []byte(`export default { setup() {} };`),
	}, nil, false, false, func(zw *zip.Writer) error {
		w, err := zw.Create("Server.js")
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(`evil`))
		return err
	})
	if _, err := ParsePackage(pkgCollision, nil); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage on case-colliding zip entry, got %v", err)
	}

	// 3. Built-in theme ID hijacking ("default", "brocade") is refused.
	for _, builtinID := range []string{"default", "brocade"} {
		slp := buildTestSLP(t, map[string][]byte{
			"plugin.json": []byte(`{"id":"` + builtinID + `","kind":"theme","version":"1.0.0","name":"Hijack","apiVersion":1}`),
		}, nil, false, false)
		pkg, err := ParsePackage(slp, nil)
		if err != nil {
			t.Fatalf("ParsePackage(%s): %v", builtinID, err)
		}
		if _, err := pkg.ValidateTheme(theme.NewRegistry()); !errors.Is(err, ErrBadPackage) {
			t.Fatalf("expected ErrBadPackage rejecting built-in theme hijack %q, got %v", builtinID, err)
		}
	}

	// 4. ExtractPackageFiles preserves atomicity and cleans up backup swap dirs.
	slpV1 := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{"id":"extract-ok","kind":"plugin","version":"1.0.0","name":"Extract","apiVersion":1,"server":"server.js"}`),
		"server.js":   []byte(`export default { v: 1 };`),
	}, nil, false, false)
	pkg1, err := ParsePackage(slpV1, nil)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "extract-ok", "1.0.0")
	if err := ExtractPackageFiles(pkg1, dest); err != nil {
		t.Fatalf("ExtractPackageFiles v1: %v", err)
	}
	slpV2 := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{"id":"extract-ok","kind":"plugin","version":"1.0.0","name":"Extract","apiVersion":1,"server":"server.js"}`),
		"server.js":   []byte(`export default { v: 2 };`),
	}, nil, false, false)
	pkg2, err := ParsePackage(slpV2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExtractPackageFiles(pkg2, dest); err != nil {
		t.Fatalf("ExtractPackageFiles overwrite: %v", err)
	}
	rawJS, err := os.ReadFile(filepath.Join(dest, "server.js"))
	if err != nil || !strings.Contains(string(rawJS), "v: 2") {
		t.Fatalf("unexpected extracted server.js: err=%v body=%q", err, string(rawJS))
	}
}

func TestParsePackage_NativeLimitsAndPlatforms(t *testing.T) {
	priv, pubPEM := testKeypairPEM(t)

	// 1. Missing executable must fail with ErrBadPackage.
	noBin := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "native-missing-bin",
			"kind": "plugin",
			"runtime": "native",
			"version": "1.0.0",
			"name": "Native Missing",
			"apiVersion": 1,
			"native": { "protocolVersion": 1, "executable": "bin/plug", "platforms": ["linux/amd64"] }
		}`),
	}, priv, false, false)
	if _, err := ParsePackageForPlatform(noBin, []string{pubPEM}, "linux/amd64"); !errors.Is(err, ErrBadPackage) {
		t.Fatalf("expected ErrBadPackage when native binary missing, got %v", err)
	}

	// 2. Single-platform native binary larger than MaxPackageFileBytes (10 MiB) is allowed up to 64 MiB.
	bigBin := bytes.Repeat([]byte{0x7f, 'E', 'L', 'F'}, 3*1024*1024) // 12 MiB > 10 MiB JS limit
	singleSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "native-big",
			"kind": "plugin",
			"runtime": "native",
			"version": "1.0.0",
			"name": "Native Big",
			"apiVersion": 1,
			"native": { "protocolVersion": 1, "executable": "bin/plug", "platforms": ["linux/amd64"] }
		}`),
		"bin/plug_linux_amd64": bigBin,
	}, priv, false, false)
	parsedOK, err := ParsePackageForPlatform(singleSLP, []string{pubPEM}, "linux/amd64")
	if err != nil {
		t.Fatalf("ParsePackageForPlatform native 12MiB binary: %v", err)
	}
	if !parsedOK.HostCompatible || len(parsedOK.ContainedPlatforms) != 1 || parsedOK.ContainedPlatforms[0] != "linux/amd64" {
		t.Fatalf("unexpected native single parse: compat=%v contained=%v", parsedOK.HostCompatible, parsedOK.ContainedPlatforms)
	}
	parsedOtherArch, err := ParsePackageForPlatform(singleSLP, []string{pubPEM}, "linux/arm64")
	if err != nil {
		t.Fatalf("ParsePackageForPlatform mismatched arch should still inspect cleanly: %v", err)
	}
	if parsedOtherArch.HostCompatible {
		t.Fatal("expected HostCompatible=false for linux/arm64 on linux/amd64-only package")
	}

	// 3. Fat pack containing multiple platforms detects all contained platforms cleanly.
	fatSLP := buildTestSLP(t, map[string][]byte{
		"plugin.json": []byte(`{
			"id": "native-fat",
			"kind": "plugin",
			"runtime": "native",
			"version": "1.0.0",
			"name": "Native Fat",
			"apiVersion": 1,
			"native": {
				"protocolVersion": 1,
				"executable": "bin/plug",
				"platforms": ["linux/amd64", "linux/arm64", "windows/amd64"]
			}
		}`),
		"bin/plug_linux_amd64":       []byte("elf-amd64"),
		"bin/plug_linux_arm64":       []byte("elf-arm64"),
		"bin/plug_windows_amd64.exe": []byte("pe-amd64"),
	}, priv, false, false)
	for _, plat := range []string{"linux/amd64", "linux/arm64", "windows/amd64"} {
		fatParsed, err := ParsePackageForPlatform(fatSLP, []string{pubPEM}, plat)
		if err != nil {
			t.Fatalf("fat parse %s: %v", plat, err)
		}
		if !fatParsed.HostCompatible || len(fatParsed.ContainedPlatforms) != 3 {
			t.Fatalf("fat parse %s: compat=%v contained=%v", plat, fatParsed.HostCompatible, fatParsed.ContainedPlatforms)
		}
	}
}
