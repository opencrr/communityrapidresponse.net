/**
 * Verification API module
 * Handles postcard and vouch verification flows.
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
 * Request vouch verification by specifying city and state
 * Creates a pending user_region entry for the matching region
 * @param {string} city - City name to request vouch for
 * @param {string} state - State code (2-letter) to request vouch for
 * @returns {Promise<Object>} Vouch request result with region info
 */
export async function requestVouchVerification(city, state) {
    const response = await post('/verification/vouch/request', {
        city,
        state,
    });
    return response;
}

/**
 * Vouch for another user (Tier 1 only)
 * @param {string} userId - UUID of user to vouch for
 * @returns {Promise<Object>} Vouch result
 */
export async function vouchForUser(userId) {
    const response = await post('/verification/vouch', {
        user_id: userId,
    });
    return response;
}

/**
 * Get pending vouch requests (Tier 1 only)
 * @returns {Promise<Object[]>} Array of pending vouch requests
 */
export async function getPendingVouchRequests() {
    const response = await get('/verification/vouch/pending');
    return response.requests || response || [];
}

/**
 * Get users who have vouched for current user
 * @returns {Promise<Object[]>} Array of vouchers
 */
export async function getMyVouchers() {
    const response = await get('/verification/vouch/my-vouchers');
    return response.vouchers || response || [];
}

/**
 * Get users current user has vouched for (Tier 1 only)
 * @returns {Promise<Object[]>} Array of vouchees
 */
export async function getMyVouchees() {
    const response = await get('/verification/vouch/my-vouchees');
    return response.vouchees || response || [];
}

/**
 * Get remaining vouches for current month (Tier 1 only)
 * @returns {Promise<Object>} Vouch limit info
 */
export async function getVouchLimitStatus() {
    const response = await get('/verification/vouch/limit');
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

/**
 * Get vouch status for a specific user and region
 * @param {string} userId - User ID to get vouch status for
 * @param {string} regionId - Region ID to check vouches in
 * @returns {Promise<Object>} Vouch status with count and vouchers
 */
export async function getVouchStatus(userId, regionId) {
    const response = await get(`/verification/vouch/status/${userId}?region_id=${regionId}`);
    return response;
}

export default {
    requestPostcardVerification,
    verifyPostcardCode,
    getVerificationStatus,
    requestVouchVerification,
    vouchForUser,
    getPendingVouchRequests,
    getMyVouchers,
    getMyVouchees,
    getVouchLimitStatus,
    getVouchStatus,
    cancelVerificationRequest,
    resendPostcard,
};
