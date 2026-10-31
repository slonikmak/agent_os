package wailsread

import (
	"context"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
	"time"
)

type InboxResult struct {
	Data  *InboxDetails `json:"data"`
	Error *Failure      `json:"error"`
}

func (f *Facade) GetInboxMessage(ctx context.Context, id string) (result InboxResult) {
	requestID := diagnostics.ID()
	defer f.diag.Operation("wailsread", "inbox.read", requestID, time.Now())
	fault := func() InboxResult {
		return InboxResult{Error: f.faultOperation(requestID, "inbox.read", "internal").Error}
	}
	defer func() {
		if recover() != nil {
			result = InboxResult{Error: f.faultOperation(requestID, "inbox.read", "panic").Error}
		}
	}()
	reader, ok := f.reader.(queries.InboxReader)
	if !ok || f.diag.Status().RestartRequired {
		return InboxResult{Error: publicFailure(failure.New("service_unavailable"), requestID, nil)}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	detail, err := reader.GetInboxMessage(ctx, id)
	if err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return InboxResult{Error: safe}
		}
		return fault()
	}
	view := FromInboxDetails(detail)
	if err := validateResponse("inbox-details", view); err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return InboxResult{Error: safe}
		}
		return fault()
	}
	return InboxResult{Data: &view}
}
