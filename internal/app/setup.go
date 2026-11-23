package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/backup"
	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/importer/bs"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/fsperm"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/logging"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/secrets"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

const (
	setupSessionTTL         = 2 * time.Hour
	setupChunkSize    int64 = 8 << 20  // 8 MiB
	maxChunkBodyBytes int64 = 9 << 20  // 9 MiB (allows small framing overhead)
	defaultMaxUpload  int64 = 1 << 30  // 1 GiB
	maxArchiveEntries       = 300_000  // Zip-bomb entry count cap
	maxArchiveFile          = 64 << 20 // 64 MiB per texture file
	maxArchiveTotal   int64 = 4 << 30  // 4 GiB uncompressed cap
	probeObjectKey          = "7b5a14d12d68d65e32b5de6c83b361724cf84c0451f89df5de64a476cb41f999"
)

type setupSession struct {
	expiresAt time.Time
}

type uploadEntry struct {
	id        string
	purpose   string
	filename  string
	totalSize int64
	received  int64
	complete  bool
	partPath  string
	finalPath string
	file      *os.File
}

type setupJob struct {
	mu         sync.RWMutex
	id         string
	kind       string
	status     string
	stage      string
	percent    int
	done       int
	total      int
	message    string
	errText    string
	redirectTo string
	report     *bs.Report
	updatedAt  time.Time
}

func (j *setupJob) update(stage string, done, total int, msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stage = stage
	j.done = done
	j.total = total
	if msg != "" {
		j.message = msg
	}
	if total > 0 {
		j.percent = min(99, (done*100)/total)
	}
	j.updatedAt = time.Now()
}

func (j *setupJob) view() httpapi.SetupJobView {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return httpapi.SetupJobView{
		ID:         j.id,
		Kind:       j.kind,
		Status:     j.status,
		Stage:      j.stage,
		Percent:    j.percent,
		Done:       j.done,
		Total:      j.total,
		Message:    j.message,
		Error:      j.errText,
		RedirectTo: j.redirectTo,
		Report:     httpapi.BSReportToView(j.report),
		HasReport:  j.report != nil,
		UpdatedAt:  j.updatedAt,
	}
}

// setupController implements httpapi.SetupController during Pre-Core Setup Mode.
type setupController struct {
	paths          Paths
	baseConfig     config.Config
	envOverrides   map[string]bool
	initDBError    string
	maxUploadBytes int64
	logCtl         *logging.Logger
	log            *slog.Logger
	broker         *pubsub.Broker
	themes         *theme.Registry
	settingsReg    *settings.Registry
	onTransition   func(*Core) error

	tokenLimit *account.Limiter

	mu         sync.Mutex
	inProgress bool
	completed  atomic.Bool
	sessions   map[string]setupSession
	uploads    map[string]*uploadEntry
	parsedEnvs map[string]bs.Source
	jobs       map[string]*setupJob
	activeJob  string
}

func newSetupController(
	paths Paths,
	baseCfg config.Config,
	envOverrides map[string]bool,
	initDBError string,
	logCtl *logging.Logger,
	log *slog.Logger,
	broker *pubsub.Broker,
	themes *theme.Registry,
	settingsReg *settings.Registry,
	onTransition func(*Core) error,
) *setupController {
	maxUp := defaultMaxUpload
	if raw := strings.TrimSpace(os.Getenv("SKINLOOM_SETUP__MAX_UPLOAD_BYTES")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			maxUp = v
		}
	}
	return &setupController{
		paths:          paths,
		baseConfig:     baseCfg,
		envOverrides:   envOverrides,
		initDBError:    initDBError,
		maxUploadBytes: maxUp,
		logCtl:         logCtl,
		log:            log,
		broker:         broker,
		themes:         themes,
		settingsReg:    settingsReg,
		onTransition:   onTransition,
		tokenLimit:     account.NewLimiter(20, 15*time.Minute),
		sessions:       map[string]setupSession{},
		uploads:        map[string]*uploadEntry{},
		parsedEnvs:     map[string]bs.Source{},
		jobs:           map[string]*setupJob{},
	}
}

func (sc *setupController) Status(_ context.Context) (bool, bool, error) {
	locked := sc.envDBLocked()
	if sc.completed.Load() {
		return false, locked, nil
	}
	return true, locked, nil
}

func (sc *setupController) envDBLocked() bool {
	return sc.envOverrides["database.driver"] || sc.envOverrides["database.dsn"]
}

func (sc *setupController) envStorageLocked() bool {
	for _, k := range []string{
		"storage.driver", "storage.path", "storage.endpoint", "storage.bucket",
		"storage.region", "storage.access_key", "storage.secret_key",
		"storage.prefix", "storage.insecure", "storage.path_style", "storage.public_url",
	} {
		if sc.envOverrides[k] {
			return true
		}
	}
	return false
}

func (sc *setupController) CreateSession(ctx context.Context, rawToken, clientIP string) (string, *httpapi.SetupState, error) {
	if sc.completed.Load() {
		return "", nil, httpapi.ErrSetupAlreadyDone
	}
	key := account.ClientKey(clientIP)
	if wait, blocked := sc.tokenLimit.Take(key); blocked {
		return "", nil, &httpapi.SetupRateLimitError{RetryAfter: wait}
	}
	ok, err := secrets.VerifySetupToken(sc.paths.DataDir, rawToken)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, httpapi.ErrSetupInvalidToken
	}
	sc.tokenLimit.Reset(key)

	cookieToken := account.NewToken()
	hash := account.HashToken(cookieToken)

	sc.mu.Lock()
	now := time.Now()
	for k, s := range sc.sessions {
		if now.After(s.expiresAt) {
			delete(sc.sessions, k)
		}
	}
	sc.sessions[hash] = setupSession{expiresAt: now.Add(setupSessionTTL)}
	sc.mu.Unlock()

	st, err := sc.GetState(ctx, httpapi.SetupStateRequest{})
	if err != nil {
		return "", nil, err
	}
	return cookieToken, st, nil
}

func (sc *setupController) ValidateSession(cookieToken string) bool {
	if cookieToken == "" {
		return false
	}
	hash := account.HashToken(cookieToken)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.completed.Load() && len(sc.jobs) == 0 {
		return false
	}
	s, ok := sc.sessions[hash]
	if !ok {
		return false
	}
	if time.Now().After(s.expiresAt) {
		delete(sc.sessions, hash)
		return false
	}
	return true
}

func (sc *setupController) GetState(_ context.Context, req httpapi.SetupStateRequest) (*httpapi.SetupState, error) {
	sc.mu.Lock()
	activeJob := sc.activeJob
	sc.mu.Unlock()

	st := &httpapi.SetupState{
		NeedsSetup:  !sc.completed.Load(),
		InContainer: runningInContainer(),
		EnvLocked: httpapi.EnvLockedInfo{
			Database:       sc.envDBLocked(),
			DatabaseDriver: sc.baseConfig.Database.Driver,
			DatabaseMask:   maskDSN(sc.baseConfig.Database.Driver, sc.baseConfig.Database.DSN),
			Storage:        sc.envStorageLocked(),
			StorageDriver:  sc.baseConfig.Storage.Driver,
			StorageMask:    maskStorage(sc.baseConfig.Storage),
			TrustedProxies: sc.envOverrides["server.trusted_proxies"],
		},
		Database:        summarizeDB(sc.baseConfig.Database),
		Storage:         summarizeStorage(sc.baseConfig.Storage),
		TrustedProxies:  append([]string{}, sc.baseConfig.Server.TrustedProxies...),
		DefaultTimezone: "Asia/Shanghai",
		MaxUploadBytes:  sc.maxUploadBytes,
		ActiveJobID:     activeJob,
		InitDBError:     sc.initDBError,
	}
	if st.TrustedProxies == nil {
		st.TrustedProxies = []string{}
	}
	if req.Client.Host != "" {
		proto := cmp.Or(req.Client.Proto, "http")
		st.SuggestedURL = proto + "://" + req.Client.Host
	}
	if !req.Client.ViaTrustedProxy && req.HasForwarded {
		if addr, err := netip.ParseAddr(strings.TrimSpace(req.PeerIP)); err == nil {
			addr = addr.Unmap()
			if addr.IsLoopback() || addr.IsPrivate() {
				cidr := addr.String()
				if addr.Is4() && !addr.IsLoopback() {
					p, _ := addr.Prefix(16)
					cidr = p.String()
				}
				st.DetectedProxy = &httpapi.DetectedProxy{
					PeerIP:        addr.String(),
					SuggestedCIDR: cidr,
					Proto:         req.ForwardedProto,
					Host:          req.ForwardedHost,
				}
				if req.ForwardedHost != "" {
					proto := cmp.Or(strings.ToLower(req.ForwardedProto), "https")
					if proto != "http" && proto != "https" {
						proto = "https"
					}
					st.SuggestedURL = proto + "://" + req.ForwardedHost
				}
			}
		}
	}
	return st, nil
}

