//go:build windows

package fsperm

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A private folder and the files created in it afterwards grant access to exactly the
// current user, SYSTEM and Administrators, and inherit nothing from above.
func TestPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Private(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "key")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{user.User.Sid.String(), "S-1-5-18", "S-1-5-32-544"}
	slices.Sort(want)
	for _, p := range []string{dir, file} {
		if got := trustees(t, p); !slices.Equal(got, want) {
			t.Errorf("%s grants %v, want %v", p, got, want)
		}
	}

	// A file of its own, outside a private folder.
	log := filepath.Join(t.TempDir(), "skinloom.log")
	if err := os.WriteFile(log, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Private(log); err != nil {
		t.Fatal(err)
	}
	if got := trustees(t, log); !slices.Equal(got, want) {
		t.Errorf("file grants %v, want %v", got, want)
	}
}

// A path that is already private keeps its list, so an administrator running a command
// does not take away the access skinloom service install gave LOCAL SERVICE.
func TestPrivateKeepsAPrivateList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	setDACL(t, dir, "D:P(A;OICI;FA;;;"+user.User.Sid.String()+")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1301bf;;;S-1-5-19)", true)
	if err := Private(dir); err != nil {
		t.Fatal(err)
	}
	want := []string{user.User.Sid.String(), "S-1-5-18", "S-1-5-19", "S-1-5-32-544"}
	slices.Sort(want)
	if got := trustees(t, dir); !slices.Equal(got, want) {
		t.Errorf("grants %v, want %v", got, want)
	}
}

// A folder the account may use but not re-permission (the owner's rights limited to
// Modify, as for a folder another account made) is left alone instead of failing:
// the service must still start in a data directory first used interactively.
func TestPrivateLeavesWhatItCannotChange(t *testing.T) {
	parent := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	// OW (OWNER RIGHTS) with Modify takes away the owner's implicit right to change
	// permissions; the parent keeps full control for the cleanup.
	setDACL(t, parent, "D:P(A;;FA;;;"+user.User.Sid.String()+")(A;OICIIO;0x1301bf;;;OW)", true)
	dir := filepath.Join(parent, "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Private(dir); err != nil {
		t.Fatalf("Private: %v", err)
	}
	if got := trustees(t, dir); !slices.Equal(got, []string{"S-1-3-4"}) {
		t.Errorf("grants %v, want it unchanged (OWNER RIGHTS only)", got)
	}
}

func setDACL(t *testing.T, path, sddl string, protected bool) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	if protected {
		info |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

// trustees lists the SIDs path's DACL allows, sorted, and fails on a deny entry.
func trustees(t *testing.T, path string) []string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	var sids []string
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("%s: ace type %d", path, ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if s := sid.String(); !slices.Contains(sids, s) {
			sids = append(sids, s)
		}
	}
	slices.Sort(sids)
	return sids
}
