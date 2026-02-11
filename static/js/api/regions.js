/**
 * Regions API module
 */

import { get, post, put, del, ApiError } from './client.js?v=c49aed9';
import { setRegions, setCurrentRegion } from '../utils/store.js?v=c49aed9';

/**
 * Get all regions with optional filters
 * Returns empty array if no regions exist (handles 404 gracefully)
 * @param {Object} [params] - Query parameters
 * @param {string} [params.type] - Filter by region type (state, county, city, neighborhood)
 * @param {string} [params.parent_id] - Filter by parent region ID
 * @param {number} [params.lat] - Latitude for location-based filtering
 * @param {number} [params.lng] - Longitude for location-based filtering
 * @returns {Promise<Object[]>} Array of regions
 */
export async function getRegions(params = {}) {
    try {
        const response = await get('/communities', params);
        // Handle null/undefined regions - ensure we always return an array
        const regions = Array.isArray(response.regions) ? response.regions :
                       Array.isArray(response) ? response : [];
        setRegions(regions);
        return regions;
    } catch (error) {
        // Treat 404 as "no regions" rather than an error
        if (error instanceof ApiError && error.status === 404) {
            setRegions([]);
            return [];
        }
        throw error;
    }
}

/**
 * Get a single region by ID
 * @param {string} regionId - Region UUID
 * @returns {Promise<Object>} Region data
 */
export async function getRegion(regionId) {
    const response = await get(`/communities/${regionId}`);
    const region = response.region || response;
    setCurrentRegion(region);
    return region;
}

/**
 * Get child regions of a parent region
 * Returns empty array if no child regions (handles 404 gracefully)
 * @param {string} parentId - Parent region UUID
 * @returns {Promise<Object[]>} Array of child regions
 */
export async function getChildRegions(parentId) {
    try {
        return await getRegions({ parent_id: parentId });
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get regions containing a point
 * @param {number} lat - Latitude
 * @param {number} lng - Longitude
 * @returns {Promise<Object[]>} Array of regions containing the point
 */
export async function getRegionsAtPoint(lat, lng) {
    return get('/communities/at-point', { lat, lng });
}

/**
 * Create a new region (admin only)
 * @param {Object} data - Region data
 * @param {string} data.name - Region name
 * @param {string} data.type - Region type (state, county, city, neighborhood)
 * @param {string} [data.parent_region_id] - Parent region UUID
 * @param {Object} data.geometry - GeoJSON Polygon geometry
 * @returns {Promise<Object>} Created region with id and name
 */
export async function createRegion(data) {
    const response = await post('/communities', data);
    // Backend returns { region_id, name, created_by }
    return {
        id: response.region_id,
        name: response.name,
        ...response,
    };
}

/**
 * Update a region (admin only)
 * @param {string} regionId - Region UUID
 * @param {Object} data - Updated region data
 * @returns {Promise<Object>} Updated region
 */
export async function updateRegion(regionId, data) {
    const response = await put(`/communities/${regionId}`, data);
    return response.region || response;
}

/**
 * Delete a region (requires consensus)
 * @param {string} regionId - Region UUID
 * @returns {Promise<void>}
 */
export async function deleteRegion(regionId) {
    await del(`/communities/${regionId}`);
}

/**
 * Get members of a region (auth required, caller must be a member)
 * @param {string} regionId - Region UUID
 * @returns {Promise<Object>} Response with members array
 */
export async function getRegionMembers(regionId) {
    return await get(`/communities/${regionId}/members`);
}

/**
 * Get regions the current user is a member of
 * Returns empty array if no regions (handles 404 gracefully)
 * Note: Uses /regions which returns user-scoped results for non-superusers
 * @returns {Promise<Object[]>} Array of user's regions
 */
export async function getMyRegions() {
    try {
        const response = await get('/communities');
        return response.regions || response || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get regions the current user can administer
 * Returns empty array if no regions (handles 404 gracefully)
 * @returns {Promise<Object[]>} Array of regions user can admin
 */
export async function getAdminRegions() {
    try {
        const response = await get('/communities/admin');
        return response.regions || response || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get the admin boundary (city/county) for region creation
 * @returns {Promise<Object|null>} Admin boundary GeoJSON or null if not available
 */
export async function getAdminBoundary() {
    try {
        const response = await get('/communities/admin-boundary');
        return response.boundary || response;
    } catch (error) {
        // Endpoint may not be implemented yet
        return null;
    }
}

/**
 * Validate if a polygon is within the user's admin boundary
 * @param {Object} polygon - GeoJSON Polygon geometry
 * @returns {Promise<Object>} Validation result
 */
export async function validatePolygon(polygon) {
    try {
        return await post('/communities/validate-polygon', { polygon });
    } catch (error) {
        // Endpoint may not be implemented - return valid to allow submission
        // Backend will validate on actual region creation
        return { valid: true, message: 'Validation skipped - will be checked on submission' };
    }
}

/**
 * Convert region boundary to GeoJSON Feature for map display
 * @param {Object} region - Region object with boundary
 * @returns {Object} GeoJSON Feature
 */
export function regionToFeature(region) {
    // Backend returns 'geometry' for RegionWithDetails, 'boundary' is not used
    const geometry = region.geometry || region.boundary;
    return {
        type: 'Feature',
        id: region.id,
        properties: {
            id: region.id,
            name: region.name,
            type: region.type || region.region_type,
            parent_id: region.parent_id || region.parent_region_id,
        },
        geometry: geometry,
    };
}

/**
 * Convert array of regions to GeoJSON FeatureCollection
 * @param {Object[]} regions - Array of regions
 * @returns {Object} GeoJSON FeatureCollection
 */
export function regionsToFeatureCollection(regions) {
    return {
        type: 'FeatureCollection',
        features: regions.map(regionToFeature),
    };
}

export default {
    getRegions,
    getRegion,
    getChildRegions,
    getRegionsAtPoint,
    createRegion,
    updateRegion,
    deleteRegion,
    getRegionMembers,
    getMyRegions,
    getAdminRegions,
    getAdminBoundary,
    validatePolygon,
    regionToFeature,
    regionsToFeatureCollection,
};
