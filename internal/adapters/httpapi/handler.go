// Package httpapi confines and authenticates the local read/receive API.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"personaldesk/internal/application/intake"
	"personaldesk/internal/application/materials"
	"personaldesk/internal/contracts"
	"strings"
	"time"

	"personaldesk/internal/adapters/diagnostics"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"personaldesk/internal/application/queries"
	v1 "personaldesk/internal/contracts/v1"
)

type Handler struct {
	reader   queries.Reader
	auth     *access.Authenticator
	host     string
	diag     *diagnostics.Buffer
	receiver intake.Receiver
	creator  intake.TaskCreator
	uploader materials.Uploader
}

func NewMaterials(reader queries.Reader, auth *access.Authenticator, host string, diag *diagnostics.Buffer, receiver intake.Receiver, creator intake.TaskCreator, uploader materials.Uploader) *Handler {
	h := NewCommands(reader, auth, host, diag, receiver, creator)
	h.uploader = uploader
	return h
}

func NewCommands(reader queries.Reader, auth *access.Authenticator, host string, diag *diagnostics.Buffer, receiver intake.Receiver, creator intake.TaskCreator) *Handler {
	h := New(reader, auth, host, diag, receiver)
	h.creator = creator
	return h
}

func New(reader queries.Reader, auth *access.Authenticator, host string, diag *diagnostics.Buffer, receivers ...intake.Receiver) *Handler {
	h := &Handler{reader: reader, auth: auth, host: host, diag: diag}
	if len(receivers) > 0 {
		h.receiver = receivers[0]
	}
	return h
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := diagnostics.ID()
	operationName := "framework"
	mutation := false
	started := time.Now()
	defer func() { h.diag.Operation("httpapi", operationName, requestID, started) }()
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	defer func() {
		if recover() != nil {
			if mutation {
				h.diag.RequireRestart()
			}
			h.faultAt(w, requestID, operationName, "panic")
		}
	}()
	if r.Host != h.host || len(r.Header.Values("Host")) != 0 || len(r.Header.Values("Origin")) != 0 {
		h.reject(w, 403, "forbidden", requestID)
		return
	}
	auth := r.Header.Values("Authorization")
	var principal access.Principal
	var authenticated bool
	if len(auth) == 1 {
		principal, authenticated = h.auth.Authenticate(auth[0])
	}
	if !authenticated {
		h.reject(w, 401, "unauthorized", requestID)
		return
	}
	versions := r.Header.Values("X-API-Version")
	if len(versions) > 1 || (len(versions) == 1 && versions[0] != "v1") || unsupportedMajorVersion(r.URL.Path) {
		h.reject(w, 400, "unsupported_api_version", requestID)
		return
	}

	receive := r.Method == http.MethodPost && r.URL.Path == "/api/v1/inbox/messages" && h.receiver != nil
	resolutionPath, resolutionPrefix := strings.CutPrefix(r.URL.Path, "/api/v1/inbox/messages/")
	resolutionID, resolutionSuffix := strings.CutSuffix(resolutionPath, "/resolutions")
	create := r.Method == http.MethodPost && h.creator != nil && resolutionPrefix && resolutionSuffix && queries.ValidID(resolutionID)
	inboxReader, canReadInbox := h.reader.(queries.InboxReader)
	detailID := strings.TrimPrefix(r.URL.Path, "/api/v1/inbox/messages/")
	detail := r.Method == http.MethodGet && canReadInbox && strings.HasPrefix(r.URL.Path, "/api/v1/inbox/messages/") && detailID != "" && !strings.Contains(detailID, "/")
	taskReader, canReadTask := h.reader.(queries.TaskReader)
	taskID := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	taskDetail := r.Method == http.MethodGet && canReadTask && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/") && taskID != "" && !strings.Contains(taskID, "/")
	uploadID, uploadSuffix := strings.CutSuffix(taskID, "/materials")
	upload := r.Method == http.MethodPost && h.uploader != nil && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/") && uploadSuffix && uploadID != "" && !strings.Contains(uploadID, "/")
	materialReader, canReadMaterial := h.reader.(queries.MaterialReader)
	materialID, materialPrefix := strings.CutPrefix(r.URL.Path, "/api/v1/materials/")
	materialDetail := r.Method == http.MethodGet && canReadMaterial && materialPrefix && materialID != "" && !strings.Contains(materialID, "/")
	workspace := r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspace"
	discovery := r.Method == http.MethodGet && r.URL.Path == "/api/v1"
	if !receive && !create && !upload && !materialDetail && !detail && !taskDetail && !workspace && !discovery {
		h.reject(w, 404, "not_found", requestID)
		return
	}
	if r.URL.RawQuery != "" {
		h.reject(w, 400, "invalid_request", requestID)
		return
	}
	operation := access.ReadWorkspace
	if receive {
		operation = access.Receive
	} else if create {
		operation = access.ResolveCreateTask
	} else if detail {
		operation = access.ReadInbox
	} else if taskDetail {
		operation = access.ReadTask
	} else if upload {
		operation = access.UploadMaterial
	} else if materialDetail {
		operation = access.ReadMaterial
	}
	if !access.Allowed(principal, operation) {
		h.reject(w, 403, "forbidden", requestID)
		return
	}
	switch {
	case receive:
		operationName = "inbox.receive"
	case create:
		operationName = "inbox.resolve.createTask"
	case detail:
		operationName = "inbox.read"
	case taskDetail:
		operationName = "task.read"
	case materialDetail:
		operationName = "material.read"
	case workspace:
		operationName = "workspace.read"
	case discovery:
		operationName = "discovery.read"
	}
	// material upload has no released v1 diagnostic operation; framework is the
	// truthful generic boundary, rather than mislabelling it as another command.
	if h.diag.Status().RestartRequired {
		h.reject(w, 503, "service_unavailable", requestID)
		return
	}
	mutation = receive || create || upload
	w = &operationWriter{ResponseWriter: w, operation: operationName, mutation: mutation}
	ctx, cancel := context.WithTimeout(access.WithPrincipal(r.Context(), principal), 3*time.Second)
	defer cancel()
	if receive || create || upload {
		if f := ValidateMutationHeaders(r.Header); f != nil {
			h.publicFailure(w, f, requestID)
			return
		}
		if upload {
			input, err := v1.Decode[v1.UploadMaterial]("upload-material", r.Body, v1.MaxBodyBytes)
			if err != nil {
				if errors.Is(err, contracts.ErrTooLarge) {
					h.reject(w, 413, "payload_too_large", requestID)
				} else {
					h.reject(w, 400, "invalid_request", requestID)
				}
				return
			}
			result, status, err := h.uploader.UploadMaterial(ctx, uploadID, r.Header.Get("Idempotency-Key"), input.Input(principal))
			if err != nil {
				h.applicationFailure(w, err, requestID)
				return
			}
			h.write(w, status, v1.FromUploadMaterialResult(result), requestID)
			return
		}
		if create {
			input, err := v1.Decode[v1.CreateTask]("create-task", r.Body, v1.MaxBodyBytes)
			if err != nil {
				if errors.Is(err, contracts.ErrTooLarge) {
					h.reject(w, 413, "payload_too_large", requestID)
				} else {
					h.reject(w, 400, "invalid_request", requestID)
				}
				return
			}
			result, status, err := h.creator.CreateTask(ctx, resolutionID, r.Header.Get("Idempotency-Key"), input.Input(principal))
			if err != nil {
				h.applicationFailure(w, err, requestID)
				return
			}
			h.write(w, status, v1.FromCreateTaskResult(result), requestID)
			return
		}
		input, err := v1.Decode[v1.Receive]("receive", r.Body, v1.MaxBodyBytes)
		if err != nil {
			code := "invalid_request"
			status := 400
			if errors.Is(err, contracts.ErrTooLarge) {
				code = "payload_too_large"
				status = 413
			}
			h.reject(w, status, code, requestID)
			return
		}
		result, status, err := h.receiver.ReceiveMessage(ctx, r.Header.Get("Idempotency-Key"), input.Input(principal))
		if err != nil {
			h.applicationFailure(w, err, requestID)
			return
		}
		h.write(w, status, v1.FromReceiveResult(result), requestID)
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 0 {
		h.reject(w, 400, "invalid_request", requestID)
		return
	}
	media := r.Header.Values("Content-Type")
	if len(media) > 1 || (len(media) == 1 && media[0] != "application/json" && media[0] != "application/json; charset=utf-8") {
		h.reject(w, 415, "unsupported_media_type", requestID)
		return
	}
	body := http.MaxBytesReader(w, r.Body, v1.MaxBodyBytes)
	n, err := io.Copy(io.Discard, body)
	_ = body.Close()
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			h.reject(w, 413, "payload_too_large", requestID)
		} else {
			h.reject(w, 400, "invalid_request", requestID)
		}
		return
	}
	if n != 0 {
		h.reject(w, 400, "invalid_request", requestID)
		return
	}
	if discovery {
		description := v1.Describe()
		if canReadTask {
			description.Capabilities = append(description.Capabilities, "tasks.read")
		}
		if canReadInbox {
			description.Capabilities = append(description.Capabilities, "inbox.read")
		}
		if h.receiver != nil {
			description.Capabilities = append(description.Capabilities, "inbox.receive")
		}
		if h.creator != nil {
			description.Capabilities = append(description.Capabilities, "inbox.resolve.createTask")
		}
		if canReadMaterial && h.uploader != nil {
			description.APIRevision = 2
			description.Capabilities = append(description.Capabilities, "materials.read", "materials.upload")
		}
		h.write(w, 200, description, requestID)
		return
	}
	if materialDetail {
		result, err := materialReader.ReadMaterial(ctx, materialID)
		if err != nil {
			h.applicationFailure(w, err, requestID)
			return
		}
		h.write(w, 200, v1.FromMaterialResult(result), requestID)
		return
	}
	if taskDetail {
		result, err := taskReader.GetTaskDetails(ctx, taskID)
		if err != nil {
			h.applicationFailure(w, err, requestID)
			return
		}
		h.write(w, 200, v1.FromTaskDetails(result), requestID)
		return
	}
	if detail {
		result, err := inboxReader.GetInboxMessage(ctx, detailID)
		if err != nil {
			h.applicationFailure(w, err, requestID)
			return
		}
		h.write(w, 200, v1.FromInboxDetails(result), requestID)
		return
	}
	snapshot, err := h.reader.GetWorkspaceSnapshot(ctx)
	if err != nil {
		h.applicationFailure(w, err, requestID)
		return
	}
	h.write(w, 200, v1.FromSnapshot(snapshot), requestID)
}
func (h *Handler) applicationFailure(w http.ResponseWriter, err error, requestID string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		h.publicFailure(w, failure.New("service_unavailable"), requestID)
		return
	}
	if failure.IsRecoveredPanic(err) {
		operation := "framework"
		if own, ok := w.(*operationWriter); ok {
			operation = own.operation
			if own.mutation {
				h.diag.RequireRestart()
			}
		}
		h.faultAt(w, requestID, operation, "panic")
		return
	}
	var f *failure.Failure
	if errors.As(err, &f) && f.Code != "internal_error" {
		h.publicFailure(w, f, requestID)
		return
	}
	h.fault(w, requestID)
}

