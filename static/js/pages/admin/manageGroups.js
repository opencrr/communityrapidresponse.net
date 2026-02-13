/**
 * Admin: Manage Signal Groups page
 * Allows admins to create and manage Signal groups.
 * Uses E2E encryption for invite links.
 */

import {
    getAdminGroups,
    createGroup,
    updateGroup,
} from '../../api/signalGroups.js';
import { createSecretProposal } from '../../api/secretProposals.js';
import { createDeletionProposal } from '../../api/deletions.js';
import { getAdminRegions } from '../../api/regions.js';
import { getPublicKeys } from '../../api/encryption.js';
import { ApiError } from '../../api/client.js';
import { isAdmin } from '../../utils/store.js';
import { encryptForMembers, getPrivateKey, decryptSecret } from '../../crypto/index.js';
import toast from '../../components/toast.js';
import modal from '../../components/modal.js';
import { navigate } from '../../app.js';

/**
 * Render the manage groups page
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
                        You need both postcard and vouch verification to manage Signal groups.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>View Verification Status</a>
                </div>
            </div>
        `;
        return;
    }

    // Get region ID from URL if present
    const urlParams = new URLSearchParams(window.location.search);
    const regionId = urlParams.get('region');

    // Show loading state
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header flex justify-between items-center">
                    <div>
                        <h1 class="page__title">Manage Signal Groups</h1>
                        <p class="page__subtitle">Create and manage Signal groups for your communities.</p>
                    </div>
                    <button class="btn btn--primary" id="create-group-btn">
                        Create Group
                    </button>
                </div>

                <div id="groups-content">
                    <div class="loading">
                        <div class="spinner spinner--lg"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind create button
    document.getElementById('create-group-btn').addEventListener('click', () => {
        showCreateGroupModal(regionId);
    });

    // Load groups
    await loadGroups(container);
}

/**
 * Load and display admin groups
 * @param {HTMLElement} container - Page container
 */
