package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/importer/bs"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/logging"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/secrets"
)

func newTestSetupController(t *testing.T, dataDir string, baseCfg config.Config, envOv map[string]bool) (*setupController, *Core) {
	t.Helper()
	logCtl, log := logging.New(logging.Options{Console: io.Discard, Level: "info", Format: "text"})
	broker := pubsub.New()
	t.Cleanup(broker.Close)
	themes, reg, err := OpenThemesAndRegistry(dataDir, log)
	if err != nil {
		t.Fatal(err)
	}
	var transitioned *Core
	sc := newSetupController(
		Paths{DataDir: dataDir, ConfigPath: filepath.Join(dataDir, "config.toml")},
		baseCfg,
		envOv,
		"",
		logCtl,
		log,
		broker,
		themes,
		reg,
		func(c *Core) error {
			transitioned = c
			return nil
		},
	)
	t.Cleanup(func() {
		if transitioned != nil {
			transitioned.Close(context.Background())
		}
	})
	return sc, transitioned
}

func TestSetupControllerWizardAndConnectExisting(t *testing.T) {
	ctx := context.Background()
	data1 := t.TempDir()
	dbFile := filepath.Join(data1, "shared.db")

	tok, err := secrets.CreateSetupToken(data1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	logCtl, log := logging.New(logging.Options{Console: io.Discard, Level: "info", Format: "text"})
	broker := pubsub.New()
	defer broker.Close()
	themes, reg, err := OpenThemesAndRegistry(data1, log)
	if err != nil {
		t.Fatal(err)
	}
	var core1 *Core
	sc := newSetupController(
		Paths{DataDir: data1, ConfigPath: filepath.Join(data1, "config.toml")},
		config.Defaults(),
		map[string]bool{},
		"",
		logCtl,
		log,
		broker,
		themes,
		reg,
		func(c *Core) error {
			core1 = c
			return nil
		},
	)

	if _, _, err := sc.CreateSession(ctx, "wrong-token", "198.51.100.1"); err == nil {
		t.Fatal("expected invalid token error")
	}
	cookie, st, err := sc.CreateSession(ctx, tok, "198.51.100.1")
	if err != nil || !st.NeedsSetup || !sc.ValidateSession(cookie) {
		t.Fatalf("CreateSession: cookie=%q st=%+v err=%v", cookie, st, err)
	}

	// Detected proxy hint when behind an untrusted private proxy.
	state, err := sc.GetState(ctx, httpapi.SetupStateRequest{
		Client:         realip.Info{ClientIP: "172.18.0.2", Proto: "http", Host: "internal:17546"},
		PeerIP:         "172.18.0.2",
		HasForwarded:   true,
		ForwardedHost:  "skins.example.com",
		ForwardedProto: "https",
	})
	if err != nil || state.DetectedProxy == nil || state.SuggestedURL != "https://skins.example.com" {
		t.Fatalf("GetState proxy hint: %+v err=%v", state, err)
	}

	// Validation error must not write config.toml.
	_, _, err = sc.CompleteWizard(ctx, httpapi.CompleteWizardInput{
		Database: httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: dbFile},
		Storage:  httpapi.StorageConfigInput{Driver: "local", Path: "textures"},
		SiteURL:  "ftp://bad",
		Email:    "admin@example.com",
		Nickname: "Admin",
		Password: "admin-pass-1",
	})
	if err == nil {
		t.Fatal("expected bad siteUrl validation error")
	}
	if _, statErr := os.Stat(filepath.Join(data1, "config.toml")); !os.IsNotExist(statErr) {
		t.Fatal("config.toml must not be written when validation fails")
	}

	u, sess, err := sc.CompleteWizard(ctx, httpapi.CompleteWizardInput{
		Database:       httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: dbFile},
		Storage:        httpapi.StorageConfigInput{Driver: "local", Path: "textures"},
		TrustedProxies: []string{"172.18.0.0/16"},
		SiteName:       "Test Site",
		SiteURL:        "https://skins.example.com/",
		Timezone:       "UTC",
		Email:          "admin@example.com",
		Nickname:       "Admin",
		Password:       "admin-pass-1",
		Mail: &httpapi.MailConfigInput{
			Driver: "log",
		},
	})
	if err != nil || u == nil || sess == nil || core1 == nil {
		t.Fatalf("CompleteWizard: u=%v sess=%v core=%v err=%v", u, sess, core1, err)
	}
	if core1.Settings.Current().Localized("site.name", "en") != "Test Site" ||
		core1.Settings.Current().String("mail.driver") != "log" ||
		core1.Settings.Current().String("site.timezone") != "UTC" {
		t.Fatalf("saved settings mismatch: site.name=%q mail.driver=%q tz=%q",
			core1.Settings.Current().Localized("site.name", "en"),
			core1.Settings.Current().String("mail.driver"),
			core1.Settings.Current().String("site.timezone"))
	}
	core1.Close(ctx)
	if sc.ValidateSession(cookie) {
		t.Fatal("setup session must be invalidated after completion")
	}
	if secrets.IsSetupPending(data1) || !secrets.IsSetupDone(data1) {
		t.Fatal("setup markers not updated properly")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(data1, "config.toml"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
		}
	}

	// Second node with empty data2 connects to the existing shared DB.
	data2 := t.TempDir()
	logCtl2, log2 := logging.New(logging.Options{Console: io.Discard, Level: "info", Format: "text"})
	broker2 := pubsub.New()
	defer broker2.Close()
	themes2, reg2, _ := OpenThemesAndRegistry(data2, log2)
	var core2 *Core
	sc2 := newSetupController(
		Paths{DataDir: data2, ConfigPath: filepath.Join(data2, "config.toml")},
		config.Defaults(),
		map[string]bool{},
		"",
		logCtl2,
		log2,
		broker2,
		themes2,
		reg2,
		func(c *Core) error {
			core2 = c
			return nil
		},
	)
	probe, err := sc2.TestDB(ctx, httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: dbFile})
	if err != nil || !probe.OK || probe.State != "skinloom_installed" || probe.Users != 1 {
		t.Fatalf("TestDB existing: %+v err=%v", probe, err)
	}
	connRes, err := sc2.ConnectExisting(ctx, httpapi.ConnectExistingInput{
		Database: httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: dbFile},
		Storage:  httpapi.StorageConfigInput{Driver: "local", Path: "textures"},
	})
	if err != nil || connRes.RedirectTo != "/auth/login" || core2 == nil {
		t.Fatalf("ConnectExisting: res=%+v core=%v err=%v", connRes, core2, err)
	}
	core2.Close(ctx)
}

