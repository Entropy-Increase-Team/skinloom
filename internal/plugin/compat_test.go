package plugin

import (
	"slices"
	"strings"
	"testing"
)

func TestSemverParseAndCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3+meta", 0},
		{"go1.26", "1.26.0", 0},
		{"go1.26.10", "1.26.2", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"go1.26rc1", "go1.26.0", -1},
		{"2.0.0", "1.99.99", 1},
	}
	for _, tc := range cases {
		va, err := ParseSemver(tc.a)
		if err != nil {
			t.Fatalf("ParseSemver(%q): %v", tc.a, err)
		}
		vb, err := ParseSemver(tc.b)
		if err != nil {
			t.Fatalf("ParseSemver(%q): %v", tc.b, err)
		}
		if got := va.Compare(vb); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}

	for _, bad := range []string{"", "1.2.3.4", "abc", "1.-2.0"} {
		if _, err := ParseSemver(bad); err == nil {
			t.Errorf("expected ParseSemver(%q) to fail", bad)
		}
	}
}

func TestSemverRangeMatch(t *testing.T) {
	cases := []struct {
		expr string
		ver  string
		want bool
	}{
		{"*", "0.1.0", true},
		{"", "9.9.9", true},
		{">=1.0.0 <2.0.0", "1.5.2", true},
		{">=1.0.0 <2.0.0", "2.0.0", false},
		{">=1.0.0, <2.0.0", "1.0.0", true},
		{">=1.26", "go1.26.10", true},
		{">=1.26", "go1.25.9", false},
		{"^1.2.3", "1.9.0", true},
		{"^1.2.3", "2.0.0", false},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2", "1.2.5", true},
		{"~1", "1.9.0", true},
		{"1.x", "1.4.2", true},
		{"1.x", "2.0.0", false},
		{"1.2.*", "1.2.99", true},
		{"1.2.*", "1.3.0", false},
		{"1.0.0 - 2.0.0", "1.5.0", true},
		{"1.0.0 - 2.0.0", "2.0.0", true},
		{"1.0.0 - 2.0.0", "2.0.1", false},
		{"1.0 - 2.1", "2.1.9", true},
		{"1.0 - 2.1", "2.2.0", false},
		{">=1.0.0 <1.5.0 || >=2.0.0", "1.8.0", false},
		{">=1.0.0 <1.5.0 || >=2.0.0", "2.1.0", true},
		{"!=1.2.3 >=1.0.0", "1.2.3", false},
		{"!=1.2.3 >=1.0.0", "1.2.4", true},
	}
	for _, tc := range cases {
		rng, err := ParseSemverRange(tc.expr)
		if err != nil {
			t.Fatalf("ParseSemverRange(%q): %v", tc.expr, err)
		}
		if got := rng.MatchString(tc.ver); got != tc.want {
			t.Errorf("Range(%q).Match(%q) = %v, want %v", tc.expr, tc.ver, got, tc.want)
		}
	}

	for _, bad := range []string{">=1.0 || ", ">=1.x", "^", "~"} {
		if _, err := ParseSemverRange(bad); err == nil {
			t.Errorf("expected ParseSemverRange(%q) to fail", bad)
		}
	}
}

