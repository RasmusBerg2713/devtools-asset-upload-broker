package assets

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const MaxAssetBytes int64 = 50 << 20

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

type UploadRequest struct {
	BuildEventID string `json:"build_event_id"`
	ReleaseID    string `json:"release_id"`
	AssetName    string `json:"asset_name"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type Decision struct {
	Accepted       bool        `json:"accepted"`
	ObjectKey      string      `json:"object_key,omitempty"`
	UploadURL      string      `json:"upload_url,omitempty"`
	UploadMethod   string      `json:"upload_method,omitempty"`
	ExpiresSeconds int         `json:"expires_seconds,omitempty"`
	Diagnostic     *Diagnostic `json:"diagnostic,omitempty"`
}

type SignedUpload struct {
	URL    string
	Method string
}

type Signer interface {
	PresignUpload(context.Context, string, string, string, int64, string) (SignedUpload, error)
}

type Broker struct {
	bucket string
	signer Signer
}

func NewBroker(bucket string, signer Signer) *Broker {
	return &Broker{bucket: bucket, signer: signer}
}

func (b *Broker) Decide(ctx context.Context, request UploadRequest) (Decision, error) {
	key, diagnostic := objectKey(request)
	if diagnostic != nil {
		return Decision{Accepted: false, Diagnostic: diagnostic}, nil
	}
	signed, err := b.signer.PresignUpload(ctx, b.bucket, key, request.ContentType, request.SizeBytes, request.BuildEventID+":"+request.ReleaseID+":"+request.AssetName)
	if err != nil {
		return Decision{}, err
	}
	method := signed.Method
	if method == "" {
		method = "PUT"
	}
	return Decision{
		Accepted:       true,
		ObjectKey:      key,
		UploadURL:      signed.URL,
		UploadMethod:   method,
		ExpiresSeconds: 600,
	}, nil
}

func objectKey(request UploadRequest) (string, *Diagnostic) {
	if !safeID.MatchString(request.BuildEventID) {
		return "", &Diagnostic{Code: "invalid_build_event", Message: "build_event_id must be a safe identifier", Field: "build_event_id"}
	}
	if !safeID.MatchString(request.ReleaseID) {
		return "", &Diagnostic{Code: "invalid_release", Message: "release_id must be a safe identifier", Field: "release_id"}
	}
	name := filepath.Base(strings.TrimSpace(request.AssetName))
	if name == "." || name == "" || name != request.AssetName || !safeID.MatchString(name) {
		return "", &Diagnostic{Code: "invalid_asset_name", Message: "asset_name must be a plain file name", Field: "asset_name"}
	}
	if request.SizeBytes <= 0 || request.SizeBytes > MaxAssetBytes {
		return "", &Diagnostic{Code: "asset_size_rejected", Message: fmt.Sprintf("size_bytes must be between 1 and %d", MaxAssetBytes), Field: "size_bytes"}
	}
	if request.ContentType == "" || !strings.Contains(request.ContentType, "/") {
		return "", &Diagnostic{Code: "invalid_content_type", Message: "content_type must be a media type", Field: "content_type"}
	}
	return fmt.Sprintf("builds/%s/releases/%s/%s", request.BuildEventID, request.ReleaseID, name), nil
}

var ErrSignerContract = errors.New("signer returned an empty URL")
