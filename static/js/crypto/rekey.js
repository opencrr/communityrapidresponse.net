/**
 * Re-keying module
 * Handles automatic re-keying when other users rotate their encryption keys.
 * After a user rotates their keys (e.g. password reset), their wrapped DEKs
 * are flagged for re-keying. Other members who share secrets with that user
 * unwrap their own copy of the DEK and re-wrap it for the user's new public key.
 *
 * Also handles group/connection secret rotation when a member leaves:
 * Surviving members generate a fresh DEK, re-encrypt the payload, and wrap
 * the fresh DEK for all survivors.
 */

import { importPublicKey } from './keyManager.js';
import { unwrapDEK, wrapDEK, generateDEK, decryptPayload, encryptPayload, wrapDEKForRecipients } from './envelope.js';
import { getPendingRekeys, submitRekeys } from '../api/encryption.js';
import { getPrivateKey } from './index.js';

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

/**
 * Rotate the DEK for a group/connection secret.
 * When a group is removed from a connection, surviving members need to:
 * 1. Generate a fresh DEK
 * 2. Decrypt the current payload with their wrapped DEK
 * 3. Re-encrypt the payload with the fresh DEK
 * 4. Wrap the fresh DEK for all surviving members
 * 5. Submit the new payload + wrapped keys
 *
 * @param {Object} params
 * @param {string} params.secretId - Secret UUID
 * @param {string} params.encryptedPayload - Current encrypted payload (base64)
 * @param {string} params.encryptionIv - Current encryption IV (base64)
 * @param {string} params.callerWrappedDek - Caller's wrapped DEK
 * @param {Array<{user_id: string, public_key: string}>} params.survivors - Surviving members with public keys
 * @returns {Promise<{success: boolean, message?: string}>}
 */
export async function performGroupRotation(params) {
    try {
        const { secretId, encryptedPayload, encryptionIv, callerWrappedDek, survivors } = params;

        if (!secretId || !encryptedPayload || !encryptionIv || !callerWrappedDek || !survivors || survivors.length === 0) {
            throw new Error('Missing required parameters for group rotation');
        }

        const privateKey = await getPrivateKey();
        if (!privateKey) {
            throw new Error('No private key available for rotation');
        }

        // Step 1: Unwrap the caller's DEK
        const currentDek = await unwrapDEK(callerWrappedDek, privateKey);

        // Step 2: Decrypt the current payload
        const plaintext = await decryptPayload(encryptedPayload, encryptionIv, currentDek);

        // Step 3: Generate a fresh DEK
        const freshDek = await generateDEK();

        // Step 4: Re-encrypt the payload with the fresh DEK
        const { ciphertext: newPayload, iv: newIv } = await encryptPayload(plaintext, freshDek);

        // Step 5: Wrap the fresh DEK for all survivors
        const wrappedKeys = await wrapDEKForRecipients(freshDek, survivors);

        // Step 6: Build the rekey entries
        const rekeyEntries = wrappedKeys.map(wk => ({
            secret_id: secretId,
            target_user_id: wk.user_id,
            wrapped_dek: wk.wrapped_dek,
        }));

        // Step 7: Submit the rotation
        const result = await submitRekeys({
            rekeys: rekeyEntries,
            encrypted_payload: newPayload,
            encryption_iv: newIv,
        });

        if (!result || result.rekeyed === 0) {
            return { success: false, message: 'No survivors updated' };
        }

        return { success: true, message: `Rotated DEK for ${result.rekeyed} survivor(s)` };
    } catch (err) {
        console.error('Failed to perform group rotation:', err);
        throw err;
    }
}
