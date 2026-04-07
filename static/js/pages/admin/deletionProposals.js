/**
 * Admin: Deletion Proposals page
 * Allows admins to create, view, and vote on proposals to delete signal groups and sub-regions.
 */

import {
    getDeletionProposals,
    getDeletionProposalDetails,
    createDeletionProposal,
    voteOnDeletionProposal,
    expireDeletionProposal,
} from '../../api/deletions.js';
import { getAdminRegions } from '../../api/regions.js';
import { getGroupsByRegion, getAdminGroups } from '../../api/signalGroups.js';
import { getRegion } from '../../api/regions.js';
import { getMySchools } from '../../api/schools.js';
// School signal groups are now managed through the group model
const getSchoolSignalGroups = async () => ({ groups: [] });
const getDistrictSignalGroups = async () => ({ groups: [] });
import { ApiError } from '../../api/client.js';
import { isAdmin, isSuperuser } from '../../utils/store.js';
import toast from '../../components/toast.js';
import modal from '../../components/modal.js';

let currentFilter = 'pending';
let adminRegions = [];
let mySchools = [];

/**
 * Render the deletion proposals page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const userIsAdmin = isAdmin();

    if (!userIsAdmin) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Admin Access Required</h3>
                    <p class="empty-state__description">
                        You need both postcard and vouch verification to view deletion proposals.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>View Verification Status</a>
                </div>
            </div>
        `;
        return;
    }

    const urlParams = new URLSearchParams(window.location.search);
    currentFilter = urlParams.get('status') || 'pending';

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <div class="page__header-content">
                        <h1 class="page__title">Deletion Proposals</h1>
                        <p class="page__subtitle">Review and vote on proposals to delete Signal groups and sub-regions.</p>
                    </div>
                    <button class="btn btn--primary" id="create-deletion-proposal-btn">
                        + New Proposal
                    </button>
                </div>

                <div class="proposal-filters mb-4">
                    <div class="filter-tabs">
                        <button class="filter-tab ${currentFilter === 'pending' ? 'filter-tab--active' : ''}" data-status="pending">
                            Pending
                        </button>
                        <button class="filter-tab ${currentFilter === 'approved' ? 'filter-tab--active' : ''}" data-status="approved">
                            Approved
                        </button>
                        <button class="filter-tab ${currentFilter === 'expired' ? 'filter-tab--active' : ''}" data-status="expired">
                            Expired
                        </button>
                        <button class="filter-tab ${currentFilter === '' ? 'filter-tab--active' : ''}" data-status="">
                            All
                        </button>
                    </div>
                </div>

                <div id="proposals-content">
                    <div class="loading">
                        <div class="spinner spinner--lg"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind filter tabs
    document.querySelectorAll('.filter-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            const status = tab.dataset.status;
            currentFilter = status;
            const url = new URL(window.location);
            if (status) {
                url.searchParams.set('status', status);
            } else {
                url.searchParams.delete('status');
            }
            window.history.replaceState({}, '', url);
            document.querySelectorAll('.filter-tab').forEach(t => t.classList.remove('filter-tab--active'));
            tab.classList.add('filter-tab--active');
            loadProposals();
        });
    });

    // Bind create proposal button
    const createBtn = document.getElementById('create-deletion-proposal-btn');
    if (createBtn) {
        createBtn.addEventListener('click', showCreateProposalModal);
    }

    // Load admin regions and schools in parallel
    try {
        const [regions, schools] = await Promise.all([
            getAdminRegions().catch(() => []),
            getMySchools().catch(() => []),
        ]);
        adminRegions = regions;
        mySchools = (schools || []).filter(s => s.is_admin && s.verification_status === 'verified');
    } catch (error) {
        console.warn('Failed to load admin data:', error);
        adminRegions = [];
        mySchools = [];
    }

    await loadProposals();
}

/**
 * Load and display proposals
 */
