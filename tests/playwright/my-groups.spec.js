import { test, expect } from '@playwright/test';
import { apiLogin } from './helpers.js';

test.describe('My Groups', () => {
    test('alice sees her groups', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups');
        await expect(page.locator('.group-card')).toHaveCount(2); // Seattle MA + Provisional
        await expect(page.getByText('Seattle Mutual Aid')).toBeVisible();
        await expect(page.getByText('Provisional Group')).toBeVisible();
    });

    test('alice sees Create Group button (address verified)', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups');
        await expect(page.getByText('Create Group')).toBeVisible();
    });

    test('liam does not see Create Group button (unverified)', async ({ page }) => {
        await apiLogin(page, 'liam@test.com');
        await page.goto('/groups');
        await expect(page.getByText('Create Group')).not.toBeVisible();
    });

    test('liam sees groups he belongs to', async ({ page }) => {
        await apiLogin(page, 'liam@test.com');
        await page.goto('/groups');
        await expect(page.getByText('Open Community Hub')).toBeVisible();
        await expect(page.getByText('Seattle Mutual Aid')).toBeVisible();
    });

    test('judy sees no groups', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/groups');
        await expect(page.getByText("You're not a member of any groups yet")).toBeVisible();
    });

    test('alice sees her connections', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups');
        await expect(page.getByText('PNW Mutual Aid Network')).toBeVisible();
    });
});
