package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	v1 "personaldesk/internal/contracts/v1"
	"strings"
)

type mutationHeaders struct {
	Authorization  string `json:"authorization"`
	IdempotencyKey string `json:"idempotencyKey"`
	ContentType    string `json:"contentType"`
	APIVersion     string `json:"apiVersion"`
}

// ValidateMutationHeaders is a reusable boundary, not a registered operation.
// Authorization/role policy must precede its use and any workflow/replay lookup.
func ValidateMutationHeaders(headers http.Header) *failure.Failure {
	if len(headers.Values("Authorization")) != 1 || !strings.HasPrefix(headers.Get("Authorization"), "Bearer ") || !access.ValidToken(strings.TrimPrefix(headers.Get("Authorization"), "Bearer ")) {
		return failure.New("unauthorized")
	}
	if len(headers.Values("Idempotency-Key")) != 1 {
		return failure.New("invalid_request")
	}
	if len(headers.Values("Content-Type")) != 1 {
		return failure.New("unsupported_media_type")
	}
	versions := headers.Values("X-API-Version")
	if len(versions) > 1 || (len(versions) == 1 && versions[0] != "v1") {
		return failure.New("unsupported_api_version")
	}
	contentType := headers.Get("Content-Type")
	if contentType != "application/json" && contentType != "application/json; charset=utf-8" {
		return failure.New("unsupported_media_type")
	}
	data, _ := json.Marshal(mutationHeaders{headers.Get("Authorization"), headers.Get("Idempotency-Key"), contentType, "v1"})
	if _, err := v1.Validate("mutation-headers", bytes.NewReader(data), 1024); err != nil {
		return failure.New("invalid_request")
	}
	return nil
}

// Unknown routes still use not_found; only a numeric major-version segment is
// recognized as an explicit unsupported API version.
func unsupportedMajorVersion(path string) bool {
	if !strings.HasPrefix(path, "/api/v") {
		return false
	}
	segment := strings.SplitN(strings.TrimPrefix(path, "/api/"), "/", 2)[0]
	digits := strings.TrimPrefix(segment, "v")
	return digits != "" && strings.Trim(digits, "0123456789") == "" && segment != "v1"
}
