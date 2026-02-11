/**
 * Community Rapid Response - Main Application Entry Point
 * Handles routing, initialization, and page management.
 */

import { store, isAuthenticated, setUser, clearUser, hasReadAccess, isAdmin, isSuperuser } from './utils/store.js?v=c49aed9';
import { getCurrentUser } from './api/auth.js?v=c49aed9';
import { renderHeader } from './components/header.js?v=c49aed9';
import { renderFooter } from './components/footer.js?v=c49aed9';

// Page imports
import homePage from './pages/home.js?v=c49aed9';
import loginPage from './pages/login.js?v=c49aed9';
import registerPage from './pages/register.js?v=c49aed9';
import mfaSetupPage from './pages/mfaSetup.js?v=c49aed9';
import mfaVerifyPage from './pages/mfaVerify.js?v=c49aed9';
import dashboardPage from './pages/dashboard.js?v=c49aed9';
import regionsPage from './pages/regions.js?v=c49aed9';
import regionDetailPage from './pages/regionDetail.js?v=c49aed9';
import verificationPage from './pages/verification.js?v=c49aed9';
import vouchPage from './pages/vouch.js?v=c49aed9';
import groupsPage from './pages/groups.js?v=c49aed9';
import verifyEmailPage from './pages/verifyEmail.js?v=c49aed9';
import createRegionPage from './pages/admin/createRegion.js?v=c49aed9';
import manageGroupsPage from './pages/admin/manageGroups.js?v=c49aed9';
import manageUsersPage from './pages/admin/manageUsers.js?v=c49aed9';
import auditLogsPage from './pages/admin/auditLogs.js?v=c49aed9';
import inviteLinkProposalsPage from './pages/admin/inviteLinkProposals.js?v=c49aed9';
import blocklistProposalsPage from './pages/admin/blocklistProposals.js?v=c49aed9';
import deletionProposalsPage from './pages/admin/deletionProposals.js?v=c49aed9';
import userReportsPage from './pages/admin/userReports.js?v=c49aed9';
import blockedAddressesPage from './pages/admin/blockedAddresses.js?v=c49aed9';
import membershipRequestsPage from './pages/admin/membershipRequests.js?v=c49aed9';
import helpPage from './pages/help.js?v=c49aed9';
import aboutPage from './pages/about.js?v=c49aed9';
import membershipPage from './pages/membership.js?v=c49aed9';
import profilePage from './pages/profile.js?v=c49aed9';
import forgotPasswordPage from './pages/forgotPassword.js?v=c49aed9';
import resetPasswordPage from './pages/resetPassword.js?v=c49aed9';
import schoolsPage from './pages/schools.js?v=c49aed9';
import schoolDetailPage from './pages/schoolDetail.js?v=c49aed9';
import districtDetailPage from './pages/districtDetail.js?v=c49aed9';
import privacyPage from './pages/privacy.js?v=c49aed9';
import termsPage from './pages/terms.js?v=c49aed9';

// Route definitions
// - auth: requires login
// - guestOnly: only accessible when not logged in
// - requiresReadAccess: requires at least one verification (postcard OR vouch)
// - requiresAdmin: requires BOTH postcard AND vouch verification
// - requiresSuperuser: requires superuser status (database-created accounts only)
const routes = [
    { path: '/', page: homePage, auth: false },
    { path: '/help', page: helpPage, auth: false },
    { path: '/about', page: aboutPage, auth: false },
    { path: '/privacy', page: privacyPage, auth: false },
    { path: '/terms', page: termsPage, auth: false },
    { path: '/login', page: loginPage, auth: false, guestOnly: true },
    { path: '/register', page: registerPage, auth: false, guestOnly: true },
    { path: '/forgot-password', page: forgotPasswordPage, auth: false, guestOnly: true },
    { path: '/reset-password', page: resetPasswordPage, auth: false },
    { path: '/profile', page: profilePage, auth: true },
    { path: '/verify-email', page: verifyEmailPage, auth: false },
    { path: '/mfa/setup', page: mfaSetupPage, auth: false, mfaFlow: true },
    { path: '/mfa/verify', page: mfaVerifyPage, auth: false, mfaFlow: true },
    { path: '/dashboard', page: dashboardPage, auth: true },
    { path: '/communities', page: regionsPage, auth: false },
    { path: '/communities/:id', page: regionDetailPage, auth: false },
    { path: '/schools', page: schoolsPage, auth: false },
    { path: '/schools/:id', page: schoolDetailPage, auth: false },
    { path: '/school-districts/:id', page: districtDetailPage, auth: false },
    { path: '/verify', page: verificationPage, auth: true },
    { path: '/vouch', page: vouchPage, auth: true },
    { path: '/groups', page: groupsPage, auth: true, requiresReadAccess: true },
    { path: '/admin/communities', page: createRegionPage, auth: true, requiresAdmin: true },
    { path: '/admin/groups', page: manageGroupsPage, auth: true, requiresAdmin: true },
    { path: '/admin/proposals', page: inviteLinkProposalsPage, auth: true, requiresAdmin: true },
    { path: '/admin/blocklist-proposals', page: blocklistProposalsPage, auth: true, requiresAdmin: true },
    { path: '/admin/deletion-proposals', page: deletionProposalsPage, auth: true, requiresAdmin: true },
    { path: '/admin/reports', page: userReportsPage, auth: true, requiresAdmin: true },
    { path: '/admin/membership-requests', page: membershipRequestsPage, auth: true, requiresAdmin: true },
    { path: '/admin/users', page: manageUsersPage, auth: true, requiresSuperuser: true },
    { path: '/admin/blocked-addresses', page: blockedAddressesPage, auth: true, requiresSuperuser: true },
    { path: '/admin/audit-logs', page: auditLogsPage, auth: true, requiresSuperuser: true },
    { path: '/membership', page: membershipPage, auth: true },
];

