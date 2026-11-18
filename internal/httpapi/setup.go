package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/importer/bs"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
)

// SetupCookie holds the short-lived session token for the multi-step setup wizard.
const SetupCookie = "skinloom_setup"

// Setup-specific error sentinels mapped to RFC 9457 responses.
var (
	ErrSetupInvalidToken   = errors.New("invalid or expired setup token")
	ErrSetupAlreadyDone    = errors.New("setup has already been completed")
	ErrSetupSessionInvalid = errors.New("setup session is missing or expired")
	ErrSetupNotFound       = errors.New("setup resource not found")
	ErrSetupConflict       = errors.New("setup state conflict")
)

// SetupValidationError carries per-field 422 validation errors.
type SetupValidationError struct {
	Fields []SetupFieldError
}

func (e *SetupValidationError) Error() string {
	return "invalid setup input"
}

// SetupFieldError names one invalid field in a setup request body.
type SetupFieldError struct {
	Field   string
	Message string
}

// SetupRateLimitError carries the retry window when setup token attempts are throttled.
type SetupRateLimitError struct {
	RetryAfter time.Duration
}

func (e *SetupRateLimitError) Error() string {
	return "too many failed setup attempts, try again later"
}

// SetupProbeError carries a gateway/upstream diagnostic failure (e.g. SMTP test error).
type SetupProbeError struct {
	Detail string
}

func (e *SetupProbeError) Error() string { return e.Detail }

// SetupController coordinates Pre-Core setup, infrastructure probes, backup restore,
// Blessing Skin migration, and atomic transition to Running Mode.
type SetupController interface {
	Status(ctx context.Context) (needsSetup bool, lockedByEnv bool, err error)
	CompleteQuickSetup(ctx context.Context, in QuickSetupInput) (*ent.User, *account.NewSession, error)
	CreateSession(ctx context.Context, token string, clientIP string) (cookieToken string, state *SetupState, err error)
	ValidateSession(cookieToken string) bool
	GetState(ctx context.Context, req SetupStateRequest) (*SetupState, error)
	TestDB(ctx context.Context, in DBConfigInput) (*TestDBResult, error)
	TestStorage(ctx context.Context, in StorageConfigInput) (*TestStorageResult, error)
	TestMail(ctx context.Context, in TestMailInput, locale string) (*TestMailResult, error)
	ConnectExisting(ctx context.Context, in ConnectExistingInput) (*ConnectExistingResult, error)
	CompleteWizard(ctx context.Context, in CompleteWizardInput) (*ent.User, *account.NewSession, error)
	CreateUpload(ctx context.Context, in CreateUploadInput) (*CreateUploadResult, error)
	PutUploadChunk(ctx context.Context, uploadID string, offset int64, body io.Reader) (*PutUploadChunkResult, error)
	ParseBSEnv(ctx context.Context, in ParseBSEnvInput) (*ParseBSEnvResult, error)
	TestBSSource(ctx context.Context, in BSSourceInput) (*TestBSSourceResult, error)
	StartBSDryRun(ctx context.Context, in BSDryRunInput) (*StartJobResult, error)
	StartBSImport(ctx context.Context, in BSImportInput) (*StartJobResult, error)
	InspectRestore(ctx context.Context, in InspectRestoreInput) (*InspectRestoreResult, error)
	StartRestore(ctx context.Context, in ExecuteRestoreInput) (*StartJobResult, error)
	GetJob(ctx context.Context, jobID string) (*SetupJobView, error)
	GetJobReportText(ctx context.Context, jobID string) ([]byte, string, error)
}

// QuickSetupInput is the single-step POST /api/v1/setup payload handled via SetupController.
type QuickSetupInput struct {
	Token    string
	Email    string
	Nickname string
	Password string
	SiteName string
	SiteURL  string
	Meta     account.RequestMeta
}

