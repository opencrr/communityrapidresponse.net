/**
 * Admin: Blocklist Proposals page
 * Allows admins to create, view, and vote on blocklist proposals.
 */

import {
    getBlocklistProposals,
    getBlocklistProposalDetails,
    createBlocklistProposal,
    voteOnBlocklistProposal,
    expireBlocklistProposal,
    getRegionUsers,
} from '../../api/blocklist.js?v=c49aed9';
import { getAdminRegions } from '../../api/regions.js?v=c49aed9';
import { ApiError } from '../../api/client.js?v=c49aed9';
import { isAdmin, isSuperuser, getUser } from '../../utils/store.js?v=c49aed9';
import toast from '../../components/toast.js?v=c49aed9';
import modal from '../../components/modal.js?v=c49aed9';
import { navigate } from '../../app.js?v=c49aed9';

let currentFilter = 'pending';
let adminRegions = [];

/**
 * Render the blocklist proposals page
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
                        You need both postcard and vouch verification to view blocklist proposals.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>View Verification Status</a>
                </div>
            </div>
        `;
        return;
    }

    // Get status filter from URL
    const urlParams = new URLSearchParams(window.location.search);
    currentFilter = urlParams.get('status') || 'pending';

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <div class="page__header-content">
                        <h1 class="page__title">Blocklist Proposals</h1>
                        <p class="page__subtitle">Review and vote on proposals to blocklist users from your communities.</p>
                    </div>
                    <button class="btn btn--primary" id="create-proposal-btn">
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
            // Update URL without reload
            const url = new URL(window.location);
            if (status) {
                url.searchParams.set('status', status);
            } else {
                url.searchParams.delete('status');
            }
            window.history.replaceState({}, '', url);
            // Update active tab
            document.querySelectorAll('.filter-tab').forEach(t => t.classList.remove('filter-tab--active'));
            tab.classList.add('filter-tab--active');
            // Reload proposals
            loadProposals();
        });
    });

    // Bind create proposal button
    const createBtn = document.getElementById('create-proposal-btn');
    if (createBtn) {
        createBtn.addEventListener('click', showCreateProposalModal);
    }

    // Load admin regions for the create modal
    try {
        adminRegions = await getAdminRegions();
    } catch (error) {
        console.warn('Failed to load admin regions:', error);
        adminRegions = [];
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
        const proposals = await getBlocklistProposals(params);

        if (proposals.length === 0) {
            const emptyMessage = currentFilter === 'pending'
                ? 'There are no pending blocklist proposals requiring your vote.'
                : `No ${currentFilter || ''} blocklist proposals found.`;
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

        // Bind card actions
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
 * @param {Object} proposal - Proposal data
 * @returns {string} HTML string
 */
