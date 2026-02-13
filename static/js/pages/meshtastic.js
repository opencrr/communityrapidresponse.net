/**
 * Meshtastic channels page
 * Shows all Meshtastic channels the user has access to.
 * Decrypts encrypted channel URLs client-side and displays parsed channel info.
 */

import { listMeshtasticChannels } from '../api/meshtastic.js';
import { hasReadAccess, isAdmin } from '../utils/store.js';
import { getPrivateKey, decryptSecret } from '../crypto/index.js';
import { renderMeshtasticCardHTML, initMeshtasticCardQR, bindMeshtasticCopyButtons } from '../components/meshtasticCard.js';

// Cache decrypted channel URLs per secret_id for the session
const decryptedCache = new Map();

/**
 * Get a scope key for grouping (region:id, school:id, or district:id)
 * @param {Object} channel - Channel data
 * @returns {string} Scope key
 */
function getScopeKey(channel) {
    if (channel.region_id) return `region:${channel.region_id}`;
    if (channel.school_id) return `school:${channel.school_id}`;
    if (channel.district_id) return `district:${channel.district_id}`;
    return 'unknown:';
}

/**
 * Get the display name for a channel's scope
 * @param {Object} channel - Channel data
 * @returns {string} Scope name
 */
function getScopeName(channel) {
    if (channel.region_id) return channel.region_name || 'Unknown Community';
    if (channel.school_id) return channel.school_name || 'Unknown School';
    if (channel.district_id) return channel.district_name || 'Unknown District';
    return 'Unknown';
}

/**
 * Get the link for a channel's scope
 * @param {Object} channel - Channel data
 * @returns {string} Scope link path
 */
function getScopeLink(channel) {
    if (channel.region_id) return `/communities/${channel.region_id}`;
    if (channel.school_id) return `/schools/${channel.school_id}`;
    if (channel.district_id) return `/school-districts/${channel.district_id}`;
    return '#';
}

/**
 * Get the scope type for sorting (regions first, schools second, districts third)
 * @param {string} scopeKey - Scope key
 * @returns {number} Sort order
 */
function getScopeOrder(scopeKey) {
    if (scopeKey.startsWith('region:')) return 0;
    if (scopeKey.startsWith('school:')) return 1;
    if (scopeKey.startsWith('district:')) return 2;
    return 3;
}

/**
 * Decrypt a channel's encrypted secret
 * @param {Object} channel - Channel with encrypted_secret
 * @returns {Promise<string|null>} Decrypted channel URL or null
 */
async function decryptChannelSecret(channel) {
    const secret = channel.encrypted_secret;
    if (!secret || !secret.encrypted_payload || !secret.wrapped_dek) {
        return null;
    }

    // Check cache
    if (decryptedCache.has(secret.secret_id)) {
        return decryptedCache.get(secret.secret_id);
    }

    try {
        const privateKey = await getPrivateKey();
        if (!privateKey) {
            return null;
        }

        const plaintext = await decryptSecret(
            secret.encrypted_payload,
            secret.encryption_iv,
            secret.wrapped_dek,
            privateKey
        );

        decryptedCache.set(secret.secret_id, plaintext);
        return plaintext;
    } catch (error) {
        console.error('Failed to decrypt secret for channel:', channel.id, error);
        return null;
    }
}

/**
 * Render the Meshtastic channels page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const userHasReadAccess = hasReadAccess();
    const userIsAdmin = isAdmin();

    if (!userHasReadAccess) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Verification Required</h3>
                    <p class="empty-state__description">
                        You need to verify your address or get vouched to access Meshtastic channels.
                    </p>
                    <a href="/verify" class="btn btn--primary" data-link>Verify Your Address</a>
                </div>
            </div>
        `;
        return;
    }

    // Show loading state
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">Meshtastic Channels</h1>
                    <p class="page__subtitle">
                        Mesh networking channels for your communities. Scan the QR code with the Meshtastic app to connect.
                    </p>
                </div>

                <div class="loading">
                    <div class="spinner spinner--lg"></div>
                </div>
            </div>
        </div>
    `;

    try {
        const response = await listMeshtasticChannels();
        const channels = response.channels || [];
        await renderChannels(container, channels);
    } catch (error) {
        console.error('Failed to load Meshtastic channels:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">Error Loading Channels</h3>
                    <p class="empty-state__description">
                        Failed to load Meshtastic channels. Please try again later.
                    </p>
                    <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
                </div>
            </div>
        `;
    }
}

/**
 * Render the channels list, decrypting secrets in parallel
 * @param {HTMLElement} container - Container element
 * @param {Object[]} channels - Array of Meshtastic channels
 */
