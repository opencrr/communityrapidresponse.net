/**
 * Authentication API module
 */

import { get, post, del, ApiError } from './client.js';
import { setUser, clearUser } from '../utils/store.js';

/**
 * Register a new user
 * @param {Object} data - Registration data
 * @param {string} data.username - Username (visible to other users)
 * @param {string} data.email - User email
 * @param {string} data.password - User password (min 12 characters)
 * @returns {Promise<Object>} User data
 */
export async function register(data) {
    const response = await post('/auth/register', data);
    if (response.user) {
        setUser(response.user);
    }
    return response;
}

/**
 * Log in a user
 * @param {Object} credentials - Login credentials
 * @param {string} credentials.email - User email
 * @param {string} credentials.password - User password
 * @returns {Promise<Object>} User data
 */
export async function login(credentials) {
    const response = await post('/auth/login', credentials);
    if (response.user) {
        setUser(response.user);
    }
    return response;
}

/**
 * Log out the current user
 * @returns {Promise<void>}
 */
export async function logout() {
    try {
        await post('/auth/logout');
    } finally {
        clearUser();
    }
}

/**
 * Get the current authenticated user
 * @returns {Promise<Object|null>} User data or null if not authenticated
 */
export async function getCurrentUser() {
    try {
        const response = await get('/users/me');
        if (response.user) {
            setUser(response.user);
            return response.user;
        }
        return null;
    } catch (error) {
        if (error instanceof ApiError) {
            // Handle 401 - user not authenticated
            if (error.status === 401) {
                clearUser();
                return null;
            }
            // Handle 403 with email_verification_required - user is logged in but hasn't verified email
            // This is expected on /verify-email and should not be treated as an authentication failure
            if (error.status === 403 && error.data?.error === 'email_verification_required') {
                clearUser();
                return null;
            }
        }
        throw error;
    }
}

/**
 * Check if user is authenticated (makes API call)
 * @returns {Promise<boolean>}
 */
export async function checkAuth() {
    const user = await getCurrentUser();
    return !!user;
}

/**
 * Request password reset email
 * @param {string} email - User email
 * @returns {Promise<void>}
 */
export async function requestPasswordReset(email) {
    await post('/auth/forgot-password', { email });
}

/**
 * Reset password with token
 * @param {Object} data - Reset data
 * @param {string} data.token - Reset token
 * @param {string} data.password - New password
 * @returns {Promise<void>}
 */
export async function resetPassword(data) {
    await post('/auth/reset-password', data);
}

/**
 * Change password for authenticated user
 * @param {Object} data - Password change data
 * @param {string} data.current_password - Current password
 * @param {string} data.new_password - New password (min 12 characters)
 * @returns {Promise<Object>} Success response
 */
export async function changePassword(data) {
    return await post('/auth/change-password', data);
}

/**
 * Validate a password reset token
 * @param {string} token - Reset token from URL
 * @returns {Promise<Object>} Validation result
 */
export async function validateResetToken(token) {
    return await get(`/auth/validate-reset-token?token=${encodeURIComponent(token)}`);
}

/**
 * Get deletion preflight warnings (sole-admin regions, sole-verified schools)
 * @returns {Promise<Object>} Warnings data
 */
export async function getDeletionWarnings() {
    return await get('/users/me/deletion-preflight');
}

/**
 * Delete the current user's account
 * @param {string} password - User's password for confirmation
 * @returns {Promise<Object>} Success response
 */
export async function deleteAccount(password) {
    const response = await del('/users/me', { password });
    clearUser();
    return response;
}

export default {
    register,
    login,
    logout,
    getCurrentUser,
    checkAuth,
    requestPasswordReset,
    resetPassword,
    changePassword,
    validateResetToken,
    getDeletionWarnings,
    deleteAccount,
};
