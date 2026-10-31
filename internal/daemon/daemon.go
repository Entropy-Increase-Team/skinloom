// Package daemon installs SkinLoom as a background service of the operating system
// (docs/design/04 §9.1, 02 F76): a systemd unit on Linux, a launchd daemon on macOS,
// a Windows service. The service runs "skinloom service run", which serves the site
// like "skinloom up" and answers the service manager's start and stop.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/kardianos/service"
)

// Name is the service name on every system.
const Name = "SkinLoom"

// LogFile is where a service run logs when the config names no log.file: nobody reads
// a service's console. Relative to the data directory.
const LogFile = "logs/skinloom.log"

// Options describe an installed service.
type Options struct {
	Exe        string // absolute path of the skinloom binary
	DataDir    string // absolute
	ConfigPath string // "" = <DataDir>/config.toml
	// User runs the service on Linux and macOS ("" = the system's default, root).
	// Windows services run as LOCAL SERVICE.
	User string
}

// dir is the release bundle the binary is in: the frontend (web/) and Node.js
// (runtime/) sit next to it.
func (o Options) dir() string { return filepath.Dir(o.Exe) }

// args are the service's command line after the binary.
func (o Options) args() []string {
	a := []string{"service", "run", "--data", o.DataDir}
	if o.ConfigPath != "" {
		a = append(a, "--config", o.ConfigPath)
	}
	return a
}

// userName is a user name the service may run as (and that systemd accepts).
var userName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

// validate refuses what would not survive the trip into a unit file or service
// definition: a line break in a path would add directives to a unit run by root.
func (o Options) validate() error {
	for name, v := range map[string]string{"binary path": o.Exe, "data directory": o.DataDir, "config path": o.ConfigPath, "user": o.User} {
		if strings.ContainsFunc(v, unicode.IsControl) {
			return fmt.Errorf("the %s contains a control character", name)
		}
	}
	if o.User != "" && !userName.MatchString(o.User) {
		return fmt.Errorf("%q is not a user name", o.User)
	}
	return nil
}

// underHome: path is in a tree systemd's ProtectHome=true hides from the service.
func underHome(path string) bool {
	for _, dir := range []string{"/home", "/root", "/run/user"} {
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

// writable are the directories the service writes to: the data and the frontend's
// render cache.
func (o Options) writable() []string {
	return []string{o.DataDir, filepath.Join(o.dir(), "web", "apps", "web", ".next")}
}

func (o Options) config() *service.Config {
	c := &service.Config{
		Name:             Name,
		DisplayName:      "SkinLoom (云织)",
		Description:      "SkinLoom (云织) Minecraft skin site",
		Executable:       o.Exe,
		Arguments:        o.args(),
		WorkingDirectory: o.dir(),
		UserName:         o.User,
		Option: service.KeyValue{
			// systemd: our unit with its hardening, instead of the library's.
			"SystemdScript": SystemdUnit(o),
			// launchd: start at boot, restart when it exits.
			"RunAtLoad": true,
			"KeepAlive": true,
			// Windows: restart after a crash; start after the network is up.
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"DelayedAutoStart":       true,
		},
	}
	platformConfig(c)
	return c
}

// SystemdUnit is the unit file written on Linux, for validated options. It has no
// {{ }}: the library runs it through text/template and must get it back unchanged.
func SystemdUnit(o Options) string {
	var rw []string
	for _, d := range o.writable() {
		rw = append(rw, systemdPath("-"+d)) // "-": a missing directory (no frontend) is not an error
	}
	user := ""
	if o.User != "" {
		user = "User=" + o.User + "\nGroup=" + o.User + "\n"
	}
	// A bundle in a home directory cannot be hidden from the service that runs it:
	// read-only there, and ReadWritePaths still opens the data directory.
	protectHome := "true"
	if underHome(o.dir()) || underHome(o.DataDir) {
		protectHome = "read-only"
	}
	return `[Unit]
Description=SkinLoom (云织) Minecraft skin site
Documentation=https://skinloom.cn
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
` + user + `WorkingDirectory=` + strings.ReplaceAll(o.dir(), "%", "%%") + `
ExecStart=` + systemdQuote(append([]string{o.Exe}, o.args()...)) + `
Restart=on-failure
RestartSec=5
TimeoutStopSec=60
LimitNOFILE=65535

# Hardening. The frontend (Node.js) needs JIT, so MemoryDenyWriteExecute stays off.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=` + strings.Join(rw, " ") + `
ProtectHome=` + protectHome + `
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictSUIDSGID=true
RestrictRealtime=true
RestrictNamespaces=true
LockPersonality=true
SystemCallArchitectures=native
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
`
}

// systemdPath quotes a path of a path list (ReadWritePaths=), which expands
// specifiers (%) but not variables ($).
func systemdPath(p string) string {
	p = strings.ReplaceAll(p, "%", "%%")
	if !strings.ContainsAny(p, " \t\"'\\") {
		return p
	}
	return `"` + strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(p) + `"`
}

// systemdQuote quotes each argument of a command line (ExecStart=) that needs it
// ("…", with \ and " escaped, $ and % doubled).
func systemdQuote(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\"'\\$%;") {
			out[i] = a
			continue
		}
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `$$`, `%`, `%%`)
		out[i] = `"` + r.Replace(a) + `"`
	}
	return strings.Join(out, " ")
}

