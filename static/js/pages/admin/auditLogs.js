/**
 * Admin: Audit Logs page (Superuser only)
 * Allows superusers to view and filter audit logs.
 */

import { getAuditLogs, exportAuditLogs } from '../../api/admin.js';
import { ApiError } from '../../api/client.js';
import { isSuperuser } from '../../utils/store.js';
import toast from '../../components/toast.js';

let currentLogs = [];
let currentPage = 1;
let currentLimit = 25; // Default number of logs to display
let currentFilters = {};
let totalLogs = 0;

// Available page size options
const pageSizeOptions = [10, 25, 50, 100];

// Action type labels for display
const actionLabels = {
    user_registered: 'User Registered',
    user_login: 'Login',
    user_login_failed: 'Login Failed',
    user_logout: 'Logout',
    user_verified: 'User Verified',
    user_vouched: 'User Vouched',
    mfa_setup_started: 'MFA Setup Started',
    mfa_setup_completed: 'MFA Setup Completed',
    mfa_verified: 'MFA Verified',
    mfa_verify_failed: 'MFA Verify Failed',
    postcard_requested: 'Postcard Requested',
    postcard_verified: 'Postcard Verified',
    vouch_requested: 'Vouch Requested',
    vouch_given: 'Vouch Given',
    region_created: 'Community Created',
    region_updated: 'Community Updated',
    region_deleted: 'Community Deleted',
    signal_group_created: 'Signal Group Created',
    signal_group_updated: 'Signal Group Updated',
    signal_group_deleted: 'Signal Group Deleted',
    invite_link_updated: 'Invite Link Updated',
    proposal_created: 'Proposal Created',
    proposal_voted: 'Proposal Voted',
    proposal_approved: 'Proposal Approved',
    proposal_rejected: 'Proposal Rejected',
    user_blocked: 'User Blocked',
    user_unblocked: 'User Unblocked',
    superuser_grant_vouch: 'Superuser Grant Vouch',
    superuser_revoke_vouch: 'Superuser Revoke Vouch',
    superuser_user_search: 'Superuser User Search',
    superuser_user_delete: 'Superuser User Delete',
};

// Action categories for filtering
const actionCategories = {
    'Authentication': ['user_registered', 'user_login', 'user_login_failed', 'user_logout'],
    'MFA': ['mfa_setup_started', 'mfa_setup_completed', 'mfa_verified', 'mfa_verify_failed'],
    'Verification': ['postcard_requested', 'postcard_verified', 'vouch_requested', 'vouch_given', 'user_verified', 'user_vouched'],
    'Regions': ['region_created', 'region_updated', 'region_deleted'],
    'Signal Groups': ['signal_group_created', 'signal_group_updated', 'signal_group_deleted', 'invite_link_updated'],
    'Proposals': ['proposal_created', 'proposal_voted', 'proposal_approved', 'proposal_rejected'],
    'Moderation': ['user_blocked', 'user_unblocked'],
    'Superuser': ['superuser_grant_vouch', 'superuser_revoke_vouch', 'superuser_user_search', 'superuser_user_delete'],
};

