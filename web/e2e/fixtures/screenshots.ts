import type { Page, TestInfo } from '@playwright/test';

export const captureScreenshot = async (page: Page, testInfo: TestInfo, name: string) => {
  const screenshot = await page.screenshot({ fullPage: true });
  await testInfo.attach(name, {
    body: screenshot,
    contentType: 'image/png',
  });
};