// SetupStateRequest carries raw HTTP proxy probe details for GetState.
type SetupStateRequest struct {
	Client         realip.Info
	PeerIP         string
	HasForwarded   bool
	ForwardedHost  string
	ForwardedProto string
	LocalDataDir   string
}

// EnvLockedInfo summarizes which infrastructure fields are fixed by SKINLOOM_* env vars.
type EnvLockedInfo struct {
	Database       bool   `json:"database"`
	DatabaseDriver string `json:"databaseDriver,omitempty"`
	DatabaseMask   string `json:"databaseMask,omitempty"`
	Storage        bool   `json:"storage"`
	StorageDriver  string `json:"storageDriver,omitempty"`
	StorageMask    string `json:"storageMask,omitempty"`
	TrustedProxies bool   `json:"trustedProxies"`
}

// DetectedProxy describes an untrusted reverse proxy sending X-Forwarded-* headers.
type DetectedProxy struct {
	PeerIP        string `json:"peerIp"`
	SuggestedCIDR string `json:"suggestedCidr"`
	Proto         string `json:"proto,omitempty"`
	Host          string `json:"host,omitempty"`
}

// SetupState is the sanitized setup environment returned to an authenticated wizard session.
type SetupState struct {
	NeedsSetup      bool           `json:"needsSetup"`
	InContainer     bool           `json:"inContainer"`
	EnvLocked       EnvLockedInfo  `json:"envLocked"`
	Database        DBSummary      `json:"database"`
	Storage         StorageSummary `json:"storage"`
	TrustedProxies  []string       `json:"trustedProxies"`
	DetectedProxy   *DetectedProxy `json:"detectedProxy,omitempty"`
	SuggestedURL    string         `json:"suggestedSiteUrl"`
	DefaultTimezone string         `json:"defaultTimezone"`
	MaxUploadBytes  int64          `json:"maxUploadBytes"`
	ActiveJobID     string         `json:"activeJobId,omitempty"`
	InitDBError     string         `json:"initDbError,omitempty"`
}

// DBSummary holds non-secret initial database hints.
type DBSummary struct {
	Driver     string `json:"driver"`
	SQLitePath string `json:"sqlitePath,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Database   string `json:"database,omitempty"`
}

// StorageSummary holds non-secret initial storage hints.
type StorageSummary struct {
	Driver    string `json:"driver"`
	Path      string `json:"path,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Region    string `json:"region,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	PathStyle bool   `json:"pathStyle,omitempty"`
	Insecure  bool   `json:"insecure,omitempty"`
	PublicURL string `json:"publicUrl,omitempty"`
}

// DBConfigInput defines database parameters for probing or saving.
type DBConfigInput struct {
	UseEnv     bool   `json:"useEnv,omitempty"`
	Driver     string `json:"driver,omitempty" enum:"sqlite,mysql,postgres,"`
	Host       string `json:"host,omitempty" maxLength:"255"`
	Port       int    `json:"port,omitempty"`
	Database   string `json:"database,omitempty" maxLength:"255"`
	Username   string `json:"username,omitempty" maxLength:"255"`
	Password   string `json:"password,omitempty" maxLength:"1024"`
	SSLMode    string `json:"sslMode,omitempty" maxLength:"64"`
	SQLitePath string `json:"sqlitePath,omitempty" maxLength:"512"`
	RawDSN     string `json:"rawDsn,omitempty" maxLength:"1024"`
}

