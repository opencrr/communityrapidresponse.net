import { test, expect } from '@playwright/test';
import { apiLogin } from './helpers.js';

test.describe('Connections', () => {
    test('admin sees topic board', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/connections');
        await expect(page.getByRole('heading', { name: 'Topic Board' })).toBeVisible();
    });

    test('topic board shows postings for admin', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/connections');
        // Alice is admin of Seattle MA — should see Portland MA's posting
        // Wait for postings to load
        await page.waitForTimeout(2000); // Allow time for async load
        await expect(page.getByText('Pacific Northwest')).toBeVisible();
    });

    test('non-admin does not see topic board', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/connections');
        await expect(page.getByRole('heading', { name: 'Topic Board' })).not.toBeVisible();
    });

    test('alice sees PNW Mutual Aid Network connection', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/connections');
        await expect(page.getByText('PNW Mutual Aid Network')).toBeVisible();
    });
});
