/**
 * Group management page (admin only)
 * Provides settings editing, invite link management, invitations,
 * topic board management, and blocked groups.
 */

import {
    getGroup,
    updateGroup,
    deleteGroup,
    createInviteLink,
    listInviteLinks,
    createInvitation,
    createOrUpdatePosting,
    getPosting,
    removePosting,
    blockGroup,
    listBlockedGroups,
    unblockGroup,
} from '../api/groups.js';
import { isSuperuser } from '../utils/store.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';

let currentGroup = null;

/**
 * Render the group management page
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
        currentGroup = group;

        if (!group.is_user_admin) {
            container.innerHTML = `
                <div class="page page--centered">
                    <div class="empty-state">
                        <div class="empty-state__icon">&#x1F512;</div>
                        <h3 class="empty-state__title">Admin Access Required</h3>
                        <p class="empty-state__description">You must be a group admin to manage this group.</p>
                        <a href="/groups/${groupId}" class="btn btn--primary" data-link>Back to Group</a>
                    </div>
                </div>
            `;
            return;
        }

        renderManagePage(container, group);
    } catch (error) {
        console.error('Failed to load group:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">Group Not Found</h3>
                    <p class="empty-state__description">Could not load group settings.</p>
                    <a href="/groups/browse" class="btn btn--primary" data-link>Browse Groups</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render the management page content with tabs
 * @param {HTMLElement} container - Container element
 * @param {Object} group - Group data
 */
