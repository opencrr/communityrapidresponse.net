/**
 * Admin Membership Requests Page
 * Handles viewing and voting on sub-region membership requests.
 */

import {
    listPendingForAdmin,
    getRequest,
    voteOnRequest,
    listPendingForRegion,
} from '../../api/membership.js';
import { getUser, isAdmin, isSuperuser } from '../../utils/store.js';
import { ApiError } from '../../api/client.js';
import toast from '../../components/toast.js';
import modal from '../../components/modal.js';
import { navigate } from '../../app.js';

let currentFilter = 'all';

/**
 * Render the admin membership requests page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const user = getUser();
    const userIsAdmin = isAdmin();
    const userIsSuperuser = isSuperuser();

    if (!userIsAdmin && !userIsSuperuser) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Admin Access Required</h3>
                    <p class="empty-state__description">
                        You need both postcard and vouch verification to access this page.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>Back to Dashboard</a>
                </div>
            </div>
        `;
        return;
    }

    container.innerHTML = `
        <div class="page">
            <div class="page__container" style="max-width: 1000px;">
                <div class="page__header">
                    <h1 class="page__title">Membership Requests</h1>
                    <p class="page__subtitle">
                        Review and approve requests to join communities you administer.
                    </p>
                </div>

                <!-- Filter tabs -->
                <div class="tabs" style="margin-bottom: var(--space-4);">
                    <button class="tab tab--active" data-filter="all">All Pending</button>
                    <button class="tab" data-filter="voted">Voted</button>
                    <button class="tab" data-filter="not-voted">Not Voted</button>
                </div>

                <!-- Stats Overview -->
                <div class="stats-row" id="stats-container" style="margin-bottom: var(--space-6);">
                    <div class="stat-card">
                        <div class="stat-card__value" id="stat-pending">-</div>
                        <div class="stat-card__label">Pending</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-card__value" id="stat-voted">-</div>
                        <div class="stat-card__label">You Voted</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-card__value" id="stat-need-vote">-</div>
                        <div class="stat-card__label">Need Your Vote</div>
                    </div>
                </div>

                <!-- Requests List -->
                <div class="card">
                    <div class="card__header">
                        <h2 class="card__title">Pending Requests</h2>
                    </div>
                    <div class="card__body" id="requests-container">
                        <div class="loading">
                            <div class="spinner"></div>
                        </div>
                    </div>
                </div>

                <!-- Info Section -->
                <div class="privacy-notice" style="margin-top: var(--space-6);">
                    <div class="privacy-notice__icon">&#x2139;</div>
                    <div class="privacy-notice__text">
                        <strong>Consensus Approval</strong>
                        Membership requests require 2 admin approvals before the user is added to the community.
                        This ensures community consensus on new members.
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind filter handlers
    bindFilterHandlers();

    // Load data
    await loadRequests();
}

/**
 * Bind filter tab handlers
 */
function bindFilterHandlers() {
    document.querySelectorAll('.tab[data-filter]').forEach(tab => {
        tab.addEventListener('click', async () => {
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('tab--active'));
            tab.classList.add('tab--active');
            currentFilter = tab.dataset.filter;
            await loadRequests();
        });
    });
}

/**
 * Load and render pending requests
 */
