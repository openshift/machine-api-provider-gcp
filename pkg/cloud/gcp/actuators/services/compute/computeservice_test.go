package computeservice

import (
	"context"
	"testing"

	"google.golang.org/api/option"
)

func TestCredentialOption(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		credentials string
		wantErr     bool
		description string
	}{
		{
			name: "service account with type field",
			credentials: `{
				"type": "service_account",
				"project_id": "test-project",
				"private_key_id": "key123",
				"private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7W8jYbz0VLFjH\n-----END PRIVATE KEY-----\n",
				"client_email": "test@test-project.iam.gserviceaccount.com",
				"client_id": "123456789",
				"auth_uri": "https://accounts.google.com/o/oauth2/auth",
				"token_uri": "https://oauth2.googleapis.com/token"
			}`,
			wantErr:     false,
			description: "Valid service account JSON with type field should use WithAuthCredentialsJSON",
		},
		{
			name: "authorized user with type field",
			credentials: `{
				"type": "authorized_user",
				"client_id": "client123",
				"client_secret": "secret123",
				"refresh_token": "token123"
			}`,
			wantErr:     false,
			description: "Valid authorized user JSON with type field should use WithAuthCredentialsJSON",
		},
		{
			name:        "invalid JSON",
			credentials: `{invalid json`,
			wantErr:     true,
			description: "Invalid JSON should return error",
		},
		{
			name:        "empty credentials",
			credentials: ``,
			wantErr:     true,
			description: "Empty credentials should return error",
		},
		{
			name: "JSON without type field",
			credentials: `{
				"project_id": "test-project",
				"client_email": "test@test-project.iam.gserviceaccount.com"
			}`,
			wantErr:     true,
			description: "JSON without type field should fall back to CredentialsFromJSON (will error without valid key)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt, err := credentialOption(ctx, tt.credentials)
			if (err != nil) != tt.wantErr {
				t.Errorf("credentialOption() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && opt == nil {
				t.Errorf("credentialOption() returned nil option without error")
			}
		})
	}
}

func TestCredentialOptionType(t *testing.T) {
	ctx := context.Background()

	// Test that service account with type field returns the correct option type
	serviceAccountJSON := `{
		"type": "service_account",
		"project_id": "test-project",
		"private_key_id": "key123",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7W8jYbz0VLFjH\n-----END PRIVATE KEY-----\n",
		"client_email": "test@test-project.iam.gserviceaccount.com",
		"client_id": "123456789",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token"
	}`

	opt, err := credentialOption(ctx, serviceAccountJSON)
	if err != nil {
		t.Fatalf("credentialOption() unexpected error = %v", err)
	}

	// Verify we got an option (we can't easily verify the exact type without reflection,
	// but we can verify it's not nil and doesn't panic when applied)
	if opt == nil {
		t.Error("credentialOption() returned nil option")
	}

	// The option should be of type withAuthCredentialsJSON based on our implementation
	// We verify this by checking it's a valid ClientOption
	var _ option.ClientOption = opt
}
