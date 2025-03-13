package tagservice

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"

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
type BuilderFuncType func(ctx context.Context, serviceAccountJSON string, endpoint *configv1.GCPServiceEndpoint) (TagService, error)

// NewTagService return a new tagService.
func NewTagService(ctx context.Context, serviceAccountJSON string, endpoint *configv1.GCPServiceEndpoint) (TagService, error) {
	options := []option.ClientOption{
		option.WithCredentialsJSON([]byte(serviceAccountJSON)),
	}
	if endpoint != nil && endpoint.URL != "" {
		options = append(options, option.WithEndpoint(endpoint.URL))
	}

	service, err := tags.NewService(ctx, options...)
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