/**
 * Render the audit logs page
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
                        Only superusers can view audit logs.
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
                    <h1 class="page__title">Audit Logs</h1>
                    <p class="page__subtitle">View system activity and security events.</p>
                </div>

                <!-- Filters Section -->
                <div class="card mb-6">
                    <div class="card__header">
                        <h3 style="font-weight: 600;">Filters</h3>
                        <button type="button" class="btn btn--sm btn--secondary" id="clear-filters-btn">Clear Filters</button>
                    </div>
                    <div class="card__body">
                        <form id="filter-form" class="audit-filters">
                            <div class="audit-filters__row">
                                <div class="form-group">
                                    <label class="form-label">Action Type</label>
                                    <select id="filter-action" class="form-select">
                                        <option value="">All Actions</option>
                                        ${Object.entries(actionCategories).map(([category, actions]) => `
                                            <optgroup label="${category}">
                                                ${actions.map(action => `
                                                    <option value="${action}">${actionLabels[action] || action}</option>
                                                `).join('')}
                                            </optgroup>
                                        `).join('')}
                                    </select>
                                </div>
                                <div class="form-group">
                                    <label class="form-label">User ID</label>
                                    <input type="text" id="filter-user-id" class="form-input" placeholder="Enter user ID...">
                                </div>
                                <div class="form-group">
                                    <label class="form-label">IP Address</label>
                                    <input type="text" id="filter-ip" class="form-input" placeholder="e.g., 192.168.1.1">
                                </div>
                            </div>
                            <div class="audit-filters__row">
                                <div class="form-group">
                                    <label class="form-label">Resource Type</label>
                                    <select id="filter-resource-type" class="form-select">
                                        <option value="">All Resources</option>
                                        <option value="user">User</option>
                                        <option value="region">Community</option>
                                        <option value="signal_group">Signal Group</option>
                                        <option value="verification_request">Verification Request</option>
                                        <option value="invite_link_proposal">Invite Link Proposal</option>
                                    </select>
                                </div>
                                <div class="form-group">
                                    <label class="form-label">Resource ID</label>
                                    <input type="text" id="filter-resource-id" class="form-input" placeholder="Enter resource ID...">
                                </div>
                                <div class="form-group">
                                    <label class="form-label">Date Range</label>
                                    <div class="date-range">
                                        <input type="date" id="filter-date-from" class="form-input">
                                        <span>to</span>
                                        <input type="date" id="filter-date-to" class="form-input">
                                    </div>
                                </div>
                            </div>
                            <div class="audit-filters__actions">
                                <div class="form-group" style="margin-bottom: 0; margin-right: auto;">
                                    <label class="form-label" style="margin-bottom: var(--space-1);">Results per page</label>
                                    <select id="filter-limit" class="form-select" style="width: auto; min-width: 80px;">
                                        ${pageSizeOptions.map(size => `
                                            <option value="${size}" ${size === currentLimit ? 'selected' : ''}>${size}</option>
                                        `).join('')}
                                    </select>
                                </div>
                                <button type="submit" class="btn btn--primary">Apply Filters</button>
                                <button type="button" class="btn btn--secondary" id="export-json-btn">Export JSON</button>
                                <button type="button" class="btn btn--secondary" id="export-csv-btn">Export CSV</button>
                            </div>
                        </form>
                    </div>
                </div>

                <!-- Results Section -->
                <div id="logs-content">
                    <div class="loading">
                        <div class="spinner spinner--lg"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind events
    const filterForm = document.getElementById('filter-form');
    filterForm.addEventListener('submit', handleFilterSubmit);

    document.getElementById('clear-filters-btn').addEventListener('click', handleClearFilters);
    document.getElementById('export-json-btn').addEventListener('click', () => handleExport('json'));
    document.getElementById('export-csv-btn').addEventListener('click', () => handleExport('csv'));

    // Handle page size changes
    document.getElementById('filter-limit').addEventListener('change', handleLimitChange);

    // Load initial data (latest logs)
    await loadLogs();
}

/**
 * Handle page size limit change
 * @param {Event} event - Change event
 */
async function handleLimitChange(event) {
    currentLimit = parseInt(event.target.value, 10);
    currentPage = 1; // Reset to first page when limit changes
    await loadLogs();
}

/**
 * Handle filter form submission
 * @param {Event} event - Submit event
 */
async function handleFilterSubmit(event) {
    event.preventDefault();
    currentPage = 1;
    collectFilters();
    await loadLogs();
}

/**
 * Collect filter values from form
 */
