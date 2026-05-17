package domain

import "context"

type ModelRepository interface {
	List(ctx context.Context, orgID, query string, offset, limit int) (items []Model, total int64, err error)
	// ListByDefinitionKeys returns only models whose model_definition_key is in keys.
	// Used for capability-filtered listing (capabilities live in the catalog, not the DB).
	ListByDefinitionKeys(ctx context.Context, orgID string, keys []string, query string, offset, limit int) (items []Model, total int64, err error)
	// ListByIDs fetches specific models by primary key scoped to the org. Order is not guaranteed.
	ListByIDs(ctx context.Context, orgID string, ids []string) ([]Model, error)
	// ListAll returns every model across all orgs. Used only by the health monitor.
	ListAll(ctx context.Context) ([]Model, error)
	Create(ctx context.Context, model *Model) error
	FindByID(ctx context.Context, orgID, id string) (*Model, error)
	Update(ctx context.Context, model *Model) error
	Delete(ctx context.Context, orgID, id string) error
}

type ModelConfigurationRepository interface {
	FindByModelID(ctx context.Context, orgID, modelID string) (*ModelConfiguration, error)
	// ListConfiguredModelIDs returns the subset of modelIDs that have a configuration row, scoped to org.
	ListConfiguredModelIDs(ctx context.Context, orgID string, modelIDs []string) ([]string, error)
	ListByModelIDs(ctx context.Context, orgID string, modelIDs []string) ([]ModelConfiguration, error)
	// ListAllByModelIDs returns configurations for the given model IDs across all orgs.
	// Used only by the health monitor which runs without org context.
	ListAllByModelIDs(ctx context.Context, modelIDs []string) ([]ModelConfiguration, error)
	Upsert(ctx context.Context, orgID string, config *ModelConfiguration) error
	DeleteByModelID(ctx context.Context, orgID, modelID string) error
}

type ConversationRepository interface {
	List(ctx context.Context, orgID string, offset, limit int) (items []Conversation, total int64, err error)
	Create(ctx context.Context, c *Conversation) error
	FindByID(ctx context.Context, orgID, id string) (*Conversation, error)
	Delete(ctx context.Context, orgID, id string) error
	ListMessages(ctx context.Context, orgID, conversationID string) ([]ConversationMessage, error)
	AddMessage(ctx context.Context, orgID string, msg *ConversationMessage) error
}

type JobRepository interface {
	List(ctx context.Context, orgID string, offset, limit int) (items []Job, total int64, err error)
	ListByStatus(ctx context.Context, status JobStatus) ([]Job, error)
	Create(ctx context.Context, job *Job) error
	FindByID(ctx context.Context, orgID, id string) (*Job, error)
	// FindByIDForWorker looks up a job without org scoping. Used only by the
	// background job processor which runs without a user request context.
	FindByIDForWorker(ctx context.Context, id string) (*Job, error)
	Update(ctx context.Context, job *Job) error
}

type ModelKeyRotationRepository interface {
	Create(ctx context.Context, r *ModelKeyRotation) error
	ListByModelID(ctx context.Context, modelID string, limit, offset int) ([]ModelKeyRotation, int64, error)
}
