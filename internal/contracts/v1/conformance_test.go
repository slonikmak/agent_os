package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/queries"
	"personaldesk/internal/contracts"
	"personaldesk/internal/testsupport/contractfixture"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := Examples.ReadFile("examples/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func typedRoundTrip[T any](t *testing.T, schema string, data []byte) {
	t.Helper()
	value, err := Decode[T](schema, bytes.NewReader(data), MaxResponseBytes)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Validate(schema, bytes.NewReader(encoded), MaxResponseBytes); err != nil {
		t.Fatalf("typed mapping no longer conforms: %s", encoded)
	}
	var expected, actual any
	if err = json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("typed inventory lost fields: %s", encoded)
	}
}
func TestEveryPublishedFixtureUsesExecutableSchemasAndTypedInventories(t *testing.T) {
	entries, err := Examples.ReadDir("examples")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")
		data := fixture(t, name)
		t.Run(name, func(t *testing.T) {
			switch name {
			case "workspace", "workspace-populated":
				typedRoundTrip[WorkspaceSnapshot](t, "workspace", data)
			case "discovery":
				typedRoundTrip[Discovery](t, "discovery", data)
			case "discovery-revision2":
				typedRoundTrip[Discovery](t, name, data)
			case "upload-material":
				typedRoundTrip[UploadMaterial](t, name, data)
			case "upload-material-result":
				typedRoundTrip[UploadMaterialResult](t, name, data)
			case "error":
				typedRoundTrip[ErrorEnvelope](t, "error", data)
			case "task-summary":
				typedRoundTrip[TaskSummary](t, name, data)
			case "task-details":
				typedRoundTrip[TaskDetails](t, name, data)
			case "inbox-metadata":
				typedRoundTrip[InboxMetadata](t, name, data)
			case "inbox-details":
				typedRoundTrip[InboxDetails](t, name, data)
			case "receive", "receive-omitted":
				typedRoundTrip[Receive](t, "receive", data)
			case "create-task":
				typedRoundTrip[CreateTask](t, name, data)
			case "receive-result":
				typedRoundTrip[ReceiveResult](t, name, data)
			case "create-task-result":
				typedRoundTrip[CreateTaskResult](t, name, data)
			case "material", "material-unavailable":
				typedRoundTrip[MaterialResult](t, "material", data)
			case "diagnostic-event":
				typedRoundTrip[DiagnosticEvent](t, name, data)
			case "frontend-fault":
				typedRoundTrip[FrontendFault](t, name, data)
			case "manifest":
				typedRoundTrip[Manifest](t, name, data)
			case "archive":
				typedRoundTrip[Archive](t, name, data)
			case "export-exported", "export-cancelled", "export-failed":
				typedRoundTrip[ExportResult](t, "export-result", data)
			default:
				t.Fatalf("fixture lacks conformance mapping: %s", name)
			}
		})
	}
}

func TestApplicationMappingsMatchPublishedFacts(t *testing.T) {
	values := map[string]any{
		"workspace-populated": FromSnapshot(contractfixture.Snapshot()),
		"archive":             DescribeArchive(),
		"task-details":        FromTaskDetails(queries.TaskDetails{WorkspaceRevision: 3, Task: contractfixture.Task()}),
		"inbox-details":       FromInboxDetails(queries.InboxDetails{WorkspaceRevision: 2, Message: contractfixture.Message()}),
		"receive-result":      FromReceiveResult(intake.ReceiveResult{WorkspaceRevision: 2, Message: contractfixture.Message()}),
		"create-task-result":  FromCreateTaskResult(intake.CreateTaskResult{WorkspaceRevision: 3, Message: contractfixture.AppliedMessage(), Task: contractfixture.Task()}),
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			actual, _ := json.Marshal(value)
			var a, b any
			_ = json.Unmarshal(actual, &a)
			_ = json.Unmarshal(fixture(t, name), &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("mapping differs: %s", actual)
			}
		})
	}
}

