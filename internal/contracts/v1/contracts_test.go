package v1

import (
	"encoding/json"
	"reflect"
	"testing"

	"personaldesk/internal/application/queries"
)

func TestPublishedExamplesMatchImplementedDTOs(t *testing.T) {
	snapshot := queries.WorkspaceSnapshot{WorkspaceID: "00000000000000000000000000000001", WorkspaceRevision: 1, CreatedAt: "2026-10-29T00:00:00Z"}
	examples := map[string]any{
		"workspace": FromSnapshot(snapshot),
		"discovery": Describe(),
		"error":     ErrorEnvelope{Error: Error{Code: "internal_error", Message: "The workspace could not be read", RequestID: "00000000000000000000000000000001", ErrorID: "00000000000000000000000000000002"}},
	}
	for name, value := range examples {
		t.Run(name, func(t *testing.T) {
			expectedBytes, err := Examples.ReadFile("examples/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			actualBytes, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var expected, actual any
			if err = json.Unmarshal(expectedBytes, &expected); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(actualBytes, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(expected, actual) {
				t.Fatalf("published example differs from DTO: %s", actualBytes)
			}
		})
	}
}
