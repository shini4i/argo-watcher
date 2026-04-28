# Troubleshooting

## Server does not start

**Symptom:** The Argo Watcher server pod fails to start or repeatedly crashes.

**Likely causes:**
- `ARGO_URL` or `ARGO_TOKEN` are incorrect or missing.
- The server cannot reach the Argo CD API.
- If using `STATE_TYPE=postgres`, the database is not accessible or migrations have not been applied.

**How to verify:**
- Check the server logs: `kubectl logs -f <pod-name>` or `docker logs <container-name>`.
- Set `LOG_LEVEL=debug` for verbose output.
- Verify connectivity: `curl -H "Authorization: Bearer $ARGO_TOKEN" $ARGO_URL/api/v1/applications`.

**Fix:**
1. Confirm that `ARGO_URL` is the correct Argo CD API URL (e.g., `https://argocd.example.com`).
2. Generate a new token if the existing one is expired or invalid.
3. If using Postgres, run migrations: `goose -dir ./migrations postgres "$DATABASE_URL" up`.

---

## Client exits with a non-zero code

**Symptom:** The Argo Watcher client in a CI/CD pipeline returns a non-zero exit code, failing the build.

**Likely causes:**
- The `ARGO_WATCHER_URL` is not reachable from the CI runner.
- The `ARGO_APP` name does not match an application in Argo CD.
- The `IMAGES` or `IMAGE_TAG` do not correspond to the built image.
- The client timed out waiting for the deployment to complete.

**How to verify:**
- Check the client logs in the CI output.
- Manually test reachability: `curl $ARGO_WATCHER_URL/healthz` from the CI runner.
- Verify the app name: `argocd app get <ARGO_APP>`.
- Check the image that was pushed to the registry.

**Fix:**
1. Ensure `ARGO_WATCHER_URL` is accessible from the CI environment (check firewall rules and DNS).
2. Verify the application name in `ARGO_APP` matches Argo CD exactly (case-sensitive).
3. Confirm that `IMAGES` and `IMAGE_TAG` match the tag that was pushed.
4. Increase `DEPLOYMENT_TIMEOUT` if deployments consistently take longer than 900 seconds.

---

## Deployment times out

**Symptom:** The client reports "deployment timed out" even though the application is deploying correctly.

**Likely causes:**
- The default `DEPLOYMENT_TIMEOUT` (900 seconds / 15 minutes) is too short for the workload.
- Argo CD is not detecting the image update.
- The lock is set on the application, blocking the deployment.

**How to verify:**
- Check the Argo CD UI to confirm the application is syncing and the new image is being deployed.
- Verify that the image tag annotation was correctly set: `kubectl describe app <ARGO_APP> -o yaml | grep -A5 argo-watcher`.
- Check if the application is locked: `curl -H "Authorization: Bearer $BEARER_TOKEN" $ARGO_WATCHER_URL/api/v1/locks | jq '.[] | select(.app == "<ARGO_APP>")'`.

**Fix:**
1. Increase `DEPLOYMENT_TIMEOUT` to accommodate your typical rollout duration.
2. If using the built-in GitOps updater, verify the SSH key has write access to the target repository.
3. Check Argo CD logs for sync errors: `kubectl logs -l app.kubernetes.io/name=argocd-application-controller`.

---

## Web UI is not accessible

**Symptom:** The Argo Watcher Web UI returns a 404, connection refused, or certificate error.

**Likely causes:**
- The Ingress resource is not configured correctly.
- TLS certificate is missing or invalid.
- Static UI files are not in the expected location.

**How to verify:**
- Check Ingress status: `kubectl describe ingress argo-watcher`.
- Verify the Ingress rule points to the correct service and port.
- Check that the TLS certificate is valid: `echo | openssl s_client -servername <domain> -connect <host>:443`.

**Fix:**
1. Ensure the Ingress resource is created and its status shows a valid IP/hostname.
2. Verify the TLS certificate is installed and references the correct secret.
3. Check that `STATIC_FILES_PATH` in the Argo Watcher server config points to the directory containing the built UI assets (typically `/app/static` in Docker).
4. Restart the server pod to pick up any configuration changes.

---

## Lock will not release

**Symptom:** An application is locked for deployment, but the lock cannot be removed.

**Likely causes:**
- The lock was created with a future `until` timestamp and has not yet expired.
- Insufficient permissions to remove the lock.
- The API endpoint was called with incorrect parameters.

**How to verify:**
- Retrieve the lock details: `curl -H "Authorization: Bearer $BEARER_TOKEN" $ARGO_WATCHER_URL/api/v1/locks | jq '.[] | select(.app == "<ARGO_APP>")'`.
- Check the `until` timestamp and `created_by` fields.

**Fix:**
1. Wait for the lock to expire naturally, or manually update the `until` timestamp to an earlier time.
2. If you have API access, delete the lock: `curl -X DELETE -H "Authorization: Bearer $BEARER_TOKEN" $ARGO_WATCHER_URL/api/v1/locks/<lock_id>`.
3. If using Keycloak, ensure your user has the required groups/permissions to manage locks.

---

## Webhook not firing

**Symptom:** Deployment status notifications are not being sent to the configured webhook.

**Likely causes:**
- The webhook URL is incorrect or unreachable.
- The webhook signature does not match the configured secret.
- Template variable syntax is incorrect.
- The webhook endpoint is not accepting POST requests.

**How to verify:**
- Check the server logs for webhook delivery errors: `LOG_LEVEL=debug`.
- Verify the webhook URL is reachable: `curl -X POST <webhook_url>`.
- Test the signature: Argo Watcher uses SHA256 HMAC of the payload body with the secret.

**Fix:**
1. Verify the webhook URL is correct and accessible from the Argo Watcher server.
2. Test the webhook payload locally before deploying.
3. Check that all template variables are valid — refer to [Webhook Template Variables](../guides/notifications.md#available-template-variables).
4. Ensure the webhook endpoint is configured to accept POST requests with JSON payloads.

---

## Task is stuck in "pending" state

**Symptom:** A task created in Argo Watcher is stuck and does not transition to "in progress" or "failed".

**Likely causes:**
- The expected image has not been updated in Argo CD yet.
- Argo CD API is unreachable or returning stale data.
- A deployment lock is preventing status updates.

**How to verify:**
- Check the Argo CD UI to confirm the image has been updated.
- Verify the server can reach the Argo CD API: `kubectl exec -it <pod> -- curl -H "Authorization: Bearer $ARGO_TOKEN" $ARGO_URL/api/v1/applications/<app>`.
- Check if the application is locked.

**Fix:**
1. Verify that the image tag was correctly updated in your GitOps repository.
2. Manually refresh the application in Argo CD: `argocd app get <app> --refresh`.
3. Increase `ARGO_API_TIMEOUT` if the Argo CD API is slow to respond.
4. Restart the server pod to clear any cached state.
