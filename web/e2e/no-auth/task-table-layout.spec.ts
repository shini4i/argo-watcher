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

test('the header row is not presented as a clickable task row', async ({ page, request }) => {
  const id = await seedTask(request, 'layout-header-cursor');
  await waitForDeployed(request, id);

  await page.goto('/');
  const headerRow = page.locator('.RaDatagrid-headerRow');
  await expect(headerRow).toBeVisible();

  // react-admin puts RaDatagrid-row on the header row too, so a rule meant for
  // task rows gives the header a pointer cursor and the row hover highlight.
  await expect(headerRow).not.toHaveCSS('cursor', 'pointer');
});
