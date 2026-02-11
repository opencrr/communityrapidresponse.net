/**
 * Blocklist API module
 * Handles blocklist proposal and blocklisted address operations.
 */

import { get, post, ApiError } from './client.js';

/**
 * Get all blocklist proposals the user can see
 * Admins see proposals for their regions, superusers see all
 * @param {Object} [params] - Query parameters
 * @param {string} [params.status] - Filter by status (pending, approved, expired)
 * @param {string} [params.region_id] - Filter by region
 * @returns {Promise<Object[]>} Array of proposals
 */
export async function getBlocklistProposals(params = {}) {
    const query = new URLSearchParams();
    if (params.status) query.set('status', params.status);
    if (params.region_id) query.set('region_id', params.region_id);
    const queryString = query.toString();
    const url = queryString ? `/blocklist-proposals?${queryString}` : '/blocklist-proposals';
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
 * Get full details of a blocklist proposal
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Proposal details with votes
 */
export async function getBlocklistProposalDetails(proposalId) {
    return await get(`/blocklist-proposals/${proposalId}`);
}

/**
 * Create a blocklist proposal for a user in a region
 * @param {string} regionId - Region UUID
 * @param {string} targetUserId - User to blocklist
 * @param {string} reason - Reason for blocklisting
 * @param {string} [evidence] - Optional evidence
 * @returns {Promise<Object>} Created proposal
 */
export async function createBlocklistProposal(regionId, targetUserId, reason, evidence = null) {
    const body = {
        target_user_id: targetUserId,
        reason,
    };
    if (evidence) {
        body.evidence = evidence;
    }
    const response = await post(`/communities/${regionId}/blocklist-proposals`, body);
    return response.proposal || response;
}

/**
 * Vote on a blocklist proposal
 * @param {string} proposalId - Proposal UUID
 * @param {boolean} approve - True to approve, false to reject
 * @returns {Promise<Object>} Updated proposal
 */
export async function voteOnBlocklistProposal(proposalId, approve) {
    const response = await post(`/blocklist-proposals/${proposalId}/vote`, {
        vote: approve,
    });
    return response.proposal || response;
}

/**
 * Expire a blocklist proposal (superuser only)
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Result
 */
export async function expireBlocklistProposal(proposalId) {
    return await post(`/blocklist-proposals/${proposalId}/expire`);
}

/**
 * Get all blocklisted addresses (superuser only)
 * @param {boolean} [activeOnly=true] - Only return active (non-expired) addresses
 * @returns {Promise<Object[]>} Array of blocklisted addresses
 */
export async function getBlocklistedAddresses(activeOnly = true) {
    const url = activeOnly ? '/admin/blocklisted-addresses?active=true' : '/admin/blocklisted-addresses';
    try {
        const response = await get(url);
        return response.addresses || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Expire a blocklisted address early (superuser only)
 * @param {string} addressHash - Address hash to expire
 * @returns {Promise<Object>} Result
 */
export async function expireBlocklistedAddress(addressHash) {
    return await post(`/admin/blocklisted-addresses/${addressHash}/expire`);
}

/**
 * Get users in a region (for proposal creation)
 * @param {string} regionId - Region UUID
 * @returns {Promise<Object[]>} Array of users in the region
 */
export async function getRegionUsers(regionId) {
    try {
        const response = await get(`/communities/${regionId}/users`);
        return response.users || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

export default {
    getBlocklistProposals,
    getBlocklistProposalDetails,
    createBlocklistProposal,
    voteOnBlocklistProposal,
    expireBlocklistProposal,
    getBlocklistedAddresses,
    expireBlocklistedAddress,
    getRegionUsers,
};
