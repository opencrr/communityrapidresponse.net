/**
 * Connections list page
 * Shows the user's active connections and pending proposals needing response.
 */

import { listMyConnections, listPendingProposals, respondToProposal } from '../api/connections.js';
import toast from '../components/toast.js';

/**
 * Render the connections list page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">My Connections</h1>
                    <p class="page__subtitle">Inter-group connections for collaboration across regions.</p>
                </div>
                <div class="loading" id="loading"><div class="spinner"></div></div>
                <div id="proposals-section" style="display:none"></div>
                <div id="connections-list" style="display:none"></div>
                <div class="empty-state" id="empty-state" style="display:none">
                    <div class="empty-state__icon">&#x1F517;</div>
                    <h3 class="empty-state__title">No Connections Yet</h3>
                    <p class="empty-state__description">
                        Use the <a href="/discover" data-link>Discover</a> page to find groups working on similar topics.
                    </p>
                </div>
            </div>
        </div>
    `;

    await loadData();
}

/**
 * Load connections and pending proposals
 */
async function loadData() {
    const loadingEl = document.getElementById('loading');
    const proposalsSection = document.getElementById('proposals-section');
    const connectionsListEl = document.getElementById('connections-list');
    const emptyEl = document.getElementById('empty-state');

    try {
        const [connectionsResponse, proposalsResponse] = await Promise.all([
            listMyConnections(),
            listPendingProposals(),
        ]);

        const connections = connectionsResponse.connections || [];
        const proposals = proposalsResponse.proposals || [];

        loadingEl.style.display = 'none';

        // Render pending proposals
        if (proposals.length > 0) {
            proposalsSection.style.display = '';
            renderProposals(proposals, proposalsSection);
        }

        // Render connections
        if (connections.length > 0) {
            connectionsListEl.style.display = '';
            renderConnections(connections, connectionsListEl);
        } else if (proposals.length === 0) {
            emptyEl.style.display = '';
        }
    } catch (error) {
        console.error('Failed to load connections:', error);
        loadingEl.style.display = 'none';
        emptyEl.querySelector('.empty-state__title').textContent = 'Error Loading Connections';
        emptyEl.querySelector('.empty-state__description').textContent =
            'Failed to load connections. Please try again later.';
        emptyEl.style.display = '';
    }
}

/**
 * Render pending proposals that need a response
 * @param {Object[]} proposals - Array of proposal objects
 * @param {HTMLElement} sectionEl - Container element
 */
function renderProposals(proposals, sectionEl) {
    sectionEl.innerHTML = `
        <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Pending Proposals</h2>
        ${proposals.map(proposal => {
            const initiatorName = proposal.initiator_group_name || 'Unknown Group';
            const targetName = proposal.target_group_name || 'Unknown Group';
            const createdAt = formatDate(proposal.created_at);

            return `
                <div class="proposal-card" data-proposal-id="${escapeHtml(proposal.id)}">
                    <div>
                        <strong>${escapeHtml(initiatorName)}</strong> wants to connect with <strong>${escapeHtml(targetName)}</strong>
                        ${proposal.message ? `<p style="margin-top: var(--space-2); color: var(--color-gray-600);">${escapeHtml(proposal.message)}</p>` : ''}
                        <div style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-500);">${createdAt}</div>
                    </div>
                    <div class="proposal-card__actions">
                        <button class="btn btn--primary btn--sm proposal-accept-btn" data-proposal-id="${escapeHtml(proposal.id)}">Accept</button>
                        <button class="btn btn--ghost btn--sm proposal-decline-btn" data-proposal-id="${escapeHtml(proposal.id)}">Decline</button>
                    </div>
                </div>
            `;
        }).join('')}
    `;

    // Bind proposal buttons
    sectionEl.querySelectorAll('.proposal-accept-btn').forEach(btn => {
        btn.addEventListener('click', () => handleProposalResponse(btn.dataset.proposalId, 'accept'));
    });

    sectionEl.querySelectorAll('.proposal-decline-btn').forEach(btn => {
        btn.addEventListener('click', () => handleProposalResponse(btn.dataset.proposalId, 'decline'));
    });
}

/**
 * Handle accept/decline on a proposal
 * @param {string} proposalId - Proposal UUID
 * @param {string} action - 'accept' or 'decline'
 */
async function handleProposalResponse(proposalId, action) {
    const cardEl = document.querySelector(`[data-proposal-id="${proposalId}"]`);
    const buttons = cardEl?.querySelectorAll('button');
    if (buttons) buttons.forEach(btn => btn.disabled = true);

    try {
        await respondToProposal(proposalId, { action });
        toast.success(action === 'accept' ? 'Connection accepted' : 'Proposal declined');

        // Remove the card
        if (cardEl) cardEl.remove();

        // If accepted, reload to show new connection
        if (action === 'accept') {
            await loadData();
        }
    } catch (error) {
        toast.error(error.data?.message || `Failed to ${action} proposal`);
        if (buttons) buttons.forEach(btn => btn.disabled = false);
    }
}

/**
 * Render connection cards
 * @param {Object[]} connections - Array of connection objects
 * @param {HTMLElement} listEl - Container element
 */
function renderConnections(connections, listEl) {
    listEl.innerHTML = `
        <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Active Connections</h2>
        <div class="discovery__results">
            ${connections.map(connection => {
                const memberGroups = connection.member_groups || [];
                const connectionName = connection.name || memberGroups.map(g => g.name).join(' & ') || 'Connection';
                const createdAt = formatDate(connection.created_at);

                return `
                    <a href="/connections/${connection.id}" class="connection-card" data-link>
                        <div style="font-weight: 500;">${escapeHtml(connectionName)}</div>
                        <div class="connection-card__groups">
                            ${memberGroups.map(group =>
                                `<span class="tag-badge">${escapeHtml(group.name)}</span>`
                            ).join('')}
                        </div>
                        <div style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-500);">
                            Connected ${createdAt}
                        </div>
                    </a>
                `;
            }).join('')}
        </div>
    `;
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
