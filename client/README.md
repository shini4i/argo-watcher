# Client

This is just an example of what can be used within pipeline to communicate with argo-watcher

## Mandatory environment variable

| Variable | Description |
|---|---|
| ARGO_WATCHER_URL | The url of argo-watcher instance |
| ARGO_APP | The name of argo app to check for images rollout |
| COMMIT_AUTHOR | The person who made commit/triggered pipeline |
| PROJECT_NAME | An identificator of the business project (not related to argo project) |
| IMAGES | A list of images (separated by ",") that should contain specific tag |
| IMAGE_TAG | An image tag that is expected to be rolled out |

## Example configuration
### gitlab-ci
```yaml
await-deployment:
  image: ghcr.io/shini4i/argo-watcher-client:v0.0.7
  variables:
    ARGO_WATCHER_URL: argo-watcher.example.com
    ARGO_APP: example
    COMMIT_AUTHOR: $GITLAB_USER_EMAIL
    PROJECT_NAME: $CI_PROJECT_PATH
    IMAGES: $CI_REGISTRY_IMAGE
    IMAGE_TAG: $CI_PIPELINE_ID
  script: ["/bin/client"]
```
