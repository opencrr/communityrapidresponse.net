/**
 * Connections API module
 */

import { get, post, del } from './client.js';

// Connections
export async function proposeConnection(data) { return post('/connections', data); }
export async function listMyConnections() { return get('/connections'); }
export async function getConnection(id) { return get(`/connections/${id}`); }
export async function inviteToConnection(id, data) { return post(`/connections/${id}/invite`, data); }
export async function leaveConnection(id) { return post(`/connections/${id}/leave`); }

// Connection proposals
export async function listPendingProposals() { return get('/connection-proposals'); }
export async function respondToProposal(id, data) { return post(`/connection-proposals/${id}/respond`, data); }

// Connection signal chats
export async function proposeSignalChat(connectionId, data) {
    const proposerGroupId = data.proposer_group_id || data.group_id;
    if (!proposerGroupId) throw new Error('proposer_group_id is required');
    const query = new URLSearchParams({ proposer_group_id: proposerGroupId }).toString();
    return post(`/connections/${connectionId}/signal-group-proposals?${query}`, data);
}
export async function listChatProposals(connectionId) { return get(`/connections/${connectionId}/signal-group-proposals`); }
export async function voteOnChatProposal(proposalId, data) { return post(`/connection-chat-proposals/${proposalId}/vote`, data); }
export async function listConnectionSignalGroups(connectionId) { return get(`/connections/${connectionId}/signal-groups`); }
export async function getConnectionChatSecret(connectionId, signalGroupId) { return get(`/connections/${connectionId}/signal-groups/${signalGroupId}/secret`); }

// Connection resource sharing
export async function shareResource(connectionId, data) { return post(`/connections/${connectionId}/shared-resources`, data); }
export async function unshareResource(connectionId, resourceId) { return del(`/connections/${connectionId}/shared-resources/${resourceId}`); }
export async function listConnectionResources(connectionId) { return get(`/connections/${connectionId}/shared-resources`); }

export default {
    proposeConnection,
    listMyConnections,
    getConnection,
    inviteToConnection,
    leaveConnection,
    listPendingProposals,
    respondToProposal,
    proposeSignalChat,
    listChatProposals,
    voteOnChatProposal,
    listConnectionSignalGroups,
    getConnectionChatSecret,
    shareResource,
    unshareResource,
    listConnectionResources,
};
