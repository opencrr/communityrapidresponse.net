import { test, expect } from '@playwright/test';
import { login, apiLogin } from './helpers.js';

test.describe('Navigation', () => {
    test('unauthenticated user sees login/register', async ({ page }) => {
        await page.goto('/');
        await expect(page.getByRole('link', { name: 'Login' })).toBeVisible();
        await expect(page.getByRole('link', { name: 'Register' })).toBeVisible();
    });

    test('authenticated user sees My Groups, Discover, Connections, Schools, Profile', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups');
        const nav = page.locator('nav');
        await expect(nav.getByRole('link', { name: 'My Groups' })).toBeVisible();
        await expect(nav.getByRole('link', { name: 'Discover' })).toBeVisible();
        await expect(nav.getByRole('link', { name: 'Connections' })).toBeVisible();
        await expect(nav.getByRole('link', { name: 'Schools' })).toBeVisible();
        await expect(nav.getByRole('link', { name: 'Profile' })).toBeVisible();
    });

    test('superuser sees Admin link', async ({ page }) => {
        await apiLogin(page, 'alice@test.com');
        await page.goto('/groups');
        await expect(page.locator('nav').getByRole('link', { name: 'Admin' })).toBeVisible();
    });

    test('non-superuser does not see Admin link', async ({ page }) => {
        await apiLogin(page, 'bob@test.com');
        await page.goto('/groups');
        await expect(page.locator('nav').getByRole('link', { name: 'Admin' })).not.toBeVisible();
    });

    test('login redirects to My Groups', async ({ page }) => {
        await login(page, 'alice@test.com');
        expect(page.url()).toContain('/groups');
    });
});
