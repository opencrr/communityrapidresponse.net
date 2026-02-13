/**
 * Re-keying module
 * Handles automatic re-keying when other users rotate their encryption keys.
 * After a user rotates their keys (e.g. password reset), their wrapped DEKs
 * are flagged for re-keying. Other members who share secrets with that user
 * unwrap their own copy of the DEK and re-wrap it for the user's new public key.
 */

import { getPrivateKey, unwrapDEK, wrapDEK, importPublicKey } from './index.js';
import { getPendingRekeys, submitRekeys } from '../api/encryption.js';

/**
 * Check for and perform any pending re-keys.
 * Called automatically after login when encryption keys are loaded.
 * @returns {Promise<{performed: number, failed: number}>}
 */
export async function checkAndPerformRekeys() {
    try {
        const response = await getPendingRekeys();
        const pendingList = response.pending_rekeys;

        if (!pendingList || pendingList.length === 0) {
            return { performed: 0, failed: 0 };
        }

        return await performRekeys(pendingList);
    } catch (err) {
        console.error('Failed to check pending re-keys:', err);
        return { performed: 0, failed: 0 };
    }
}

/**
 * Perform re-keying for a list of pending re-key entries.
 * For each entry: unwrap own DEK → wrap for target user's new public key → submit.
 * @param {Array<{secret_id: string, target_user_id: string, target_public_key: string, caller_wrapped_dek: string}>} pendingList
 * @returns {Promise<{performed: number, failed: number}>}
 */
async function performRekeys(pendingList) {
    const privateKey = await getPrivateKey();
    if (!privateKey) {
        console.warn('No private key available for re-keying');
        return { performed: 0, failed: pendingList.length };
    }

    const rekeyEntries = [];
    let failedCount = 0;

    for (const entry of pendingList) {
        try {
            // Unwrap our copy of the DEK
            const dek = await unwrapDEK(entry.caller_wrapped_dek, privateKey);

            // Import the target user's new public key
            const targetPublicKey = await importPublicKey(entry.target_public_key);

            // Wrap the DEK for the target user's new public key
            const wrappedDEK = await wrapDEK(dek, targetPublicKey);

            rekeyEntries.push({
                secret_id: entry.secret_id,
                target_user_id: entry.target_user_id,
                wrapped_dek: wrappedDEK,
            });
        } catch (err) {
            console.error(`Failed to re-key secret ${entry.secret_id} for user ${entry.target_user_id}:`, err);
            failedCount++;
        }
    }

    if (rekeyEntries.length === 0) {
        return { performed: 0, failed: failedCount };
    }

    try {
        const result = await submitRekeys({ rekeys: rekeyEntries });
        const performed = result.rekeyed || 0;
        return { performed, failed: failedCount + (rekeyEntries.length - performed) };
    } catch (err) {
        console.error('Failed to submit re-keys:', err);
        return { performed: 0, failed: failedCount + rekeyEntries.length };
    }
}