func (sc *setupController) TestDB(ctx context.Context, in httpapi.DBConfigInput) (*httpapi.TestDBResult, error) {
	dbCfg, err := sc.resolveDB(in)
	if err != nil {
		return nil, err
	}
	res, perr := db.Probe(ctx, db.Options{
		Driver:  dbCfg.Driver,
		DSN:     dbCfg.DSN,
		DataDir: sc.paths.DataDir,
	})
	out := &httpapi.TestDBResult{
		OK:          res.OK,
		Dialect:     string(res.Dialect),
		Version:     res.Version,
		State:       string(res.State),
		Summary:     res.Summary,
		Users:       res.Users,
		Textures:    res.Textures,
		Players:     res.Players,
		HintSQL:     res.HintSQL,
		ErrorDetail: redactSecretErr(res.ErrorDetail),
	}
	if out.ErrorDetail == "" && perr != nil {
		out.ErrorDetail = redactSecretErr(perr.Error())
	}
	return out, nil
}

func (sc *setupController) TestStorage(ctx context.Context, in httpapi.StorageConfigInput) (*httpapi.TestStorageResult, error) {
	stCfg, err := sc.resolveStorage(in)
	if err != nil {
		return nil, err
	}
	if stCfg.Driver == "s3" {
		s3ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		s3, err := storage.NewS3(s3ctx, storage.S3Options{
			Endpoint: stCfg.Endpoint, Bucket: stCfg.Bucket, Region: stCfg.Region,
			AccessKey: stCfg.AccessKey, SecretKey: stCfg.SecretKey, Prefix: stCfg.Prefix,
			Insecure: stCfg.Insecure, PathStyle: stCfg.PathStyle, PublicURL: stCfg.PublicURL,
		})
		if err != nil {
			return &httpapi.TestStorageResult{OK: false, Driver: "s3", ErrorDetail: redactSecretErr(err.Error())}, nil
		}
		probeData := []byte("skinloom-probe")
		if err := s3.Put(s3ctx, probeObjectKey, probeData); err != nil {
			return &httpapi.TestStorageResult{OK: false, Driver: "s3", ErrorDetail: "write probe failed: " + redactSecretErr(err.Error())}, nil
		}
		f, err := s3.Open(s3ctx, probeObjectKey)
		if err != nil {
			_ = s3.Delete(s3ctx, probeObjectKey)
			return &httpapi.TestStorageResult{OK: false, Driver: "s3", ErrorDetail: "read probe failed: " + redactSecretErr(err.Error())}, nil
		}
		_, rerr := io.ReadAll(f)
		_ = f.Close()
		derr := s3.Delete(s3ctx, probeObjectKey)
		if rerr != nil {
			return &httpapi.TestStorageResult{OK: false, Driver: "s3", ErrorDetail: "read probe failed: " + rerr.Error()}, nil
		}
		if derr != nil {
			return &httpapi.TestStorageResult{OK: false, Driver: "s3", ErrorDetail: "delete probe failed: " + derr.Error()}, nil
		}
		return &httpapi.TestStorageResult{
			OK:      true,
			Driver:  "s3",
			Summary: fmt.Sprintf("Connected to bucket %s (%s) and verified read/write/delete", stCfg.Bucket, stCfg.Endpoint),
		}, nil
	}

	dir := stCfg.Dir(sc.paths.DataDir)
	_, statErr := os.Stat(dir)
	existed := statErr == nil
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &httpapi.TestStorageResult{OK: false, Driver: "local", ErrorDetail: err.Error()}, nil
	}
	if !existed {
		defer func() { _ = os.Remove(dir) }()
	}
	f, err := os.CreateTemp(dir, ".skinloom-probe-*")
	if err != nil {
		return &httpapi.TestStorageResult{OK: false, Driver: "local", ErrorDetail: err.Error()}, nil
	}
	name := f.Name()
	_, werr := f.WriteString("ok")
	cerr := f.Close()
	_ = os.Remove(name)
	if werr != nil || cerr != nil {
		return &httpapi.TestStorageResult{OK: false, Driver: "local", ErrorDetail: errors.Join(werr, cerr).Error()}, nil
	}
	return &httpapi.TestStorageResult{
		OK:      true,
		Driver:  "local",
		Summary: "Directory is writable: " + dir,
	}, nil
}

func (sc *setupController) TestMail(ctx context.Context, in httpapi.TestMailInput, locale string) (*httpapi.TestMailResult, error) {
	if err := account.ValidateEmail(in.To); err != nil {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "to", Message: "not a valid email address"}}}
	}
	mc, _, err := sc.validateMailInput(in.Mail, "mail.")
	if err != nil {
		return nil, err
	}
	if !mc.Enabled() {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "mail.driver", Message: "select smtp or log to send a test email"}}}
	}
	msg := mail.Render(mail.KindTest, cmp.Or(locale, "zh-CN"), strings.TrimSpace(in.To), mail.Data{Site: cmp.Or(strings.TrimSpace(in.Site), "SkinLoom")})
	mailer := mail.New(sc.log.With("component", "mail"))
	if err := mailer.Send(ctx, mc, msg); err != nil {
		return &httpapi.TestMailResult{OK: false, ErrorDetail: err.Error()}, nil
	}
	return &httpapi.TestMailResult{OK: true, Summary: "Test email sent to " + strings.TrimSpace(in.To)}, nil
}

func (sc *setupController) CompleteQuickSetup(ctx context.Context, in httpapi.QuickSetupInput) (*ent.User, *account.NewSession, error) {
	if sc.completed.Load() {
		return nil, nil, httpapi.ErrSetupAlreadyDone
	}
	key := account.ClientKey(in.Meta.IP)
	if wait, blocked := sc.tokenLimit.Take(key); blocked {
		return nil, nil, &httpapi.SetupRateLimitError{RetryAfter: wait}
	}
	ok, err := secrets.VerifySetupToken(sc.paths.DataDir, in.Token)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, httpapi.ErrSetupInvalidToken
	}

	changes, err := validateSiteFields(in.SiteName, in.SiteURL, "")
	if err != nil {
		return nil, nil, err
	}
	if err := account.ValidateNewAccount(in.Email, in.Nickname, in.Password); err != nil {
		return nil, nil, err
	}

	sc.mu.Lock()
	if sc.completed.Load() {
		sc.mu.Unlock()
		return nil, nil, httpapi.ErrSetupAlreadyDone
	}
	if sc.inProgress {
		sc.mu.Unlock()
		return nil, nil, httpapi.ErrSetupConflict
	}
	sc.inProgress = true
	sc.mu.Unlock()
	defer func() {
		sc.mu.Lock()
		sc.inProgress = false
		sc.mu.Unlock()
	}()

	cfg := sc.baseConfig
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	core, err := sc.openCoreWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	u, sess, err := core.Accounts.CompleteVerifiedSetup(ctx, in.Email, in.Nickname, in.Password, in.Meta)
	if err != nil {
		core.Close(context.Background())
		return nil, nil, err
	}
	doneCtx := context.WithoutCancel(ctx)
	if len(changes) > 0 {
		if _, err := core.Settings.Set(doneCtx, &u.ID, changes, nil); err != nil {
			sc.log.Warn("setup: could not save site settings", "err", err)
		}
	}
	if err := sc.finishTransition(core); err != nil {
		core.Close(context.Background())
		return nil, nil, err
	}
	sc.tokenLimit.Reset(key)
	return u, sess, nil
}

