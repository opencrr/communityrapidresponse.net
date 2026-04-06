/**
 * My Groups page — login landing page
 * Shows the user's groups, pending invitations/proposals, and connections.
 */

import { listMyGroups, listMyInvitations, respondToInvitation } from '../api/groups.js';
import { listMyConnections, listPendingProposals, respondToProposal } from '../api/connections.js';
import { isPostcardVerified } from '../utils/store.js';
import toast from '../components/toast.js';

/**
 * Render the My Groups page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const showCreateButton = isPostcardVerified();

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header" style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: var(--space-3);">
                    <h1 class="page__title">My Groups</h1>
                    ${showCreateButton ? `<a href="/groups/create" class="btn btn--primary" data-link>Create Group</a>` : ''}
                </div>
                <div class="loading" id="loading"><div class="spinner"></div></div>
                <div id="pending-section" style="display:none"></div>
                <div id="groups-section" style="display:none"></div>
                <div id="connections-section" style="display:none"></div>
            </div>
        </div>
    `;

    await loadAllData();
}

/**
 * Load all data in parallel and render each section
 */
async function loadAllData() {
    const loadingEl = document.getElementById('loading');

    try {
        const [groupsResponse, invitationsResponse, connectionsResponse, proposalsResponse] = await Promise.all([
            listMyGroups(),
            listMyInvitations(),
            listMyConnections(),
            listPendingProposals(),
        ]);

        const groups = groupsResponse.groups || [];
        const invitations = invitationsResponse.invitations || [];
        const connections = connectionsResponse.connections || [];
        const proposals = proposalsResponse.proposals || [];

        loadingEl.style.display = 'none';

        renderPendingSection(invitations, proposals);
        renderGroupsSection(groups);
        renderConnectionsSection(connections);
    } catch (error) {
        console.error('Failed to load My Groups data:', error);
        loadingEl.style.display = 'none';

        const groupsSection = document.getElementById('groups-section');
        if (groupsSection) {
            groupsSection.style.display = '';
            groupsSection.innerHTML = `
                <div class="empty-state">
                    <h3 class="empty-state__title">Error Loading Data</h3>
                    <p class="empty-state__description">Failed to load your groups. Please try again later.</p>
                </div>
            `;
        }
    }
}

/**
 * Render the pending items section (invitations + proposals)
 * @param {Object[]} invitations - Group invitations
 * @param {Object[]} proposals - Connection proposals
 */
function renderPendingSection(invitations, proposals) {
    const sectionEl = document.getElementById('pending-section');
    if (!sectionEl) return;

    if (invitations.length === 0 && proposals.length === 0) return;

    sectionEl.style.display = '';
    sectionEl.innerHTML = `
        <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Pending Items</h2>
        <div id="pending-items">
            ${invitations.map(renderInvitationCard).join('')}
            ${proposals.map(renderProposalCard).join('')}
        </div>
    `;

    bindPendingActions(sectionEl);
}

/**
 * Render a single invitation card
 * @param {Object} invitation - Invitation object
 * @returns {string} HTML string
 */
function renderInvitationCard(invitation) {
    const groupName = invitation.group_name || 'Unknown Group';
    const inviterName = invitation.inviter_username || 'Someone';

    return `
        <div class="invitation-card" data-invitation-id="${escapeHtml(invitation.id)}">
            <div>
                <div style="font-weight: 500;">Invitation to "${escapeHtml(groupName)}"</div>
                <div style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">
                    from ${escapeHtml(inviterName)}
                </div>
                ${invitation.message ? `<p style="margin-top: var(--space-2); color: var(--color-gray-600);">${escapeHtml(invitation.message)}</p>` : ''}
            </div>
            <div class="invitation-card__actions">
                <button class="btn btn--primary btn--sm invitation-accept-btn" data-invitation-id="${escapeHtml(invitation.id)}">Accept</button>
                <button class="btn btn--ghost btn--sm invitation-decline-btn" data-invitation-id="${escapeHtml(invitation.id)}">Decline</button>
            </div>
        </div>
    `;
}

/**
 * Render a single proposal card
 * @param {Object} proposal - Connection proposal object
 * @returns {string} HTML string
 */
