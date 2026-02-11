/**
 * MFA API module
 */

import { post, ApiError } from './client.js?v=c49aed9';
import { setUser } from '../utils/store.js?v=c49aed9';

/**
 * Initiate MFA setup
 * Returns QR code and secret for authenticator app
 * @returns {Promise<{qr_code: string, secret: string}>}
 */
export async function initSetup() {
    const response = await post('/mfa/setup', {});
    return response;
}

/**
 * Complete MFA setup by verifying a TOTP code
 * @param {string} code - TOTP code from authenticator app
 * @returns {Promise<{backup_codes: string[], token: string, user: Object}>}
 */
export async function completeSetup(code) {
    const response = await post('/mfa/setup/complete', { code });
    if (response.user) {
        setUser(response.user);
    }
    return response;
}

/**
 * Verify MFA during login with TOTP code
 * @param {string} code - TOTP code from authenticator app
 * @returns {Promise<{token: string, user: Object, backup_codes_count: number}>}
 */
export async function verify(code) {
    const response = await post('/mfa/verify', { code });
    if (response.user) {
        setUser(response.user);
    }
    return response;
}

/**
 * Verify MFA during login with backup code
 * @param {string} backupCode - Backup code
 * @returns {Promise<{token: string, user: Object, backup_codes_count: number}>}
 */
export async function verifyWithBackupCode(backupCode) {
    const response = await post('/mfa/verify', { backup_code: backupCode });
    if (response.user) {
        setUser(response.user);
    }
    return response;
}

export default {
    initSetup,
    completeSetup,
    verify,
    verifyWithBackupCode,
};
