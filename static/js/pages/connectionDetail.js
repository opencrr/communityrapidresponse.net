/**
 * Connection detail page
 * Shows full connection info: member groups, signal chats, shared resources,
 * pending chat proposals, and admin actions.
 */

import {
    getConnection,
    listConnectionSignalGroups,
    listConnectionResources,
    listChatProposals,
    inviteToConnection,
    proposeSignalChat,
    voteOnChatProposal,
    shareResource,
    leaveConnection,
} from '../api/connections.js';
import { listResources, listMyGroups } from '../api/groups.js';
import { getPrivateKey, decryptSecret } from '../crypto/index.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';

/**
 * Render the connection detail page
 * @param {HTMLElement} container - Container element
 * @param {Object} params - Route params with id
 */
export async function render(container, params) {
    const connectionId = params.id;

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="loading"><div class="spinner spinner--lg"></div></div>
            </div>
        </div>
    `;

    try {
        const connection = await getConnection(connectionId);
        await renderConnectionDetail(container, connection);
    } catch (error) {
        console.error('Failed to load connection:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">Connection Not Found</h3>
                    <p class="empty-state__description">
                        This connection doesn't exist or you don't have access.
                    </p>
                    <a href="/connections" class="btn btn--primary" data-link>Back to Connections</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render the full connection detail
 * @param {HTMLElement} container - Container element
 * @param {Object} connection - Connection data from API
 */
async function renderConnectionDetail(container, connection) {
    const memberGroups = connection.member_groups || [];
    const connectionName = connection.name || memberGroups.map(g => g.name).join(' & ') || 'Connection';

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="region-detail">
                    <!-- Breadcrumb -->
                    <nav class="region-detail__breadcrumb">
                        <a href="/connections" data-link>Connections</a>
                        &rsaquo; ${escapeHtml(connectionName)}
                    </nav>

                    <!-- Header -->
                    <div class="region-detail__header">
                        <h1 class="page__title">${escapeHtml(connectionName)}</h1>
                        <div style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-500);">
                            Connected ${formatDate(connection.created_at)}
                        </div>
                    </div>

                    <!-- Member Groups -->
                    <div class="connection-detail__section">
                        <h2 class="connection-detail__section-title">Member Groups</h2>
                        <div class="discovery__results">
                            ${memberGroups.map(group => `
                                <a href="/groups/${group.id}" class="connection-card" data-link>
                                    <div style="font-weight: 500;">${escapeHtml(group.name)}</div>
                                    ${group.region_label ? `<div style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">${escapeHtml(group.region_label)}</div>` : ''}
                                </a>
                            `).join('')}
                        </div>
                    </div>

                    <!-- Signal Chats -->
                    <div class="connection-detail__section" id="signal-chats-section">
                        <h2 class="connection-detail__section-title">Signal Chats</h2>
                        <div class="loading" id="signal-loading"><div class="spinner"></div></div>
                    </div>

                    <!-- Shared Resources -->
                    <div class="connection-detail__section" id="resources-section">
                        <h2 class="connection-detail__section-title">Shared Resources</h2>
                        <div class="loading" id="resources-loading"><div class="spinner"></div></div>
                    </div>

                    <!-- Pending Chat Proposals -->
                    <div class="connection-detail__section" id="chat-proposals-section" style="display:none">
                        <h2 class="connection-detail__section-title">Pending Chat Proposals</h2>
                        <div id="chat-proposals-list"></div>
                    </div>

                    <!-- Actions -->
                    <div class="connection-detail__section">
                        <h2 class="connection-detail__section-title">Actions</h2>
                        <div style="display: flex; flex-wrap: wrap; gap: var(--space-3);">
                            <button class="btn btn--secondary btn--sm" id="invite-group-btn">Invite Group</button>
                            <button class="btn btn--secondary btn--sm" id="propose-chat-btn">Propose Signal Chat</button>
                            <button class="btn btn--secondary btn--sm" id="share-resource-btn">Share Resource</button>
                            <button class="btn btn--ghost btn--sm" id="leave-connection-btn" style="color: var(--color-error);">Leave Connection</button>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Load dynamic sections in parallel
    loadSignalChats(connection.id);
    loadResources(connection.id);
    loadChatProposals(connection.id);

    // Bind action buttons
    bindActions(connection);
}

/**
 * Load and render signal chats for this connection
 * @param {string} connectionId - Connection UUID
 */
async function loadSignalChats(connectionId) {
    const section = document.getElementById('signal-chats-section');
    const loadingEl = document.getElementById('signal-loading');
    if (!section) return;

    try {
        const response = await listConnectionSignalGroups(connectionId);
        const signalGroups = response.signal_groups || [];

        // Decrypt invite links
        const privateKey = await getPrivateKey();
        const decryptedGroups = await Promise.all(signalGroups.map(async (sg) => {
            let link = null;
            const secret = sg.encrypted_secret;
            if (secret && secret.encrypted_payload && secret.wrapped_dek && privateKey) {
                try {
                    link = await decryptSecret(secret.encrypted_payload, secret.encryption_iv, secret.wrapped_dek, privateKey);
                } catch (e) { /* decryption failed */ }
            }
            return { ...sg, _decryptedLink: link };
        }));

        if (loadingEl) loadingEl.remove();

        if (decryptedGroups.length === 0) {
            section.innerHTML += '<p style="color: var(--color-gray-500); font-size: var(--font-size-sm);">No signal chats yet.</p>';
            return;
        }

        const listHtml = decryptedGroups.map(sg => `
            <div class="card" style="margin-bottom: var(--space-3);">
                <div class="card__body">
                    <div style="display: flex; justify-content: space-between; align-items: start; flex-wrap: wrap; gap: var(--space-2);">
                        <div>
                            <div style="font-weight: 500;">${escapeHtml(sg.name)}</div>
                            ${sg.description ? `<p style="font-size: var(--font-size-sm); color: var(--color-gray-600); margin-top: var(--space-1);">${escapeHtml(sg.description)}</p>` : ''}
                        </div>
                        ${sg.access_level ? `<span class="visibility-badge visibility-badge--${sg.access_level}">${formatAccessLevel(sg.access_level)}</span>` : ''}
                    </div>
                    <div class="group-invite" style="margin-top: var(--space-3);">
                        <span class="group-invite__link">${escapeHtml(sg._decryptedLink || 'Invite link hidden')}</span>
                        ${sg._decryptedLink ? `<button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(sg._decryptedLink)}">Copy</button>` : ''}
                    </div>
                </div>
            </div>
        `).join('');

        section.innerHTML = `<h2 class="connection-detail__section-title">Signal Chats</h2>` + listHtml;
        bindCopyButtons(section);
    } catch (error) {
        console.error('Failed to load signal chats:', error);
        if (loadingEl) loadingEl.remove();
    }
}

/**
 * Load and render shared resources for this connection
 * @param {string} connectionId - Connection UUID
 */
async function loadResources(connectionId) {
    const section = document.getElementById('resources-section');
    const loadingEl = document.getElementById('resources-loading');
    if (!section) return;

    try {
        const response = await listConnectionResources(connectionId);
        const resources = response.shared_resources || [];

        if (loadingEl) loadingEl.remove();

        if (resources.length === 0) {
            section.innerHTML += '<p style="color: var(--color-gray-500); font-size: var(--font-size-sm);">No shared resources yet.</p>';
            return;
        }

        const listHtml = resources.map(resource => `
            <div class="resource-item">
                <div>
                    <div class="resource-item__title">${escapeHtml(resource.title)}</div>
                    ${resource.url ? (safeUrl(resource.url)
                        ? `<a href="${escapeHtml(safeUrl(resource.url))}" target="_blank" rel="noopener noreferrer" class="resource-item__url">${escapeHtml(resource.url)}</a>`
                        : `<span class="resource-item__url">${escapeHtml(resource.url)}</span>`) : ''}
                    ${resource.description ? `<div class="resource-item__description">${escapeHtml(resource.description)}</div>` : ''}
                </div>
                ${resource.visibility ? `<span class="visibility-badge visibility-badge--${resource.visibility}">${formatVisibility(resource.visibility)}</span>` : ''}
            </div>
        `).join('');

        section.innerHTML = `<h2 class="connection-detail__section-title">Shared Resources</h2><div class="resource-list">${listHtml}</div>`;
    } catch (error) {
        console.error('Failed to load resources:', error);
        if (loadingEl) loadingEl.remove();
    }
}

/**
 * Load and render pending chat proposals
 * @param {string} connectionId - Connection UUID
 */
async function loadChatProposals(connectionId) {
    const section = document.getElementById('chat-proposals-section');
    const listEl = document.getElementById('chat-proposals-list');
    if (!section || !listEl) return;

    try {
        const response = await listChatProposals(connectionId);
        const proposals = response.proposals || [];

        if (proposals.length === 0) return;

        section.style.display = '';
        listEl.innerHTML = proposals.map(proposal => `
            <div class="proposal-card" data-proposal-id="${escapeHtml(proposal.id)}">
                <div>
                    <strong>${escapeHtml(proposal.name || 'Unnamed Chat')}</strong>
                    ${proposal.description ? `<p style="margin-top: var(--space-1); color: var(--color-gray-600); font-size: var(--font-size-sm);">${escapeHtml(proposal.description)}</p>` : ''}
                    <div style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-500);">
                        Proposed by ${escapeHtml(proposal.proposer_group_name || 'Unknown')} &middot; ${formatDate(proposal.created_at)}
                    </div>
                </div>
                <div class="proposal-card__actions">
                    <button class="btn btn--primary btn--sm chat-vote-approve" data-proposal-id="${escapeHtml(proposal.id)}">Approve</button>
                    <button class="btn btn--ghost btn--sm chat-vote-reject" data-proposal-id="${escapeHtml(proposal.id)}">Reject</button>
                </div>
            </div>
        `).join('');

        listEl.querySelectorAll('.chat-vote-approve').forEach(btn => {
            btn.addEventListener('click', () => handleChatVote(btn.dataset.proposalId, 'approve'));
        });

        listEl.querySelectorAll('.chat-vote-reject').forEach(btn => {
            btn.addEventListener('click', () => handleChatVote(btn.dataset.proposalId, 'reject'));
        });
    } catch (error) {
        console.error('Failed to load chat proposals:', error);
    }
}

/**
 * Handle voting on a chat proposal
 * @param {string} proposalId - Proposal UUID
 * @param {string} vote - 'approve' or 'reject'
 */
async function handleChatVote(proposalId, vote) {
    try {
        await voteOnChatProposal(proposalId, { vote });
        toast.success(`Chat proposal ${vote}d`);

        const cardEl = document.querySelector(`[data-proposal-id="${proposalId}"]`);
        if (cardEl) cardEl.remove();
    } catch (error) {
        toast.error(error.data?.message || `Failed to ${vote} proposal`);
    }
}

/**
 * Bind action button handlers
 * @param {Object} connection - Connection data
 */
function bindActions(connection) {
    // Invite group
    const inviteBtn = document.getElementById('invite-group-btn');
    if (inviteBtn) {
        inviteBtn.addEventListener('click', async () => {
            const groupId = prompt('Enter the group ID to invite:');
            if (!groupId?.trim()) return;

            try {
                await inviteToConnection(connection.id, { group_id: groupId.trim() });
                toast.success('Group invitation sent');
            } catch (error) {
                toast.error(error.data?.message || 'Failed to send invitation');
            }
        });
    }

    // Propose signal chat
    const proposeChatBtn = document.getElementById('propose-chat-btn');
    if (proposeChatBtn) {
        proposeChatBtn.addEventListener('click', async () => {
            const chatName = prompt('Signal chat name:');
            if (!chatName?.trim()) return;

            const chatDescription = prompt('Description (optional):') || '';

            try {
                await proposeSignalChat(connection.id, {
                    name: chatName.trim(),
                    description: chatDescription.trim(),
                });
                toast.success('Signal chat proposed. Other group admins must approve.');
            } catch (error) {
                toast.error(error.data?.message || 'Failed to propose signal chat');
            }
        });
    }

    // Share resource
    const shareBtn = document.getElementById('share-resource-btn');
    if (shareBtn) {
        shareBtn.addEventListener('click', async () => {
            // Load user's groups to pick which group's resources to share
            try {
                const myGroupsResponse = await listMyGroups();
                const myGroups = (myGroupsResponse.groups || []).filter(g => g.is_admin);

                if (myGroups.length === 0) {
                    toast.error('You are not an admin of any groups');
                    return;
                }

                // For simplicity, pick first admin group or let user choose
                const groupOptions = myGroups.map(g => g.name).join(', ');
                const groupIndex = myGroups.length === 1
                    ? 0
                    : parseInt(prompt(`Which group? Enter number (1-${myGroups.length}):\n${myGroups.map((g, i) => `${i + 1}. ${g.name}`).join('\n')}`), 10) - 1;

                if (isNaN(groupIndex) || groupIndex < 0 || groupIndex >= myGroups.length) return;

                const selectedGroup = myGroups[groupIndex];
                const resourcesResponse = await listResources(selectedGroup.id);
                const resources = resourcesResponse.resources || [];

                if (resources.length === 0) {
                    toast.error('No resources in this group to share');
                    return;
                }

                const resourceIndex = resources.length === 1
                    ? 0
                    : parseInt(prompt(`Which resource? Enter number (1-${resources.length}):\n${resources.map((r, i) => `${i + 1}. ${r.title}`).join('\n')}`), 10) - 1;

                if (isNaN(resourceIndex) || resourceIndex < 0 || resourceIndex >= resources.length) return;

                const selectedResource = resources[resourceIndex];
                await shareResource(connection.id, {
                    group_id: selectedGroup.id,
                    resource_id: selectedResource.id,
                });
                toast.success('Resource shared with connection');

                // Reload resources section
                loadResources(connection.id);
            } catch (error) {
                toast.error(error.data?.message || 'Failed to share resource');
            }
        });
    }

    // Leave connection
    const leaveBtn = document.getElementById('leave-connection-btn');
    if (leaveBtn) {
        leaveBtn.addEventListener('click', async () => {
            const confirmed = await modal.confirm({
                title: 'Leave Connection?',
                message: 'Your group will be removed from this connection. This cannot be undone.',
            });
            if (!confirmed) return;

            try {
                await leaveConnection(connection.id);
                toast.success('You have left the connection');
                navigate('/connections');
            } catch (error) {
                toast.error(error.data?.message || 'Failed to leave connection');
            }
        });
    }
}

/**
 * Bind copy button handlers within a container
 * @param {HTMLElement} container - Container with copy buttons
 */
function bindCopyButtons(container) {
    container.querySelectorAll('.copy-link-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
            const link = btn.dataset.link;
            try {
                await navigator.clipboard.writeText(link);
                toast.success('Copied to clipboard');
            } catch {
                toast.error('Failed to copy');
            }
        });
    });
}

/**
 * Format access level for display
 * @param {string} level - Access level value
 * @returns {string} Human-readable label
 */
function formatAccessLevel(level) {
    const labels = {
        admin_only: 'Admin Only',
        all_members: 'All Members',
    };
    return labels[level] || level;
}

/**
 * Format visibility for display
 * @param {string} visibility - Visibility value
 * @returns {string} Human-readable label
 */
function formatVisibility(visibility) {
    const labels = {
        admin_only: 'Admin Only',
        all_members: 'All Members',
    };
    return labels[visibility] || visibility;
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

// Return the URL only if it is an http(s) URL, else '' — defense-in-depth
// against javascript:/data: scheme XSS when a stored URL is rendered into href.
function safeUrl(rawUrl) {
    if (!rawUrl) return '';
    try {
        const parsed = new URL(rawUrl, window.location.origin);
        if (parsed.protocol === 'http:' || parsed.protocol === 'https:') return rawUrl;
    } catch (e) { /* malformed URL */ }
    return '';
}

export function cleanup() {}

export default { render, cleanup };
