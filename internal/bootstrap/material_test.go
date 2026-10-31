package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/adapters/wailsread"
	v1 "personaldesk/internal/contracts/v1"
	"reflect"
	"strings"
	"testing"
)

func TestActualMaterialHTTPNativeParityRestartAndRoleBounds(t *testing.T) {
	root := newRoot(t)
	w := start(t, root)
	credentials, err := loadCredentials(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, role, key, body string) ([]byte, int) {
		t.Helper()
		r, _ := http.NewRequest(method, discovery(t, root).Endpoint+path, strings.NewReader(body))
		token := credentials.Reader
		if role == "worker" {
			token = credentials.Worker
		} else if role == "coordinator" {
			token = credentials.Coordinator
		}
		r.Header.Set("Authorization", "Bearer "+token)
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", key)
		}
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return data, response.StatusCode
	}
	data, status := request("POST", "/api/v1/inbox/messages", "worker", "receive", `{"source":"manual","scope":"synthetic","messageId":"materials","kind":"user_request","text":"Keep original context","artifacts":[{"kind":"file","coordinate":"/private/credentials.json","label":null}]}`)
	if status != 201 {
		t.Fatal(status, string(data))
	}
	var received v1.ReceiveResult
	_ = json.Unmarshal(data, &received)
	data, status = request("POST", "/api/v1/inbox/messages/"+received.Message.ID+"/resolutions", "coordinator", "create", `{"expectedMessageRevision":1,"action":"createTask","reason":"Separate decision","task":{"kind":"request","title":"Material reader"}}`)
	if status != 201 {
		t.Fatal(status, string(data))
	}
	var created v1.CreateTaskResult
	_ = json.Unmarshal(data, &created)
	path := "/api/v1/tasks/" + created.Task.ID + "/materials"
	body := `{"expectedTaskRevision":1,"mediaType":"text/markdown","text":"# Saved 👋\n<script>inert</script>\nhttps://example.invalid/"}`
	for _, role := range []string{"reader", "worker"} {
		data, status = request("POST", path, role, "forbidden", body)
		if status != 403 {
			t.Fatal(status, string(data))
		}
	}
	data, status = request("POST", path, "coordinator", "upload", body)
	if status != 201 {
		t.Fatal(status, string(data))
	}
	receipt := string(data)
	var upload v1.UploadMaterialResult
	_ = json.Unmarshal(data, &upload)
	for _, role := range []string{"reader", "worker", "coordinator"} {
		data, status = request("GET", "/api/v1/materials/"+upload.MaterialID, role, "", "")
		if status != 200 {
			t.Fatal(status, string(data))
		}
		native := wailsread.New(w, diagnostics.New(), "").ReadMaterial(context.Background(), upload.MaterialID)
		encoded, _ := json.Marshal(native.Data)
		if native.Error != nil || string(encoded) != string(data) {
			t.Fatal("native material differs", native.Error)
		}
	}
	for _, bad := range []string{`{"expectedTaskRevision":2,"mediaType":"text/html","text":"x"}`, `{"expectedTaskRevision":2,"mediaType":"text/plain","text":"\u0000"}`, `{"expectedTaskRevision":2,"mediaType":"text/plain","text":"x","path":"/private/path"}`, `{"expectedTaskRevision":2,"mediaType":"text/plain","text":"x","label":" "}`} {
		data, status = request("POST", path, "coordinator", "bad", bad)
		if status != 400 {
			t.Fatal(status, string(data))
		}
	}
	for _, id := range []string{"M-unknown", "workspace.sqlite"} {
		data, status = request("GET", "/api/v1/materials/"+id, "reader", "", "")
		if status != 404 {
			t.Fatal("unassociated name has authority", status, string(data))
		}
	}
	data, status = request("GET", "/api/v1/materials/%20", "reader", "", "")
	if status != 400 {
		t.Fatal(status, string(data))
	}
	data, status = request("GET", "/api/v1", "reader", "", "")
	if status != 200 {
		t.Fatal(status)
	}
	var discovery v1.Discovery
	_ = json.Unmarshal(data, &discovery)
	if discovery.APIRevision != 2 || !strings.Contains(string(data), "materials.upload") {
		t.Fatal(string(data))
	}
	saved, _ := w.GetTaskDetails(context.Background(), created.Task.ID)
	if len(saved.Task.Materials) != 2 || saved.Task.Materials[0].MaterialID != nil {
		t.Fatal("original coordinate changed")
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = start(t, root)
	after, _ := w.GetTaskDetails(context.Background(), created.Task.ID)
	if !reflect.DeepEqual(saved, after) {
		t.Fatal("restart lost association")
	}
	data, status = request("POST", path, "coordinator", "upload", body)
	if status != 201 || string(data) != receipt {
		t.Fatal("restart upload replay drift", status, string(data))
	}
}
