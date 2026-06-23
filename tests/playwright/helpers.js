/**
 * Login as a fixture user and return the page with auth cookie set.
 */
export async function login(page, email, password = 'password') {
    // Navigate to login page
    await page.goto('/login');

    // Fill in credentials
    await page.fill('input[name="email"], input[type="email"], #email', email);
    await page.fill('input[name="password"], input[type="password"], #password', password);

    // Submit
    await page.click('button[type="submit"]');

    // Wait for redirect to My Groups (login landing)
    await page.waitForURL('**/groups', { timeout: 10000 });
}

/**
 * Login via API and set cookie (faster than UI login for setup)
 */
export async function apiLogin(page, email, password = 'password') {
    const response = await page.request.post('/api/v1/auth/login', {
        data: { email, password },
    });
    const body = await response.json();

    // Set the auth cookie
    await page.context().addCookies([{
        name: 'token',
        value: body.token,
        domain: 'localhost',
        path: '/',
    }]);

    return body;
}
