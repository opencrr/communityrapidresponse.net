/**
 * Membership Page
 * Handles user's sub-region membership requests and invitations.
 */

import {
    listMyRequests,
    cancelRequest,
    listMyInvitations,
    respondToInvitation,
} from '../api/membership.js?v=c49aed9';
import { getUser, isPostcardVerified, isVouchVerified } from '../utils/store.js?v=c49aed9';
import { ApiError } from '../api/client.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';
import modal from '../components/modal.js?v=c49aed9';
import { navigate } from '../app.js?v=c49aed9';

/**
 * Render the membership page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const user = getUser();
    const postcardVerified = isPostcardVerified();
    const vouchVerified = isVouchVerified();
    const hasAnyVerification = postcardVerified || vouchVerified;

    container.innerHTML = `
        <div class="page">
            <div class="page__container" style="max-width: 900px;">
                <div class="page__header text-center">
                    <h1 class="page__title">Community Membership</h1>
                    <p class="page__subtitle">
                        Manage your requests to join neighborhoods, city blocks, and other communities.
                    </p>
                </div>

                ${!hasAnyVerification ? `
                    <div class="privacy-notice" style="margin-bottom: var(--space-6); background: #fef3c7;">
                        <div class="privacy-notice__icon">&#x26A0;</div>
                        <div class="privacy-notice__text">
                            <strong>Verification Required</strong>
                            You need to be verified (postcard or vouch) before you can request community membership.
                            <a href="/verify" data-link style="color: var(--color-primary-600);">Start verification</a>
                        </div>
                    </div>
                ` : ''}

                <!-- Pending Invitations -->
                <div class="card" style="margin-bottom: var(--space-6);">
                    <div class="card__header">
                        <h2 class="card__title">Invitations</h2>
                    </div>
                    <div class="card__body" id="invitations-container">
                        <div class="loading">
                            <div class="spinner"></div>
                        </div>
                    </div>
                </div>

                <!-- My Requests -->
                <div class="card" style="margin-bottom: var(--space-6);">
                    <div class="card__header">
                        <h2 class="card__title">My Requests</h2>
                    </div>
                    <div class="card__body" id="requests-container">
                        <div class="loading">
                            <div class="spinner"></div>
                        </div>
                    </div>
                </div>

                <!-- How It Works -->
                <div class="card">
                    <div class="card__header">
                        <h2 class="card__title">How Community Membership Works</h2>
                    </div>
                    <div class="card__body">
                        <div class="instructions-grid">
                            <div class="instruction-step">
                                <div class="instruction-step__number">1</div>
                                <div class="instruction-step__content">
                                    <h4>Find a Community</h4>
                                    <p>Browse the <a href="/communities" data-link>communities</a> page to find neighborhoods or city blocks you want to join.</p>
                                </div>
                            </div>

                            <div class="instruction-step">
                                <div class="instruction-step__number">2</div>
                                <div class="instruction-step__content">
                                    <h4>Request Membership</h4>
                                    <p>Click "Request to Join" on a community within your verified area. You must already be a member of the parent community.</p>
                                </div>
                            </div>

                            <div class="instruction-step">
                                <div class="instruction-step__number">3</div>
                                <div class="instruction-step__content">
                                    <h4>Admin Approval</h4>
                                    <p>Community admins will review your request. At least 2 admins must approve before you're added.</p>
                                </div>
                            </div>

                            <div class="instruction-step">
                                <div class="instruction-step__number">4</div>
                                <div class="instruction-step__content">
                                    <h4>Join the Community</h4>
                                    <p>Once approved, you'll have access to the community's Signal groups and can participate in local discussions.</p>
                                </div>
                            </div>
                        </div>

                        <div class="privacy-notice" style="margin-top: var(--space-4);">
                            <div class="privacy-notice__icon">&#x1F4E8;</div>
                            <div class="privacy-notice__text">
                                <strong>Admin Invitations</strong>
                                Admins can also invite you directly to join a community. Invitations appear above and can be accepted with one click.
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Load data
    await Promise.all([
        loadInvitations(),
        loadRequests(),
    ]);
}

/**
 * Load and render pending invitations
 */