func TestNativeManifestValidation(t *testing.T) {
	validNative := `{
		"id": "bs-importer",
		"kind": "plugin",
		"runtime": "native",
		"version": "1.0.0",
		"name": {"zh-CN": "迁移助手", "en": "BS Importer"},
		"apiVersion": 1,
		"engines": {"skinloom": ">=1.0.0 <2.0.0", "go": ">=1.26"},
		"native": {
			"protocolVersion": 1,
			"executable": "bin/bs-importer",
			"platforms": ["linux/amd64", "windows/amd64"],
			"httpProxy": true,
			"database": {"required": true}
		},
		"permissions": ["storage.sql", "http.stream", "users.read"]
	}`
	mf, err := ParseManifest([]byte(validNative))
	if err != nil {
		t.Fatalf("expected valid native manifest, got: %v", err)
	}
	if mf.EffectiveRuntime() != RuntimeNative {
		t.Fatalf("EffectiveRuntime() = %q, want native", mf.EffectiveRuntime())
	}
	if !slices.Contains(mf.EffectivePermissions(), PermNativeExec) {
		t.Fatalf("expected implicit %q in EffectivePermissions: %v", PermNativeExec, mf.EffectivePermissions())
	}
	winCands := mf.NativeExecutableCandidates("windows", "amd64")
	if winCands[0] != "bin/bs-importer_windows_amd64.exe" {
		t.Fatalf("unexpected windows candidate[0]: %v", winCands)
	}
	linCands := mf.NativeExecutableCandidates("linux", "arm64")
	if linCands[0] != "bin/bs-importer_linux_arm64" {
		t.Fatalf("unexpected linux candidate[0]: %v", linCands)
	}

	invalidCases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "js plugin using native-only permission",
			raw:  `{"id":"bad-js","kind":"plugin","version":"1.0.0","name":"Bad","apiVersion":1,"permissions":["storage.sql"]}`,
			want: "requires runtime",
		},
		{
			name: "js plugin with native block",
			raw:  `{"id":"bad-js2","kind":"plugin","runtime":"js","version":"1.0.0","name":"Bad","apiVersion":1,"native":{"protocolVersion":1,"executable":"bin/a"}}`,
			want: "only allowed when runtime",
		},
		{
			name: "native plugin missing native block",
			raw:  `{"id":"bad-nat","kind":"plugin","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1}`,
			want: "native configuration is required",
		},
		{
			name: "native plugin with server.js",
			raw:  `{"id":"bad-nat2","kind":"plugin","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1,"server":"server.js","native":{"protocolVersion":1,"executable":"bin/a"}}`,
			want: "must not declare server JS",
		},
		{
			name: "native database without storage.sql perm",
			raw:  `{"id":"bad-nat3","kind":"plugin","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1,"native":{"protocolVersion":1,"executable":"bin/a","database":{"required":true}}}`,
			want: "storage.sql",
		},
		{
			name: "native httpProxy without http.stream perm",
			raw:  `{"id":"bad-nat4","kind":"plugin","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1,"native":{"protocolVersion":1,"executable":"bin/a","httpProxy":true}}`,
			want: "http.stream",
		},
		{
			name: "theme cannot be native",
			raw:  `{"id":"bad-theme","kind":"theme","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1,"native":{"protocolVersion":1,"executable":"bin/a"}}`,
			want: "themes cannot use native runtime",
		},
		{
			name: "bad platform format",
			raw:  `{"id":"bad-plat","kind":"plugin","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1,"native":{"protocolVersion":1,"executable":"bin/a","platforms":["linux"]}}`,
			want: "invalid native.platforms",
		},
		{
			name: "bad protocol version",
			raw:  `{"id":"bad-proto","kind":"plugin","runtime":"native","version":"1.0.0","name":"Bad","apiVersion":1,"native":{"protocolVersion":2,"executable":"bin/a"}}`,
			want: "protocolVersion",
		},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseManifest error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestFiveDimensionalCompatibility(t *testing.T) {
	baseEnv := HostEnv{
		OS:                    "linux",
		Arch:                  "arm64",
		SkinloomVersion:       "1.2.0",
		APIVersion:            1,
		NativeProtocolVersion: 1,
		GoVersion:             "go1.26.10",
		Installed:             map[string]string{"core-auth": "1.1.0"},
	}

	makeNativePkg := func(id string, ver RegistryVersion) RegistryPackage {
		return RegistryPackage{
			ID:       id,
			Kind:     KindPlugin,
			Runtime:  RuntimeNative,
			Official: true,
			Name:     LocalizedText{"en": id},
			Versions: []RegistryVersion{ver},
		}
	}

	t.Run("dimension 1: platform match and mismatch", func(t *testing.T) {
		pkg := makeNativePkg("nat-plug", RegistryVersion{
			Version:               "1.0.0",
			APIVersion:            1,
			NativeProtocolVersion: 1,
			Artifacts: map[string]RegistryArtifact{
				"linux/amd64": {URLs: []string{"https://example.com/amd64.slp"}, SHA256: "aaa"},
			},
		})
		res := CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if res.Compatible || res.Status != CompatIncompatiblePlatform {
			t.Fatalf("expected incompatible_platform, got %+v", res)
		}

		pkg.Versions[0].Artifacts["linux/arm64"] = RegistryArtifact{URLs: []string{"https://example.com/arm64.slp"}, SHA256: "bbb"}
		res = CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if !res.Compatible || res.ArtifactKey != "linux/arm64" || res.Artifact.SHA256 != "bbb" {
			t.Fatalf("expected compatible linux/arm64 artifact, got %+v", res)
		}

		// JS plugin matches "any" on all platforms.
		jsPkg := RegistryPackage{
			ID:      "js-plug",
			Kind:    KindPlugin,
			Runtime: RuntimeJS,
			Versions: []RegistryVersion{{
				Version:    "1.0.0",
				APIVersion: 1,
				Artifacts:  map[string]RegistryArtifact{"any": {URLs: []string{"https://example.com/any.slp"}, SHA256: "ccc"}},
			}},
		}
		jsRes := CheckVersionCompat(jsPkg, jsPkg.Versions[0], baseEnv, nil)
		if !jsRes.Compatible || jsRes.ArtifactKey != "any" {
			t.Fatalf("expected JS any match, got %+v", jsRes)
		}
	})

	t.Run("dimension 2: skinloom semver constraint", func(t *testing.T) {
		pkg := makeNativePkg("ver-plug", RegistryVersion{
			Version:    "1.0.0",
			APIVersion: 1,
			Engines:    EnginesSpec{Skinloom: ">=2.0.0"},
			Artifacts:  map[string]RegistryArtifact{"linux/arm64": {SHA256: "a"}},
		})
		res := CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if res.Compatible || res.Status != CompatIncompatibleSkinloom {
			t.Fatalf("expected incompatible_skinloom, got %+v", res)
		}

		// Dev build with AllowDevSkinloom bypasses engines.skinloom.
		devEnv := baseEnv
		devEnv.SkinloomVersion = "0.0.0-dev"
		devEnv.AllowDevSkinloom = true
		res = CheckVersionCompat(pkg, pkg.Versions[0], devEnv, nil)
		if !res.Compatible {
			t.Fatalf("expected dev build to be compatible, got %+v", res)
		}
	})

	t.Run("dimension 3: API and native protocol version", func(t *testing.T) {
		pkg := makeNativePkg("api-plug", RegistryVersion{
			Version:               "1.0.0",
			APIVersion:            2,
			NativeProtocolVersion: 1,
			Artifacts:             map[string]RegistryArtifact{"linux/arm64": {SHA256: "a"}},
		})
		res := CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if res.Compatible || res.Status != CompatIncompatibleAPI {
			t.Fatalf("expected incompatible_api, got %+v", res)
		}

		pkg.Versions[0].APIVersion = 1
		pkg.Versions[0].NativeProtocolVersion = 2
		res = CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if res.Compatible || res.Status != CompatIncompatibleProtocol {
			t.Fatalf("expected incompatible_protocol, got %+v", res)
		}
	})

	t.Run("dimension 4: Go baseline version", func(t *testing.T) {
		pkg := makeNativePkg("go-plug", RegistryVersion{
			Version:    "1.0.0",
			APIVersion: 1,
			Engines:    EnginesSpec{Go: ">=1.27"},
			Artifacts:  map[string]RegistryArtifact{"linux/arm64": {SHA256: "a"}},
		})
		res := CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if res.Compatible || res.Status != CompatIncompatibleGo {
			t.Fatalf("expected incompatible_go, got %+v", res)
		}

		pkg.Versions[0].Engines.Go = ">=1.26"
		res = CheckVersionCompat(pkg, pkg.Versions[0], baseEnv, nil)
		if !res.Compatible {
			t.Fatalf("expected Go >=1.26 compatible with go1.26.10, got %+v", res)
		}
	})

	t.Run("dimension 5: cascade deps, conflict, and cycle detection", func(t *testing.T) {
		catalog := map[string]RegistryPackage{
			"dep-base": {
				ID: "dep-base", Kind: KindPlugin, Runtime: RuntimeJS,
				Versions: []RegistryVersion{
					{Version: "1.0.0", APIVersion: 1, Artifacts: map[string]RegistryArtifact{"any": {SHA256: "base1"}}},
					{Version: "2.0.0", APIVersion: 1, Artifacts: map[string]RegistryArtifact{"any": {SHA256: "base2"}}},
				},
			},
			"dep-mid": {
				ID: "dep-mid", Kind: KindPlugin, Runtime: RuntimeJS,
				Versions: []RegistryVersion{
					{
						Version: "1.2.0", APIVersion: 1,
						Dependencies: map[string]string{"dep-base": "^1.0.0", "core-auth": "^1.0.0"},
						Artifacts:    map[string]RegistryArtifact{"any": {SHA256: "mid1"}},
					},
				},
			},
			"app-ok": {
				ID: "app-ok", Kind: KindPlugin, Runtime: RuntimeNative,
				Versions: []RegistryVersion{
					{
						Version: "1.0.0", APIVersion: 1, NativeProtocolVersion: 1,
						Dependencies: map[string]string{"dep-mid": "^1.0.0"},
						Artifacts:    map[string]RegistryArtifact{"linux/arm64": {SHA256: "app1"}},
					},
				},
			},
			"app-conflict": {
				ID: "app-conflict", Kind: KindPlugin, Runtime: RuntimeJS,
				Versions: []RegistryVersion{
					{
						Version: "1.0.0", APIVersion: 1,
						Dependencies: map[string]string{"dep-mid": "^1.0.0", "dep-base": "^2.0.0"},
						Artifacts:    map[string]RegistryArtifact{"any": {SHA256: "conf"}},
					},
				},
			},
			"cyc-a": {
				ID: "cyc-a", Kind: KindPlugin, Runtime: RuntimeJS,
				Versions: []RegistryVersion{{
					Version: "1.0.0", APIVersion: 1,
					Dependencies: map[string]string{"cyc-b": "^1.0.0"},
					Artifacts:    map[string]RegistryArtifact{"any": {SHA256: "ca"}},
				}},
			},
			"cyc-b": {
				ID: "cyc-b", Kind: KindPlugin, Runtime: RuntimeJS,
				Versions: []RegistryVersion{{
					Version: "1.0.0", APIVersion: 1,
					Dependencies: map[string]string{"cyc-a": "^1.0.0"},
					Artifacts:    map[string]RegistryArtifact{"any": {SHA256: "cb"}},
				}},
			},
		}

		// Cascade topological order: dep-base -> dep-mid (core-auth already installed so skipped!)
		plan, err := ResolveDependencyPlan("app-ok", "1.0.0", baseEnv, catalog)
		if err != nil {
			t.Fatalf("ResolveDependencyPlan(app-ok): %v", err)
		}
		if len(plan.Install) != 2 || plan.Install[0].ID != "dep-base" || plan.Install[0].Version != "1.0.0" || plan.Install[1].ID != "dep-mid" {
			t.Fatalf("unexpected cascade install plan: %+v", plan.Install)
		}

		// Diamond conflict: dep-mid wants dep-base ^1.0.0 while app-conflict wants ^2.0.0.
		resConflict := CheckVersionCompat(catalog["app-conflict"], catalog["app-conflict"].Versions[0], baseEnv, catalog)
		if resConflict.Compatible || resConflict.Status != CompatConflictDependency {
			t.Fatalf("expected conflict_dependency, got %+v", resConflict)
		}

		// Cycle detection: cyc-a -> cyc-b -> cyc-a.
		resCycle := CheckVersionCompat(catalog["cyc-a"], catalog["cyc-a"].Versions[0], baseEnv, catalog)
		if resCycle.Compatible || resCycle.Status != CompatCyclicDependency {
			t.Fatalf("expected cyclic_dependency, got %+v", resCycle)
		}

		// Missing dependency not in installed or catalog.
		missingPkg := makeNativePkg("miss-plug", RegistryVersion{
			Version: "1.0.0", APIVersion: 1,
			Dependencies: map[string]string{"nonexistent": "^1.0.0"},
			Artifacts:    map[string]RegistryArtifact{"linux/arm64": {SHA256: "m"}},
		})
		resMissing := CheckVersionCompat(missingPkg, missingPkg.Versions[0], baseEnv, catalog)
		if resMissing.Compatible || resMissing.Status != CompatMissingDependency {
			t.Fatalf("expected missing_dependency, got %+v", resMissing)
		}
	})
}

