import { expect, test } from '@playwright/test';

const dashboardSearch = 'input[placeholder="Search services"]';
const commandSearch = 'input[placeholder="Search services and pages"]';

test('dashboard filters update counts and active chips', async ({ page }) => {
  await page.goto('/services');

  await expect(page.getByText('3 services across 2 tags.')).toBeVisible();
  await page.locator(dashboardSearch).fill('grafana');

  await expect(page.getByText('1 of 3 services')).toBeVisible();
  await expect(page.locator(dashboardSearch)).toHaveValue('grafana');
  await expect(page.getByRole('heading', { name: 'Grafana' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Prometheus' })).toBeHidden();

  await page.getByRole('button', { name: /Clear all/ }).click();
  await expect(page.locator(dashboardSearch)).toHaveValue('');
  await expect(page.getByRole('heading', { name: 'Prometheus' })).toBeVisible();
});

test('empty state appears when filters match nothing', async ({ page }) => {
  await page.goto('/services');

  await page.locator(dashboardSearch).fill('does-not-exist');

  await expect(page.getByText('0 of 3 services')).toBeVisible();
  await expect(page.getByText('No matching services')).toBeVisible();
});

test('keyboard shortcuts focus search and open command palette', async ({ page, browserName }) => {
  await page.goto('/services');

  const modifier = process.platform === 'darwin' && browserName === 'chromium' ? 'Meta' : 'Control';
  await page.keyboard.press(`${modifier}+/`);
  await expect(page.locator(dashboardSearch)).toBeFocused();

  await page.keyboard.press(`${modifier}+K`);
  await expect(page.locator(commandSearch)).toBeFocused();
  await page.locator(commandSearch).fill('wiki');
  await expect(page.getByRole('link', { name: /Wiki/ })).toBeVisible();
});

test('source and tag chips apply filters', async ({ page }) => {
  await page.goto('/services');

  // Tag filter via the inline tag list (the search-picker dropdown only
  // appears when there are too many tags to show inline).
  const docsTag = page.locator('fieldset').getByRole('button', { name: 'docs', exact: true });
  await docsTag.click();
  await expect(docsTag).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByText('1 of 3 services')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Wiki' })).toBeVisible();

  // Toggle the tag off, then click the source chip on a service card.
  await docsTag.click();
  await expect(docsTag).toHaveAttribute('aria-pressed', 'false');
  await page.getByRole('button', { name: /Filter by source Static · Manual/ }).first().click();
  // Sources legend now shows "1 selected"; all three services are from
  // that source so the visible count is 3 of 3.
  await expect(page.locator('legend').filter({ hasText: 'Sources' }).getByText('1 selected')).toBeVisible();
  await expect(page.getByText('3 of 3 services')).toBeVisible();
});