func (sc *setupController) ConnectExisting(ctx context.Context, in httpapi.ConnectExistingInput) (*httpapi.ConnectExistingResult, error) {
	cfg, err := sc.buildTargetConfig(in.Database, in.Storage, in.TrustedProxies)
	if err != nil {
		return nil, err
	}
	probe, perr := db.Probe(ctx, db.Options{Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: sc.paths.DataDir})
	if perr != nil || !probe.OK {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{
			Field:   "database",
			Message: cmp.Or(redactSecretErr(probe.ErrorDetail), "cannot connect to the target database"),
		}}}
	}
	if probe.State != db.ProbeSkinLoomInstalled {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{
			Field:   "database",
			Message: "target database does not contain an initialized SkinLoom installation",
		}}}
	}

	sc.mu.Lock()
	if sc.completed.Load() {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupAlreadyDone
	}
	if sc.inProgress {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupConflict
	}
	sc.inProgress = true
	sc.mu.Unlock()
	defer func() {
		sc.mu.Lock()
		sc.inProgress = false
		sc.mu.Unlock()
	}()

	core, err := sc.openCoreWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if needs, err := core.Accounts.NeedsSetup(ctx); err != nil || needs {
		core.Close(context.Background())
		return nil, cmp.Or(err, errors.New("target database is not initialized"))
	}
	if err := sc.finishTransition(core); err != nil {
		core.Close(context.Background())
		return nil, err
	}
	return &httpapi.ConnectExistingResult{RedirectTo: "/auth/login"}, nil
}

func (sc *setupController) CompleteWizard(ctx context.Context, in httpapi.CompleteWizardInput) (*ent.User, *account.NewSession, error) {
	cfg, err := sc.buildTargetConfig(in.Database, in.Storage, in.TrustedProxies)
	if err != nil {
		return nil, nil, err
	}
	changes, err := validateSiteFields(in.SiteName, in.SiteURL, in.Timezone)
	if err != nil {
		return nil, nil, err
	}
	if in.Mail != nil && in.Mail.Driver != "" {
		_, mailChanges, err := sc.validateMailInput(*in.Mail, "mail.")
		if err != nil {
			return nil, nil, err
		}
		for k, v := range mailChanges {
			changes[k] = v
		}
	}
	if err := account.ValidateNewAccount(in.Email, in.Nickname, in.Password); err != nil {
		return nil, nil, err
	}

	// Refuse reusing a suspected BS database before mutating anything.
	if probe, _ := db.Probe(ctx, db.Options{Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: sc.paths.DataDir}); probe.State == db.ProbeSuspectedBS {
		return nil, nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{
			Field:   "database",
			Message: "suspected Blessing Skin database detected; use a new empty database or run Blessing Skin migration",
		}}}
	}

	sc.mu.Lock()
	if sc.completed.Load() {
		sc.mu.Unlock()
		return nil, nil, httpapi.ErrSetupAlreadyDone
	}
	if sc.inProgress {
		sc.mu.Unlock()
		return nil, nil, httpapi.ErrSetupConflict
	}
	sc.inProgress = true
	sc.mu.Unlock()
	defer func() {
		sc.mu.Lock()
		sc.inProgress = false
		sc.mu.Unlock()
	}()

	core, err := sc.openCoreWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	u, sess, err := core.Accounts.CompleteVerifiedSetup(ctx, in.Email, in.Nickname, in.Password, in.ClientMeta)
	if err != nil {
		core.Close(context.Background())
		return nil, nil, err
	}
	doneCtx := context.WithoutCancel(ctx)
	if len(changes) > 0 {
		if _, err := core.Settings.Set(doneCtx, &u.ID, changes, nil); err != nil {
			sc.log.Warn("setup: could not save initial settings", "err", err)
		}
	}
	if err := sc.finishTransition(core); err != nil {
		core.Close(context.Background())
		return nil, nil, err
	}
	return u, sess, nil
}

func (sc *setupController) openCoreWithConfig(ctx context.Context, cfg config.Config) (*Core, error) {
	if err := os.MkdirAll(sc.paths.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := secrets.MarkSetupPending(sc.paths.DataDir); err != nil {
		return nil, err
	}
	if err := config.Write(sc.paths.ConfigPath, cfg, sc.envOverrides); err != nil {
		return nil, err
	}
	return OpenCoreWithOptions(ctx, sc.paths, CoreOptions{
		Broker:             sc.broker,
		Themes:             sc.themes,
		SettingsReg:        sc.settingsReg,
		LogCtl:             sc.logCtl,
		Log:                sc.log,
		SkipConfigTemplate: true,
	})
}

func (sc *setupController) finishTransition(core *Core) error {
	if _, err := secrets.Ensure(sc.paths.DataDir); err != nil {
		return err
	}
	_ = secrets.ClearSetupToken(sc.paths.DataDir)
	_ = secrets.ClearSetupPending(sc.paths.DataDir)
	_ = secrets.MarkSetupDone(sc.paths.DataDir)
	if err := sc.onTransition(core); err != nil {
		return err
	}
	sc.completed.Store(true)
	sc.mu.Lock()
	if len(sc.jobs) == 0 {
		clear(sc.sessions)
	}
	clear(sc.parsedEnvs)
	sc.cleanupUploadsLocked()
	sc.mu.Unlock()
	// Ensure LiveRefresh (hello version 0 in Setup Mode) always observes a distinct version.
	ver := max(core.Settings.Current().Version, int64(1))
	sc.broker.Publish(pubsub.TopicConfigChanged, ver)
	return nil
}

func (sc *setupController) cleanupUploadsLocked() {
	for id, u := range sc.uploads {
		if u.file != nil {
			_ = u.file.Close()
			u.file = nil
		}
		if u.partPath != "" {
			_ = os.Remove(u.partPath)
		}
		if u.finalPath != "" {
			_ = os.Remove(u.finalPath)
		}
		delete(sc.uploads, id)
	}
	_ = os.RemoveAll(filepath.Join(sc.paths.DataDir, "import", "uploads"))
}

// --- Chunked Uploads & Archive Handling ---

func (sc *setupController) CreateUpload(_ context.Context, in httpapi.CreateUploadInput) (*httpapi.CreateUploadResult, error) {
	if sc.completed.Load() {
		return nil, httpapi.ErrSetupAlreadyDone
	}
	if in.TotalSize <= 0 || in.TotalSize > sc.maxUploadBytes {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{
			Field:   "totalSize",
			Message: fmt.Sprintf("file size must be between 1 byte and %d MiB", sc.maxUploadBytes>>20),
		}}}
	}
	dir := filepath.Join(sc.paths.DataDir, "import", "uploads")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if err := fsperm.Private(dir); err != nil {
		return nil, err
	}
	id := account.NewToken()
	partPath := filepath.Join(dir, id+".part")
	finalPath := filepath.Join(dir, id+".archive")
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := fsperm.Private(partPath); err != nil {
		_ = f.Close()
		_ = os.Remove(partPath)
		return nil, err
	}
	sc.mu.Lock()
	// Close and remove any earlier upload for the same purpose to avoid leaked descriptors/disk.
	for oldID, old := range sc.uploads {
		if old.purpose == in.Purpose {
			if old.file != nil {
				_ = old.file.Close()
				old.file = nil
			}
			_ = os.Remove(old.partPath)
			_ = os.Remove(old.finalPath)
			delete(sc.uploads, oldID)
		}
	}
	sc.uploads[id] = &uploadEntry{
		id:        id,
		purpose:   in.Purpose,
		filename:  filepath.Base(in.Filename),
		totalSize: in.TotalSize,
		partPath:  partPath,
		finalPath: finalPath,
		file:      f,
	}
	sc.mu.Unlock()
	return &httpapi.CreateUploadResult{
		UploadID:       id,
		ChunkSize:      setupChunkSize,
		MaxUploadBytes: sc.maxUploadBytes,
	}, nil
}

