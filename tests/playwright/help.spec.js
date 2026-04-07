import { test, expect } from '@playwright/test';

test.describe('Help', () => {
    test('shows all 6 tabs', async ({ page }) => {
        await page.goto('/help');
        await expect(page.getByRole('button', { name: 'Getting Started' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Groups' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Connections' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Verification' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Safety & Privacy' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Meshtastic' })).toBeVisible();
    });

    test('Getting Started tab has Signal info', async ({ page }) => {
        await page.goto('/help');
        await expect(page.getByText('What is Signal?')).toBeVisible();
    });

    test('Verification tab explains what verification does NOT do', async ({ page }) => {
        await page.goto('/help');
        await page.getByRole('button', { name: 'Verification' }).click();
        await expect(page.getByRole('heading', { name: 'What does verification prove?' })).toBeVisible();
        await expect(page.getByText('does not verify:', { exact: false }).first()).toBeVisible();
    });

    test('tab switching works', async ({ page }) => {
        await page.goto('/help');
        await page.getByRole('button', { name: 'Meshtastic' }).click();
        await expect(page.getByText('mesh communication')).toBeVisible();
    });
});
