/**
 * Membership API client for sub-region membership requests and invitations.
 */

import { get, post, del } from './client.js?v=c49aed9';

/**
 * Request to join a sub-region
 * @param {string} regionId - The sub-region ID to join
 * @returns {Promise<Object>} The created membership request
 */
export async function requestMembership(regionId) {
    return post(`/communities/${regionId}/membership-requests`, {});
}

/**
 * List the current user's membership requests
 * @param {Object} [filter] - Optional filter
 * @param {string} [filter.status] - Filter by status
 * @param {string} [filter.type] - Filter by type ('request' or 'invitation')
 * @returns {Promise<Object>} List of membership requests
 */
export async function listMyRequests(filter = {}) {
    return get('/membership-requests', filter);
}

/**
 * Cancel a pending membership request
 * @param {string} requestId - The request ID to cancel
 * @returns {Promise<Object>} Cancellation result
 */
export async function cancelRequest(requestId) {
    return del(`/membership-requests/${requestId}`);
}

/**
 * Get details of a specific membership request
 * @param {string} requestId - The request ID
 * @returns {Promise<Object>} Request details with votes
 */
export async function getRequest(requestId) {
    return get(`/membership-requests/${requestId}`);
}

/**
 * List pending membership requests for regions where user is admin
 * @returns {Promise<Object>} List of pending requests
 */
export async function listPendingForAdmin() {
    return get('/membership-requests/admin');
}

/**
 * List pending membership requests for a specific region
 * @param {string} regionId - The region ID
 * @returns {Promise<Object>} List of pending requests
 */
export async function listPendingForRegion(regionId) {
    return get(`/communities/${regionId}/membership-requests`);
}

/**
 * Vote on a membership request
 * @param {string} requestId - The request ID
 * @param {boolean} approve - True to approve, false to reject
 * @returns {Promise<Object>} Vote result
 */
export async function voteOnRequest(requestId, approve) {
    return post(`/membership-requests/${requestId}/vote`, { vote: approve });
}

/**
 * Invite a user to join a sub-region
 * @param {string} regionId - The sub-region ID
 * @param {string} userId - The user ID to invite
 * @returns {Promise<Object>} The created invitation
 */
export async function createInvitation(regionId, userId) {
    return post(`/communities/${regionId}/invitations`, { user_id: userId });
}

/**
 * List pending invitations for the current user
 * @returns {Promise<Object>} List of invitations
 */
export async function listMyInvitations() {
    return get('/invitations');
}

/**
 * Respond to an invitation
 * @param {string} invitationId - The invitation ID
 * @param {boolean} accept - True to accept, false to decline
 * @returns {Promise<Object>} Response result
 */
export async function respondToInvitation(invitationId, accept) {
    return post(`/invitations/${invitationId}/respond`, { accept });
}

export default {
    requestMembership,
    listMyRequests,
    cancelRequest,
    getRequest,
    listPendingForAdmin,
    listPendingForRegion,
    voteOnRequest,
    createInvitation,
    listMyInvitations,
    respondToInvitation,
};
