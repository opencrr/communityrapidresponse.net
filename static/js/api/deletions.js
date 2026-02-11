/**
 * Deletion Proposals API module
 * Handles deletion proposal operations for signal groups and sub-regions.
 */

import { get, post, ApiError } from './client.js?v=c49aed9';

/**
 * Get all deletion proposals the user can see
 * Admins see proposals for their scopes, superusers see all
 * @param {Object} [params] - Query parameters
 * @param {string} [params.status] - Filter by status (pending, approved, expired)
 * @param {string} [params.region_id] - Filter by region
 * @returns {Promise<Object[]>} Array of proposals
 */
export async function getDeletionProposals(params = {}) {
    const query = new URLSearchParams();
    if (params.status) query.set('status', params.status);
    if (params.region_id) query.set('region_id', params.region_id);
    const queryString = query.toString();
    const url = queryString ? `/deletion-proposals?${queryString}` : '/deletion-proposals';
    try {
        const response = await get(url);
        return response.proposals || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get full details of a deletion proposal
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Proposal details with votes
 */
export async function getDeletionProposalDetails(proposalId) {
    return await get(`/deletion-proposals/${proposalId}`);
}

/**
 * Create a deletion proposal
 * @param {Object} data - Proposal data
 * @param {string} data.asset_type - "signal_group" or "sub_region"
 * @param {string} data.asset_id - Asset UUID
 * @param {string} data.reason - Reason for deletion
 * @returns {Promise<Object>} Created proposal
 */
export async function createDeletionProposal(data) {
    const response = await post('/deletion-proposals', data);
    return response.proposal || response;
}

/**
 * Vote on a deletion proposal
 * @param {string} proposalId - Proposal UUID
 * @param {boolean} approve - True to approve, false to reject
 * @returns {Promise<Object>} Updated proposal
 */
export async function voteOnDeletionProposal(proposalId, approve) {
    const response = await post(`/deletion-proposals/${proposalId}/vote`, {
        vote: approve,
    });
    return response.proposal || response;
}

/**
 * Expire a deletion proposal (superuser only)
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Result
 */
export async function expireDeletionProposal(proposalId) {
    return await post(`/deletion-proposals/${proposalId}/expire`);
}

export default {
    getDeletionProposals,
    getDeletionProposalDetails,
    createDeletionProposal,
    voteOnDeletionProposal,
    expireDeletionProposal,
};