func (sc *setupController) PutUploadChunk(_ context.Context, uploadID string, offset int64, body io.Reader) (*httpapi.PutUploadChunkResult, error) {
	sc.mu.Lock()
	u, ok := sc.uploads[uploadID]
	if !ok {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupNotFound
	}
	if u.complete {
		rec := u.received
		sc.mu.Unlock()
		return &httpapi.PutUploadChunkResult{Received: rec, Complete: true}, nil
	}
	if offset != u.received {
		sc.mu.Unlock()
		return nil, fmt.Errorf("unexpected chunk offset %d, want %d", offset, u.received)
	}
	sc.mu.Unlock()

	data, err := io.ReadAll(io.LimitReader(body, maxChunkBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxChunkBodyBytes {
		return nil, errors.New("chunk exceeds 9 MiB limit")
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()
	if u.complete {
		return &httpapi.PutUploadChunkResult{Received: u.received, Complete: true}, nil
	}
	if offset != u.received {
		return nil, fmt.Errorf("concurrent chunk conflict at offset %d", offset)
	}
	if u.received+int64(len(data)) > u.totalSize {
		return nil, errors.New("upload exceeds declared totalSize")
	}
	if _, err := u.file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := u.file.Write(data); err != nil {
		return nil, err
	}
	u.received += int64(len(data))
	if u.received == u.totalSize {
		if err := u.file.Sync(); err != nil {
			return nil, err
		}
		if err := u.file.Close(); err != nil {
			return nil, err
		}
		u.file = nil
		if err := os.Rename(u.partPath, u.finalPath); err != nil {
			return nil, err
		}
		u.complete = true
	}
	return &httpapi.PutUploadChunkResult{Received: u.received, Complete: u.complete}, nil
}

func (sc *setupController) resolveUploadedPath(uploadID, purpose string) (string, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	u, ok := sc.uploads[uploadID]
	if !ok {
		return "", httpapi.ErrSetupNotFound
	}
	if purpose != "" && u.purpose != purpose {
		return "", &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "uploadId", Message: "upload purpose mismatch"}}}
	}
	if !u.complete {
		return "", &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "uploadId", Message: "upload is not complete yet"}}}
	}
	return u.finalPath, nil
}

// --- Blessing Skin Migration ---

func (sc *setupController) ParseBSEnv(_ context.Context, in httpapi.ParseBSEnvInput) (*httpapi.ParseBSEnvResult, error) {
	var (
		src  bs.Source
		info bs.EnvInfo
		err  error
	)
	switch {
	case strings.TrimSpace(in.Content) != "":
		src, info, err = bs.FromEnvBytes([]byte(in.Content))
	case strings.TrimSpace(in.Path) != "":
		src, info, err = bs.FromEnvWithInfo(strings.TrimSpace(in.Path))
	default:
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "content", Message: "provide .env content or path"}}}
	}
	if err != nil {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "content", Message: err.Error()}}}
	}
	envID := account.NewToken()
	sc.mu.Lock()
	if len(sc.parsedEnvs) >= 32 {
		clear(sc.parsedEnvs)
	}
	sc.parsedEnvs[envID] = src
	sc.mu.Unlock()
	return &httpapi.ParseBSEnvResult{EnvID: envID, Env: info}, nil
}

func (sc *setupController) TestBSSource(ctx context.Context, in httpapi.BSSourceInput) (*httpapi.TestBSSourceResult, error) {
	if in.SourceType == "bridge" {
		client, err := newBridgeClientFromInput(in)
		if err != nil {
			return nil, err
		}
		m, err := client.Manifest(ctx)
		if err != nil {
			return &httpapi.TestBSSourceResult{OK: false, ErrorDetail: redactSecretErr(err.Error())}, nil
		}
		res := &httpapi.TestBSSourceResult{OK: true, Counts: m.Counts}
		var warnings []string
		if !m.WriteFrozen {
			warnings = append(warnings, "Write-freeze is not enabled on Blessing Skin; enable it in the bridge plugin before running the final cutover.")
		}
		if strings.ToLower(m.FSDriver) == "s3" {
			warnings = append(warnings, "Blessing Skin uses S3 storage; transfer speeds depend on the bridge host.")
		}
		res.Warning = strings.Join(warnings, " ")
		return res, nil
	}
	src, err := sc.buildBSSource(in)
	if err != nil {
		return nil, err
	}
	sum, err := bs.TestSource(ctx, src)
	if err != nil {
		return &httpapi.TestBSSourceResult{OK: false, ErrorDetail: redactSecretErr(err.Error())}, nil
	}
	res := &httpapi.TestBSSourceResult{OK: true, Counts: sum}
	if in.TextureSource == "dir" && src.TexturesDir != "" && !sum.TextureDirStats.Exists {
		res.Warning = "Texture directory does not exist on this server: " + src.TexturesDir
	}
	return res, nil
}

func (sc *setupController) StartBSDryRun(_ context.Context, in httpapi.BSDryRunInput) (*httpapi.StartJobResult, error) {
	var (
		src           bs.Source
		bridgeClient  *bs.BridgeClient
		uploadArchive string
		err           error
	)
	if in.Source.SourceType == "bridge" {
		bridgeClient, err = newBridgeClientFromInput(in.Source)
		if err != nil {
			return nil, err
		}
	} else {
		src, err = sc.buildBSSource(in.Source)
		if err != nil {
			return nil, err
		}
		if in.Source.TextureSource == "upload" && in.Source.UploadID != "" {
			uploadArchive, err = sc.resolveUploadedPath(in.Source.UploadID, "bs_textures")
			if err != nil {
				return nil, err
			}
		}
	}
	job := sc.newJob("bs_dry_run")
	go sc.runBSJob(job, src, bridgeClient, in.Source.Timezone, uploadArchive, in.Source.TextureSource == "none", true, nil, config.Config{}, "", nil)
	return &httpapi.StartJobResult{JobID: job.id}, nil
}

func (sc *setupController) StartBSImport(_ context.Context, in httpapi.BSImportInput) (*httpapi.StartJobResult, error) {
	cfg, err := sc.buildTargetConfig(in.Database, in.Storage, in.TrustedProxies)
	if err != nil {
		return nil, err
	}
	var (
		src           bs.Source
		bridgeClient  *bs.BridgeClient
		uploadArchive string
	)
	if in.Source.SourceType == "bridge" {
		bridgeClient, err = newBridgeClientFromInput(in.Source)
		if err != nil {
			return nil, err
		}
	} else {
		src, err = sc.buildBSSource(in.Source)
		if err != nil {
			return nil, err
		}
		if in.Source.TextureSource == "upload" && in.Source.UploadID != "" {
			uploadArchive, err = sc.resolveUploadedPath(in.Source.UploadID, "bs_textures")
			if err != nil {
				return nil, err
			}
		}
	}
	var siteURL string
	if strings.TrimSpace(in.SiteURL) != "" {
		changes, err := validateSiteFields("", in.SiteURL, "")
		if err != nil {
			return nil, err
		}
		if raw, ok := changes["site.url"]; ok {
			_ = json.Unmarshal(raw, &siteURL)
		}
	}
	if in.FallbackAdmin != nil {
		if err := account.ValidateNewAccount(in.FallbackAdmin.Email, in.FallbackAdmin.Nickname, in.FallbackAdmin.Password); err != nil {
			return nil, err
		}
	}

	sc.mu.Lock()
	if sc.completed.Load() {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupAlreadyDone
	}
	if sc.inProgress {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupConflict
	}
	sc.inProgress = true
	sc.mu.Unlock()

	job := sc.newJob("bs_import")
	go sc.runBSJob(job, src, bridgeClient, in.Source.Timezone, uploadArchive, in.Source.TextureSource == "none", false, &cfg, cfg, siteURL, in.FallbackAdmin)
	return &httpapi.StartJobResult{JobID: job.id}, nil
}

func newBridgeClientFromInput(in httpapi.BSSourceInput) (*bs.BridgeClient, error) {
	client, err := bs.NewBridgeClient(bs.BridgeConfig{
		URL:                in.BridgeURL,
		Token:              in.BridgeToken,
		AllowInsecureLocal: in.AllowInsecureLocal,
		Timezone:           in.Timezone,
	})
	if err != nil {
		field := "bridgeUrl"
		if strings.Contains(strings.ToLower(err.Error()), "token") {
			field = "bridgeToken"
		}
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{
			Field:   field,
			Message: err.Error(),
		}}}
	}
	return client, nil
}

