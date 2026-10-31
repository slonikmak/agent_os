package failure

import (
	"errors"
	"fmt"
	"testing"
)

func TestRecoveredPanicClassificationSurvivesInternalSanitization(t *testing.T) {
	recovered := RecoveredPanic()
	if recovered.Code != "internal_error" || !IsRecoveredPanic(recovered) || !IsRecoveredPanic(Safe(recovered)) || !IsRecoveredPanic(fmt.Errorf("boundary: %w", recovered)) {
		t.Fatal("fixed panic classification lost")
	}
	for _, ordinary := range []error{New("internal_error"), New("invalid_request"), errors.New("PRIVATE /path"), Safe(New("revision_conflict"))} {
		if IsRecoveredPanic(ordinary) {
			t.Fatal("ordinary outcome classified as panic")
		}
	}
}
