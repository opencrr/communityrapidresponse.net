/**
 * High-level Crypto API
 * Combines key management and envelope encryption into a simple interface
 */

import {
    generateBackupableKeypair,
    storeInIndexedDB,
    loadFromIndexedDB,
    hasLocalKey,
    clearLocalKeys,
    exportPublicKey,
    backupPrivateKey,
    restorePrivateKey,
    importPublicKey,
} from './keyManager.js';

import {
    encryptForMembers,
    decryptSecret,
    generateDEK,
    aesGcmEncrypt,
    wrapDEK,
    unwrapDEK,
    wrapDEKForRecipients,
} from './envelope.js';

import * as encryptionAPI from '../api/encryption.js';

/**
 * Initialize encryption for a new user (during registration)
 * Generates keypair, stores locally, backs up to server
 * @param {string} password - user's password for key backup
 * @returns {Promise<boolean>} true if successful
 */
export async function initializeEncryption(password) {
    try {
        const keypair = await generateBackupableKeypair();
        const publicKeyBase64 = await exportPublicKey(keypair.publicKey);
        const backup = await backupPrivateKey(keypair.privateKey, password);

        // Upload to server
        await encryptionAPI.uploadKeys({
            public_key: publicKeyBase64,
            wrapped_private_key: backup.wrappedKey,
            key_salt: backup.salt,
            key_iv: backup.iv,
        });

        // Store in IndexedDB
        await storeInIndexedDB(keypair);

        return true;
    } catch (err) {
        console.error('Failed to initialize encryption:', err);
        return false;
    }
}

/**
 * Restore encryption keys on login (when IndexedDB is empty)
 * Fetches wrapped backup from server, unwraps with password
 * @param {string} password - user's password
 * @returns {Promise<boolean>} true if successful
 */
export async function restoreEncryption(password) {
    try {
        const response = await encryptionAPI.getKeys();
        if (!response || !response.wrapped_private_key) {
            return false;
        }

        const privateKey = await restorePrivateKey(
            response.wrapped_private_key,
            response.key_salt,
            response.key_iv,
            password
        );

        const publicKey = await importPublicKey(response.public_key);

        await storeInIndexedDB({ publicKey, privateKey });
        return true;
    } catch (err) {
        console.error('Failed to restore encryption keys:', err);
        return false;
    }
}

/**
 * Re-wrap private key backup with a new password (on password change)
 * @param {string} newPassword
 * @returns {Promise<boolean>}
 */
export async function rewrapBackup(newPassword) {
    try {
        const keypair = await loadFromIndexedDB();
        if (!keypair) {
            return false;
        }

        const backup = await backupPrivateKey(keypair.privateKey, newPassword);
        await encryptionAPI.updateKeys({
            wrapped_private_key: backup.wrappedKey,
            key_salt: backup.salt,
            key_iv: backup.iv,
        });

        return true;
    } catch (err) {
        console.error('Failed to re-wrap encryption backup:', err);
        return false;
    }
}

/**
 * Rotate encryption keys (on password reset — generates new keypair)
 * @param {string} newPassword
 * @returns {Promise<boolean>}
 */
export async function rotateKeys(newPassword) {
    try {
        await clearLocalKeys();

        const keypair = await generateBackupableKeypair();
        const publicKeyBase64 = await exportPublicKey(keypair.publicKey);
        const backup = await backupPrivateKey(keypair.privateKey, newPassword);

        await encryptionAPI.rotateKeys({
            public_key: publicKeyBase64,
            wrapped_private_key: backup.wrappedKey,
            key_salt: backup.salt,
            key_iv: backup.iv,
        });

        await storeInIndexedDB(keypair);
        return true;
    } catch (err) {
        console.error('Failed to rotate encryption keys:', err);
        return false;
    }
}

/**
 * Get the user's private key from IndexedDB
 * @returns {Promise<CryptoKey|null>}
 */
export async function getPrivateKey() {
    const keypair = await loadFromIndexedDB();
    return keypair ? keypair.privateKey : null;
}

/**
 * Get the user's public key from IndexedDB
 * @returns {Promise<CryptoKey|null>}
 */
export async function getPublicKey() {
    const keypair = await loadFromIndexedDB();
    return keypair ? keypair.publicKey : null;
}

/**
 * Check if the user has a local encryption key
 * @returns {Promise<boolean>}
 */
export { hasLocalKey, clearLocalKeys };

// Re-export envelope encryption functions for direct use
export { encryptForMembers, decryptSecret, generateDEK, aesGcmEncrypt, wrapDEK, unwrapDEK, wrapDEKForRecipients, importPublicKey, exportPublicKey };
