/**
 * Groups API module
 */

import { get, post, put, del } from './client.js';

// Group CRUD
export async function createGroup(data) { return post('/groups', data); }
export async function getGroup(id) { return get(`/groups/${id}`); }
export async function updateGroup(id, data) { return put(`/groups/${id}`, data); }
export async function deleteGroup(id) { return del(`/groups/${id}`); }
export async function listMyGroups() { return get('/groups'); }
export async function browseGroups(params = {}) { return get('/groups/browse', params); }

// Members
export async function listGroupMembers(id) { return get(`/groups/${id}/members`); }
export async function leaveGroup(id) { return post(`/groups/${id}/leave`); }

// Invite links
export async function createInviteLink(groupId, data = {}) { return post(`/groups/${groupId}/invite-links`, data); }
export async function listInviteLinks(groupId) { return get(`/groups/${groupId}/invite-links`); }
export async function joinViaLink(token) { return post(`/groups/join/${token}`); }

// Invitations
export async function createInvitation(groupId, data) { return post(`/groups/${groupId}/invitations`, data); }
export async function listMyInvitations() { return get('/group-invitations'); }
export async function respondToInvitation(id, data) { return post(`/group-invitations/${id}/respond`, data); }

// Signal groups
export async function createSignalGroup(groupId, data) { return post(`/groups/${groupId}/signal-groups`, data); }
export async function listSignalGroups(groupId) { return get(`/groups/${groupId}/signal-groups`); }

// Resources
export async function createResource(groupId, data) { return post(`/groups/${groupId}/resources`, data); }
export async function listResources(groupId) { return get(`/groups/${groupId}/resources`); }
export async function updateResource(groupId, resourceId, data) { return put(`/groups/${groupId}/resources/${resourceId}`, data); }
export async function deleteResource(groupId, resourceId) { return del(`/groups/${groupId}/resources/${resourceId}`); }

// Trust vouching
export async function vouchForMember(groupId, data) { return post(`/groups/${groupId}/trust-vouches`, data); }
export async function getTrustVouchStatus(groupId, userId) { return get(`/groups/${groupId}/trust-vouches/${userId}`); }

// Topic board
export async function createOrUpdatePosting(groupId, data) { return post(`/groups/${groupId}/topic-board`, data); }
export async function getPosting(groupId) { return get(`/groups/${groupId}/topic-board`); }
export async function removePosting(groupId) { return del(`/groups/${groupId}/topic-board`); }
export async function browseTopicBoard(params) { return get('/topic-board', params); }

// Blocking
export async function blockGroup(groupId, data) { return post(`/groups/${groupId}/blocks`, data); }
export async function unblockGroup(groupId, blockedGroupId) { return del(`/groups/${groupId}/blocks/${blockedGroupId}`); }
export async function listBlockedGroups(groupId) { return get(`/groups/${groupId}/blocks`); }

export default {
    createGroup,
    getGroup,
    updateGroup,
    deleteGroup,
    listMyGroups,
    browseGroups,
    listGroupMembers,
    leaveGroup,
    createInviteLink,
    listInviteLinks,
    joinViaLink,
    createInvitation,
    listMyInvitations,
    respondToInvitation,
    createSignalGroup,
    listSignalGroups,
    createResource,
    listResources,
    updateResource,
    deleteResource,
    vouchForMember,
    getTrustVouchStatus,
    createOrUpdatePosting,
    getPosting,
    removePosting,
    browseTopicBoard,
    blockGroup,
    unblockGroup,
    listBlockedGroups,
};
