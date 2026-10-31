package wailsread

import (
	"context"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
	"time"
)

type MaterialReadResult struct {
	Data  *MaterialResult `json:"data"`
	Error *Failure        `json:"error"`
}

func (f *Facade) ReadMaterial(ctx context.Context, id string) (result MaterialReadResult) {
	requestID := diagnostics.ID()
	defer f.diag.Operation("wailsread", "material.read", requestID, time.Now())
	fault := func() MaterialReadResult {
		return MaterialReadResult{Error: f.faultOperation(requestID, "material.read", "internal").Error}
	}
	defer func() {
		if recover() != nil {
			result = MaterialReadResult{Error: f.faultOperation(requestID, "material.read", "panic").Error}
		}
	}()
	reader, ok := f.reader.(queries.MaterialReader)
	if !ok || f.diag.Status().RestartRequired {
		return MaterialReadResult{Error: publicFailure(failure.New("service_unavailable"), requestID, nil)}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	detail, err := reader.ReadMaterial(ctx, id)
	if err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return MaterialReadResult{Error: safe}
		}
		return fault()
	}
	view := FromMaterialResult(detail)
	if err := validateResponse("material", view); err != nil {
		if safe, ok := expectedFailure(err, requestID); ok {
			return MaterialReadResult{Error: safe}
		}
		return fault()
	}
	return MaterialReadResult{Data: &view}
}