// TestDBResult is returned by POST /api/v1/setup/test-db.
type TestDBResult struct {
	OK          bool   `json:"ok"`
	Dialect     string `json:"dialect,omitempty"`
	Version     string `json:"version,omitempty"`
	State       string `json:"state" enum:"empty,skinloom_installed,suspected_bs,missing_db,error"`
	Summary     string `json:"summary,omitempty"`
	Users       int    `json:"users,omitempty"`
	Textures    int    `json:"textures,omitempty"`
	Players     int    `json:"players,omitempty"`
	HintSQL     string `json:"hintSql,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
}

// StorageConfigInput defines local or S3 storage parameters for probing or saving.
type StorageConfigInput struct {
	UseEnv    bool   `json:"useEnv,omitempty"`
	Driver    string `json:"driver,omitempty" enum:"local,s3,"`
	Path      string `json:"path,omitempty" maxLength:"512"`
	Endpoint  string `json:"endpoint,omitempty" maxLength:"255"`
	Bucket    string `json:"bucket,omitempty" maxLength:"255"`
	Region    string `json:"region,omitempty" maxLength:"128"`
	AccessKey string `json:"accessKey,omitempty" maxLength:"255"`
	SecretKey string `json:"secretKey,omitempty" maxLength:"1024"`
	Prefix    string `json:"prefix,omitempty" maxLength:"255"`
	Insecure  bool   `json:"insecure,omitempty"`
	PathStyle bool   `json:"pathStyle,omitempty"`
	PublicURL string `json:"publicUrl,omitempty" maxLength:"512"`
}

// TestStorageResult is returned by POST /api/v1/setup/test-storage.
type TestStorageResult struct {
	OK          bool   `json:"ok"`
	Driver      string `json:"driver"`
	Summary     string `json:"summary,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
}

// MailConfigInput defines optional SMTP/log mail settings during setup.
type MailConfigInput struct {
	Driver     string `json:"driver,omitempty" enum:"none,smtp,log,"`
	From       string `json:"from,omitempty" maxLength:"255"`
	Host       string `json:"host,omitempty" maxLength:"255"`
	Port       int    `json:"port,omitempty"`
	Username   string `json:"username,omitempty" maxLength:"255"`
	Password   string `json:"password,omitempty" maxLength:"1024"`
	Encryption string `json:"encryption,omitempty" enum:"tls,starttls,none,"`
}

// TestMailInput requests a synchronous test email during setup.
type TestMailInput struct {
	Mail MailConfigInput `json:"mail"`
	To   string          `json:"to" maxLength:"255"`
	Site string          `json:"siteName,omitempty" maxLength:"64"`
}

