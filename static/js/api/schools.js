/**
 * Schools API module
 */

import { get, post, ApiError } from './client.js';

/**
 * Search schools by name, state, or district
 * @param {Object} [params] - Query parameters
 * @param {string} [params.query] - Search query (name)
 * @param {string} [params.state] - Filter by state (2-letter code)
 * @param {string} [params.district_id] - Filter by district ID
 * @param {number} [params.page] - Page number (default 1)
 * @param {number} [params.limit] - Results per page (default 20)
 * @returns {Promise<Object>} Search results with schools array and pagination
 */
export async function searchSchools(params = {}) {
    try {
        const response = await get('/schools', params);
        return response;
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return { schools: [], total: 0, page: 1, limit: 20, has_more: false };
        }
        throw error;
    }
}

/**
 * Get a single school by ID
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} School details
 */
export async function getSchool(schoolId) {
    return await get(`/schools/${schoolId}`);
}

/**
 * Join a school (creates pending membership)
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} Join response
 */
export async function joinSchool(schoolId) {
    return await post(`/schools/${schoolId}/join`);
}

/**
 * Leave a school
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} Leave response
 */
export async function leaveSchool(schoolId) {
    return await post(`/schools/${schoolId}/leave`);
}

/**
 * Vouch for a school member
 * @param {string} schoolId - School UUID
 * @param {Object} data - Vouch data
 * @param {string} data.user_identifier - Email, username, or user ID
 * @returns {Promise<Object>} Vouch response
 */
export async function vouchForSchoolMember(schoolId, data) {
    return await post(`/schools/${schoolId}/vouch`, data);
}

/**
 * Get pending vouch requests for a school
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} Pending vouch users
 */
export async function getPendingSchoolVouches(schoolId) {
    return await get(`/schools/${schoolId}/vouch/pending`);
}

/**
 * Get vouch status for a user in a school
 * @param {string} schoolId - School UUID
 * @param {string} userId - User UUID
 * @returns {Promise<Object>} Vouch status
 */
export async function getSchoolVouchStatus(schoolId, userId) {
    return await get(`/schools/${schoolId}/vouch-status/${userId}`);
}

/**
 * Get members of a school
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} Members list
 */
export async function getSchoolMembers(schoolId) {
    return await get(`/schools/${schoolId}/members`);
}

/**
 * Get signal groups for a school
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} Signal groups list
 */
export async function getSchoolSignalGroups(schoolId) {
    return await get(`/schools/${schoolId}/signal-groups`);
}

/**
 * Create a signal group for a school (admin only)
 * @param {string} schoolId - School UUID
 * @param {Object} data - Signal group data
 * @param {string} data.name - Group name
 * @param {string} data.invite_link - Signal invite link
 * @param {string} [data.description] - Group description
 * @returns {Promise<Object>} Created group
 */
export async function createSchoolSignalGroup(schoolId, data) {
    return await post(`/schools/${schoolId}/signal-groups`, data);
}

/**
 * Get user's schools
 * @returns {Promise<Object>} User's schools list
 */
export async function getMySchools() {
    try {
        const response = await get('/schools/my');
        return response;
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return { schools: [] };
        }
        throw error;
    }
}

/**
 * Search school districts
 * @param {Object} [params] - Query parameters
 * @param {string} [params.query] - Search query
 * @param {string} [params.state] - Filter by state
 * @returns {Promise<Object>} Districts list
 */
export async function searchDistricts(params = {}) {
    try {
        return await get('/school-districts', params);
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return { districts: [] };
        }
        throw error;
    }
}

/**
 * Get a school district by ID
 * @param {string} districtId - District UUID
 * @returns {Promise<Object>} District details
 */
export async function getDistrict(districtId) {
    return await get(`/school-districts/${districtId}`);
}

/**
 * Get members of a district (across all schools)
 * @param {string} districtId - District UUID
 * @returns {Promise<Object>} Members list
 */
export async function getDistrictMembers(districtId) {
    return await get(`/school-districts/${districtId}/members`);
}

/**
 * Get signal groups for a district
 * @param {string} districtId - District UUID
 * @returns {Promise<Object>} Signal groups list
 */
export async function getDistrictSignalGroups(districtId) {
    return await get(`/school-districts/${districtId}/signal-groups`);
}

/**
 * Create a signal group for a district
 * @param {string} districtId - District UUID
 * @param {Object} data - Signal group data
 * @returns {Promise<Object>} Created group
 */
export async function createDistrictSignalGroup(districtId, data) {
    return await post(`/school-districts/${districtId}/signal-groups`, data);
}

/**
 * Convert a district object to a GeoJSON Feature for map display
 * @param {Object} district - District with geometry
 * @returns {Object} GeoJSON Feature
 */
export function districtToFeature(district) {
    return {
        type: 'Feature',
        id: district.id,
        properties: {
            id: district.id,
            name: district.name,
            type: 'school_district',
        },
        geometry: district.geometry,
    };
}

export default {
    searchSchools,
    getSchool,
    joinSchool,
    leaveSchool,
    vouchForSchoolMember,
    getPendingSchoolVouches,
    getSchoolVouchStatus,
    getSchoolMembers,
    getSchoolSignalGroups,
    createSchoolSignalGroup,
    getMySchools,
    searchDistricts,
    getDistrict,
    getDistrictMembers,
    getDistrictSignalGroups,
    createDistrictSignalGroup,
    districtToFeature,
};
