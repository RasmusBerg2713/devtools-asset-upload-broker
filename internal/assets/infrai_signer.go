package assets

import (
	"context"

	"example.com/devtools-asset-upload/internal/infrai"
)

type InfraiSigner struct {
	client *infrai.Client
}

func NewInfraiSigner(client *infrai.Client) *InfraiSigner {
	return &InfraiSigner{client: client}
}

func (s *InfraiSigner) PresignUpload(ctx context.Context, bucket, key, contentType string, maxBytes int64, idempotencyKey string) (SignedUpload, error) {
	result, err := s.client.PresignUpload(ctx, bucket, key, contentType, maxBytes, idempotencyKey)
	if err != nil {
		return SignedUpload{}, err
	}
	if result.URL == "" {
		return SignedUpload{}, ErrSignerContract
	}
	return SignedUpload{URL: result.URL, Method: result.Method}, nil
}