// TestMailResult is returned by POST /api/v1/setup/test-mail.
type TestMailResult struct {
	OK          bool   `json:"ok"`
	Summary     string `json:"summary,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
}

// ConnectExistingInput attaches this node to an already initialized SkinLoom database.
type ConnectExistingInput struct {
	Database       DBConfigInput      `json:"database"`
	Storage        StorageConfigInput `json:"storage"`
	TrustedProxies []string           `json:"trustedProxies,omitempty"`
}

// ConnectExistingResult instructs the UI where to redirect after attaching to an existing DB.
type ConnectExistingResult struct {
	RedirectTo string `json:"redirectTo"`
}

// CompleteWizardInput finishes a brand-new SkinLoom installation with full infra & admin config.
type CompleteWizardInput struct {
	Database       DBConfigInput       `json:"database"`
	Storage        StorageConfigInput  `json:"storage"`
	TrustedProxies []string            `json:"trustedProxies,omitempty"`
	SiteName       string              `json:"siteName,omitempty" maxLength:"64"`
	SiteURL        string              `json:"siteUrl,omitempty" maxLength:"512"`
	Timezone       string              `json:"timezone,omitempty" maxLength:"128"`
	Email          string              `json:"email" maxLength:"255"`
	Nickname       string              `json:"nickname" maxLength:"128"`
	Password       string              `json:"password" maxLength:"1024"`
	Mail           *MailConfigInput    `json:"mail,omitempty"`
	ClientMeta     account.RequestMeta `json:"-"`
}

// CreateUploadInput initiates a chunked archive upload.
type CreateUploadInput struct {
	Purpose   string `json:"purpose" enum:"bs_textures,backup"`
	Filename  string `json:"filename" maxLength:"255"`
	TotalSize int64  `json:"totalSize"`
}

// CreateUploadResult returns the upload session id and chunk size.
type CreateUploadResult struct {
	UploadID       string `json:"uploadId"`
	ChunkSize      int64  `json:"chunkSize"`
	MaxUploadBytes int64  `json:"maxUploadBytes"`
}

// PutUploadChunkResult reports chunk progress.
type PutUploadChunkResult struct {
	Received int64 `json:"received"`
	Complete bool  `json:"complete"`
}

// ParseBSEnvInput parses a BS .env file from content or server path.
type ParseBSEnvInput struct {
	Content string `json:"content,omitempty" maxLength:"262144"`
	Path    string `json:"path,omitempty" maxLength:"1024"`
}

// ParseBSEnvResult returns sanitized metadata parsed from BS .env.
type ParseBSEnvResult struct {
	EnvID string     `json:"envId,omitempty"`
	Env   bs.EnvInfo `json:"env"`
}

// BSSourceInput specifies how to read the Blessing Skin source site.
type BSSourceInput struct {
	EnvID              string   `json:"envId,omitempty" maxLength:"128"`
	SourceType         string   `json:"sourceType,omitempty" enum:"db,bridge,"`
	Driver             string   `json:"driver,omitempty" enum:"mysql,postgres,sqlite,"`
	Host               string   `json:"host,omitempty" maxLength:"255"`
	Port               int      `json:"port,omitempty"`
	Database           string   `json:"database,omitempty" maxLength:"255"`
	Username           string   `json:"username,omitempty" maxLength:"255"`
	Password           string   `json:"password,omitempty" maxLength:"1024"`
	Socket             string   `json:"socket,omitempty" maxLength:"512"`
	RawDSN             string   `json:"rawDsn,omitempty" maxLength:"1024"`
	Prefix             string   `json:"prefix,omitempty" maxLength:"64"`
	PasswordMethod     string   `json:"passwordMethod,omitempty" maxLength:"64"`
	Salt               string   `json:"salt,omitempty" maxLength:"1024"`
	TransitionMethods  []string `json:"transitionMethods,omitempty"`
	Timezone           string   `json:"timezone,omitempty" maxLength:"128"`
	TextureSource      string   `json:"textureSource,omitempty" enum:"dir,upload,none,bridge,"`
	TexturesDir        string   `json:"texturesDir,omitempty" maxLength:"1024"`
	UploadID           string   `json:"uploadId,omitempty" maxLength:"128"`
	BridgeURL          string   `json:"bridgeUrl,omitempty" maxLength:"512"`
	BridgeToken        string   `json:"bridgeToken,omitempty" maxLength:"512"`
	AllowInsecureLocal bool     `json:"allowInsecureLocal,omitempty"`
	MailFromEnv        *bs.Mail `json:"-"`
}

// TestBSSourceResult reports read-only counts from BS.
type TestBSSourceResult struct {
	OK          bool             `json:"ok"`
	Counts      bs.SourceSummary `json:"counts"`
	Warning     string           `json:"warning,omitempty"`
	ErrorDetail string           `json:"errorDetail,omitempty"`
}

// BSDryRunInput starts a dry-run BS migration job.
type BSDryRunInput struct {
	Source BSSourceInput `json:"source"`
}

// FallbackAdminInput creates a fallback superadmin when BS had zero superadmins.
type FallbackAdminInput struct {
	Email    string `json:"email" maxLength:"255"`
	Nickname string `json:"nickname" maxLength:"128"`
	Password string `json:"password" maxLength:"1024"`
}

// BSImportInput starts a real BS migration job and transitions the server.
type BSImportInput struct {
	Database       DBConfigInput       `json:"database"`
	Storage        StorageConfigInput  `json:"storage"`
	TrustedProxies []string            `json:"trustedProxies,omitempty"`
	Source         BSSourceInput       `json:"source"`
	SiteURL        string              `json:"siteUrl,omitempty" maxLength:"512"`
	FallbackAdmin  *FallbackAdminInput `json:"fallbackAdmin,omitempty"`
}

// InspectRestoreInput inspects an uploaded or server-local SkinLoom backup archive.
type InspectRestoreInput struct {
	UploadID string `json:"uploadId,omitempty" maxLength:"128"`
	Path     string `json:"path,omitempty" maxLength:"1024"`
}

// BackupManifestView mirrors backup.Manifest safely for OpenAPI.
type BackupManifestView struct {
	Format       int       `json:"format"`
	Version      string    `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	Database     string    `json:"database"`
	Migration    int64     `json:"migration"`
	HasDatabase  bool      `json:"hasDatabase"`
	Textures     int       `json:"textures"`
	TextureBytes int64     `json:"textureBytes"`
}

