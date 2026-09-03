import { test, expect } from '@playwright/test';

// On every page load the app calls GET /api/v1/users/me (static/js/app.js -> getCurrentUser())
// to determine auth state. For an unauthenticated visitor this legitimately returns 401, and
// static/js/api/auth.js handles it gracefully - but Chromium still reports the failed resource
// load itself as a console message of type "error". This is the one documented, expected
// exemption. It is scoped to both the exact message text AND the exact request URL, so it can
// only ever suppress this one known-benign 401 and cannot mask unrelated regressions (including
// ones that happen to also read "401 (Unauthorized)" against a different endpoint).
const CONSOLE_ERROR_ALLOWLIST = [
    {
        text: 'Failed to load resource: the server responded with a status of 401 (Unauthorized)',
        urlSuffix: '/api/v1/users/me',
    },
];

test.describe('Home page', () => {
    test('landing page loads without console errors', async ({ page }) => {
        const consoleErrors = [];
        const pageErrors = [];

        page.on('console', msg => {
            if (msg.type() === 'error') {
                consoleErrors.push({ text: msg.text(), url: msg.location().url });
            }
        });

        page.on('pageerror', err => {
            pageErrors.push(err.toString());
        });

        await page.goto('/');

        // Wait for the home map to actually finish loading. static/js/pages/home.js
        // appends a `.map-legend` element only after its onLoad handler resolves, so
        // this is a direct signal of map-load completion - unlike networkidle, which
        // can settle several seconds before the map's 'load' event (and any error it
        // triggers, e.g. H01's ReferenceError) actually fires. If the map never loads
        // (a regression), this simply times out and we fall through to the assertions
        // below with whatever errors were captured by then.
        await page.locator('.map-legend').waitFor({ state: 'attached', timeout: 15000 }).catch(() => {});
        await page.waitForTimeout(500);

        // Uncaught exceptions (e.g. the H01 ReferenceError) always fail the test.
        expect(pageErrors).toHaveLength(0);

        const relevantErrors = consoleErrors.filter(({ text, url }) => {
            return !CONSOLE_ERROR_ALLOWLIST.some(
                allowed => text === allowed.text && url.endsWith(allowed.urlSuffix)
            );
        });

        expect(relevantErrors.map(e => e.text)).toEqual([]);
    });
});
