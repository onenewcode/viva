package windows

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	// WerRegisterAppLocalDump is exported by kernelbase.dll; wer.dll does not
	// export it, so a lookup there never succeeds.
	kernelBase            = systemDLL("kernelbase.dll")
	werRegisterAppLocal   = kernelBase.NewProc("WerRegisterAppLocalDump")
	errCrashDumpsDisabled = errors.New("windows: native crash dumps need Windows 11 22H2 or later")
)

// EnableCrashDumps asks Windows Error Reporting to leave a dump of this process
// in %LOCALAPPDATA%\relative when it dies of a native fault: a driver crash, a
// fast-fail, heap corruption. Go's own crash output never sees those, because
// the process is ended by the system before the runtime can run. It reports the
// absolute directory. relative must name a folder below LocalAppData.
func EnableCrashDumps(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "..") {
		return "", fmt.Errorf("windows: crash dump folder %q must be relative to LocalAppData", relative)
	}
	if werRegisterAppLocal.Find() != nil {
		return "", errCrashDumpsDisabled
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("windows: LOCALAPPDATA is not set")
	}
	path, err := syscall.UTF16PtrFromString(relative)
	if err != nil {
		return "", err
	}
	if status, _, _ := werRegisterAppLocal.Call(uintptr(unsafe.Pointer(path))); hresult(status) != nil {
		return "", fmt.Errorf("windows: register crash dump folder: %w", hresult(status))
	}
	return filepath.Join(base, relative), nil
}
