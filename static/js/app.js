/**
 * Community Rapid Response - Main Application Entry Point
 * Handles routing, initialization, and page management.
 */

import { store, isAuthenticated, setUser, clearUser, hasReadAccess, isAdmin, isSuperuser } from './utils/store.js';
import { getCurrentUser } from './api/auth.js';
import { renderHeader } from './components/header.js';
import { renderFooter } from './components/footer.js';
import { hasLocalKey } from './crypto/index.js';
import { checkAndPerformRekeys } from './crypto/rekey.js';

// Page imports
import homePage from './pages/home.js';
import loginPage from './pages/login.js';
import registerPage from './pages/register.js';
import mfaSetupPage from './pages/mfaSetup.js';
import mfaVerifyPage from './pages/mfaVerify.js';
import regionsPage from './pages/regions.js';
import regionDetailPage from './pages/regionDetail.js';
import verificationPage from './pages/verification.js';
import vouchPage from './pages/vouch.js';
import groupsPage from './pages/groups.js';
import meshtasticPage from './pages/meshtastic.js';
import verifyEmailPage from './pages/verifyEmail.js';
import createRegionPage from './pages/admin/createRegion.js';
import manageGroupsPage from './pages/admin/manageGroups.js';
import manageMeshtasticPage from './pages/admin/manageMeshtastic.js';
import manageUsersPage from './pages/admin/manageUsers.js';
import auditLogsPage from './pages/admin/auditLogs.js';
import secretProposalsPage from './pages/admin/secretProposals.js';
import blocklistProposalsPage from './pages/admin/blocklistProposals.js';
import deletionProposalsPage from './pages/admin/deletionProposals.js';
import userReportsPage from './pages/admin/userReports.js';
import blockedAddressesPage from './pages/admin/blockedAddresses.js';
import membershipRequestsPage from './pages/admin/membershipRequests.js';
import helpPage from './pages/help.js';
import aboutPage from './pages/about.js';
import membershipPage from './pages/membership.js';
import profilePage from './pages/profile.js';
import forgotPasswordPage from './pages/forgotPassword.js';
import resetPasswordPage from './pages/resetPassword.js';
import schoolsPage from './pages/schools.js';
import schoolDetailPage from './pages/schoolDetail.js';
import districtDetailPage from './pages/districtDetail.js';
import privacyPage from './pages/privacy.js';
import termsPage from './pages/terms.js';
import groupDetailPage from './pages/groupDetail.js';
import groupCreatePage from './pages/groupCreate.js';
import groupManagePage from './pages/groupManage.js';
import myGroupsPage from './pages/myGroups.js';
import discoveryPage from './pages/discovery.js';
import connectionsListPage from './pages/connectionsList.js';
import connectionDetailPage from './pages/connectionDetail.js';
import groupInvitationsPage from './pages/groupInvitations.js';

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
    { path: '/dashboard', redirect: '/groups' },
    { path: '/communities', redirect: '/profile' },
    { path: '/communities/:id', page: regionDetailPage, auth: true, requiresSuperuser: true },
    { path: '/schools', page: schoolsPage, auth: true },
    { path: '/schools/:id', page: schoolDetailPage, auth: true },
    { path: '/school-districts/:id', page: districtDetailPage, auth: true },
    { path: '/verify', page: verificationPage, auth: true },
    { path: '/vouch', page: vouchPage, auth: true },
    { path: '/groups', page: myGroupsPage, auth: true },
    { path: '/groups/browse', redirect: '/discover' },
    { path: '/groups/create', page: groupCreatePage, auth: true },
    { path: '/groups/:id', page: groupDetailPage, auth: true },
    { path: '/groups/:id/manage', page: groupManagePage, auth: true },
    { path: '/discover', page: discoveryPage, auth: true },
    { path: '/connections', page: connectionsListPage, auth: true },
    { path: '/connections/:id', page: connectionDetailPage, auth: true },
    { path: '/invitations', page: groupInvitationsPage, auth: true },
    { path: '/meshtastic', redirect: '/groups' },
    { path: '/admin/communities', page: createRegionPage, auth: true, requiresAdmin: true },
    { path: '/admin/groups', page: manageGroupsPage, auth: true, requiresAdmin: true },
    { path: '/admin/meshtastic', page: manageMeshtasticPage, auth: true, requiresAdmin: true },
    { path: '/admin/proposals', page: secretProposalsPage, auth: true, requiresAdmin: true },
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

    // For returning authenticated users, check for pending re-keys in background
    if (isAuthenticated()) {
        hasLocalKey().then(hasKey => {
            if (hasKey) {
                checkAndPerformRekeys().catch(err => {
                    console.error('Background re-key check failed:', err);
                });
            }
        }).catch(() => {});
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

    // Handle redirects
    if (route.redirect) {
        navigate(route.redirect);
        return;
    }

    // Check authentication
    if (route.auth && !isAuthenticated()) {
        navigate('/login');
        return;
    }

    // Check guest-only routes
    if (route.guestOnly && isAuthenticated()) {
        navigate('/groups');
        return;
    }

    // Check read access requirement (needs at least one verification)
    if (route.requiresReadAccess && !hasReadAccess()) {
        navigate('/verify');
        return;
    }

    // Check admin requirement (needs BOTH postcard AND vouch verification)
    if (route.requiresAdmin && !isAdmin()) {
        navigate('/groups');
        return;
    }

    // Check superuser requirement (database-created accounts only)
    if (route.requiresSuperuser && !isSuperuser()) {
        navigate('/groups');
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
