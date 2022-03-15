# argo-watcher
PoC version of app that will watch if the required docker image was rolled out in ArgoCD.<br>
The reason for its existence is to provide clear visibility in pipelines that the specific code version is up and running w/o checking argocd ui.

## Example POST request
```bash
curl --connect-timeout 600 --header "Content-Type: application/json" --request POST --data '{"app":"test-app","images":[{"image":"example", "tag":"v1.8.0"}]}' http://localhost:8080/api/v1/status
```
### Response
There are two possible responses:
#### 200
```
{
  "deployed": true
}
```
#### 424
```
{
  "deployed": false
}
```
