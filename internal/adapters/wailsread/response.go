package wailsread

import (
	"bytes"
	"encoding/json"

	"personaldesk/internal/application/failure"
	v1 "personaldesk/internal/contracts/v1"
)

// validateResponse keeps the native byte budget and saved-data schema policy in
// one place. Each query method owns correlation and recovery for invalid data.
func validateResponse(schema string, view any) error {
	data, err := json.Marshal(view)
	if err != nil {
		return err
	}
	if len(data) > v1.MaxResponseBytes {
		return failure.New("response_too_large")
	}
	_, err = v1.Validate(schema, bytes.NewReader(data), v1.MaxResponseBytes)
	return err
}
