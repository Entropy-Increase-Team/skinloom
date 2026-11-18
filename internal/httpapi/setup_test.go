package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/sitesettings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

type stubSetupController struct {
	validSession string
	chunkGot     []byte
}

func (s *stubSetupController) Status(context.Context) (bool, bool, error) { return true, false, nil }
func (s *stubSetupController) CompleteQuickSetup(context.Context, QuickSetupInput) (*ent.User, *account.NewSession, error) {
	return nil, nil, ErrSetupInvalidToken
}
func (s *stubSetupController) CreateSession(_ context.Context, tok, _ string) (string, *SetupState, error) {
	if tok != "good-token" {
		return "", nil, ErrSetupInvalidToken
	}
	s.validSession = "setup-sess-1"
	return s.validSession, &SetupState{NeedsSetup: true, DefaultTimezone: "Asia/Shanghai", MaxUploadBytes: 1 << 30}, nil
}
func (s *stubSetupController) ValidateSession(c string) bool {
	return c != "" && c == s.validSession
}
func (s *stubSetupController) GetState(context.Context, SetupStateRequest) (*SetupState, error) {
	return &SetupState{NeedsSetup: true, DefaultTimezone: "Asia/Shanghai", MaxUploadBytes: 1 << 30}, nil
}
func (s *stubSetupController) TestDB(context.Context, DBConfigInput) (*TestDBResult, error) {
	return &TestDBResult{OK: true, Dialect: "sqlite", State: "empty"}, nil
}
func (s *stubSetupController) TestStorage(context.Context, StorageConfigInput) (*TestStorageResult, error) {
	return &TestStorageResult{OK: true, Driver: "local"}, nil
}
func (s *stubSetupController) TestMail(context.Context, TestMailInput, string) (*TestMailResult, error) {
	return &TestMailResult{OK: true}, nil
}
func (s *stubSetupController) ConnectExisting(context.Context, ConnectExistingInput) (*ConnectExistingResult, error) {
	return &ConnectExistingResult{RedirectTo: "/auth/login"}, nil
}
func (s *stubSetupController) CompleteWizard(context.Context, CompleteWizardInput) (*ent.User, *account.NewSession, error) {
	return nil, nil, ErrSetupConflict
}
func (s *stubSetupController) CreateUpload(context.Context, CreateUploadInput) (*CreateUploadResult, error) {
	return &CreateUploadResult{UploadID: "u1", ChunkSize: 8 << 20, MaxUploadBytes: 1 << 30}, nil
}
func (s *stubSetupController) PutUploadChunk(_ context.Context, _ string, _ int64, body io.Reader) (*PutUploadChunkResult, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	s.chunkGot = append(s.chunkGot, b...)
	return &PutUploadChunkResult{Received: int64(len(s.chunkGot)), Complete: true}, nil
}
func (s *stubSetupController) ParseBSEnv(context.Context, ParseBSEnvInput) (*ParseBSEnvResult, error) {
	return &ParseBSEnvResult{}, nil
}
func (s *stubSetupController) TestBSSource(context.Context, BSSourceInput) (*TestBSSourceResult, error) {
	return &TestBSSourceResult{OK: true}, nil
}
func (s *stubSetupController) StartBSDryRun(context.Context, BSDryRunInput) (*StartJobResult, error) {
	return &StartJobResult{JobID: "j1"}, nil
}
func (s *stubSetupController) StartBSImport(context.Context, BSImportInput) (*StartJobResult, error) {
	return &StartJobResult{JobID: "j2"}, nil
}
func (s *stubSetupController) InspectRestore(context.Context, InspectRestoreInput) (*InspectRestoreResult, error) {
	return &InspectRestoreResult{}, nil
}
func (s *stubSetupController) StartRestore(context.Context, ExecuteRestoreInput) (*StartJobResult, error) {
	return &StartJobResult{JobID: "j3"}, nil
}
func (s *stubSetupController) GetJob(context.Context, string) (*SetupJobView, error) {
	return &SetupJobView{ID: "j1", Kind: "bs_dry_run", Status: "succeeded", HasReport: true}, nil
}
func (s *stubSetupController) GetJobReportText(context.Context, string) ([]byte, string, error) {
	return []byte("report ok\n"), "report.txt", nil
}

