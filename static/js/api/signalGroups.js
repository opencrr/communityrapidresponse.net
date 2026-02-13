/**
 * Signal Groups API module
 */

import { get, post, put, del, ApiError } from './client.js';
import { setSignalGroups } from '../utils/store.js';

/**
 * Get all Signal groups the user has access to
 * Returns empty array if no groups exist (handles 404 gracefully)
 * @param {Object} [params] - Query parameters
 * @param {string} [params.region_id] - Filter by region ID
 * @returns {Promise<Object[]>} Array of Signal groups (with encrypted_secret if verified)
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
 * @param {string} data.encrypted_payload - Encrypted invite link (base64)
 * @param {string} data.encryption_iv - Encryption IV (base64)
 * @param {Array<{user_id: string, wrapped_dek: string}>} data.wrapped_keys - Wrapped DEKs for members
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
    getGroupsByRegion,
    getAdminGroups,
};
