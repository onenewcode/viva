package app

import "github.com/vivaui/viva/internal/platform"

// Fault is a panic the framework caught where the operating system calls into the
// program: a window procedure, a UI Automation or drag-and-drop method, a native
// callback. The framework fails the window or answers the system with an error
// instead of letting the process die, which also hides the cause; a Fault carries
// it out.
type Fault struct {
	// Where names the boundary that caught the panic.
	Where string
	// Message is the panic value as text.
	Message string
	// Stack is the stack of the panicking goroutine.
	Stack []byte
}

// SetFaultHandler installs the function that receives every Fault, so a program
// can write it to its log. It runs on the thread that panicked, must return
// quickly and must not call back into the framework. Nil removes it. It is safe
// to call from any goroutine.
func SetFaultHandler(handler func(Fault)) {
	if handler == nil {
		platform.SetFaultHandler(nil)
		return
	}
	platform.SetFaultHandler(func(fault platform.Fault) {
		handler(Fault{Where: fault.Where, Message: fault.Message, Stack: fault.Stack})
	})
}

// EnableCrashDumps asks the system to leave a dump of the process when it dies of
// a fault no Go code can see: a driver crash, a fast-fail, a corrupted heap. On
// Windows (11 22H2 or later) it registers a folder below %LOCALAPPDATA% for
// Windows Error Reporting and returns its absolute path; a dump is a copy of
// memory, so a program should tell its user before asking for one to be shared.
// Elsewhere it returns ErrUnsupported: macOS keeps its own crash reports and a
// Linux system its core files.
func EnableCrashDumps(relativeDir string) (string, error) {
	return nativeEnableCrashDumps(relativeDir)
}

// ShowFatalError tells the user, in the system's own message box, that the
// program cannot start or go on. It is for a program with no window to show the
// reason in: without it a failed start is a window that flickers and is gone. It
// blocks until the box is dismissed and does nothing where the system offers no
// box (a Linux session without zenity or kdialog, the web). Call it from the
// main goroutine, after RunApplication has returned or before it starts.
func ShowFatalError(title, message string) {
	nativeShowFatalError(title, message)
}
