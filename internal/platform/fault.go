package platform

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"
)

// Fault is a panic a backend caught at a boundary the operating system calls into
// (a window procedure, a COM method, a native callback). The backend turns it into
// an error or a failure code, so without a report the cause would leave no trace.
type Fault struct {
	// Where names the boundary that caught it.
	Where string
	// Message is the panic value as text.
	Message string
	// Stack is the goroutine's stack at the panic.
	Stack []byte
}

var faultHandler atomic.Pointer[func(Fault)]

// SetFaultHandler installs the function that receives every caught fault, or
// removes it when handler is nil. It may be called from any goroutine.
func SetFaultHandler(handler func(Fault)) {
	if handler == nil {
		faultHandler.Store(nil)
		return
	}
	faultHandler.Store(&handler)
}

// ReportFault hands a recovered panic value to the installed handler. It must be
// called from the deferred function that recovered it, so the stack is the
// panicking one. It never panics itself.
func ReportFault(where string, recovered any) {
	handler := faultHandler.Load()
	if handler == nil {
		return
	}
	defer func() { _ = recover() }()
	(*handler)(Fault{Where: where, Message: fmt.Sprint(recovered), Stack: debug.Stack()})
}
