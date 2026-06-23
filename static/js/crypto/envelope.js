/**
 * Envelope Encryption - AES-256-GCM DEK + RSA-OAEP key wrapping
 * Encrypts secrets with a random DEK, then wraps the DEK for each member's public key
 */

import { importPublicKey, bufferToBase64, base64ToBuffer } from './keyManager.js';

/**
 * Generate a random AES-256-GCM Data Encryption Key (DEK)
 * @returns {Promise<CryptoKey>}
 */
export async function generateDEK() {
    return await crypto.subtle.generateKey(
        { name: 'AES-GCM', length: 256 },
        true, // extractable so we can wrap it
        ['encrypt', 'decrypt']
    );
}

/**
 * Encrypt plaintext with AES-256-GCM DEK
 * @param {string} plaintext
 * @param {CryptoKey} dek
 * @returns {Promise<{ciphertext: string, iv: string}>}
 */
export async function encryptPayload(plaintext, dek) {
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const encoded = new TextEncoder().encode(plaintext);

    const ciphertext = await crypto.subtle.encrypt(
        { name: 'AES-GCM', iv },
        dek,
        encoded
    );

    return {
        ciphertext: bufferToBase64(ciphertext),
        iv: bufferToBase64(iv),
    };
}

/**
 * Decrypt ciphertext with AES-256-GCM DEK
 * @param {string} ciphertextBase64
 * @param {string} ivBase64
 * @param {CryptoKey} dek
 * @returns {Promise<string>}
 */
export async function decryptPayload(ciphertextBase64, ivBase64, dek) {
    const ciphertext = base64ToBuffer(ciphertextBase64);
    const iv = base64ToBuffer(ivBase64);

    const decrypted = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv },
        dek,
        ciphertext
    );

    return new TextDecoder().decode(decrypted);
}

/**
 * Wrap a DEK with an RSA-OAEP public key
 * @param {CryptoKey} dek
 * @param {CryptoKey} publicKey - RSA-OAEP public key
 * @returns {Promise<string>} base64-encoded wrapped DEK
 */
export async function wrapDEK(dek, publicKey) {
    const wrapped = await crypto.subtle.wrapKey(
        'raw',
        dek,
        publicKey,
        { name: 'RSA-OAEP' }
    );
    return bufferToBase64(wrapped);
}

/**
 * Unwrap a DEK with an RSA-OAEP private key
 * @param {string} wrappedDekBase64
 * @param {CryptoKey} privateKey - RSA-OAEP private key
 * @returns {Promise<CryptoKey>}
 */
export async function unwrapDEK(wrappedDekBase64, privateKey) {
    const wrappedDek = base64ToBuffer(wrappedDekBase64);

    return await crypto.subtle.unwrapKey(
        'raw',
        wrappedDek,
        privateKey,
        { name: 'RSA-OAEP' },
        { name: 'AES-GCM', length: 256 },
        true, // extractable so we can re-wrap for re-keying
        ['encrypt', 'decrypt']
    );
}

/**
 * Encrypt a secret for multiple members using envelope encryption
 * @param {string} plaintext - the secret to encrypt
 * @param {Array<{user_id: string, public_key: string}>} memberPublicKeys - base64-encoded SPKI keys
 * @returns {Promise<{ciphertext: string, iv: string, wrappedKeys: Array<{user_id: string, wrapped_dek: string}>}>}
 */
export async function encryptForMembers(plaintext, memberPublicKeys) {
    const dek = await generateDEK();
    const { ciphertext, iv } = await encryptPayload(plaintext, dek);

    const wrappedKeys = [];
    for (const member of memberPublicKeys) {
        const publicKey = await importPublicKey(member.public_key);
        const wrappedDek = await wrapDEK(dek, publicKey);
        wrappedKeys.push({
            user_id: member.user_id,
            wrapped_dek: wrappedDek,
        });
    }

    return { ciphertext, iv, wrappedKeys };
}

/**
 * Decrypt a secret using envelope encryption
 * @param {string} ciphertextBase64 - encrypted payload
 * @param {string} ivBase64 - encryption IV
 * @param {string} wrappedDekBase64 - user's wrapped DEK
 * @param {CryptoKey} privateKey - user's RSA-OAEP private key
 * @returns {Promise<string>} decrypted plaintext
 */
export async function decryptSecret(ciphertextBase64, ivBase64, wrappedDekBase64, privateKey) {
    const dek = await unwrapDEK(wrappedDekBase64, privateKey);
    return await decryptPayload(ciphertextBase64, ivBase64, dek);
}

/**
 * Wrap a DEK for multiple recipients using their public keys
 * @param {CryptoKey} dek - Data Encryption Key to wrap
 * @param {Array<{user_id: string, public_key: string}>} recipientPublicKeys - base64-encoded SPKI keys
 * @returns {Promise<Array<{user_id: string, wrapped_dek: string}>>}
 */
export async function wrapDEKForRecipients(dek, recipientPublicKeys) {
    const wrappedKeys = [];
    for (const recipient of recipientPublicKeys) {
        const publicKey = await importPublicKey(recipient.public_key);
        const wrappedDek = await wrapDEK(dek, publicKey);
        wrappedKeys.push({
            user_id: recipient.user_id,
            wrapped_dek: wrappedDek,
        });
    }
    return wrappedKeys;
}