async function renderChannels(container, channels) {
    const userIsAdmin = isAdmin();

    // Decrypt all secrets in parallel
    const decryptedURLs = await Promise.all(
        channels.map(channel => decryptChannelSecret(channel))
    );

    // Attach decrypted URL to each channel for rendering
    const channelsWithURLs = channels.map((channel, i) => ({
        ...channel,
        _decryptedURL: decryptedURLs[i],
    }));

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header flex justify-between items-center">
                    <div>
                        <h1 class="page__title">Meshtastic Channels</h1>
                        <p class="page__subtitle">
                            Mesh networking channels for your communities. Scan the QR code with the Meshtastic app to connect.
                        </p>
                    </div>
                    ${userIsAdmin ? `
                        <a href="/admin/meshtastic" class="btn btn--primary" data-link>
                            Create Channel
                        </a>
                    ` : ''}
                </div>

                <!-- Search and Filter -->
                <div class="card mb-6">
                    <div class="card__body">
                        <input
                            type="search"
                            class="form-input"
                            placeholder="Search channels..."
                            id="channel-search"
                        >
                    </div>
                </div>

                ${channelsWithURLs.length === 0 ? `
                    <div class="empty-state">
                        <div class="empty-state__icon">&#x1F4E1;</div>
                        <h3 class="empty-state__title">No Meshtastic Channels</h3>
                        <p class="empty-state__description">
                            No Meshtastic channels have been set up for your communities yet.
                            ${userIsAdmin ? 'Create the first one!' : ''}
                        </p>
                        ${userIsAdmin ? `
                            <a href="/admin/meshtastic" class="btn btn--primary" data-link>Create a Channel</a>
                        ` : ''}
                    </div>
                ` : `
                    <div id="channels-list">
                        ${renderChannelsList(channelsWithURLs)}
                    </div>
                `}
            </div>
        </div>
    `;

    // Render QR codes after DOM is ready
    for (const channel of channelsWithURLs) {
        if (channel._decryptedURL) {
            initMeshtasticCardQR(channel.id, channel._decryptedURL);
        }
    }

    // Bind search
    const searchInput = document.getElementById('channel-search');
    if (searchInput) {
        searchInput.addEventListener('input', (event) => {
            filterChannels(event.target.value, channelsWithURLs);
        });
    }

    // Bind copy buttons
    bindMeshtasticCopyButtons(container);
}

/**
 * Render channels list HTML grouped by scope
 * @param {Object[]} channels - Channels to render (with _decryptedURL)
 * @returns {string} HTML string
 */
function renderChannelsList(channels) {
    // Group by scope key
    const channelsByScope = new Map();
    channels.forEach(channel => {
        const scopeKey = getScopeKey(channel);
        if (!channelsByScope.has(scopeKey)) {
            channelsByScope.set(scopeKey, []);
        }
        channelsByScope.get(scopeKey).push(channel);
    });

    // Sort scope keys: regions first, then schools, then districts; alphabetical within each type
    const sortedKeys = [...channelsByScope.keys()].sort((a, b) => {
        const orderDiff = getScopeOrder(a) - getScopeOrder(b);
        if (orderDiff !== 0) return orderDiff;
        const nameA = getScopeName(channelsByScope.get(a)[0]).toLowerCase();
        const nameB = getScopeName(channelsByScope.get(b)[0]).toLowerCase();
        return nameA.localeCompare(nameB);
    });

    let html = '';

    for (const scopeKey of sortedKeys) {
        const scopeChannels = channelsByScope.get(scopeKey);
        const representativeChannel = scopeChannels[0];
        const scopeName = getScopeName(representativeChannel);
        const scopeLink = getScopeLink(representativeChannel);

        html += `
            <div class="dashboard-section" data-scope-key="${scopeKey}">
                <h2 class="dashboard-section__title">
                    <a href="${scopeLink}" data-link style="color: inherit; text-decoration: none;">
                        ${escapeHtml(scopeName)}
                    </a>
                </h2>
                <div class="dashboard-grid">
                    ${scopeChannels.map(channel => renderChannelCard(channel)).join('')}
                </div>
            </div>
        `;
    }

    return html;
}

/**
 * Render a single channel card (delegates to shared component, adds scope name)
 * @param {Object} channel - Channel data with _decryptedURL
 * @returns {string} HTML string
 */
function renderChannelCard(channel) {
    return renderMeshtasticCardHTML(channel, channel._decryptedURL, {
        scopeName: getScopeName(channel),
    });
}

/**
 * Filter channels by search query
 * @param {string} query - Search query
 * @param {Object[]} channels - All channels (with _decryptedURL)
 */
function filterChannels(query, channels) {
    const normalizedQuery = query.toLowerCase().trim();
    const container = document.getElementById('channels-list');

    if (!normalizedQuery) {
        container.innerHTML = renderChannelsList(channels);
        reRenderQRCodes(channels);
        bindMeshtasticCopyButtons(container);
        return;
    }

    const filtered = channels.filter(channel => {
        const name = channel.name?.toLowerCase() || '';
        const description = channel.description?.toLowerCase() || '';
        const regionName = channel.region_name?.toLowerCase() || '';
        const schoolName = channel.school_name?.toLowerCase() || '';
        const districtName = channel.district_name?.toLowerCase() || '';

        return name.includes(normalizedQuery) ||
               description.includes(normalizedQuery) ||
               regionName.includes(normalizedQuery) ||
               schoolName.includes(normalizedQuery) ||
               districtName.includes(normalizedQuery);
    });

    if (filtered.length === 0) {
        container.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F50D;</div>
                <h3 class="empty-state__title">No Results</h3>
                <p class="empty-state__description">
                    No channels match your search. Try different keywords.
                </p>
            </div>
        `;
    } else {
        container.innerHTML = renderChannelsList(filtered);
        reRenderQRCodes(filtered);
        bindMeshtasticCopyButtons(container);
    }
}

/**
 * Re-render QR codes after DOM update (e.g., after search filter)
 * @param {Object[]} channels - Channels to render QR codes for
 */
function reRenderQRCodes(channels) {
    for (const channel of channels) {
        if (channel._decryptedURL) {
            initMeshtasticCardQR(channel.id, channel._decryptedURL);
        }
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
