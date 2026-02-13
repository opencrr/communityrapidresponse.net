/**
 * Admin: Manage Meshtastic Channels page
 * Allows admins to create and manage Meshtastic channels.
 * Uses E2E encryption for channel URLs.
 */

import {
    listAdminMeshtasticChannels,
    createMeshtasticChannel,
    updateMeshtasticChannel,
} from '../../api/meshtastic.js';
import { createDeletionProposal } from '../../api/deletions.js';
import { getAdminRegions } from '../../api/regions.js';
import { getPublicKeys } from '../../api/encryption.js';
import { ApiError } from '../../api/client.js';
import { isAdmin } from '../../utils/store.js';
import { encryptForMembers, getPrivateKey, decryptSecret } from '../../crypto/index.js';
import { decodeMeshtasticURL } from '../../meshtastic/decoder.js';
import { formatChannelInfo } from '../../meshtastic/display.js';
import toast from '../../components/toast.js';
import modal from '../../components/modal.js';
import { navigate } from '../../app.js';

/**
 * Render the manage Meshtastic channels page
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
                        You need both postcard and vouch verification to manage Meshtastic channels.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>View Verification Status</a>
                </div>
            </div>
        `;
        return;
    }

    // Show loading state
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header flex justify-between items-center">
                    <div>
                        <h1 class="page__title">Manage Meshtastic Channels</h1>
                        <p class="page__subtitle">Create and manage Meshtastic mesh networking channels for your communities.</p>
                    </div>
                    <button class="btn btn--primary" id="create-channel-btn">
                        Create Channel
                    </button>
                </div>

                <div id="channels-content">
                    <div class="loading">
                        <div class="spinner spinner--lg"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind create button
    document.getElementById('create-channel-btn').addEventListener('click', () => {
        showCreateChannelModal();
    });

    // Load channels
    await loadChannels(container);

    // Auto-open create modal if scope query param is present
    const urlParams = new URLSearchParams(window.location.search);
    const prefilledRegionId = urlParams.get('region');
    const prefilledSchoolId = urlParams.get('school');
    const prefilledDistrictId = urlParams.get('district');
    if (prefilledRegionId || prefilledSchoolId || prefilledDistrictId) {
        showCreateChannelModal({ regionId: prefilledRegionId, schoolId: prefilledSchoolId, districtId: prefilledDistrictId });
    }
}

/**
 * Load and display admin's Meshtastic channels
 * @param {HTMLElement} container - Page container
 */
