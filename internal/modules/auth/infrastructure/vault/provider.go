package vault

import (
	"context"
	"fmt"
)

// Provider abstracts secret storage. The noop implementation stores nothing
// (secrets remain hashed in the database). Wire a real implementation backed
// by HashiCorp Vault, AWS Secrets Manager, or GCP Secret Manager for production.
//
// HashiCorp Vault example (requires github.com/hashicorp/vault/api):
//
//	type vaultProvider struct { client *vault.Client }
//	func (v *vaultProvider) StoreKey(ctx context.Context, id, plaintext string) error {
//	    _, err := v.client.KVv2("secret").Put(ctx, "hyperstrate/keys/"+id,
//	        map[string]any{"value": plaintext})
//	    return err
//	}
type Provider interface {
	StoreKey(ctx context.Context, keyID, plaintext string) error
	LoadKey(ctx context.Context, keyID string) (string, error)
	DeleteKey(ctx context.Context, keyID string) error
}

// NoopProvider satisfies Provider but does nothing.
// Keys are validated via their SHA-256 hash stored in the database.
type NoopProvider struct{}

func (NoopProvider) StoreKey(_ context.Context, _, _ string) error { return nil }
func (NoopProvider) LoadKey(_ context.Context, keyID string) (string, error) {
	return "", fmt.Errorf("vault not configured: cannot load key %s", keyID)
}
func (NoopProvider) DeleteKey(_ context.Context, _ string) error { return nil }