let currentPage = null;
let currentRoute = null;

/**
 * Initialize the application
 */
async function init() {
    // Check authentication status
    try {
        await getCurrentUser();
    } catch (error) {
        clearUser();
    }

    // Render initial header and footer
    renderHeader();
    renderFooter();

    // Set up routing
    setupRouting();

    // Navigate to current URL
    await handleRouteChange();
}

/**
 * Set up routing event listeners
 */
function setupRouting() {
    // Handle browser back/forward
    window.addEventListener('popstate', handleRouteChange);

    // Handle link clicks for SPA navigation
    document.addEventListener('click', (event) => {
        // Find closest anchor tag
        const link = event.target.closest('a');
        if (!link) return;

        // Check if it's an internal link
        const href = link.getAttribute('href');
        if (!href || href.startsWith('http') || href.startsWith('//') || href.startsWith('#')) {
            return;
        }

        // Check for data-link attribute or internal path
        if (link.hasAttribute('data-link') || href.startsWith('/')) {
            event.preventDefault();
            navigate(href);
        }
    });
}

/**
 * Navigate to a new path
 * @param {string} path - Path to navigate to
 * @param {Object} [state] - Optional state to pass
 */
export function navigate(path, state = {}) {
    window.history.pushState(state, '', path);
    handleRouteChange();
}

/**
 * Handle route changes
 */
async function handleRouteChange() {
    const path = window.location.pathname;

    // Find matching route
    const match = matchRoute(path);

    if (!match) {
        // 404 - Show not found page
        renderNotFound();
        return;
    }

    const { route, params } = match;

    // Check authentication
    if (route.auth && !isAuthenticated()) {
        navigate('/login');
        return;
    }

    // Check guest-only routes
    if (route.guestOnly && isAuthenticated()) {
        navigate('/dashboard');
        return;
    }

    // Check read access requirement (needs at least one verification)
    if (route.requiresReadAccess && !hasReadAccess()) {
        navigate('/verify');
        return;
    }

    // Check admin requirement (needs BOTH postcard AND vouch verification)
    if (route.requiresAdmin && !isAdmin()) {
        // Redirect to dashboard with message about needing both verifications
        navigate('/dashboard');
        return;
    }

    // Check superuser requirement (database-created accounts only)
    if (route.requiresSuperuser && !isSuperuser()) {
        navigate('/dashboard');
        return;
    }

    // Cleanup previous page
    if (currentPage && currentPage.cleanup) {
        currentPage.cleanup();
    }

    currentRoute = route;
    currentPage = route.page;

    // Render the new page
    const main = document.getElementById('main');
    if (main) {
        // Show loading state
        main.innerHTML = `
            <div class="page page--centered">
                <div class="loading">
                    <div class="spinner spinner--lg"></div>
                </div>
            </div>
        `;

        try {
            await currentPage.render(main, params);
        } catch (error) {
            console.error('Page render error:', error);
            main.innerHTML = `
                <div class="page page--centered">
                    <div class="empty-state">
                        <div class="empty-state__icon">&#x26A0;</div>
                        <h3 class="empty-state__title">Something went wrong</h3>
                        <p class="empty-state__description">
                            An error occurred while loading this page.
                        </p>
                        <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
                    </div>
                </div>
            `;
        }
    }

    // Update header active states
    renderHeader();

    // Scroll to top
    window.scrollTo(0, 0);
}

/**
 * Match a path to a route
 * @param {string} path - URL path to match
 * @returns {Object|null} Matched route and params, or null
 */
function matchRoute(path) {
    // Remove trailing slash (except for root)
    if (path !== '/' && path.endsWith('/')) {
        path = path.slice(0, -1);
    }

    for (const route of routes) {
        const params = matchPath(route.path, path);
        if (params !== null) {
            return { route, params };
        }
    }

    return null;
}

/**
 * Match a path pattern to a URL path
 * @param {string} pattern - Route pattern (e.g., /regions/:id)
 * @param {string} path - URL path to match
 * @returns {Object|null} Params object or null if no match
 */
function matchPath(pattern, path) {
    const patternParts = pattern.split('/');
    const pathParts = path.split('/');

    if (patternParts.length !== pathParts.length) {
        return null;
    }

    const params = {};

    for (let i = 0; i < patternParts.length; i++) {
        const patternPart = patternParts[i];
        const pathPart = pathParts[i];

        if (patternPart.startsWith(':')) {
            // Parameter
            const paramName = patternPart.slice(1);
            params[paramName] = decodeURIComponent(pathPart);
        } else if (patternPart !== pathPart) {
            // No match
            return null;
        }
    }

    return params;
}

/**
 * Render 404 not found page
 */
function renderNotFound() {
    const main = document.getElementById('main');
    if (main) {
        main.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F50D;</div>
                    <h3 class="empty-state__title">Page Not Found</h3>
                    <p class="empty-state__description">
                        The page you're looking for doesn't exist.
                    </p>
                    <a href="/" class="btn btn--primary" data-link>Go Home</a>
                </div>
            </div>
        `;
    }
}

// Initialize app when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
} else {
    init();
}

// Export for use by other modules
export { navigate as default };
