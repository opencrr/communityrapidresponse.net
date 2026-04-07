import { test, expect } from '@playwright/test';
import { apiLogin } from './helpers.js';

test.describe('Discover', () => {
    test('unverified user sees discoverable groups', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/discover');
        await expect(page.getByText('Seattle Mutual Aid')).toBeVisible();
        await expect(page.getByText('Portland Mutual Aid')).toBeVisible();
        await expect(page.getByText('Open Community Hub')).toBeVisible();
    });

    test('unverified user does not see non-discoverable groups', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/discover');
        // Wait for groups to load
        await expect(page.getByText('Open Community Hub')).toBeVisible();
        // These should NOT be visible
        await expect(page.getByText('Seattle Tenants Union')).not.toBeVisible();
        await expect(page.getByText('Chicago Disaster Prep')).not.toBeVisible();
        await expect(page.getByText('The Secret Society')).not.toBeVisible();
    });

    test('disclaimer is visible', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/discover');
        await expect(page.getByText('This platform verifies member residency')).toBeVisible();
        await expect(page.getByText('Group names, descriptions, and claims')).toBeVisible();
    });
});
