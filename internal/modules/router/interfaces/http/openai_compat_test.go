package http

import (
	"bytes"
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"hyperstrate/server/internal/modules/router/application"
	routerDomain "hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/shared/pagination"

	"github.com/gin-gonic/gin"
)

type anthropicRouteErrorService struct {
	routeErr  error
	streamErr error
}

func (s *anthropicRouteErrorService) ListRouters(context.Context, pagination.Slice, string) (pagination.Paginated[application.RouterResponse], error) {
	return pagination.Paginated[application.RouterResponse]{}, nil
}
func (s *anthropicRouteErrorService) CreateRouter(context.Context, application.CreateRouterInput) (*application.RouterResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) GetRouter(context.Context, string) (*application.RouterResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) UpdateRouter(context.Context, string, application.UpdateRouterInput) (*application.RouterResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) DeleteRouter(context.Context, string) error { return nil }
func (s *anthropicRouteErrorService) ListTargets(context.Context, string) ([]application.RouterTargetResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) AddTarget(context.Context, string, application.AddTargetInput) (*application.RouterTargetResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) UpdateTarget(context.Context, string, string, application.UpdateTargetInput) (*application.RouterTargetResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) RemoveTarget(context.Context, string, string) error { return nil }
func (s *anthropicRouteErrorService) ListFeatures(context.Context, string) ([]application.RouterFeatureResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) AddFeature(context.Context, string, application.AddFeatureInput) (*application.RouterFeatureResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) UpdateFeature(context.Context, string, string, application.UpdateFeatureInput) (*application.RouterFeatureResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) RemoveFeature(context.Context, string, string) error { return nil }
func (s *anthropicRouteErrorService) ListInterceptors(context.Context, string) ([]application.RouterInterceptorResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) AddInterceptor(context.Context, string, application.AddInterceptorInput) (*application.RouterInterceptorResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) UpdateInterceptor(context.Context, string, string, application.UpdateInterceptorInput) (*application.RouterInterceptorResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) RemoveInterceptor(context.Context, string, string) error {
	return nil
}
func (s *anthropicRouteErrorService) ListRouterTeamAccess(context.Context, string) ([]routerDomain.RouterTeamAccess, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) GrantRouterTeamAccess(context.Context, string, string) error {
	return nil
}
func (s *anthropicRouteErrorService) RevokeRouterTeamAccess(context.Context, string, string) error {
	return nil
}
func (s *anthropicRouteErrorService) RouteInfer(context.Context, string, application.RouteInferInput) (*application.RouteInferResult, error) {
	if s.routeErr != nil {
		return nil, s.routeErr
	}
	return &application.RouteInferResult{Content: "ok", SelectedModelID: "mdl_1", ModelDefKey: "model"}, nil
}
func (s *anthropicRouteErrorService) RouteInferStream(context.Context, string, application.RouteInferInput) (<-chan application.StreamChunk, error) {
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	ch := make(chan application.StreamChunk)
	close(ch)
	return ch, nil
}
func (s *anthropicRouteErrorService) RouteEmbed(context.Context, string, []string) ([][]float32, string, error) {
	return nil, "", nil
}
func (s *anthropicRouteErrorService) GetBudgetStatus(context.Context, string) (*application.BudgetStatus, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) MetricsSnapshot() []application.RouterMetricSnapshot {
	return nil
}
func (s *anthropicRouteErrorService) LintRouter(context.Context, string) (*application.RouterLintResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) ListMCPTools(context.Context, string, string) ([]application.MCPServerTools, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) ExportRouter(context.Context, string) (*application.RouterExport, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) ImportRouter(context.Context, application.RouterExport) (*application.RouterResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) CreateEvaluation(context.Context, application.CreateEvaluationInput) (*application.EvaluationResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) ListEvaluations(context.Context, string, pagination.Slice) (pagination.Paginated[application.EvaluationResponse], error) {
	return pagination.Paginated[application.EvaluationResponse]{}, nil
}
func (s *anthropicRouteErrorService) GetEvaluation(context.Context, string) (*application.EvaluationResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) UpdateEvaluation(context.Context, string, application.UpdateEvaluationInput) (*application.EvaluationResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) DeleteEvaluation(context.Context, string) error { return nil }
func (s *anthropicRouteErrorService) ListEvaluationCases(context.Context, string) ([]application.EvaluationCaseResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) AddEvaluationCase(context.Context, string, application.EvaluationCaseInput) (*application.EvaluationCaseResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) DeleteEvaluationCase(context.Context, string, string) error {
	return nil
}
func (s *anthropicRouteErrorService) RunEvaluation(context.Context, string, string) (*application.EvaluationRunResponse, error) {
	return nil, nil
}
func (s *anthropicRouteErrorService) ListEvaluationRuns(context.Context, string, pagination.Slice) (pagination.Paginated[application.EvaluationRunResponse], error) {
	return pagination.Paginated[application.EvaluationRunResponse]{}, nil
}

func performAnthropicMessages(t *testing.T, svc application.Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewHandler(svc, nil)
	r := gin.New()
	r.POST("/router/:id/v1/messages", h.AnthropicMessages)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPost, "/router/rtr_missing/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAnthropicMessagesMapsRouteInferErrorsWithSharedResponder(t *testing.T) {
	body := `{"model":"claude","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`
	w := performAnthropicMessages(t, &anthropicRouteErrorService{routeErr: routerDomain.ErrRouterNotFound}, body)

	if w.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error != routerDomain.ErrRouterNotFound.Error() {
		t.Fatalf("error = %q, want %q", got.Error, routerDomain.ErrRouterNotFound.Error())
	}
}

func TestAnthropicMessagesMapsPreStreamErrorsWithSharedResponder(t *testing.T) {
	body := `{"model":"claude","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hello"}]}`
	w := performAnthropicMessages(t, &anthropicRouteErrorService{streamErr: routerDomain.ErrTeamNotAllowed}, body)

	if w.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error != routerDomain.ErrTeamNotAllowed.Error() {
		t.Fatalf("error = %q, want %q", got.Error, routerDomain.ErrTeamNotAllowed.Error())
	}
}
