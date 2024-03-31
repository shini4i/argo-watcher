---
hide:
  - navigation
---

# Notifications

## WebHook Events

There is a possibility to send notifications to a remote server using WebHooks. The following events are being sent:

- Deployment started
- Deployment finished (successful/failed)

### Configuration

The following configuration options are available:

| Variable                             | Description                           | Default       | Example                                       |
|--------------------------------------|---------------------------------------|---------------|-----------------------------------------------|
| `WEBHOOK_ENABLED`                    | Enable WebHook notifications          | false         |                                               |
| `WEBHOOK_URL`                        | The URL to send the WebHook to        |               | https://example.com/events                    |
| `WEBHOOK_FORMAT`                     | A format string to send the WebHook   |               | `{"app": "{{.App}}","status": "{{.Status}}"}` |
| `WEBHOOK_AUTHORIZATION_HEADER_NAME`  | The name of the authorization header  | Authorization |                                               |
| `WEBHOOK_AUTHORIZATION_HEADER_VALUE` | The value of the authorization header |               | Bearer token                                  |

#### Available template variables

The following template variables can be used in the `WEBHOOK_FORMAT`:

| Variable  | Type    | Example        |
|-----------|---------|----------------|
| `Id`      | string  |                |
| `Created` | float64 |                |
| `Updated` | float64 |                |
| `App`     | string  | "argo-watcher" |
| `Author`  | string  | "John Doe"     |
| `Project` | string  | "Demo"         |
| `Images`  | []Image |                |
| `Status`  | string  |                |

Pay attention to variable types when using them in the format string.
