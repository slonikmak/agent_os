package wailsread

import (
	"personaldesk/internal/adapters/diagnostics"
	v1 "personaldesk/internal/contracts/v1"
)

// DiagnosticFacade is operational only. It holds no reader, command, provider,
// path opener or domain lifecycle capability. Native Save supplies the destination.
type DiagnosticFacade struct {
	diag   *diagnostics.Buffer
	export func() v1.ExportResult
}

func NewDiagnostics(diag *diagnostics.Buffer, export ...func() v1.ExportResult) *DiagnosticFacade {
	f := &DiagnosticFacade{diag: diag}
	if len(export) > 0 {
		f.export = export[0]
	}
	return f
}
func (f *DiagnosticFacade) ReportFrontendFault(report string) diagnostics.FrontendResult {
	return f.diag.ReportFrontend(report)
}
func (f *DiagnosticFacade) GetDiagnosticStatus() diagnostics.Status { return f.diag.Status() }

func (f *DiagnosticFacade) ExportDiagnostics() v1.ExportResult {
	if f.export == nil {
		id := diagnostics.ID()
		return v1.ExportResult{Status: "failed", ErrorID: &id}
	}
	return f.export()
}
