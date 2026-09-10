package assets

import (
	"context"
	"testing"
)

type recordingSigner struct {
	key      string
	maxBytes int64
}

func (s *recordingSigner) PresignUpload(_ context.Context, _, key, _ string, maxBytes int64, _ string) (SignedUpload, error) {
	s.key = key
	s.maxBytes = maxBytes
	return SignedUpload{URL: "https://upload.example/signed", Method: "PUT"}, nil
}

func TestBrokerDecidesReleaseAssetUpload(t *testing.T) {
	tests := []struct {
		name           string
		request        UploadRequest
		wantAccepted   bool
		wantKey        string
		wantDiagnostic string
	}{
		{
			name:         "accepts a release artifact and scopes its key to the build",
			request:      UploadRequest{BuildEventID: "build-1842", ReleaseID: "v2.7.1", AssetName: "cli-linux-amd64.tar.gz", ContentType: "application/gzip", SizeBytes: 8 << 20},
			wantAccepted: true,
			wantKey:      "builds/build-1842/releases/v2.7.1/cli-linux-amd64.tar.gz",
		},
		{
			name:           "rejects an artifact above the release limit before signing",
			request:        UploadRequest{BuildEventID: "build-1842", ReleaseID: "v2.7.1", AssetName: "debug.zip", ContentType: "application/zip", SizeBytes: MaxAssetBytes + 1},
			wantDiagnostic: "asset_size_rejected",
		},
		{
			name:           "rejects a path-shaped asset name",
			request:        UploadRequest{BuildEventID: "build-1842", ReleaseID: "v2.7.1", AssetName: "other/debug.zip", ContentType: "application/zip", SizeBytes: 1024},
			wantDiagnostic: "invalid_asset_name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signer := &recordingSigner{}
			decision, err := NewBroker("developer-assets", signer).Decide(context.Background(), test.request)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Accepted != test.wantAccepted {
				t.Fatalf("Accepted = %v, want %v", decision.Accepted, test.wantAccepted)
			}
			if test.wantAccepted {
				if decision.ObjectKey != test.wantKey || signer.key != test.wantKey {
					t.Fatalf("object key = %q, signer key = %q", decision.ObjectKey, signer.key)
				}
				if signer.maxBytes != test.request.SizeBytes {
					t.Fatalf("max bytes = %d, want %d", signer.maxBytes, test.request.SizeBytes)
				}
				return
			}
			if decision.Diagnostic == nil || decision.Diagnostic.Code != test.wantDiagnostic {
				t.Fatalf("diagnostic = %#v, want code %q", decision.Diagnostic, test.wantDiagnostic)
			}
			if signer.key != "" {
				t.Fatalf("rejected request reached signer with key %q", signer.key)
			}
		})
	}
}
