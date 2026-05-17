package httpserver

import (
	"testing"

	"hyperstrate/server/docs"
	"hyperstrate/server/internal/config"
)

func TestNewRouterUsesConfiguredPublicURLForSwagger(t *testing.T) {
	NewRouter(config.Config{
		APIPublicURL: "https://api.example.com/base",
		FrontendURL:  "http://localhost:8080",
		Port:         "8090",
	})

	if docs.SwaggerInfo.Host != "api.example.com" {
		t.Fatalf("Swagger host = %q, want api.example.com", docs.SwaggerInfo.Host)
	}
	if got := docs.SwaggerInfo.Schemes; len(got) != 1 || got[0] != "https" {
		t.Fatalf("Swagger schemes = %#v, want [https]", got)
	}
	if docs.SwaggerInfo.BasePath != "/base" {
		t.Fatalf("Swagger base path = %q, want /base", docs.SwaggerInfo.BasePath)
	}
}

func TestNewRouterDefaultsSwaggerHostToConfiguredPort(t *testing.T) {
	NewRouter(config.Config{
		FrontendURL: "http://localhost:8080",
		Port:        "9999",
	})

	if docs.SwaggerInfo.Host != "localhost:9999" {
		t.Fatalf("Swagger host = %q, want localhost:9999", docs.SwaggerInfo.Host)
	}
	if got := docs.SwaggerInfo.Schemes; len(got) != 1 || got[0] != "http" {
		t.Fatalf("Swagger schemes = %#v, want [http]", got)
	}
	if docs.SwaggerInfo.BasePath != "/" {
		t.Fatalf("Swagger base path = %q, want /", docs.SwaggerInfo.BasePath)
	}
}