func (sc *setupController) runBSJob(
	job *setupJob,
	src bs.Source,
	bridgeClient *bs.BridgeClient,
	bridgeTZ string,
	uploadArchive string,
	skipFiles bool,
	dryRun bool,
	targetCfg *config.Config,
	cfg config.Config,
	siteURL string,
	fallbackAdmin *httpapi.FallbackAdminInput,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if !dryRun {
		defer func() {
			sc.mu.Lock()
			sc.inProgress = false
			sc.mu.Unlock()
		}()
	}

	var (
		reader  bs.SourceReader
		fetcher bs.FileFetcher
	)
	if bridgeClient != nil {
		job.update("reading", 0, 0, "Connecting to Blessing Skin bridge plugin")
		m, err := bridgeClient.Manifest(ctx)
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		src, err = bridgeClient.ResolveSource(m, bridgeTZ)
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		var cacheDir string
		if !dryRun {
			var cerr error
			cacheDir, cerr = os.MkdirTemp("", "skinloom-bridge-*")
			if cerr != nil {
				sc.failJob(job, nil, cerr)
				return
			}
			defer os.RemoveAll(cacheDir)
		}
		reader = &bs.BridgeSourceReader{Client: bridgeClient, Manifest: m}
		fetcher = &bs.BridgeFileFetcher{Client: bridgeClient, CacheDir: cacheDir, FastInspect: dryRun}
	}

	if uploadArchive != "" {
		job.update("extracting", 0, 0, "Extracting uploaded texture archive")
		extractDir, err := os.MkdirTemp(filepath.Join(sc.paths.DataDir, "import"), "staging-*")
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		defer os.RemoveAll(extractDir)
		if err := extractTextureArchive(ctx, uploadArchive, extractDir); err != nil {
			sc.failJob(job, nil, fmt.Errorf("extract texture archive: %w", err))
			return
		}
		src.TexturesDir = bs.DetectTexturesRoot(extractDir)
	}
	if skipFiles {
		src.TexturesDir = ""
	}

	var (
		targetDB *db.DB
		store    storage.Store
		storeDir string
		err      error
	)
	if dryRun {
		// Run dry-run against a disposable SQLite file in temp dir so we never touch data/skinloom.db.
		tmpDir, err := os.MkdirTemp("", "skinloom-dryrun-*")
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		defer os.RemoveAll(tmpDir)
		targetDB, err = db.Open(ctx, db.Options{Driver: "sqlite", DSN: "dryrun.db", DataDir: tmpDir, MaxOpenConns: 2})
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		defer targetDB.Close()
		if err := migrate.Up(ctx, targetDB, tmpDir, sc.log.With("component", "dryrun-migrate")); err != nil {
			sc.failJob(job, nil, err)
			return
		}
		store, err = storage.NewLocal(filepath.Join(tmpDir, "textures"))
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
	} else {
		if err := os.MkdirAll(sc.paths.DataDir, 0o750); err != nil {
			sc.failJob(job, nil, err)
			return
		}
		if err := secrets.MarkSetupPending(sc.paths.DataDir); err != nil {
			sc.failJob(job, nil, err)
			return
		}
		if err := config.Write(sc.paths.ConfigPath, *targetCfg, sc.envOverrides); err != nil {
			sc.failJob(job, nil, err)
			return
		}
		targetDB, err = db.Open(ctx, db.Options{Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: sc.paths.DataDir, MaxOpenConns: 4})
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		defer targetDB.Close()
		if err := migrate.Up(ctx, targetDB, sc.paths.DataDir, sc.log.With("component", "migrate")); err != nil {
			sc.failJob(job, nil, err)
			return
		}
		store, err = openStore(ctx, cfg.Storage, sc.paths.DataDir)
		if err != nil {
			sc.failJob(job, nil, err)
			return
		}
		if cfg.Storage.Driver != "s3" {
			storeDir = cfg.Storage.Dir(sc.paths.DataDir)
		}
	}

	locales := make([]string, 0, len(httpapi.Locales))
	for _, l := range httpapi.Locales {
		locales = append(locales, l.Code)
	}
	report, err := bs.Run(ctx, src, bs.Target{
		DB:       targetDB,
		Store:    store,
		StoreDir: storeDir,
		Locales:  locales,
		Settings: sc.settingsReg,
	}, bs.Options{
		DryRun:  dryRun,
		Reader:  reader,
		Fetcher: fetcher,
		StageProgress: func(stage string, done, total int) {
			job.update(stage, done, total, "")
		},
	})
	if err != nil {
		sc.failJob(job, report, err)
		return
	}
	if report != nil && report.Blocked() {
		sc.failJob(job, report, fmt.Errorf("import refused: %d BLOCKING entries", report.Count(bs.Blocking)))
		return
	}
	if dryRun {
		sc.succeedJob(job, report, "")
		return
	}

	// Close the importer pool before OpenCoreWithOptions opens the main runtime pool.
	_ = targetDB.Close()

	core, err := OpenCoreWithOptions(ctx, sc.paths, CoreOptions{
		Broker:             sc.broker,
		Themes:             sc.themes,
		SettingsReg:        sc.settingsReg,
		LogCtl:             sc.logCtl,
		Log:                sc.log,
		SkipConfigTemplate: true,
	})
	if err != nil {
		sc.failJob(job, report, err)
		return
	}
	if siteURL != "" {
		_, _ = core.Settings.Set(ctx, nil, map[string]json.RawMessage{"site.url": mustJSON(siteURL)}, nil)
	}
	if report != nil && report.Superadmins == 0 {
		if fallbackAdmin == nil || fallbackAdmin.Email == "" {
			core.Close(context.Background())
			sc.failJob(job, report, errors.New("no superadmins found in source: provide a fallback administrator account"))
			return
		}
		if _, err := core.Accounts.EnsureFallbackSuperadmin(ctx, fallbackAdmin.Email, fallbackAdmin.Nickname, fallbackAdmin.Password); err != nil {
			core.Close(context.Background())
			sc.failJob(job, report, err)
			return
		}
	}
	if err := sc.finishTransition(core); err != nil {
		core.Close(context.Background())
		sc.failJob(job, report, err)
		return
	}
	sc.succeedJob(job, report, "/auth/login")
}

// --- Backup Restore ---

func (sc *setupController) InspectRestore(_ context.Context, in httpapi.InspectRestoreInput) (*httpapi.InspectRestoreResult, error) {
	path, err := sc.resolveRestorePath(in.UploadID, in.Path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "path", Message: err.Error()}}}
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "uploadId", Message: "not a valid gzip SkinLoom backup archive"}}}
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != "manifest.json" {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "uploadId", Message: "not a SkinLoom backup: manifest.json missing"}}}
	}
	var m backup.Manifest
	if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
		return nil, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "uploadId", Message: "invalid backup manifest: " + err.Error()}}}
	}
	res := &httpapi.InspectRestoreResult{
		Manifest: httpapi.BackupManifestView{
			Format:       m.Format,
			Version:      m.Version,
			CreatedAt:    m.CreatedAt,
			Database:     m.Database,
			Migration:    m.Migration,
			HasDatabase:  m.HasDatabase,
			Textures:     m.Textures,
			TextureBytes: m.TextureBytes,
		},
		Warnings: []string{},
	}
	if m.Format != backup.Format {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Unsupported backup format %d (expected %d)", m.Format, backup.Format))
	}
	if !m.HasDatabase {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Backup does not include the %s database dump; ensure your external database is already restored before running restore", m.Database))
	}
	// Read optional config.toml entry (next entry if present) without leaking secrets.
	if hdr2, err := tr.Next(); err == nil && hdr2.Name == "config.toml" && hdr2.Size <= 1<<20 {
		rawCfg, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err == nil {
			tmpCfg, err := os.CreateTemp("", "skinloom-inspect-*.toml")
			if err == nil {
				_, _ = tmpCfg.Write(rawCfg)
				_ = tmpCfg.Close()
				if parsed, err := config.Load(tmpCfg.Name()); err == nil {
					res.HasConfig = true
					res.ConfigDialect = parsed.Database.Driver
					res.ConfigStorage = parsed.Storage.Driver
				}
				_ = os.Remove(tmpCfg.Name())
			}
		}
	}
	return res, nil
}

func (sc *setupController) StartRestore(ctx context.Context, in httpapi.ExecuteRestoreInput) (*httpapi.StartJobResult, error) {
	path, err := sc.resolveRestorePath(in.UploadID, in.Path)
	if err != nil {
		return nil, err
	}
	cfg, err := sc.buildTargetConfig(in.Database, in.Storage, in.TrustedProxies)
	if err != nil {
		return nil, err
	}

	sc.mu.Lock()
	if sc.completed.Load() {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupAlreadyDone
	}
	if sc.inProgress {
		sc.mu.Unlock()
		return nil, httpapi.ErrSetupConflict
	}
	sc.inProgress = true
	sc.mu.Unlock()

	job := sc.newJob("restore")
	go sc.runRestoreJob(job, path, cfg, in.ApplyArchiveConfig)
	return &httpapi.StartJobResult{JobID: job.id}, nil
}

