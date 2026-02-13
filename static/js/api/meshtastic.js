/**
 * API calls for Meshtastic channel management
 */

import { get, post, put } from './client.js';

/**
 * List Meshtastic channels (optionally filtered by scope)
 * GET /api/v1/meshtastic-channels?region_id=X (or school_id, district_id)
 */
export async function listMeshtasticChannels(params = {}) {
    const query = new URLSearchParams(params).toString();
    const path = query ? `/meshtastic-channels?${query}` : '/meshtastic-channels';
    return get(path);
}

/**
 * List Meshtastic channels for admin
 * GET /api/v1/meshtastic-channels/admin
 */
export async function listAdminMeshtasticChannels() {
    return get('/meshtastic-channels/admin');
}

/**
 * Create a new Meshtastic channel
 * POST /api/v1/meshtastic-channels
 */
export async function createMeshtasticChannel(data) {
    return post('/meshtastic-channels', data);
}

/**
 * Update a Meshtastic channel (name/description only)
 * PUT /api/v1/meshtastic-channels/:id
 */
export async function updateMeshtasticChannel(id, data) {
    return put(`/meshtastic-channels/${id}`, data);
}
