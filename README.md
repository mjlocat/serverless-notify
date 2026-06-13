# serverless-notify

A serverless, [Gotify](https://gotify.net)-compatible notification backend on AWS.
Drop-in for the Gotify server: existing senders (`POST /message?token=…`) and the
**stock Gotify Android app** (REST + WebSocket `/stream`) work unmodified — only the
server URL changes.

## Architecture

```
 Senders / Android app  ──HTTPS+WSS──▶  CloudFront (single host: notify.example.com)
                                          │
                                          ├─ /stream*  ─▶ API Gateway WebSocket API ─▶ ws  Lambda
                                          ├─ /image/*  ─▶ S3 (app icons, optional)
                                          └─ default   ─▶ API Gateway HTTP API      ─▶ api Lambda
                                                                                         │
                              new message ──postToConnection()──▶ live WS connections    │
                                                                                         ▼
                                                          DynamoDB (single table) + SSM (credentials)
```

Why CloudFront: the stock app derives `wss://host/stream` from the same base URL as
REST, but API Gateway will not let a WebSocket API and an HTTP API share a custom
domain. CloudFront fronts both under one hostname; a CloudFront Function rewrites the
`/stream` handshake to the WebSocket API's stage path.

- **api Lambda** (`cmd/api`): all Gotify REST endpoints + sender `POST /message`;
  fans new messages out to live WebSocket connections.
- **ws Lambda** (`cmd/ws`): `$connect` (token auth + store connection), `$disconnect`,
  `$default`.
- **DynamoDB**: applications, clients, messages, connections, and an atomic id counter
  (Gotify needs monotonic integer message ids). Layout in `internal/store/store.go`.
- **Auth**: Gotify-shaped `A…`/`C…` tokens; single-user HTTP Basic stored encrypted in
  SSM (KMS).

## Layout

```
cmd/api, cmd/ws        Lambda entry points
internal/gotify        wire types + token generator
internal/store         DynamoDB access layer
internal/auth          token / basic-auth helpers
terraform/             all infrastructure
```

## Prerequisites

- Go 1.23+, Terraform 1.5+, AWS credentials with rights to create the stack.
- A Route53 hosted zone for `example.com` (already exists) — DNS validation and the
  alias record are created automatically.

## Build & deploy

```bash
make build                 # cross-compiles arm64 bootstrap binaries into dist/

cd terraform
cp terraform.tfvars.example terraform.tfvars   # set basic_auth_username/password
terraform init
terraform plan
terraform apply            # CloudFront + ACM validation takes a few minutes
```

`make build` must run before `terraform plan/apply` (Terraform zips `dist/api` and
`dist/ws`). Re-run both after any Go change.

## First-time setup

1. Create an application token for each sender:
   ```bash
   curl -u "$USER:$PASS" -X POST https://notify.example.com/application \
     -H 'Content-Type: application/json' -d '{"name":"my-sender"}'
   # → returns {"token":"A…"}  — use it as ?token= in POST /message
   ```
2. In the Gotify Android app, add server `https://notify.example.com`, log in with the
   configured username/password. The app registers its own client (`C…`) token and
   connects the stream.

## Application icons

New applications start with the shared default icon (`static/defaultapp.png`, uploaded by
Terraform). To give an application its own icon, `POST` an image as multipart `file` to
`/application/{id}/image` (the same call the Android app's icon picker makes):

```bash
curl -u "$USER:$PASS" -X POST \
  https://notify.example.com/application/2/image \
  -F "file=@myicon.png"
# → returns the application with image set to "image/2-<random>.png"
```

The icon is stored in the private images S3 bucket and served via CloudFront at
`/image/*`; the Android app picks up the new icon on its next application refresh.
PNG, JPEG, GIF, and WebP are accepted (max 8 MB). Replacing an icon leaves the previous
object in the bucket — add an S3 lifecycle rule if you want them pruned.

> In Postman: choose **Body → form-data**, set the key to `file`, switch its type from
> *Text* to *File*, and select the image. Don't set `Content-Type` manually — let Postman
> add the multipart boundary.

## Verification

```bash
# Health / version
curl https://notify.example.com/health
curl https://notify.example.com/version

# Send a message
curl -X POST "https://notify.example.com/message?token=A…" \
  -F "title=Test" -F "message=hello" -F "priority=5"

# List messages (basic auth or client token)
curl -u "$USER:$PASS" https://notify.example.com/message

# Live WebSocket: connect, then POST a message in another shell and watch it arrive
wscat -c "wss://notify.example.com/stream?token=C…"
```

End-to-end: point the real Android app at the host, send a notification, confirm live
delivery. To validate reconnect handling, leave it connected past the 2-hour API
Gateway limit (or force a disconnect), send messages during the gap, and confirm the
app reconnects and back-fills via `GET /message?since=…`.

## Cost & limitations

- Effectively pennies/month: one always-connected phone is ~44k WebSocket
  connection-minutes (~$0.01) plus negligible Lambda + DynamoDB on-demand + CloudFront.
- API Gateway WebSocket connections have a **10-min idle timeout** and a hard **2-hour
  max duration**. The app auto-reconnects and re-fetches missed messages, so nothing is
  lost, but there can be a brief delivery delay across a reconnect. True push (FCM /
  UnifiedPush) would remove this but requires a modified app — deferred to the backlog.

## License

[MIT](LICENSE). Gotify itself is also MIT-licensed; this project reimplements its API
and does not include Gotify source.
