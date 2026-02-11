/**
 * Signal Groups API module
 */

import { get, post, put, del, ApiError } from './client.js?v=c49aed9';
import { setSignalGroups } from '../utils/store.js?v=c49aed9';

/**
 * Get all Signal groups the user has access to
 * Returns empty array if no groups exist (handles 404 gracefully)
 * @param {Object} [params] - Query parameters
 * @param {string} [params.region_id] - Filter by region ID
 * @returns {Promise<Object[]>} Array of Signal groups
 */
export async function getGroups(params = {}) {
    try {
        const response = await get('/signal-groups', params);
        const groups = response.groups || response || [];
        setSignalGroups(groups);
        return groups;
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            setSignalGroups([]);
            return [];
        }
        throw error;
    }
}

/**
 * Get a single Signal group by ID
 * @param {string} groupId - Group UUID
 * @returns {Promise<Object>} Group data
 */
export async function getGroup(groupId) {
    const response = await get(`/signal-groups/${groupId}`);
    return response.group || response;
}

/**
 * Create a new Signal group (admin only)
 * @param {Object} data - Group data
 * @param {string} data.name - Group name
 * @param {string} data.region_id - Associated region UUID
 * @param {string} data.invite_link - Signal invite link
 * @param {string} [data.description] - Group description
 * @returns {Promise<Object>} Created group
 */
export async function createGroup(data) {
    const response = await post('/signal-groups', data);
    return response.group || response;
}

/**
 * Update a Signal group (admin only)
 * @param {string} groupId - Group UUID
 * @param {Object} data - Updated group data
 * @returns {Promise<Object>} Updated group
 */
export async function updateGroup(groupId, data) {
    const response = await put(`/signal-groups/${groupId}`, data);
    return response.group || response;
}

/**
 * Delete a Signal group (requires consensus)
 * @param {string} groupId - Group UUID
 * @returns {Promise<void>}
 */
export async function deleteGroup(groupId) {
    await del(`/signal-groups/${groupId}`);
}

/**
 * Propose a new invite link for a Signal group
 * @param {string} groupId - Group UUID
 * @param {string} newInviteLink - New Signal invite link
 * @returns {Promise<Object>} Proposal data
 */
export async function proposeInviteLinkUpdate(groupId, newInviteLink) {
    const response = await post(`/signal-groups/${groupId}/invite-link-proposals`, {
        invite_link: newInviteLink,
    });
    return response.proposal || response;
}

/**
 * Get pending invite link proposals for a group
 * Returns empty array if no proposals (handles 404 gracefully)
 * @param {string} groupId - Group UUID
 * @returns {Promise<Object[]>} Array of proposals
 */
export async function getInviteLinkProposals(groupId) {
    try {
        const response = await get(`/signal-groups/${groupId}/invite-link-proposals`);
        return response.proposals || response || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Vote on an invite link proposal
 * @param {string} proposalId - Proposal UUID
 * @param {boolean} approve - True to approve, false to reject
 * @returns {Promise<Object>} Updated proposal
 */
export async function voteOnInviteLinkProposal(proposalId, approve) {
    const response = await post(`/signal-groups/invite-link-proposals/${proposalId}/vote`, {
        vote: approve,
    });
    return response.proposal || response;
}

/**
 * Get all invite link proposals the user can see
 * Admins see proposals for their regions, superusers see all
 * @param {Object} [params] - Query parameters
 * @param {string} [params.status] - Filter by status (pending, approved, expired, rejected)
 * @param {string} [params.signal_group_id] - Filter by signal group
 * @param {string} [params.region_id] - Filter by region
 * @returns {Promise<Object[]>} Array of proposals
 */
export async function getAllInviteLinkProposals(params = {}) {
    const query = new URLSearchParams();
    if (params.status) query.set('status', params.status);
    if (params.signal_group_id) query.set('signal_group_id', params.signal_group_id);
    if (params.region_id) query.set('region_id', params.region_id);
    const queryString = query.toString();
    const url = queryString ? `/signal-groups/invite-link-proposals?${queryString}` : '/signal-groups/invite-link-proposals';
    const response = await get(url);
    return response.proposals || [];
}

/**
 * Get full details of an invite link proposal
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Proposal details with votes
 */
export async function getInviteLinkProposalDetails(proposalId) {
    return await get(`/signal-groups/invite-link-proposals/${proposalId}`);
}

/**
 * Expire an invite link proposal (superuser only)
 * @param {string} proposalId - Proposal UUID
 * @returns {Promise<Object>} Result
 */
export async function expireInviteLinkProposal(proposalId) {
    return await post(`/signal-groups/invite-link-proposals/${proposalId}/expire`);
}

/**
 * Get Signal groups for a specific region
 * Returns empty array if no groups (handles 404 gracefully)
 * @param {string} regionId - Region UUID
 * @returns {Promise<Object[]>} Array of groups in the region
 */
export async function getGroupsByRegion(regionId) {
    try {
        return await getGroups({ region_id: regionId });
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get groups the current user administers
 * Returns empty array if no groups (handles 404 gracefully)
 * @returns {Promise<Object[]>} Array of admin groups
 */
export async function getAdminGroups() {
    try {
        const response = await get('/signal-groups/admin');
        return response.groups || response || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

export default {
    getGroups,
    getGroup,
    createGroup,
    updateGroup,
    deleteGroup,
    proposeInviteLinkUpdate,
    getInviteLinkProposals,
    voteOnInviteLinkProposal,
    getAllInviteLinkProposals,
    getInviteLinkProposalDetails,
    expireInviteLinkProposal,
    getGroupsByRegion,
    getAdminGroups,
};
