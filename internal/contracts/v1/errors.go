package v1

import "personaldesk/internal/application/failure"

func FromFailure(f *failure.Failure, requestID, errorID string) Error {
	safe := failure.Safe(f)
	return Error{Code: safe.Code, Message: safe.Message, Retryable: safe.Retryable, RequestID: requestID, ErrorID: errorID, CurrentRevision: safe.CurrentRevision}
}
