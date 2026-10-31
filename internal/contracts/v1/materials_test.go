package v1

import (
	"strings"
	"testing"
)

func TestUploadRejectsLossySurrogateEscapesAndPreservesLiteralText(t *testing.T) {
	for _, text := range []string{`\ud800`, `\udc00`, `\ud800x`, `\ud800\u0000`} {
		_, err := Decode[UploadMaterial]("upload-material", strings.NewReader(`{"expectedTaskRevision":1,"mediaType":"text/plain","text":"`+text+`"}`), MaxBodyBytes)
		if err == nil {
			t.Fatal("lossy text accepted", text)
		}
	}
	for _, text := range []string{`\ud83d\udc4b`, `\\ud800`, `\ufffd`} {
		_, err := Decode[UploadMaterial]("upload-material", strings.NewReader(`{"expectedTaskRevision":1,"mediaType":"text/plain","text":"`+text+`"}`), MaxBodyBytes)
		if err != nil {
			t.Fatal("valid exact text refused", text, err)
		}
	}
}