async function loadChannels(container) {
    const content = document.getElementById('channels-content');

    try {
        const response = await listAdminMeshtasticChannels();
        const channels = response.channels || [];

        if (channels.length === 0) {
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4E1;</div>
                    <h3 class="empty-state__title">No Meshtastic Channels Yet</h3>
                    <p class="empty-state__description">
                        You haven't created any Meshtastic channels yet. Create one to help neighbors connect over mesh networking!
                    </p>
                </div>
            `;
            return;
        }

        // Decrypt channel URLs for display
        const privateKey = await getPrivateKey();
        const decryptedChannels = await Promise.all(channels.map(async (channel) => {
            let decryptedURL = null;
            const secret = channel.encrypted_secret;
            if (secret && secret.encrypted_payload && secret.wrapped_dek && privateKey) {
                try {
                    decryptedURL = await decryptSecret(
                        secret.encrypted_payload,
                        secret.encryption_iv,
                        secret.wrapped_dek,
                        privateKey
                    );
                } catch (error) {
                    console.error('Failed to decrypt channel URL for channel:', channel.id, error);
                }
            }

            let channelInfo = null;
            if (decryptedURL) {
                const decoded = decodeMeshtasticURL(decryptedURL);
                channelInfo = formatChannelInfo(decoded);
            }

            return { ...channel, _decryptedURL: decryptedURL, _channelInfo: channelInfo };
        }));

        content.innerHTML = `
            <div class="dashboard-grid">
                ${decryptedChannels.map(channel => renderChannelCard(channel)).join('')}
            </div>
        `;

        // Bind card actions
        bindChannelActions();
    } catch (error) {
        console.error('Failed to load channels:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Channels</h3>
                <p class="empty-state__description">
                    Failed to load Meshtastic channels. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a channel card with admin actions
 * @param {Object} channel - Channel data with _decryptedURL, _channelInfo
 * @returns {string} HTML string
 */
function renderChannelCard(channel) {
    const displayURL = channel._decryptedURL || (channel.encrypted_secret ? 'Encrypted (key not available)' : 'No channel URL set');
    const channelInfo = channel._channelInfo;

    let infoSummary = '';
    if (channelInfo) {
        const channelNames = channelInfo.channels.map(ch => escapeHtml(ch.name)).join(', ');
        infoSummary = `
            <div style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                ${channelInfo.channelCount} channel${channelInfo.channelCount !== 1 ? 's' : ''}: ${channelNames}
                ${channelInfo.lora ? ` | ${escapeHtml(channelInfo.lora.modemPreset)} | ${escapeHtml(channelInfo.lora.region)}` : ''}
            </div>
        `;

        // Add warnings
        for (const ch of channelInfo.channels) {
            if (ch.encryptionWarning && (ch.encryptionLevel === 'danger' || ch.encryptionLevel === 'warning')) {
                infoSummary += `<div class="alert ${ch.encryptionLevel === 'danger' ? 'alert--error' : 'alert--warning'}" style="margin-top: var(--space-2); padding: var(--space-2) var(--space-3); font-size: var(--font-size-sm);">
                    ${ch.encryptionWarning}
                </div>`;
            }
        }
    }

    return `
        <div class="card channel-card" data-channel-id="${channel.id}">
            <div class="card__body">
                <div class="group-card__header">
                    <div>
                        <div class="group-card__name">${escapeHtml(channel.name)}${channel.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
                        <div class="group-card__region">${escapeHtml(channel.region_name || channel.school_name || channel.district_name || '')}</div>
                    </div>
                </div>
                ${channel.description ? `
                    <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                        ${escapeHtml(channel.description)}
                    </p>
                ` : ''}
                <div class="group-invite">
                    <span class="group-invite__link">${escapeHtml(displayURL)}</span>
                </div>
                ${infoSummary}
                <div class="admin-actions">
                    <button class="btn btn--sm btn--secondary edit-channel-btn" data-channel-id="${channel.id}">
                        Edit
                    </button>
                    <button class="btn btn--sm btn--danger delete-channel-btn" data-channel-id="${channel.id}"${channel.has_pending_deletion ? ' disabled' : ''}>
                        ${channel.has_pending_deletion ? 'Delete Proposed' : 'Delete'}
                    </button>
                </div>
            </div>
        </div>
    `;
}

/**
 * Bind event handlers for channel action buttons
 */
function bindChannelActions() {
    // Edit buttons
    document.querySelectorAll('.edit-channel-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const channelId = btn.dataset.channelId;
            showEditChannelModal(channelId);
        });
    });

    // Delete buttons
    document.querySelectorAll('.delete-channel-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const channelId = btn.dataset.channelId;
            handleDeleteChannel(channelId);
        });
    });
}

/**
 * Show create channel modal
 * @param {Object} [prefill] - Pre-fill options from query params
 * @param {string} [prefill.regionId] - Region ID to pre-select
 * @param {string} [prefill.schoolId] - School ID (reserved for future use)
 * @param {string} [prefill.districtId] - District ID (reserved for future use)
 */
async function showCreateChannelModal(prefill = {}) {
    let regions = [];
    try {
        regions = await getAdminRegions();
    } catch (error) {
        toast.error('Failed to load communities');
        return;
    }

    if (regions.length === 0) {
        toast.warning('You need to create a community first before creating a Meshtastic channel.');
        navigate('/admin/communities');
        return;
    }

    const regionOptions = regions.map(r =>
        `<option value="${r.id}"${prefill.regionId === r.id ? ' selected' : ''}>${escapeHtml(r.name)}</option>`
    ).join('');

    const formHtml = `
        <form id="create-channel-form">
            <div class="form-group">
                <label for="channel-name" class="form-label form-label--required">Channel Name</label>
                <input
                    type="text"
                    id="channel-name"
                    name="name"
                    class="form-input"
                    required
                    placeholder="e.g., Emergency Mesh Network"
                >
            </div>
            <div class="form-group">
                <label for="channel-region" class="form-label form-label--required">Community</label>
                <select id="channel-region" name="region_id" class="form-input" required>
                    <option value="">Select community</option>
                    ${regionOptions}
                </select>
            </div>
            <div class="form-group">
                <label for="channel-url" class="form-label form-label--required">Meshtastic Channel URL</label>
                <input
                    type="url"
                    id="channel-url"
                    name="channel_url"
                    class="form-input"
                    required
                    placeholder="https://meshtastic.org/e/#..."
                >
                <p class="form-hint">Paste the full Meshtastic channel URL from the Meshtastic app. The URL will be encrypted end-to-end.</p>
            </div>
            <div id="url-preview" class="hidden"></div>
            <div class="form-group">
                <label for="channel-description" class="form-label">Description (optional)</label>
                <textarea
                    id="channel-description"
                    name="description"
                    class="form-input"
                    rows="3"
                    placeholder="What is this channel for?"
                ></textarea>
            </div>
            <div id="create-channel-error" class="form-error hidden"></div>
        </form>
    `;

    modal.showModal({
        title: 'Create Meshtastic Channel',
        content: formHtml,
        actions: [
            { label: 'Cancel', type: 'secondary' },
            {
                label: 'Create Channel',
                type: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    await handleCreateChannel();
                },
            },
        ],
    });

    // Set up URL preview
    setupURLPreview();
}

/**
 * Set up live URL preview in the create modal
 */
function setupURLPreview() {
    const urlInput = document.getElementById('channel-url');
    const preview = document.getElementById('url-preview');
    if (!urlInput || !preview) return;

    urlInput.addEventListener('input', () => {
        const url = urlInput.value.trim();
        if (!url) {
            preview.classList.add('hidden');
            return;
        }

        const decoded = decodeMeshtasticURL(url);
        if (!decoded) {
            preview.classList.remove('hidden');
            preview.innerHTML = '<div class="alert alert--warning" style="margin-top: var(--space-2);">Could not parse this URL. Make sure it is a valid Meshtastic channel URL.</div>';
            return;
        }

        const info = formatChannelInfo(decoded);
        if (!info) {
            preview.classList.add('hidden');
            return;
        }

        let html = '<div class="alert alert--info" style="margin-top: var(--space-2);"><strong>URL Preview:</strong><br>';
        html += `Channels: ${info.channelCount}`;
        if (info.lora) {
            html += ` | Preset: ${escapeHtml(info.lora.modemPreset)} | Region: ${escapeHtml(info.lora.region)}`;
        }
        for (const ch of info.channels) {
            if (ch.encryptionWarning && ch.encryptionLevel !== 'info') {
                const colorVar = ch.encryptionLevel === 'danger' ? 'var(--color-error-600)' : 'var(--color-warning-600)';
                html += `<br><span style="color: ${colorVar};">${ch.encryptionWarning}</span>`;
            }
        }
        html += '</div>';

        preview.classList.remove('hidden');
        preview.innerHTML = html;
    });
}

/**
 * Handle create channel form submission
 * Encrypts the channel URL for all region members before sending
 */
async function handleCreateChannel() {
    const form = document.getElementById('create-channel-form');
    const errorElement = document.getElementById('create-channel-error');

    if (!form.checkValidity()) {
        form.reportValidity();
        return;
    }

    const channelURL = form.channel_url.value.trim();
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

        // Encrypt channel URL for all members
        const encrypted = await encryptForMembers(channelURL, memberKeys);

        const data = {
            name: form.name.value.trim(),
            region_id: regionId,
            encrypted_payload: encrypted.ciphertext,
            encryption_iv: encrypted.iv,
            wrapped_keys: encrypted.wrappedKeys,
            description: form.description.value.trim() || undefined,
        };

        await createMeshtasticChannel(data);
        modal.closeModal();
        toast.success('Meshtastic channel created successfully!');
        await loadChannels(document.getElementById('main'));
    } catch (error) {
        let errorMessage = 'Failed to create channel.';
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
 * Show edit channel modal
 * @param {string} channelId - Channel ID
 */
async function showEditChannelModal(channelId) {
    const card = document.querySelector(`[data-channel-id="${channelId}"]`);
    if (!card) return;

    const name = card.querySelector('.group-card__name')?.textContent || '';
    const description = card.querySelector('p')?.textContent || '';

    const formHtml = `
        <form id="edit-channel-form">
            <div class="form-group">
                <label for="edit-channel-name" class="form-label form-label--required">Channel Name</label>
                <input
                    type="text"
                    id="edit-channel-name"
                    name="name"
                    class="form-input"
                    required
                    value="${escapeHtml(name)}"
                >
            </div>
            <div class="form-group">
                <label for="edit-channel-description" class="form-label">Description</label>
                <textarea
                    id="edit-channel-description"
                    name="description"
                    class="form-input"
                    rows="3"
                >${escapeHtml(description)}</textarea>
            </div>
            <div id="edit-channel-error" class="form-error hidden"></div>
        </form>
    `;

    modal.showModal({
        title: 'Edit Meshtastic Channel',
        content: formHtml,
        actions: [
            { label: 'Cancel', type: 'secondary' },
            {
                label: 'Save Changes',
                type: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    await handleEditChannel(channelId);
                },
            },
        ],
    });
}

/**
 * Handle edit channel form submission
 * @param {string} channelId - Channel ID
 */
async function handleEditChannel(channelId) {
    const form = document.getElementById('edit-channel-form');
    const errorElement = document.getElementById('edit-channel-error');

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
        await updateMeshtasticChannel(channelId, data);
        modal.closeModal();
        toast.success('Channel updated successfully!');
        await loadChannels(document.getElementById('main'));
    } catch (error) {
        let errorMessage = 'Failed to update channel.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
    }
}

/**
 * Handle delete channel
 * @param {string} channelId - Channel ID
 */
async function handleDeleteChannel(channelId) {
    const reason = await modal.prompt({
        title: 'Propose Meshtastic Channel Deletion',
        message: 'This will create a deletion proposal that requires approval from other admins. Please provide a reason:',
        placeholder: 'Reason for deletion...',
        confirmLabel: 'Propose Deletion',
        confirmType: 'danger',
    });

    if (!reason) return;

    try {
        await createDeletionProposal({
            asset_type: 'meshtastic_channel',
            asset_id: channelId,
            reason: reason,
        });
        toast.success('Deletion proposal created. Other admins will vote on it.');
        await loadChannels(document.getElementById('main'));
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
