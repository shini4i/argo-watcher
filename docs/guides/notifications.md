# Notifications

Argo Watcher can send deployment status notifications to external services via webhooks. This is useful for integrating with Slack, Microsoft Teams, PagerDuty, or any custom service that accepts HTTP POST requests.

## Webhook Events

The following events trigger a webhook notification:

- **Deployment started** -- Sent when a new deployment task is accepted.
- **Deployment finished** -- Sent when a deployment succeeds or fails.

## Configuration

| Variable                             | Description                                          | Default         | Example                                       |
|--------------------------------------|------------------------------------------------------|-----------------|-----------------------------------------------|
| `WEBHOOK_ENABLED`                    | Enable webhook notifications                         | `false`         |                                               |
| `WEBHOOK_URL`                        | URL to send the webhook POST request to             |                 | `https://example.com/events`                  |
| `WEBHOOK_CONTENT_TYPE`               | Content-Type header for webhook requests             | `application/json` |                                            |
| `WEBHOOK_FORMAT`                     | Go template string defining the webhook payload     |                 | `{"app": "{{.App}}", "status": "{{.Status}}"}` |
| `WEBHOOK_AUTHORIZATION_HEADER_NAME`  | Name of the authorization header                     | `Authorization` |                                               |
| `WEBHOOK_AUTHORIZATION_HEADER_VALUE` | Value of the authorization header                    |                 | `Bearer token`                                |
| `WEBHOOK_ALLOWED_RESPONSE_CODES`     | Comma-separated list of accepted HTTP response codes | `200`           | `200,201,202`                                 |

## Available Template Variables

The `WEBHOOK_FORMAT` value is a [Go template](https://pkg.go.dev/text/template) string. The following variables are available:

| Variable  | Type      | Description                               | Example          |
|-----------|-----------|-------------------------------------------|------------------|
| `Id`      | `string`  | Unique task identifier (UUID)             | `"be8c42c0-..."` |
| `Created` | `float64` | Task creation time (Unix timestamp)       | `1648390029.0`   |
| `Updated` | `float64` | Last update time (Unix timestamp)         | `1648390145.0`   |
| `App`     | `string`  | Argo CD application name                  | `"my-app"`       |
| `Author`  | `string`  | Person who triggered the deployment       | `"John Doe"`     |
| `Project` | `string`  | Business project identifier               | `"Demo"`         |
| `Images`  | `[]Image` | List of images being deployed (see below) |                  |
| `Status`  | `string`  | Current deployment status                 | `"deployed"`     |

### The Image Object

Each item in the `Images` list has the following fields:

| Field   | Type     | Description               | Example                                |
|---------|----------|---------------------------|----------------------------------------|
| `Image` | `string` | Full image name (no tag)  | `"ghcr.io/shini4i/argo-watcher"`       |
| `Tag`   | `string` | Image tag being deployed  | `"v0.8.0"`                             |

!!! tip
    Use `{{range .Images}}` to iterate over the images list in your template. Pay attention to variable types -- for example, `Created` and `Updated` are `float64` values, not strings.

## Examples

### Simple JSON Payload

```bash
WEBHOOK_FORMAT='{"app": "{{.App}}", "status": "{{.Status}}", "author": "{{.Author}}"}'
```

Produces:

```json
{
  "app": "my-app",
  "status": "deployed",
  "author": "John Doe"
}
```

### Detailed Payload with Images

```bash
WEBHOOK_FORMAT='{"app": "{{.App}}", "status": "{{.Status}}", "author": "{{.Author}}", "project": "{{.Project}}", "images": [{{range $i, $img := .Images}}{{if $i}},{{end}}{"image": "{{$img.Image}}", "tag": "{{$img.Tag}}"}{{end}}]}'
```

Produces:

```json
{
  "app": "my-app",
  "status": "deployed",
  "author": "John Doe",
  "project": "Demo",
  "images": [
    {"image": "ghcr.io/shini4i/argo-watcher", "tag": "v0.8.0"}
  ]
}
```

### Slack-Compatible Payload

```bash
WEBHOOK_FORMAT='{"text": "Deployment of *{{.App}}* by {{.Author}}: {{.Status}}"}'
```
