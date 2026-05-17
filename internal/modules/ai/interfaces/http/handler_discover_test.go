package http

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"hyperstrate/server/internal/config"
	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"

	"github.com/gin-gonic/gin"
)

type discoverService struct {
	application.Service
}

func (discoverService) ListCatalog(string) []domain.ModelDefinition {
	return nil
}

func TestDiscoverOllamaModelsUsesConfiguredDefaultBaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requestedPath string
	upstream := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		requestedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(gin.H{"models": []gin.H{{"name": "llama3.2:latest", "size": 123}}})
	}))
	defer upstream.Close()

	handler := NewHandler(discoverService{}, config.Config{OllamaBaseURL: upstream.URL})
	router := gin.New()
	router.GET("/discover", handler.DiscoverOllamaModels)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/discover", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if requestedPath != "/api/tags" {
		t.Fatalf("requested path = %q, want /api/tags", requestedPath)
	}
}

func TestDiscoverOllamaModelsQueryBaseURLOverridesConfiguredDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	configured := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		t.Fatalf("configured default should not be called")
	}))
	defer configured.Close()

	var requestedPath string
	override := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		requestedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(gin.H{"models": []gin.H{}})
	}))
	defer override.Close()

	handler := NewHandler(discoverService{}, config.Config{OllamaBaseURL: configured.URL})
	router := gin.New()
	router.GET("/discover", handler.DiscoverOllamaModels)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/discover?baseUrl="+override.URL, nil)
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if requestedPath != "/api/tags" {
		t.Fatalf("requested path = %q, want /api/tags", requestedPath)
	}
}
