package httpapi

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/stats"
)

// SystemStatus is the admin panel's system status page (02 F40). It names the
// database engine and version but never the connection string or user (BS showed the
// DB user name).
type SystemStatus struct {
	Server   ServerStatus   `json:"server"`
	Database DatabaseStatus `json:"database"`
	Storage  StorageStatus  `json:"storage"`
	Jobs     JobsStatus     `json:"jobs"`
	Mail     MailStatus     `json:"mail"`
	Config   ConfigStatus   `json:"config"`
	Checks   []StatusCheck  `json:"checks" doc:"Things worth a look; level ok when nothing is wrong"`
}

type ServerStatus struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit,omitempty"`
	BuildDate string    `json:"buildDate,omitempty"`
	Go        string    `json:"go"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Role      string    `json:"role" doc:"server.role"`
	Web       string    `json:"web" doc:"web.mode"`
	StartedAt time.Time `json:"startedAt"`
}

type DatabaseStatus struct {
	Dialect    string  `json:"dialect" enum:"sqlite,mysql,postgres"`
	Version    string  `json:"version"`
	Migration  int64   `json:"migration" doc:"Latest migration applied"`
	Pending    int     `json:"pending" doc:"Migrations not applied (normally 0: they run at startup)"`
	PingMillis float64 `json:"pingMillis"`
}

type StorageStatus struct {
	Driver string `json:"driver" enum:"local,s3"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

type JobsStatus struct {
	Pending       int        `json:"pending"`
	Overdue       int        `json:"overdue" doc:"Pending for over a minute past their time"`
	Failed        int        `json:"failed" doc:"Given up after the last retry, kept for 7 days"`
	OldestOverdue *time.Time `json:"oldestOverdue,omitempty"`
}

type MailStatus struct {
	Driver string `json:"driver"`
	Ready  bool   `json:"ready" doc:"Mail is set up and site.url is set: verification and password reset work"`
}

type ConfigStatus struct {
	Version int64  `json:"version" doc:"Config version"`
	Theme   string `json:"theme"`
	Changed int    `json:"changed" doc:"Settings changed from their defaults"`
}

// StatusCheck is one finding. The frontend words it by ID.
type StatusCheck struct {
	ID    string `json:"id" enum:"database,migrations,jobs_overdue,jobs_failed,mail,site_url"`
	Level string `json:"level" enum:"ok,warn,error"`
}

func registerStatus(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getSystemStatus",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/status",
		Summary:     "System status",
		Description: "Version, runtime, database, storage, job queue and mail of this node, with findings worth a look.",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *adminAuth) (*struct{ Body SystemStatus }, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		s, err := systemStatus(ctx, d, time.Now())
		if err != nil {
			return nil, err
		}
		return &struct{ Body SystemStatus }{s}, nil
	})
}

func systemStatus(ctx context.Context, d Deps, now time.Time) (SystemStatus, error) {
	// A database that answers the ping but then stalls must not hang the page.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	snap := d.Settings.Current()
	s := SystemStatus{
		Server: ServerStatus{Version: buildinfo.Version, Commit: buildinfo.Commit, BuildDate: buildinfo.Date,
			Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
			Role: d.Runtime.Role, Web: d.Runtime.Web, StartedAt: d.Runtime.Started},
		Database: DatabaseStatus{Dialect: string(d.DB.Dialect)},
		Storage:  StorageStatus{Driver: d.Runtime.Storage},
		Mail:     MailStatus{Driver: mail.ConfigFrom(snap).Driver, Ready: mailReady(snap)},
		Config:   ConfigStatus{Version: snap.Version, Theme: d.Themes.Resolve(snap).ID},
	}
	check := func(id string, ok bool, bad string) {
		level := "ok"
		if !ok {
			level = bad
		}
		s.Checks = append(s.Checks, StatusCheck{ID: id, Level: level})
	}

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	start := time.Now()
	pingErr := d.DB.SQL.PingContext(pingCtx)
	cancel()
	s.Database.PingMillis = float64(time.Since(start).Microseconds()) / 1000
	check("database", pingErr == nil, "error")
	if pingErr != nil {
		return s, nil // the rest needs the database
	}
	var err error
	if s.Database.Version, err = d.DB.ServerVersion(ctx); err != nil {
		return s, err
	}
	if s.Database.Migration, s.Database.Pending, err = migrate.Versions(ctx, d.DB); err != nil {
		return s, err
	}
	check("migrations", s.Database.Pending == 0, "error")
	if s.Storage.Files, s.Storage.Bytes, err = stats.Storage(ctx, d.DB.Ent); err != nil {
		return s, err
	}
	q, err := jobs.Counts(ctx, d.DB.Ent, now)
	if err != nil {
		return s, err
	}
	s.Jobs = JobsStatus{Pending: q.Pending, Overdue: q.Overdue, Failed: q.Failed, OldestOverdue: q.OldestOverdue}
	check("jobs_overdue", q.Overdue == 0, "warn")
	check("jobs_failed", q.Failed == 0, "warn")
	check("mail", mail.ConfigFrom(snap).Enabled(), "warn")
	check("site_url", snap.String("site.url") != "", "warn")
	for _, def := range d.Settings.Registry().All() {
		if snap.IsSet(def.Key) {
			s.Config.Changed++
		}
	}
	return s, nil
}