function renderProposalCard(proposal) {
    const initiatorName = proposal.initiator_group_name || 'Unknown Group';

    return `
        <div class="proposal-card" data-proposal-id="${escapeHtml(proposal.id)}">
            <div>
                <div style="font-weight: 500;">Connection proposal</div>
                <div style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">
                    from ${escapeHtml(initiatorName)}
                </div>
                ${proposal.message ? `<p style="margin-top: var(--space-2); color: var(--color-gray-600);">${escapeHtml(proposal.message)}</p>` : ''}
            </div>
            <div class="proposal-card__actions">
                <button class="btn btn--primary btn--sm proposal-accept-btn" data-proposal-id="${escapeHtml(proposal.id)}">Accept</button>
                <button class="btn btn--ghost btn--sm proposal-decline-btn" data-proposal-id="${escapeHtml(proposal.id)}">Decline</button>
            </div>
        </div>
    `;
}

/**
 * Bind accept/decline event handlers for pending items
 * @param {HTMLElement} sectionEl - The pending section element
 */
function bindPendingActions(sectionEl) {
    sectionEl.querySelectorAll('.invitation-accept-btn').forEach(btn => {
        btn.addEventListener('click', () => handleInvitationResponse(btn.dataset.invitationId, 'accept'));
    });
    sectionEl.querySelectorAll('.invitation-decline-btn').forEach(btn => {
        btn.addEventListener('click', () => handleInvitationResponse(btn.dataset.invitationId, 'decline'));
    });
    sectionEl.querySelectorAll('.proposal-accept-btn').forEach(btn => {
        btn.addEventListener('click', () => handleProposalResponse(btn.dataset.proposalId, 'accept'));
    });
    sectionEl.querySelectorAll('.proposal-decline-btn').forEach(btn => {
        btn.addEventListener('click', () => handleProposalResponse(btn.dataset.proposalId, 'decline'));
    });
}

/**
 * Handle accept/decline on an invitation
 * @param {string} invitationId - Invitation UUID
 * @param {string} action - 'accept' or 'decline'
 */
async function handleInvitationResponse(invitationId, action) {
    const cardEl = document.querySelector(`.invitation-card[data-invitation-id="${invitationId}"]`);
    const buttons = cardEl?.querySelectorAll('button');
    if (buttons) buttons.forEach(btn => btn.disabled = true);

    try {
        await respondToInvitation(invitationId, { action });
        toast.success(action === 'accept' ? 'Invitation accepted' : 'Invitation declined');

        if (cardEl) cardEl.remove();
        maybeHidePendingSection();

        // Reload groups list if accepted so the new group appears
        if (action === 'accept') {
            await reloadGroups();
        }
    } catch (error) {
        toast.error(error.data?.message || `Failed to ${action} invitation`);
        if (buttons) buttons.forEach(btn => btn.disabled = false);
    }
}

/**
 * Handle accept/decline on a connection proposal
 * @param {string} proposalId - Proposal UUID
 * @param {string} action - 'accept' or 'decline'
 */
async function handleProposalResponse(proposalId, action) {
    const cardEl = document.querySelector(`.proposal-card[data-proposal-id="${proposalId}"]`);
    const buttons = cardEl?.querySelectorAll('button');
    if (buttons) buttons.forEach(btn => btn.disabled = true);

    try {
        await respondToProposal(proposalId, { action });
        toast.success(action === 'accept' ? 'Connection accepted' : 'Proposal declined');

        if (cardEl) cardEl.remove();
        maybeHidePendingSection();

        // Reload connections if accepted so the new connection appears
        if (action === 'accept') {
            await reloadConnections();
        }
    } catch (error) {
        toast.error(error.data?.message || `Failed to ${action} proposal`);
        if (buttons) buttons.forEach(btn => btn.disabled = false);
    }
}

/**
 * Hide the pending section if no items remain
 */
function maybeHidePendingSection() {
    const pendingItems = document.getElementById('pending-items');
    const pendingSection = document.getElementById('pending-section');
    if (pendingItems && pendingSection && pendingItems.children.length === 0) {
        pendingSection.style.display = 'none';
    }
}

/**
 * Reload only the groups section after an invitation is accepted
 */