async function loadGroups(container) {
    const content = document.getElementById('groups-content');

    try {
        const groups = await getAdminGroups();

        if (groups.length === 0) {
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4AC;</div>
                    <h3 class="empty-state__title">No Signal Groups Yet</h3>
                    <p class="empty-state__description">
                        You haven't created any Signal groups yet. Create one to help neighbors connect!
                    </p>
                </div>
            `;
            return;
        }

        // Decrypt invite links for display
        const privateKey = await getPrivateKey();
        const decryptedGroups = await Promise.all(groups.map(async (group) => {
            let decryptedLink = null;
            const secret = group.encrypted_secret;
            if (secret && secret.encrypted_payload && secret.wrapped_dek && privateKey) {
                try {
                    decryptedLink = await decryptSecret(
                        secret.encrypted_payload,
                        secret.encryption_iv,
                        secret.wrapped_dek,
                        privateKey
                    );
                } catch (error) {
                    console.error('Failed to decrypt invite link for group:', group.id, error);
                }
            }
            return { ...group, _decryptedLink: decryptedLink };
        }));

        content.innerHTML = `
            <div class="dashboard-grid">
                ${decryptedGroups.map(group => renderGroupCard(group)).join('')}
            </div>
        `;

        // Bind card actions
        bindGroupActions();
    } catch (error) {
        console.error('Failed to load groups:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Groups</h3>
                <p class="empty-state__description">
                    Failed to load Signal groups. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a group card with admin actions
 * @param {Object} group - Group data with _decryptedLink
 * @returns {string} HTML string
 */
function renderGroupCard(group) {
    const displayLink = group._decryptedLink || (group.encrypted_secret ? 'Encrypted (key not available)' : 'No invite link set');

    return `
        <div class="card group-card" data-group-id="${group.id}">
            <div class="card__body">
                <div class="group-card__header">
                    <div>
                        <div class="group-card__name">${escapeHtml(group.name)}${group.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
                        <div class="group-card__region">${escapeHtml(group.region_name || group.school_name || group.district_name || '')}</div>
                    </div>
                </div>
                ${group.description ? `
                    <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                        ${escapeHtml(group.description)}
                    </p>
                ` : ''}
                <div class="group-invite">
                    <span class="group-invite__link">${escapeHtml(displayLink)}</span>
                </div>
                <div class="admin-actions">
                    <button class="btn btn--sm btn--secondary edit-group-btn" data-group-id="${group.id}">
                        Edit
                    </button>
                    <button class="btn btn--sm btn--secondary update-link-btn"
                        data-group-id="${group.id}"
                        data-region-id="${group.region_id || ''}"
                        data-school-id="${group.school_id || ''}"
                        data-district-id="${group.district_id || ''}"
                        data-secret-id="${group.encrypted_secret?.secret_id || ''}">
                        Update Link
                    </button>
                    <button class="btn btn--sm btn--danger delete-group-btn" data-group-id="${group.id}"${group.has_pending_deletion ? ' disabled' : ''}>
                        ${group.has_pending_deletion ? 'Delete Proposed' : 'Delete'}
                    </button>
                </div>
            </div>
        </div>
    `;
}

/**
 * Bind event handlers for group action buttons
 */
function bindGroupActions() {
    // Edit buttons
    document.querySelectorAll('.edit-group-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const groupId = btn.dataset.groupId;
            showEditGroupModal(groupId);
        });
    });

    // Update link buttons
    document.querySelectorAll('.update-link-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const groupId = btn.dataset.groupId;
            const scopeParams = {};
            if (btn.dataset.regionId) scopeParams.region_id = btn.dataset.regionId;
            if (btn.dataset.schoolId) scopeParams.school_id = btn.dataset.schoolId;
            if (btn.dataset.districtId) scopeParams.district_id = btn.dataset.districtId;
            showUpdateLinkModal(groupId, scopeParams);
        });
    });

    // Delete buttons
    document.querySelectorAll('.delete-group-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const groupId = btn.dataset.groupId;
            handleDeleteGroup(groupId);
        });
    });
}

/**
 * Show create group modal
 * @param {string} preselectedRegionId - Pre-selected region ID
 */
async function showCreateGroupModal(preselectedRegionId) {
    let regions = [];
    try {
        regions = await getAdminRegions();
    } catch (error) {
        toast.error('Failed to load communities');
        return;
    }

    if (regions.length === 0) {
        toast.warning('You need to create a community first before creating a Signal group.');
        navigate('/admin/communities');
        return;
    }

    const regionOptions = regions.map(r =>
        `<option value="${r.id}" ${r.id === preselectedRegionId ? 'selected' : ''}>${escapeHtml(r.name)}</option>`
    ).join('');

    const formHtml = `
        <form id="create-group-form">
            <div class="form-group">
                <label for="group-name" class="form-label form-label--required">Group Name</label>
                <input
                    type="text"
                    id="group-name"
                    name="name"
                    class="form-input"
                    required
                    placeholder="e.g., Downtown Neighbors"
                >
            </div>
            <div class="form-group">
                <label for="group-region" class="form-label form-label--required">Community</label>
                <select id="group-region" name="region_id" class="form-input" required>
                    <option value="">Select community</option>
                    ${regionOptions}
                </select>
            </div>
            <div class="form-group">
                <label for="group-link" class="form-label form-label--required">Signal Invite Link</label>
                <input
                    type="url"
                    id="group-link"
                    name="invite_link"
                    class="form-input"
                    required
                    placeholder="https://signal.group/..."
                >
                <p class="form-hint">The invite link will be encrypted end-to-end. The server never sees the plaintext.</p>
            </div>
            <div class="form-group">
                <label for="group-description" class="form-label">Description (optional)</label>
                <textarea
                    id="group-description"
                    name="description"
                    class="form-input"
                    rows="3"
                    placeholder="What is this group for?"
                ></textarea>
            </div>
            <div id="create-group-error" class="form-error hidden"></div>
        </form>
    `;

    modal.showModal({
        title: 'Create Signal Group',
        content: formHtml,
        actions: [
            { label: 'Cancel', type: 'secondary' },
            {
                label: 'Create Group',
                type: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    await handleCreateGroup();
                },
            },
        ],
    });
}

/**
 * Handle create group form submission
 * Encrypts the invite link for all region members before sending
 */
async function handleCreateGroup() {
    const form = document.getElementById('create-group-form');
    const errorElement = document.getElementById('create-group-error');

    if (!form.checkValidity()) {
        form.reportValidity();
        return;
    }

    const inviteLink = form.invite_link.value.trim();
    const regionId = form.region_id.value;

    errorElement.classList.add('hidden');

    try {
        // Fetch public keys for all members of this region
        const publicKeysResponse = await getPublicKeys({ region_id: regionId });
        const memberKeys = publicKeysResponse.public_keys || [];

        if (memberKeys.length === 0) {
            errorElement.textContent = 'No members with encryption keys found in this community. Members need to log in to set up encryption.';
            errorElement.classList.remove('hidden');
            return;
        }

        // Encrypt invite link for all members
        const encrypted = await encryptForMembers(inviteLink, memberKeys);

        const data = {
            name: form.name.value.trim(),
            region_id: regionId,
            encrypted_payload: encrypted.ciphertext,
            encryption_iv: encrypted.iv,
            wrapped_keys: encrypted.wrappedKeys,
            description: form.description.value.trim() || undefined,
        };

        await createGroup(data);
        modal.closeModal();
        toast.success('Signal group created successfully!');
        await loadGroups(document.getElementById('main'));
    } catch (error) {
        let errorMessage = 'Failed to create group.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        } else if (error.name === 'OperationError') {
            errorMessage = 'Encryption failed. Please ensure your encryption keys are set up.';
        }
        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
    }
}

/**
 * Show edit group modal
 * @param {string} groupId - Group ID
 */
async function showEditGroupModal(groupId) {
    const card = document.querySelector(`[data-group-id="${groupId}"]`);
    if (!card) return;

    const name = card.querySelector('.group-card__name')?.textContent || '';
    const description = card.querySelector('p')?.textContent || '';

    const formHtml = `
        <form id="edit-group-form">
            <div class="form-group">
                <label for="edit-group-name" class="form-label form-label--required">Group Name</label>
                <input
                    type="text"
                    id="edit-group-name"
                    name="name"
                    class="form-input"
                    required
                    value="${escapeHtml(name)}"
                >
            </div>
            <div class="form-group">
                <label for="edit-group-description" class="form-label">Description</label>
                <textarea
                    id="edit-group-description"
                    name="description"
                    class="form-input"
                    rows="3"
                >${escapeHtml(description)}</textarea>
            </div>
            <div id="edit-group-error" class="form-error hidden"></div>
        </form>
    `;

    modal.showModal({
        title: 'Edit Signal Group',
        content: formHtml,
        actions: [
            { label: 'Cancel', type: 'secondary' },
            {
                label: 'Save Changes',
                type: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    await handleEditGroup(groupId);
                },
            },
        ],
    });
}

/**
 * Handle edit group form submission
 * @param {string} groupId - Group ID
 */
async function handleEditGroup(groupId) {
    const form = document.getElementById('edit-group-form');
    const errorElement = document.getElementById('edit-group-error');

    if (!form.checkValidity()) {
        form.reportValidity();
        return;
    }

    const data = {
        name: form.name.value.trim(),
        description: form.description.value.trim() || null,
    };

    errorElement.classList.add('hidden');

    try {
        await updateGroup(groupId, data);
        modal.closeModal();
        toast.success('Group updated successfully!');
        await loadGroups(document.getElementById('main'));
    } catch (error) {
        let errorMessage = 'Failed to update group.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
    }
}

/**
 * Show update invite link modal (creates a secret update proposal)
 * Encrypts the new link for admins only during proposal phase
 * @param {string} groupId - Group ID
 * @param {Object} scopeParams - Scope parameters (region_id, school_id, or district_id)
 */
function showUpdateLinkModal(groupId, scopeParams) {
    const formHtml = `
        <form id="update-link-form">
            <p style="margin-bottom: var(--space-4); color: var(--color-gray-600);">
                Updating the invite link requires approval from at least 3 admins. A proposal will be created for voting.
                The new link is encrypted so only admins can see it during voting.
            </p>
            <div class="form-group">
                <label for="new-invite-link" class="form-label form-label--required">New Invite Link</label>
                <input
                    type="url"
                    id="new-invite-link"
                    name="invite_link"
                    class="form-input"
                    required
                    placeholder="https://signal.group/..."
                >
            </div>
            <div class="form-group">
                <label for="update-reason" class="form-label">Reason (optional)</label>
                <input
                    type="text"
                    id="update-reason"
                    name="reason"
                    class="form-input"
                    placeholder="Why are you updating the link?"
                >
            </div>
            <div id="update-link-error" class="form-error hidden"></div>
        </form>
    `;

    modal.showModal({
        title: 'Propose New Invite Link',
        content: formHtml,
        actions: [
            { label: 'Cancel', type: 'secondary' },
            {
                label: 'Submit Proposal',
                type: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    await handleUpdateLink(groupId, scopeParams);
                },
            },
        ],
    });
}

/**
 * Handle update link proposal submission
 * Encrypts the new link for admins in the scope
 * @param {string} groupId - Group ID
 * @param {Object} scopeParams - Scope parameters for fetching admin keys
 */
async function handleUpdateLink(groupId, scopeParams) {
    const form = document.getElementById('update-link-form');
    const errorElement = document.getElementById('update-link-error');

    if (!form.checkValidity()) {
        form.reportValidity();
        return;
    }

    const newLink = form.invite_link.value.trim();
    const reason = form.reason?.value.trim() || undefined;

    errorElement.classList.add('hidden');

    try {
        // Fetch admin public keys for the scope
        const publicKeysResponse = await getPublicKeys(scopeParams);
        const adminKeys = publicKeysResponse.public_keys || [];

        if (adminKeys.length === 0) {
            errorElement.textContent = 'No admins with encryption keys found. Admins need to log in to set up encryption.';
            errorElement.classList.remove('hidden');
            return;
        }

        // Encrypt the new link for admins only (during proposal phase)
        const encrypted = await encryptForMembers(newLink, adminKeys);

        await createSecretProposal(groupId, {
            encrypted_payload: encrypted.ciphertext,
            encryption_iv: encrypted.iv,
            wrapped_keys: encrypted.wrappedKeys,
            reason: reason,
        });
        modal.closeModal();
        toast.success('Invite link update proposal submitted. Other admins will vote on it.');
    } catch (error) {
        let errorMessage = 'Failed to submit proposal.';
        if (error instanceof ApiError) {
            if (error.message) {
                errorMessage = error.message;
            }
            if (error.status === 400 && error.message.includes('3 admins')) {
                errorMessage = 'This community needs at least 3 admins to update invite links.';
            }
        } else if (error.name === 'OperationError') {
            errorMessage = 'Encryption failed. Please ensure your encryption keys are set up.';
        }
        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
    }
}

/**
 * Handle delete group
 * @param {string} groupId - Group ID
 */
async function handleDeleteGroup(groupId) {
    const reason = await modal.prompt({
        title: 'Propose Signal Group Deletion',
        message: 'This will create a deletion proposal that requires approval from other admins. Please provide a reason:',
        placeholder: 'Reason for deletion...',
        confirmLabel: 'Propose Deletion',
        confirmType: 'danger',
    });

    if (!reason) return;

    try {
        await createDeletionProposal({
            asset_type: 'signal_group',
            asset_id: groupId,
            reason: reason,
        });
        toast.success('Deletion proposal created. Other admins will vote on it.');
        await loadGroups(document.getElementById('main'));
    } catch (error) {
        let errorMessage = 'Failed to create deletion proposal.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
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

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
