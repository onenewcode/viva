package platform

import (
	"strings"
	"testing"
)

func TestReportFaultCarriesTheStackOfThePanic(t *testing.T) {
	var got Fault
	SetFaultHandler(func(fault Fault) { got = fault })
	defer SetFaultHandler(nil)

	func() {
		defer func() { ReportFault("test boundary", recover()) }()
		var table map[string]int
		table["x"] = 1
	}()

	if got.Where != "test boundary" || !strings.Contains(got.Message, "nil map") {
		t.Fatalf("fault = %q, %q", got.Where, got.Message)
	}
	if !strings.Contains(string(got.Stack), "TestReportFaultCarriesTheStackOfThePanic") {
		t.Fatalf("the stack does not reach the panicking function:\n%s", got.Stack)
	}
}

func TestReportFaultWithoutAHandlerIsHarmless(t *testing.T) {
	SetFaultHandler(nil)
	ReportFault("nowhere", "boom")
}

func TestAFailingHandlerDoesNotEscape(t *testing.T) {
	SetFaultHandler(func(Fault) { panic("handler broke") })
	defer SetFaultHandler(nil)
	ReportFault("somewhere", "boom")
}
