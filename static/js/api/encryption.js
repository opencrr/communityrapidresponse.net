/**
 * API calls for encryption key management
 */

import { get, post, put } from './client.js';

/**
 * Upload encryption keys (public key + wrapped private key backup)
 * POST /api/v1/encryption/keys
 */
export async function uploadKeys(data) {
    return post('/encryption/keys', data);
}

/**
 * Get own wrapped encryption key backup
 * GET /api/v1/encryption/keys
 */
export async function getKeys() {
    return get('/encryption/keys');
}

/**
 * Update wrapped private key (re-wrap with new password)
 * PUT /api/v1/encryption/keys
 */
export async function updateKeys(data) {
    return put('/encryption/keys', data);
}

/**
 * Rotate encryption keys (upload new keypair)
 * POST /api/v1/encryption/keys/rotate
 */
export async function rotateKeys(data) {
    return post('/encryption/keys/rotate', data);
}

/**
 * Get public keys for members of a scope
 * GET /api/v1/encryption/public-keys?region_id=X (or school_id, district_id)
 */
export async function getPublicKeys(params) {
    const query = new URLSearchParams(params).toString();
    return get(`/encryption/public-keys?${query}`);
}

/**
 * Get pending re-key operations for the current user
 * GET /api/v1/encryption/pending-rekeys
 */
export async function getPendingRekeys() {
    return get('/encryption/pending-rekeys');
}

/**
 * Submit re-keyed wrapped DEKs
 * POST /api/v1/encryption/rekey
 * @param {Object} data - { rekeys: [{ secret_id, target_user_id, wrapped_dek }], encrypted_payload?, encryption_iv? }
 * For user key rotation: only rekeys (re-wrap existing DEK for new public key)
 * For group rotation: rekeys + encrypted_payload + encryption_iv (new encrypted secret + wrapped fresh DEK)
 */
export async function submitRekeys(data) {
    return post('/encryption/rekey', data);
}
