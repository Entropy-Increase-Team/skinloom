//go:build windows

package cli

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/importer/bs"
)

// The import report names accounts and their email addresses. Mode 0600 means nothing
// on Windows, so the file must stop inheriting its folder's permissions (ADR 0039), as
// the logs and backups do.
func TestImportReportIsPrivateOnWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import", "bs-report.txt")
	r := &bs.Report{Imported: map[string]int{}}
	if err := writeReport(&app.Paths{}, r, true, &path); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("%s inherits its folder's permissions", path)
	}
}
