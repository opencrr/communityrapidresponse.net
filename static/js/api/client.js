/**
 * Base API client with authentication and error handling.
 * All API calls use credentials: 'include' for httpOnly cookie auth.
 */

const API_BASE_URL = '/api/v1';

/**
 * Read a cookie value by name
 * @param {string} name - Cookie name
 * @returns {string|null} Cookie value or null if not found
 */
function getCookie(name) {
    const cookies = document.cookie.split(';');
    for (const cookie of cookies) {
        const [cookieName, ...cookieValueParts] = cookie.trim().split('=');
        if (cookieName === name) {
            return decodeURIComponent(cookieValueParts.join('='));
        }
    }
    return null;
}

/**
 * Custom error class for API errors
 */
export class ApiError extends Error {
    constructor(message, status, data = null) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.data = data;
    }
}

/**
 * Make an API request with proper error handling
 * @param {string} endpoint - API endpoint (relative to /api/v1)
 * @param {Object} options - Fetch options
 * @returns {Promise<Object>} Response data
 */
async function request(endpoint, options = {}) {
    const url = `${API_BASE_URL}${endpoint}`;

    const defaultHeaders = {
        'Content-Type': 'application/json',
    };

    // Add CSRF token for state-changing requests
    const requestMethod = (options.method || 'GET').toUpperCase();
    if (requestMethod !== 'GET' && requestMethod !== 'HEAD') {
        const csrfToken = getCookie('csrf_token');
        if (csrfToken) {
            defaultHeaders['X-CSRF-Token'] = csrfToken;
        }
    }

    const config = {
        ...options,
        headers: {
            ...defaultHeaders,
            ...options.headers,
        },
        credentials: 'include', // Include httpOnly cookies
    };

    // Don't set Content-Type for FormData
    if (options.body instanceof FormData) {
        delete config.headers['Content-Type'];
    }

    try {
        const response = await fetch(url, config);

        // Handle no content responses
        if (response.status === 204) {
            return null;
        }

        // Try to parse JSON response
        let data;
        const contentType = response.headers.get('Content-Type');
        if (contentType && contentType.includes('application/json')) {
            data = await response.json();
        } else {
            data = await response.text();
        }

        // Handle error responses
        if (!response.ok) {
            // Check for email verification required error (403)
            if (response.status === 403 && data?.error === 'email_verification_required') {
                // Redirect to verify-email page
                window.location.pathname = '/verify-email';
                // Return a dummy promise that never resolves (page navigation in progress)
                return new Promise(() => {});
            }

            const errorMessage = data?.error || data?.message || `Request failed with status ${response.status}`;
            throw new ApiError(errorMessage, response.status, data);
        }

        return data;
    } catch (error) {
        // Re-throw ApiError as-is
        if (error instanceof ApiError) {
            throw error;
        }

        // Network errors
        if (error.name === 'TypeError' && error.message.includes('fetch')) {
            throw new ApiError('Network error. Please check your connection.', 0);
        }

        // Other errors
        throw new ApiError(error.message || 'An unexpected error occurred', 0);
    }
}

/**
 * GET request
 * @param {string} endpoint - API endpoint
 * @param {Object} [params] - Query parameters
 * @returns {Promise<Object>} Response data
 */
export async function get(endpoint, params = {}) {
    const queryString = new URLSearchParams(params).toString();
    const url = queryString ? `${endpoint}?${queryString}` : endpoint;
    return request(url, { method: 'GET' });
}

/**
 * POST request
 * @param {string} endpoint - API endpoint
 * @param {Object} body - Request body
 * @returns {Promise<Object>} Response data
 */
export async function post(endpoint, body = {}) {
    return request(endpoint, {
        method: 'POST',
        body: JSON.stringify(body),
    });
}

/**
 * PUT request
 * @param {string} endpoint - API endpoint
 * @param {Object} body - Request body
 * @returns {Promise<Object>} Response data
 */
export async function put(endpoint, body = {}) {
    return request(endpoint, {
        method: 'PUT',
        body: JSON.stringify(body),
    });
}

/**
 * PATCH request
 * @param {string} endpoint - API endpoint
 * @param {Object} body - Request body
 * @returns {Promise<Object>} Response data
 */
export async function patch(endpoint, body = {}) {
    return request(endpoint, {
        method: 'PATCH',
        body: JSON.stringify(body),
    });
}

/**
 * DELETE request
 * @param {string} endpoint - API endpoint
 * @param {Object} [body] - Optional request body
 * @returns {Promise<Object>} Response data
 */
export async function del(endpoint, body) {
    const options = { method: 'DELETE' };
    if (body !== undefined) {
        options.body = JSON.stringify(body);
    }
    return request(endpoint, options);
}

/**
 * Upload file(s)
 * @param {string} endpoint - API endpoint
 * @param {FormData} formData - Form data with files
 * @returns {Promise<Object>} Response data
 */
export async function upload(endpoint, formData) {
    return request(endpoint, {
        method: 'POST',
        body: formData,
    });
}

export default {
    get,
    post,
    put,
    patch,
    del,
    upload,
    ApiError,
};
