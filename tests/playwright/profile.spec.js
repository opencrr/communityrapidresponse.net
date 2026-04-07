import { test, expect } from '@playwright/test';
import { apiLogin } from './helpers.js';

test.describe('Profile', () => {
    test('shows account info', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/profile');
        const mainContent = page.locator('main');
        await expect(mainContent.getByText('alice@test.com')).toBeVisible();
    });

    test('verified user sees verified badge', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/profile');
        await expect(page.getByText('Address Verified')).toBeVisible();
    });

    test('unverified user sees verify prompt', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/profile');
        await expect(page.getByRole('link', { name: 'Verify Your Address' })).toBeVisible();
    });

    test('superuser sees only their verified regions', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/profile');
        await expect(page.getByText('Seattle')).toBeVisible();
        // Should NOT see regions she's not verified in
        await expect(page.getByText('Portland')).not.toBeVisible();
        await expect(page.getByText('Chicago')).not.toBeVisible();
    });

    test('shows delete account warning', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/profile');
        await expect(page.getByText('permanently remove all your data')).toBeVisible();
    });
});
