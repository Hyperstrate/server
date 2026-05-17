package persistence

import (
	"context"
	"errors"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/auth/domain"

	"gorm.io/gorm"
)

func periodStart(period domain.ResetPeriod) time.Time {
	now := time.Now().UTC()
	switch period {
	case domain.ResetPeriodDaily:
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case domain.ResetPeriodWeekly:
		weekday := int(now.Weekday())
		return time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, time.UTC)
	case domain.ResetPeriodMonthly:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Time{}
	}
}

func orgScope(db *gorm.DB, orgID string) *gorm.DB {
	return db.Where("org_id = ?", orgID)
}

// ── Organization repository ───────────────────────────────────────────────────

type gormOrganizationRepository struct{ db *gorm.DB }

func NewOrganizationRepository(db *gorm.DB) domain.OrganizationRepository {
	return &gormOrganizationRepository{db: db}
}

func (r *gormOrganizationRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	return n, r.db.WithContext(ctx).Model(&domain.Organization{}).Count(&n).Error
}

func (r *gormOrganizationRepository) Create(ctx context.Context, o *domain.Organization) error {
	return r.db.WithContext(ctx).Create(o).Error
}

func (r *gormOrganizationRepository) FindByID(ctx context.Context, id string) (*domain.Organization, error) {
	var o domain.Organization
	if err := r.db.WithContext(ctx).First(&o, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrOrganizationNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (r *gormOrganizationRepository) FindBySlug(ctx context.Context, slug string) (*domain.Organization, error) {
	var o domain.Organization
	if err := r.db.WithContext(ctx).First(&o, "slug = ?", slug).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrOrganizationNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (r *gormOrganizationRepository) List(ctx context.Context, offset, limit int) ([]domain.Organization, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.Organization{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orgs []domain.Organization
	err := r.db.WithContext(ctx).Order("name ASC").Offset(offset).Limit(limit).Find(&orgs).Error
	return orgs, total, err
}

func (r *gormOrganizationRepository) Update(ctx context.Context, o *domain.Organization) error {
	result := r.db.WithContext(ctx).Where("id = ?", o.ID).Save(o)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrOrganizationNotFound
	}
	return nil
}

func (r *gormOrganizationRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&domain.Organization{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrOrganizationNotFound
	}
	return nil
}

// ── APIKey repository ─────────────────────────────────────────────────────────

type gormAPIKeyRepository struct{ db *gorm.DB }

func NewAPIKeyRepository(db *gorm.DB) domain.APIKeyRepository {
	return &gormAPIKeyRepository{db: db}
}

func (r *gormAPIKeyRepository) ListByOrg(ctx context.Context, orgID, routerID, teamID string, offset, limit int) ([]domain.APIKey, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.APIKey{}).Where("org_id = ?", orgID)
	if routerID != "" {
		q = q.Where("router_id = ? AND scope = ?", routerID, domain.APIKeyScopeRouter)
	}
	if teamID != "" {
		q = q.Where("team_id = ?", teamID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var keys []domain.APIKey
	err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&keys).Error
	return keys, total, err
}

func (r *gormAPIKeyRepository) Create(ctx context.Context, k *domain.APIKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

func (r *gormAPIKeyRepository) FindByID(ctx context.Context, orgID, id string) (*domain.APIKey, error) {
	var k domain.APIKey
	if err := orgScope(r.db.WithContext(ctx), orgID).First(&k, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAPIKeyNotFound
		}
		return nil, err
	}
	return &k, nil
}

func (r *gormAPIKeyRepository) FindByKeyHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	var k domain.APIKey
	if err := r.db.WithContext(ctx).First(&k, "key_hash = ?", hash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAPIKeyNotFound
		}
		return nil, err
	}
	return &k, nil
}

func (r *gormAPIKeyRepository) Update(ctx context.Context, k *domain.APIKey) error {
	result := orgScope(r.db.WithContext(ctx), k.OrgID).Where("id = ?", k.ID).Save(k)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrAPIKeyNotFound
	}
	return nil
}

