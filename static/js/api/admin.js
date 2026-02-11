/**
 * Admin API module
 * Handles superuser-specific operations like granting vouch verification.
 */

import { get, post, del, ApiError } from './client.js?v=c49aed9';

/**
 * Search for users by email or username
 * @param {string} query - Search query
 * @returns {Promise<Object[]>} List of matching users
 */
export async function searchUsers(query) {
    try {
        const response = await get('/admin/users', { q: query });
        return response.users || response || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get user details by ID
 * @param {string} userId - User ID
 * @returns {Promise<Object>} User details
 */
export async function getUser(userId) {
    return get(`/admin/users/${userId}`);
}

/**
 * Grant vouch verification to a user (superuser only)
 * This bypasses the normal vouch process and directly grants vouch_verified status
 * @param {string} userId - User ID to grant vouch verification to
 * @returns {Promise<Object>} Updated user object
 */
export async function grantVouchVerification(userId) {
    return post(`/admin/users/${userId}/grant-vouch`);
}

/**
 * Revoke vouch verification from a user (superuser only)
 * @param {string} userId - User ID to revoke vouch verification from
 * @returns {Promise<Object>} Updated user object
 */
export async function revokeVouchVerification(userId) {
    return post(`/admin/users/${userId}/revoke-vouch`);
}

/**
 * Get all users with pagination (superuser only)
 * @param {Object} params - Query parameters
 * @param {number} [params.page=1] - Page number
 * @param {number} [params.limit=20] - Items per page
 * @param {string} [params.filter] - Filter by status (all, verified, unverified)
 * @returns {Promise<Object>} Paginated users response
 */
export async function getUsers(params = {}) {
    try {
        const response = await get('/admin/users', params);
        return {
            users: response.users || [],
            total: response.total || 0,
            page: response.page || 1,
            limit: response.limit || 20,
        };
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return { users: [], total: 0, page: 1, limit: 20 };
        }
        throw error;
    }
}

/**
 * Block a user (superuser only)
 * @param {string} userId - User ID to block
 * @param {string} [reason] - Optional reason for blocking
 * @returns {Promise<Object>} Result
 */
export async function blockUser(userId, reason = '') {
    return post(`/admin/users/${userId}/block`, { reason });
}

/**
 * Unblock a user (superuser only)
 * @param {string} userId - User ID to unblock
 * @returns {Promise<Object>} Result
 */
export async function unblockUser(userId) {
    return post(`/admin/users/${userId}/unblock`);
}

/**
 * Permanently delete a user (superuser only)
 * @param {string} userId - User ID to delete
 * @returns {Promise<Object>} Result
 */
export async function deleteUser(userId) {
    return del(`/admin/users/${userId}`);
}

/**
 * Get audit logs with filters (superuser only)
 * @param {Object} params - Query parameters
 * @param {string} [params.user_id] - Filter by user ID
 * @param {string} [params.action] - Filter by action(s), comma-separated
 * @param {string} [params.resource_type] - Filter by resource type
 * @param {string} [params.resource_id] - Filter by resource ID
 * @param {string} [params.date_from] - Start date (ISO 8601)
 * @param {string} [params.date_to] - End date (ISO 8601)
 * @param {string} [params.ip_address] - Filter by IP address
 * @param {number} [params.page=1] - Page number
 * @param {number} [params.limit=50] - Items per page
 * @returns {Promise<Object>} Paginated audit logs response
 */
export async function getAuditLogs(params = {}) {
    const response = await get('/admin/audit-logs', params);
    return {
        logs: response.logs || [],
        total: response.total || 0,
        page: response.page || 1,
        limit: response.limit || 50,
        has_more: response.has_more || false,
    };
}

/**
 * Export audit logs (superuser only)
 * @param {Object} params - Query parameters (same as getAuditLogs)
 * @param {string} [format='json'] - Export format ('json' or 'csv')
 * @returns {Promise<Blob>} File blob for download
 */
export async function exportAuditLogs(params = {}, format = 'json') {
    const queryParams = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined && value !== null && value !== '') {
            queryParams.append(key, value);
        }
    });
    queryParams.append('format', format);

    const response = await fetch(`/api/v1/admin/audit-logs/export?${queryParams}`, {
        credentials: 'include',
    });

    if (!response.ok) {
        throw new ApiError('Failed to export audit logs', response.status);
    }

    return response.blob();
}

export default {
    searchUsers,
    getUser,
    grantVouchVerification,
    revokeVouchVerification,
    getUsers,
    blockUser,
    unblockUser,
    deleteUser,
    getAuditLogs,
    exportAuditLogs,
};
