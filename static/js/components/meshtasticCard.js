/**
 * Shared Meshtastic channel card component
 * Renders channel cards with QR codes, URL display, parsed channel info,
 * LoRa config, and security warnings. Used by meshtastic.js and detail pages.
 */

import { decodeMeshtasticURL } from '../meshtastic/decoder.js';
import { formatChannelInfo } from '../meshtastic/display.js';
import { renderQRCode } from './qrCode.js';
import toast from './toast.js';

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
 * Render the HTML for a single Meshtastic channel card body
 * @param {Object} channel - Channel data
 * @param {string|null} decryptedURL - Decrypted channel URL (or null)
 * @param {Object} [options] - Optional display options
 * @param {string} [options.scopeName] - Scope name to display below channel name (e.g., region/school name)
 * @returns {string} HTML string for the card
 */
export function renderMeshtasticCardHTML(channel, decryptedURL, options = {}) {
    let channelInfo = null;
    if (decryptedURL) {
        const decoded = decodeMeshtasticURL(decryptedURL);
        channelInfo = formatChannelInfo(decoded);
    }

    let channelDetailsHtml = '';
    if (decryptedURL && channelInfo) {
        // Security warnings
        let warningsHtml = '';
        for (const ch of channelInfo.channels) {
            if (ch.encryptionWarning) {
                const alertClass = ch.encryptionLevel === 'danger' ? 'alert--error'
                    : ch.encryptionLevel === 'warning' ? 'alert--warning' : 'alert--info';
                warningsHtml += `<div class="alert ${alertClass}" style="margin-top: var(--space-2); padding: var(--space-2) var(--space-3); font-size: var(--font-size-sm);">
                    ${ch.encryptionWarning}
                </div>`;
            }
        }

        // Channel list
        let channelListHtml = '';
        for (const ch of channelInfo.channels) {
            channelListHtml += `<div style="padding: var(--space-1) 0; font-size: var(--font-size-sm);">
                <strong>${escapeHtml(ch.name)}</strong> &mdash; ${escapeHtml(ch.encryption)}
            </div>`;
        }

        // LoRa info
        let loraInfoHtml = '';
        if (channelInfo.lora) {
            loraInfoHtml = `
                <div style="margin-top: var(--space-3); display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: var(--space-2); font-size: var(--font-size-sm);">
                    <div><strong>Preset:</strong> ${escapeHtml(channelInfo.lora.modemPreset)}</div>
                    <div><strong>Region:</strong> ${escapeHtml(channelInfo.lora.region)}</div>
                    <div><strong>Hop Limit:</strong> ${channelInfo.lora.hopLimit}</div>
                    ${channelInfo.lora.txPower ? `<div><strong>TX Power:</strong> ${channelInfo.lora.txPower} dBm</div>` : ''}
                </div>
            `;
        }

        channelDetailsHtml = `
            ${warningsHtml}
            <div style="margin-top: var(--space-2);">
                <strong>Channels (${channelInfo.channelCount}):</strong>
                ${channelListHtml}
            </div>
            ${loraInfoHtml}
        `;
    }

    let secretContentHtml = '';
    if (decryptedURL) {
        secretContentHtml = `
            <div style="display: flex; gap: var(--space-6); flex-wrap: wrap; margin-top: var(--space-4);">
                <div id="qr-${channel.id}" style="flex-shrink: 0;"></div>
                <div style="flex: 1; min-width: 200px;">
                    <div class="form-group" style="margin-bottom: var(--space-3);">
                        <label class="form-label">Channel URL</label>
                        <div style="display: flex; gap: var(--space-2);">
                            <input type="text" class="form-input" value="${escapeHtml(decryptedURL)}" readonly style="font-size: var(--font-size-sm);">
                            <button class="btn btn--secondary btn--sm copy-url-btn" data-url="${escapeHtml(decryptedURL)}">Copy</button>
                        </div>
                    </div>
                    ${channelDetailsHtml}
                </div>
            </div>
        `;
    } else if (channel.encrypted_secret) {
        secretContentHtml = `
            <div class="alert alert--warning" style="margin-top: var(--space-4);">
                Encryption key needed to view this channel. Try logging out and back in to restore your keys.
            </div>
        `;
    }

    return `
        <div class="card channel-card" data-channel-id="${channel.id}" data-channel-name="${escapeHtml(channel.name).toLowerCase()}">
            <div class="card__body">
                <div class="group-card__header">
                    <div>
                        <div class="group-card__name">${escapeHtml(channel.name)}${channel.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
                        ${options.scopeName ? `<div class="group-card__region">${escapeHtml(options.scopeName)}</div>` : ''}
                    </div>
                </div>
                ${channel.description ? `
                    <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                        ${escapeHtml(channel.description)}
                    </p>
                ` : ''}
                ${secretContentHtml}
            </div>
        </div>
    `;
}

/**
 * Render a QR code into the #qr-{channelId} element after DOM insertion
 * @param {string} channelId - Channel ID
 * @param {string} url - Channel URL to encode
 */
export function initMeshtasticCardQR(channelId, url) {
    const qrContainer = document.getElementById(`qr-${channelId}`);
    if (qrContainer) {
        renderQRCode(qrContainer, url, 180).catch(() => {});
    }
}

/**
 * Bind click handlers to .copy-url-btn elements within a container
 * @param {HTMLElement} container - Container element to search within
 */
export function bindMeshtasticCopyButtons(container) {
    const copyButtons = container.querySelectorAll('.copy-url-btn');
    copyButtons.forEach(btn => {
        btn.addEventListener('click', async () => {
            const url = btn.dataset.url;
            try {
                await navigator.clipboard.writeText(url);
                toast.success('Channel URL copied to clipboard');
            } catch (error) {
                toast.error('Failed to copy URL');
            }
        });
    });
}