func TestSelectBestVersionYankedAndBlocked(t *testing.T) {
	env := HostEnv{
		OS:                    "linux",
		Arch:                  "amd64",
		SkinloomVersion:       "1.5.0",
		APIVersion:            1,
		NativeProtocolVersion: 1,
		GoVersion:             "go1.26.10",
		Installed:             map[string]string{"demo-plug": "1.0.0"},
	}

	pkg := RegistryPackage{
		ID:      "demo-plug",
		Kind:    KindPlugin,
		Runtime: RuntimeNative,
		Versions: []RegistryVersion{
			{
				Version: "1.1.0", APIVersion: 1, NativeProtocolVersion: 1,
				Artifacts: map[string]RegistryArtifact{"linux/amd64": {SHA256: "v110"}},
			},
			{
				Version: "1.2.0", APIVersion: 1, NativeProtocolVersion: 1, Yanked: true,
				Artifacts: map[string]RegistryArtifact{"linux/amd64": {SHA256: "v120"}},
			},
			{
				Version: "2.0.0", APIVersion: 1, NativeProtocolVersion: 1,
				Engines:   EnginesSpec{Skinloom: ">=2.0.0"},
				Artifacts: map[string]RegistryArtifact{"linux/amd64": {SHA256: "v200"}},
			},
		},
	}

	summary := SelectBestVersion(pkg, env, nil, nil)
	if !summary.Compatible || summary.RecommendedVersion != "1.1.0" || !summary.UpgradeAvailable {
		t.Fatalf("expected upgrade to 1.1.0 (skipping 2.0.0 incompatible and 1.2.0 yanked), got %+v", summary)
	}

	// Blocked rule covering >=1.1.0 blocks 1.1.0 -> incompatible_blocked
	blocked := map[string]RegistryBlocked{
		"demo-plug": {ID: "demo-plug", Versions: ">=1.1.0", Reason: "CVE-2026-0001"},
	}
	summaryBlocked := SelectBestVersion(pkg, env, nil, blocked)
	if summaryBlocked.Compatible || summaryBlocked.Status != CompatBlocked {
		t.Fatalf("expected blocked status, got %+v", summaryBlocked)
	}
}

