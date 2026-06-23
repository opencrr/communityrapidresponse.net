/**
 * Group invitations page
 * Shows pending group invitations for the current user with accept/decline actions.
 */

import { listMyInvitations, respondToInvitation } from '../api/groups.js';
import toast from '../components/toast.js';

/**
 * Render the group invitations page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">Group Invitations</h1>
                    <p class="page__subtitle">Pending invitations to join groups.</p>
                </div>
                <div class="loading" id="loading"><div class="spinner"></div></div>
                <div id="invitations-list" style="display:none"></div>
                <div class="empty-state" id="empty-state" style="display:none">
                    <div class="empty-state__icon">&#x2709;</div>
                    <h3 class="empty-state__title">No Pending Invitations</h3>
                    <p class="empty-state__description">
                        You don't have any pending group invitations right now.
                    </p>
                    <a href="/groups/browse" class="btn btn--primary" data-link>Browse Groups</a>
                </div>
            </div>
        </div>
    `;

    await loadInvitations();
}

/**
 * Load invitations from the API and render them
 */
async function loadInvitations() {
    const loadingEl = document.getElementById('loading');
    const listEl = document.getElementById('invitations-list');
    const emptyEl = document.getElementById('empty-state');

    try {
        const response = await listMyInvitations();
        const invitations = response.invitations || [];

        loadingEl.style.display = 'none';

        if (invitations.length === 0) {
            emptyEl.style.display = '';
            return;
        }

        listEl.style.display = '';
        renderInvitations(invitations, listEl);
    } catch (error) {
        console.error('Failed to load invitations:', error);
        loadingEl.style.display = 'none';
        emptyEl.querySelector('.empty-state__title').textContent = 'Error Loading Invitations';
        emptyEl.querySelector('.empty-state__description').textContent =
            'Failed to load invitations. Please try again later.';
        emptyEl.style.display = '';
    }
}

/**
 * Render invitation cards
 * @param {Object[]} invitations - Array of invitation objects
 * @param {HTMLElement} listEl - Container element
 */
function renderInvitations(invitations, listEl) {
    listEl.innerHTML = invitations.map(invitation => {
        const groupName = invitation.group_name || 'Unknown Group';
        const inviterName = invitation.inviter_username || 'Someone';
        const createdAt = formatDate(invitation.created_at);

        return `
            <div class="invitation-card" data-invitation-id="${escapeHtml(invitation.id)}">
                <div>
                    <div style="font-weight: 500;">
                        <a href="/groups/${invitation.group_id}" data-link>${escapeHtml(groupName)}</a>
                    </div>
                    <div style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">
                        Invited by ${escapeHtml(inviterName)} &middot; ${createdAt}
                    </div>
                    ${invitation.message ? `<p style="margin-top: var(--space-2); color: var(--color-gray-600);">${escapeHtml(invitation.message)}</p>` : ''}
                </div>
                <div class="invitation-card__actions">
                    <button class="btn btn--primary btn--sm invitation-accept-btn" data-invitation-id="${escapeHtml(invitation.id)}">Accept</button>
                    <button class="btn btn--ghost btn--sm invitation-decline-btn" data-invitation-id="${escapeHtml(invitation.id)}">Decline</button>
                </div>
            </div>
        `;
    }).join('');

    // Bind accept buttons
    listEl.querySelectorAll('.invitation-accept-btn').forEach(btn => {
        btn.addEventListener('click', () => handleInvitationResponse(btn.dataset.invitationId, 'accept'));
    });

    // Bind decline buttons
    listEl.querySelectorAll('.invitation-decline-btn').forEach(btn => {
        btn.addEventListener('click', () => handleInvitationResponse(btn.dataset.invitationId, 'decline'));
    });
}

/**
 * Handle accept/decline on an invitation
 * @param {string} invitationId - Invitation UUID
 * @param {string} action - 'accept' or 'decline'
 */
async function handleInvitationResponse(invitationId, action) {
    const cardEl = document.querySelector(`[data-invitation-id="${invitationId}"]`);
    const buttons = cardEl?.querySelectorAll('button');
    if (buttons) buttons.forEach(btn => btn.disabled = true);

    try {
        await respondToInvitation(invitationId, { action });
        toast.success(action === 'accept' ? 'Invitation accepted! You are now a group member.' : 'Invitation declined.');

        // Remove the card
        if (cardEl) cardEl.remove();

        // Check if list is now empty
        const listEl = document.getElementById('invitations-list');
        const emptyEl = document.getElementById('empty-state');
        if (listEl && listEl.children.length === 0) {
            listEl.style.display = 'none';
            if (emptyEl) emptyEl.style.display = '';
        }
    } catch (error) {
        toast.error(error.data?.message || `Failed to ${action} invitation`);
        if (buttons) buttons.forEach(btn => btn.disabled = false);
    }
}

/**
 * Format an ISO date string for display
 * @param {string} dateStr - ISO date string
 * @returns {string} Formatted date
 */
function formatDate(dateStr) {
    if (!dateStr) return '';
    try {
        const date = new Date(dateStr);
        return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
    } catch {
        return '';
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

export function cleanup() {}

export default { render, cleanup };
