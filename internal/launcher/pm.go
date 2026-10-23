package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// PackageManagers in order of preference. bun is the project's primary package manager
// (its lockfile is committed); pnpm and npm are fallbacks for machines without bun.
var PackageManagers = []string{"bun", "pnpm", "npm"}

// FindPackageManager returns the package manager to use and its executable path.
// want is "auto" (or empty) to take the first one installed, or a specific name.
func FindPackageManager(want string, lookPath func(string) (string, error)) (name, path string, err error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if want == "" || want == "auto" {
		for _, pm := range PackageManagers {
			if p, err := lookPath(pm); err == nil {
				return pm, p, nil
			}
		}
		return "", "", errors.New("no package manager found: install bun (https://bun.sh), pnpm or npm (comes with Node.js)")
	}
	for _, pm := range PackageManagers {
		if pm == want {
			p, err := lookPath(pm)
			if err != nil {
				return "", "", fmt.Errorf("web.package_manager = %q but %s is not on PATH", want, want)
			}
			return pm, p, nil
		}
	}
	return "", "", fmt.Errorf("unknown package manager %q (want auto, bun, pnpm or npm)", want)
}

// DevArgs is the argv (after the executable) that runs the frontend's dev script with
// the given port, for each package manager's argument convention.
func DevArgs(pm string, port int) []string {
	args := []string{"run", "dev"}
	switch pm {
	case "bun":
		// bun echoes the script command on stderr, which the launcher would log as a warning
		args = []string{"run", "--silent", "dev"}
	case "npm":
		args = append(args, "--") // npm only forwards arguments after "--"
	}
	return append(args, "--port", strconv.Itoa(port), "--hostname", "127.0.0.1")
}

// devCommand builds the dev-mode command: the frontend app's own "dev" script, run in
// web/apps/web so it works the same under every package manager.
func devCommand(spec Spec) (*exec.Cmd, error) {
	pm, bin, err := FindPackageManager(spec.PackageManager, nil)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(spec.DevDir, "node_modules")); err != nil {
		return nil, fmt.Errorf("frontend dependencies are not installed: run `%s install` in %s", pm, spec.DevDir)
	}
	cmd := exec.Command(bin, DevArgs(pm, spec.Port)...)
	cmd.Dir = filepath.Join(spec.DevDir, "apps", "web")
	return cmd, nil
}
