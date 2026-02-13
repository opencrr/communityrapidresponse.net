/**
 * Secret Update Proposals API module
 * Handles encrypted secret update proposals (Signal invite links, Meshtastic channels)
 */

import { get, post, ApiError } from './client.js';

/**
 * Create a secret update proposal for a signal group
 * @param {string} groupId - Signal group UUID
 * @param {Object} data - Proposal data
 * @param {string} data.encrypted_payload - Encrypted payload (base64)
 * @param {string} data.encryption_iv - Encryption IV (base64)
 * @param {Array<{user_id: string, wrapped_dek: string}>} data.wrapped_keys - Wrapped DEKs for admins
 * @param {string} [data.reason] - Reason for the update
 * @returns {Promise<Object>} Proposal response
 */
export async function createSecretProposal(groupId, data) {
    const response = await post(`/signal-groups/${groupId}/secret-proposals`, data);
    return response.proposal || response;
}

/**
 * Vote on a secret update proposal
 * @param {string} proposalId - Proposal UUID
 * @param {boolean} approve - True to approve, false to reject
 * @returns {Promise<Object>} Updated proposal
 */
export async function voteOnProposal(proposalId, approve) {
    const response = await post(`/secret-proposals/${proposalId}/vote`, {
        vote: approve,
    });
    return response.proposal || response;
}

/**
 * Get full details of a secret update proposal
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Proposal details with votes
 */
export async function getProposal(proposalId) {
    return await get(`/secret-proposals/${proposalId}`);
}

/**
 * List secret update proposals the user can see
 * @param {Object} [params] - Query parameters
 * @param {string} [params.status] - Filter by status
 * @param {string} [params.encrypted_secret_id] - Filter by secret
 * @param {string} [params.region_id] - Filter by region
 * @param {string} [params.school_id] - Filter by school
 * @param {string} [params.district_id] - Filter by district
 * @returns {Promise<Object[]>} Array of proposals
 */
export async function listProposals(params = {}) {
    const query = new URLSearchParams();
    if (params.status) query.set('status', params.status);
    if (params.encrypted_secret_id) query.set('encrypted_secret_id', params.encrypted_secret_id);
    if (params.region_id) query.set('region_id', params.region_id);
    if (params.school_id) query.set('school_id', params.school_id);
    if (params.district_id) query.set('district_id', params.district_id);
    const queryString = query.toString();
    const url = queryString ? `/secret-proposals?${queryString}` : '/secret-proposals';
    const response = await get(url);
    return response.proposals || [];
}

/**
 * Expire a secret update proposal (superuser only)
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Result
 */
export async function expireProposal(proposalId) {
    return await post(`/secret-proposals/${proposalId}/expire`);
}

/**
 * Finalize a secret update after consensus is reached
 * Re-encrypts the secret for all members (not just admins)
 * @param {string} secretId - Encrypted secret UUID
 * @param {Object} data - Finalization data
 * @param {string} data.encrypted_payload - Re-encrypted payload
 * @param {string} data.encryption_iv - New IV
 * @param {Array<{user_id: string, wrapped_dek: string}>} data.wrapped_keys - Wrapped DEKs for all members
 * @returns {Promise<Object>} Result
 */
export async function finalizeSecretUpdate(secretId, data) {
    return await post(`/encrypted-secrets/${secretId}/finalize`, data);
}

export default {
    createSecretProposal,
    voteOnProposal,
    getProposal,
    listProposals,
    expireProposal,
    finalizeSecretUpdate,
};
