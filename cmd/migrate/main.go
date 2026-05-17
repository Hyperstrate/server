// cmd/migrate is called by the Atlas CLI to load the GORM schema.
// It prints the CREATE statements for all registered models to stdout so that
// `atlas migrate diff` can compare them against the current migration directory
// and generate a new versioned SQL file for any changes.
//
// Usage (Atlas CLI calls this automatically via atlas.hcl):
//
//	atlas migrate diff --env local           # generate new migration file
//	atlas migrate diff --env local <name>    # generate named migration file
//	atlas migrate apply --env local          # apply pending migrations (dev)
//	atlas migrate apply --env production     # apply pending migrations (prod)
package main

import (
	"flag"
	"io"
	"log"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

	aiDomain "hyperstrate/server/internal/modules/ai/domain"
	authDomain "hyperstrate/server/internal/modules/auth/domain"
	obsDomain "hyperstrate/server/internal/modules/observability/domain"
	promptDomain "hyperstrate/server/internal/modules/prompts/domain"
	routerDomain "hyperstrate/server/internal/modules/router/domain"
)

func main() {
	dialect := flag.String("dialect", "sqlite", "database dialect: sqlite | postgres")
	flag.Parse()

	stmts, err := schemaForDialect(*dialect)
	if err != nil {
		log.Fatalf("failed to load gorm schema: %v", err)
	}
	if _, err := io.WriteString(os.Stdout, stmts); err != nil {
		log.Fatalf("failed to write schema: %v", err)
	}
}

func schemaForDialect(dialect string) (string, error) {
	return gormschema.New(dialect).Load(migrationModels()...)
}

func migrationModels() []any {
	return []any{
		&aiDomain.Model{},
		&aiDomain.ModelConfiguration{},
		&aiDomain.ModelKeyRotation{},
		&aiDomain.Conversation{},
		&aiDomain.ConversationMessage{},
		&aiDomain.Job{},
		&aiDomain.MCPServer{},
		&promptDomain.Prompt{},
		&promptDomain.PromptVersion{},
		&routerDomain.Router{},
		&routerDomain.RouterConfiguration{},
		&routerDomain.RouterTeamAccess{},
		&routerDomain.RouterTarget{},
		&routerDomain.RouterFeature{},
		&routerDomain.RouterInterceptor{},
		&routerDomain.RouterEvaluation{},
		&routerDomain.RouterEvaluationCase{},
		&routerDomain.RouterEvaluationRun{},
		&authDomain.Organization{},
		&authDomain.User{},
		&authDomain.Team{},
		&authDomain.UserTeam{},
		&authDomain.APIKey{},
		&authDomain.VirtualKey{},
		&authDomain.OIDCGroupMapping{},
		&obsDomain.InferenceLog{},
		&obsDomain.AuditLog{},
		&obsDomain.ProviderHealth{},
		&obsDomain.WebhookDelivery{},
		&obsDomain.InferencePayload{},
		&obsDomain.AgentSessionEvent{},
		&obsDomain.ToolCallArchive{},
		&obsDomain.CompressionEvent{},
	}
}