function renderManagePage(container, group) {
    const userIsSuperuser = isSuperuser();

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <nav class="region-detail__breadcrumb">
                    <a href="/groups/browse" data-link>Browse Groups</a>
                    &rsaquo; <a href="/groups/${group.id}" data-link>${escapeHtml(group.name)}</a>
                    &rsaquo; Manage
                </nav>

                <h1 class="page__title" style="margin-top: var(--space-4);">Manage: ${escapeHtml(group.name)}</h1>

                <div class="tabs" style="margin-top: var(--space-6);">
                    <button class="tab tab--active" data-tab="settings">Settings</button>
                    <button class="tab" data-tab="invite-links">Invite Links</button>
                    <button class="tab" data-tab="invitations">Invitations</button>
                    <button class="tab" data-tab="topic-board">Topic Board</button>
                    <button class="tab" data-tab="blocked">Blocked Groups</button>
                </div>

                <div id="tab-content" class="mt-6">
                    <div id="tab-settings" class="manage-tab-content"></div>
                    <div id="tab-invite-links" class="manage-tab-content" style="display:none"></div>
                    <div id="tab-invitations" class="manage-tab-content" style="display:none"></div>
                    <div id="tab-topic-board" class="manage-tab-content" style="display:none"></div>
                    <div id="tab-blocked" class="manage-tab-content" style="display:none"></div>
                </div>

                ${userIsSuperuser ? `
                    <div class="mt-6" style="padding-top: var(--space-4); border-top: 1px solid var(--color-gray-200);">
                        <button class="btn btn--danger" id="delete-group-btn">Delete Group</button>
                    </div>
                ` : ''}
            </div>
        </div>
    `;

    // Initialize tabs
    bindTabSwitching();

    // Load initial tab
    renderSettingsTab(group);
    loadInviteLinksTab(group.id);
    renderInvitationsTab(group.id);
    loadTopicBoardTab(group.id);
    loadBlockedGroupsTab(group.id);

    // Delete handler (superuser only)
    const deleteBtn = container.querySelector('#delete-group-btn');
    if (deleteBtn) {
        deleteBtn.addEventListener('click', () => handleDeleteGroup(group));
    }
}

/**
 * Bind tab switching behavior
 */
function bindTabSwitching() {
    const tabs = document.querySelectorAll('.tab[data-tab]');
    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            // Deactivate all tabs
            tabs.forEach(t => t.classList.remove('tab--active'));
            document.querySelectorAll('.manage-tab-content').forEach(c => c.style.display = 'none');

            // Activate clicked tab
            tab.classList.add('tab--active');
            const tabId = tab.dataset.tab;
            const content = document.getElementById(`tab-${tabId}`);
            if (content) content.style.display = '';
        });
    });
}

/**
 * Render the settings tab
 * @param {Object} group - Group data
 */
function renderSettingsTab(group) {
    const section = document.getElementById('tab-settings');
    if (!section) return;

    section.innerHTML = `
        <form id="settings-form">
            <div class="form-group">
                <label class="form-label" for="edit-name">Group Name</label>
                <input type="text" class="form-input" id="edit-name" value="${escapeHtml(group.name)}" required minlength="3" maxlength="100">
            </div>
            <div class="form-group">
                <label class="form-label" for="edit-description">Description</label>
                <textarea class="form-input" id="edit-description" rows="4" maxlength="2000">${escapeHtml(group.description || '')}</textarea>
            </div>
            <div class="form-group">
                <label class="form-label">Visibility</label>
                <div style="display: flex; gap: var(--space-4); margin-top: var(--space-2);">
                    <label style="display: flex; align-items: center; gap: var(--space-2); cursor: pointer;">
                        <input type="radio" name="edit-visibility" value="listed" ${group.visibility === 'listed' ? 'checked' : ''}>
                        <span>Listed</span>
                    </label>
                    <label style="display: flex; align-items: center; gap: var(--space-2); cursor: pointer;">
                        <input type="radio" name="edit-visibility" value="unlisted" ${group.visibility === 'unlisted' ? 'checked' : ''}>
                        <span>Unlisted</span>
                    </label>
                </div>
            </div>
            <div class="form-group">
                <label style="display: flex; align-items: center; gap: var(--space-2); cursor: pointer;">
                    <input type="checkbox" id="edit-discoverable" ${group.discoverable_by_unverified ? 'checked' : ''}>
                    <span class="form-label" style="margin-bottom: 0;">Discoverable by unverified users</span>
                </label>
                <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">
                    When enabled, unverified users can see this group in browse results (but cannot join).
                </p>
            </div>
            <div class="form-group">
                <label class="form-label" for="edit-tags">Topic Tags</label>
                <input type="text" class="form-input" id="edit-tags" value="${escapeHtml((group.topic_tags || []).join(', '))}" placeholder="comma-separated tags">
            </div>
            <div class="form-actions" style="display: flex; gap: var(--space-3); justify-content: flex-end;">
                <button type="submit" class="btn btn--primary" id="save-settings-btn">Save Changes</button>
            </div>
        </form>
    `;

    const form = document.getElementById('settings-form');
    if (form) {
        form.addEventListener('submit', async (event) => {
            event.preventDefault();
            const saveBtn = document.getElementById('save-settings-btn');
            saveBtn.disabled = true;
            saveBtn.textContent = 'Saving...';

            const name = document.getElementById('edit-name')?.value?.trim();
            const description = document.getElementById('edit-description')?.value?.trim();
            const visibility = form.querySelector('input[name="edit-visibility"]:checked')?.value;
            const discoverableByUnverified = document.getElementById('edit-discoverable')?.checked;
            const tagsValue = document.getElementById('edit-tags')?.value;
            const topicTags = tagsValue ? tagsValue.split(',').map(t => t.trim()).filter(Boolean) : [];

            try {
                const updateData = { name, visibility, discoverable_by_unverified: discoverableByUnverified, topic_tags: topicTags };
                if (description !== undefined) updateData.description = description;

                await updateGroup(group.id, updateData);
                toast.success('Group settings updated');
                // Update local group data
                group.name = name;
                group.description = description;
                group.visibility = visibility;
                group.discoverable_by_unverified = discoverableByUnverified;
                group.topic_tags = topicTags;
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to update settings');
            }
            saveBtn.disabled = false;
            saveBtn.textContent = 'Save Changes';
        });
    }
}

/**
 * Load and render the invite links tab
 * @param {string} groupId - Group UUID
 */
async function loadInviteLinksTab(groupId) {
    const section = document.getElementById('tab-invite-links');
    if (!section) return;

    try {
        const response = await listInviteLinks(groupId);
        const links = response.invite_links || [];

        section.innerHTML = `
            <h3 style="font-weight: 600; margin-bottom: var(--space-4);">Create Invite Link</h3>
            <div style="display: flex; gap: var(--space-3); flex-wrap: wrap; align-items: flex-end;">
                <div class="form-group" style="flex: 1; min-width: 200px; margin-bottom: 0;">
                    <label class="form-label" for="link-label">Label (optional)</label>
                    <input type="text" class="form-input" id="link-label" placeholder="e.g. For neighbors">
                </div>
                <div class="form-group" style="width: 120px; margin-bottom: 0;">
                    <label class="form-label" for="link-max-uses">Max Uses</label>
                    <input type="number" class="form-input" id="link-max-uses" min="1" placeholder="Unlimited">
                </div>
                <button class="btn btn--primary" id="create-link-btn">Create Link</button>
            </div>

            <div id="invite-links-list" class="mt-6">
                ${links.length === 0
                    ? '<p style="color: var(--color-gray-500);">No invite links yet.</p>'
                    : renderInviteLinksList(links)
                }
            </div>
        `;

        const createLinkBtn = document.getElementById('create-link-btn');
        if (createLinkBtn) {
            createLinkBtn.addEventListener('click', async () => {
                const label = document.getElementById('link-label')?.value?.trim() || undefined;
                const maxUsesValue = document.getElementById('link-max-uses')?.value;
                const maxUses = maxUsesValue ? parseInt(maxUsesValue, 10) : undefined;

                createLinkBtn.disabled = true;
                createLinkBtn.textContent = 'Creating...';
                try {
                    const linkData = {};
                    if (label) linkData.label = label;
                    if (maxUses && maxUses > 0) linkData.max_uses = maxUses;
                    await createInviteLink(groupId, linkData);
                    toast.success('Invite link created');
                    await loadInviteLinksTab(groupId);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to create invite link');
                }
                createLinkBtn.disabled = false;
                createLinkBtn.textContent = 'Create Link';
            });
        }

        bindCopyButtons(section);
    } catch (error) {
        console.error('Failed to load invite links:', error);
        section.innerHTML = '<p style="color: var(--color-error);">Failed to load invite links.</p>';
    }
}

/**
 * Render invite links list HTML
 * @param {Object[]} links - Invite links
 * @returns {string} HTML
 */
function renderInviteLinksList(links) {
    return `
        <h3 style="font-weight: 600; margin-bottom: var(--space-3);">Existing Links</h3>
        <div class="card">
            <div class="card__body">
                ${links.map(link => `
                    <div style="display: flex; justify-content: space-between; align-items: center; padding: var(--space-2) 0; border-bottom: 1px solid var(--color-gray-100);">
                        <div>
                            <code style="font-size: var(--font-size-sm); background: var(--color-gray-100); padding: var(--space-1) var(--space-2); border-radius: var(--radius-sm);">${escapeHtml(link.token)}</code>
                            ${link.label ? `<span style="margin-left: var(--space-2); color: var(--color-gray-500); font-size: var(--font-size-sm);">${escapeHtml(link.label)}</span>` : ''}
                            <span style="margin-left: var(--space-2); font-size: var(--font-size-xs); color: var(--color-gray-400);">
                                ${link.max_uses ? `${link.use_count || 0}/${link.max_uses} uses` : 'Unlimited'}
                            </span>
                        </div>
                        <button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(link.token)}">Copy</button>
                    </div>
                `).join('')}
            </div>
        </div>
    `;
}

/**
 * Render the invitations tab
 * @param {string} groupId - Group UUID
 */
function renderInvitationsTab(groupId) {
    const section = document.getElementById('tab-invitations');
    if (!section) return;

    section.innerHTML = `
        <h3 style="font-weight: 600; margin-bottom: var(--space-4);">Invite a User</h3>
        <div style="display: flex; gap: var(--space-3); align-items: flex-end;">
            <div class="form-group" style="flex: 1; margin-bottom: 0;">
                <label class="form-label" for="invite-identifier">Username or Email</label>
                <input type="text" class="form-input" id="invite-identifier" placeholder="Enter username or email">
            </div>
            <button class="btn btn--primary" id="send-invite-btn">Send Invitation</button>
        </div>
    `;

    const sendBtn = document.getElementById('send-invite-btn');
    if (sendBtn) {
        sendBtn.addEventListener('click', async () => {
            const identifier = document.getElementById('invite-identifier')?.value?.trim();
            if (!identifier) {
                toast.error('Please enter a username or email');
                return;
            }
            sendBtn.disabled = true;
            sendBtn.textContent = 'Sending...';
            try {
                await createInvitation(groupId, { user_identifier: identifier });
                toast.success('Invitation sent');
                document.getElementById('invite-identifier').value = '';
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to send invitation');
            }
            sendBtn.disabled = false;
            sendBtn.textContent = 'Send Invitation';
        });
    }
}

/**
 * Load and render the topic board tab
 * @param {string} groupId - Group UUID
 */
async function loadTopicBoardTab(groupId) {
    const section = document.getElementById('tab-topic-board');
    if (!section) return;

    try {
        let existingPosting = null;
        try {
            existingPosting = await getPosting(groupId);
        } catch (e) {
            // No posting exists yet
        }

        section.innerHTML = `
            <h3 style="font-weight: 600; margin-bottom: var(--space-4);">Topic Board Posting</h3>
            <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-bottom: var(--space-4);">
                Post a message visible on the community topic board.
            </p>
            <div class="form-group">
                <label class="form-label" for="posting-title">Title</label>
                <input type="text" class="form-input" id="posting-title" value="${escapeHtml(existingPosting?.title || '')}" maxlength="200" placeholder="Posting title">
            </div>
            <div class="form-group">
                <label class="form-label" for="posting-body">Body</label>
                <textarea class="form-input" id="posting-body" rows="4" maxlength="5000" placeholder="Posting content">${escapeHtml(existingPosting?.body || '')}</textarea>
            </div>
            <div style="display: flex; gap: var(--space-3);">
                <button class="btn btn--primary" id="save-posting-btn">${existingPosting ? 'Update Posting' : 'Create Posting'}</button>
                ${existingPosting ? '<button class="btn btn--danger" id="remove-posting-btn">Remove Posting</button>' : ''}
            </div>
        `;

        const savePostingBtn = document.getElementById('save-posting-btn');
        if (savePostingBtn) {
            savePostingBtn.addEventListener('click', async () => {
                const title = document.getElementById('posting-title')?.value?.trim();
                const body = document.getElementById('posting-body')?.value?.trim();
                if (!title) {
                    toast.error('Title is required');
                    return;
                }
                savePostingBtn.disabled = true;
                savePostingBtn.textContent = 'Saving...';
                try {
                    await createOrUpdatePosting(groupId, { title, body });
                    toast.success('Posting saved');
                    await loadTopicBoardTab(groupId);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to save posting');
                }
                savePostingBtn.disabled = false;
                savePostingBtn.textContent = existingPosting ? 'Update Posting' : 'Create Posting';
            });
        }

        const removePostingBtn = document.getElementById('remove-posting-btn');
        if (removePostingBtn) {
            removePostingBtn.addEventListener('click', async () => {
                const confirmed = await modal.confirm({
                    title: 'Remove Posting?',
                    message: 'This will remove your group\'s posting from the topic board.',
                });
                if (!confirmed) return;
                try {
                    await removePosting(groupId);
                    toast.success('Posting removed');
                    await loadTopicBoardTab(groupId);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to remove posting');
                }
            });
        }
    } catch (error) {
        console.error('Failed to load topic board:', error);
        section.innerHTML = '<p style="color: var(--color-error);">Failed to load topic board.</p>';
    }
}

/**
 * Load and render the blocked groups tab
 * @param {string} groupId - Group UUID
 */
async function loadBlockedGroupsTab(groupId) {
    const section = document.getElementById('tab-blocked');
    if (!section) return;

    try {
        const response = await listBlockedGroups(groupId);
        const blockedGroups = response.blocked_groups || [];

        section.innerHTML = `
            <h3 style="font-weight: 600; margin-bottom: var(--space-4);">Block a Group</h3>
            <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-bottom: var(--space-4);">
                Blocked groups' members cannot see your group and vice versa.
            </p>
            <div style="display: flex; gap: var(--space-3); align-items: flex-end;">
                <div class="form-group" style="flex: 1; margin-bottom: 0;">
                    <label class="form-label" for="block-group-id">Group ID to Block</label>
                    <input type="text" class="form-input" id="block-group-id" placeholder="Enter group ID">
                </div>
                <div class="form-group" style="flex: 1; margin-bottom: 0;">
                    <label class="form-label" for="block-reason">Reason (optional)</label>
                    <input type="text" class="form-input" id="block-reason" placeholder="Why block this group?">
                </div>
                <button class="btn btn--danger" id="block-group-btn">Block</button>
            </div>

            <div class="mt-6">
                ${blockedGroups.length === 0
                    ? '<p style="color: var(--color-gray-500);">No blocked groups.</p>'
                    : `
                        <h3 style="font-weight: 600; margin-bottom: var(--space-3);">Blocked Groups</h3>
                        <div class="card">
                            <div class="card__body">
                                ${blockedGroups.map(blocked => `
                                    <div style="display: flex; justify-content: space-between; align-items: center; padding: var(--space-2) 0; border-bottom: 1px solid var(--color-gray-100);">
                                        <div>
                                            <strong>${escapeHtml(blocked.blocked_group_name || blocked.blocked_group_id)}</strong>
                                            ${blocked.reason ? `<span style="margin-left: var(--space-2); color: var(--color-gray-500); font-size: var(--font-size-sm);">${escapeHtml(blocked.reason)}</span>` : ''}
                                        </div>
                                        <button class="btn btn--sm btn--ghost unblock-btn" data-blocked-id="${blocked.blocked_group_id}" style="color: var(--color-error);">Unblock</button>
                                    </div>
                                `).join('')}
                            </div>
                        </div>
                    `
                }
            </div>
        `;

        const blockBtn = document.getElementById('block-group-btn');
        if (blockBtn) {
            blockBtn.addEventListener('click', async () => {
                const blockedGroupId = document.getElementById('block-group-id')?.value?.trim();
                const reason = document.getElementById('block-reason')?.value?.trim() || undefined;
                if (!blockedGroupId) {
                    toast.error('Please enter a group ID');
                    return;
                }
                blockBtn.disabled = true;
                try {
                    await blockGroup(groupId, { blocked_group_id: blockedGroupId, reason });
                    toast.success('Group blocked');
                    await loadBlockedGroupsTab(groupId);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to block group');
                }
                blockBtn.disabled = false;
            });
        }

        section.querySelectorAll('.unblock-btn').forEach(btn => {
            btn.addEventListener('click', async () => {
                const blockedId = btn.dataset.blockedId;
                try {
                    await unblockGroup(groupId, blockedId);
                    toast.success('Group unblocked');
                    await loadBlockedGroupsTab(groupId);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to unblock group');
                }
            });
        });
    } catch (error) {
        console.error('Failed to load blocked groups:', error);
        section.innerHTML = '<p style="color: var(--color-error);">Failed to load blocked groups.</p>';
    }
}

/**
 * Handle group deletion (superuser only)
 * @param {Object} group - Group data
 */
async function handleDeleteGroup(group) {
    const confirmed = await modal.confirm({
        title: `Delete "${group.name}"?`,
        message: 'This action cannot be undone. All group data, members, signal groups, and resources will be permanently deleted.',
    });
    if (!confirmed) return;

    try {
        await deleteGroup(group.id);
        toast.success(`Group "${group.name}" deleted`);
        navigate('/groups/browse');
    } catch (error) {
        toast.error(error.data?.message || error.message || 'Failed to delete group');
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
    currentGroup = null;
}

export default { render, cleanup };