function renderProposalCard(proposal) {
    const statusClass = getStatusClass(proposal.status);
    const statusLabel = capitalizeFirst(proposal.status);
    const isPending = proposal.status === 'pending';

    return `
        <div class="card proposal-card proposal-card--${proposal.status}" data-proposal-id="${proposal.id}">
            <div class="card__body">
                <div class="proposal-card__header">
                    <div>
                        <div class="proposal-card__group">Blocklist: ${escapeHtml(proposal.target_user?.username || 'Unknown User')}</div>
                        <div class="proposal-card__region">${escapeHtml(proposal.region_name)}</div>
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
                        ${isPending ? `Expires: ${formatDate(proposal.expires_at)}` : `${statusLabel}: ${formatDate(proposal.resolved_at || proposal.expires_at)}`}
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
 * @param {number} currentVotes - Number of approval votes cast
 * @param {number} votesNeeded - Number of votes needed for approval
 * @returns {string} HTML string
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

/**
 * Get CSS class for status badge
 * @param {string} status - Proposal status
 * @returns {string} CSS class suffix
 */
function getStatusClass(status) {
    switch (status) {
        case 'pending': return 'pending';
        case 'approved': return 'success';
        case 'expired': return 'expired';
        case 'rejected': return 'danger';
        default: return 'secondary';
    }
}

/**
 * Capitalize first letter
 * @param {string} str - String to capitalize
 * @returns {string} Capitalized string
 */
function capitalizeFirst(str) {
    if (!str) return '';
    return str.charAt(0).toUpperCase() + str.slice(1);
}

/**
 * Truncate string to length
 * @param {string} str - String to truncate
 * @param {number} length - Max length
 * @returns {string} Truncated string
 */
function truncate(str, length) {
    if (!str) return '';
    if (str.length <= length) return str;
    return str.substring(0, length) + '...';
}

/**
 * Bind event handlers for proposal actions
 */
function bindProposalActions() {
    document.querySelectorAll('.view-proposal-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const proposalId = btn.dataset.proposalId;
            showProposalDetailModal(proposalId);
        });
    });
}

/**
 * Show proposal detail modal
 * @param {string} proposalId - Proposal ID
 */
async function showProposalDetailModal(proposalId) {
    const userIsSuperuser = isSuperuser();

    // Show loading modal
    modal.showModal({
        title: 'Loading Proposal...',
        content: `
            <div class="loading">
                <div class="spinner spinner--lg"></div>
            </div>
        `,
        actions: [],
    });

    try {
        const proposal = await getBlocklistProposalDetails(proposalId);
        modal.closeModal();

        const isPending = proposal.status === 'pending';
        const statusClass = getStatusClass(proposal.status);

        // Build vote list
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

        // Build user vote status
        let userVoteStatus = '';
        if (!proposal.can_vote && isPending) {
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

        const contentHtml = `
            <div class="proposal-detail">
                <div class="proposal-detail__header">
                    <div class="proposal-detail__group">Blocklist: ${escapeHtml(proposal.target_user?.username || 'Unknown User')}</div>
                    <div class="proposal-detail__region">${escapeHtml(proposal.region_name)}</div>
                    <span class="badge badge--${statusClass}">${capitalizeFirst(proposal.status)}</span>
                </div>

                <div class="proposal-detail__section">
                    <label>Target User</label>
                    <p>${escapeHtml(proposal.target_user?.username || 'Unknown')} (${escapeHtml(proposal.target_user?.email || 'No email')})</p>
                </div>

                <div class="proposal-detail__section">
                    <label>Proposed by</label>
                    <p>${escapeHtml(proposal.proposed_by?.username || 'Unknown')}</p>
                </div>

                <div class="proposal-detail__section">
                    <label>Reason</label>
                    <p>${escapeHtml(proposal.reason)}</p>
                </div>

                ${proposal.evidence ? `
                    <div class="proposal-detail__section">
                        <label>Evidence</label>
                        <p>${escapeHtml(proposal.evidence)}</p>
                    </div>
                ` : ''}

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
                    <strong>Warning:</strong> If approved, this user will be removed from <strong>all communities</strong> and their verified address will be blocklisted for 2 years.
                </div>

                <div class="proposal-detail__timestamps">
                    <span>Created: ${formatDate(proposal.created_at)}</span>
                    <span>Expires: ${formatDate(proposal.expires_at)}</span>
                    ${proposal.resolved_at ? `<span>Resolved: ${formatDate(proposal.resolved_at)}</span>` : ''}
                </div>
            </div>
        `;

        // Build actions
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
            // Add expire button at the start for superusers
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
            title: 'Blocklist Proposal Details',
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
 * @param {string} proposalId - Proposal ID
 * @param {boolean} approve - True to approve, false to reject
 */
async function handleVote(proposalId, approve) {
    try {
        const result = await voteOnBlocklistProposal(proposalId, approve);
        modal.closeModal();

        if (result.user_blocklisted) {
            toast.success('Vote recorded! User has been blocklisted from all communities.');
        } else {
            toast.success(`Vote recorded! ${approve ? 'Approved' : 'Rejected'}.`);
        }

        // Reload proposals
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
 * @param {string} proposalId - Proposal ID
 */
async function handleExpire(proposalId) {
    const confirmed = await modal.confirm({
        title: 'Expire Proposal',
        message: 'Are you sure you want to expire this proposal? This action cannot be undone.',
        confirmLabel: 'Expire',
        confirmType: 'warning',
    });

    if (!confirmed) {
        // Re-open the detail modal
        showProposalDetailModal(proposalId);
        return;
    }

    try {
        await expireBlocklistProposal(proposalId);
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
 * Show create proposal modal
 */
async function showCreateProposalModal() {
    if (adminRegions.length === 0) {
        toast.error('You are not an admin of any communities.');
        return;
    }

    let selectedRegionId = adminRegions[0]?.id || '';
    let regionUsers = [];
    let selectedUserId = '';

    const renderModalContent = () => `
        <div class="form">
            <div class="form__group">
                <label for="region-select" class="form__label">Community</label>
                <select id="region-select" class="form__input">
                    ${adminRegions.map(r => `
                        <option value="${r.id}" ${r.id === selectedRegionId ? 'selected' : ''}>${escapeHtml(r.name)}</option>
                    `).join('')}
                </select>
                <p class="form__help">Select the community where you want to blocklist a user.</p>
            </div>

            <div class="form__group">
                <label for="user-select" class="form__label">Target User</label>
                <select id="user-select" class="form__input" ${regionUsers.length === 0 ? 'disabled' : ''}>
                    ${regionUsers.length === 0
                        ? '<option value="">Loading users...</option>'
                        : regionUsers.map(u => `
                            <option value="${u.id}" ${u.id === selectedUserId ? 'selected' : ''}>${escapeHtml(u.username)} (${escapeHtml(u.email)})</option>
                        `).join('')
                    }
                </select>
                <p class="form__help">Select the user to blocklist from this community.</p>
            </div>

            <div class="form__group">
                <label for="reason-input" class="form__label">Reason</label>
                <textarea id="reason-input" class="form__input form__textarea" rows="3" placeholder="Enter the reason for blocklisting this user..." required></textarea>
                <p class="form__help">This will be visible to other admins voting on the proposal.</p>
            </div>

            <div class="form__group">
                <label for="evidence-input" class="form__label">Evidence (optional)</label>
                <textarea id="evidence-input" class="form__input form__textarea" rows="2" placeholder="Provide any supporting evidence..."></textarea>
            </div>

            <div class="form__notice form__notice--warning">
                <strong>Note:</strong> If approved by 3 admins, the user will be removed from <strong>all regions</strong> and their verified address will be blocklisted for 2 years.
            </div>
        </div>
    `;

    const loadRegionUsers = async (regionId) => {
        try {
            regionUsers = await getRegionUsers(regionId);
            // Filter out the current user
            const currentUser = getUser();
            regionUsers = regionUsers.filter(u => u.id !== currentUser?.id);
            selectedUserId = regionUsers[0]?.id || '';
        } catch (error) {
            console.error('Failed to load region users:', error);
            regionUsers = [];
        }
    };

    // Initial load of users for first region
    await loadRegionUsers(selectedRegionId);

    const showModal = () => {
        modal.showModal({
            title: 'Create Blocklist Proposal',
            content: renderModalContent(),
            actions: [
                { label: 'Cancel', type: 'secondary' },
                {
                    label: 'Create Proposal',
                    type: 'primary',
                    closeOnClick: false,
                    onClick: async () => {
                        const reason = document.getElementById('reason-input')?.value?.trim();
                        const evidence = document.getElementById('evidence-input')?.value?.trim();
                        const targetUserId = document.getElementById('user-select')?.value;
                        const regionId = document.getElementById('region-select')?.value;

                        if (!targetUserId) {
                            toast.error('Please select a user to blocklist.');
                            return;
                        }

                        if (!reason) {
                            toast.error('Please provide a reason for the blocklist proposal.');
                            return;
                        }

                        try {
                            await createBlocklistProposal(regionId, targetUserId, reason, evidence || null);
                            modal.closeModal();
                            toast.success('Blocklist proposal created successfully.');
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

        // Bind region change handler
        const regionSelect = document.getElementById('region-select');
        if (regionSelect) {
            regionSelect.addEventListener('change', async (e) => {
                selectedRegionId = e.target.value;
                const userSelect = document.getElementById('user-select');
                if (userSelect) {
                    userSelect.innerHTML = '<option value="">Loading users...</option>';
                    userSelect.disabled = true;
                }
                await loadRegionUsers(selectedRegionId);
                if (userSelect) {
                    userSelect.innerHTML = regionUsers.length === 0
                        ? '<option value="">No users found</option>'
                        : regionUsers.map(u => `
                            <option value="${u.id}">${escapeHtml(u.username)} (${escapeHtml(u.email)})</option>
                        `).join('');
                    userSelect.disabled = regionUsers.length === 0;
                }
            });
        }
    };

    showModal();
}

/**
 * Format date for display
 * @param {string} dateString - ISO date string
 * @returns {string} Formatted date
 */
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

/**
 * Escape HTML to prevent XSS
 * @param {string} text - Text to escape
 * @returns {string} Escaped text
 */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function cleanup() {
    currentFilter = 'pending';
    adminRegions = [];
}

export default { render, cleanup };