func TestCreationPresenceAndNormalization(t *testing.T) {
	omitted, err := Decode[Receive]("receive", bytes.NewReader(fixture(t, "receive-omitted")), MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	nulls, err := Decode[Receive]("receive", bytes.NewReader(fixture(t, "receive")), MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if omitted.Sender.Set || !nulls.Sender.Set || nulls.Sender.Value != nil {
		t.Fatal("lost omission/null presence")
	}
	valueData := strings.Replace(string(fixture(t, "receive")), `"sender": null`, `"sender": "Claimed sender"`, 1)
	value, err := Decode[Receive]("receive", strings.NewReader(valueData), MaxBodyBytes)
	if err != nil || !value.Sender.Set || value.Sender.Value == nil || *value.Sender.Value != "Claimed sender" {
		t.Fatal("lost value presence")
	}
	input := omitted.Input(contractfixture.Submitter())
	if input.Sender != nil || input.OccurredAt != nil || input.SourceURL != nil || input.TaskRef != nil || input.Artifacts == nil || len(input.Artifacts) != 0 {
		t.Fatal("creation normalization invented defaults")
	}
	if input.SubmittedBy != contractfixture.Submitter() || value.Input(contractfixture.Submitter()).SubmittedBy != input.SubmittedBy {
		t.Fatal("claimed fields replaced authenticated identity")
	}
	creation, err := Decode[CreateTask]("create-task", bytes.NewReader(fixture(t, "create-task")), MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !creation.Task.Summary.Set || creation.Task.Summary.Value != nil || creation.Input(contractfixture.Coordinator()).AppliedBy != contractfixture.Coordinator() {
		t.Fatal("creation/coordinator normalization")
	}
}

func TestMalformedJSONNeverProducesPartialTypedInputOrPrivateError(t *testing.T) {
	valid := string(fixture(t, "receive"))
	tests := map[string]string{
		"unknown":            strings.Replace(valid, `"source":`, `"privateMessage":"SECRET /private/home", "source":`, 1),
		"case-alias":         strings.Replace(valid, `"source":`, `"Source":`, 1),
		"duplicate":          strings.Replace(valid, `"source":`, `"source":"SECRET", "source":`, 1),
		"invalid-utf8":       strings.Replace(valid, "manual", string([]byte{0xff}), 1),
		"trailing":           valid + ` {"secret":"SECRET"}`,
		"depth":              strings.Repeat("[", contracts.MaxDepth+1) + "0" + strings.Repeat("]", contracts.MaxDepth+1),
		"required-null":      strings.Replace(valid, `"source": "manual"`, `"source": null`, 1),
		"invalid-enum":       strings.Replace(valid, "user_request", "execute_provider", 1),
		"invalid-time":       strings.Replace(valid, `"occurredAt": null`, `"occurredAt": "2026-02-30T12:00:00Z"`, 1),
		"non-utc-time":       strings.Replace(valid, `"occurredAt": null`, `"occurredAt": "2026-10-29T12:00:00+00:00"`, 1),
		"blank-text":         strings.Replace(valid, "Clarify the export behavior", "   ", 1),
		"artifacts-null":     valid[:strings.Index(valid, `"artifacts"`)] + `"artifacts":null}`,
		"material-authority": strings.Replace(valid, `"kind": "file"`, `"materialId":"M-0001", "kind": "file"`, 1),
		"nested-duplicate":   strings.Replace(valid, `"coordinate":`, `"coordinate":"SECRET", "coordinate":`, 1),
		"nested-alias":       strings.Replace(valid, `"coordinate":`, `"Coordinate":`, 1),
		"excessive-text":     strings.Replace(valid, "Clarify the export behavior", strings.Repeat("x", 65537), 1),
		"whitespace-tail":    valid + strings.Repeat(" ", MaxBodyBytes),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			value, err := Decode[Receive]("receive", strings.NewReader(data), MaxBodyBytes)
			if err == nil {
				t.Fatal("accepted malformed input")
			}
			if value.Source != "" || value.Artifacts != nil {
				t.Fatal("partial typed input escaped")
			}
			if err.Error() != contracts.ErrInvalid.Error() && !errors.Is(err, contracts.ErrTooLarge) {
				t.Fatal("unsafe error text")
			}
		})
	}
	create := string(fixture(t, "create-task"))
	for _, revision := range []string{"0", "-1", "1.5", "1.0", "1e0", "1e999999999", "9007199254740992", "9007199254740991.1", `"1"`, "null"} {
		data := strings.Replace(create, `"expectedMessageRevision": 1`, `"expectedMessageRevision": `+revision, 1)
		if _, err := Validate("create-task", strings.NewReader(data), MaxBodyBytes); err == nil {
			t.Fatalf("schema boundary accepted revision token %s", revision)
		}
		if _, err := Decode[CreateTask]("create-task", strings.NewReader(data), MaxBodyBytes); err == nil {
			t.Fatalf("typed decoder accepted revision %s", revision)
		}
	}
	for _, change := range []string{strings.Replace(create, `"title":`, `"Title":`, 1), strings.Replace(create, `"title":`, `"title":"SECRET", "title":`, 1), strings.Replace(create, `"kind": "request"`, `"kind": null`, 1), strings.Replace(create, `"title": "Clarify export behavior"`, `"title": null`, 1)} {
		if _, err := Decode[CreateTask]("create-task", strings.NewReader(change), MaxBodyBytes); err == nil {
			t.Fatal("accepted nested invalid task")
		}
	}
	// A max-safe revision remains exact through schema and typed mapping.
	data := strings.Replace(create, `"expectedMessageRevision": 1`, `"expectedMessageRevision": 9007199254740991`, 1)
	value, err := Decode[CreateTask]("create-task", strings.NewReader(data), MaxBodyBytes)
	if err != nil || value.ExpectedMessageRevision != queries.MaxRevision {
		t.Fatal("lost numeric precision")
	}
}

func TestQueryMaterialProvenanceAndDiagnosticRejections(t *testing.T) {
	tests := []struct{ schema, fixture, old, new string }{
		{"task-summary", "task-summary", `"summary": null,`, ``},
		{"workspace", "workspace-populated", `"status": "not_configured"`, `"status": "invented"`},
		{"material", "material", `"id": "M-0001"`, `"id": "../credentials.json"`},
		{"material", "material-unavailable", `"text": null`, `"text": "SECRET"`},
		{"inbox-details", "inbox-details", `"submittedBy": {`, `"submittedBy": {"providerVerified":true,`},
		{"create-task-result", "create-task-result", `"role": "coordinator"`, `"role": "worker"`},
		{"diagnostic-event", "diagnostic-event", `"code": "query.fault"`, `"code": "SECRET"`},
		{"diagnostic-event", "diagnostic-event", `"line": 52`, `"line": 52, "file":"/private/home"`},
		{"diagnostic-event", "diagnostic-event", `"schemaVersion": 1`, `"schemaVersion": 2`},
		{"diagnostic-event", "diagnostic-event", `"requestId": "11111111111111111111111111111111"`, `"requestId": "SECRET"`},
		{"frontend-fault", "frontend-fault", `"component": "root"`, `"component": "root", "message":"SECRET"`},
		{"frontend-fault", "frontend-fault", `"location": "frontend.render"`, `"location": "queries.workspace"`},
		{"manifest", "manifest", `"go": "go1.27.3"`, `"go": "/private/home/SECRET"`},
		{"manifest", "manifest", `"omittedRecords": 2`, `"omittedRecords": -1`},
		{"archive", "archive", `"logs/diagnostics.jsonl"`, `"../credentials.json"`},
		{"archive", "archive", `"maxRecordBytes": 16384`, `"maxRecordBytes": 999999`},
		{"export-result", "export-cancelled", `"errorId": null`, `"errorId": "11111111111111111111111111111111"`},
	}
	for _, test := range tests {
		t.Run(test.schema+"/"+test.old, func(t *testing.T) {
			data := strings.Replace(string(fixture(t, test.fixture)), test.old, test.new, 1)
			if _, err := Validate(test.schema, strings.NewReader(data), MaxResponseBytes); err == nil {
				t.Fatalf("accepted invalid boundary: %s", test.schema)
			}
		})
	}
	// Coordinates remain inert metadata even when they name a forbidden service path.
	data := strings.Replace(string(fixture(t, "receive")), "synthetic/report.md", "../credentials.json", 1)
	input, err := Decode[Receive]("receive", strings.NewReader(data), MaxBodyBytes)
	if err != nil || input.Artifacts[0].Coordinate != "../credentials.json" {
		t.Fatal("metadata coordinate unexpectedly became file authority")
	}
}

func TestSchemaResourcesCannotLoadOutsideEmbeddedSet(t *testing.T) {
	if _, err := (embeddedOnly{}).Load("https://example.com/private.schema.json"); err == nil {
		t.Fatal("remote loader enabled")
	}
	schemas, err := schemaCache()
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) < 20 {
		t.Fatal("missing schema inventories")
	}
}

