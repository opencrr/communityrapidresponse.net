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

    test('admin sees Create Signal Group button', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');
        await expect(page.getByRole('button', { name: '+ Create Signal Group' })).toBeVisible();
    });

    test('non-admin member does not see Create Signal Group button', async ({ page }) => {
        await apiLogin(page, 'dave@test.com');
        await page.goto('/groups/group-seattle-ma');
        await expect(page.getByRole('button', { name: '+ Create Signal Group' })).not.toBeVisible();
    });

    test('admin can open Create Signal Group modal', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');
        await page.getByRole('button', { name: '+ Create Signal Group' }).click();
        await expect(page.getByText('Create Signal Group')).toBeVisible();
        await expect(page.getByLabel('Chat Name')).toBeVisible();
        // invite link field hidden for open tier by default
        await expect(page.getByLabel('Signal Invite Link')).not.toBeVisible();
    });

    test('invite link field shown when restricted tier selected', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');
        await page.getByRole('button', { name: '+ Create Signal Group' }).click();
        await page.getByLabel('Admin Only').check();
        await expect(page.getByLabel('Signal Invite Link')).toBeVisible();
    });

    test('admin can create an open signal group', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups/group-seattle-ma');
        await page.getByRole('button', { name: '+ Create Signal Group' }).click();
        await page.getByLabel('Chat Name').fill('New Open Chat');
        // open tier is default; no invite link required
        await page.getByRole('button', { name: 'Create' }).click();
        await expect(page.getByText('Signal group created.')).toBeVisible();
        await expect(page.getByText('New Open Chat')).toBeVisible();
    });
});