func (sc *setupController) runRestoreJob(job *setupJob, archivePath string, cfg config.Config, applyArchiveConfig bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	defer func() {
		sc.mu.Lock()
		sc.inProgress = false
		sc.mu.Unlock()
	}()

	job.update("restoring", 0, 0, "Unpacking backup archive")
	f, err := os.Open(archivePath)
	if err != nil {
		sc.failJob(job, nil, err)
		return
	}
	defer f.Close()

	if err := os.MkdirAll(sc.paths.DataDir, 0o750); err != nil {
		sc.failJob(job, nil, err)
		return
	}
	if err := secrets.MarkSetupPending(sc.paths.DataDir); err != nil {
		sc.failJob(job, nil, err)
		return
	}
	if !applyArchiveConfig {
		if err := config.Write(sc.paths.ConfigPath, cfg, sc.envOverrides); err != nil {
			sc.failJob(job, nil, err)
			return
		}
	}

	res, err := backup.Restore(ctx, f, backup.Target{
		DataDir:    sc.paths.DataDir,
		ConfigPath: sc.paths.ConfigPath,
		Force:      true,
		OpenStore: func(ctx context.Context, archiveCfg config.Config) (storage.Store, error) {
			st := cfg.Storage
			if applyArchiveConfig {
				st = archiveCfg.Storage
			}
			return openStore(ctx, st, sc.paths.DataDir)
		},
		OpenPluginPackageStore: func(ctx context.Context, archiveCfg config.Config) (storage.Store, error) {
			st := cfg.Storage
			if applyArchiveConfig {
				st = archiveCfg.Storage
			}
			return openPluginPackageStore(ctx, st, sc.paths.DataDir)
		},
	})
	if err != nil {
		sc.failJob(job, nil, err)
		return
	}

	if !applyArchiveConfig {
		if res.Manifest.HasDatabase && !db.IsSQLite(cfg.Database.Driver) {
			sc.failJob(job, nil, errors.New("backup archive contains a SQLite database; select SQLite in Step 2 to restore it"))
			return
		}
		if res.Database != "" && db.IsSQLite(cfg.Database.Driver) {
			wantPath := db.SQLitePath(cfg.Database.DSN, sc.paths.DataDir)
			if filepath.Clean(res.Database) != filepath.Clean(wantPath) {
				if err := os.MkdirAll(filepath.Dir(wantPath), 0o750); err != nil {
					sc.failJob(job, nil, err)
					return
				}
				if err := os.Rename(res.Database, wantPath); err != nil {
					sc.failJob(job, nil, err)
					return
				}
			}
		}
		if err := config.Write(sc.paths.ConfigPath, cfg, sc.envOverrides); err != nil {
			sc.failJob(job, nil, err)
			return
		}
	}

	job.update("migrating", 0, 0, "Opening database and running migrations")
	core, err := OpenCoreWithOptions(ctx, sc.paths, CoreOptions{
		Broker:             sc.broker,
		Themes:             sc.themes,
		SettingsReg:        sc.settingsReg,
		LogCtl:             sc.logCtl,
		Log:                sc.log,
		SkipConfigTemplate: true,
	})
	if err != nil {
		sc.failJob(job, nil, err)
		return
	}
	if needs, err := core.Accounts.NeedsSetup(ctx); err != nil || needs {
		core.Close(context.Background())
		sc.failJob(job, nil, cmp.Or(err, errors.New("restored target database has no initialized SkinLoom accounts; restore your SQL dump before running backup restore")))
		return
	}
	if err := sc.finishTransition(core); err != nil {
		core.Close(context.Background())
		sc.failJob(job, nil, err)
		return
	}
	sc.succeedJob(job, nil, "/auth/login")
}

func (sc *setupController) resolveRestorePath(uploadID, localPath string) (string, error) {
	if uploadID != "" {
		return sc.resolveUploadedPath(uploadID, "backup")
	}
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "uploadId", Message: "provide a backup archive upload or server path"}}}
	}
	return localPath, nil
}

func (sc *setupController) GetJob(_ context.Context, jobID string) (*httpapi.SetupJobView, error) {
	sc.mu.Lock()
	j, ok := sc.jobs[jobID]
	sc.mu.Unlock()
	if !ok {
		return nil, httpapi.ErrSetupNotFound
	}
	v := j.view()
	return &v, nil
}

func (sc *setupController) GetJobReportText(_ context.Context, jobID string) ([]byte, string, error) {
	sc.mu.Lock()
	j, ok := sc.jobs[jobID]
	sc.mu.Unlock()
	if !ok {
		return nil, "", httpapi.ErrSetupNotFound
	}
	j.mu.RLock()
	r := j.report
	j.mu.RUnlock()
	if r == nil {
		return nil, "", httpapi.ErrSetupNotFound
	}
	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		return nil, "", err
	}
	name := "bs-migration-report.txt"
	if r.DryRun {
		name = "bs-migration-dry-run-report.txt"
	}
	return buf.Bytes(), name, nil
}

func (sc *setupController) newJob(kind string) *setupJob {
	id := account.NewToken()
	j := &setupJob{
		id:        id,
		kind:      kind,
		status:    "running",
		stage:     "starting",
		updatedAt: time.Now(),
	}
	sc.mu.Lock()
	sc.jobs[id] = j
	sc.activeJob = id
	sc.mu.Unlock()
	return j
}

func (sc *setupController) failJob(j *setupJob, report *bs.Report, err error) {
	j.mu.Lock()
	j.status = "failed"
	j.report = report
	j.errText = redactSecretErr(err.Error())
	j.updatedAt = time.Now()
	j.mu.Unlock()
	sc.mu.Lock()
	if sc.activeJob == j.id {
		sc.activeJob = ""
	}
	sc.mu.Unlock()
}

func (sc *setupController) succeedJob(j *setupJob, report *bs.Report, redirectTo string) {
	j.mu.Lock()
	j.status = "succeeded"
	j.stage = "done"
	j.percent = 100
	j.report = report
	j.redirectTo = redirectTo
	j.updatedAt = time.Now()
	j.mu.Unlock()
	sc.mu.Lock()
	if sc.activeJob == j.id {
		sc.activeJob = ""
	}
	sc.mu.Unlock()
}

// --- Input Resolution & Validation Helpers ---

func (sc *setupController) buildTargetConfig(dbIn httpapi.DBConfigInput, stIn httpapi.StorageConfigInput, proxies []string) (config.Config, error) {
	cfg := sc.baseConfig
	dbCfg, err := sc.resolveDB(dbIn)
	if err != nil {
		return config.Config{}, err
	}
	stCfg, err := sc.resolveStorage(stIn)
	if err != nil {
		return config.Config{}, err
	}
	cfg.Database = dbCfg
	cfg.Storage = stCfg
	if !sc.envOverrides["server.trusted_proxies"] && proxies != nil {
		cleaned := make([]string, 0, len(proxies))
		for _, p := range proxies {
			if p = strings.TrimSpace(p); p != "" {
				cleaned = append(cleaned, p)
			}
		}
		if _, err := realip.New(cleaned); err != nil {
			return config.Config{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "trustedProxies", Message: err.Error()}}}
		}
		cfg.Server.TrustedProxies = cleaned
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "storage", Message: err.Error()}}}
	}
	return cfg, nil
}

