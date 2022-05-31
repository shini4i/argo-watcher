# Client

This is just an example of what can be used within pipeline to communicate with argo-watcher

## Environment variable

| Variable | Description | Mandatory |
|---|---|---|
| ARGO_WATCHER_URL | The url of argo-watcher instance | Yes
| ARGO_APP | The name of argo app to check for images rollout | Yes
| COMMIT_AUTHOR | The person who made commit/triggered pipeline | Yes
| PROJECT_NAME | An identificator of the business project (not related to argo project) | Yes
| IMAGES | A list of images (separated by ",") that should contain specific tag | Yes
| IMAGE_TAG | An image tag that is expected to be rolled out | Yes
| DEBUG | Print various debug information | No

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
    DEBUG: True
  script: ["/bin/client"]
```
