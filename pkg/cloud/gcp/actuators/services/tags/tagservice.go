package tagservice

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/oauth2/google"
	tags "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/option"
)

// TagService is a pass through wrapper for google.golang.org/api/cloudresourcemanager/v3
// to enable tests to mock this struct and control behavior.
type TagService interface {
	GetNamespacedName(context.Context, string) (*tags.TagValue, error)
}

// tagService implements TagService interface.
type tagService struct {
	tagValuesService *tags.TagValuesService
}

// BuilderFuncType is function type for building GCP tag client.
type BuilderFuncType func(ctx context.Context, serviceAccountJSON string) (TagService, error)

// credentialOption returns the appropriate client option for the service account JSON.
// When raw credential JSON is available and contains a type field, WithAuthCredentialsJSON
// is used so that the Google API library can apply self-signed JWT authentication for
// non-default universe domains (e.g., Google Cloud Dedicated).
func credentialOption(ctx context.Context, serviceAccountJSON string) (option.ClientOption, error) {
	credJSON := []byte(serviceAccountJSON)

	var f struct {
		Type option.CredentialsType `json:"type"`
	}
	if err := json.Unmarshal(credJSON, &f); err == nil && f.Type != "" {
		return option.WithAuthCredentialsJSON(f.Type, credJSON), nil
	}

	// Fall back to traditional credentials for cases without type field
	creds, err := google.CredentialsFromJSON(ctx, credJSON, tags.CloudPlatformScope)
	if err != nil {
		return nil, err
	}
	return option.WithCredentials(creds), nil
}

// NewTagService return a new tagService.
func NewTagService(ctx context.Context, serviceAccountJSON string) (TagService, error) {
	credOpt, err := credentialOption(ctx, serviceAccountJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential option: %w", err)
	}

	service, err := tags.NewService(ctx, credOpt)
	if err != nil {
		return nil, fmt.Errorf("could not create new tag service: %w", err)
	}

	return &tagService{
		tagValuesService: tags.NewTagValuesService(service),
	}, nil
}

// GetNamespacedName returns the tag's metadata fetched using its namespaced name.
func (t *tagService) GetNamespacedName(ctx context.Context, namespacedName string) (*tags.TagValue, error) {
	return t.tagValuesService.GetNamespaced().
		Context(ctx).
		Name(namespacedName).
		Do()
}
