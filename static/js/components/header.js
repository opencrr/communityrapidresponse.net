/**
 * Header/navigation component
 * Renders the site header with navigation links and user status.
 */

import { store, isAuthenticated, getUser, isAdmin, hasReadAccess, getVerificationStatus, isSuperuser } from '../utils/store.js';
import { logout } from '../api/auth.js';
import { navigate } from '../app.js';
import toast from './toast.js';

/**
 * Render the header component
 */
export function renderHeader() {
    const headerElement = document.getElementById('header');
    if (!headerElement) return;

    const authenticated = isAuthenticated();
    const user = getUser();
    const verificationStatus = getVerificationStatus();
    const userIsAdmin = isAdmin();
    const userHasReadAccess = hasReadAccess();
    const userIsSuperuser = isSuperuser();

    headerElement.innerHTML = `
        <nav class="header">
            <div class="header__container">
                <a href="/" class="header__logo" data-link>
                    Community Rapid Response
                </a>

                <div class="header__nav" id="main-nav">
                    <a href="/" class="header__nav-link" data-link>Home</a>
                    <a href="/communities" class="header__nav-link" data-link>Communities</a>
                    <a href="/schools" class="header__nav-link" data-link>Schools</a>
                    <a href="/help" class="header__nav-link" data-link>Help</a>
                    <a href="/about" class="header__nav-link" data-link>About</a>
                    ${authenticated ? `
                        <a href="/dashboard" class="header__nav-link" data-link>Dashboard</a>
                        ${userHasReadAccess ? `<a href="/groups" class="header__nav-link" data-link>Groups</a>` : ''}
                        ${userIsSuperuser ? `<a href="/admin/users" class="header__nav-link" data-link>Users</a>` : ''}
                    ` : ''}
                    <div class="header__mobile-actions">
                        ${authenticated ? `
                            <div class="header__user">
                                <span class="header__tier-badge header__tier-badge--${verificationStatus.level}">
                                    ${verificationStatus.label}
                                </span>
                                <a href="/profile" class="header__username" data-link style="text-decoration: none; color: inherit;">${escapeHtml(user?.name || user?.email || '')}</a>
                            </div>
                            <button class="btn btn--ghost" id="logout-btn-mobile">Log out</button>
                        ` : `
                            <a href="/login" class="btn btn--ghost" data-link>Log in</a>
                            <a href="/register" class="btn btn--primary" data-link>Sign up</a>
                        `}
                    </div>
                </div>

                <div class="header__actions">
                    ${authenticated ? `
                        <div class="header__user">
                            <span class="header__tier-badge header__tier-badge--${verificationStatus.level}">
                                ${verificationStatus.label}
                            </span>
                            <a href="/profile" class="header__username" data-link style="text-decoration: none; color: inherit;">${escapeHtml(user?.name || user?.email || '')}</a>
                        </div>
                        <button class="btn btn--ghost btn--sm" id="logout-btn">Log out</button>
                    ` : `
                        <a href="/login" class="btn btn--ghost btn--sm" data-link>Log in</a>
                        <a href="/register" class="btn btn--primary btn--sm" data-link>Sign up</a>
                    `}
                </div>

                <button class="header__menu-toggle" id="menu-toggle" aria-label="Toggle menu">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="3" y1="6" x2="21" y2="6"></line>
                        <line x1="3" y1="12" x2="21" y2="12"></line>
                        <line x1="3" y1="18" x2="21" y2="18"></line>
                    </svg>
                </button>
            </div>
        </nav>
    `;

    // Add active state to current nav link
    updateActiveNavLink();

    // Bind event handlers
    bindHeaderEvents();
}

/**
 * Update active state on navigation links
 */
function updateActiveNavLink() {
    const currentPath = window.location.pathname;
    const navLinks = document.querySelectorAll('.header__nav-link');

    navLinks.forEach(link => {
        const href = link.getAttribute('href');
        link.classList.remove('header__nav-link--active');

        if (href === currentPath || (href !== '/' && currentPath.startsWith(href))) {
            link.classList.add('header__nav-link--active');
        }
    });
}

/**
 * Bind event handlers for header elements
 */
function bindHeaderEvents() {
    // Logout buttons (desktop and mobile)
    const logoutButton = document.getElementById('logout-btn');
    if (logoutButton) {
        logoutButton.addEventListener('click', handleLogout);
    }
    const logoutButtonMobile = document.getElementById('logout-btn-mobile');
    if (logoutButtonMobile) {
        logoutButtonMobile.addEventListener('click', handleLogout);
    }

    // Mobile menu toggle
    const menuToggle = document.getElementById('menu-toggle');
    const mainNav = document.getElementById('main-nav');
    if (menuToggle && mainNav) {
        menuToggle.addEventListener('click', () => {
            mainNav.classList.toggle('header__nav--open');
        });
    }

    // Close mobile menu when clicking a link
    const navLinks = document.querySelectorAll('.header__nav-link');
    navLinks.forEach(link => {
        link.addEventListener('click', () => {
            mainNav?.classList.remove('header__nav--open');
        });
    });
}

/**
 * Handle logout button click
 */
async function handleLogout() {
    try {
        await logout();
        toast.success('You have been logged out');
        navigate('/');
    } catch (error) {
        toast.error('Failed to log out. Please try again.');
        console.error('Logout error:', error);
    }
}

/**
 * Escape HTML to prevent XSS
 * @param {string} text - Text to escape
 * @returns {string} Escaped text
 */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Subscribe to auth state changes to re-render header
store.subscribe(() => {
    renderHeader();
}, ['user', 'isAuthenticated']);

export default {
    renderHeader,
};