async function reloadGroups() {
    try {
        const response = await listMyGroups();
        const groups = response.groups || [];
        renderGroupsSection(groups);
    } catch (error) {
        console.error('Failed to reload groups:', error);
    }
}

/**
 * Reload only the connections section after a proposal is accepted
 */
async function reloadConnections() {
    try {
        const response = await listMyConnections();
        const connections = response.connections || [];
        renderConnectionsSection(connections);
    } catch (error) {
        console.error('Failed to reload connections:', error);
    }
}

/**
 * Render the groups section
 * @param {Object[]} groups - Array of group objects
 */
function renderGroupsSection(groups) {
    const sectionEl = document.getElementById('groups-section');
    if (!sectionEl) return;

    sectionEl.style.display = '';

    if (groups.length === 0) {
        sectionEl.innerHTML = `
            <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Your Groups</h2>
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F465;</div>
                <h3 class="empty-state__title">No Groups Yet</h3>
                <p class="empty-state__description">
                    You're not a member of any groups yet. Browse groups on the <a href="/discover" data-link>Discover</a> page.
                </p>
            </div>
        `;
        return;
    }

    sectionEl.innerHTML = `
        <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Your Groups</h2>
        <div class="group-browse__list">
            ${groups.map(renderGroupCard).join('')}
        </div>
    `;
}

/**
 * Render a single group card
 * @param {Object} group - Group object with name, role, member_count, topic_tags
 * @returns {string} HTML string
 */
function renderGroupCard(group) {
    const tags = group.topic_tags || [];
    const memberCount = group.member_count || 0;
    const roleBadge = renderRoleBadge(group.role);

    return `
        <a href="/groups/${group.id}" class="group-card" data-link data-group-id="${group.id}">
            <div class="group-card__name">${escapeHtml(group.name)}</div>
            ${roleBadge ? `<div style="margin-top: var(--space-1);">${roleBadge}</div>` : ''}
            <div class="group-card__meta">
                <span>${memberCount} member${memberCount !== 1 ? 's' : ''}</span>
            </div>
            ${tags.length > 0 ? `
                <div class="group-card__tags">
                    ${tags.map(tag => `<span class="tag-badge">${escapeHtml(tag)}</span>`).join('')}
                </div>
            ` : ''}
        </a>
    `;
}

/**
 * Render a role badge for the user's role in a group
 * @param {string} role - 'admin', 'trusted', or 'member'
 * @returns {string} HTML string for the badge, or empty string
 */
function renderRoleBadge(role) {
    const badgeMap = {
        admin: { cssClass: 'tier-badge--admin_only', label: 'admin' },
        trusted: { cssClass: 'tier-badge--trusted', label: 'trusted' },
        member: { cssClass: 'tier-badge--member', label: 'member' },
    };

    const badge = badgeMap[role];
    if (!badge) return '';

    return `<span class="tier-badge ${badge.cssClass}">${badge.label}</span>`;
}

/**
 * Render the connections section (only if connections exist)
 * @param {Object[]} connections - Array of connection objects
 */
function renderConnectionsSection(connections) {
    const sectionEl = document.getElementById('connections-section');
    if (!sectionEl) return;

    if (connections.length === 0) {
        sectionEl.style.display = 'none';
        return;
    }

    sectionEl.style.display = '';
    sectionEl.innerHTML = `
        <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Your Connections</h2>
        <div class="discovery__results">
            ${connections.map(renderConnectionCard).join('')}
        </div>
    `;
}

/**
 * Render a single connection card
 * @param {Object} connection - Connection object
 * @returns {string} HTML string
 */
function renderConnectionCard(connection) {
    const memberGroups = connection.member_groups || [];
    const connectionName = connection.name || memberGroups.map(g => g.name).join(' & ') || 'Connection';
    const groupNames = memberGroups.map(g => escapeHtml(g.name)).join(' \u00b7 ');

    return `
        <a href="/connections/${connection.id}" class="connection-card" data-link>
            <div style="font-weight: 500;">${escapeHtml(connectionName)}</div>
            ${groupNames ? `<div style="margin-top: var(--space-1); font-size: var(--font-size-sm); color: var(--color-gray-500);">${groupNames}</div>` : ''}
        </a>
    `;
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
