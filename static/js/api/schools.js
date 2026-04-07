/**
 * Schools API module
 * Thin layer for NCES school search and school-to-group linking.
 */

import { get, post, ApiError } from './client.js';

/**
 * Search schools by name, state, or district
 * @param {Object} [params] - Query parameters
 * @param {string} [params.query] - Search query (name)
 * @param {string} [params.state] - Filter by state (2-letter code)
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
 * @returns {Promise<Object>} School details with group_id if linked
 */
export async function getSchool(schoolId) {
    return await get(`/schools/${schoolId}`);
}

/**
 * Join a school (creates or joins the linked group)
 * @param {string} schoolId - School UUID
 * @returns {Promise<Object>} Join response with group_id
 */
export async function joinSchool(schoolId) {
    return await post(`/schools/${schoolId}/join`);
}

/**
 * Get user's school groups
 * @returns {Promise<Object>} User's school groups list
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
    getMySchools,
    searchDistricts,
    getDistrict,
    districtToFeature,
};
