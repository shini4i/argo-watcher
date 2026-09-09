import { expect, test } from '@playwright/test';
import { seedTask, waitForDeployed } from '../helpers';

/**
 * @description An unbreakable CI bot address sets the Author cell's min-content
 * width under `table-layout: auto` unless the text box is capped. jsdom runs no
 * layout, so the unit tests can only assert declared CSS — the rendered width is
 * measurable here and nowhere else.
 */
const BOT_AUTHOR = 'project_1758_bot_062d75c8b91e27fa4e5bb374cd9c1c39@noreply.example.net';

/**
 * @description Mirrors `AUTHOR_MAX_WIDTH` in TasksDatagrid.tsx, restated because
 * this suite compiles without JSX support and cannot import a component module.
 * `CELL_PADDING_ALLOWANCE` covers the surrounding table cell's own padding.
 */
const AUTHOR_MAX_WIDTH = 200;
const CELL_PADDING_ALLOWANCE = 48;

test('a long bot author is clipped instead of widening its column', async ({ page, request }) => {
  const id = await seedTask(request, BOT_AUTHOR);
  await waitForDeployed(request, id);

  // Wider than the table's natural width, so only the cap constrains the column:
  // a viewport narrow enough to scroll would mask an uncapped cell.
  await page.setViewportSize({ width: 1920, height: 800 });
  await page.goto('/');

  const author = page.getByTitle(BOT_AUTHOR);
  await expect(author).toBeVisible();

  const box = (await author.boundingBox())!;
  expect(box.width).toBeLessThanOrEqual(AUTHOR_MAX_WIDTH);

  // Clipped, not merely narrow: the text overflows the box it is painted in.
  const overflows = await author.evaluate(el => el.scrollWidth > el.clientWidth);
  expect(overflows, 'the address must be ellipsised, not laid out in full').toBe(true);

  const cellWidth = await author.evaluate(el => el.closest('td')!.getBoundingClientRect().width);
  expect(cellWidth).toBeLessThanOrEqual(AUTHOR_MAX_WIDTH + CELL_PADDING_ALLOWANCE);
});
