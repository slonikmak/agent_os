package v1

// The versioned allowlist also governs future capture/export. Optional event
// fields are omitted; query/manifest unknown values are explicit null.
const (
	MaxDiagnosticRecordBytes    = 16 << 10
	MaxFrontendReportBytes      = 4096
	MaxArchiveBytes             = 30 << 20
	MaxActiveLogBytes           = 5 << 20
	RetainedLogBackups          = 4
	FrontendReportsPerMinute    = 20
	FrontendRepeatWindowSeconds = 60
)

const ManifestEntry = "manifest.json"
const DiagnosticLogEntry = "logs/diagnostics.jsonl"
const ReproductionEntry = "reproduction-steps.txt"

func ArchiveEntries() [3]string {
	return [3]string{ManifestEntry, DiagnosticLogEntry, ReproductionEntry}
}

type Frame struct {
	Location string `json:"location"`
	Line     int    `json:"line"`
}
type DiagnosticEvent struct {
	SchemaVersion int     `json:"schemaVersion"`
	At            string  `json:"at"`
	Level         string  `json:"level"`
	Code          string  `json:"code"`
	Component     string  `json:"component"`
	Operation     string  `json:"operation"`
	RequestID     string  `json:"requestId,omitempty"`
	ErrorID       string  `json:"errorId,omitempty"`
	SessionID     string  `json:"sessionId,omitempty"`
	DurationMs    *int64  `json:"durationMs,omitempty"`
	AppVersion    string  `json:"appVersion,omitempty"`
	BuildVersion  string  `json:"buildVersion,omitempty"`
	ErrorClass    string  `json:"errorClass,omitempty"`
	Frames        []Frame `json:"frames,omitempty"`
}
type FrontendFault struct {
	SchemaVersion int     `json:"schemaVersion"`
	Code          string  `json:"code"`
	Component     string  `json:"component"`
	Frames        []Frame `json:"frames"`
}
type Coverage struct {
	Complete         bool  `json:"complete"`
	AcceptedRecords  int64 `json:"acceptedRecords"`
	OmittedRecords   int64 `json:"omittedRecords"`
	UnavailableFiles int   `json:"unavailableFiles"`
	TruncatedRecords int64 `json:"truncatedRecords"`
}
type RuntimeVersions struct {
	Go      *string `json:"go"`
	Wails   *string `json:"wails"`
	Webview *string `json:"webview"`
}
type Manifest struct {
	SchemaVersion        int             `json:"schemaVersion"`
	CreatedAt            string          `json:"createdAt"`
	AppVersion           string          `json:"appVersion"`
	BuildVersion         string          `json:"buildVersion"`
	OS                   *string         `json:"os"`
	OSVersion            *string         `json:"osVersion"`
	Arch                 *string         `json:"arch"`
	Runtime              RuntimeVersions `json:"runtime"`
	SchemaStatus         string          `json:"schemaStatus"`
	SchemaVersionApplied *int64          `json:"schemaVersionApplied"`
	SessionID            *string         `json:"sessionId"`
	ErrorID              *string         `json:"errorId"`
	SharedDiagnostics    string          `json:"sharedDiagnostics"`
	Coverage             Coverage        `json:"coverage"`
}
type ExportResult struct {
	Status   string    `json:"status"`
	Coverage *Coverage `json:"coverage"`
	ErrorID  *string   `json:"errorId"`
}

// Archive is the technical export policy inventory; no filesystem/export behavior
// is implemented here. Entries is a fresh fixed-name list for each caller.
type Archive struct {
	SchemaVersion               int      `json:"schemaVersion"`
	Entries                     []string `json:"entries"`
	MaxUncompressedBytes        int      `json:"maxUncompressedBytes"`
	MaxRecordBytes              int      `json:"maxRecordBytes"`
	MaxLogFiles                 int      `json:"maxLogFiles"`
	MaxActiveLogBytes           int      `json:"maxActiveLogBytes"`
	RetainedBackups             int      `json:"retainedBackups"`
	FrontendReportsPerMinute    int      `json:"frontendReportsPerMinute"`
	FrontendRepeatWindowSeconds int      `json:"frontendRepeatWindowSeconds"`
	MaxFrontendReportBytes      int      `json:"maxFrontendReportBytes"`
}

func DescribeArchive() Archive {
	entries := ArchiveEntries()
	return Archive{1, entries[:], MaxArchiveBytes, MaxDiagnosticRecordBytes, RetainedLogBackups + 1, MaxActiveLogBytes, RetainedLogBackups, FrontendReportsPerMinute, FrontendRepeatWindowSeconds, MaxFrontendReportBytes}
}
