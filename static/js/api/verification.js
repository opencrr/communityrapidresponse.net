/**
 * Verification API module
 * Handles postcard verification flows.
 */

import { get, post } from './client.js';

/**
 * Request postcard verification
 * Note: Address is processed in memory and NEVER stored in the database
 * @param {Object} data - Verification request data
 * @param {string} data.region_id - Region UUID to verify for
 * @param {Object} data.address - Address object
 * @param {string} data.address.line1 - Street address
 * @param {string} data.address.city - City
 * @param {string} data.address.state - State (2-letter code)
 * @param {string} data.address.postal_code - ZIP/postal code
 * @param {string} [data.address.line2] - Unit/apartment number
 * @returns {Promise<Object>} Verification request result
 */
export async function requestPostcardVerification(data) {
    const response = await post('/verification/postcard/request', data);
    return response;
}

/**
 * Verify postcard code
 * @param {string} code - Verification code from postcard
 * @returns {Promise<Object>} Verification result
 */
export async function verifyPostcardCode(code) {
    const response = await post('/verification/postcard/verify', { verification_code: code });
    return response;
}

/**
 * Get current verification status
 * @returns {Promise<Object>} Verification status
 */
export async function getVerificationStatus() {
    const response = await get('/verification/status');
    return response;
}

/**
 * Cancel pending verification request
 * @returns {Promise<void>}
 */
export async function cancelVerificationRequest() {
    await post('/verification/cancel');
}

/**
 * Resend postcard (if within limits)
 * @returns {Promise<Object>} Resend result
 */
export async function resendPostcard() {
    const response = await post('/verification/postcard/resend');
    return response;
}

export default {
    requestPostcardVerification,
    verifyPostcardCode,
    getVerificationStatus,
    cancelVerificationRequest,
    resendPostcard,
};
