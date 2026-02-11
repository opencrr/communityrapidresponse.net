/**
 * Admin: Blocked Location Fingerprints page (Superuser only)
 * Allows superusers to view and manage blocked address fingerprints.
 *
 * Note: We never store actual addresses. When a user is blocked, we store
 * a one-way fingerprint (hash) of their verified address. This allows us
 * to prevent re-registration from that location without knowing the actual address.
 */

import {
    getBlocklistedAddresses,
    expireBlocklistedAddress,
} from '../../api/blocklist.js?v=c49aed9';
import { ApiError } from '../../api/client.js?v=c49aed9';
import { isSuperuser } from '../../utils/store.js?v=c49aed9';
import toast from '../../components/toast.js?v=c49aed9';
import modal from '../../components/modal.js?v=c49aed9';
import { navigate } from '../../app.js?v=c49aed9';

let showActiveOnly = true;

/**
 * Render the blocked location fingerprints page
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
                        Only superusers can view and manage blocked location fingerprints.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>Return to Dashboard</a>
                </div>
            </div>
        `;
        return;
    }

    // Get filter from URL
    const urlParams = new URLSearchParams(window.location.search);
    showActiveOnly = urlParams.get('active') !== 'false';

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <div class="page__header-content">
                        <h1 class="page__title">Blocked Location Fingerprints</h1>
                        <p class="page__subtitle">Manage location fingerprints associated with blocked users.</p>
                    </div>
                </div>

                <div class="card card--info mb-4">
                    <div class="card__body">
                        <strong>Privacy Note:</strong> We never store actual addresses. When a user is blocked,
                        we save a one-way fingerprint (cryptographic hash) of their verified address. This prevents
                        re-registration from that location while keeping the actual address private&mdash;even from administrators.
                    </div>
                </div>

                <div class="filter-bar mb-4">
                    <label class="checkbox">
                        <input type="checkbox" id="active-only-checkbox" ${showActiveOnly ? 'checked' : ''}>
                        <span class="checkbox__label">Show active only</span>
                    </label>
                </div>

                <div id="addresses-content">
                    <div class="loading">
                        <div class="spinner spinner--lg"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind filter checkbox
    const checkbox = document.getElementById('active-only-checkbox');
    if (checkbox) {
        checkbox.addEventListener('change', () => {
            showActiveOnly = checkbox.checked;
            // Update URL
            const url = new URL(window.location);
            if (showActiveOnly) {
                url.searchParams.delete('active');
            } else {
                url.searchParams.set('active', 'false');
            }
            window.history.replaceState({}, '', url);
            loadAddresses();
        });
    }

    await loadAddresses();
}

/**
 * Load and display blocked location fingerprints
 */
async function loadAddresses() {
    const content = document.getElementById('addresses-content');
    if (!content) return;

    content.innerHTML = `
        <div class="loading">
            <div class="spinner spinner--lg"></div>
        </div>
    `;

    try {
        const addresses = await getBlocklistedAddresses(showActiveOnly);

        if (addresses.length === 0) {
            const emptyMessage = showActiveOnly
                ? 'There are no active blocked location fingerprints.'
                : 'No blocked location fingerprints found.';
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F3E0;</div>
                    <h3 class="empty-state__title">No Blocked Location Fingerprints</h3>
                    <p class="empty-state__description">${emptyMessage}</p>
                </div>
            `;
            return;
        }

        content.innerHTML = `
            <div class="table-container">
                <table class="table">
                    <thead>
                        <tr>
                            <th>Location Fingerprint</th>
                            <th>Blocked User</th>
                            <th>Blocked At</th>
                            <th>Expires At</th>
                            <th>Status</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${addresses.map(address => renderAddressRow(address)).join('')}
                    </tbody>
                </table>
            </div>
        `;

        // Bind action buttons
        bindAddressActions();
    } catch (error) {
        console.error('Failed to load location fingerprints:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Data</h3>
                <p class="empty-state__description">
                    Failed to load blocked location fingerprints. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a single fingerprint row
 * @param {Object} address - Fingerprint data
 * @returns {string} HTML string
 */
function renderAddressRow(address) {
    const isActive = address.is_active;
    const truncatedHash = truncateHash(address.address_hash);

    return `
        <tr class="${isActive ? '' : 'table__row--muted'}">
            <td>
                <code class="address-hash" title="${escapeHtml(address.address_hash)}">${truncatedHash}</code>
            </td>
            <td>${escapeHtml(address.username || 'Unknown')}</td>
            <td>${formatDate(address.blocked_at)}</td>
            <td>${formatDate(address.expires_at)}</td>
            <td>
                <span class="badge badge--${isActive ? 'danger' : 'secondary'}">
                    ${isActive ? 'Active' : 'Expired'}
                </span>
            </td>
            <td>
                ${isActive ? `
                    <button class="btn btn--sm btn--warning expire-address-btn" data-hash="${escapeHtml(address.address_hash)}">
                        Expire Now
                    </button>
                ` : '-'}
            </td>
        </tr>
    `;
}

/**
 * Truncate fingerprint hash for display
 * @param {string} hash - Full hash
 * @returns {string} Truncated hash (first 8 + last 8 chars)
 */
function truncateHash(hash) {
    if (!hash || hash.length <= 16) return hash || '';
    return `${hash.substring(0, 8)}...${hash.substring(hash.length - 8)}`;
}

/**
 * Bind event handlers for address actions
 */
function bindAddressActions() {
    document.querySelectorAll('.expire-address-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const hash = btn.dataset.hash;
            handleExpireAddress(hash);
        });
    });
}

/**
 * Handle expiring a location fingerprint
 * @param {string} addressHash - Location fingerprint to expire
 */
async function handleExpireAddress(addressHash) {
    const truncated = truncateHash(addressHash);

    const confirmed = await modal.confirm({
        title: 'Expire Location Fingerprint',
        message: `Are you sure you want to expire this blocked location fingerprint (${truncated})? This will allow new users to register and verify from this location.`,
        confirmLabel: 'Expire',
        confirmType: 'warning',
    });

    if (!confirmed) return;

    try {
        await expireBlocklistedAddress(addressHash);
        toast.success('Location fingerprint has been expired.');
        await loadAddresses();
    } catch (error) {
        let errorMessage = 'Failed to expire location fingerprint.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Format date for display
 * @param {string} dateString - ISO date string
 * @returns {string} Formatted date
 */
function formatDate(dateString) {
    if (!dateString) return '';
    const date = new Date(dateString);
    return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
    });
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
    showActiveOnly = true;
}

export default { render, cleanup };
