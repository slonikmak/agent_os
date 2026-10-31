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

type inboxReaderStub struct {
	readerFunc
	read func(context.Context, string) (queries.InboxDetails, error)
}

func (r inboxReaderStub) GetInboxMessage(ctx context.Context, id string) (queries.InboxDetails, error) {
	return r.read(ctx, id)
}
func TestInboxNativeReadMappingFaultPrivacyAndResponseBudget(t *testing.T) {
	for _, test := range []string{"saved", "fault", "panic", "unavailable", "budget", "invalid"} {
		t.Run(test, func(t *testing.T) {
			message := contractfixture.Message()
			diag := diagnostics.New()
			reader := inboxReaderStub{read: func(context.Context, string) (queries.InboxDetails, error) {
				switch test {
				case "fault":
					return queries.InboxDetails{}, errors.New("PRIVATE SQL PATH")
				case "panic":
					panic("PRIVATE SQL PATH")
				case "unavailable":
					return queries.InboxDetails{}, queries.Unavailable()
				case "budget":
					message.Text = strings.Repeat("x", v1.MaxResponseBytes)
				case "invalid":
					message.Text = ""
				}
				return queries.InboxDetails{WorkspaceRevision: 2, Message: message}, nil
			}}
			result := New(reader, diag, "").GetInboxMessage(context.Background(), message.ID)
			if test == "saved" {
				if result.Error != nil || result.Data == nil {
					t.Fatal(result.Error)
				}
				want, _ := json.Marshal(v1.FromInboxDetails(queries.InboxDetails{WorkspaceRevision: 2, Message: message}))
				got, _ := json.Marshal(result.Data)
				if string(want) != string(got) {
					t.Fatal("mapping drift")
				}
				return
			}
			if result.Error == nil || result.Data != nil {
				t.Fatal("missing failure")
			}
			code := "internal_error"
			if test == "unavailable" {
				code = "service_unavailable"
			}
			if test == "budget" {
				code = "response_too_large"
			}
			if result.Error.Code != code || len(result.Error.RequestID) != 32 {
				t.Fatal(result.Error)
			}
			class := ""
			if code == "internal_error" {
				class = "internal"
				if test == "panic" {
					class = "panic"
				}
			}
			assertNativeFailure(t, diag, result.Error, "inbox.read", code, class)
			data, _ := json.Marshal(result)
			records, _ := json.Marshal(diag.Snapshot())
			if strings.Contains(string(data)+string(records), "PRIVATE") {
				t.Fatal("fault content leaked")
			}
		})
	}
}
