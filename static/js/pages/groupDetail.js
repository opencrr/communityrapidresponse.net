/**
 * Group detail page
 * Shows full group info with sections gated by membership/admin status.
 */

import { getGroup, listGroupMembers, listSignalGroups, listResources, listInviteLinks, leaveGroup, joinViaLink } from '../api/groups.js';
import { isAuthenticated, getUser, isSuperuser } from '../utils/store.js';
import { getPrivateKey, decryptSecret } from '../crypto/index.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';

/**
 * Render the group detail page
 * @param {HTMLElement} container - Container element
 * @param {Object} params - Route params with id
 */
export async function render(container, params) {
    const groupId = params.id;

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="loading">
                    <div class="spinner spinner--lg"></div>
                </div>
            </div>
        </div>
    `;

    try {
        const group = await getGroup(groupId);
        await renderGroupDetail(container, group);
    } catch (error) {
        console.error('Failed to load group:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">Group Not Found</h3>
                    <p class="empty-state__description">
                        The group you're looking for doesn't exist or could not be loaded.
                    </p>
                    <a href="/groups/browse" class="btn btn--primary" data-link>Browse Groups</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render the full group detail
 * @param {HTMLElement} container - Container element
 * @param {Object} group - Group data from API
 */
async function renderGroupDetail(container, group) {
    const authenticated = isAuthenticated();
    const currentUser = getUser();
    const userIsSuperuser = isSuperuser();
    const isMember = group.is_member || false;
    const isAdmin = group.is_admin || false;
    const isTrusted = group.is_trusted || false;
    const tags = group.topic_tags || [];
    const memberCount = group.member_count || 0;

    const roleBadge = isAdmin
        ? '<span class="badge badge--info">Admin</span>'
        : isTrusted
            ? '<span class="badge badge--warning">Trusted</span>'
            : isMember
                ? '<span class="badge badge--success">Member</span>'
                : '';

    const statusBadge = group.status === 'active'
        ? ''
        : `<span class="badge badge--default">${escapeHtml(group.status)}</span>`;

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="region-detail">
                    <!-- Breadcrumb -->
                    <nav class="region-detail__breadcrumb">
                        <a href="/groups/browse" data-link>Browse Groups</a>
                        &rsaquo; ${escapeHtml(group.name)}
                    </nav>

                    <!-- Header -->
                    <div class="region-detail__header">
                        <div style="display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap;">
                            <h1 class="page__title">${escapeHtml(group.name)}</h1>
                            ${roleBadge}
                            ${statusBadge}
                            ${group.is_founding_member ? '<span class="badge badge--default">Founding Member</span>' : ''}
                        </div>
                        ${group.description ? `
                            <p style="margin-top: var(--space-3); color: var(--color-gray-600);">
                                ${escapeHtml(group.description)}
                            </p>
                        ` : ''}
                    </div>

                    <!-- Tags -->
                    ${tags.length > 0 ? `
                        <div class="group-card__tags" style="margin-top: var(--space-3);">
                            ${tags.map(tag => `<span class="tag-badge">${escapeHtml(tag)}</span>`).join('')}
                        </div>
                    ` : ''}

                    <!-- Stats -->
                    <div class="region-detail__stats">
                        <div class="region-stat">
                            <div class="region-stat__value">${memberCount}</div>
                            <div class="region-stat__label">Members</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${group.visibility === 'listed' ? 'Listed' : 'Unlisted'}</div>
                            <div class="region-stat__label">Visibility</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${formatDate(group.created_at)}</div>
                            <div class="region-stat__label">Created</div>
                        </div>
                    </div>

                    <!-- Join section (non-members) -->
                    ${!isMember && authenticated ? `
                        <div class="card" style="margin-top: var(--space-4);">
                            <div class="card__body text-center">
                                <p style="color: var(--color-gray-600); margin-bottom: var(--space-3);">
                                    Have an invite link? Enter it below to join this group.
                                </p>
                                <div style="display: flex; gap: var(--space-2); max-width: 500px; margin: 0 auto;">
                                    <input type="text" class="form-input" id="invite-token-input" placeholder="Paste invite token..." style="flex: 1;">
                                    <button class="btn btn--primary" id="join-via-link-btn">Join</button>
                                </div>
                            </div>
                        </div>
                    ` : ''}

                    <!-- Signal Groups (members only, filtered by API) -->
                    ${isMember ? '<div id="signal-groups-section" class="mt-6"></div>' : ''}

                    <!-- Resources (members only, filtered by API) -->
                    ${isMember ? '<div id="resources-section" class="mt-6"></div>' : ''}

                    <!-- Members (members only) -->
                    ${isMember ? '<div id="members-section" class="mt-6"></div>' : ''}

                    <!-- Admin section -->
                    ${isAdmin ? `
                        <div class="group-detail__section" style="margin-top: var(--space-6);">
                            <h2 class="group-detail__section-title">Admin</h2>
                            <div style="display: flex; flex-wrap: wrap; gap: var(--space-3);">
                                <a href="/groups/${group.id}/manage" class="btn btn--secondary" data-link>Manage Group</a>
                            </div>
                            <div id="invite-links-section" class="mt-6"></div>
                        </div>
                    ` : ''}

                    <!-- Leave / actions -->
                    ${isMember ? `
                        <div class="mt-6" style="padding-top: var(--space-4); border-top: 1px solid var(--color-gray-200);">
                            <button class="btn btn--ghost btn--sm" id="leave-group-btn" style="color: var(--color-error);">Leave Group</button>
                        </div>
                    ` : ''}
                </div>
            </div>
        </div>
    `;

    // Bind events
    bindEvents(container, group);

    // Load dynamic sections for members
    if (isMember) {
        loadSignalGroups(group.id);
        loadResources(group.id);
        loadMembers(group.id);
    }

    // Load invite links for admins
    if (isAdmin) {
        loadInviteLinks(group.id);
    }
}

