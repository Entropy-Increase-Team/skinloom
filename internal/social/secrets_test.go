package social

import (
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// A saved clientSecret is write-only. An administrator could read it anyway by keeping
// it redacted while pointing the provider somewhere else (type oidc, an issuer they
// control) and starting a sign-in: the secret was restored and sent to that issuer. A
// redacted secret is now kept only while its type, issuer and tenant are unchanged.
func TestRestoreDropsSecretForNewEndpoint(t *testing.T) {
	stored := []any{map[string]any{"id": "gh", "type": "github", "clientId": "id", "clientSecret": "real-secret"}}
	edited := []any{map[string]any{"id": "gh", "type": "oidc", "issuer": "https://attacker.example",
		"clientId": "id", "clientSecret": settings.Redacted}}
	out := restoreSecrets(stored, edited).([]any)[0].(map[string]any)
	if out["clientSecret"] == "real-secret" {
		t.Fatalf("the saved GitHub secret is restored into a provider now sent to %v", out["issuer"])
	}
}
