/**
 * Admin: User Reports page
 * Allows admins to view, review, and resolve user reports.
 */

import {
    getReports,
    getReportDetails,
    resolveReport,
} from '../../api/reports.js?v=c49aed9';
import { ApiError } from '../../api/client.js?v=c49aed9';
import { isAdmin, isSuperuser } from '../../utils/store.js?v=c49aed9';
import toast from '../../components/toast.js?v=c49aed9';
import modal from '../../components/modal.js?v=c49aed9';
import { navigate } from '../../app.js?v=c49aed9';

let currentFilter = 'pending';

const REASON_LABELS = {
    harassment: 'Harassment',
    spam: 'Spam',
    impersonation: 'Impersonation',
    fraudulent_verification: 'Fraudulent Verification',
    other: 'Other',
};

/**
 * Render the user reports page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const userIsAdmin = isAdmin();

    if (!userIsAdmin) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Admin Access Required</h3>
                    <p class="empty-state__description">
                        You need both postcard and vouch verification to view user reports.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>View Verification Status</a>
                </div>
            </div>
        `;
        return;
    }

    // Get status filter from URL
    const urlParams = new URLSearchParams(window.location.search);
    currentFilter = urlParams.get('status') || 'pending';

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <div class="page__header-content">
                        <h1 class="page__title">User Reports</h1>
                        <p class="page__subtitle">Review reports filed by community members against other users.</p>
                    </div>
                </div>

                <div class="proposal-filters mb-4">
                    <div class="filter-tabs">
                        <button class="filter-tab ${currentFilter === 'pending' ? 'filter-tab--active' : ''}" data-status="pending">
                            Pending
                        </button>
                        <button class="filter-tab ${currentFilter === 'dismissed' ? 'filter-tab--active' : ''}" data-status="dismissed">
                            Dismissed
                        </button>
                        <button class="filter-tab ${currentFilter === 'resolved_blocklist' ? 'filter-tab--active' : ''}" data-status="resolved_blocklist">
                            Resolved
                        </button>
                        <button class="filter-tab ${currentFilter === '' ? 'filter-tab--active' : ''}" data-status="">
                            All
                        </button>
                    </div>
                </div>

                <div id="reports-content">
                    <div class="loading">
                        <div class="spinner spinner--lg"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind filter tabs
    document.querySelectorAll('.filter-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            const status = tab.dataset.status;
            currentFilter = status;
            const url = new URL(window.location);
            if (status) {
                url.searchParams.set('status', status);
            } else {
                url.searchParams.delete('status');
            }
            window.history.replaceState({}, '', url);
            document.querySelectorAll('.filter-tab').forEach(t => t.classList.remove('filter-tab--active'));
            tab.classList.add('filter-tab--active');
            loadReports();
        });
    });

    await loadReports();
}

/**
 * Load and display reports
 */