function collectFilters() {
    currentFilters = {};

    const action = document.getElementById('filter-action').value;
    if (action) currentFilters.action = action;

    const userId = document.getElementById('filter-user-id').value.trim();
    if (userId) currentFilters.user_id = userId;

    const ip = document.getElementById('filter-ip').value.trim();
    if (ip) currentFilters.ip_address = ip;

    const resourceType = document.getElementById('filter-resource-type').value;
    if (resourceType) currentFilters.resource_type = resourceType;

    const resourceId = document.getElementById('filter-resource-id').value.trim();
    if (resourceId) currentFilters.resource_id = resourceId;

    const dateFrom = document.getElementById('filter-date-from').value;
    if (dateFrom) currentFilters.date_from = new Date(dateFrom).toISOString();

    const dateTo = document.getElementById('filter-date-to').value;
    if (dateTo) {
        // Set to end of day
        const endDate = new Date(dateTo);
        endDate.setHours(23, 59, 59, 999);
        currentFilters.date_to = endDate.toISOString();
    }
}

/**
 * Handle clear filters
 */
async function handleClearFilters() {
    document.getElementById('filter-action').value = '';
    document.getElementById('filter-user-id').value = '';
    document.getElementById('filter-ip').value = '';
    document.getElementById('filter-resource-type').value = '';
    document.getElementById('filter-resource-id').value = '';
    document.getElementById('filter-date-from').value = '';
    document.getElementById('filter-date-to').value = '';
    document.getElementById('filter-limit').value = '25';

    currentFilters = {};
    currentPage = 1;
    currentLimit = 25;
    await loadLogs();
}

/**
 * Handle export
 * @param {string} format - Export format ('json' or 'csv')
 */
async function handleExport(format) {
    try {
        toast.info(`Preparing ${format.toUpperCase()} export...`);

        const blob = await exportAuditLogs(currentFilters, format);

        // Create download link
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `audit_logs_${new Date().toISOString().slice(0, 10)}.${format}`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);

        toast.success(`Audit logs exported as ${format.toUpperCase()}`);
    } catch (error) {
        console.error('Export failed:', error);
        toast.error('Failed to export audit logs');
    }
}

/**
 * Load and display audit logs
 */
