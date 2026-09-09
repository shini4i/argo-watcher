import { expect, test } from '@playwright/test';
import { seedTask, waitForDeployed } from '../helpers';

/**
 * @description The task table's header rule. jsdom paints nothing and runs no
 * layout, so the unit tests can only assert declared CSS — that the rule
 * actually renders, and renders once, is measurable here and nowhere else.
 */

/** Wide enough that the header sticks and the rule spans several columns. */
const WIDE_VIEWPORT = { width: 2065, height: 900 };

test('the header rule survives the header sticking to the top', async ({ page, request }) => {
  const id = await seedTask(request, 'layout-header-rule');
  await waitForDeployed(request, id);

  await page.setViewportSize(WIDE_VIEWPORT);
  await page.goto('/');
  await expect(page.getByRole('columnheader', { name: 'Application' })).toBeVisible();

  const header = await page.evaluate(() =>
    [...document.querySelectorAll('.RaDatagrid-headerCell')].map(el => {
      const style = getComputedStyle(el);
      return { shadow: style.boxShadow, border: style.borderBottomWidth, position: style.position };
    }),
  );

  expect(header.length).toBeGreaterThan(1);
  for (const cell of header) {
    // A collapsed border is painted by the table, which a sticky cell cannot
    // carry with it, leaving the rule drawn across one column only.
    expect(cell.position).toBe('sticky');
    expect(cell.border).toBe('0px');
    expect(cell.shadow).not.toBe('none');
  }

  // A shadow does not collapse with the first row's top border the way the old
  // border did, so leaving both in place stacks them into a 2px rule.
  const firstRowBorder = await page.evaluate(
    () => getComputedStyle(document.querySelector('tbody .RaDatagrid-row')!).borderTopWidth,
  );
  expect(firstRowBorder).toBe('0px');
});

test('no row navigates on click — only the View button does', async ({ page, request }) => {
  const id = await seedTask(request, 'layout-row-inert');
  await waitForDeployed(request, id);

  await page.goto('/');
  await expect(page.getByRole('columnheader', { name: 'Application' })).toBeVisible();
  const listPath = new URL(page.url()).pathname;

  // react-admin puts RaDatagrid-row on the header row too, so a rule meant for
  // task rows would style the header as one; neither may act as a link.
  await page.locator('.RaDatagrid-headerRow').click();
  await page.locator('tbody .RaDatagrid-row td.cell-author').first().click();
  // react-admin keeps its list state in the query string, so compare the path.
  await expect
    .poll(() => new URL(page.url()).pathname, { message: 'a row click must not navigate' })
    .toBe(listPath);

  // Scoped and exact: an unscoped substring match also hits the top bar's Overview link.
  await page
    .locator('tbody .RaDatagrid-row td.cell-view')
    .first()
    .getByRole('link', { name: 'View', exact: true })
    .click();
  await expect
    .poll(() => new URL(page.url()).pathname)
    .toMatch(/^\/task\/[0-9a-f-]{36}$/);
});
