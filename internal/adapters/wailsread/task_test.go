package wailsread

import (
	"context"
	"encoding/json"
	"errors"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
	"personaldesk/internal/testsupport/contractfixture"
	"strings"
	"testing"
)

type taskReaderStub struct {
	readerFunc
	read func(context.Context, string) (queries.TaskDetails, error)
}

func (r taskReaderStub) GetTaskDetails(ctx context.Context, id string) (queries.TaskDetails, error) {
	return r.read(ctx, id)
}
func TestTaskNativeMappingFaultPrivacyBudgetAndValidation(t *testing.T) {
	for _, mode := range []string{"saved", "fault", "panic", "unavailable", "budget", "invalid", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			task := contractfixture.Task()
			diag := diagnostics.New()
			reader := taskReaderStub{read: func(ctx context.Context, id string) (queries.TaskDetails, error) {
				if _, ok := ctx.Deadline(); !ok || id != task.ID {
					t.Fatal("missing bounded task context")
				}
				switch mode {
				case "fault":
					return queries.TaskDetails{}, errors.New("PRIVATE SQL PATH")
				case "panic":
					panic("PRIVATE SQL PATH")
				case "unavailable":
					return queries.TaskDetails{}, queries.Unavailable()
				case "unknown":
					return queries.TaskDetails{}, &queries.Failure{Code: "PRIVATE", Message: "PRIVATE"}
				case "budget":
					task.Timeline[0].Reason = strings.Repeat("x", v1.MaxResponseBytes)
				case "invalid":
					task.CreatedAt = "PRIVATE PATH"
				}
				return queries.TaskDetails{WorkspaceRevision: 3, Task: task}, nil
			}}
			result := New(reader, diag, "").GetTaskDetails(context.Background(), task.ID)
			if mode == "saved" {
				want, _ := json.Marshal(v1.FromTaskDetails(queries.TaskDetails{WorkspaceRevision: 3, Task: task}))
				got, _ := json.Marshal(result.Data)
				if result.Error != nil || string(want) != string(got) {
					t.Fatal("native task mapping drift", result.Error)
				}
				return
			}
			code := "internal_error"
			if mode == "unavailable" {
				code = "service_unavailable"
			}
			if mode == "budget" {
				code = "response_too_large"
			}
			if result.Data != nil || result.Error == nil || result.Error.Code != code || len(result.Error.RequestID) != 32 || (code == "internal_error" && result.Error.ErrorID == nil) {
				t.Fatal(result)
			}
			class := ""
			if code == "internal_error" {
				class = "internal"
				if mode == "panic" {
					class = "panic"
				}
			}
			assertNativeFailure(t, diag, result.Error, "task.read", code, class)
			data, _ := json.Marshal(result)
			records, _ := json.Marshal(diag.Snapshot())
			if strings.Contains(string(data)+string(records), "PRIVATE") {
				t.Fatal("raw task fault leaked")
			}
		})
	}
}
