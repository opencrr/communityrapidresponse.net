/**
 * Connections list page
 * Shows the user's active connections and pending proposals needing response.
 */

import { listMyConnections, listPendingProposals, respondToProposal, proposeConnection } from '../api/connections.js';
import { listMyGroups, browseTopicBoard } from '../api/groups.js';
import toast from '../components/toast.js';

let adminGroups = [];

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
                <div id="topic-board-section" style="display:none">
                    <h2 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-2);">Topic Board</h2>
                    <p style="color: var(--color-gray-600); margin-bottom: var(--space-3);">Discover groups in other regions. Only visible to group admins.</p>
                    <div class="discovery__controls">
                        <select id="topic-group-select" class="form-input"></select>
                        <input type="text" id="topic-tag-search" class="form-input" placeholder="Filter by topic (e.g., mutual-aid)" />
                        <button class="btn btn--primary" id="topic-search-btn">Filter</button>
                    </div>
                    <div class="loading" id="topic-loading" style="display:none"><div class="spinner"></div></div>
                    <div id="topic-results" class="discovery__results"></div>
                    <div class="empty-state" id="topic-empty" style="display:none">
                        <div class="empty-state__icon">&#x1F50D;</div>
                        <h3 class="empty-state__title">No Postings Found</h3>
                        <p class="empty-state__description">No groups have posted to the topic board yet, or all are filtered out.</p>
                    </div>
                </div>

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
    await loadTopicBoard();
    bindTopicBoardEvents();
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

// ---------------------------------------------------------------------------
// Topic Board (admin of active group only)
// ---------------------------------------------------------------------------

async function loadTopicBoard() {
    try {
        const response = await listMyGroups();
        const groups = response.groups || [];
        adminGroups = groups.filter(g => g.is_user_admin && g.status === 'active');

        if (adminGroups.length === 0) return;

        const sectionEl = document.getElementById('topic-board-section');
        if (sectionEl) sectionEl.style.display = '';

        const selectEl = document.getElementById('topic-group-select');
        if (!selectEl) return;

        selectEl.innerHTML = adminGroups.map((group, i) =>
            `<option value="${escapeHtml(group.id)}" ${i === 0 ? 'selected' : ''}>${escapeHtml(group.name)}</option>`
        ).join('');

        await performTopicSearch();
    } catch (error) {
        console.error('Failed to load admin groups for topic board:', error);
    }
}

function bindTopicBoardEvents() {
    const searchBtn = document.getElementById('topic-search-btn');
    const groupSelect = document.getElementById('topic-group-select');
    const tagSearch = document.getElementById('topic-tag-search');

    if (groupSelect) {
        groupSelect.addEventListener('change', () => {
            if (groupSelect.value) performTopicSearch();
        });
    }

    if (searchBtn) {
        searchBtn.addEventListener('click', performTopicSearch);
    }

    if (tagSearch) {
        tagSearch.addEventListener('keydown', (event) => {
            if (event.key === 'Enter' && groupSelect?.value) performTopicSearch();
        });
    }
}

async function performTopicSearch() {
    const groupSelect = document.getElementById('topic-group-select');
    const tagSearch = document.getElementById('topic-tag-search');
    const resultsEl = document.getElementById('topic-results');
    const emptyEl = document.getElementById('topic-empty');
    const loadingEl = document.getElementById('topic-loading');

    const selectedGroupId = groupSelect?.value;
    const tagQuery = tagSearch?.value?.trim();

    if (!selectedGroupId) {
        toast.error('Please select one of your groups first');
        return;
    }

    if (loadingEl) loadingEl.style.display = '';
    if (resultsEl) resultsEl.innerHTML = '';
    if (emptyEl) emptyEl.style.display = 'none';

    try {
        const params = { group_id: selectedGroupId, limit: 50 };
        if (tagQuery) params.tag = tagQuery;

        const response = await browseTopicBoard(params);
        const postings = response.postings || [];

        if (loadingEl) loadingEl.style.display = 'none';

        if (postings.length === 0) {
            if (emptyEl) emptyEl.style.display = '';
            return;
        }

        resultsEl.innerHTML = postings.map(posting => {
            const tags = posting.tags || [];
            return `
                <div class="posting-card">
                    <div class="posting-card__region">${escapeHtml(posting.region_label)}</div>
                    <p class="posting-card__description">${escapeHtml(posting.description)}</p>
                    <div class="group-card__tags">
                        ${tags.map(tag => `<span class="tag-badge">${escapeHtml(tag)}</span>`).join('')}
                    </div>
                    <div class="posting-card__actions">
                        <button class="btn btn--primary btn--sm request-connection-btn"
                            data-posting-group-id="${escapeHtml(posting.group_id)}"
                            data-from-group-id="${escapeHtml(selectedGroupId)}">
                            Request Connection
                        </button>
                    </div>
                </div>
            `;
        }).join('');

        resultsEl.querySelectorAll('.request-connection-btn').forEach(btn => {
            btn.addEventListener('click', async () => {
                const targetGroupId = btn.dataset.postingGroupId;
                const fromGroupId = btn.dataset.fromGroupId;
                if (!confirm('Request a connection with this group?')) return;
                btn.disabled = true;
                try {
                    await proposeConnection({ name: '', group_ids: [targetGroupId] });
                    toast.success('Connection request sent!');
                    btn.textContent = 'Request Sent';
                } catch (error) {
                    toast.error(error.data?.message || 'Failed to send connection request');
                    btn.disabled = false;
                }
            });
        });
    } catch (error) {
        console.error('Topic board search failed:', error);
        if (loadingEl) loadingEl.style.display = 'none';
        toast.error(error.data?.message || 'Search failed');
    }
}

export function cleanup() {
    adminGroups = [];
}

export default { render, cleanup };