async function loadRequests() {
    const container = document.getElementById('requests-container');
    if (!container) return;

    container.innerHTML = `
        <div class="loading">
            <div class="spinner"></div>
        </div>
    `;

    try {
        const response = await listPendingForAdmin();
        const allRequests = response.requests || [];

        // Update stats
        updateStats(allRequests);

        // Filter requests based on current filter
        let requests = allRequests;
        if (currentFilter === 'voted') {
            requests = allRequests.filter(r => r.has_voted);
        } else if (currentFilter === 'not-voted') {
            requests = allRequests.filter(r => !r.has_voted);
        }

        if (requests.length === 0) {
            const emptyMessage = currentFilter === 'all'
                ? 'No pending membership requests for your regions.'
                : currentFilter === 'voted'
                    ? 'You haven\'t voted on any requests yet.'
                    : 'You\'ve voted on all pending requests.';

            container.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <p>${emptyMessage}</p>
                </div>
            `;
            return;
        }

        container.innerHTML = `
            <div class="requests-list">
                ${requests.map(req => renderRequestRow(req)).join('')}
            </div>
        `;

        bindRequestHandlers();
    } catch (error) {
        console.error('Failed to load requests:', error);
        container.innerHTML = `
            <div class="form-error">Failed to load requests. Please refresh the page.</div>
        `;
    }
}

/**
 * Update stats display
 * @param {Array} requests - All requests
 */
function updateStats(requests) {
    const pending = requests.length;
    const voted = requests.filter(r => r.has_voted).length;
    const needVote = requests.filter(r => !r.has_voted).length;

    const statPending = document.getElementById('stat-pending');
    const statVoted = document.getElementById('stat-voted');
    const statNeedVote = document.getElementById('stat-need-vote');

    if (statPending) statPending.textContent = pending;
    if (statVoted) statVoted.textContent = voted;
    if (statNeedVote) statNeedVote.textContent = needVote;
}

/**
 * Render a single request row
 * @param {Object} request - Request data
 * @returns {string} HTML string
 */
function renderRequestRow(request) {
    const expiresAt = new Date(request.expires_at);
    const daysLeft = Math.ceil((expiresAt - new Date()) / (1000 * 60 * 60 * 24));
    const votesNeeded = request.votes_needed || 2;
    const currentVotes = request.current_votes || 0;
    const progressPercent = Math.min((currentVotes / votesNeeded) * 100, 100);

    return `
        <div class="request-row" data-request-id="${request.request_id}">
            <div class="request-row__main">
                <div class="request-row__user">
                    <strong>${escapeHtml(request.user_name || request.username || 'Unknown User')}</strong>
                    <span class="request-row__email">${escapeHtml(request.user_email || '')}</span>
                </div>
                <div class="request-row__region">
                    <span class="badge badge--region">${escapeHtml(request.region_name || 'Unknown Community')}</span>
                </div>
            </div>
            <div class="request-row__meta">
                <div class="request-row__progress">
                    <span class="request-row__votes">${currentVotes}/${votesNeeded} approvals</span>
                    <div class="vouch-progress__bar" style="width: 80px; height: 4px;">
                        <div class="vouch-progress__fill" style="width: ${progressPercent}%"></div>
                    </div>
                </div>
                <span class="request-row__expires">${daysLeft}d left</span>
            </div>
            <div class="request-row__actions">
                ${request.has_voted ? `
                    <span class="badge ${request.your_vote ? 'badge--success' : 'badge--danger'}">
                        ${request.your_vote ? 'Approved' : 'Rejected'}
                    </span>
                ` : `
                    <button class="btn btn--success btn--sm vote-approve-btn" data-id="${request.request_id}">
                        Approve
                    </button>
                    <button class="btn btn--danger btn--sm vote-reject-btn" data-id="${request.request_id}">
                        Reject
                    </button>
                `}
                <button class="btn btn--secondary btn--sm view-details-btn" data-id="${request.request_id}">
                    Details
                </button>
            </div>
        </div>
    `;
}

/**
 * Bind event handlers for request actions
 */
function bindRequestHandlers() {
    document.querySelectorAll('.vote-approve-btn').forEach(btn => {
        btn.addEventListener('click', () => handleVote(btn.dataset.id, true));
    });

    document.querySelectorAll('.vote-reject-btn').forEach(btn => {
        btn.addEventListener('click', () => handleVote(btn.dataset.id, false));
    });

    document.querySelectorAll('.view-details-btn').forEach(btn => {
        btn.addEventListener('click', () => showRequestDetails(btn.dataset.id));
    });
}

/**
 * Handle voting on a request
 * @param {string} requestId - The request ID
 * @param {boolean} approve - True to approve, false to reject
 */
async function handleVote(requestId, approve) {
    const action = approve ? 'approve' : 'reject';

    const confirmed = await modal.confirm({
        title: `${approve ? 'Approve' : 'Reject'} Request?`,
        message: approve
            ? 'This will add your approval vote. If this is the 2nd approval, the user will be added to the community.'
            : 'This will record your rejection. The request may still be approved by other admins.',
    });

    if (!confirmed) return;

    const row = document.querySelector(`[data-request-id="${requestId}"]`);
    const buttons = row?.querySelectorAll('button');
    buttons?.forEach(btn => {
        btn.disabled = true;
        if (btn.classList.contains(`vote-${action}-btn`)) {
            btn.classList.add('btn--loading');
        }
    });

    try {
        const result = await voteOnRequest(requestId, approve);

        if (result.membership_granted) {
            toast.success('Request approved! User has been added to the community.');
        } else {
            toast.success(`Vote recorded: ${approve ? 'Approved' : 'Rejected'}`);
        }

        await loadRequests();
    } catch (error) {
        console.error('Failed to vote:', error);
        toast.error(error.message || `Failed to ${action} request.`);
        buttons?.forEach(btn => {
            btn.disabled = false;
            btn.classList.remove('btn--loading');
        });
    }
}

/**
 * Show request details in a modal
 * @param {string} requestId - The request ID
 */
async function showRequestDetails(requestId) {
    try {
        const request = await getRequest(requestId);

        const votesHtml = request.votes && request.votes.length > 0
            ? request.votes.map(v => `
                <div class="vote-item">
                    <span>${escapeHtml(v.voter_name || 'Admin')}</span>
                    <span class="badge ${v.vote ? 'badge--success' : 'badge--danger'}">
                        ${v.vote ? 'Approve' : 'Reject'}
                    </span>
                    <span class="vote-item__date">${formatDate(v.voted_at)}</span>
                </div>
            `).join('')
            : '<p>No votes yet</p>';

        await modal.alert({
            title: 'Request Details',
            message: `
                <div class="request-details">
                    <div class="request-details__section">
                        <h4>User</h4>
                        <p><strong>${escapeHtml(request.user_name || request.username)}</strong></p>
                        <p>${escapeHtml(request.user_email || '')}</p>
                    </div>
                    <div class="request-details__section">
                        <h4>Community</h4>
                        <p>${escapeHtml(request.region_name)}</p>
                    </div>
                    <div class="request-details__section">
                        <h4>Status</h4>
                        <p>${request.current_votes || 0} of ${request.votes_needed || 2} approvals</p>
                    </div>
                    <div class="request-details__section">
                        <h4>Submitted</h4>
                        <p>${formatDate(request.created_at)}</p>
                    </div>
                    <div class="request-details__section">
                        <h4>Expires</h4>
                        <p>${formatDate(request.expires_at)}</p>
                    </div>
                    <div class="request-details__section">
                        <h4>Votes</h4>
                        <div class="votes-list">
                            ${votesHtml}
                        </div>
                    </div>
                </div>
            `,
        });
    } catch (error) {
        console.error('Failed to load request details:', error);
        toast.error('Failed to load request details.');
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

/**
 * Format a date string
 * @param {string} dateStr - ISO date string
 * @returns {string} Formatted date
 */
function formatDate(dateStr) {
    if (!dateStr) return '';
    const date = new Date(dateStr);
    return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
    });
}

export function cleanup() {
    currentFilter = 'all';
}

export default { render, cleanup };
