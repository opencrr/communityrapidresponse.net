/**
 * Display helpers for decoded Meshtastic channel information
 */

const MODEM_PRESETS = {
    0: 'Long Fast',
    1: 'Long Slow',
    2: 'Very Long Slow',
    3: 'Medium Slow',
    4: 'Medium Fast',
    5: 'Short Slow',
    6: 'Short Fast',
    7: 'Long Moderate',
    8: 'Short Turbo',
};

const REGIONS = {
    0: 'Unset',
    1: 'US',
    2: 'EU_433',
    3: 'EU_868',
    4: 'CN',
    5: 'JP',
    6: 'ANZ',
    7: 'KR',
    8: 'TW',
    9: 'RU',
    10: 'IN',
    11: 'NZ_865',
    12: 'TH',
    13: 'LORA_24',
    14: 'UA_433',
    15: 'UA_868',
    16: 'MY_433',
    17: 'MY_919',
    18: 'SG_923',
};

/**
 * Get human-readable modem preset name
 */
export function getModemPresetName(preset) {
    return MODEM_PRESETS[preset] || `Unknown (${preset})`;
}

/**
 * Get human-readable region name
 */
export function getRegionName(region) {
    return REGIONS[region] || `Unknown (${region})`;
}

/**
 * Analyze PSK (Pre-Shared Key) security
 * @param {Uint8Array|null} psk - The PSK bytes
 * @param {number} pskBytes - Length of PSK
 * @returns {{ type: string, warning: string|null, level: string }}
 */
export function analyzePSKSecurity(psk, pskBytes) {
    if (!psk || pskBytes === 0) {
        return {
            type: 'No encryption',
            warning: 'This channel has no encryption. All traffic is readable by anyone.',
            level: 'danger',
        };
    }

    if (pskBytes === 1 && psk[0] === 1) {
        return {
            type: 'Default key',
            warning: 'This channel uses the default key. All Meshtastic devices can decrypt traffic.',
            level: 'warning',
        };
    }

    if (pskBytes === 16) {
        return {
            type: 'AES-128',
            warning: 'AES-128 encryption. AES-256 (32-byte key) is recommended for stronger security.',
            level: 'info',
        };
    }

    if (pskBytes === 32) {
        return {
            type: 'AES-256',
            warning: null,
            level: 'success',
        };
    }

    return {
        type: `Custom (${pskBytes} bytes)`,
        warning: null,
        level: 'info',
    };
}

/**
 * Format channel info for display
 * @param {Object} decoded - Decoded channel set from decoder.js
 * @returns {Object} Formatted display data
 */
export function formatChannelInfo(decoded) {
    if (!decoded) return null;

    const { channels, lora } = decoded;

    return {
        channelCount: channels.length,
        channels: channels.map((ch, i) => {
            const security = analyzePSKSecurity(ch.psk, ch.pskBytes);
            return {
                index: ch.channelIndex || i,
                name: ch.name || `Channel ${ch.channelIndex || i}`,
                encryption: security.type,
                encryptionLevel: security.level,
                encryptionWarning: security.warning,
                uplinkEnabled: ch.uplinkEnabled,
                downlinkEnabled: ch.downlinkEnabled,
            };
        }),
        lora: lora ? {
            modemPreset: getModemPresetName(lora.modemPreset),
            region: getRegionName(lora.region),
            hopLimit: lora.hopLimit,
            txEnabled: lora.txEnabled,
            txPower: lora.txPower,
        } : null,
    };
}