func TestSetupModeGuardCSRFAndSessionGate(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	themesReg := theme.NewRegistry()
	if err := themesReg.LoadDir(themes.Builtin, "builtin"); err != nil {
		t.Fatal(err)
	}
	reg, err := sitesettings.Registry(themesReg)
	if err != nil {
		t.Fatal(err)
	}
	broker := pubsub.New()
	defer broker.Close()

	stub := &stubSetupController{}
	up := false
	h, _ := New(Deps{
		Log:         log,
		Setup:       stub,
		SettingsReg: reg,
		Themes:      themesReg,
		Broker:      broker,
		WebReady:    func() bool { return up },
	})

	// /readyz in Setup Mode tracks WebReady instead of DB.
	if rec := do(t, h, http.MethodGet, "/readyz", "", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz with web down = %d, want 503", rec.Code)
	}
	up = true
	if rec := do(t, h, http.MethodGet, "/readyz", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("/readyz with web up = %d, want 200", rec.Code)
	}

	// Bootstrap works without DB and announces needsSetup: true.
	bootRec := do(t, h, http.MethodGet, "/api/v1/site/bootstrap", "", "")
	if bootRec.Code != http.StatusOK || !strings.Contains(bootRec.Body.String(), `"needsSetup":true`) {
		t.Fatalf("setup bootstrap: %d %s", bootRec.Code, bootRec.Body)
	}

	// Business and game routes are blocked with 503 in Setup Mode.
	for _, blocked := range []string{"/api/v1/textures", "/Steve.json", "/oauth/authorize"} {
		if rec := do(t, h, http.MethodGet, blocked, "", ""); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s in setup mode: %d, want 503", blocked, rec.Code)
		}
	}

	// Unauthenticated access to /api/v1/setup/state must be 401.
	if rec := do(t, h, http.MethodGet, "/api/v1/setup/state", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth /setup/state = %d, want 401", rec.Code)
	}

	// CSRF guard protects /api/v1/setup/* even before cookie is set.
	if rec := do(t, h, http.MethodPost, "/api/v1/setup/session", "", `{"token":"good-token"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF header = %d, want 403", rec.Code)
	}

	// Valid session creation sets HttpOnly SameSite=Lax Path=/api/v1/setup cookie.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/session", strings.NewReader(`{"token":"good-token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CSRFHeader, "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body)
	}
	var setupCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SetupCookie {
			setupCookie = c
		}
	}
	if setupCookie == nil || !setupCookie.HttpOnly || setupCookie.Path != "/api/v1/setup" {
		t.Fatalf("unexpected setup cookie: %+v", setupCookie)
	}

	// Cross-site Origin on /api/v1/setup/uploads/u1/chunks must be rejected by CSRF guard.
	badOriginReq := httptest.NewRequest(http.MethodPut, "/api/v1/setup/uploads/u1/chunks?offset=0", bytes.NewReader([]byte("abc")))
	badOriginReq.Host = "skins.example.com"
	badOriginReq.Header.Set(CSRFHeader, "1")
	badOriginReq.Header.Set("Origin", "https://evil.example")
	badOriginReq.AddCookie(setupCookie)
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badOriginReq)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("cross-site chunk upload = %d, want 403", badRec.Code)
	}

	// Valid chunk upload and report download succeed.
	chunkReq := httptest.NewRequest(http.MethodPut, "/api/v1/setup/uploads/u1/chunks?offset=0", bytes.NewReader([]byte("hello")))
	chunkReq.Header.Set(CSRFHeader, "1")
	chunkReq.AddCookie(setupCookie)
	chunkRec := httptest.NewRecorder()
	h.ServeHTTP(chunkRec, chunkReq)
	if chunkRec.Code != http.StatusOK || string(stub.chunkGot) != "hello" {
		t.Fatalf("chunk upload: %d %s got=%q", chunkRec.Code, chunkRec.Body, stub.chunkGot)
	}

	repReq := httptest.NewRequest(http.MethodGet, "/api/v1/setup/jobs/j1/report.txt", nil)
	repReq.AddCookie(setupCookie)
	repRec := httptest.NewRecorder()
	h.ServeHTTP(repRec, repReq)
	if repRec.Code != http.StatusOK || !strings.Contains(repRec.Body.String(), "report ok") {
		t.Fatalf("report.txt: %d %s", repRec.Code, repRec.Body)
	}
	_ = realip.Info{}
}

func TestAdminRescanMissingBlobsEndpoint(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	if rec := do(t, h, http.MethodPost, "/api/v1/admin/textures/rescan", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous rescan = %d, want 401", rec.Code)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/admin/textures/rescan", "Bearer "+testToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin rescan: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Checked int `json:"checked"`
		Updated int `json:"updated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
}