func TestTransitiveBlockedDependencyAndPrereleaseRange(t *testing.T) {
	// 1. Prerelease range gating: >=1.0.0 must not match 1.0.0-alpha.1 or 2.0.0-alpha.1 unless constraint has prerelease.
	rStable, err := ParseSemverRange(">=1.0.0")
	if err != nil {
		t.Fatalf("ParseSemverRange: %v", err)
	}
	vAlpha, _ := ParseSemver("1.0.0-alpha.1")
	vNextAlpha, _ := ParseSemver("2.0.0-alpha.1")
	vRelease, _ := ParseSemver("1.0.0")
	if rStable.Match(vAlpha) || rStable.Match(vNextAlpha) {
		t.Fatalf("stable range >=1.0.0 must not match prerelease versions")
	}
	if !rStable.Match(vRelease) {
		t.Fatalf("stable range >=1.0.0 must match 1.0.0")
	}
	rPre, err := ParseSemverRange(">=1.0.0-alpha.1 <1.0.0")
	if err != nil {
		t.Fatalf("ParseSemverRange prerelease: %v", err)
	}
	vAlpha2, _ := ParseSemver("1.0.0-alpha.2")
	if !rPre.Match(vAlpha2) {
		t.Fatalf("prerelease range >=1.0.0-alpha.1 <1.0.0 should match 1.0.0-alpha.2")
	}

	// 2. Transitive blocked dependency must be rejected by SelectBestVersion and ResolveDependencyPlanWithBlocked.
	env := HostEnv{
		OS:                    "linux",
		Arch:                  "amd64",
		SkinloomVersion:       "1.2.0",
		APIVersion:            1,
		NativeProtocolVersion: 1,
		GoVersion:             "go1.26.10",
	}
	catalog := map[string]RegistryPackage{
		"leaf-blocked": {
			ID: "leaf-blocked", Kind: KindPlugin, Runtime: RuntimeJS,
			Versions: []RegistryVersion{{
				Version: "1.0.0", APIVersion: 1,
				Artifacts: map[string]RegistryArtifact{"any": {SHA256: "leaf"}},
			}},
		},
		"top-app": {
			ID: "top-app", Kind: KindPlugin, Runtime: RuntimeJS,
			Versions: []RegistryVersion{{
				Version: "1.0.0", APIVersion: 1,
				Dependencies: map[string]string{"leaf-blocked": "^1.0.0"},
				Artifacts:    map[string]RegistryArtifact{"any": {SHA256: "top"}},
			}},
		},
	}
	blocked := map[string]RegistryBlocked{
		"leaf-blocked": {ID: "leaf-blocked", Versions: ">=1.0.0", Reason: "Compromised transitive dependency"},
	}

	summary := SelectBestVersion(catalog["top-app"], env, catalog, blocked)
	if summary.Compatible || summary.Status != CompatBlocked {
		t.Fatalf("expected top-app to be CompatBlocked due to blocked dependency, got %+v", summary)
	}
	if _, err := ResolveDependencyPlanWithBlocked("top-app", "1.0.0", env, catalog, blocked); err == nil {
		t.Fatalf("expected ResolveDependencyPlanWithBlocked to fail when transitive dependency is blocked")
	}
}
