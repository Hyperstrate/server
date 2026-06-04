package application_test

import (
	"context"
	"testing"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/modules/functions/domain"
)

func TestBuildServiceCreatesBuildAndLinksRevision(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Invocations, repos.Logs)
	buildSvc := application.NewBuildService(repos.Builds, repos.Revisions)
	ctx := functionsCtx()

	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "builders"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "render",
		Entrypoint: "main.render",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}

	build, err := buildSvc.CreateRevisionBuild(ctx, fn.ActiveRevisionID, application.CreateBuildInput{
		Source: application.BuildSourceSpec{Type: "archive", URI: "s3://artifacts/source.tar.gz"},
	})
	if err != nil {
		t.Fatalf("CreateRevisionBuild returned error: %v", err)
	}
	if build.ID == "" || build.Status != domain.BuildStatusQueued {
		t.Fatalf("unexpected build response: %+v", build)
	}
	rev := repos.Revisions.byID[fn.ActiveRevisionID]
	if rev.BuildID != build.ID {
		t.Fatalf("expected revision build id %q, got %q", build.ID, rev.BuildID)
	}

	done, err := buildSvc.CompleteBuild(ctx, build.ID, application.CompleteBuildInput{
		Status: domain.BuildStatusSucceeded,
		Artifact: application.BuildArtifactSpec{
			ImageRef:         "registry.example.com/fn:sha256",
			SourceArchiveRef: "s3://artifacts/source.tar.gz",
			Digest:           "sha256:abc",
		},
	})
	if err != nil {
		t.Fatalf("CompleteBuild returned error: %v", err)
	}
	if done.Status != domain.BuildStatusSucceeded || done.Artifact.ImageRef == "" || done.FinishedAt == nil {
		t.Fatalf("unexpected completed build: %+v", done)
	}

	otherOrgCtx := authDomain.WithOrgID(context.Background(), "org_other")
	if _, err := buildSvc.CreateRevisionBuild(otherOrgCtx, fn.ActiveRevisionID, application.CreateBuildInput{
		Source: application.BuildSourceSpec{Type: "archive", URI: "s3://private/source.tar.gz"},
	}); err != domain.ErrRevisionNotFound {
		t.Fatalf("expected ErrRevisionNotFound for other org, got %v", err)
	}
}
