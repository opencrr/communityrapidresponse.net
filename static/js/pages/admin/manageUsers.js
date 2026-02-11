/**
 * Admin: Manage Users page (Superuser only)
 * Allows superusers to search for users and grant/revoke vouch verification.
 */

import {
    searchUsers,
    getUsers,
    grantVouchVerification,
    revokeVouchVerification,
    blockUser,
    unblockUser,
    deleteUser,
} from '../../api/admin.js?v=c49aed9';
import { ApiError } from '../../api/client.js?v=c49aed9';
import { isSuperuser } from '../../utils/store.js?v=c49aed9';
import toast from '../../components/toast.js?v=c49aed9';
import modal from '../../components/modal.js?v=c49aed9';

let currentUsers = [];
let currentPage = 1;
let searchQuery = '';

/**
 * Render the manage users page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const userIsSuperuser = isSuperuser();

    if (!userIsSuperuser) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Superuser Access Required</h3>
                    <p class="empty-state__description">
                        Only superusers can manage users. Superuser accounts must be created directly in the database.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>Back to Dashboard</a>
                </div>
            </div>
        `;
        return;
    }

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">Manage Users</h1>
                    <p class="page__subtitle">Search for users and manage their verification status.</p>
                </div>

                <!-- Search Section -->
                <div class="card mb-6">
                    <div class="card__body">
                        <form id="search-form" class="search-form">
                            <div class="form-group" style="margin-bottom: 0; flex: 1;">
                                <input
                                    type="text"
                                    id="search-input"
                                    name="query"
                                    class="form-input"
                                    placeholder="Search by email or username..."
                                    autocomplete="off"
                                >
                            </div>
                            <button type="submit" class="btn btn--primary">Search</button>
                        </form>
                    </div>
                </div>

                <!-- Results Section -->
                <div id="users-content">
                    <div class="empty-state">
                        <div class="empty-state__icon">&#x1F50D;</div>
                        <h3 class="empty-state__title">Search for Users</h3>
                        <p class="empty-state__description">
                            Enter an email or username above to find users.
                        </p>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind search form
    const searchForm = document.getElementById('search-form');
    searchForm.addEventListener('submit', handleSearch);
}

/**
 * Handle search form submission
 * @param {Event} event - Submit event
 */
async function handleSearch(event) {
    event.preventDefault();

    const input = document.getElementById('search-input');
    searchQuery = input.value.trim();

    if (!searchQuery) {
        toast.warning('Please enter a search term');
        return;
    }

    await loadUsers();
}

/**
 * Load and display users
 */