// InspectRestoreResult summarizes an inspected backup archive.
type InspectRestoreResult struct {
	Manifest      BackupManifestView `json:"manifest"`
	HasConfig     bool               `json:"hasConfig"`
	ConfigDialect string             `json:"configDialect,omitempty"`
	ConfigStorage string             `json:"configStorage,omitempty"`
	Warnings      []string           `json:"warnings"`
}

// ExecuteRestoreInput executes a backup restore job and transitions the server.
type ExecuteRestoreInput struct {
	UploadID           string             `json:"uploadId,omitempty" maxLength:"128"`
	Path               string             `json:"path,omitempty" maxLength:"1024"`
	Database           DBConfigInput      `json:"database"`
	Storage            StorageConfigInput `json:"storage"`
	TrustedProxies     []string           `json:"trustedProxies,omitempty"`
	ApplyArchiveConfig bool               `json:"applyArchiveConfig,omitempty"`
}

// StartJobResult returns the created async job id.
type StartJobResult struct {
	JobID string `json:"jobId"`
}

// BSReportEntryView is one migration report entry in JSON.
type BSReportEntryView struct {
	Severity   string `json:"severity"`
	Table      string `json:"table"`
	ID         string `json:"id,omitempty"`
	Message    string `json:"message"`
	LeftBehind bool   `json:"leftBehind,omitempty"`
}

// BSReportView is the structured BS migration report in JSON.
type BSReportView struct {
	DryRun      bool                `json:"dryRun"`
	Entries     []BSReportEntryView `json:"entries"`
	Imported    map[string]int      `json:"imported"`
	Files       int                 `json:"files"`
	FilesCopied int                 `json:"filesCopied"`
	Superadmins int                 `json:"superadmins"`
	Blocked     bool                `json:"blocked"`
	Failed      string              `json:"failed,omitempty"`
}

