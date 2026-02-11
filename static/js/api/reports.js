/**
 * Reports API module
 * Handles user report operations.
 */

import { get, post, ApiError } from './client.js';

/**
 * Create a report for a user in a region
 * @param {string} regionId - Region UUID
 * @param {string} reportedUserId - User to report
 * @param {string} reason - Report reason
 * @param {string} [details] - Optional details
 * @returns {Promise<Object>} Created report
 */
export async function createReport(regionId, reportedUserId, reason, details = null) {
    const body = {
        reported_user_id: reportedUserId,
        reason,
    };
    if (details) {
        body.details = details;
    }
    return await post(`/communities/${regionId}/reports`, body);
}

/**
 * Create a report for a user in a school
 * @param {string} schoolId - School UUID
 * @param {string} reportedUserId - User to report
 * @param {string} reason - Report reason
 * @param {string} [details] - Optional details
 * @returns {Promise<Object>} Created report
 */
export async function createSchoolReport(schoolId, reportedUserId, reason, details = null) {
    const body = {
        reported_user_id: reportedUserId,
        reason,
    };
    if (details) {
        body.details = details;
    }
    return await post(`/schools/${schoolId}/reports`, body);
}

/**
 * Create a report for a user in a school district
 * @param {string} districtId - District UUID
 * @param {string} reportedUserId - User to report
 * @param {string} reason - Report reason
 * @param {string} [details] - Optional details
 * @returns {Promise<Object>} Created report
 */
export async function createDistrictReport(districtId, reportedUserId, reason, details = null) {
    const body = {
        reported_user_id: reportedUserId,
        reason,
    };
    if (details) {
        body.details = details;
    }
    return await post(`/school-districts/${districtId}/reports`, body);
}

/**
 * Get all reports the user can see (admin/superuser)
 * @param {Object} [params] - Query parameters
 * @param {string} [params.status] - Filter by status
 * @param {string} [params.region_id] - Filter by region
 * @param {string} [params.school_id] - Filter by school
 * @param {string} [params.district_id] - Filter by district
 * @returns {Promise<Object[]>} Array of reports
 */
export async function getReports(params = {}) {
    const query = new URLSearchParams();
    if (params.status) query.set('status', params.status);
    if (params.region_id) query.set('region_id', params.region_id);
    if (params.school_id) query.set('school_id', params.school_id);
    if (params.district_id) query.set('district_id', params.district_id);
    const queryString = query.toString();
    const url = queryString ? `/reports?${queryString}` : '/reports';
    try {
        const response = await get(url);
        return response.reports || [];
    } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
            return [];
        }
        throw error;
    }
}

/**
 * Get full details of a report
 * @param {string} reportId - Report UUID
 * @returns {Promise<Object>} Report details
 */
export async function getReportDetails(reportId) {
    return await get(`/reports/${reportId}`);
}

/**
 * Resolve a report (dismiss or initiate blocklist)
 * @param {string} reportId - Report UUID
 * @param {string} action - "dismiss" or "initiate_blocklist"
 * @param {string} [note] - Optional resolution note
 * @returns {Promise<Object>} Result
 */
export async function resolveReport(reportId, action, note = null) {
    const body = { action };
    if (note) {
        body.note = note;
    }
    return await post(`/reports/${reportId}/resolve`, body);
}

export default {
    createReport,
    createSchoolReport,
    createDistrictReport,
    getReports,
    getReportDetails,
    resolveReport,
};
