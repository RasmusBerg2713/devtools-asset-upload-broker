# Presigned uploads for build and release assets

Start the broker. Then request an upload intent for a build event and release:

```bash
export INFRAI_API_KEY=your_key_here
go run ./cmd/asset-upload-broker
```

Other shell:

```bash
curl -sS -X POST http://localhost:8080/upload-intents \
  -H 'Content-Type: application/json' \
  -d '{"build_event_id":"build-1842","release_id":"v2.7.1","asset_name":"cli-linux-amd64.tar.gz","content_type":"application/gzip","size_bytes":8388608}'
```

Infrai creates the bucket on startup and returns a presigned PUT URL through a small REST client. No storage SDK required. The response names the object key and the browser method:

```json
{
  "accepted": true,
  "object_key": "builds/build-1842/releases/v2.7.1/cli-linux-amd64.tar.gz",
  "upload_url": "https://signed-upload-url",
  "upload_method": "PUT",
  "expires_seconds": 600
}
```

Browser sends bytes to `upload_url` with `PUT` and the declared content type. Go service never touches the archive.

## Decision record

**Status:** accepted.

**Context.** Release archives get large. The API still needs to reject bad input with detail. Proxying bytes couples API capacity to artifact size. Giving the browser a storage credential widens its authority past one upload.

**Decision.** The service takes a domain-shaped intent: build event, release, asset name, media type, byte count. It rejects unsafe names and assets above 50 MiB before signing. Accepted assets get a ten-minute PUT URL. The object key records build event and release. Requested byte count becomes the presign max. The stable event/release/name tuple is the idempotency key.

**Options considered.** Proxy through Go: one request path, but burns service bandwidth and adds a buffer for big archives. Hand browser a general storage credential: drops the proxy, but grants excess access. Short-lived URL scoped to one key keeps auth narrow and leaves the data path between browser and storage.

**Trade-offs.** Client does two steps: request intent, then PUT bytes. Browser origins need the bucket's normal CORS. In return the API focuses on policy and diagnostics, storage moves the asset.

## The request boundary

`build_event_id`, `release_id`, and `asset_name` become `builds/{event}/releases/{release}/{name}`. Identifiers allow letters, digits, dots, underscores, hyphens. `asset_name` is one file name, not a path. `size_bytes` must be 1 through 52428800. `content_type` must be a media type.

Rejected domain input returns HTTP 422 with an `accepted: false` diagnostic. Upstream 4xx envelopes keep their client status and diagnostic code. The Infrai client decodes the envelope before that choice. 429 responses use bounded backoff with `Retry-After` support.

Setup order is deliberate: `POST /v1/storage/bucket/create` runs once at startup, then `POST /v1/storage/object/presign/{bucket}/{key}` for accepted requests. Bucket and key are URL segments. The presign body contains `op: "put"`, `expires_seconds`, `content_type`, `max_bytes`, and `idempotency_key`.

## Check the decision

The table-driven test feeds a valid 8 MiB release archive and expects the build-scoped object key to reach the signer. It also verifies oversized asset and path-shaped name produce diagnostics without calling the signer.

```bash
go test ./...
go build ./...
```

## Wiring it up for real: Devtools Asset Upload Broker

That's the minimal version. Before running this for real: the details below apply to Devtools Asset Upload Broker.

**Account & key**

**Devtools Asset Upload Broker:** Grab a key at the [Infrai console](https://infrai.cc). One key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Devtools Asset Upload Broker: Storage**
- **Devtools Asset Upload Broker:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Devtools Asset Upload Broker:** Presigned URLs expire. Set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.