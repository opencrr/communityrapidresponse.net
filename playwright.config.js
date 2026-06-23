import { defineConfig } from '@playwright/test';

export default defineConfig({
    testDir: './tests/playwright',
    timeout: 30000,
    retries: 0,
    use: {
        baseURL: 'http://localhost:3000',
        headless: true,
        screenshot: 'only-on-failure',
        trace: 'on-first-retry',
    },
    projects: [
        { name: 'chromium', use: { browserName: 'chromium' } },
    ],
});