// New returns the service for o. run serves the site until its context is cancelled;
// it is used only by Run.
func New(o Options, run func(ctx context.Context) error) (service.Service, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	p := &program{run: run}
	s, err := service.New(p, o.config())
	if err != nil {
		return nil, err
	}
	p.logger, _ = s.Logger(nil) // the system log: Event Log, syslog, journal
	return s, nil
}

// ErrNotInstalled is returned for a service that is not installed.
var ErrNotInstalled = service.ErrNotInstalled

// Install registers the service and gives it write access to what it writes.
func Install(o Options) error {
	if strings.Contains(o.Exe, "go-build") {
		return errors.New("this binary is a temporary one from go run: build skinloom or use a release bundle")
	}
	s, err := New(o, nil)
	if err != nil {
		return err
	}
	// Registering first fails fast without the rights to; prepare's changes (owners,
	// ACLs) then happen only for a service that exists, and are undone with it.
	if err := s.Install(); err != nil {
		return privileged(err)
	}
	if err := prepare(o); err != nil {
		_ = s.Uninstall()
		return err
	}
	return nil
}

// Control runs one of install's companions: uninstall, start, stop, restart.
func Control(o Options, action string) error {
	s, err := New(o, nil)
	if err != nil {
		return err
	}
	return privileged(service.Control(s, action))
}

// Status reports running, stopped or ErrNotInstalled.
func Status(o Options) (string, error) {
	s, err := New(o, nil)
	if err != nil {
		return "", err
	}
	st, err := s.Status()
	if err != nil {
		return "", err
	}
	switch st {
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	}
	return "unknown", nil
}

// Interactive is false when the process was started by the service manager.
func Interactive() bool { return service.Interactive() }

// privileged makes the permission error of a plain shell say what to do.
func privileged(err error) error {
	if err != nil && (errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "access is denied") ||
		strings.Contains(err.Error(), "permission denied")) {
		return fmt.Errorf("%w\nrun it from an administrator terminal (Windows) or with sudo", err)
	}
	return err
}

// Run serves under the service manager: it returns when the service is stopped.
func Run(s service.Service) error { return s.Run() }

type program struct {
	run    func(ctx context.Context) error
	logger service.Logger
	cancel context.CancelFunc
	done   chan error
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.done = cancel, make(chan error, 1)
	go func() {
		err := p.run(ctx)
		if ctx.Err() == nil {
			// The site stopped on its own (a config error, say): exit, so the service
			// manager restarts it, instead of looking healthy while serving nothing.
			// The system log gets the reason too: a Windows service has no console, and
			// the error may come before the log file is open.
			slog.Error("SkinLoom stopped", "err", err)
			if p.logger != nil {
				_ = p.logger.Error(fmt.Sprintf("SkinLoom stopped: %v", err))
			}
			os.Exit(1)
		}
		p.done <- err
	}()
	return nil
}

func (p *program) Stop(service.Service) error {
	p.cancel()
	select {
	case err := <-p.done:
		return err
	case <-time.After(50 * time.Second): // below TimeoutStopSec; the site's own shutdown takes up to ~35s
		return errors.New("SkinLoom did not stop within 50 seconds")
	}
}