func TestArrayScalarAndTechnicalBudgets(t *testing.T) {
	receive := map[string]any{}
	if err := json.Unmarshal(fixture(t, "receive"), &receive); err != nil {
		t.Fatal(err)
	}
	artifacts := receive["artifacts"].([]any)
	for len(artifacts) < 33 {
		artifacts = append(artifacts, artifacts[0])
	}
	receive["artifacts"] = artifacts
	data, _ := json.Marshal(receive)
	if _, err := Validate("receive", bytes.NewReader(data), MaxBodyBytes); err == nil {
		t.Fatal("artifact array budget ignored")
	}
	frontend := FrontendFault{SchemaVersion: 1, Code: "ui.render_fault", Component: "root", Frames: []Frame{}}
	for range 17 {
		frontend.Frames = append(frontend.Frames, Frame{Location: "frontend.render", Line: 1})
	}
	data, _ = json.Marshal(frontend)
	if _, err := Validate("frontend-fault", bytes.NewReader(data), MaxFrontendReportBytes); err == nil {
		t.Fatal("frame array budget ignored")
	}
	snapshot := FromSnapshot(contractfixture.Snapshot())
	for len(snapshot.Tasks) < 10001 {
		snapshot.Tasks = append(snapshot.Tasks, snapshot.Tasks[0])
	}
	data, _ = json.Marshal(snapshot)
	if _, err := Validate("workspace", bytes.NewReader(data), MaxResponseBytes); err == nil {
		t.Fatal("workspace array budget ignored")
	}
	title := strings.Replace(string(fixture(t, "task-summary")), "Clarify export behavior", strings.Repeat("x", 257), 1)
	if _, err := Validate("task-summary", strings.NewReader(title), MaxResponseBytes); err == nil {
		t.Fatal("title budget ignored")
	}
	event := string(fixture(t, "diagnostic-event")) + strings.Repeat(" ", MaxDiagnosticRecordBytes)
	if _, err := Validate("diagnostic-event", strings.NewReader(event), MaxDiagnosticRecordBytes); !errors.Is(err, contracts.ErrTooLarge) {
		t.Fatal("diagnostic byte budget ignored")
	}
	manifest := strings.Replace(string(fixture(t, "manifest")), `"complete": false`, `"complete": true`, 1)
	if _, err := Validate("manifest", strings.NewReader(manifest), MaxResponseBytes); err == nil {
		t.Fatal("coverage claimed complete with omitted records")
	}
	archive, err := Decode[Archive]("archive", bytes.NewReader(fixture(t, "archive")), 4096)
	if err != nil {
		t.Fatal(err)
	}
	entries := ArchiveEntries()
	if !reflect.DeepEqual(archive.Entries, entries[:]) || archive.MaxUncompressedBytes != MaxArchiveBytes || archive.MaxRecordBytes != MaxDiagnosticRecordBytes || archive.MaxFrontendReportBytes != MaxFrontendReportBytes || archive.FrontendReportsPerMinute != FrontendReportsPerMinute || archive.FrontendRepeatWindowSeconds != FrontendRepeatWindowSeconds || archive.MaxActiveLogBytes != MaxActiveLogBytes || archive.RetainedBackups != RetainedLogBackups {
		t.Fatal("technical budgets drifted from schema")
	}
}
