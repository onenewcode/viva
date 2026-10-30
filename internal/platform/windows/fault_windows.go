package windows

import (
	"syscall"

	"github.com/vivaui/viva/internal/platform"
)

// The system calls into this package from threads and moments the program does
// not choose: a UI Automation client asks an element a question, the shell drops
// files on the window, the font enumerator reports a family. A Go panic that
// leaves a callback ends the whole process, with nothing recorded. Every callback
// handed to the system is therefore made by one of the functions below: a panic
// is reported through platform.ReportFault and answered with E_FAIL, which the
// caller treats as that one question failing.

const eFail = 0x80004005

// guardFault is deferred by a callback wrapper: it recovers a panic, reports it
// and sets the callback's result to E_FAIL.
func guardFault(where string, result *uintptr) {
	if recovered := recover(); recovered != nil {
		platform.ReportFault(where, recovered)
		*result = eFail
	}
}

func guarded1(where string, function func(uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(a uintptr) (result uintptr) {
		defer guardFault(where, &result)
		return function(a)
	})
}

func guarded2(where string, function func(uintptr, uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(a, b uintptr) (result uintptr) {
		defer guardFault(where, &result)
		return function(a, b)
	})
}

func guarded3(where string, function func(uintptr, uintptr, uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(a, b, c uintptr) (result uintptr) {
		defer guardFault(where, &result)
		return function(a, b, c)
	})
}

func guarded4(where string, function func(uintptr, uintptr, uintptr, uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(a, b, c, d uintptr) (result uintptr) {
		defer guardFault(where, &result)
		return function(a, b, c, d)
	})
}

func guarded5(where string, function func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr) uintptr {
	return syscall.NewCallback(func(a, b, c, d, e uintptr) (result uintptr) {
		defer guardFault(where, &result)
		return function(a, b, c, d, e)
	})
}
