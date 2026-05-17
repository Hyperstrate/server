package application_test

// Org-isolation security tests for the auth module.
//
// Each test uses org-scoped stubs that return ErrNotFound when the caller's
// org does not own the resource, mirroring what the GORM implementations do.

import (
	"context"
	"errors"
	"testing"

	"hyperstrate/server/internal/modules/auth/application"
	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/shared/pagination"
)

// orgCtx returns a context carrying the given org ID.
func orgCtx(orgID string) context.Context {
	return domain.WithOrgID(context.Background(), orgID)
}

const (
	orgA = "org-alpha"
	orgB = "org-bravo"
)

// ── VirtualKey isolation ──────────────────────────────────────────────────────

func TestOrgIsolation_UpdateVirtualKey_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	// Key owned by orgA
	d.vkeys.byID["vkey_1"] = &domain.VirtualKey{
		ID: "vkey_1", OrgID: orgA, RouterID: "rtr_1", Name: "owned by A",
	}
	svc := d.build()

	// orgB tries to update it
	newName := "tampered"
	_, err := svc.UpdateVirtualKey(orgCtx(orgB), "vkey_1", application.UpdateVirtualKeyInput{Name: &newName})
	if !errors.Is(err, domain.ErrVirtualKeyNotFound) {
		t.Errorf("want ErrVirtualKeyNotFound for cross-org update, got %v", err)
	}
}

func TestOrgIsolation_RevokeVirtualKey_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.vkeys.byID["vkey_1"] = &domain.VirtualKey{ID: "vkey_1", OrgID: orgA}
	svc := d.build()

	err := svc.RevokeVirtualKey(orgCtx(orgB), "vkey_1")
	if !errors.Is(err, domain.ErrVirtualKeyNotFound) {
		t.Errorf("want ErrVirtualKeyNotFound for cross-org revoke, got %v", err)
	}
}

func TestOrgIsolation_GetVirtualKey_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.vkeys.byID["vkey_1"] = &domain.VirtualKey{ID: "vkey_1", OrgID: orgA}
	svc := d.build()

	_, err := svc.GetVirtualKey(orgCtx(orgB), "vkey_1")
	if !errors.Is(err, domain.ErrVirtualKeyNotFound) {
		t.Errorf("want ErrVirtualKeyNotFound for cross-org get, got %v", err)
	}
}

func TestOrgIsolation_GetVirtualKey_ownerOrgSucceeds(t *testing.T) {
	d := newDeps()
	d.vkeys.byID["vkey_1"] = &domain.VirtualKey{ID: "vkey_1", OrgID: orgA, Name: "mine"}
	svc := d.build()

	resp, err := svc.GetVirtualKey(orgCtx(orgA), "vkey_1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if resp.Name != "mine" {
		t.Errorf("Name = %q, want mine", resp.Name)
	}
}

// ── Team isolation ────────────────────────────────────────────────────────────

func TestOrgIsolation_GetTeam_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, Name: "A team", IsEnabled: true}
	svc := d.build()

	_, err := svc.GetTeam(orgCtx(orgB), "team_1")
	if !errors.Is(err, domain.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound for cross-org get, got %v", err)
	}
}

func TestOrgIsolation_GetTeam_ownerOrgSucceeds(t *testing.T) {
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, Name: "A team", IsEnabled: true}
	svc := d.build()

	resp, err := svc.GetTeam(orgCtx(orgA), "team_1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if resp.Name != "A team" {
		t.Errorf("Name = %q, want A team", resp.Name)
	}
}

func TestOrgIsolation_UpdateTeam_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, Name: "original", IsEnabled: true}
	svc := d.build()

	newName := "tampered"
	_, err := svc.UpdateTeam(orgCtx(orgB), "team_1", application.UpdateTeamInput{Name: &newName})
	if !errors.Is(err, domain.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound for cross-org update, got %v", err)
	}
}

func TestOrgIsolation_DeleteTeam_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, IsEnabled: true}
	svc := d.build()

	err := svc.DeleteTeam(orgCtx(orgB), "team_1")
	if !errors.Is(err, domain.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound for cross-org delete, got %v", err)
	}
}

func TestOrgIsolation_AddTeamMember_crossOrgTeamReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, IsEnabled: true}
	d.users.byID["usr_1"] = &domain.User{ID: "usr_1"}
	svc := d.build()

	err := svc.AddTeamMember(orgCtx(orgB), "team_1", "usr_1")
	if !errors.Is(err, domain.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound for cross-org team, got %v", err)
	}
}

