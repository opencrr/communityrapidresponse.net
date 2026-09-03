import { test, expect } from '@playwright/test';

// Allowlist for known third-party console errors that are not critical to the app.
// Entries must be exact substrings observed in genuine third-party noise.
// If no benign third-party messages are observed, this array remains empty.
const CONSOLE_ERROR_ALLOWLIST = [];

test.describe('Home page', () => {
    test('landing page loads without console errors', async ({ page }) => {
        const consoleErrors = [];
        const pageErrors = [];

        // Capture console errors
        page.on('console', msg => {
            if (msg.type() === 'error') {
                consoleErrors.push(msg.text());
            }
        });

        // Capture page errors (uncaught exceptions)
        page.on('pageerror', err => {
            pageErrors.push(err.toString());
        });

        // Navigate to home page
        await page.goto('/');

        // Wait for network idle to ensure all resources are loaded
        await page.waitForLoadState('networkidle');

        // Additional settle time to ensure map initialization completes
        await page.waitForTimeout(500);

        // Assert no uncaught exceptions
        expect(pageErrors).toHaveLength(0);

        // Filter out allowlisted console errors
        const relevantErrors = consoleErrors.filter(error => {
            return !CONSOLE_ERROR_ALLOWLIST.some(allowlisted => error.includes(allowlisted));
        });

        // Assert no relevant console errors
        expect(relevantErrors).toHaveLength(0);
    });
});