func (r *gormAPIKeyRepository) Delete(ctx context.Context, orgID, id string) error {
	result := orgScope(r.db.WithContext(ctx), orgID).Delete(&domain.APIKey{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrAPIKeyNotFound
	}
	return nil
}

func (r *gormAPIKeyRepository) DeleteByRouterID(ctx context.Context, routerID string) error {
	return r.db.WithContext(ctx).Delete(&domain.APIKey{}, "router_id = ? AND scope = ?", routerID, domain.APIKeyScopeRouter).Error
}

// ── VirtualKey repository ─────────────────────────────────────────────────────

type gormVirtualKeyRepository struct{ db *gorm.DB }

func NewVirtualKeyRepository(db *gorm.DB) domain.VirtualKeyRepository {
	return &gormVirtualKeyRepository{db: db}
}

func (r *gormVirtualKeyRepository) List(ctx context.Context, orgID, routerID, teamID string, offset, limit int) ([]domain.VirtualKey, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.VirtualKey{}).Where("org_id = ?", orgID)
	if routerID != "" {
		q = q.Where("router_id = ?", routerID)
	}
	if teamID != "" {
		q = q.Where("team_id = ?", teamID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var keys []domain.VirtualKey
	err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&keys).Error
	return keys, total, err
}

func (r *gormVirtualKeyRepository) ListByTeamID(ctx context.Context, teamID string) ([]domain.VirtualKey, error) {
	var keys []domain.VirtualKey
	err := r.db.WithContext(ctx).Where("team_id = ?", teamID).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

func (r *gormVirtualKeyRepository) Create(ctx context.Context, k *domain.VirtualKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

func (r *gormVirtualKeyRepository) FindByID(ctx context.Context, orgID, id string) (*domain.VirtualKey, error) {
	var k domain.VirtualKey
	if err := orgScope(r.db.WithContext(ctx), orgID).First(&k, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrVirtualKeyNotFound
		}
		return nil, err
	}
	return &k, nil
}

func (r *gormVirtualKeyRepository) FindByKeyHash(ctx context.Context, hash string) (*domain.VirtualKey, error) {
	var k domain.VirtualKey
	if err := r.db.WithContext(ctx).First(&k, "key_hash = ?", hash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrVirtualKeyNotFound
		}
		return nil, err
	}
	return &k, nil
}

func (r *gormVirtualKeyRepository) Update(ctx context.Context, k *domain.VirtualKey) error {
	result := orgScope(r.db.WithContext(ctx), k.OrgID).Where("id = ?", k.ID).Save(k)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrVirtualKeyNotFound
	}
	return nil
}

func (r *gormVirtualKeyRepository) Delete(ctx context.Context, orgID, id string) error {
	result := orgScope(r.db.WithContext(ctx), orgID).Delete(&domain.VirtualKey{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrVirtualKeyNotFound
	}
	return nil
}

// ── Team repository ───────────────────────────────────────────────────────────

type gormTeamRepository struct{ db *gorm.DB }

func NewTeamRepository(db *gorm.DB) domain.TeamRepository {
	return &gormTeamRepository{db: db}
}

func (r *gormTeamRepository) ListByOrgID(ctx context.Context, orgID, query string, offset, limit int) ([]domain.Team, int64, error) {
	scope := teamSearchScope(query)
	var total int64
	if err := scope(r.db.WithContext(ctx).Model(&domain.Team{}).Where("org_id = ?", orgID)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var teams []domain.Team
	err := scope(r.db.WithContext(ctx).Where("org_id = ?", orgID)).Order("name ASC").Offset(offset).Limit(limit).Find(&teams).Error
	return teams, total, err
}

func teamSearchScope(query string) func(*gorm.DB) *gorm.DB {
	query = strings.TrimSpace(strings.ToLower(query))
	return func(db *gorm.DB) *gorm.DB {
		if query == "" {
			return db
		}
		like := "%" + query + "%"
		return db.Where(
			"LOWER(id) LIKE ? OR LOWER(name) LIKE ? OR LOWER(description) LIKE ?",
			like,
			like,
			like,
		)
	}
}

func (r *gormTeamRepository) ListByIDs(ctx context.Context, orgID string, ids []string) ([]domain.Team, error) {
	var teams []domain.Team
	err := r.db.WithContext(ctx).Where("org_id = ? AND id IN ?", orgID, ids).Find(&teams).Error
	return teams, err
}

func (r *gormTeamRepository) Create(ctx context.Context, t *domain.Team) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *gormTeamRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Team, error) {
	var t domain.Team
	if err := orgScope(r.db.WithContext(ctx), orgID).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTeamNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *gormTeamRepository) Update(ctx context.Context, t *domain.Team) error {
	result := orgScope(r.db.WithContext(ctx), t.OrgID).Where("id = ?", t.ID).Save(t)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrTeamNotFound
	}
	return nil
}

