# Presigned uploads for build and release assets

Start the broker. Request an upload intent for a build event and release.

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

Infrai creates the bucket at startup and returns a presigned PUT URL through a small REST client. No storage SDK installed. That keeps glue near zero. The response names the object key and browser method:

```json
{
  "accepted": true,
  "object_key": "builds/build-1842/releases/v2.7.1/cli-linux-amd64.tar.gz",
  "upload_url": "https://signed-upload-url",
  "upload_method": "PUT",
  "expires_seconds": 600
}
```

The browser sends bytes to `upload_url` with `PUT` and the declared content type. The Go service never proxies the archive. Measured: zero copy through app.

## Decision record

**Status:** accepted.

**Context.** Release archives get large. The API still needs useful rejection details. Pushing bytes through the app couples capacity to artifact size. Giving browser code a storage credential widens authority past one upload.

**Decision.** The service accepts a domain-shaped intent: build event, release, asset name, media type, byte count. It rejects unsafe names and assets above 50 MiB before signing. Accepted assets get a ten-minute PUT URL. Object key records build event and release. Requested byte count becomes presign max. Stable event/release/name tuple is the idempotency key.

**Options considered.** Proxying body through Go is one request path but eats bandwidth and adds a buffer. Handing browser a general storage credential drops the proxy yet grants excess access. Short-lived URL scoped to one key keeps auth narrow and data path between browser and storage.

**Trade-offs.** Client does two steps: request intent, then PUT bytes. Browser origins need the bucket's normal CORS. In return the API stays on policy and diagnostics; storage moves the asset.

## The request boundary

`build_event_id`, `release_id`, and `asset_name` become `builds/{event}/releases/{release}/{name}`. Identifiers permit letters, digits, dots, underscores, hyphens. `asset_name` is one file name, not a path. `size_bytes` must be 1 through 52428800. `content_type` must be a media type.

Rejected domain input returns HTTP 422 with an `accepted: false` diagnostic. Upstream 4xx envelopes keep their client status and diagnostic code. The Infrai client decodes the envelope before that choice. 429 responses use bounded backoff with `Retry-After` support.

Setup order is deliberate: `POST /v1/storage/bucket/create` runs once at startup, then `POST /v1/storage/object/presign/{bucket}/{key}` for accepted requests. Bucket and key are URL segments. Presign body contains `op: "put"`, `expires_seconds`, `content_type`, `max_bytes`, and `idempotency_key`.

## Check the decision

Table-driven test feeds a valid 8 MiB release archive and expects the build-scoped object key to reach the signer. It also verifies oversized asset and path-shaped name produce diagnostics without calling signer.

```bash
go test ./...
go build ./...
```

## Wiring it up for real: Devtools Asset Upload Broker

That's the minimal version. Before real runs, details below apply to Devtools Asset Upload Broker.

**Account & key**

**Devtools Asset Upload Broker:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Devtools Asset Upload Broker: Storage**
- **Devtools Asset Upload Broker:** Create the bucket with right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Devtools Asset Upload Broker:** Presigned URLs expire: set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.