func TestOrgIsolation_AddTeamMember_crossOrgUserReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, IsEnabled: true}
	d.users.byID["usr_1"] = &domain.User{ID: "usr_1", Email: "user@org-b.test", OrgID: orgB}
	svc := d.build()

	err := svc.AddTeamMember(orgCtx(orgA), "team_1", "usr_1")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound for cross-org user, got %v", err)
	}
	if got := d.teams.members["team_1"]; len(got) != 0 {
		t.Fatalf("cross-org user was added to team: %#v", got)
	}
}

func TestOrgIsolation_ListUsersReturnsOnlyCallerOrg(t *testing.T) {
	d := newDeps()
	d.users.byID["usr_a"] = &domain.User{ID: "usr_a", Email: "a@example.com", OrgID: orgA}
	d.users.byID["usr_b"] = &domain.User{ID: "usr_b", Email: "b@example.com", OrgID: orgB}
	svc := d.build()

	users, err := svc.ListUsers(orgCtx(orgA), pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if users.Meta.Total != 1 || len(users.Items) != 1 {
		t.Fatalf("got %d/%d users, want exactly one org-scoped user", len(users.Items), users.Meta.Total)
	}
	if users.Items[0].ID != "usr_a" {
		t.Fatalf("listed user %q, want usr_a", users.Items[0].ID)
	}
}

func TestOrgIsolation_UpdateUser_crossOrgReturnsNotFound(t *testing.T) {
	d := newDeps()
	d.users.byID["usr_1"] = &domain.User{
		ID: "usr_1", Email: "user@org-b.test", Role: domain.UserRoleMember, OrgID: orgB,
	}
	svc := d.build()

	role := domain.UserRoleAdmin
	_, err := svc.UpdateUser(orgCtx(orgA), "usr_1", application.UpdateUserInput{Role: &role})
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound for cross-org update, got %v", err)
	}
	if got := d.users.byID["usr_1"].Role; got != domain.UserRoleMember {
		t.Fatalf("cross-org update changed role to %q", got)
	}
}

func TestOrgIsolation_CreateVirtualKey_teamBelongsToDifferentOrg_returnsError(t *testing.T) {
	// team_1 belongs to orgA; orgB tries to create a vkey pointing at it
	d := newDeps()
	d.teams.teams["team_1"] = &domain.Team{ID: "team_1", OrgID: orgA, IsEnabled: true}
	svc := d.build()

	_, err := svc.CreateVirtualKey(orgCtx(orgB), application.CreateVirtualKeyInput{
		RouterID: "rtr_1", Name: "key", TeamID: "team_1",
	})
	if err == nil {
		t.Error("expected error when team belongs to a different org, got nil")
	}
}

// ── Organization CRUD ─────────────────────────────────────────────────────────

func TestUpdateOrganization_nameAndSlugChange(t *testing.T) {
	d := newDeps()
	d.orgs.orgs = map[string]*domain.Organization{
		"org_1": {ID: "org_1", Name: "Old Name", Slug: "old-name", IsEnabled: true},
	}
	svc := d.build()

	newName := "New Name"
	resp, err := svc.UpdateOrganization(context.Background(), "org_1", application.UpdateOrganizationInput{Name: &newName})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "New Name" {
		t.Errorf("Name = %q, want New Name", resp.Name)
	}
	if resp.Slug != "new-name" {
		t.Errorf("Slug = %q, want new-name", resp.Slug)
	}
}

func TestUpdateOrganization_notFound_returnsError(t *testing.T) {
	d := newDeps()
	svc := d.build()
	_, err := svc.UpdateOrganization(context.Background(), "org_nonexistent", application.UpdateOrganizationInput{})
	if !errors.Is(err, domain.ErrOrganizationNotFound) {
		t.Errorf("want ErrOrganizationNotFound, got %v", err)
	}
}

func TestDeleteOrganization_notFound_returnsError(t *testing.T) {
	d := newDeps()
	svc := d.build()
	err := svc.DeleteOrganization(context.Background(), "org_nonexistent")
	if !errors.Is(err, domain.ErrOrganizationNotFound) {
		t.Errorf("want ErrOrganizationNotFound, got %v", err)
	}
}

func TestCreateOrganization_setsSlug(t *testing.T) {
	d := newDeps()
	svc := d.build()
	resp, err := svc.CreateOrganization(context.Background(), application.CreateOrganizationInput{Name: "Hello World"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Slug != "hello-world" {
		t.Errorf("Slug = %q, want hello-world", resp.Slug)
	}
}