func (h *Handler) fault(w http.ResponseWriter, requestID string) {
	operation := "framework"
	if own, ok := w.(*operationWriter); ok {
		operation = own.operation
	}
	h.faultAt(w, requestID, operation, "internal")
}
func (h *Handler) faultAt(w http.ResponseWriter, requestID, operation, class string) {
	errorID := h.diag.Fault("httpapi", operation, requestID, class)
	h.write(w, 500, v1.ErrorEnvelope{Error: v1.FromFailure(failure.New("internal_error"), requestID, errorID)}, requestID)
}
func (h *Handler) reject(w http.ResponseWriter, status int, code, requestID string) {
	h.write(w, status, v1.ErrorEnvelope{Error: v1.FromFailure(failure.New(code), requestID, "")}, requestID)
}
func (h *Handler) publicFailure(w http.ResponseWriter, f *failure.Failure, requestID string) {
	safe := failure.Safe(f)
	status := map[string]int{"invalid_request": 400, "unsupported_api_version": 400, "unsupported_operation": 400, "unauthorized": 401, "forbidden": 403, "not_found": 404, "revision_conflict": 409, "idempotency_conflict": 409, "message_identity_conflict": 409, "terminal_resolution": 409, "payload_too_large": 413, "unsupported_media_type": 415, "service_unavailable": 503, "response_too_large": 503}[safe.Code]
	if status == 0 {
		h.fault(w, requestID)
		return
	}
	h.write(w, status, v1.ErrorEnvelope{Error: v1.FromFailure(safe, requestID, "")}, requestID)
}
func (h *Handler) write(w http.ResponseWriter, status int, value any, requestID string) {
	data, err := json.Marshal(value)
	if err != nil {
		h.fault(w, requestID)
		return
	}
	if len(data) > v1.MaxResponseBytes {
		h.reject(w, 503, "response_too_large", requestID)
		return
	}
	schema := ""
	switch value.(type) {
	case v1.WorkspaceSnapshot:
		schema = "workspace"
	case v1.InboxDetails:
		schema = "inbox-details"
	case v1.TaskDetails:
		schema = "task-details"
	case v1.ReceiveResult:
		schema = "receive-result"
	case v1.CreateTaskResult:
		schema = "create-task-result"
	case v1.MaterialResult:
		schema = "material"
	case v1.UploadMaterialResult:
		schema = "upload-material-result"
	case v1.Discovery:
		schema = "discovery"
		if value.(v1.Discovery).APIRevision == 2 {
			schema = "discovery-revision2"
		}
	}
	if schema != "" {
		if _, err = v1.Validate(schema, bytes.NewReader(data), v1.MaxResponseBytes); err != nil {
			h.fault(w, requestID)
			return
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

type operationWriter struct {
	http.ResponseWriter
	operation string
	mutation  bool
}