async function loadUsers() {
    const content = document.getElementById('users-content');

    content.innerHTML = `
        <div class="loading">
            <div class="spinner spinner--lg"></div>
        </div>
    `;

    try {
        currentUsers = await searchUsers(searchQuery);

        if (currentUsers.length === 0) {
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F645;</div>
                    <h3 class="empty-state__title">No Users Found</h3>
                    <p class="empty-state__description">
                        No users match your search for "${escapeHtml(searchQuery)}".
                    </p>
                </div>
            `;
            return;
        }

        content.innerHTML = `
            <div class="card">
                <div class="card__header">
                    <h3 style="font-weight: 600;">Search Results (${currentUsers.length})</h3>
                </div>
                <div class="card__body" style="padding: 0;">
                    <table class="data-table">
                        <thead>
                            <tr>
                                <th>Email</th>
                                <th>Verification Status</th>
                                <th>Created</th>
                                <th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${currentUsers.map(user => renderUserRow(user)).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;

        // Bind action buttons
        bindUserActions();
    } catch (error) {
        console.error('Failed to load users:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Users</h3>
                <p class="empty-state__description">
                    Failed to load users. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a user table row
 * @param {Object} user - User data
 * @returns {string} HTML string
 */
function renderUserRow(user) {
    const statusBadges = [];

    if (user.is_superuser) {
        statusBadges.push('<span class="badge badge--superuser">Superuser</span>');
    }
    if (user.is_blocked) {
        statusBadges.push('<span class="badge badge--blocked">Blocked</span>');
    }
    if (user.postcard_verified) {
        statusBadges.push('<span class="badge badge--postcard">Postcard</span>');
    }
    if (user.vouch_verified) {
        statusBadges.push('<span class="badge badge--vouch">Vouched</span>');
    }
    if (!user.is_superuser && !user.is_blocked && !user.postcard_verified && !user.vouch_verified) {
        statusBadges.push('<span class="badge badge--unverified">Unverified</span>');
    }

    const canGrantVouch = !user.is_superuser && !user.vouch_verified;
    const canRevokeVouch = !user.is_superuser && user.vouch_verified;
    const canBlock = !user.is_superuser && !user.is_blocked;
    const canUnblock = !user.is_superuser && user.is_blocked;
    const canDelete = !user.is_superuser;

    const createdDate = user.created_at
        ? new Date(user.created_at).toLocaleDateString()
        : 'Unknown';

    return `
        <tr data-user-id="${user.id}" ${user.is_blocked ? 'class="user-row--blocked"' : ''}>
            <td>
                <div class="user-info">
                    <div class="user-info__email">${escapeHtml(user.email)}</div>
                    ${user.username ? `<div class="user-info__username">@${escapeHtml(user.username)}</div>` : ''}
                </div>
            </td>
            <td>
                <div class="badge-group">
                    ${statusBadges.join('')}
                </div>
            </td>
            <td>${createdDate}</td>
            <td>
                <div class="action-buttons">
                    ${user.is_superuser ? `
                        <span class="text-muted">Superuser (no actions)</span>
                    ` : `
                        ${canGrantVouch ? `
                            <button class="btn btn--sm btn--primary grant-vouch-btn" data-user-id="${user.id}" data-user-email="${escapeHtml(user.email)}">
                                Grant Vouch
                            </button>
                        ` : ''}
                        ${canRevokeVouch ? `
                            <button class="btn btn--sm btn--secondary revoke-vouch-btn" data-user-id="${user.id}" data-user-email="${escapeHtml(user.email)}">
                                Revoke Vouch
                            </button>
                        ` : ''}
                        ${canBlock ? `
                            <button class="btn btn--sm btn--warning block-btn" data-user-id="${user.id}" data-user-email="${escapeHtml(user.email)}">
                                Block
                            </button>
                        ` : ''}
                        ${canUnblock ? `
                            <button class="btn btn--sm btn--secondary unblock-btn" data-user-id="${user.id}" data-user-email="${escapeHtml(user.email)}">
                                Unblock
                            </button>
                        ` : ''}
                        <button class="btn btn--sm btn--danger delete-btn" data-user-id="${user.id}" data-user-email="${escapeHtml(user.email)}">
                            Delete
                        </button>
                    `}
                </div>
            </td>
        </tr>
    `;
}

/**
 * Bind event handlers for user action buttons
 */
function bindUserActions() {
    // Grant vouch buttons
    document.querySelectorAll('.grant-vouch-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const userId = btn.dataset.userId;
            const userEmail = btn.dataset.userEmail;
            handleGrantVouch(userId, userEmail);
        });
    });

    // Revoke vouch buttons
    document.querySelectorAll('.revoke-vouch-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const userId = btn.dataset.userId;
            const userEmail = btn.dataset.userEmail;
            handleRevokeVouch(userId, userEmail);
        });
    });

    // Block buttons
    document.querySelectorAll('.block-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const userId = btn.dataset.userId;
            const userEmail = btn.dataset.userEmail;
            handleBlock(userId, userEmail);
        });
    });

    // Unblock buttons
    document.querySelectorAll('.unblock-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const userId = btn.dataset.userId;
            const userEmail = btn.dataset.userEmail;
            handleUnblock(userId, userEmail);
        });
    });

    // Delete buttons
    document.querySelectorAll('.delete-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const userId = btn.dataset.userId;
            const userEmail = btn.dataset.userEmail;
            handleDelete(userId, userEmail);
        });
    });
}