async function loadReports() {
    const content = document.getElementById('reports-content');
    if (!content) return;

    content.innerHTML = `
        <div class="loading">
            <div class="spinner spinner--lg"></div>
        </div>
    `;

    try {
        const params = {};
        if (currentFilter) {
            params.status = currentFilter;
        }
        const reports = await getReports(params);

        if (reports.length === 0) {
            const emptyMessage = currentFilter === 'pending'
                ? 'There are no pending user reports requiring your review.'
                : `No ${currentFilter ? currentFilter.replace('_', ' ') : ''} user reports found.`;
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4CB;</div>
                    <h3 class="empty-state__title">No Reports</h3>
                    <p class="empty-state__description">${emptyMessage}</p>
                </div>
            `;
            return;
        }

        content.innerHTML = `
            <div class="proposals-grid">
                ${reports.map(report => renderReportCard(report)).join('')}
            </div>
        `;

        bindReportActions();
    } catch (error) {
        console.error('Failed to load reports:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Reports</h3>
                <p class="empty-state__description">
                    Failed to load reports. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a report card
 * @param {Object} report - Report data
 * @returns {string} HTML string
 */
function renderReportCard(report) {
    const statusClass = getStatusClass(report.status);
    const statusLabel = formatStatus(report.status);
    const reasonLabel = REASON_LABELS[report.reason] || report.reason;
    const scopeLabel = formatScopeType(report.scope_type);

    return `
        <div class="card proposal-card proposal-card--${report.status}" data-report-id="${report.id}">
            <div class="card__body">
                <div class="proposal-card__header">
                    <div>
                        <div class="proposal-card__group">Report: ${escapeHtml(report.reported_user)}</div>
                        <div class="proposal-card__region">${scopeLabel}: ${escapeHtml(report.scope_name)}</div>
                    </div>
                    <span class="badge badge--${statusClass}">${statusLabel}</span>
                </div>
                <div class="proposal-card__reason">
                    <strong>Reason:</strong> ${escapeHtml(reasonLabel)}
                </div>
                ${report.report_count > 1 ? `
                    <div style="margin-top: var(--space-1); color: var(--color-warning-600); font-size: var(--font-size-sm);">
                        This user has ${report.report_count} pending report(s) total
                    </div>
                ` : ''}
                <div class="proposal-card__meta">
                    <span>${formatDate(report.created_at)}</span>
                </div>
                <div class="proposal-card__footer">
                    <span></span>
                    <button class="btn btn--sm btn--primary view-report-btn" data-report-id="${report.id}">
                        View
                    </button>
                </div>
            </div>
        </div>
    `;
}

/**
 * Get CSS class for status badge
 */
function getStatusClass(status) {
    switch (status) {
        case 'pending': return 'pending';
        case 'dismissed': return 'secondary';
        case 'resolved_blocklist': return 'success';
        default: return 'secondary';
    }
}

/**
 * Format status for display
 */
function formatStatus(status) {
    switch (status) {
        case 'pending': return 'Pending';
        case 'dismissed': return 'Dismissed';
        case 'resolved_blocklist': return 'Resolved';
        default: return status;
    }
}

/**
 * Format scope type for display
 */
function formatScopeType(scopeType) {
    switch (scopeType) {
        case 'region': return 'Community';
        case 'school': return 'School';
        case 'district': return 'District';
        default: return scopeType;
    }
}

/**
 * Bind event handlers for report actions
 */
function bindReportActions() {
    document.querySelectorAll('.view-report-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const reportId = btn.dataset.reportId;
            showReportDetailModal(reportId);
        });
    });
}

/**
 * Show report detail modal
 */
async function showReportDetailModal(reportId) {
    modal.showModal({
        title: 'Loading Report...',
        content: `
            <div class="loading">
                <div class="spinner spinner--lg"></div>
            </div>
        `,
        actions: [],
    });

    try {
        const report = await getReportDetails(reportId);
        modal.closeModal();

        const isPending = report.status === 'pending';
        const statusClass = getStatusClass(report.status);
        const reasonLabel = REASON_LABELS[report.reason] || report.reason;
        const scopeLabel = formatScopeType(report.scope_type);

        const contentHtml = `
            <div class="proposal-detail">
                <div class="proposal-detail__header">
                    <div class="proposal-detail__group">Report: ${escapeHtml(report.reported_username)}</div>
                    <div class="proposal-detail__region">${scopeLabel}: ${escapeHtml(report.scope_name)}</div>
                    <span class="badge badge--${statusClass}">${formatStatus(report.status)}</span>
                </div>

                <div class="proposal-detail__section">
                    <label>Reported User</label>
                    <p>${escapeHtml(report.reported_username)}</p>
                </div>

                <div class="proposal-detail__section">
                    <label>Reported by</label>
                    <p>${escapeHtml(report.reporter_username)}</p>
                </div>

                <div class="proposal-detail__section">
                    <label>Reason</label>
                    <p>${escapeHtml(reasonLabel)}</p>
                </div>

                ${report.details ? `
                    <div class="proposal-detail__section">
                        <label>Details</label>
                        <p>${escapeHtml(report.details)}</p>
                    </div>
                ` : ''}

                ${report.resolved_by_username ? `
                    <div class="proposal-detail__section">
                        <label>Resolved by</label>
                        <p>${escapeHtml(report.resolved_by_username)}</p>
                    </div>
                ` : ''}

                ${report.resolution_note ? `
                    <div class="proposal-detail__section">
                        <label>Resolution Note</label>
                        <p>${escapeHtml(report.resolution_note)}</p>
                    </div>
                ` : ''}

                ${isPending ? `
                    <div class="form__group" style="margin-top: var(--space-4);">
                        <label for="resolution-note" class="form__label">Resolution Note (optional)</label>
                        <textarea id="resolution-note" class="form__input form__textarea" rows="2" placeholder="Add a note about this resolution..."></textarea>
                    </div>
                ` : ''}

                <div class="proposal-detail__timestamps">
                    <span>Created: ${formatDate(report.created_at)}</span>
                    ${report.resolved_at ? `<span>Resolved: ${formatDate(report.resolved_at)}</span>` : ''}
                </div>
            </div>
        `;

        const actions = [{ label: 'Close', type: 'secondary' }];

        if (isPending) {
            actions.push({
                label: 'Dismiss',
                type: 'warning',
                closeOnClick: false,
                onClick: async () => {
                    await handleResolve(reportId, 'dismiss');
                },
            });
            actions.push({
                label: 'Initiate Blocklist',
                type: 'danger',
                closeOnClick: false,
                onClick: async () => {
                    await handleResolve(reportId, 'initiate_blocklist');
                },
            });
        }

        modal.showModal({
            title: 'Report Details',
            content: contentHtml,
            actions,
        });
    } catch (error) {
        console.error('Failed to load report details:', error);
        modal.closeModal();
        toast.error('Failed to load report details');
    }
}

/**
 * Handle resolving a report
 */
async function handleResolve(reportId, action) {
    const note = document.getElementById('resolution-note')?.value?.trim() || null;

    try {
        const result = await resolveReport(reportId, action, note);
        modal.closeModal();

        if (action === 'dismiss') {
            toast.success('Report has been dismissed.');
        } else {
            toast.success('Report resolved. Redirecting to create blocklist proposal...');
            // Redirect to blocklist proposals page with pre-populated params
            const params = new URLSearchParams();
            if (result.reported_user_id) params.set('target_user_id', result.reported_user_id);
            if (result.region_id) params.set('region_id', result.region_id);
            if (result.school_id) params.set('school_id', result.school_id);
            if (result.district_id) params.set('district_id', result.district_id);
            const queryString = params.toString();
            navigate(`/admin/blocklist-proposals${queryString ? '?' + queryString : ''}`);
            return;
        }

        await loadReports();
    } catch (error) {
        let errorMessage = 'Failed to resolve report.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Format date for display
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
 */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function cleanup() {
    currentFilter = 'pending';
}

export default { render, cleanup };