async function loadLogs() {
    const content = document.getElementById('logs-content');

    content.innerHTML = `
        <div class="loading">
            <div class="spinner spinner--lg"></div>
        </div>
    `;

    try {
        const params = {
            ...currentFilters,
            page: currentPage,
            limit: currentLimit,
        };

        const response = await getAuditLogs(params);
        currentLogs = response.logs;
        totalLogs = response.total;

        if (currentLogs.length === 0) {
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4DD;</div>
                    <h3 class="empty-state__title">No Audit Logs Found</h3>
                    <p class="empty-state__description">
                        ${Object.keys(currentFilters).length > 0
                            ? 'No logs match your current filters. Try adjusting your search criteria.'
                            : 'There are no audit logs recorded yet.'}
                    </p>
                </div>
            `;
            return;
        }

        const totalPages = Math.ceil(totalLogs / currentLimit);

        content.innerHTML = `
            <div class="card">
                <div class="card__header">
                    <h3 style="font-weight: 600;">Results (${totalLogs.toLocaleString()} total)</h3>
                    <div class="pagination-info">
                        Page ${currentPage} of ${totalPages}
                    </div>
                </div>
                <div class="card__body" style="padding: 0; overflow-x: auto;">
                    <table class="data-table audit-table">
                        <thead>
                            <tr>
                                <th>Timestamp</th>
                                <th>Action</th>
                                <th>User</th>
                                <th>Resource</th>
                                <th>IP Address</th>
                                <th>Details</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${currentLogs.map(log => renderLogRow(log)).join('')}
                        </tbody>
                    </table>
                </div>
                ${totalPages > 1 ? `
                    <div class="card__footer">
                        <div class="pagination">
                            <button class="btn btn--sm btn--secondary" id="prev-page-btn" ${currentPage <= 1 ? 'disabled' : ''}>
                                Previous
                            </button>
                            <span class="pagination__info">Page ${currentPage} of ${totalPages}</span>
                            <button class="btn btn--sm btn--secondary" id="next-page-btn" ${currentPage >= totalPages ? 'disabled' : ''}>
                                Next
                            </button>
                        </div>
                    </div>
                ` : ''}
            </div>
        `;

        // Bind pagination buttons
        const prevBtn = document.getElementById('prev-page-btn');
        const nextBtn = document.getElementById('next-page-btn');

        if (prevBtn) {
            prevBtn.addEventListener('click', async () => {
                if (currentPage > 1) {
                    currentPage--;
                    await loadLogs();
                }
            });
        }

        if (nextBtn) {
            nextBtn.addEventListener('click', async () => {
                if (currentPage < totalPages) {
                    currentPage++;
                    await loadLogs();
                }
            });
        }

        // Bind detail toggles
        document.querySelectorAll('.toggle-details-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                const logId = btn.dataset.logId;
                const detailsRow = document.getElementById(`details-${logId}`);
                if (detailsRow) {
                    detailsRow.classList.toggle('hidden');
                    btn.textContent = detailsRow.classList.contains('hidden') ? 'Show' : 'Hide';
                }
            });
        });

    } catch (error) {
        console.error('Failed to load audit logs:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Audit Logs</h3>
                <p class="empty-state__description">
                    Failed to load audit logs. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a log table row
 * @param {Object} log - Log entry
 * @returns {string} HTML string
 */
function renderLogRow(log) {
    const timestamp = new Date(log.created_at).toLocaleString();
    const actionLabel = actionLabels[log.action] || log.action;
    const actionClass = getActionClass(log.action);

    const hasDetails = log.details && Object.keys(log.details).length > 0;

    return `
        <tr>
            <td class="audit-table__timestamp">${timestamp}</td>
            <td>
                <span class="badge ${actionClass}">${escapeHtml(actionLabel)}</span>
            </td>
            <td class="audit-table__user">
                ${log.user_id
                    ? `<code class="user-id" title="${escapeHtml(log.user_id)}">${escapeHtml(log.user_id.substring(0, 8))}...</code>`
                    : '<span class="text-muted">-</span>'}
            </td>
            <td class="audit-table__resource">
                ${log.resource_type
                    ? `<span class="resource-type">${escapeHtml(log.resource_type)}</span>
                       ${log.resource_id ? `<code class="resource-id" title="${escapeHtml(log.resource_id)}">${escapeHtml(log.resource_id.substring(0, 8))}...</code>` : ''}`
                    : '<span class="text-muted">-</span>'}
            </td>
            <td class="audit-table__ip">
                ${log.ip_address
                    ? `<code>${escapeHtml(log.ip_address)}</code>`
                    : '<span class="text-muted">-</span>'}
            </td>
            <td class="audit-table__details">
                ${hasDetails
                    ? `<button class="btn btn--sm btn--link toggle-details-btn" data-log-id="${log.id}">Show</button>`
                    : '<span class="text-muted">-</span>'}
            </td>
        </tr>
        ${hasDetails ? `
            <tr id="details-${log.id}" class="details-row hidden">
                <td colspan="6">
                    <div class="details-content">
                        <strong>Details:</strong>
                        <pre>${escapeHtml(JSON.stringify(log.details, null, 2))}</pre>
                        ${log.user_agent ? `
                            <div class="user-agent">
                                <strong>User Agent:</strong> ${escapeHtml(log.user_agent)}
                            </div>
                        ` : ''}
                    </div>
                </td>
            </tr>
        ` : ''}
    `;
}

/**
 * Get CSS class for action badge
 * @param {string} action - Action type
 * @returns {string} CSS class
 */
function getActionClass(action) {
    if (action.includes('failed') || action.includes('block')) {
        return 'badge--danger';
    }
    if (action.includes('login') || action.includes('verified') || action.includes('completed') || action.includes('approved')) {
        return 'badge--success';
    }
    if (action.includes('created') || action.includes('registered')) {
        return 'badge--primary';
    }
    if (action.includes('deleted') || action.includes('revoke')) {
        return 'badge--warning';
    }
    if (action.includes('superuser')) {
        return 'badge--superuser';
    }
    return 'badge--secondary';
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
    currentLogs = [];
    currentPage = 1;
    currentLimit = 25;
    currentFilters = {};
    totalLogs = 0;
}

export default { render, cleanup };
