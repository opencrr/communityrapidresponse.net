/**
 * Region detail page
 * Shows single region information with map and child regions.
 */

import { getRegion, updateRegion, deleteRegion, regionToFeature } from '../api/regions.js';
import { getGroupsByRegion } from '../api/signalGroups.js';
import { listMeshtasticChannels } from '../api/meshtastic.js';
import { initMap, addRegionsLayer, destroyMap, fitToBounds } from '../components/map.js';
import { renderMeshtasticCardHTML, initMeshtasticCardQR, bindMeshtasticCopyButtons } from '../components/meshtasticCard.js';
import { isAuthenticated, hasReadAccess, isAdmin, isSuperuser } from '../utils/store.js';
import { getPrivateKey, decryptSecret } from '../crypto/index.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';
import { navigate } from '../app.js';

let mapInstance = null;

/**
 * Render the region detail page
 * @param {HTMLElement} container - Container element to render into
 * @param {Object} params - Route parameters
 * @param {string} params.id - Region ID
 */
export async function render(container, params) {
    const regionId = params.id;

    // Show loading state
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
        // Load region data
        const canViewSecrets = isAuthenticated() && hasReadAccess();
        const [region, groups, meshtasticResponse] = await Promise.all([
            getRegion(regionId),
            canViewSecrets
                ? getGroupsByRegion(regionId).catch(() => [])
                : Promise.resolve([]),
            canViewSecrets
                ? listMeshtasticChannels({ region_id: regionId }).catch(() => ({ channels: [] }))
                : Promise.resolve({ channels: [] }),
        ]);

        // Use sub_regions from the region response (uses ST_Contains for geographic containment)
        const childRegions = Array.isArray(region.sub_regions) ? region.sub_regions : [];

        // Decrypt invite links and meshtastic channel URLs
        const privateKey = await getPrivateKey();
        const decryptedGroups = await Promise.all(groups.map(async (group) => {
            let link = null;
            const secret = group.encrypted_secret;
            if (secret && secret.encrypted_payload && secret.wrapped_dek && privateKey) {
                try {
                    link = await decryptSecret(secret.encrypted_payload, secret.encryption_iv, secret.wrapped_dek, privateKey);
                } catch (e) { /* decryption failed */ }
            }
            return { ...group, _decryptedLink: link };
        }));

        const meshtasticChannels = meshtasticResponse.channels || [];
        const decryptedMeshtastic = await Promise.all(meshtasticChannels.map(async (channel) => {
            let url = null;
            const secret = channel.encrypted_secret;
            if (secret && secret.encrypted_payload && secret.wrapped_dek && privateKey) {
                try {
                    url = await decryptSecret(secret.encrypted_payload, secret.encryption_iv, secret.wrapped_dek, privateKey);
                } catch (e) { /* decryption failed */ }
            }
            return { ...channel, _decryptedURL: url };
        }));

        renderRegionDetail(container, region, childRegions, decryptedGroups, decryptedMeshtastic);
    } catch (error) {
        console.error('Failed to load region:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">Community Not Found</h3>
                    <p class="empty-state__description">
                        The community you're looking for doesn't exist or couldn't be loaded.
                    </p>
                    <a href="/communities" class="btn btn--primary" data-link>Browse All Communities</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render region detail content
 * @param {HTMLElement} container - Container element
 * @param {Object} region - Region data
 * @param {Object[]} childRegions - Child regions
 * @param {Object[]} groups - Signal groups in this region
 * @param {Object[]} meshtasticChannels - Meshtastic channels in this region
 */
function renderRegionDetail(container, region, childRegions, groups, meshtasticChannels = []) {
    const authenticated = isAuthenticated();
    const userHasReadAccess = hasReadAccess();
    const userIsAdmin = isAdmin();
    const userIsSuperuser = isSuperuser();

    const typeLabels = {
        state: 'State',
        county: 'County',
        city: 'City',
        locality: 'Locality',
        neighborhood: 'Neighborhood',
    };

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="region-detail">
                    <!-- Breadcrumb -->
                    <nav class="region-detail__breadcrumb">
                        <a href="/communities" data-link>All Communities</a>
                        ${region.parent_name ? `
                            &rsaquo; <a href="/communities/${region.parent_id}" data-link>${escapeHtml(region.parent_name)}</a>
                        ` : ''}
                        &rsaquo; ${escapeHtml(region.name)}
                    </nav>

                    <!-- Header -->
                    <div class="region-detail__header">
                        <div style="display: flex; align-items: center; gap: var(--space-3);">
                            <h1 class="page__title" id="region-name">${escapeHtml(region.name)}</h1>
                            ${userIsAdmin ? `
                                <button class="btn btn--sm btn--secondary" id="edit-name-btn" title="Edit community name">
                                    &#x270E; Edit
                                </button>
                            ` : ''}
                        </div>
                        <span class="region-item__type" style="margin-top: var(--space-2); display: inline-block;">
                            ${typeLabels[region.region_type] || region.region_type}
                        </span>
                    </div>

                    <!-- Stats -->
                    <div class="region-detail__stats">
                        <div class="region-stat">
                            <div class="region-stat__value">${region.member_count || 0}</div>
                            <div class="region-stat__label">Members</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${childRegions.length}</div>
                            <div class="region-stat__label">Sub-communities</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${groups.length}</div>
                            <div class="region-stat__label">Signal Groups</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${meshtasticChannels.length}</div>
                            <div class="region-stat__label">Meshtastic</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${region.full_admin_count || 0}</div>
                            <div class="region-stat__label">Admins</div>
                        </div>
                    </div>

                    ${region.bootstrap_mode ? `
                        <div class="privacy-notice" style="margin-top: var(--space-4); background: #dbeafe;">
                            <div class="privacy-notice__icon">&#x1F680;</div>
                            <div class="privacy-notice__text">
                                <strong>Bootstrap Mode Active</strong>
                                This region has fewer than 3 fully verified admins. During bootstrap mode, any community member can vouch for others
                                (3 vouches required with 1-hour cooldown) to help establish the initial admin team.
                            </div>
                        </div>
                    ` : ''}

                    ${renderJoinButton(region, authenticated, userHasReadAccess)}

                    <!-- Map -->
                    <div class="card mt-6">
                        <div class="card__body" style="padding: 0; height: 400px;">
                            <div class="map-container" id="region-map"></div>
                        </div>
                    </div>

                    <!-- Child Regions -->
                    ${childRegions.length > 0 ? `
                        <div class="mt-6">
                            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                                Sub-communities
                            </h2>
                            <div class="dashboard-grid">
                                ${childRegions.map(child => `
                                    <a href="/communities/${child.id}" class="region-item" data-link>
                                        <span class="region-item__type">${formatRegionType(child.type)}</span>
                                        <div class="region-item__name">${escapeHtml(child.name)}</div>
                                        ${child.member_count !== undefined ? `
                                            <div class="region-item__meta">${child.member_count} members</div>
                                        ` : ''}
                                    </a>
                                `).join('')}
                            </div>
                        </div>
                    ` : ''}

                    <!-- Signal Groups (for users with read access) -->
                    ${authenticated && userHasReadAccess ? `
                        <div class="mt-6">
                            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                                Signal Groups
                            </h2>
                            ${groups.length > 0 ? `
                                <div class="dashboard-grid">
                                    ${groups.map(group => `
                                        <div class="card group-card">
                                            <div class="card__body">
                                                <div class="group-card__name">${escapeHtml(group.name)}${group.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
                                                ${group.description ? `
                                                    <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                                        ${escapeHtml(group.description)}
                                                    </p>
                                                ` : ''}
                                                <div class="group-invite">
                                                    <span class="group-invite__link">${escapeHtml(group._decryptedLink || 'Invite link hidden')}</span>
                                                    ${group._decryptedLink ? `
                                                        <button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(group._decryptedLink)}">
                                                            Copy
                                                        </button>
                                                    ` : ''}
                                                </div>
                                            </div>
                                        </div>
                                    `).join('')}
                                </div>
                            ` : `
                                <div class="card">
                                    <div class="card__body">
                                        <p class="text-center" style="color: var(--color-gray-600);">
                                            No Signal groups have been created for this community yet.
                                        </p>
                                    </div>
                                </div>
                            `}
                        </div>

                        <!-- Meshtastic Channels -->
                        <div class="mt-6" id="meshtastic-section">
                            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                                Meshtastic Channels
                            </h2>
                            ${meshtasticChannels.length > 0 ? `
                                <div class="dashboard-grid">
                                    ${meshtasticChannels.map(channel => renderMeshtasticCardHTML(channel, channel._decryptedURL)).join('')}
                                </div>
                            ` : `
                                <div class="card">
                                    <div class="card__body">
                                        <p class="text-center" style="color: var(--color-gray-600);">
                                            No Meshtastic channels have been created for this community yet.
                                        </p>
                                    </div>
                                </div>
                            `}
                        </div>
                    ` : `
                        ${authenticated ? `
                            <div class="mt-6">
                                <div class="card">
                                    <div class="card__body">
                                        <p class="text-center" style="color: var(--color-gray-600);">
                                            <a href="/vouch" data-link>Get vouched by community members</a> to view Signal groups in this community.
                                        </p>
                                    </div>
                                </div>
                            </div>
                        ` : `
                            <div class="mt-6">
                                <div class="card">
                                    <div class="card__body">
                                        <p class="text-center" style="color: var(--color-gray-600);">
                                            <a href="/login" data-link>Log in</a> and get vouched to view Signal groups.
                                        </p>
                                    </div>
                                </div>
                            </div>
                        `}
                    `}

                    <!-- Members (for authenticated members) -->
                    ${authenticated && region.is_member ? `
                        <div id="members-section" class="mt-6"></div>
                    ` : ''}

                    <!-- Admin Actions (requires both postcard and vouch verification) -->
                    ${authenticated && userIsAdmin ? `
                        <div class="admin-actions mt-6">
                            <a href="/admin/communities?parent=${region.id}" class="btn btn--secondary" data-link>
                                Create Sub-community
                            </a>
                            <a href="/admin/groups?region=${region.id}" class="btn btn--secondary" data-link>
                                Create Signal Group
                            </a>
                            <a href="/admin/meshtastic?region=${region.id}" class="btn btn--secondary" data-link>
                                Create Meshtastic Channel
                            </a>
                            ${userIsSuperuser ? `
                                <button class="btn btn--danger" id="delete-region-btn">
                                    Delete Community
                                </button>
                            ` : ''}
                        </div>
                    ` : ''}
                </div>
            </div>
        </div>
    `;

    // Initialize map
    initRegionMap(region, childRegions);

    // Init Meshtastic QR codes and copy buttons
    for (const channel of meshtasticChannels) {
        if (channel._decryptedURL) {
            initMeshtasticCardQR(channel.id, channel._decryptedURL);
        }
    }
    const meshtasticSection = container.querySelector('#meshtastic-section');
    if (meshtasticSection) {
        bindMeshtasticCopyButtons(meshtasticSection);
    }

    // Bind copy buttons
    const copyButtons = container.querySelectorAll('.copy-link-btn');
    copyButtons.forEach(btn => {
        btn.addEventListener('click', async () => {
            const link = btn.dataset.link;
            try {
                await navigator.clipboard.writeText(link);
                toast.success('Invite link copied to clipboard');
            } catch (error) {
                toast.error('Failed to copy link');
            }
        });
    });

    // Bind edit name button (for admins)
    const editNameBtn = container.querySelector('#edit-name-btn');
    if (editNameBtn) {
        editNameBtn.addEventListener('click', () => {
            showEditNameModal(region, container, childRegions, groups, meshtasticChannels);
        });
    }

    // Bind delete button (for superusers)
    const deleteBtn = container.querySelector('#delete-region-btn');
    if (deleteBtn) {
        deleteBtn.addEventListener('click', () => {
            showDeleteConfirmModal(region);
        });
    }


}

/**
 * Render join request button for sub-regions
 * @param {Object} region - Region data
 * @param {boolean} authenticated - User is logged in
 * @param {boolean} userHasReadAccess - User has at least one verification
 * @returns {string} HTML string
 */
function renderJoinButton(region, authenticated, userHasReadAccess) {
    // Only show for sub-regions (regions with a parent)
    if (!region.parent_id) {
        return '';
    }

    // User must be logged in
    if (!authenticated) {
        return `
            <div class="card" style="margin-top: var(--space-4);">
                <div class="card__body text-center">
                    <p style="color: var(--color-gray-600);">
                        <a href="/login" data-link>Log in</a> to request membership in this community.
                    </p>
                </div>
            </div>
        `;
    }

    // User must have vouch verification (read access)
    if (!userHasReadAccess) {
        return `
            <div class="card" style="margin-top: var(--space-4);">
                <div class="card__body text-center">
                    <p style="color: var(--color-gray-600);">
                        <a href="/vouch" data-link>Get vouched by community members</a> to request membership in this community.
                    </p>
                </div>
            </div>
        `;
    }

    // Check if user is already a member (from region data)
    if (region.is_member) {
        return `
            <div class="card" style="margin-top: var(--space-4); border-left: 4px solid var(--color-success);">
                <div class="card__body text-center">
                    <p style="color: var(--color-success); font-weight: 500;">
                        &#x2713; You are a member of this community
                    </p>
                </div>
            </div>
        `;
    }

    // Check if user has a pending request
    if (region.has_pending_request) {
        return `
            <div class="card" style="margin-top: var(--space-4); border-left: 4px solid var(--color-warning);">
                <div class="card__body text-center">
                    <p style="color: var(--color-warning); font-weight: 500;">
                        &#x23F3; Your membership request is pending admin approval
                    </p>
                    <a href="/membership" class="btn btn--secondary btn--sm" style="margin-top: var(--space-2);" data-link>
                        View My Requests
                    </a>
                </div>
            </div>
        `;
    }

    // Show the request button
    return `
        <div class="card" style="margin-top: var(--space-4);">
            <div class="card__body text-center">
                <p style="color: var(--color-gray-600); margin-bottom: var(--space-3);">
                    Want to join this neighborhood?
                </p>
                <button class="btn btn--primary" id="request-membership-btn" data-region-id="${region.id}">
                    Request to Join
                </button>
                <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-2);">
                    Admins will review and approve your request.
                </p>
            </div>
        </div>
    `;
}

/**
 * Show modal to edit region name
 * @param {Object} region - Region data
 * @param {HTMLElement} container - Container element
 * @param {Object[]} childRegions - Child regions (for re-render)
 * @param {Object[]} groups - Signal groups (for re-render)
 * @param {Object[]} meshtasticChannels - Meshtastic channels (for re-render)
 */
function showEditNameModal(region, container, childRegions, groups, meshtasticChannels = []) {
    const modalContainer = document.getElementById('modal-container');

    modalContainer.innerHTML = `
        <div class="modal-backdrop" id="edit-name-modal">
            <div class="modal">
                <div class="modal__header">
                    <h3 class="modal__title">Edit Community Name</h3>
                    <button class="modal__close" id="close-modal">&times;</button>
                </div>
                <div class="modal__body">
                    <form id="edit-name-form">
                        <div class="form-group">
                            <label class="form-label" for="region-name-input">Community Name</label>
                            <input
                                type="text"
                                id="region-name-input"
                                class="form-input"
                                value="${escapeHtml(region.name)}"
                                required
                                minlength="2"
                                maxlength="100"
                            >
                        </div>
                        <div class="form-actions" style="margin-top: var(--space-4); display: flex; gap: var(--space-3); justify-content: flex-end;">
                            <button type="button" class="btn btn--secondary" id="cancel-edit">Cancel</button>
                            <button type="submit" class="btn btn--primary" id="save-name-btn">Save</button>
                        </div>
                    </form>
                </div>
            </div>
        </div>
    `;

    const modal = document.getElementById('edit-name-modal');
    const form = document.getElementById('edit-name-form');
    const closeBtn = document.getElementById('close-modal');
    const cancelBtn = document.getElementById('cancel-edit');
    const nameInput = document.getElementById('region-name-input');

    // Focus the input
    nameInput.focus();
    nameInput.select();

    // Close modal handlers
    const closeModal = () => {
        modalContainer.innerHTML = '';
    };

    closeBtn.addEventListener('click', closeModal);
    cancelBtn.addEventListener('click', closeModal);
    modal.addEventListener('click', (e) => {
        if (e.target === modal) closeModal();
    });

    // Handle form submission
    form.addEventListener('submit', async (e) => {
        e.preventDefault();

        const newName = nameInput.value.trim();
        if (!newName) {
            toast.error('Community name is required');
            return;
        }

        if (newName === region.name) {
            closeModal();
            return;
        }

        const saveBtn = document.getElementById('save-name-btn');
        saveBtn.disabled = true;
        saveBtn.textContent = 'Saving...';

        try {
            await updateRegion(region.id, { name: newName });
            toast.success('Community name updated');

            // Update the region object and re-render
            region.name = newName;
            closeModal();
            renderRegionDetail(container, region, childRegions, groups, meshtasticChannels);
        } catch (error) {
            console.error('Failed to update region name:', error);
            toast.error(error.message || 'Failed to update community name');
            saveBtn.disabled = false;
            saveBtn.textContent = 'Save';
        }
    });
}

/**
 * Show confirmation modal to delete region (superuser only)
 * @param {Object} region - Region data
 */
function showDeleteConfirmModal(region) {
    const modalContainer = document.getElementById('modal-container');

    modalContainer.innerHTML = `
        <div class="modal-backdrop" id="delete-confirm-modal">
            <div class="modal">
                <div class="modal__header">
                    <h3 class="modal__title">Delete Community</h3>
                    <button class="modal__close" id="close-modal">&times;</button>
                </div>
                <div class="modal__body">
                    <p style="margin-bottom: var(--space-4);">
                        Are you sure you want to delete <strong>${escapeHtml(region.name)}</strong>?
                    </p>
                    <p style="color: var(--color-error); font-size: var(--font-size-sm);">
                        This action cannot be undone. All sub-communities and signal groups within this community will also be deleted.
                    </p>
                    <div class="form-actions" style="margin-top: var(--space-6); display: flex; gap: var(--space-3); justify-content: flex-end;">
                        <button type="button" class="btn btn--secondary" id="cancel-delete">Cancel</button>
                        <button type="button" class="btn btn--danger" id="confirm-delete-btn">Delete Community</button>
                    </div>
                </div>
            </div>
        </div>
    `;

    const modal = document.getElementById('delete-confirm-modal');
    const closeBtn = document.getElementById('close-modal');
    const cancelBtn = document.getElementById('cancel-delete');
    const confirmBtn = document.getElementById('confirm-delete-btn');

    // Close modal handlers
    const closeModal = () => {
        modalContainer.innerHTML = '';
    };

    closeBtn.addEventListener('click', closeModal);
    cancelBtn.addEventListener('click', closeModal);
    modal.addEventListener('click', (e) => {
        if (e.target === modal) closeModal();
    });

    // Handle delete confirmation
    confirmBtn.addEventListener('click', async () => {
        confirmBtn.disabled = true;
        confirmBtn.textContent = 'Deleting...';

        try {
            await deleteRegion(region.id);
            toast.success(`Community "${region.name}" deleted`);
            closeModal();
            navigate('/communities');
        } catch (error) {
            console.error('Failed to delete region:', error);
            toast.error(error.message || 'Failed to delete community');
            confirmBtn.disabled = false;
            confirmBtn.textContent = 'Delete Community';
        }
    });
}

/**
 * Initialize the region map
 * @param {Object} region - Region data
 * @param {Object[]} childRegions - Child regions
 */
function initRegionMap(region, childRegions) {
    const mapContainer = document.getElementById('region-map');
    if (!mapContainer) return;

    destroyMap();

    mapInstance = initMap({
        container: mapContainer,
        center: [-98.5795, 39.8283],
        zoom: 10,
        interactive: true,
        onLoad: (map) => {
            // Add this region
            const regionFeature = regionToFeature(region);

            // Combine with child regions
            const allFeatures = [regionFeature];
            childRegions.forEach(child => {
                if (child.geometry || child.boundary) {
                    allFeatures.push(regionToFeature(child));
                }
            });

            const geojson = {
                type: 'FeatureCollection',
                features: allFeatures,
            };

            addRegionsLayer(map, geojson, {
                onClick: (feature) => {
                    if (feature.properties.id !== region.id) {
                        navigate(`/communities/${feature.properties.id}`);
                    }
                },
            });

            // Fit to this region's bounds
            if (region.geometry || region.boundary) {
                fitToBounds(map, regionFeature, { maxZoom: 14 });
            }
        },
    });
}

/**
 * Format region type for display
 * @param {string} type - Region type
 * @returns {string} Formatted type
 */
function formatRegionType(type) {
    const labels = {
        state: 'State',
        county: 'County',
        city: 'City',
        locality: 'Locality',
        neighborhood: 'Neighborhood',
    };
    return labels[type] || type;
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
 * Cleanup when leaving the page
 */
export function cleanup() {
    if (mapInstance) {
        destroyMap();
        mapInstance = null;
    }
}

export default { render, cleanup };
