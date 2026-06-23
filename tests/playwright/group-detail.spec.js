import { test, expect } from '@playwright/test';
import { apiLogin } from './helpers.js';

test.describe('Group Detail', () => {
    test('member sees signal groups, resources, and members', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');

        // Group info
        await expect(page.getByRole('heading', { name: 'Seattle Mutual Aid' })).toBeVisible();

        // Signal groups section
        await expect(page.getByText('Signal Groups')).toBeVisible();
        await expect(page.getByText('General Chat')).toBeVisible();
        await expect(page.getByText('Operations', { exact: true })).toBeVisible();
        await expect(page.getByText('Leadership')).toBeVisible(); // admin sees admin_only tier

        // Resources section
        await expect(page.getByText('Resources')).toBeVisible();
        await expect(page.getByText('Mutual Aid Handbook')).toBeVisible();

        // Members section
        await expect(page.getByRole('heading', { name: /Members/ })).toBeVisible();
    });

    test('admin sees Manage Group link', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');
        await expect(page.getByText('Manage Group')).toBeVisible();
    });

    test('non-admin member does not see Manage Group', async ({ page }) => {
        await apiLogin(page, 'dave@test.com');
        await page.goto('/groups/group-seattle-ma');
        // Dave is a member but not admin
        await expect(page.getByRole('heading', { name: 'Seattle Mutual Aid' })).toBeVisible();
        await expect(page.getByText('Manage Group')).not.toBeVisible();
    });

    test('non-member cannot see unlisted group', async ({ page }) => {
        await apiLogin(page, 'judy@test.com');
        await page.goto('/groups/group-secret-society');
        await expect(page.getByText('Group Not Found')).toBeVisible();
    });

    test('member sees meshtastic channels', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');
        await expect(page.getByText('Meshtastic Channels')).toBeVisible();
        await expect(page.getByText('Emergency Mesh Network')).toBeVisible();
    });

    test('unverified member sees open and member tier content', async ({ page }) => {
        await apiLogin(page, 'liam@test.com');
        await page.goto('/groups/group-open-hub');
        await expect(page.getByText('Public Chat')).toBeVisible();
        await expect(page.getByText('Members Only')).toBeVisible();
        // Should NOT see admin_only
        await expect(page.getByText('Admin Channel')).not.toBeVisible();
    });
});
