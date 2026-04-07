/**
 * School district detail page
 * Shows district info, schools list, and signal groups.
 */

import { getDistrict, districtToFeature } from '../api/schools.js';
import { listMeshtasticChannels } from '../api/meshtastic.js';
import { initMap, addRegionsLayer, fitToBounds, destroyMap } from '../components/map.js';
import { renderMeshtasticCardHTML, initMeshtasticCardQR, bindMeshtasticCopyButtons } from '../components/meshtasticCard.js';
import { isAuthenticated } from '../utils/store.js';
import { getPrivateKey, decryptSecret } from '../crypto/index.js';

let districtId = null;
let districtData = null;
let mapInstance = null;

/**
 * Render the district detail page
 * @param {HTMLElement} container - Container element
 * @param {Object} params - Route params
 */
export async function render(container, params) {
    districtId = params.id;

    container.innerHTML = `
        <div class="page">
            <div class="loading">
                <div class="spinner"></div>
            </div>
        </div>
    `;

    try {
        districtData = await getDistrict(districtId);
        renderDistrictPage(container);
    } catch (error) {
        console.error('Failed to load district:', error);
        container.innerHTML = `
            <div class="page">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">District Not Found</h3>
                    <p class="empty-state__description">
                        This district could not be found or may have been removed.
                    </p>
                    <a href="/schools" class="btn btn--primary" data-link>Browse Schools</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render the full district page
 */
async function renderDistrictPage(container) {
    const district = districtData;
    const schools = district.schools || [];

    let html = `
        <div class="page">
            <div class="page__container">
            <div class="page__header">
                <a href="/schools" class="btn btn--ghost btn--sm" data-link style="margin-bottom: var(--space-3);">
                    &larr; Back to Schools
                </a>
                <h1 class="page__title">${escapeHtml(district.name)}</h1>
                <p class="page__description">
                    ${escapeHtml(district.state)}
                    ${district.district_type ? ` &middot; ${escapeHtml(formatDistrictType(district.district_type))}` : ''}
                </p>
            </div>

            ${district.geometry ? `
            <section style="margin-bottom: var(--space-8);">
                <div id="district-map" style="height: 400px; border-radius: var(--radius-md); overflow: hidden;"></div>
            </section>
            ` : ''}

            <section style="margin-bottom: var(--space-8);">
                <h2 style="margin-bottom: var(--space-4);">Schools in this District</h2>
    `;

    if (schools.length === 0) {
        html += `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F3EB;</div>
                <h3 class="empty-state__title">No Schools Found</h3>
                <p class="empty-state__description">
                    No schools have been added to this district yet.
                </p>
            </div>
        `;
    } else {
        html += `
            <p style="color: var(--color-gray-500); margin-bottom: var(--space-3);">
                ${schools.length} school${schools.length !== 1 ? 's' : ''}
            </p>
            <div class="card-list">
        `;

        for (const school of schools) {
            const bootstrapBadge = school.bootstrap_mode
                ? '<span class="badge badge--warning" style="margin-left: var(--space-2);">Bootstrap</span>'
                : '';

            html += `
                <a href="/schools/${school.id}" class="card card--clickable" data-link>
                    <div class="card__body">
                        <div style="display: flex; justify-content: space-between; align-items: start;">
                            <div>
                                <h3 class="card__title">${escapeHtml(school.name)}${bootstrapBadge}</h3>
                                <p class="card__meta">
                                    ${escapeHtml(school.city || '')}${school.city ? ', ' : ''}${escapeHtml(school.state)}
                                </p>
                            </div>
                            <div style="text-align: right; font-size: var(--font-size-sm); color: var(--color-gray-500);">
                                <div>${school.member_count || 0} member${(school.member_count || 0) !== 1 ? 's' : ''}</div>
                                <div>${school.verified_count || 0} verified</div>
                            </div>
                        </div>
                    </div>
                </a>
            `;
        }

        html += '</div>';
    }

    html += '</section>';

    // Meshtastic channels section
    html += `
        <section style="margin-bottom: var(--space-8);">
            <h2 style="margin-bottom: var(--space-4);">District Meshtastic Channels</h2>
            <div id="district-meshtastic-channels">
                <div class="loading"><div class="spinner"></div></div>
            </div>
        </section>
    `;

    html += '</div></div>';
    container.innerHTML = html;

    // Initialize map if geometry is available
    if (district.geometry) {
        initDistrictMap(district);
    }

    // Load meshtastic channels for authenticated users
    await loadMeshtasticChannels();
}

/**
 * Initialize the district boundary map
 */
function initDistrictMap(district) {
    const mapContainer = document.getElementById('district-map');
    if (!mapContainer) return;

    destroyMap();

    const feature = districtToFeature(district);
    const geojson = {
        type: 'FeatureCollection',
        features: [feature],
    };

    mapInstance = initMap({
        container: mapContainer,
        interactive: true,
        onLoad: (map) => {
            addRegionsLayer(map, geojson);
            fitToBounds(map, geojson, { maxZoom: 12 });
        },
    });
}

/**
 * Load and render Meshtastic channels for the district
 */
async function loadMeshtasticChannels() {
    const channelsContainer = document.getElementById('district-meshtastic-channels');
    if (!channelsContainer) return;

    if (!isAuthenticated()) {
        channelsContainer.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F4E1;</div>
                <p class="empty-state__description">
                    <a href="/login" data-link>Log in</a> and become a verified member of a school in this district to see Meshtastic channels.
                </p>
            </div>
        `;
        return;
    }

    try {
        const response = await listMeshtasticChannels({ district_id: districtId });
        const channels = response.channels || [];

        if (channels.length === 0) {
            channelsContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4E1;</div>
                    <h3 class="empty-state__title">No Meshtastic Channels Yet</h3>
                    <p class="empty-state__description">
                        No district-level Meshtastic channels have been created yet.
                    </p>
                </div>
            `;
        } else {
            // Decrypt channel URLs
            const privateKey = await getPrivateKey();
            const decryptedChannels = await Promise.all(channels.map(async (channel) => {
                let url = null;
                const secret = channel.encrypted_secret;
                if (secret && secret.encrypted_payload && secret.wrapped_dek && privateKey) {
                    try {
                        url = await decryptSecret(secret.encrypted_payload, secret.encryption_iv, secret.wrapped_dek, privateKey);
                    } catch (e) { /* decryption failed */ }
                }
                return { ...channel, _decryptedURL: url };
            }));

            channelsContainer.innerHTML = `
                <div class="dashboard-grid">
                    ${decryptedChannels.map(channel => renderMeshtasticCardHTML(channel, channel._decryptedURL)).join('')}
                </div>
            `;

            // Init QR codes and bind copy buttons
            for (const channel of decryptedChannels) {
                if (channel._decryptedURL) {
                    initMeshtasticCardQR(channel.id, channel._decryptedURL);
                }
            }
            bindMeshtasticCopyButtons(channelsContainer);
        }

        // Add admin create link if user can create
        if (response.can_create) {
            channelsContainer.insertAdjacentHTML('beforeend', `
                <div style="margin-top: var(--space-4);">
                    <a href="/admin/meshtastic?district=${districtId}" class="btn btn--secondary" data-link>
                        Create Meshtastic Channel
                    </a>
                </div>
            `);
        }
    } catch (error) {
        console.error('Failed to load district Meshtastic channels:', error);
        if (error.status === 403) {
            channelsContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <p class="empty-state__description">
                        You must be a verified member of a school in this district to view Meshtastic channels.
                    </p>
                </div>
            `;
        } else {
            channelsContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <p class="empty-state__description">
                        Failed to load Meshtastic channels. Please try again.
                    </p>
                </div>
            `;
        }
    }
}

/**
 * Format district type for display
 */
function formatDistrictType(type) {
    const types = {
        'unified': 'Unified School District',
        'elementary': 'Elementary School District',
        'secondary': 'Secondary School District',
    };
    return types[type] || type;
}

/**
 * Escape HTML to prevent XSS
 */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function cleanup() {
    if (mapInstance) {
        destroyMap();
        mapInstance = null;
    }
    districtId = null;
    districtData = null;
}

export default { render, cleanup };