/**
 * Bind event handlers
 * @param {HTMLElement} container - Page container
 * @param {Object} group - Group data
 */
function bindEvents(container, group) {
    // Join via invite link
    const joinBtn = container.querySelector('#join-via-link-btn');
    if (joinBtn) {
        joinBtn.addEventListener('click', async () => {
            const tokenInput = container.querySelector('#invite-token-input');
            const token = tokenInput?.value?.trim();
            if (!token) {
                toast.error('Please enter an invite token');
                return;
            }
            joinBtn.disabled = true;
            joinBtn.textContent = 'Joining...';
            try {
                await joinViaLink(token);
                toast.success('You have joined the group!');
                // Re-render to show member content
                const updatedGroup = await getGroup(group.id);
                const main = document.getElementById('main');
                if (main) await renderGroupDetail(main, updatedGroup);
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to join group');
                joinBtn.disabled = false;
                joinBtn.textContent = 'Join';
            }
        });
    }

    // Leave group
    const leaveBtn = container.querySelector('#leave-group-btn');
    if (leaveBtn) {
        leaveBtn.addEventListener('click', async () => {
            const confirmed = await modal.confirm({
                title: 'Leave Group?',
                message: `Are you sure you want to leave "${group.name}"? You will lose access to group resources and signal groups.`,
            });
            if (!confirmed) return;

            try {
                await leaveGroup(group.id);
                toast.success('You have left the group');
                navigate('/groups/browse');
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to leave group');
            }
        });
    }
}

/**
 * Load and display signal groups for this group
 * @param {string} groupId - Group UUID
 */
async function loadSignalGroups(groupId) {
    const section = document.getElementById('signal-groups-section');
    if (!section) return;

    try {
        const response = await listSignalGroups(groupId);
        const signalGroups = response.signal_groups || response.groups || [];

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

        if (decryptedGroups.length === 0) {
            section.innerHTML = '';
            return;
        }

        section.innerHTML = `
            <h2 class="group-detail__section-title">Signal Groups</h2>
            <div class="dashboard-grid">
                ${decryptedGroups.map(sg => `
                    <div class="card group-card">
                        <div class="card__body">
                            <div class="group-card__name">${escapeHtml(sg.name)}</div>
                            ${sg.description ? `<p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">${escapeHtml(sg.description)}</p>` : ''}
                            ${sg.access_tier ? `<span class="tier-badge tier-badge--${sg.access_tier}">${formatAccessTier(sg.access_tier)}</span>` : ''}
                            <div class="group-invite" style="margin-top: var(--space-3);">
                                <span class="group-invite__link">${escapeHtml(sg._decryptedLink || 'Invite link hidden')}</span>
                                ${sg._decryptedLink ? `
                                    <button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(sg._decryptedLink)}">Copy</button>
                                ` : ''}
                            </div>
                        </div>
                    </div>
                `).join('')}
            </div>
        `;

        bindCopyButtons(section);
    } catch (error) {
        console.error('Failed to load signal groups:', error);
    }
}

/**
 * Load and display resources for this group
 * @param {string} groupId - Group UUID
 */
async function loadResources(groupId) {
    const section = document.getElementById('resources-section');
    if (!section) return;

    try {
        const response = await listResources(groupId);
        const resources = response.resources || [];

        if (resources.length === 0) {
            section.innerHTML = '';
            return;
        }

        section.innerHTML = `
            <h2 class="group-detail__section-title">Resources</h2>
            <div class="resource-list">
                ${resources.map(resource => `
                    <div class="resource-item">
                        <div>
                            <div class="resource-item__title">${escapeHtml(resource.title)}</div>
                            ${resource.url ? `<a href="${escapeHtml(resource.url)}" target="_blank" rel="noopener noreferrer" class="resource-item__url">${escapeHtml(resource.url)}</a>` : ''}
                            ${resource.description ? `<div class="resource-item__description">${escapeHtml(resource.description)}</div>` : ''}
                        </div>
                        ${resource.access_tier ? `<span class="tier-badge tier-badge--${resource.access_tier}">${formatAccessTier(resource.access_tier)}</span>` : ''}
                    </div>
                `).join('')}
            </div>
        `;
    } catch (error) {
        console.error('Failed to load resources:', error);
    }
}