/**
 * Handle grant vouch verification
 * @param {string} userId - User ID
 * @param {string} userEmail - User email for display
 */
async function handleGrantVouch(userId, userEmail) {
    const confirmed = await modal.confirm({
        title: 'Grant Vouch Verification',
        message: `Are you sure you want to grant vouch verification to ${userEmail}? This will give them read-only access to Signal groups. If they are also postcard verified, they will gain admin access.`,
        confirmLabel: 'Grant Vouch',
        confirmType: 'primary',
    });

    if (!confirmed) return;

    try {
        await grantVouchVerification(userId);
        toast.success(`Vouch verification granted to ${userEmail}`);
        await loadUsers();
    } catch (error) {
        let errorMessage = 'Failed to grant vouch verification.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Handle revoke vouch verification
 * @param {string} userId - User ID
 * @param {string} userEmail - User email for display
 */
async function handleRevokeVouch(userId, userEmail) {
    const confirmed = await modal.confirm({
        title: 'Revoke Vouch Verification',
        message: `Are you sure you want to revoke vouch verification from ${userEmail}? This may remove their access to Signal groups or admin privileges.`,
        confirmLabel: 'Revoke Vouch',
        confirmType: 'danger',
    });

    if (!confirmed) return;

    try {
        await revokeVouchVerification(userId);
        toast.success(`Vouch verification revoked from ${userEmail}`);
        await loadUsers();
    } catch (error) {
        let errorMessage = 'Failed to revoke vouch verification.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Handle block user
 * @param {string} userId - User ID
 * @param {string} userEmail - User email for display
 */
async function handleBlock(userId, userEmail) {
    const result = await modal.prompt({
        title: 'Block User',
        message: `Are you sure you want to block ${userEmail}? This will revoke their access to all Signal groups and prevent them from logging in.`,
        inputLabel: 'Reason for blocking (optional)',
        inputPlaceholder: 'Enter reason...',
        confirmLabel: 'Block User',
        confirmType: 'danger',
    });

    if (result === null) return;

    try {
        await blockUser(userId, result || '');
        toast.success(`${userEmail} has been blocked`);
        await loadUsers();
    } catch (error) {
        let errorMessage = 'Failed to block user.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Handle unblock user
 * @param {string} userId - User ID
 * @param {string} userEmail - User email for display
 */
async function handleUnblock(userId, userEmail) {
    const confirmed = await modal.confirm({
        title: 'Unblock User',
        message: `Are you sure you want to unblock ${userEmail}? They will be able to log in again.`,
        confirmLabel: 'Unblock User',
        confirmType: 'primary',
    });

    if (!confirmed) return;

    try {
        await unblockUser(userId);
        toast.success(`${userEmail} has been unblocked`);
        await loadUsers();
    } catch (error) {
        let errorMessage = 'Failed to unblock user.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Handle delete user
 * @param {string} userId - User ID
 * @param {string} userEmail - User email for display
 */
async function handleDelete(userId, userEmail) {
    const confirmed = await modal.confirm({
        title: 'Permanently Delete User',
        message: `Are you sure you want to PERMANENTLY delete ${userEmail}? This action cannot be undone. All of their data, region memberships, and verification status will be removed.`,
        confirmLabel: 'Delete Permanently',
        confirmType: 'danger',
    });

    if (!confirmed) return;

    // Double confirmation for permanent deletion
    const doubleConfirmed = await modal.confirm({
        title: 'Confirm Permanent Deletion',
        message: `This is your final warning. Deleting ${userEmail} is IRREVERSIBLE. Are you absolutely sure?`,
        confirmLabel: 'Yes, Delete Forever',
        confirmType: 'danger',
    });

    if (!doubleConfirmed) return;

    try {
        await deleteUser(userId);
        toast.success(`${userEmail} has been permanently deleted`);
        await loadUsers();
    } catch (error) {
        let errorMessage = 'Failed to delete user.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
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

export function cleanup() {
    currentUsers = [];
    currentPage = 1;
    searchQuery = '';
}

export default { render, cleanup };
