import { test, expect } from '@playwright/test';

// The dev/e2e stack runs with EMAIL_ENABLED=false by default (docker-compose.yml,
// .env.example), so every fixture user and every freshly-registered e2e user is
// created with email_verified=true (internal/handlers/auth.go Register / seed-fixtures
// main.go). There is no way to seed an email_unverified user without changing shared
// stack defaults that other specs rely on, so these specs drive the flow by mocking
// the specific API responses (login's email_action branch, and the 403
// email_verification_required error body) rather than seeding real backend state.

test.describe('Email verification pending flow', () => {
    test('unverified login shows the pending page with message, email, and a working resend button', async ({ page }) => {
        await page.route('**/api/v1/auth/login', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    token: 'fake-email-unverified-token',
                    email_action: 'verify_email',
                    message: 'Please verify your email address to continue',
                    user: {
                        id: 'user-unverified',
                        username: 'pat',
                        email: 'pat@test.com',
                        email_verified: false,
                    },
                }),
            });
        });

        let resendCalls = 0;
        await page.route('**/api/v1/auth/resend-verification', async (route) => {
            resendCalls += 1;
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({ message: 'Verification email sent' }),
            });
        });

        await page.goto('/login');
        await page.fill('#email', 'pat@test.com');
        await page.fill('#password', 'password');
        await page.click('button[type="submit"]');

        await page.waitForURL('**/verify-email');
        await expect(page.getByText('Please verify your email address to continue')).toBeVisible();
        await expect(page.getByText('pat@test.com')).toBeVisible();

        const resendBtn = page.locator('#resend-btn');
        await expect(resendBtn).toBeVisible();
        await resendBtn.click();

        await expect(page.getByText('Verification email sent! Check your inbox.')).toBeVisible();
        expect(resendCalls).toBe(1);
    });

    test('resend shows rate-limit feedback when the API returns 429', async ({ page }) => {
        await page.route('**/api/v1/auth/login', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    token: 'fake-email-unverified-token',
                    email_action: 'verify_email',
                    message: 'Please verify your email address to continue',
                    user: { id: 'user-unverified', username: 'pat', email: 'pat@test.com', email_verified: false },
                }),
            });
        });

        await page.route('**/api/v1/auth/resend-verification', async (route) => {
            await route.fulfill({
                status: 429,
                contentType: 'application/json',
                body: JSON.stringify({ error: 'Too many requests. Please wait before trying again.' }),
            });
        });

        await page.goto('/login');
        await page.fill('#email', 'pat@test.com');
        await page.fill('#password', 'password');
        await page.click('button[type="submit"]');

        await page.waitForURL('**/verify-email');
        await page.locator('#resend-btn').click();

        await expect(page.getByText('Too many requests. Please wait before trying again.')).toBeVisible();
    });

    test('direct navigation to /verify-email with a stale unverified session renders once without looping', async ({ page }) => {
        // Simulates hitting F5 (or a bookmarked link) on the pending page while the
        // httpOnly cookie is still the 10-minute email_unverified token: app.js init()
        // calls getCurrentUser() -> GET /users/me, which is full-token-only and returns
        // this 403 for an email_unverified session.
        let usersMeCalls = 0;
        await page.route('**/api/v1/users/me', async (route) => {
            usersMeCalls += 1;
            await route.fulfill({
                status: 403,
                contentType: 'application/json',
                body: JSON.stringify({ error: 'email_verification_required', token_type: 'email_unverified' }),
            });
        });

        await page.goto('/verify-email');

        await expect(page.getByRole('heading', { name: 'Verify Your Email' })).toBeVisible();
        await expect(page.locator('#resend-btn')).toBeVisible();

        // Give a would-be redirect loop a chance to manifest before asserting it didn't.
        await page.waitForTimeout(500);
        expect(new URL(page.url()).pathname).toBe('/verify-email');
        expect(usersMeCalls).toBe(1);
    });

    test('a 403 email_verification_required from any other API call redirects a stale session to the pending page', async ({ page }) => {
        // /users/me succeeds (simulating in-memory SPA state that still thinks it's
        // fully authenticated), but a different protected endpoint the profile page
        // calls returns the email_verification_required 403 - this is the path the
        // generic client.js interceptor (as opposed to the getCurrentUser()-specific
        // handling) is responsible for.
        await page.route('**/api/v1/users/me', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    user: { id: 'user-stale', username: 'pat', email: 'pat@test.com', email_verified: true, verification_tier: 0 },
                }),
            });
        });
        await page.route('**/api/v1/verification/status', async (route) => {
            await route.fulfill({
                status: 403,
                contentType: 'application/json',
                body: JSON.stringify({ error: 'email_verification_required', token_type: 'email_unverified' }),
            });
        });

        await page.goto('/profile');

        await page.waitForURL('**/verify-email', { timeout: 10000 });
        await expect(page.getByRole('heading', { name: 'Verify Your Email' })).toBeVisible();
    });
});