// SetupJobView reports async dry-run, import, or restore progress and outcome.
type SetupJobView struct {
	ID         string        `json:"id"`
	Kind       string        `json:"kind" enum:"bs_dry_run,bs_import,restore"`
	Status     string        `json:"status" enum:"running,succeeded,failed"`
	Stage      string        `json:"stage"`
	Percent    int           `json:"percent"`
	Done       int           `json:"done"`
	Total      int           `json:"total"`
	Message    string        `json:"message,omitempty"`
	Error      string        `json:"error,omitempty"`
	RedirectTo string        `json:"redirectTo,omitempty"`
	Report     *BSReportView `json:"report,omitempty"`
	HasReport  bool          `json:"hasReport"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

// BSReportToView converts *bs.Report into a JSON-friendly BSReportView.
func BSReportToView(r *bs.Report) *BSReportView {
	if r == nil {
		return nil
	}
	entries := make([]BSReportEntryView, 0, len(r.Entries))
	for _, e := range r.Entries {
		entries = append(entries, BSReportEntryView{
			Severity:   e.Severity.String(),
			Table:      e.Table,
			ID:         e.ID,
			Message:    e.Message,
			LeftBehind: e.LeftBehind,
		})
	}
	imported := make(map[string]int, len(r.Imported))
	for k, v := range r.Imported {
		imported[k] = v
	}
	var failed string
	if r.Failed != nil {
		failed = r.Failed.Error()
	}
	return &BSReportView{
		DryRun:      r.DryRun,
		Entries:     entries,
		Imported:    imported,
		Files:       r.Files,
		FilesCopied: r.FilesCopied,
		Superadmins: r.Superadmins,
		Blocked:     r.Blocked(),
		Failed:      failed,
	}
}

func registerSetup(api huma.API, d Deps) {
	setupSession := func(ctx context.Context, cookie string) error {
		if d.Setup == nil {
			return huma.Error409Conflict("setup has already been completed")
		}
		if needs, _, err := d.Setup.Status(ctx); err == nil && !needs {
			return huma.Error409Conflict("setup has already been completed")
		}
		if cookie == "" || !d.Setup.ValidateSession(cookie) {
			return huma.Error401Unauthorized("setup session is missing or expired")
		}
		return nil
	}
	setupJobSession := func(cookie string) error {
		if d.Setup == nil {
			return huma.Error409Conflict("setup has already been completed")
		}
		if cookie == "" || !d.Setup.ValidateSession(cookie) {
			return huma.Error401Unauthorized("setup session is missing or expired")
		}
		return nil
	}

	huma.Register(api, huma.Operation{
		OperationID: "createSetupSession",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/session",
		Summary:     "Verify the setup token and start a setup wizard session",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Token string `json:"token" maxLength:"512"`
		}
	}) (*struct {
		SetCookie http.Cookie `header:"Set-Cookie"`
		Body      SetupState
	}, error) {
		if d.Setup == nil {
			return nil, huma.Error409Conflict("setup has already been completed")
		}
		ri := requestInfo(ctx)
		cookieVal, state, err := d.Setup.CreateSession(ctx, in.Body.Token, ri.client.ClientIP)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
			Body      SetupState
		}{
			SetCookie: setupSessionCookie(cookieVal, ri),
			Body:      *state,
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getSetupState",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup/state",
		Summary:     "Current sanitized setup environment and detected reverse proxy",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
	}) (*struct{ Body SetupState }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		ri := requestInfo(ctx)
		sr := SetupStateRequest{
			Client:         ri.client,
			PeerIP:         ri.peerIP,
			HasForwarded:   ri.hasForwarded,
			ForwardedHost:  ri.forwardedHost,
			ForwardedProto: ri.forwardedProto,
		}
		st, err := d.Setup.GetState(ctx, sr)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body SetupState }{Body: *st}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "testSetupDB",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/test-db",
		Summary:     "Probe target database connectivity and detect existing data",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        DBConfigInput
	}) (*struct{ Body TestDBResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.TestDB(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body TestDBResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "testSetupStorage",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/test-storage",
		Summary:     "Verify local or S3 texture storage permissions",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        StorageConfigInput
	}) (*struct{ Body TestStorageResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.TestStorage(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body TestStorageResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "testSetupMail",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/test-mail",
		Summary:     "Send a test email during setup to verify SMTP settings",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        TestMailInput
	}) (*struct{ Body TestMailResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.TestMail(ctx, in.Body, requestInfo(ctx).locale)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body TestMailResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "connectExistingSetup",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/connect-existing",
		Summary:     "Save infrastructure configuration and attach to an existing SkinLoom database",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        ConnectExistingInput
	}) (*struct {
		SetCookie http.Cookie `header:"Set-Cookie"`
		Body      ConnectExistingResult
	}, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.ConnectExisting(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
			Body      ConnectExistingResult
		}{
			SetCookie: clearedSetupSessionCookie(requestInfo(ctx)),
			Body:      *res,
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "completeSetupWizard",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/complete",
		Summary:     "Write configuration, migrate database, create superadmin, and transition to running mode",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		UserAgent   string `header:"User-Agent"`
		SetupCookie string `cookie:"skinloom_setup"`
		Body        CompleteWizardInput
	}) (*struct {
		SetCookies []http.Cookie `header:"Set-Cookie"`
		Body       UserView
	}, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		ri := requestInfo(ctx)
		in.Body.ClientMeta = ri.meta(in.UserAgent)
		u, sess, err := d.Setup.CompleteWizard(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct {
			SetCookies []http.Cookie `header:"Set-Cookie"`
			Body       UserView
		}{
			SetCookies: []http.Cookie{sessionCookie(sess, ri), clearedSetupSessionCookie(ri)},
			Body:       userView(u),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createSetupUpload",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/uploads",
		Summary:     "Start a chunked archive upload for BS migration textures or SkinLoom backup",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        CreateUploadInput
	}) (*struct{ Body CreateUploadResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.CreateUpload(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body CreateUploadResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "parseSetupBSEnv",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/bs/parse-env",
		Summary:     "Parse a Blessing Skin .env file (content or path) into sanitized metadata",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        ParseBSEnvInput
	}) (*struct{ Body ParseBSEnvResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.ParseBSEnv(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body ParseBSEnvResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "testSetupBSSource",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/bs/test-source",
		Summary:     "Read-only connectivity and row count check of the Blessing Skin source",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        BSSourceInput
	}) (*struct{ Body TestBSSourceResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.TestBSSource(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body TestBSSourceResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "startSetupBSDryRun",
		Method:        http.MethodPost,
		Path:          "/api/v1/setup/bs/dry-run",
		Summary:       "Run a non-destructive Blessing Skin migration dry-run job",
		Tags:          []string{"setup"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        BSDryRunInput
	}) (*struct{ Body StartJobResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.StartBSDryRun(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body StartJobResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "startSetupBSImport",
		Method:        http.MethodPost,
		Path:          "/api/v1/setup/bs/import",
		Summary:       "Start the Blessing Skin import job and transition into running mode",
		Tags:          []string{"setup"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        BSImportInput
	}) (*struct{ Body StartJobResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.StartBSImport(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body StartJobResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "inspectSetupRestore",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/restore/inspect",
		Summary:     "Read manifest metadata from an uploaded or local SkinLoom backup archive",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        InspectRestoreInput
	}) (*struct{ Body InspectRestoreResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.InspectRestore(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body InspectRestoreResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "executeSetupRestore",
		Method:        http.MethodPost,
		Path:          "/api/v1/setup/restore/execute",
		Summary:       "Restore a SkinLoom backup archive and transition into running mode",
		Tags:          []string{"setup"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		Body        ExecuteRestoreInput
	}) (*struct{ Body StartJobResult }, error) {
		if err := setupSession(ctx, in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.StartRestore(ctx, in.Body)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body StartJobResult }{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getSetupJob",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup/jobs/{id}",
		Summary:     "Get status and report of a setup dry-run, import, or restore job",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *struct {
		SetupCookie string `cookie:"skinloom_setup"`
		ID          string `path:"id"`
	}) (*struct{ Body SetupJobView }, error) {
		if err := setupJobSession(in.SetupCookie); err != nil {
			return nil, err
		}
		res, err := d.Setup.GetJob(ctx, in.ID)
		if err != nil {
			return nil, mapSetupError(err)
		}
		return &struct{ Body SetupJobView }{Body: *res}, nil
	})
}

func mountSetupRaw(r chi.Router, d Deps) {
	requireCookie := func(w http.ResponseWriter, req *http.Request, allowCompleted bool) bool {
		if d.Setup == nil {
			writeProblem(w, http.StatusConflict, "setup has already been completed")
			return false
		}
		if !allowCompleted {
			if needs, _, err := d.Setup.Status(req.Context()); err == nil && !needs {
				writeProblem(w, http.StatusConflict, "setup has already been completed")
				return false
			}
		}
		c, err := req.Cookie(SetupCookie)
		if err != nil || !d.Setup.ValidateSession(c.Value) {
			writeProblem(w, http.StatusUnauthorized, "setup session is missing or expired")
			return false
		}
		return true
	}

	r.Put("/api/v1/setup/uploads/{id}/chunks", func(w http.ResponseWriter, req *http.Request) {
		if !requireCookie(w, req, false) {
			return
		}
		id := chi.URLParam(req, "id")
		offsetStr := req.URL.Query().Get("offset")
		if offsetStr == "" {
			writeProblem(w, http.StatusBadRequest, "missing offset query parameter")
			return
		}
		offset, err := strconv.ParseInt(offsetStr, 10, 64)
		if err != nil || offset < 0 {
			writeProblem(w, http.StatusBadRequest, "invalid offset query parameter")
			return
		}
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Minute))
		req.Body = http.MaxBytesReader(w, req.Body, 9<<20)

		res, err := d.Setup.PutUploadChunk(req.Context(), id, offset, req.Body)
		if err != nil {
			writeSetupHTTPError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	})

	r.Get("/api/v1/setup/jobs/{id}/report.txt", func(w http.ResponseWriter, req *http.Request) {
		if !requireCookie(w, req, true) {
			return
		}
		id := chi.URLParam(req, "id")
		data, filename, err := d.Setup.GetJobReportText(req.Context(), id)
		if err != nil {
			writeSetupHTTPError(w, err)
			return
		}
		if filename == "" {
			filename = "bs-migration-report.txt"
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}

func setupSessionCookie(token string, ri reqInfo) http.Cookie {
	return http.Cookie{
		Name:     SetupCookie,
		Value:    token,
		Path:     "/api/v1/setup",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   ri.client.Proto == "https",
	}
}

func clearedSetupSessionCookie(ri reqInfo) http.Cookie {
	return http.Cookie{
		Name:     SetupCookie,
		Value:    "",
		Path:     "/api/v1/setup",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   ri.client.Proto == "https",
	}
}

func mapSetupError(err error) error {
	var rl *SetupRateLimitError
	if errors.As(err, &rl) {
		return rateLimitError(&account.RateLimitError{RetryAfter: rl.RetryAfter}, "too many failed setup attempts, try again later")
	}
	var ve *SetupValidationError
	if errors.As(err, &ve) {
		details := make([]error, 0, len(ve.Fields))
		for _, f := range ve.Fields {
			loc := f.Field
			if !strings.HasPrefix(loc, "body.") {
				loc = "body." + loc
			}
			details = append(details, &huma.ErrorDetail{Location: loc, Message: f.Message})
		}
		return huma.Error422UnprocessableEntity("invalid setup input", details...)
	}
	var pe *SetupProbeError
	if errors.As(err, &pe) {
		return huma.NewError(http.StatusBadGateway, pe.Detail)
	}
	switch {
	case errors.Is(err, ErrSetupInvalidToken), errors.Is(err, account.ErrInvalidToken):
		return huma.Error403Forbidden("the setup link is invalid, used or expired; restart SkinLoom or run `skinloom setup-link` for a new one")
	case errors.Is(err, ErrSetupAlreadyDone), errors.Is(err, account.ErrSetupDone), errors.Is(err, ErrSetupConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrSetupSessionInvalid):
		return huma.Error401Unauthorized(err.Error())
	case errors.Is(err, ErrSetupNotFound):
		return huma.Error404NotFound(err.Error())
	default:
		return apiError(err)
	}
}

func writeSetupHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSetupNotFound):
		writeProblem(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSetupSessionInvalid):
		writeProblem(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrSetupConflict), errors.Is(err, ErrSetupAlreadyDone):
		writeProblem(w, http.StatusConflict, err.Error())
	default:
		var ve *SetupValidationError
		if errors.As(err, &ve) && len(ve.Fields) > 0 {
			writeProblem(w, http.StatusUnprocessableEntity, ve.Fields[0].Message)
			return
		}
		writeProblem(w, http.StatusBadRequest, err.Error())
	}
}
