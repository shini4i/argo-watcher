# Notifications

Argo Watcher reports deployments to external services in two ways: a generic webhook (Slack, Teams, PagerDuty, anything accepting an HTTP POST) and a Mattermost integration that threads its messages. Both can be enabled at once; each enabled strategy receives every event.

Two events are sent per deployment: one when the task is accepted (status `in progress`) and one when it reaches a final state (`deployed`, `failed`, `aborted`, `cancelled`, or `app not found`).

## Generic webhook

| Variable | Description | Default | Example |
|---|---|---|---|
| `WEBHOOK_ENABLED` | Enable webhook notifications | `false` | |
| `WEBHOOK_URL` | Where to POST | | `https://example.com/events` |
| `WEBHOOK_CONTENT_TYPE` | `Content-Type` of the request | `application/json` | |
| `WEBHOOK_FORMAT` | Go template rendering the request body | | `{"app": "{{.App}}", "status": "{{.Status}}"}` |
| `WEBHOOK_AUTHORIZATION_HEADER_NAME` | Header carrying the credential | `Authorization` | `X-Token` |
| `WEBHOOK_AUTHORIZATION_HEADER_VALUE` | Its value | | `Bearer token` |
| `WEBHOOK_ALLOWED_RESPONSE_CODES` | Response codes treated as success | `200` | `200,201,202` |

Argo Watcher does not sign the payload; the receiver authenticates the request through the authorization header above, so treat `WEBHOOK_URL` itself as a secret when the receiver has no other check.

### Template variables

`WEBHOOK_FORMAT` (and `MATTERMOST_FORMAT`) is a [Go template](https://pkg.go.dev/text/template) rendered over the task:

When `WEBHOOK_CONTENT_TYPE` is a JSON type, values are escaped for you before the template renders them, so a quote or a newline in `StatusReason` cannot break the body. Put each value inside a JSON string (`"{{.Author}}"`) and quote nothing yourself. For any other content type the values are rendered as they are.

!!! warning "If your format quotes values itself"
    A format that produces its own quotes — `{{printf "%q" .StatusReason}}` — now escapes an
    already-escaped value, and the receiver shows a literal `\n` instead of a line break.
    Drop the `printf` and wrap the value in quotes instead:

    ```diff
    - WEBHOOK_FORMAT='{"text": {{printf "%q" .StatusReason}}}'
    + WEBHOOK_FORMAT='{"text": "{{.StatusReason}}"}'
    ```

| Variable | Type | Description |
|---|---|---|
| `Id` | `string` | Task id (UUID) |
| `Created` | `float64` | Creation time, Unix seconds |
| `Updated` | `float64` | Last update, Unix seconds |
| `App` | `string` | Argo CD application name |
| `Author` | `string` | Who triggered the deployment |
| `Project` | `string` | Business project identifier |
| `Images` | `[]Image` | Images being deployed; each has `.Image` (name, no tag) and `.Tag` |
| `Status` | `string` | Current status, e.g. `deployed` |
| `StatusReason` | `string` | Why it failed; empty on success |
| `IsRollback` | `bool` | `true` when returning to a previously deployed version |
| `RollbackTargetId` | `string` | Id of the task being rolled back to; empty otherwise |

!!! tip
    `Created` and `Updated` are numbers, not strings. Iterate images with `{{range .Images}}`.

### Examples

A minimal JSON body:

```bash
WEBHOOK_FORMAT='{"app": "{{.App}}", "status": "{{.Status}}", "author": "{{.Author}}"}'
```

Every image, with tags:

```bash
WEBHOOK_FORMAT='{"app": "{{.App}}", "status": "{{.Status}}", "images": [{{range $i, $img := .Images}}{{if $i}},{{end}}{"image": "{{$img.Image}}", "tag": "{{$img.Tag}}"}{{end}}]}'
```

A Slack message that calls out rollbacks and failure reasons:

```bash
WEBHOOK_FORMAT='{"text": "{{if .IsRollback}}:rewind: ROLLBACK of {{else}}Deployment of {{end}}*{{.App}}* by {{.Author}}: {{.Status}}{{with .StatusReason}} — {{.}}{{end}}"}'
```

## Mattermost

The generic webhook posts each event independently, which gets noisy. The Mattermost strategy uses the REST API instead: the start event creates a root post and the result is a **thread reply** to it, mentioning the author.

It needs a [bot account](https://docs.mattermost.com/integrations/cloud-bot-accounts.html) with access to the channel — incoming webhooks cannot reply in threads.

| Variable | Description | Default |
|---|---|---|
| `MATTERMOST_ENABLED` | Enable Mattermost notifications | `false` |
| `MATTERMOST_URL` | Base URL of the instance, without `/api/v4` | |
| `MATTERMOST_TOKEN` | Bot access token | |
| `MATTERMOST_CHANNEL_ID` | Target channel id (the 26-character id, not the name) | |
| `MATTERMOST_FORMAT` | Go template rendering the post (markdown) | |
| `MATTERMOST_MENTION_AUTHOR` | Prepend `@<Author>` to every post | `false` |

Branch on `{{.Status}}` to tell the start post from the result:

```bash
MATTERMOST_FORMAT='{{if eq .Status "in progress"}}:rocket: Deploying **{{.App}}** {{range $i, $img := .Images}}{{if $i}}, {{end}}`{{$img.Tag}}`{{end}}{{else if eq .Status "deployed"}}:white_check_mark: **{{.App}}** deployed{{else}}:x: **{{.App}}**: {{.Status}}{{end}}'
```

With `MATTERMOST_MENTION_AUTHOR=true` the mention is prepended for you, so the template does not need `{{.Author}}`. It only notifies someone when `Author` happens to match a Mattermost username.

!!! note
    The link between a start post and its thread is held in memory. If Argo Watcher restarts mid-deployment — or runs with several replicas — the result is posted as a normal channel message instead of a reply.