func TestChunkedUploadAndArchiveSafety(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	sc, _ := newTestSetupController(t, data, config.Defaults(), map[string]bool{})

	// Build a valid zip archive with textures/<hash>.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	hash := strings.Repeat("a", 64)
	w, err := zw.Create("textures/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("png-data"))
	_ = zw.Close()
	payload := zipBuf.Bytes()

	up, err := sc.CreateUpload(ctx, httpapi.CreateUploadInput{
		Purpose:   "bs_textures",
		Filename:  "textures.zip",
		TotalSize: int64(len(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mid := len(payload) / 2
	r1, err := sc.PutUploadChunk(ctx, up.UploadID, 0, bytes.NewReader(payload[:mid]))
	if err != nil || r1.Complete || r1.Received != int64(mid) {
		t.Fatalf("chunk 1: %+v err=%v", r1, err)
	}
	// Wrong offset must be rejected.
	if _, err := sc.PutUploadChunk(ctx, up.UploadID, 0, bytes.NewReader(payload[mid:])); err == nil {
		t.Fatal("expected offset mismatch error")
	}
	r2, err := sc.PutUploadChunk(ctx, up.UploadID, int64(mid), bytes.NewReader(payload[mid:]))
	if err != nil || !r2.Complete || r2.Received != int64(len(payload)) {
		t.Fatalf("chunk 2: %+v err=%v", r2, err)
	}

	archivePath, err := sc.resolveUploadedPath(up.UploadID, "bs_textures")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := extractTextureArchive(ctx, archivePath, dest); err != nil {
		t.Fatalf("extractTextureArchive: %v", err)
	}
	root := bs.DetectTexturesRoot(dest)
	if filepath.Base(root) != "textures" {
		t.Fatalf("DetectTexturesRoot = %q, want .../textures", root)
	}
	if got, err := os.ReadFile(filepath.Join(root, hash)); err != nil || string(got) != "png-data" {
		t.Fatalf("extracted file = %q, err=%v", got, err)
	}

	// Zip-Slip path must be rejected.
	var badZip bytes.Buffer
	bzw := zip.NewWriter(&badZip)
	bw, _ := bzw.Create("../../evil")
	_, _ = bw.Write([]byte("x"))
	_ = bzw.Close()
	badZipPath := filepath.Join(t.TempDir(), "slip.zip")
	_ = os.WriteFile(badZipPath, badZip.Bytes(), 0o600)
	if err := extractTextureArchive(ctx, badZipPath, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected zip-slip refusal, got %v", err)
	}

	// Symlink inside tar.gz must be rejected.
	var badTar bytes.Buffer
	gz := gzip.NewWriter(&badTar)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "textures/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()
	badTarPath := filepath.Join(t.TempDir(), "sym.tar.gz")
	_ = os.WriteFile(badTarPath, badTar.Bytes(), 0o600)
	if err := extractTextureArchive(ctx, badTarPath, t.TempDir()); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
}

func TestSetupControllerBSImportAndBackupRestore(t *testing.T) {
	ctx := context.Background()
	bsDir := t.TempDir()
	bsDBPath := filepath.Join(bsDir, "bs.sqlite")
	pool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(bsDBPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE users (uid integer primary key, email text, nickname text, score integer, avatar integer, password text, ip text, permission integer, last_sign_at text, register_at text, verified integer, locale text)`,
		`CREATE TABLE players (pid integer primary key, uid integer, name text, tid_skin integer, tid_cape integer, last_modified text)`,
		`CREATE TABLE textures (tid integer primary key, name text, type text, hash text, size integer, uploader integer, public integer, upload_at text, likes integer)`,
		`CREATE TABLE options (option_name text, option_value text)`,
		`INSERT INTO users VALUES (1, 'bsadmin@example.com', 'BSAdmin', 100, 0, '$2y$10$abcdefghijklmnopqrstuv', '127.0.0.1', 2, '2026-01-01 00:00:00', '2026-01-01 00:00:00', 1, 'zh_CN')`,
		`INSERT INTO players VALUES (1, 1, 'BSSteve', 1, 0, '2026-01-01 00:00:00')`,
	} {
		if _, err := pool.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	img.Set(0, 0, color.NRGBA{1, 2, 3, 255})
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	sum := sha256.Sum256(pngBuf.Bytes())
	texHash := hex.EncodeToString(sum[:])
	if _, err := pool.Exec(`INSERT INTO textures VALUES (1, 'Skin1', 'steve', ?, 1, 1, 1, '2026-01-01 00:00:00', 0)`, texHash); err != nil {
		t.Fatal(err)
	}
	pool.Close()

	// Create uploaded zip of textures.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, _ := zw.Create("storage/textures/" + texHash)
	_, _ = fw.Write(pngBuf.Bytes())
	_ = zw.Close()

	data := t.TempDir()
	logCtl, log := logging.New(logging.Options{Console: io.Discard, Level: "info", Format: "text"})
	broker := pubsub.New()
	defer broker.Close()
	themes, reg, _ := OpenThemesAndRegistry(data, log)
	var importedCore *Core
	sc := newSetupController(
		Paths{DataDir: data, ConfigPath: filepath.Join(data, "config.toml")},
		config.Defaults(),
		map[string]bool{},
		"",
		logCtl,
		log,
		broker,
		themes,
		reg,
		func(c *Core) error {
			importedCore = c
			return nil
		},
	)

	envText := "DB_CONNECTION=sqlite\nDB_DATABASE=" + filepath.ToSlash(bsDBPath) + "\nMAIL_PASSWORD=secret-mail-pass\nSALT=secret-salt\n"
	parsed, err := sc.ParseBSEnv(ctx, httpapi.ParseBSEnvInput{Content: envText})
	if err != nil {
		t.Fatal(err)
	}
	rawParsed, _ := json.Marshal(parsed)
	if strings.Contains(string(rawParsed), "secret-mail-pass") || strings.Contains(string(rawParsed), "secret-salt") {
		t.Fatalf("ParseBSEnv leaked secrets: %s", rawParsed)
	}
	testRes, err := sc.TestBSSource(ctx, httpapi.BSSourceInput{EnvID: parsed.EnvID, TextureSource: "none"})
	if err != nil || !testRes.OK || testRes.Counts.Users != 1 || testRes.Counts.Players != 1 {
		t.Fatalf("TestBSSource: %+v err=%v", testRes, err)
	}

	up, err := sc.CreateUpload(ctx, httpapi.CreateUploadInput{Purpose: "bs_textures", Filename: "tex.zip", TotalSize: int64(zipBuf.Len())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.PutUploadChunk(ctx, up.UploadID, 0, bytes.NewReader(zipBuf.Bytes())); err != nil {
		t.Fatal(err)
	}

	// Dry run first.
	dryJob, err := sc.StartBSDryRun(ctx, httpapi.BSDryRunInput{
		Source: httpapi.BSSourceInput{EnvID: parsed.EnvID, TextureSource: "upload", UploadID: up.UploadID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dryView := waitJob(t, sc, dryJob.JobID)
	if dryView.Status != "succeeded" || dryView.Report == nil || dryView.Report.Imported["users"] != 1 {
		t.Fatalf("dry-run failed: %+v", dryView)
	}
	txt, _, err := sc.GetJobReportText(ctx, dryJob.JobID)
	if err != nil || !strings.Contains(string(txt), "Dry run") {
		t.Fatalf("report.txt: %v %s", err, txt)
	}

	// Real import.
	impJob, err := sc.StartBSImport(ctx, httpapi.BSImportInput{
		Database: httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: "skinloom.db"},
		Storage:  httpapi.StorageConfigInput{Driver: "local", Path: "textures"},
		Source:   httpapi.BSSourceInput{EnvID: parsed.EnvID, TextureSource: "upload", UploadID: up.UploadID},
		SiteURL:  "https://bs-migrated.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	impView := waitJob(t, sc, impJob.JobID)
	if impView.Status != "succeeded" || importedCore == nil {
		t.Fatalf("import failed: %+v", impView)
	}
	if importedCore.Settings.Current().String("site.url") != "https://bs-migrated.example.com" {
		t.Fatalf("site.url = %q", importedCore.Settings.Current().String("site.url"))
	}
	// Take a backup of this site, close it, and restore into a brand new data directory via SetupController.
	backupDir := t.TempDir()
	backupPath, _, err := Backup(ctx, Paths{DataDir: data, ConfigPath: filepath.Join(data, "config.toml")}, BackupOptions{OutDir: backupDir, Textures: true})
	importedCore.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}

	restoredDir := t.TempDir()
	logCtlR, logR := logging.New(logging.Options{Console: io.Discard, Level: "info", Format: "text"})
	brokerR := pubsub.New()
	defer brokerR.Close()
	themesR, regR, _ := OpenThemesAndRegistry(restoredDir, logR)
	var restoredCore *Core
	scR := newSetupController(
		Paths{DataDir: restoredDir, ConfigPath: filepath.Join(restoredDir, "config.toml")},
		config.Defaults(),
		map[string]bool{},
		"",
		logCtlR,
		logR,
		brokerR,
		themesR,
		regR,
		func(c *Core) error {
			restoredCore = c
			return nil
		},
	)
	ins, err := scR.InspectRestore(ctx, httpapi.InspectRestoreInput{Path: backupPath})
	if err != nil || !ins.Manifest.HasDatabase || ins.Manifest.Textures != 1 {
		t.Fatalf("InspectRestore: %+v err=%v", ins, err)
	}
	restJob, err := scR.StartRestore(ctx, httpapi.ExecuteRestoreInput{
		Path:     backupPath,
		Database: httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: "custom-restored.db"},
		Storage:  httpapi.StorageConfigInput{Driver: "local", Path: "textures"},
	})
	if err != nil {
		t.Fatal(err)
	}
	restView := waitJob(t, scR, restJob.JobID)
	if restView.Status != "succeeded" || restoredCore == nil {
		t.Fatalf("restore job failed: %+v", restView)
	}
	defer restoredCore.Close(ctx)
	if got := restoredCore.Settings.Current().String("site.url"); got != "https://bs-migrated.example.com" {
		t.Fatalf("restored site.url = %q", got)
	}
	if _, err := os.Stat(filepath.Join(restoredDir, "custom-restored.db")); err != nil {
		t.Fatalf("expected custom SQLite path custom-restored.db to exist: %v", err)
	}
}

func waitJob(t *testing.T, sc *setupController, id string) *httpapi.SetupJobView {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		v, err := sc.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != "running" {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("job %s timed out", id)
	return nil
}

func TestSetupControllerBridgeMigration(t *testing.T) {
	ctx := context.Background()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(pngBuf.Bytes())
	texHash := hex.EncodeToString(sum[:])

	const tok = "bridge-test-secret"
	mux := http.NewServeMux()
	mux.HandleFunc("/plugin/skinloom-bridge/manifest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pluginVersion":  "1.0.0",
			"prefix":         "bs_",
			"timezone":       "UTC",
			"passwordMethod": "BCRYPT",
			"uuidTable":      false,
			"missingTables":  []string{"user_closet", "reports", "notifications", "language_lines", "oauth_clients"},
			"otherTables":    []any{},
			"options":        map[string]string{"site_name": "BridgeSite"},
			"counts":         map[string]any{"users": 1, "players": 1, "textures": 1, "closet": 0},
		})
	})
	mux.HandleFunc("/plugin/skinloom-bridge/rows", func(w http.ResponseWriter, r *http.Request) {
		table := r.URL.Query().Get("table")
		switch table {
		case "users":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table": "users",
				"rows": []map[string]any{{
					"id": 1, "email": "admin@bridge.example.com", "nickname": "BridgeAdmin",
					"password": "$2y$10$abcdefghijklmnopqrstuv", "permission": 1, "verified": true,
				}},
				"hasMore": false,
			})
		case "players":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table":   "players",
				"rows":    []map[string]any{{"id": 1, "userId": 1, "name": "BridgeSteve", "skin": 1, "cape": 0}},
				"hasMore": false,
			})
		case "textures":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table":   "textures",
				"rows":    []map[string]any{{"id": 1, "name": "BridgeSkin", "type": "steve", "hash": texHash, "sizeKB": 1, "uploader": 1, "public": true}},
				"hasMore": false,
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"table": table, "rows": []any{}, "hasMore": false})
		}
	})
	mux.HandleFunc("/plugin/skinloom-bridge/textures/"+texHash, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Texture-Sha256", texHash)
		w.Header().Set("X-Texture-Width", "64")
		w.Header().Set("X-Texture-Height", "64")
		w.Header().Set("Content-Length", "100")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Del("Content-Length")
		_, _ = w.Write(pngBuf.Bytes())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	data := t.TempDir()
	sc, _ := newTestSetupController(t, data, config.Defaults(), map[string]bool{})

	srcIn := httpapi.BSSourceInput{
		SourceType:         "bridge",
		BridgeURL:          srv.URL,
		BridgeToken:        tok,
		AllowInsecureLocal: true,
	}
	res, err := sc.TestBSSource(ctx, srcIn)
	if err != nil || !res.OK || res.Counts.Users != 1 {
		t.Fatalf("TestBSSource bridge: %+v err=%v", res, err)
	}

	dryJob, err := sc.StartBSDryRun(ctx, httpapi.BSDryRunInput{Source: srcIn})
	if err != nil {
		t.Fatal(err)
	}
	dryView := waitJob(t, sc, dryJob.JobID)
	if dryView.Status != "succeeded" || dryView.Report == nil || dryView.Report.Blocked {
		t.Fatalf("bridge dry-run failed: %+v", dryView)
	}

	impJob, err := sc.StartBSImport(ctx, httpapi.BSImportInput{
		Source:   srcIn,
		Database: httpapi.DBConfigInput{Driver: "sqlite", SQLitePath: "skinloom.db"},
		Storage:  httpapi.StorageConfigInput{Driver: "local", Path: "textures"},
		SiteURL:  "https://bridge-target.example.com",
		// Same email as imported permission=1 BS user: must promote cleanly without unique constraint failure.
		FallbackAdmin: &httpapi.FallbackAdminInput{
			Email:    "admin@bridge.example.com",
			Nickname: "PromotedSuperAdmin",
			Password: "new-super-password",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	impView := waitJob(t, sc, impJob.JobID)
	if impView.Status != "succeeded" || !sc.completed.Load() {
		t.Fatalf("bridge import failed: %+v", impView)
	}
	blobFile := filepath.Join(data, "textures", texHash)
	if _, err := os.Stat(blobFile); err != nil {
		t.Fatalf("expected bridged texture stored at %s: %v", blobFile, err)
	}
}