/**
 * Load and display group members
 * @param {string} groupId - Group UUID
 */
async function loadMembers(groupId) {
    const section = document.getElementById('members-section');
    if (!section) return;

    try {
        const response = await listGroupMembers(groupId);
        const members = response.members || [];

        if (members.length === 0) {
            section.innerHTML = '';
            return;
        }

        section.innerHTML = `
            <h2 class="group-detail__section-title">Members (${members.length})</h2>
            <div class="card">
                <div class="card__body">
                    <table style="width: 100%; border-collapse: collapse; text-align: left;">
                        <thead>
                            <tr style="border-bottom: 2px solid var(--color-gray-200);">
                                <th style="padding: var(--space-2) var(--space-3);">Username</th>
                                <th style="padding: var(--space-2) var(--space-3);">Role</th>
                                <th style="padding: var(--space-2) var(--space-3);">Status</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${members.map(member => `
                                <tr style="border-bottom: 1px solid var(--color-gray-100);">
                                    <td style="padding: var(--space-2) var(--space-3);">
                                        ${escapeHtml(member.username)}
                                        ${member.is_founding_member ? '<span class="badge badge--default" style="margin-left: var(--space-1);">Founder</span>' : ''}
                                    </td>
                                    <td style="padding: var(--space-2) var(--space-3);">
                                        ${member.role === 'admin'
                                            ? '<span class="badge badge--info">Admin</span>'
                                            : member.role === 'trusted'
                                                ? '<span class="badge badge--warning">Trusted</span>'
                                                : 'Member'}
                                    </td>
                                    <td style="padding: var(--space-2) var(--space-3);">
                                        ${member.is_founding_member ? '<span class="badge badge--default">Founding</span>' : ''}
                                    </td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        `;
    } catch (error) {
        console.error('Failed to load members:', error);
    }
}

/**
 * Load and display invite links (admin only)
 * @param {string} groupId - Group UUID
 */
async function loadInviteLinks(groupId) {
    const section = document.getElementById('invite-links-section');
    if (!section) return;

    try {
        const response = await listInviteLinks(groupId);
        const links = response.invite_links || [];

        if (links.length === 0) {
            section.innerHTML = `
                <p style="color: var(--color-gray-500); font-size: var(--font-size-sm);">No invite links created yet. Create one from the manage page.</p>
            `;
            return;
        }

        section.innerHTML = `
            <h3 style="font-size: var(--font-size-lg); font-weight: 600; margin-bottom: var(--space-3);">Active Invite Links</h3>
            <div class="card">
                <div class="card__body">
                    ${links.map(link => `
                        <div style="display: flex; justify-content: space-between; align-items: center; padding: var(--space-2) 0; border-bottom: 1px solid var(--color-gray-100);">
                            <div>
                                <code style="font-size: var(--font-size-sm); background: var(--color-gray-100); padding: var(--space-1) var(--space-2); border-radius: var(--radius-sm);">${escapeHtml(link.token)}</code>
                                ${link.label ? `<span style="margin-left: var(--space-2); color: var(--color-gray-500); font-size: var(--font-size-sm);">${escapeHtml(link.label)}</span>` : ''}
                                ${link.max_uses ? `<span style="margin-left: var(--space-2); font-size: var(--font-size-xs); color: var(--color-gray-400);">${link.use_count || 0}/${link.max_uses} uses</span>` : ''}
                            </div>
                            <button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(link.token)}">Copy</button>
                        </div>
                    `).join('')}
                </div>
            </div>
        `;

        bindCopyButtons(section);
    } catch (error) {
        console.error('Failed to load invite links:', error);
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
            } catch (error) {
                toast.error('Failed to copy');
            }
        });
    });
}

/**
 * Format access tier for display
 * @param {string} tier - Access tier value
 * @returns {string} Human-readable tier
 */
function formatAccessTier(tier) {
    const labels = {
        open: 'Open',
        resident: 'Resident',
        member: 'Member',
        trusted: 'Trusted',
        admin_only: 'Admin Only',
    };
    return labels[tier] || tier;
}

/**
 * Format an ISO date string for display
 * @param {string} dateStr - ISO date string
 * @returns {string} Formatted date
 */
function formatDate(dateStr) {
    if (!dateStr) return 'N/A';
    try {
        const date = new Date(dateStr);
        return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
    } catch {
        return 'N/A';
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
