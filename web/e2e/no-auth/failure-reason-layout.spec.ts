import { expect, test } from '@playwright/test';
import { seedTask, waitForDeployed } from '../helpers';

/**
 * @description The full failure reason on the task detail page. Argo CD quotes
 * object specs as JSON whose runs between break opportunities are wider than
 * the viewport, and only a real layout engine can tell whether those runs wrap
 * or widen the page — jsdom computes no widths, so the unit tests can only
 * assert declared CSS.
 */

/** Shaped after a real sync failure: a rejected Job with its spec quoted inline. */
const SYNC_FAILURE = [
  'Sync operation phase: Failed',
  'Sync operation message: one or more objects failed to apply, reason: error when replacing "/dev/shm/2264556553": ' +
    'Job.batch "demo-rollout-prepull-a99ac87a017e" is invalid: [spec.selector: Required value, spec.template: Invalid value: ' +
    '{"labels":{"app.kubernetes.io/instance":"demo-stage","app.kubernetes.io/name":"demo-rollout-prepull"},' +
    '"Spec":{"Volumes":null,"InitContainers":null,"Containers":[{"Name":"rollout-prepull",' +
    '"Image":"registry.example.com/platform/demo:26.14.16-stage","Env":[{"Name":"DEMO_PREPULL_TAG","Value":"26.14.16-stage","ValueFrom":null}],' +
    '"Resources":{"Limits":{"cpu":"100m","memory":"128Mi"},"Requests":{"cpu":"10m","memory":"32Mi"},"Claims":null},' +
    '"ResizePolicy":null,"RestartPolicy":null,"TerminationMessagePath":"/dev/termination-log","TerminationMessagePolicy":"File",' +
    '"ImagePullPolicy":"IfNotPresent","SecurityContext":{"Capabilities":{"Add":null,"Drop":["ALL"]},"Privileged":null,' +
    '"SELinuxOptions":null,"WindowsOptions":null,"RunAsUser":9999,"RunAsGroup":null,"RunAsNonRoot":true,' +
    '"ReadOnlyRootFilesystem":null,"AllowPrivilegeEscalation":false,"ProcMount":null,"SeccompProfile":null}}]}}',
].join('\n');

for (const viewport of [
  { width: 1280, height: 900 },
  { width: 390, height: 844 },
]) {
  test(`the raw reason wraps instead of widening the page at ${viewport.width}px`, async ({ page, request }) => {
    const id = await seedTask(request, `layout-failure-reason-${viewport.width}`);
    await waitForDeployed(request, id);

    // The mock Argo CD only ever reports success, so the failure is injected
    // into the task the page reads rather than produced by a real sync.
    await page.route(`**/api/v1/tasks/${id}`, async route => {
      const response = await route.fetch();
      const task = (await response.json()) as Record<string, unknown>;
      await route.fulfill({ response, json: { ...task, status: 'failed', status_reason: SYNC_FAILURE } });
    });

    await page.setViewportSize(viewport);
    await page.goto(`/task/${id}`);
    await page.getByRole('button', { name: /Full reason from Argo CD/ }).click();
    const raw = page.locator('pre', { hasText: '"SeccompProfile":null' });
    await expect(raw).toBeVisible();

    const widths = await raw.evaluate(el => ({
      block: { scrollWidth: el.scrollWidth, clientWidth: el.clientWidth },
      page: {
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      },
    }));
    // The text must neither stretch the page nor hide behind a scrollbar of its own.
    expect(widths.page.scrollWidth).toBeLessThanOrEqual(widths.page.clientWidth);
    expect(widths.block.scrollWidth).toBeLessThanOrEqual(widths.block.clientWidth);
  });
}
