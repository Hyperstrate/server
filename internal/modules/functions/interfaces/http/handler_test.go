package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/pagination"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesMountsAdminAndInferEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	handler := NewHandler(&stubService{}, &stubRunnerService{})
	handler.RegisterAdminRoutes(engine.Group("/functions"))
	handler.RegisterInferRoutes(engine.Group("/functions"))
	handler.RegisterRunnerRoutes(engine.Group("/functions"))

	routes := map[string]bool{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	expected := []string{
		"POST /functions/apps",
		"POST /functions/apps/:appId/functions",
		"POST /functions/runner-pools",
		"POST /functions/:functionId/invocations",
		"GET /functions/invocations/:invocationId",
		"GET /functions/invocations/:invocationId/logs",
		"POST /functions/runner/agents/register",
		"POST /functions/runner/agents/heartbeat",
		"POST /functions/runner/invocations/lease",
		"POST /functions/runner/invocations/:invocationId/logs",
		"POST /functions/runner/invocations/:invocationId/complete",
	}
	for _, route := range expected {
		if !routes[route] {
			t.Fatalf("expected route %q to be registered", route)
		}
	}
}

func TestCreateAppBindsInputAndReturnsCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubService{
		createApp: func(_ context.Context, input application.CreateAppInput) (*application.AppResponse, error) {
			if input.Name != "image-pipeline" || input.Description != "Python workers" {
				t.Fatalf("unexpected create app input: %+v", input)
			}
			return &application.AppResponse{ID: "fapp_123", Name: input.Name, Description: input.Description}, nil
		},
	}
	engine := gin.New()
	NewHandler(svc, &stubRunnerService{}).RegisterAdminRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/apps", map[string]any{
		"name":        "image-pipeline",
		"description": "Python workers",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.AppResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "fapp_123" || body.Name != "image-pipeline" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestDeployFunctionUsesAppParamAndReturnsCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubService{
		deployFunction: func(_ context.Context, appID string, input application.DeployFunctionInput) (*application.FunctionResponse, error) {
			if appID != "fapp_123" {
				t.Fatalf("appID = %q", appID)
			}
			if input.Name != "summarize" || input.Entrypoint != "main.handle" || input.Image.Base != "python:3.12-slim" {
				t.Fatalf("unexpected deploy input: %+v", input)
			}
			return &application.FunctionResponse{
				ID:               "fn_123",
				AppID:            appID,
				Name:             input.Name,
				Entrypoint:       input.Entrypoint,
				Status:           domain.FunctionStatusDeploying,
				ActiveRevisionID: "frev_123",
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(svc, &stubRunnerService{}).RegisterAdminRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/apps/fapp_123/functions", map[string]any{
		"name":       "summarize",
		"entrypoint": "main.handle",
		"image": map[string]any{
			"base": "python:3.12-slim",
		},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.FunctionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "fn_123" || body.AppID != "fapp_123" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestInvokeFunctionReturnsAcceptedQueuedInvocation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubService{
		invokeFunction: func(_ context.Context, functionID string, input application.InvokeFunctionInput) (*application.InvocationResponse, error) {
			if functionID != "fn_123" {
				t.Fatalf("functionID = %q", functionID)
			}
			if input.Mode != domain.InvocationModeAsync || input.Payload["text"] != "hello" {
				t.Fatalf("unexpected invoke input: %+v", input)
			}
			return &application.InvocationResponse{
				ID:         "finv_123",
				FunctionID: functionID,
				Mode:       input.Mode,
				Status:     domain.InvocationStatusQueued,
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(svc, &stubRunnerService{}).RegisterInferRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/fn_123/invocations", map[string]any{
		"mode": "async",
		"payload": map[string]any{
			"text": "hello",
		},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.InvocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "finv_123" || body.Status != domain.InvocationStatusQueued {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestListInvocationLogsReturnsOrderedLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubService{
		listInvocationLogs: func(_ context.Context, invocationID string, slice pagination.Slice) (pagination.Paginated[application.LogResponse], error) {
			if invocationID != "finv_123" {
				t.Fatalf("invocationID = %q", invocationID)
			}
			if slice.Page != 2 || slice.PerPage != 1 {
				t.Fatalf("unexpected pagination slice: %+v", slice)
			}
			items := []application.LogResponse{
				{ID: "flog_1", InvocationID: invocationID, Seq: 1, Stream: domain.LogStreamSystem, Message: "starting"},
			}
			return pagination.New(items, 2, slice), nil
		},
	}
	engine := gin.New()
	NewHandler(svc, &stubRunnerService{}).RegisterInferRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodGet, "/functions/invocations/finv_123/logs?page=2&perPage=1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body pagination.Paginated[application.LogResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Meta.Total != 2 || body.Meta.Page != 2 || body.Meta.PerPage != 1 || len(body.Items) != 1 || body.Items[0].Message != "starting" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestGetInvocationReturnsInvocation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := &stubService{
		getInvocation: func(_ context.Context, invocationID string) (*application.InvocationResponse, error) {
			if invocationID != "finv_123" {
				t.Fatalf("invocationID = %q", invocationID)
			}
			return &application.InvocationResponse{ID: invocationID, Status: domain.InvocationStatusSucceeded}, nil
		},
	}
	engine := gin.New()
	NewHandler(svc, &stubRunnerService{}).RegisterInferRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodGet, "/functions/invocations/finv_123", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.InvocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "finv_123" || body.Status != domain.InvocationStatusSucceeded {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestAppendInvocationLogUsesBearerTokenWithoutJSONRunnerCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		appendInvocationLog: func(_ context.Context, invocationID string, input application.AppendRunnerLogInput) (*application.LogResponse, error) {
			if invocationID != "finv_123" || input.AgentID != "" || input.SessionToken != "hsra_header" || input.LeaseID != "fls_123" || input.Stream != domain.LogStreamStdout || input.Message != "hello" {
				t.Fatalf("unexpected append log call: invocation=%q input=%+v", invocationID, input)
			}
			return &application.LogResponse{
				ID:           "flog_123",
				InvocationID: invocationID,
				Stream:       input.Stream,
				Message:      input.Message,
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSONWithHeaders(engine, http.MethodPost, "/functions/runner/invocations/finv_123/logs", map[string]any{
		"leaseId": "fls_123",
		"stream":  "stdout",
		"message": "hello",
	}, map[string]string{
		"Authorization": "Bearer hsra_header",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAppendInvocationLogReturnsUnauthorizedForRunnerAuthError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		appendInvocationLog: func(_ context.Context, _ string, _ application.AppendRunnerLogInput) (*application.LogResponse, error) {
			return nil, domain.ErrRunnerUnauthorized
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSONWithHeaders(engine, http.MethodPost, "/functions/runner/invocations/finv_123/logs", map[string]any{
		"leaseId": "fls_123",
		"stream":  "stdout",
		"message": "hello",
	}, map[string]string{
		"Authorization": "Bearer hsra_header",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAppendInvocationLogMapsMissingInvocationToNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		appendInvocationLog: func(_ context.Context, invocationID string, input application.AppendRunnerLogInput) (*application.LogResponse, error) {
			if invocationID != "finv_missing" || input.AgentID != "fragent_123" || input.LeaseID != "fls_123" || input.Stream != domain.LogStreamStdout || input.Message != "hello" {
				t.Fatalf("unexpected append log call: invocation=%q input=%+v", invocationID, input)
			}
			return nil, domain.ErrInvocationNotFound
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/runner/invocations/finv_missing/logs", map[string]any{
		"agentId":      "fragent_123",
		"sessionToken": "hsra_session",
		"leaseId":      "fls_123",
		"stream":       "stdout",
		"message":      "hello",
	})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAppValidationErrorUsesFieldEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	NewHandler(&stubService{}, &stubRunnerService{}).RegisterAdminRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/apps", map[string]any{"description": "missing name"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error == "" || len(body.Fields["name"]) == 0 {
		t.Fatalf("expected field validation envelope, got %+v", body)
	}
}

func TestCreateRunnerPoolReturnsOneTimeBootstrapToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		createRunnerPool: func(_ context.Context, input application.CreateRunnerPoolInput) (*application.RunnerPoolResponse, error) {
			if input.Name != "aws-prod" || input.Provider != "byoc" || input.Capabilities["gpu"] != "H100" {
				t.Fatalf("unexpected runner pool input: %+v", input)
			}
			return &application.RunnerPoolResponse{
				ID:             "frpool_123",
				Name:           input.Name,
				Provider:       input.Provider,
				Status:         domain.RunnerPoolStatusActive,
				BootstrapToken: "hsrp_once",
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterAdminRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/runner-pools", map[string]any{
		"name":     "aws-prod",
		"provider": "byoc",
		"capabilities": map[string]any{
			"gpu": "H100",
		},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.RunnerPoolResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "frpool_123" || body.BootstrapToken != "hsrp_once" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestRegisterRunnerAgentUsesBootstrapToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		registerRunnerAgent: func(_ context.Context, input application.RegisterRunnerAgentInput) (*application.RunnerAgentRegistrationResponse, error) {
			if input.PoolID != "frpool_123" || input.BootstrapToken != "hsrp_once" || input.PublicKey != "pub" {
				t.Fatalf("unexpected runner register input: %+v", input)
			}
			return &application.RunnerAgentRegistrationResponse{
				ID:           "fragent_123",
				PoolID:       input.PoolID,
				Hostname:     "runner-1",
				Status:       domain.RunnerAgentStatusOnline,
				SessionToken: "hsra_session",
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/runner/agents/register", map[string]any{
		"poolId":         "frpool_123",
		"bootstrapToken": "hsrp_once",
		"publicKey":      "pub",
		"hostname":       "runner-1",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.RunnerAgentRegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "fragent_123" || body.SessionToken != "hsra_session" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestHeartbeatRunnerAgentUsesBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		heartbeatRunnerAgent: func(_ context.Context, input application.HeartbeatRunnerAgentInput) (*application.RunnerAgentHeartbeatResponse, error) {
			if input.AgentID != "" || input.SessionToken != "hsra_session" || input.Capabilities["gpu"] != "H100" {
				t.Fatalf("unexpected heartbeat input: %+v", input)
			}
			now := time.Now().UTC()
			return &application.RunnerAgentHeartbeatResponse{
				ID:               "fragent_123",
				Status:           domain.RunnerAgentStatusOnline,
				Capabilities:     map[string]any{"gpu": "H100"},
				LastHeartbeatAt:  &now,
				SessionExpiresAt: now.Add(24 * time.Hour),
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSONWithHeaders(engine, http.MethodPost, "/functions/runner/agents/heartbeat", map[string]any{
		"capabilities": map[string]any{"gpu": "H100"},
	}, map[string]string{
		"Authorization": "Bearer hsra_session",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.RunnerAgentHeartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "fragent_123" || body.LastHeartbeatAt == nil {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestLeaseNextInvocationReturnsAssignedInvocation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		leaseNextInvocation: func(_ context.Context, input application.LeaseInvocationInput) (*application.RunnerLeaseResponse, error) {
			if input.AgentID != "fragent_123" || input.SessionToken != "hsra_session" || input.LeaseSecs != 30 {
				t.Fatalf("unexpected lease input: %+v", input)
			}
			return &application.RunnerLeaseResponse{
				Invocation: application.InvocationResponse{
					ID:       "finv_123",
					RunnerID: input.AgentID,
					LeaseID:  "fls_lease",
					Status:   domain.InvocationStatusAssigned,
					Attempt:  1,
				},
				Revision: application.RevisionResponse{
					ID:         "frev_123",
					Entrypoint: "main.handle",
					Image:      application.ImageSpec{Base: "python:3.12-slim"},
				},
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/runner/invocations/lease", map[string]any{
		"agentId":      "fragent_123",
		"sessionToken": "hsra_session",
		"leaseSecs":    30,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.RunnerLeaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Invocation.ID != "finv_123" || body.Invocation.Status != domain.InvocationStatusAssigned || body.Invocation.LeaseID != "fls_lease" || body.Revision.Image.Base == "" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestLeaseNextInvocationAcceptsBearerRunnerSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		leaseNextInvocation: func(_ context.Context, input application.LeaseInvocationInput) (*application.RunnerLeaseResponse, error) {
			if input.AgentID != "" || input.SessionToken != "hsra_header" {
				t.Fatalf("unexpected lease input: %+v", input)
			}
			return &application.RunnerLeaseResponse{
				Invocation: application.InvocationResponse{
					ID:       "finv_123",
					RunnerID: "fragent_123",
					LeaseID:  "fls_lease",
					Status:   domain.InvocationStatusAssigned,
				},
				Revision: application.RevisionResponse{ID: "frev_123"},
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSONWithHeaders(engine, http.MethodPost, "/functions/runner/invocations/lease", map[string]any{
		"leaseSecs": 30,
	}, map[string]string{
		"Authorization": "Bearer hsra_header",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestLeaseNextInvocationRequiresRunnerSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	NewHandler(&stubService{}, &stubRunnerService{}).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/runner/invocations/lease", map[string]any{
		"leaseSecs": 30,
	})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "missing or invalid runner session token" {
		t.Fatalf("unexpected error body: %+v", body)
	}
}

func TestLeaseNextInvocationReturnsNoContentWhenQueueIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		leaseNextInvocation: func(_ context.Context, input application.LeaseInvocationInput) (*application.RunnerLeaseResponse, error) {
			if input.AgentID != "" || input.SessionToken != "hsra_session" {
				t.Fatalf("unexpected lease input: %+v", input)
			}
			return nil, domain.ErrNoInvocationAvailable
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSONWithHeaders(engine, http.MethodPost, "/functions/runner/invocations/lease", map[string]any{
		"leaseSecs": 30,
	}, map[string]string{
		"Authorization": "Bearer hsra_session",
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rec.Body.String())
	}
}

func TestCompleteInvocationUsesBearerTokenWithoutJSONRunnerCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		completeInvocation: func(_ context.Context, invocationID string, input application.CompleteInvocationInput) (*application.InvocationResponse, error) {
			if invocationID != "finv_123" || input.AgentID != "" || input.SessionToken != "hsra_header" || input.LeaseID != "fls_lease" || input.Status != domain.InvocationStatusSucceeded {
				t.Fatalf("unexpected complete input: invocation=%q input=%+v", invocationID, input)
			}
			return &application.InvocationResponse{
				ID:     invocationID,
				Status: domain.InvocationStatusSucceeded,
				Result: map[string]any{"ok": true},
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSONWithHeaders(engine, http.MethodPost, "/functions/runner/invocations/finv_123/complete", map[string]any{
		"leaseId": "fls_lease",
		"status":  "succeeded",
		"result": map[string]any{
			"ok": true,
		},
	}, map[string]string{
		"Authorization": "Bearer hsra_header",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCompleteInvocationReturnsTerminalInvocation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	runnerSvc := &stubRunnerService{
		completeInvocation: func(_ context.Context, invocationID string, input application.CompleteInvocationInput) (*application.InvocationResponse, error) {
			if invocationID != "finv_123" || input.AgentID != "fragent_123" || input.LeaseID != "fls_lease" || input.Status != domain.InvocationStatusSucceeded {
				t.Fatalf("unexpected complete input: invocation=%q input=%+v", invocationID, input)
			}
			return &application.InvocationResponse{
				ID:     invocationID,
				Status: domain.InvocationStatusSucceeded,
				Result: map[string]any{"ok": true},
			}, nil
		},
	}
	engine := gin.New()
	NewHandler(&stubService{}, runnerSvc).RegisterRunnerRoutes(engine.Group("/functions"))

	rec := performJSON(engine, http.MethodPost, "/functions/runner/invocations/finv_123/complete", map[string]any{
		"agentId":      "fragent_123",
		"sessionToken": "hsra_session",
		"leaseId":      "fls_lease",
		"status":       "succeeded",
		"result": map[string]any{
			"ok": true,
		},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body application.InvocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "finv_123" || body.Status != domain.InvocationStatusSucceeded || body.Result["ok"] != true {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func performJSON(engine *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	return performJSONWithHeaders(engine, method, path, body, nil)
}

func performJSONWithHeaders(engine *gin.Engine, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	payload, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

type stubService struct {
	createApp           func(context.Context, application.CreateAppInput) (*application.AppResponse, error)
	deployFunction      func(context.Context, string, application.DeployFunctionInput) (*application.FunctionResponse, error)
	invokeFunction      func(context.Context, string, application.InvokeFunctionInput) (*application.InvocationResponse, error)
	getInvocation       func(context.Context, string) (*application.InvocationResponse, error)
	listInvocationLogs  func(context.Context, string, pagination.Slice) (pagination.Paginated[application.LogResponse], error)
	appendInvocationLog func(context.Context, string, application.AppendLogInput) (*application.LogResponse, error)
}

type stubRunnerService struct {
	createRunnerPool     func(context.Context, application.CreateRunnerPoolInput) (*application.RunnerPoolResponse, error)
	registerRunnerAgent  func(context.Context, application.RegisterRunnerAgentInput) (*application.RunnerAgentRegistrationResponse, error)
	heartbeatRunnerAgent func(context.Context, application.HeartbeatRunnerAgentInput) (*application.RunnerAgentHeartbeatResponse, error)
	leaseNextInvocation  func(context.Context, application.LeaseInvocationInput) (*application.RunnerLeaseResponse, error)
	appendInvocationLog  func(context.Context, string, application.AppendRunnerLogInput) (*application.LogResponse, error)
	completeInvocation   func(context.Context, string, application.CompleteInvocationInput) (*application.InvocationResponse, error)
}

func (s *stubRunnerService) CreateRunnerPool(ctx context.Context, input application.CreateRunnerPoolInput) (*application.RunnerPoolResponse, error) {
	if s.createRunnerPool == nil {
		return nil, errors.New("unexpected CreateRunnerPool call")
	}
	return s.createRunnerPool(ctx, input)
}

func (s *stubRunnerService) RegisterRunnerAgent(ctx context.Context, input application.RegisterRunnerAgentInput) (*application.RunnerAgentRegistrationResponse, error) {
	if s.registerRunnerAgent == nil {
		return nil, errors.New("unexpected RegisterRunnerAgent call")
	}
	return s.registerRunnerAgent(ctx, input)
}

func (s *stubRunnerService) HeartbeatRunnerAgent(ctx context.Context, input application.HeartbeatRunnerAgentInput) (*application.RunnerAgentHeartbeatResponse, error) {
	if s.heartbeatRunnerAgent == nil {
		return nil, errors.New("unexpected HeartbeatRunnerAgent call")
	}
	return s.heartbeatRunnerAgent(ctx, input)
}

func (s *stubRunnerService) LeaseNextInvocation(ctx context.Context, input application.LeaseInvocationInput) (*application.RunnerLeaseResponse, error) {
	if s.leaseNextInvocation == nil {
		return nil, errors.New("unexpected LeaseNextInvocation call")
	}
	return s.leaseNextInvocation(ctx, input)
}

func (s *stubRunnerService) AppendInvocationLog(ctx context.Context, invocationID string, input application.AppendRunnerLogInput) (*application.LogResponse, error) {
	if s.appendInvocationLog == nil {
		return nil, errors.New("unexpected AppendInvocationLog call")
	}
	return s.appendInvocationLog(ctx, invocationID, input)
}

func (s *stubRunnerService) CompleteInvocation(ctx context.Context, invocationID string, input application.CompleteInvocationInput) (*application.InvocationResponse, error) {
	if s.completeInvocation == nil {
		return nil, errors.New("unexpected CompleteInvocation call")
	}
	return s.completeInvocation(ctx, invocationID, input)
}

func (s *stubService) CreateApp(ctx context.Context, input application.CreateAppInput) (*application.AppResponse, error) {
	if s.createApp == nil {
		return nil, errors.New("unexpected CreateApp call")
	}
	return s.createApp(ctx, input)
}

func (s *stubService) DeployFunction(ctx context.Context, appID string, input application.DeployFunctionInput) (*application.FunctionResponse, error) {
	if s.deployFunction == nil {
		return nil, errors.New("unexpected DeployFunction call")
	}
	return s.deployFunction(ctx, appID, input)
}

func (s *stubService) InvokeFunction(ctx context.Context, functionID string, input application.InvokeFunctionInput) (*application.InvocationResponse, error) {
	if s.invokeFunction == nil {
		return nil, errors.New("unexpected InvokeFunction call")
	}
	return s.invokeFunction(ctx, functionID, input)
}

func (s *stubService) GetInvocation(ctx context.Context, invocationID string) (*application.InvocationResponse, error) {
	if s.getInvocation == nil {
		return nil, errors.New("unexpected GetInvocation call")
	}
	return s.getInvocation(ctx, invocationID)
}

func (s *stubService) ListInvocationLogs(ctx context.Context, invocationID string, slice pagination.Slice) (pagination.Paginated[application.LogResponse], error) {
	if s.listInvocationLogs == nil {
		return pagination.Paginated[application.LogResponse]{}, errors.New("unexpected ListInvocationLogs call")
	}
	return s.listInvocationLogs(ctx, invocationID, slice)
}

func (s *stubService) AppendInvocationLog(ctx context.Context, invocationID string, input application.AppendLogInput) (*application.LogResponse, error) {
	if s.appendInvocationLog == nil {
		return nil, errors.New("unexpected AppendInvocationLog call")
	}
	return s.appendInvocationLog(ctx, invocationID, input)
}
