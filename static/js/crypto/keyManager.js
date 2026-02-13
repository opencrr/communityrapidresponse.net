/**
 * Key Manager - RSA-OAEP keypair lifecycle management
 * Handles generation, IndexedDB storage, and PBKDF2-based backup/restore
 */

const DB_NAME = 'crr-crypto';
const DB_VERSION = 1;
const STORE_NAME = 'keys';
const KEY_ID = 'user-keypair';

const PBKDF2_ITERATIONS = 600000;
const RSA_KEY_SIZE = 2048;

/**
 * Open the IndexedDB database
 */
function openDB() {
    return new Promise((resolve, reject) => {
        const request = indexedDB.open(DB_NAME, DB_VERSION);
        request.onupgradeneeded = () => {
            const db = request.result;
            if (!db.objectStoreNames.contains(STORE_NAME)) {
                db.createObjectStore(STORE_NAME, { keyPath: 'id' });
            }
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

/**
 * Generate a new RSA-OAEP keypair
 * @returns {Promise<CryptoKeyPair>}
 */
export async function generateKeypair() {
    return await crypto.subtle.generateKey(
        {
            name: 'RSA-OAEP',
            modulusLength: RSA_KEY_SIZE,
            publicExponent: new Uint8Array([1, 0, 1]),
            hash: 'SHA-256',
        },
        false, // non-extractable private key
        ['encrypt', 'decrypt', 'wrapKey', 'unwrapKey']
    );
}

/**
 * Store keypair in IndexedDB (non-extractable)
 * @param {CryptoKeyPair} keypair
 */
export async function storeInIndexedDB(keypair) {
    const db = await openDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        const store = tx.objectStore(STORE_NAME);
        store.put({
            id: KEY_ID,
            publicKey: keypair.publicKey,
            privateKey: keypair.privateKey,
        });
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
    });
}

/**
 * Load keypair from IndexedDB
 * @returns {Promise<{publicKey: CryptoKey, privateKey: CryptoKey}|null>}
 */
export async function loadFromIndexedDB() {
    try {
        const db = await openDB();
        return new Promise((resolve, reject) => {
            const tx = db.transaction(STORE_NAME, 'readonly');
            const store = tx.objectStore(STORE_NAME);
            const request = store.get(KEY_ID);
            request.onsuccess = () => {
                if (request.result) {
                    resolve({
                        publicKey: request.result.publicKey,
                        privateKey: request.result.privateKey,
                    });
                } else {
                    resolve(null);
                }
            };
            request.onerror = () => reject(request.error);
        });
    } catch {
        return null;
    }
}

/**
 * Check if a local key exists in IndexedDB
 * @returns {Promise<boolean>}
 */
export async function hasLocalKey() {
    const keys = await loadFromIndexedDB();
    return keys !== null;
}

/**
 * Clear keys from IndexedDB
 */
export async function clearLocalKeys() {
    const db = await openDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        const store = tx.objectStore(STORE_NAME);
        store.delete(KEY_ID);
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
    });
}

/**
 * Export public key as base64-encoded SPKI
 * @param {CryptoKey} publicKey
 * @returns {Promise<string>}
 */
export async function exportPublicKey(publicKey) {
    const exported = await crypto.subtle.exportKey('spki', publicKey);
    return bufferToBase64(exported);
}

/**
 * Import public key from base64-encoded SPKI
 * @param {string} base64Key
 * @returns {Promise<CryptoKey>}
 */
export async function importPublicKey(base64Key) {
    const keyData = base64ToBuffer(base64Key);
    return await crypto.subtle.importKey(
        'spki',
        keyData,
        { name: 'RSA-OAEP', hash: 'SHA-256' },
        true,
        ['encrypt', 'wrapKey']
    );
}

/**
 * Derive an AES-256 wrapping key from a password using PBKDF2
 * @param {string} password
 * @param {Uint8Array} salt
 * @returns {Promise<CryptoKey>}
 */
async function deriveWrappingKey(password, salt) {
    const passwordKey = await crypto.subtle.importKey(
        'raw',
        new TextEncoder().encode(password),
        'PBKDF2',
        false,
        ['deriveKey']
    );

    return await crypto.subtle.deriveKey(
        {
            name: 'PBKDF2',
            salt,
            iterations: PBKDF2_ITERATIONS,
            hash: 'SHA-256',
        },
        passwordKey,
        { name: 'AES-GCM', length: 256 },
        false,
        ['wrapKey', 'unwrapKey']
    );
}

/**
 * Backup private key by wrapping with password-derived key
 * @param {CryptoKey} privateKey - the private key to wrap
 * @param {string} password - user's password
 * @returns {Promise<{wrappedKey: string, salt: string, iv: string}>}
 */
export async function backupPrivateKey(privateKey, password) {
    const salt = crypto.getRandomValues(new Uint8Array(16));
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const wrappingKey = await deriveWrappingKey(password, salt);

    // Export the private key first (need extractable version for wrapping)
    // Since we generated non-extractable keys, we need to re-export via wrapping
    const wrapped = await crypto.subtle.wrapKey(
        'pkcs8',
        privateKey,
        wrappingKey,
        { name: 'AES-GCM', iv }
    );

    return {
        wrappedKey: bufferToBase64(wrapped),
        salt: bufferToBase64(salt),
        iv: bufferToBase64(iv),
    };
}

/**
 * Restore private key from wrapped backup
 * @param {string} wrappedKeyBase64
 * @param {string} saltBase64
 * @param {string} ivBase64
 * @param {string} password
 * @returns {Promise<CryptoKey>}
 */
export async function restorePrivateKey(wrappedKeyBase64, saltBase64, ivBase64, password) {
    const wrappedKey = base64ToBuffer(wrappedKeyBase64);
    const salt = base64ToBuffer(saltBase64);
    const iv = base64ToBuffer(ivBase64);
    const wrappingKey = await deriveWrappingKey(password, salt);

    return await crypto.subtle.unwrapKey(
        'pkcs8',
        wrappedKey,
        wrappingKey,
        { name: 'AES-GCM', iv },
        { name: 'RSA-OAEP', hash: 'SHA-256' },
        false, // non-extractable
        ['decrypt', 'unwrapKey']
    );
}

/**
 * Generate a new keypair suitable for backup (extractable private key for wrapping)
 * @returns {Promise<CryptoKeyPair>}
 */
export async function generateBackupableKeypair() {
    return await crypto.subtle.generateKey(
        {
            name: 'RSA-OAEP',
            modulusLength: RSA_KEY_SIZE,
            publicExponent: new Uint8Array([1, 0, 1]),
            hash: 'SHA-256',
        },
        true, // extractable so we can wrap for backup
        ['encrypt', 'decrypt', 'wrapKey', 'unwrapKey']
    );
}

// --- Utility functions ---

function bufferToBase64(buffer) {
    const bytes = new Uint8Array(buffer);
    let binary = '';
    for (let i = 0; i < bytes.byteLength; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
}

function base64ToBuffer(base64) {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes.buffer;
}

export { bufferToBase64, base64ToBuffer };
