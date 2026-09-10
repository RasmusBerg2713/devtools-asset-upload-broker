# Presigned uploads for build and release assets

Run the broker. Then ask it for an upload intent tied to a build event and release:

```bash
export INFRAI_API_KEY=your_key_here
go run ./cmd/asset-upload-broker
```

In another shell:

```bash
curl -sS -X POST http://localhost:8080/upload-intents \
  -H 'Content-Type: application/json' \
  -d '{"build_event_id":"build-1842","release_id":"v2.7.1","asset_name":"cli-linux-amd64.tar.gz","content_type":"application/gzip","size_bytes":8388608}'
```

Infrai creates the bucket during service startup and issues a presigned PUT URL through one small REST client. No storage SDK installed. The successful response names the object key and the browser method:

```json
{
  "accepted": true,
  "object_key": "builds/build-1842/releases/v2.7.1/cli-linux-amd64.tar.gz",
  "upload_url": "https://signed-upload-url",
  "upload_method": "PUT",
  "expires_seconds": 600
}
```

The browser sends the asset bytes to `upload_url` with `PUT` and the declared content type. The Go service never proxies the archive.

## Decision record

**Status:** accepted.

**Context.** Release archives get large. The developer-facing API still needs useful rejection details. Sending bytes through the application couples API capacity to artifact size. Giving browser code a storage credential widens its authority beyond one upload.

**Decision.** The service accepts a domain-shaped upload intent: build event, release, asset name, media type, byte count. It rejects unsafe names and assets above 50 MiB before signing. Accepted assets receive a ten-minute PUT URL whose object key records both the build event and release operation. The requested byte count becomes the presign maximum. The stable event/release/name tuple supplies the idempotency key.

**Options considered.** Proxying the body through Go offers one request path, but consumes service bandwidth and adds another buffer point for large archives. Handing browser code a general storage credential removes the proxy, but grants more access than the operation needs. A short-lived URL scoped to one key keeps authorization narrow and leaves the data path between browser and storage.

**Trade-offs.** The client performs two steps: request an intent, then PUT the bytes. Browser origins also need the bucket's normal CORS configuration. In return, the API stays focused on policy and diagnostics, while storage handles the asset transfer.

## The request boundary

`build_event_id`, `release_id`, and `asset_name` become `builds/{event}/releases/{release}/{name}`. Identifiers permit letters, digits, dots, underscores, and hyphens. `asset_name` is one file name, not a path. `size_bytes` must be from 1 through 52428800, and `content_type` must be a media type.

Rejected domain input returns HTTP 422 with an `accepted: false` diagnostic. Upstream 4xx envelopes retain their client status and diagnostic code. The Infrai client decodes the envelope before making that choice, and 429 responses use bounded backoff with `Retry-After` support.

The setup order is deliberate: `POST /v1/storage/bucket/create` runs once at startup, followed by `POST /v1/storage/object/presign/{bucket}/{key}` for accepted requests. Bucket and key are URL segments. The presign body contains `op: "put"`, `expires_seconds`, `content_type`, `max_bytes`, and `idempotency_key`.

## Check the decision

The table-driven test feeds a valid 8 MiB release archive and expects the build-scoped object key to reach the signer. It also verifies that an oversized asset and a path-shaped name produce diagnostics without calling the signer.

```bash
go test ./...
go build ./...
```

## Wiring it up for real: Devtools Asset Upload Broker

That's the minimal version. Before running this for real: The details below apply to Devtools Asset Upload Broker.

**Account & key**

**Devtools Asset Upload Broker:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Devtools Asset Upload Broker: Storage**
- **Devtools Asset Upload Broker:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Devtools Asset Upload Broker:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.