func (r *gormTeamRepository) Delete(ctx context.Context, orgID, id string) error {
	result := orgScope(r.db.WithContext(ctx), orgID).Delete(&domain.Team{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrTeamNotFound
	}
	return nil
}

func (r *gormTeamRepository) AddMember(ctx context.Context, teamID, userID string) error {
	ut := domain.UserTeam{UserID: userID, TeamID: teamID, CreatedAt: time.Now()}
	return r.db.WithContext(ctx).
		Where(domain.UserTeam{UserID: userID, TeamID: teamID}).
		FirstOrCreate(&ut).Error
}

func (r *gormTeamRepository) RemoveMember(ctx context.Context, teamID, userID string) error {
	return r.db.WithContext(ctx).
		Delete(&domain.UserTeam{}, "user_id = ? AND team_id = ?", userID, teamID).Error
}

func (r *gormTeamRepository) ListMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	var records []domain.UserTeam
	if err := r.db.WithContext(ctx).Where("team_id = ?", teamID).Find(&records).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for _, rec := range records {
		ids = append(ids, rec.UserID)
	}
	return ids, nil
}

func (r *gormTeamRepository) ListTeamIDsForUser(ctx context.Context, userID string) ([]string, error) {
	var records []domain.UserTeam
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&records).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for _, rec := range records {
		ids = append(ids, rec.TeamID)
	}
	return ids, nil
}

// ── User repository ───────────────────────────────────────────────────────────

type gormUserRepository struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) domain.UserRepository {
	return &gormUserRepository{db: db}
}

func (r *gormUserRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	return n, r.db.WithContext(ctx).Model(&domain.User{}).Count(&n).Error
}

func (r *gormUserRepository) Create(ctx context.Context, u *domain.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *gormUserRepository) FindByID(ctx context.Context, id string) (*domain.User, error) {
	var u domain.User
	if err := r.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepository) FindByIDInOrg(ctx context.Context, orgID, id string) (*domain.User, error) {
	var u domain.User
	if err := r.db.WithContext(ctx).First(&u, "org_id = ? AND id = ?", orgID, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var u domain.User
	if err := r.db.WithContext(ctx).First(&u, "email = ?", email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepository) ListAll(ctx context.Context, offset, limit int) ([]domain.User, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []domain.User
	err := r.db.WithContext(ctx).Order("created_at ASC").Offset(offset).Limit(limit).Find(&users).Error
	return users, total, err
}

func (r *gormUserRepository) ListByOrg(ctx context.Context, orgID string, offset, limit int) ([]domain.User, int64, error) {
	var total int64
	query := r.db.WithContext(ctx).Model(&domain.User{}).Where("org_id = ?", orgID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []domain.User
	err := r.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("created_at ASC").
		Offset(offset).
		Limit(limit).
		Find(&users).Error
	return users, total, err
}

func (r *gormUserRepository) Update(ctx context.Context, u *domain.User) error {
	result := r.db.WithContext(ctx).Where("id = ?", u.ID).Save(u)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// ── OIDCGroupMapping repository ───────────────────────────────────────────────

type gormOIDCGroupMappingRepository struct{ db *gorm.DB }

func NewOIDCGroupMappingRepository(db *gorm.DB) domain.OIDCGroupMappingRepository {
	return &gormOIDCGroupMappingRepository{db: db}
}

func (r *gormOIDCGroupMappingRepository) List(ctx context.Context, orgID string) ([]domain.OIDCGroupMapping, error) {
	var rows []domain.OIDCGroupMapping
	err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (r *gormOIDCGroupMappingRepository) FindByGroup(ctx context.Context, orgID, groupName string) ([]domain.OIDCGroupMapping, error) {
	var rows []domain.OIDCGroupMapping
	err := r.db.WithContext(ctx).Where("org_id = ? AND group_name = ?", orgID, groupName).Find(&rows).Error
	return rows, err
}

func (r *gormOIDCGroupMappingRepository) Create(ctx context.Context, m *domain.OIDCGroupMapping) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *gormOIDCGroupMappingRepository) Delete(ctx context.Context, orgID, id string) error {
	return r.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).Delete(&domain.OIDCGroupMapping{}).Error
}
