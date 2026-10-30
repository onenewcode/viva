package windows

import (
	"testing"

	"github.com/vivaui/viva/internal/platform"
)

func TestGuardFaultReportsAPanicAndAnswersEFail(t *testing.T) {
	var got platform.Fault
	platform.SetFaultHandler(func(fault platform.Fault) { got = fault })
	defer platform.SetFaultHandler(nil)

	result := func() (result uintptr) {
		defer guardFault("test callback", &result)
		var provider *uiaProvider
		return uintptr(provider.References.Load()) // a nil provider, as a stale COM pointer would give
	}()

	if result != eFail {
		t.Fatalf("result = %#x, want E_FAIL", result)
	}
	if got.Where != "test callback" || len(got.Stack) == 0 {
		t.Fatalf("fault = %+v", got)
	}
}