func (sc *setupController) resolveDB(in httpapi.DBConfigInput) (config.Database, error) {
	if in.UseEnv || sc.envDBLocked() {
		return sc.baseConfig.Database, nil
	}
	out := sc.baseConfig.Database
	driver := strings.ToLower(strings.TrimSpace(in.Driver))
	if driver == "" {
		driver = cmp.Or(out.Driver, "sqlite")
	}
	switch driver {
	case "sqlite":
		out.Driver = "sqlite"
		out.DSN = cmp.Or(strings.TrimSpace(in.SQLitePath), "skinloom.db")
	case "mysql":
		out.Driver = "mysql"
		if strings.TrimSpace(in.RawDSN) != "" {
			out.DSN = strings.TrimSpace(in.RawDSN)
		} else {
			if strings.TrimSpace(in.Host) == "" || strings.TrimSpace(in.Database) == "" || strings.TrimSpace(in.Username) == "" {
				return config.Database{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "database.host", Message: "host, database and username are required for MySQL"}}}
			}
			out.DSN = config.BuildMySQLDSN(strings.TrimSpace(in.Host), in.Port, strings.TrimSpace(in.Database), strings.TrimSpace(in.Username), in.Password)
		}
	case "postgres":
		out.Driver = "postgres"
		if strings.TrimSpace(in.RawDSN) != "" {
			out.DSN = strings.TrimSpace(in.RawDSN)
		} else {
			if strings.TrimSpace(in.Host) == "" || strings.TrimSpace(in.Database) == "" || strings.TrimSpace(in.Username) == "" {
				return config.Database{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "database.host", Message: "host, database and username are required for PostgreSQL"}}}
			}
			out.DSN = config.BuildPostgresDSN(strings.TrimSpace(in.Host), in.Port, strings.TrimSpace(in.Database), strings.TrimSpace(in.Username), in.Password, strings.TrimSpace(in.SSLMode))
		}
	default:
		return config.Database{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "database.driver", Message: "driver must be sqlite, mysql or postgres"}}}
	}
	return out, nil
}

func (sc *setupController) resolveStorage(in httpapi.StorageConfigInput) (config.Storage, error) {
	if in.UseEnv || sc.envStorageLocked() {
		return sc.baseConfig.Storage, nil
	}
	out := config.Storage{
		Driver:    cmp.Or(strings.ToLower(strings.TrimSpace(in.Driver)), sc.baseConfig.Storage.Driver, "local"),
		Path:      cmp.Or(strings.TrimSpace(in.Path), "textures"),
		Endpoint:  strings.TrimSpace(in.Endpoint),
		Bucket:    strings.TrimSpace(in.Bucket),
		Region:    strings.TrimSpace(in.Region),
		AccessKey: strings.TrimSpace(in.AccessKey),
		SecretKey: in.SecretKey,
		Prefix:    strings.TrimSpace(in.Prefix),
		Insecure:  in.Insecure,
		PathStyle: in.PathStyle,
		PublicURL: strings.TrimRight(strings.TrimSpace(in.PublicURL), "/"),
	}
	if out.Driver == "s3" {
		if out.Endpoint == "" || out.Bucket == "" || out.AccessKey == "" || out.SecretKey == "" {
			return config.Storage{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "storage.endpoint", Message: "endpoint, bucket, accessKey and secretKey are required for S3"}}}
		}
		if strings.Contains(out.Endpoint, "://") {
			return config.Storage{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "storage.endpoint", Message: "endpoint must be host[:port] without http:// or https://"}}}
		}
		if out.PublicURL != "" && !strings.HasPrefix(out.PublicURL, "http://") && !strings.HasPrefix(out.PublicURL, "https://") {
			return config.Storage{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "storage.publicUrl", Message: "publicUrl must start with http:// or https://"}}}
		}
	}
	return out, nil
}

func (sc *setupController) validateMailInput(in httpapi.MailConfigInput, fieldPrefix string) (mail.Config, map[string]json.RawMessage, error) {
	driver := cmp.Or(strings.ToLower(strings.TrimSpace(in.Driver)), mail.DriverNone)
	port := in.Port
	if port == 0 {
		port = 465
	}
	enc := cmp.Or(strings.ToLower(strings.TrimSpace(in.Encryption)), "tls")
	candidates := map[string]any{
		"mail.driver":          driver,
		"mail.from":            strings.TrimSpace(in.From),
		"mail.smtp.host":       strings.TrimSpace(in.Host),
		"mail.smtp.port":       port,
		"mail.smtp.username":   strings.TrimSpace(in.Username),
		"mail.smtp.password":   in.Password,
		"mail.smtp.encryption": enc,
	}
	changes := map[string]json.RawMessage{}
	var fields []httpapi.SetupFieldError
	for k, v := range candidates {
		def, ok := sc.settingsReg.Def(k)
		if !ok {
			continue
		}
		raw, _ := json.Marshal(v)
		norm, err := def.Canonical(raw)
		if err != nil {
			fields = append(fields, httpapi.SetupFieldError{Field: fieldPrefix + strings.TrimPrefix(k, "mail."), Message: err.Error()})
			continue
		}
		changes[k] = norm
	}
	if driver == mail.DriverSMTP {
		if strings.TrimSpace(in.From) == "" {
			fields = append(fields, httpapi.SetupFieldError{Field: fieldPrefix + "from", Message: "sender address is required for SMTP"})
		}
		if strings.TrimSpace(in.Host) == "" {
			fields = append(fields, httpapi.SetupFieldError{Field: fieldPrefix + "host", Message: "SMTP host is required"})
		}
	}
	if len(fields) > 0 {
		return mail.Config{}, nil, &httpapi.SetupValidationError{Fields: fields}
	}
	return mail.Config{
		Driver:     driver,
		From:       strings.TrimSpace(in.From),
		Host:       strings.TrimSpace(in.Host),
		Port:       port,
		Username:   strings.TrimSpace(in.Username),
		Password:   in.Password,
		Encryption: enc,
	}, changes, nil
}

func (sc *setupController) buildBSSource(in httpapi.BSSourceInput) (bs.Source, error) {
	var src bs.Source
	if in.EnvID != "" {
		sc.mu.Lock()
		saved, ok := sc.parsedEnvs[in.EnvID]
		sc.mu.Unlock()
		if ok {
			src = saved
		}
	}
	if in.Prefix != "" {
		src.Prefix = strings.TrimSpace(in.Prefix)
	}
	if in.PasswordMethod != "" {
		src.PasswordMethod = strings.TrimSpace(in.PasswordMethod)
	}
	if in.Salt != "" {
		src.Salt = in.Salt
	}
	if len(in.TransitionMethods) > 0 {
		salts := strings.Split(src.Salt, ",")
		src.Transition = nil
		for i, m := range in.TransitionMethods {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			salt := ""
			if i < len(salts) {
				salt = strings.TrimSpace(salts[i])
			}
			src.Transition = append(src.Transition, bs.Method{Name: m, Salt: salt})
		}
	}
	if in.Driver != "" {
		src.Driver = strings.ToLower(strings.TrimSpace(in.Driver))
	}
	switch src.Driver {
	case "mysql":
		if strings.TrimSpace(in.RawDSN) != "" {
			src.DSN = strings.TrimSpace(in.RawDSN)
		} else if in.Host != "" || in.Database != "" || in.Username != "" || in.Password != "" || in.Socket != "" {
			host := cmp.Or(strings.TrimSpace(in.Host), "127.0.0.1")
			port := in.Port
			if port <= 0 {
				port = 3306
			}
			cfg, err := mysql.ParseDSN(src.DSN)
			if err != nil || cfg == nil {
				cfg = mysql.NewConfig()
			}
			if in.Username != "" {
				cfg.User = strings.TrimSpace(in.Username)
			}
			if in.Password != "" {
				cfg.Passwd = in.Password
			}
			if in.Database != "" {
				cfg.DBName = strings.TrimSpace(in.Database)
			}
			if in.Socket != "" {
				cfg.Net, cfg.Addr = "unix", strings.TrimSpace(in.Socket)
			} else if in.Host != "" || cfg.Addr == "" {
				cfg.Net, cfg.Addr = "tcp", fmt.Sprintf("%s:%d", host, port)
			}
			src.DSN = cfg.FormatDSN()
		}
	case "postgres":
		if strings.TrimSpace(in.RawDSN) != "" {
			src.DSN = strings.TrimSpace(in.RawDSN)
		} else if in.Host != "" || in.Database != "" || in.Username != "" || in.Password != "" {
			host := cmp.Or(strings.TrimSpace(in.Host), "127.0.0.1")
			port := in.Port
			if port <= 0 {
				port = 5432
			}
			u, _ := url.Parse(src.DSN)
			if u == nil || u.Scheme == "" {
				u = &url.URL{Scheme: "postgres"}
			}
			if in.Host != "" || u.Host == "" {
				u.Host = fmt.Sprintf("%s:%d", host, port)
			}
			if in.Database != "" {
				u.Path = "/" + strings.TrimPrefix(strings.TrimSpace(in.Database), "/")
			}
			var prevUser, prevPass string
			if u.User != nil {
				prevUser = u.User.Username()
				prevPass, _ = u.User.Password()
			}
			user := cmp.Or(strings.TrimSpace(in.Username), prevUser)
			pass := cmp.Or(in.Password, prevPass)
			if user != "" {
				u.User = url.UserPassword(user, pass)
			}
			src.DSN = u.String()
		}
	case "sqlite":
		if strings.TrimSpace(in.RawDSN) != "" {
			src.DSN = strings.TrimSpace(in.RawDSN)
		} else if strings.TrimSpace(in.Database) != "" {
			src.DSN = strings.TrimSpace(in.Database)
		}
	default:
		if src.Driver == "" && in.SourceType != "bridge" {
			return bs.Source{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "source.driver", Message: "select mysql, postgres or sqlite"}}}
		}
	}
	switch in.TextureSource {
	case "none":
		src.TexturesDir = ""
	case "dir":
		if strings.TrimSpace(in.TexturesDir) != "" {
			src.TexturesDir = strings.TrimSpace(in.TexturesDir)
		}
	}
	tz := cmp.Or(strings.TrimSpace(in.Timezone), "Asia/Shanghai")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return bs.Source{}, &httpapi.SetupValidationError{Fields: []httpapi.SetupFieldError{{Field: "source.timezone", Message: "invalid IANA time zone"}}}
	}
	src.Location = loc
	return src, nil
}