async function loadInvitations() {
    const container = document.getElementById('invitations-container');
    if (!container) return;

    try {
        const response = await listMyInvitations();
        const invitations = response.requests || [];

        if (invitations.length === 0) {
            container.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <p>No pending invitations</p>
                </div>
            `;
            return;
        }

        container.innerHTML = `
            <div class="membership-cards">
                ${invitations.map(inv => renderInvitationCard(inv)).join('')}
            </div>
        `;

        bindInvitationHandlers();
    } catch (error) {
        console.error('Failed to load invitations:', error);
        container.innerHTML = `
            <div class="form-error">Failed to load invitations. Please refresh the page.</div>
        `;
    }
}

/**
 * Render a single invitation card
 * @param {Object} invitation - Invitation data
 * @returns {string} HTML string
 */
function renderInvitationCard(invitation) {
    const expiresAt = new Date(invitation.expires_at);
    const daysLeft = Math.ceil((expiresAt - new Date()) / (1000 * 60 * 60 * 24));

    return `
        <div class="membership-card" data-invitation-id="${invitation.request_id}">
            <div class="membership-card__header">
                <span class="badge badge--invitation">Invitation</span>
                <span class="membership-card__expires">Expires in ${daysLeft} days</span>
            </div>
            <div class="membership-card__body">
                <h4 class="membership-card__title">${escapeHtml(invitation.region_name || 'Unknown Community')}</h4>
                <p class="membership-card__meta">
                    Invited by ${escapeHtml(invitation.initiated_by_name || 'Admin')}
                </p>
            </div>
            <div class="membership-card__actions">
                <button class="btn btn--primary btn--sm invitation-accept-btn" data-id="${invitation.request_id}">
                    Accept
                </button>
                <button class="btn btn--secondary btn--sm invitation-decline-btn" data-id="${invitation.request_id}">
                    Decline
                </button>
            </div>
        </div>
    `;
}

/**
 * Load and render membership requests
 */
async function loadRequests() {
    const container = document.getElementById('requests-container');
    if (!container) return;

    try {
        const response = await listMyRequests();
        const requests = (response.requests || []).filter(r => r.request_type === 'request');

        if (requests.length === 0) {
            container.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <p>No membership requests. Browse <a href="/communities" data-link>communities</a> to find communities to join.</p>
                </div>
            `;
            return;
        }

        container.innerHTML = `
            <div class="membership-cards">
                ${requests.map(req => renderRequestCard(req)).join('')}
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
 * Render a single request card
 * @param {Object} request - Request data
 * @returns {string} HTML string
 */
function renderRequestCard(request) {
    const statusBadge = getStatusBadge(request.status);
    const isPending = request.status === 'pending';
    const expiresAt = new Date(request.expires_at);
    const daysLeft = Math.ceil((expiresAt - new Date()) / (1000 * 60 * 60 * 24));

    return `
        <div class="membership-card membership-card--${request.status}" data-request-id="${request.request_id}">
            <div class="membership-card__header">
                ${statusBadge}
                ${isPending ? `<span class="membership-card__expires">Expires in ${daysLeft} days</span>` : ''}
            </div>
            <div class="membership-card__body">
                <h4 class="membership-card__title">${escapeHtml(request.region_name || 'Unknown Community')}</h4>
                <p class="membership-card__meta">
                    ${isPending ? `${request.current_votes || 0} of ${request.votes_needed || 2} admin approvals` : formatDate(request.resolved_at || request.created_at)}
                </p>
                ${isPending && request.current_votes > 0 ? `
                    <div class="vouch-progress" style="margin-top: var(--space-2);">
                        <div class="vouch-progress__bar" style="height: 4px;">
                            <div class="vouch-progress__fill" style="width: ${(request.current_votes / request.votes_needed) * 100}%"></div>
                        </div>
                    </div>
                ` : ''}
            </div>
            ${isPending ? `
                <div class="membership-card__actions">
                    <button class="btn btn--secondary btn--sm request-cancel-btn" data-id="${request.request_id}">
                        Cancel Request
                    </button>
                </div>
            ` : ''}
        </div>
    `;
}

/**
 * Get status badge HTML
 * @param {string} status - Request status
 * @returns {string} HTML string
 */
function getStatusBadge(status) {
    const badges = {
        pending: '<span class="badge badge--pending">Pending</span>',
        approved: '<span class="badge badge--success">Approved</span>',
        rejected: '<span class="badge badge--danger">Rejected</span>',
        expired: '<span class="badge badge--muted">Expired</span>',
        cancelled: '<span class="badge badge--muted">Cancelled</span>',
    };
    return badges[status] || `<span class="badge">${status}</span>`;
}

/**
 * Bind event handlers for invitation buttons
 */
function bindInvitationHandlers() {
    document.querySelectorAll('.invitation-accept-btn').forEach(btn => {
        btn.addEventListener('click', () => handleInvitationResponse(btn.dataset.id, true));
    });

    document.querySelectorAll('.invitation-decline-btn').forEach(btn => {
        btn.addEventListener('click', () => handleInvitationResponse(btn.dataset.id, false));
    });
}

/**
 * Handle invitation accept/decline
 * @param {string} invitationId - The invitation ID
 * @param {boolean} accept - True to accept, false to decline
 */
async function handleInvitationResponse(invitationId, accept) {
    const action = accept ? 'accept' : 'decline';

    if (!accept) {
        const confirmed = await modal.confirm({
            title: 'Decline Invitation?',
            message: 'Are you sure you want to decline this invitation?',
        });
        if (!confirmed) return;
    }

    const card = document.querySelector(`[data-invitation-id="${invitationId}"]`);
    const buttons = card?.querySelectorAll('button');
    buttons?.forEach(btn => {
        btn.disabled = true;
        if (btn.classList.contains(`invitation-${action}-btn`)) {
            btn.classList.add('btn--loading');
        }
    });

    try {
        await respondToInvitation(invitationId, accept);
        toast.success(accept ? 'Invitation accepted! You are now a member.' : 'Invitation declined.');
        await loadInvitations();
    } catch (error) {
        console.error('Failed to respond to invitation:', error);
        toast.error(error.message || `Failed to ${action} invitation.`);
        buttons?.forEach(btn => {
            btn.disabled = false;
            btn.classList.remove('btn--loading');
        });
    }
}

/**
 * Bind event handlers for request buttons
 */
function bindRequestHandlers() {
    document.querySelectorAll('.request-cancel-btn').forEach(btn => {
        btn.addEventListener('click', () => handleCancelRequest(btn.dataset.id));
    });
}

/**
 * Handle cancelling a request
 * @param {string} requestId - The request ID to cancel
 */
async function handleCancelRequest(requestId) {
    const confirmed = await modal.confirm({
        title: 'Cancel Request?',
        message: 'Are you sure you want to cancel this membership request?',
    });
    if (!confirmed) return;

    const btn = document.querySelector(`.request-cancel-btn[data-id="${requestId}"]`);
    if (btn) {
        btn.disabled = true;
        btn.classList.add('btn--loading');
    }

    try {
        await cancelRequest(requestId);
        toast.success('Request cancelled.');
        await loadRequests();
    } catch (error) {
        console.error('Failed to cancel request:', error);
        toast.error(error.message || 'Failed to cancel request.');
        if (btn) {
            btn.disabled = false;
            btn.classList.remove('btn--loading');
        }
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
    });
}

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