async function loadProposals() {
    const content = document.getElementById('proposals-content');
    if (!content) return;

    content.innerHTML = `
        <div class="loading">
            <div class="spinner spinner--lg"></div>
        </div>
    `;

    try {
        const params = {};
        if (currentFilter) {
            params.status = currentFilter;
        }
        const proposals = await getDeletionProposals(params);

        if (proposals.length === 0) {
            const emptyMessage = currentFilter === 'pending'
                ? 'There are no pending deletion proposals requiring your vote.'
                : `No ${currentFilter || ''} deletion proposals found.`;
            content.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4CB;</div>
                    <h3 class="empty-state__title">No Proposals</h3>
                    <p class="empty-state__description">${emptyMessage}</p>
                </div>
            `;
            return;
        }

        content.innerHTML = `
            <div class="proposals-grid">
                ${proposals.map(proposal => renderProposalCard(proposal)).join('')}
            </div>
        `;

        bindProposalActions();
    } catch (error) {
        console.error('Failed to load proposals:', error);
        content.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Error Loading Proposals</h3>
                <p class="empty-state__description">
                    Failed to load proposals. Please try again.
                </p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render a proposal card
 */
function renderProposalCard(proposal) {
    const statusClass = getStatusClass(proposal.status);
    const statusLabel = capitalizeFirst(proposal.status);
    const assetTypeLabel = proposal.asset_type === 'signal_group' ? 'Signal Group' : 'Sub-Region';

    return `
        <div class="card proposal-card proposal-card--${proposal.status}" data-proposal-id="${proposal.id}">
            <div class="card__body">
                <div class="proposal-card__header">
                    <div>
                        <div class="proposal-card__group">
                            <span class="badge badge--secondary" style="font-size: var(--font-size-xs); margin-right: var(--space-1);">${assetTypeLabel}</span>
                            ${escapeHtml(proposal.asset_name)}
                        </div>
                    </div>
                    <span class="badge badge--${statusClass}">${statusLabel}</span>
                </div>
                <div class="proposal-card__reason">
                    <strong>Reason:</strong> ${escapeHtml(truncate(proposal.reason, 100))}
                </div>
                <div class="proposal-card__meta">
                    <span>Proposed by ${escapeHtml(proposal.proposed_by || 'Unknown')}</span>
                    <span>${formatDate(proposal.created_at)}</span>
                </div>
                <div class="vote-progress">
                    ${renderVoteProgress(proposal.votes, proposal.votes_needed)}
                </div>
                <div class="proposal-card__footer">
                    <span class="proposal-card__expires">
                        ${statusLabel}: ${formatDate(proposal.created_at)}
                    </span>
                    <button class="btn btn--sm btn--primary view-proposal-btn" data-proposal-id="${proposal.id}">
                        View
                    </button>
                </div>
            </div>
        </div>
    `;
}

/**
 * Render vote progress circles
 */
function renderVoteProgress(currentVotes, votesNeeded) {
    const circles = [];
    for (let i = 0; i < votesNeeded; i++) {
        const hasVote = i < currentVotes;
        circles.push(`
            <div class="vote-progress__item ${hasVote ? 'vote-progress__item--approve' : ''}"
                 title="${hasVote ? 'Approved' : 'Pending'}">
                ${hasVote ? '&#x2713;' : ''}
            </div>
        `);
    }
    return circles.join('');
}

function getStatusClass(status) {
    switch (status) {
        case 'pending': return 'pending';
        case 'approved': return 'success';
        case 'expired': return 'expired';
        case 'rejected': return 'danger';
        default: return 'secondary';
    }
}

function capitalizeFirst(str) {
    if (!str) return '';
    return str.charAt(0).toUpperCase() + str.slice(1);
}

function truncate(str, length) {
    if (!str) return '';
    if (str.length <= length) return str;
    return str.substring(0, length) + '...';
}

function bindProposalActions() {
    document.querySelectorAll('.view-proposal-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            showProposalDetailModal(btn.dataset.proposalId);
        });
    });
}

/**
 * Show proposal detail modal
 */
async function showProposalDetailModal(proposalId) {
    const userIsSuperuser = isSuperuser();

    modal.showModal({
        title: 'Loading Proposal...',
        content: `<div class="loading"><div class="spinner spinner--lg"></div></div>`,
        actions: [],
    });

    try {
        const proposal = await getDeletionProposalDetails(proposalId);
        modal.closeModal();

        const isPending = proposal.status === 'pending';
        const statusClass = getStatusClass(proposal.status);
        const assetTypeLabel = proposal.asset_type === 'signal_group' ? 'Signal Group' : 'Sub-Region';

        // Determine scope label
        let scopeLabel = '';
        if (proposal.region_name) scopeLabel = `Community: ${escapeHtml(proposal.region_name)}`;
        if (proposal.school_name) scopeLabel = `School: ${escapeHtml(proposal.school_name)}`;
        if (proposal.district_name) scopeLabel = `District: ${escapeHtml(proposal.district_name)}`;

        let voteListHtml = '';
        if (proposal.votes && proposal.votes.length > 0) {
            voteListHtml = `
                <div class="proposal-detail__votes">
                    <h4>Votes (${proposal.votes.length}/${proposal.votes_needed})</h4>
                    <ul class="vote-list">
                        ${proposal.votes.map(vote => `
                            <li class="vote-list__item">
                                <span class="vote-list__user">${escapeHtml(vote.username)}</span>
                                <span class="badge badge--${vote.vote ? 'success' : 'danger'}">
                                    ${vote.vote ? 'Approve' : 'Reject'}
                                </span>
                                <span class="vote-list__time">${formatDate(vote.voted_at)}</span>
                            </li>
                        `).join('')}
                    </ul>
                </div>
            `;
        }

        let userVoteStatus = '';
        if (!proposal.can_vote && isPending && proposal.has_voted) {
            userVoteStatus = `
                <div class="proposal-detail__user-status">
                    <span class="badge badge--info">You have voted on this proposal</span>
                </div>
            `;
        } else if (proposal.can_vote) {
            userVoteStatus = `
                <div class="proposal-detail__user-status">
                    <span class="badge badge--warning">Your vote is needed</span>
                </div>
            `;
        }

        // Warning message based on asset type
        const warningMessage = proposal.asset_type === 'sub_region'
            ? 'If approved, this region and all its sub-regions, memberships, and Signal groups will be <strong>permanently deleted</strong>.'
            : 'If approved, this Signal group will be <strong>deactivated</strong> and hidden from all members.';

        const contentHtml = `
            <div class="proposal-detail">
                <div class="proposal-detail__header">
                    <div class="proposal-detail__group">
                        <span class="badge badge--secondary" style="margin-right: var(--space-1);">${assetTypeLabel}</span>
                        ${escapeHtml(proposal.asset_name)}
                    </div>
                    ${scopeLabel ? `<div class="proposal-detail__region">${scopeLabel}</div>` : ''}
                    <span class="badge badge--${statusClass}">${capitalizeFirst(proposal.status)}</span>
                </div>

                <div class="proposal-detail__section">
                    <label>Proposed by</label>
                    <p>${escapeHtml(proposal.proposed_by?.username || 'Unknown')}</p>
                </div>

                <div class="proposal-detail__section">
                    <label>Reason</label>
                    <p>${escapeHtml(proposal.reason)}</p>
                </div>

                <div class="proposal-detail__section">
                    <label>Vote Progress</label>
                    <div class="vote-progress vote-progress--lg">
                        ${renderVoteProgress(proposal.current_votes, proposal.votes_needed)}
                    </div>
                    <p class="vote-progress__label">${proposal.current_votes} of ${proposal.votes_needed} votes</p>
                </div>

                ${voteListHtml}
                ${userVoteStatus}

                <div class="proposal-detail__warning">
                    <strong>Warning:</strong> ${warningMessage}
                </div>

                <div class="proposal-detail__timestamps">
                    <span>Created: ${formatDate(proposal.created_at)}</span>
                    ${proposal.resolved_at ? `<span>Resolved: ${formatDate(proposal.resolved_at)}</span>` : ''}
                </div>
            </div>
        `;

        const actions = [{ label: 'Close', type: 'secondary' }];

        if (proposal.can_vote && isPending) {
            actions.push({
                label: 'Reject',
                type: 'danger',
                closeOnClick: false,
                onClick: async () => {
                    await handleVote(proposalId, false);
                },
            });
            actions.push({
                label: 'Approve',
                type: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    await handleVote(proposalId, true);
                },
            });
        }

        if (userIsSuperuser && isPending) {
            actions.splice(1, 0, {
                label: 'Expire Now',
                type: 'warning',
                closeOnClick: false,
                onClick: async () => {
                    await handleExpire(proposalId);
                },
            });
        }

        modal.showModal({
            title: 'Deletion Proposal Details',
            content: contentHtml,
            actions,
        });
    } catch (error) {
        console.error('Failed to load proposal details:', error);
        modal.closeModal();
        toast.error('Failed to load proposal details');
    }
}

/**
 * Handle voting on a proposal
 */
async function handleVote(proposalId, approve) {
    try {
        const result = await voteOnDeletionProposal(proposalId, approve);
        modal.closeModal();

        if (result.status === 'approved') {
            toast.success('Vote recorded! Deletion has been approved and applied.');
        } else {
            toast.success(`Vote recorded! ${approve ? 'Approved' : 'Rejected'}.`);
        }

        await loadProposals();
    } catch (error) {
        let errorMessage = 'Failed to record vote.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Handle expiring a proposal (superuser only)
 */
async function handleExpire(proposalId) {
    const confirmed = await modal.confirm({
        title: 'Expire Proposal',
        message: 'Are you sure you want to expire this proposal? This action cannot be undone.',
        confirmLabel: 'Expire',
        confirmType: 'warning',
    });

    if (!confirmed) {
        showProposalDetailModal(proposalId);
        return;
    }

    try {
        await expireDeletionProposal(proposalId);
        toast.success('Proposal has been expired.');
        await loadProposals();
    } catch (error) {
        let errorMessage = 'Failed to expire proposal.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        toast.error(errorMessage);
    }
}

/**
 * Show create deletion proposal modal
 */
async function showCreateProposalModal() {
    let selectedScope = 'community';
    let selectedRegionId = adminRegions[0]?.id || '';
    let selectedSchoolId = mySchools[0]?.school_id || '';
    let selectedAssetType = 'signal_group';
    let availableAssets = [];

    const loadAssets = async () => {
        availableAssets = [];
        try {
            if (selectedScope === 'community' && selectedRegionId) {
                if (selectedAssetType === 'signal_group') {
                    const groups = await getGroupsByRegion(selectedRegionId);
                    availableAssets = (groups || []).filter(g => g.is_active !== false).map(g => ({
                        id: g.id,
                        name: g.group_name || g.name,
                    }));
                } else {
                    // sub_region: get child regions
                    const region = await getRegion(selectedRegionId);
                    availableAssets = (region.sub_regions || []).map(r => ({
                        id: r.id,
                        name: r.name,
                    }));
                }
            } else if (selectedScope === 'school' && selectedSchoolId) {
                const groups = await getSchoolSignalGroups(selectedSchoolId);
                availableAssets = (groups || []).filter(g => g.is_active !== false).map(g => ({
                    id: g.id,
                    name: g.group_name || g.name,
                }));
            } else if (selectedScope === 'district') {
                // Find the district from the selected school
                const school = mySchools.find(s => s.school_id === selectedSchoolId);
                if (school && school.district_id) {
                    const groups = await getDistrictSignalGroups(school.district_id);
                    availableAssets = (groups || []).filter(g => g.is_active !== false).map(g => ({
                        id: g.id,
                        name: g.group_name || g.name,
                    }));
                }
            }
        } catch (error) {
            console.warn('Failed to load assets:', error);
            availableAssets = [];
        }
    };

    const renderModalContent = () => {
        const scopeOptions = [];
        if (adminRegions.length > 0) scopeOptions.push({ value: 'community', label: 'Community' });
        if (mySchools.length > 0) scopeOptions.push({ value: 'school', label: 'School' });
        if (mySchools.some(s => s.district_id)) scopeOptions.push({ value: 'district', label: 'District' });

        if (scopeOptions.length === 0) {
            return `<p>You are not an admin of any communities, schools, or districts.</p>`;
        }

        let scopeSelector = '';
        if (selectedScope === 'community') {
            scopeSelector = `
                <div class="form__group">
                    <label for="region-select" class="form__label">Community</label>
                    <select id="region-select" class="form__input">
                        ${adminRegions.map(r => `
                            <option value="${r.id}" ${r.id === selectedRegionId ? 'selected' : ''}>${escapeHtml(r.name)}</option>
                        `).join('')}
                    </select>
                </div>
                <div class="form__group">
                    <label for="asset-type-select" class="form__label">Asset Type</label>
                    <select id="asset-type-select" class="form__input">
                        <option value="signal_group" ${selectedAssetType === 'signal_group' ? 'selected' : ''}>Signal Group</option>
                        <option value="sub_region" ${selectedAssetType === 'sub_region' ? 'selected' : ''}>Sub-Region</option>
                    </select>
                </div>
            `;
        } else if (selectedScope === 'school') {
            scopeSelector = `
                <div class="form__group">
                    <label for="school-select" class="form__label">School</label>
                    <select id="school-select" class="form__input">
                        ${mySchools.map(s => `
                            <option value="${s.school_id}" ${s.school_id === selectedSchoolId ? 'selected' : ''}>${escapeHtml(s.school_name || s.name)}</option>
                        `).join('')}
                    </select>
                </div>
            `;
        } else if (selectedScope === 'district') {
            const schoolsWithDistrict = mySchools.filter(s => s.district_id);
            scopeSelector = `
                <div class="form__group">
                    <label for="school-select" class="form__label">School (for district context)</label>
                    <select id="school-select" class="form__input">
                        ${schoolsWithDistrict.map(s => `
                            <option value="${s.school_id}" ${s.school_id === selectedSchoolId ? 'selected' : ''}>${escapeHtml(s.school_name || s.name)} - ${escapeHtml(s.district_name || '')}</option>
                        `).join('')}
                    </select>
                </div>
            `;
        }

        return `
            <div class="form">
                <div class="form__group">
                    <label for="scope-select" class="form__label">Scope</label>
                    <select id="scope-select" class="form__input">
                        ${scopeOptions.map(o => `
                            <option value="${o.value}" ${o.value === selectedScope ? 'selected' : ''}>${o.label}</option>
                        `).join('')}
                    </select>
                    <p class="form__help">Select where the asset to delete belongs.</p>
                </div>

                ${scopeSelector}

                <div class="form__group">
                    <label for="asset-select" class="form__label">Asset to Delete</label>
                    <select id="asset-select" class="form__input" ${availableAssets.length === 0 ? 'disabled' : ''}>
                        ${availableAssets.length === 0
                            ? '<option value="">No assets found</option>'
                            : availableAssets.map(a => `
                                <option value="${a.id}">${escapeHtml(a.name)}</option>
                            `).join('')
                        }
                    </select>
                </div>

                <div class="form__group">
                    <label for="reason-input" class="form__label">Reason</label>
                    <textarea id="reason-input" class="form__input form__textarea" rows="3" placeholder="Enter the reason for this deletion..." required></textarea>
                    <p class="form__help">This will be visible to other admins voting on the proposal.</p>
                </div>

                <div class="form__notice form__notice--warning">
                    ${selectedAssetType === 'sub_region'
                        ? '<strong>Warning:</strong> If approved, this region and all its sub-regions, memberships, and Signal groups will be <strong>permanently deleted</strong>.'
                        : '<strong>Warning:</strong> If approved, this Signal group will be <strong>deactivated</strong> and hidden from all members.'}
                </div>
            </div>
        `;
    };

    await loadAssets();

    const showModal = () => {
        modal.showModal({
            title: 'Create Deletion Proposal',
            content: renderModalContent(),
            actions: [
                { label: 'Cancel', type: 'secondary' },
                {
                    label: 'Create Proposal',
                    type: 'primary',
                    closeOnClick: false,
                    onClick: async () => {
                        const assetId = document.getElementById('asset-select')?.value;
                        const reason = document.getElementById('reason-input')?.value?.trim();

                        if (!assetId) {
                            toast.error('Please select an asset to delete.');
                            return;
                        }
                        if (!reason) {
                            toast.error('Please provide a reason.');
                            return;
                        }

                        const assetType = selectedScope === 'community' ? selectedAssetType : 'signal_group';

                        try {
                            await createDeletionProposal({
                                asset_type: assetType,
                                asset_id: assetId,
                                reason,
                            });
                            modal.closeModal();
                            toast.success('Deletion proposal created successfully.');
                            await loadProposals();
                        } catch (error) {
                            let errorMessage = 'Failed to create proposal.';
                            if (error instanceof ApiError && error.message) {
                                errorMessage = error.message;
                            }
                            toast.error(errorMessage);
                        }
                    },
                },
            ],
        });

        // Bind scope change
        const scopeSelect = document.getElementById('scope-select');
        if (scopeSelect) {
            scopeSelect.addEventListener('change', async (e) => {
                selectedScope = e.target.value;
                if (selectedScope !== 'community') {
                    selectedAssetType = 'signal_group';
                }
                await loadAssets();
                showModal();
            });
        }

        // Bind region change
        const regionSelect = document.getElementById('region-select');
        if (regionSelect) {
            regionSelect.addEventListener('change', async (e) => {
                selectedRegionId = e.target.value;
                await loadAssets();
                showModal();
            });
        }

        // Bind asset type change
        const assetTypeSelect = document.getElementById('asset-type-select');
        if (assetTypeSelect) {
            assetTypeSelect.addEventListener('change', async (e) => {
                selectedAssetType = e.target.value;
                await loadAssets();
                showModal();
            });
        }

        // Bind school change
        const schoolSelect = document.getElementById('school-select');
        if (schoolSelect) {
            schoolSelect.addEventListener('change', async (e) => {
                selectedSchoolId = e.target.value;
                await loadAssets();
                showModal();
            });
        }
    };

    showModal();
}

function formatDate(dateString) {
    if (!dateString) return '';
    const date = new Date(dateString);
    return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
    });
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function cleanup() {
    currentFilter = 'pending';
    adminRegions = [];
    mySchools = [];
}

export default { render, cleanup };