func validateSiteFields(name, siteURL, timezone string) (map[string]json.RawMessage, error) {
	changes := map[string]json.RawMessage{}
	var fields []httpapi.SetupFieldError
	if name = strings.TrimSpace(name); name != "" {
		if utf8.RuneCountInString(name) > 64 {
			fields = append(fields, httpapi.SetupFieldError{Field: "siteName", Message: "must be at most 64 characters"})
		} else {
			changes["site.name"] = mustJSON(map[string]string{"zh-CN": name, "en": name})
		}
	}
	if siteURL = strings.TrimRight(strings.TrimSpace(siteURL), "/"); siteURL != "" {
		u, err := url.Parse(siteURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			fields = append(fields, httpapi.SetupFieldError{Field: "siteUrl", Message: "must be an http(s) URL such as https://skins.example.com"})
		} else {
			changes["site.url"] = mustJSON(siteURL)
		}
	}
	if timezone = strings.TrimSpace(timezone); timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			fields = append(fields, httpapi.SetupFieldError{Field: "timezone", Message: "not a valid time zone such as Asia/Shanghai or UTC"})
		} else {
			changes["site.timezone"] = mustJSON(timezone)
		}
	}
	if len(fields) > 0 {
		return nil, &httpapi.SetupValidationError{Fields: fields}
	}
	return changes, nil
}

// --- Safe Archive Extraction (Zip-Slip & Zip-Bomb Guards) ---

func extractTextureArchive(ctx context.Context, archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("read archive header: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if magic[0] == 'P' && magic[1] == 'K' {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		return extractZipTextures(ctx, f, info.Size(), destDir)
	}
	return extractTarGzTextures(ctx, f, destDir)
}

func extractZipTextures(ctx context.Context, r io.ReaderAt, size int64, destDir string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	if len(zr.File) > maxArchiveEntries {
		return fmt.Errorf("archive has too many entries (%d > %d)", len(zr.File), maxArchiveEntries)
	}
	var totalBytes int64
	for _, zf := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		mode := zf.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("archive contains forbidden symlink %q", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			continue
		}
		if !mode.IsRegular() && mode != 0 {
			return fmt.Errorf("archive contains non-regular entry %q", zf.Name)
		}
		if zf.UncompressedSize64 > uint64(maxArchiveFile) {
			return fmt.Errorf("entry %q exceeds 64 MiB limit", zf.Name)
		}
		target, ok := safeArchiveJoin(destDir, zf.Name)
		if !ok {
			return fmt.Errorf("unsafe archive path %q", zf.Name)
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		n, werr := writeExtractedFile(target, rc, &totalBytes)
		_ = rc.Close()
		if werr != nil {
			return fmt.Errorf("extract %q: %w", zf.Name, werr)
		}
		_ = n
	}
	return nil
}

func extractTarGzTextures(ctx context.Context, r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var entries int
	var totalBytes int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("archive exceeds %d entries limit", maxArchiveEntries)
		}
		switch hdr.Typeflag {
		case tar.TypeDir, tar.TypeXHeader, tar.TypeXGlobalHeader:
			continue
		case tar.TypeReg:
		default:
			return fmt.Errorf("archive contains forbidden entry type %q (%v)", hdr.Name, hdr.Typeflag)
		}
		if hdr.Size < 0 || hdr.Size > maxArchiveFile {
			return fmt.Errorf("entry %q exceeds 64 MiB limit", hdr.Name)
		}
		target, ok := safeArchiveJoin(destDir, hdr.Name)
		if !ok {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		if _, err := writeExtractedFile(target, tr, &totalBytes); err != nil {
			return fmt.Errorf("extract %q: %w", hdr.Name, err)
		}
	}
}

func writeExtractedFile(target string, r io.Reader, totalBytes *int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	limited := io.LimitReader(r, maxArchiveFile+1)
	n, err := io.Copy(f, limited)
	if err != nil {
		return n, err
	}
	if n > maxArchiveFile {
		return n, errors.New("file exceeds 64 MiB uncompressed limit")
	}
	*totalBytes += n
	if *totalBytes > maxArchiveTotal {
		return n, errors.New("archive exceeds 4 GiB uncompressed limit")
	}
	return n, nil
}

func safeArchiveJoin(root, entry string) (string, bool) {
	if strings.ContainsAny(entry, "\x00\\:") {
		return "", false
	}
	if strings.HasPrefix(entry, "/") {
		return "", false
	}
	clean := path.Clean(entry)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// --- Sanitization & Display Helpers ---

func summarizeDB(c config.Database) httpapi.DBSummary {
	s := httpapi.DBSummary{Driver: cmp.Or(c.Driver, "sqlite")}
	switch s.Driver {
	case "sqlite":
		s.SQLitePath = cmp.Or(c.DSN, "skinloom.db")
	case "mysql":
		if cfg, err := mysql.ParseDSN(c.DSN); err == nil {
			host, portStr, _ := strings.Cut(cfg.Addr, ":")
			s.Host = host
			s.Port, _ = strconv.Atoi(portStr)
			s.Database = cfg.DBName
		}
	case "postgres":
		if u, err := url.Parse(c.DSN); err == nil {
			s.Host = u.Hostname()
			s.Port, _ = strconv.Atoi(u.Port())
			s.Database = strings.TrimPrefix(u.Path, "/")
		}
	}
	return s
}

func summarizeStorage(c config.Storage) httpapi.StorageSummary {
	return httpapi.StorageSummary{
		Driver:    cmp.Or(c.Driver, "local"),
		Path:      c.Path,
		Endpoint:  c.Endpoint,
		Bucket:    c.Bucket,
		Region:    c.Region,
		Prefix:    c.Prefix,
		PathStyle: c.PathStyle,
		Insecure:  c.Insecure,
		PublicURL: c.PublicURL,
	}
}

func maskDSN(driver, dsn string) string {
	switch driver {
	case "sqlite":
		return cmp.Or(dsn, "skinloom.db")
	case "mysql":
		if cfg, err := mysql.ParseDSN(dsn); err == nil {
			return fmt.Sprintf("mysql://%s/%s", cfg.Addr, cfg.DBName)
		}
	case "postgres":
		if u, err := url.Parse(dsn); err == nil {
			return fmt.Sprintf("postgres://%s/%s", u.Host, strings.TrimPrefix(u.Path, "/"))
		}
	}
	return driver
}

func maskStorage(s config.Storage) string {
	if s.Driver == "s3" {
		return fmt.Sprintf("s3://%s/%s", s.Endpoint, s.Bucket)
	}
	return cmp.Or(s.Path, "textures")
}

var urlCredPat = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/@\s]+):([^/@\s]+)@`)

func redactSecretErr(msg string) string {
	return urlCredPat.ReplaceAllString(msg, "$1***:***@")
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// Verify helpers used by tests.
var (
	_ = fs.ErrNotExist
	_ = sha256.Sum256
	_ = hex.EncodeToString
)
