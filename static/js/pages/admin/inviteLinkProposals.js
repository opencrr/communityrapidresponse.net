/**
 * Admin: Invite Link Proposals page
 * Allows admins to view and vote on invite link update proposals.
 */

import {
    getAllInviteLinkProposals,
    getInviteLinkProposalDetails,
    voteOnInviteLinkProposal,
    expireInviteLinkProposal,
} from '../../api/signalGroups.js';
import { ApiError } from '../../api/client.js';
import { isAdmin, isSuperuser } from '../../utils/store.js';
import toast from '../../components/toast.js';
import modal from '../../components/modal.js';
import { navigate } from '../../app.js';

let currentFilter = 'pending';

/**
 * Render the invite link proposals page
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
                        You need both postcard and vouch verification to view proposals.
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
                    <h1 class="page__title">Invite Link Proposals</h1>
                    <p class="page__subtitle">Review and vote on proposals to update Signal group invite links.</p>
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
        const proposals = await getAllInviteLinkProposals(params);

        if (proposals.length === 0) {
            const emptyMessage = currentFilter === 'pending'
                ? 'There are no pending proposals requiring your vote.'
                : `No ${currentFilter || ''} proposals found.`;
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
                        <div class="proposal-card__group">${escapeHtml(proposal.group_name)}</div>
                        <div class="proposal-card__region">${escapeHtml(proposal.region_name)}</div>
                    </div>
                    <span class="badge badge--${statusClass}">${statusLabel}</span>
                </div>
                <div class="proposal-card__meta">
                    <span>Proposed by ${escapeHtml(proposal.proposed_by)}</span>
                    <span>${formatDate(proposal.created_at)}</span>
                </div>
                <div class="vote-progress">
                    ${renderVoteProgress(proposal.votes, proposal.votes_needed)}
                </div>
                <div class="proposal-card__footer">
                    <span class="proposal-card__expires">
                        ${isPending ? `Expires: ${formatDate(proposal.expires_at)}` : `${statusLabel}: ${formatDate(proposal.expires_at)}`}
                    </span>
                    <button class="btn btn--sm btn--primary view-proposal-btn" data-proposal-id="${proposal.id}">
                        ${isPending ? 'Vote' : 'View'}
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
    const loadingModal = modal.showModal({
        title: 'Loading Proposal...',
        content: `
            <div class="loading">
                <div class="spinner spinner--lg"></div>
            </div>
        `,
        actions: [],
    });

    try {
        const proposal = await getInviteLinkProposalDetails(proposalId);
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
        if (proposal.has_voted) {
            const userVote = proposal.votes.find(v => proposal.can_vote === false);
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
                    <div class="proposal-detail__group">${escapeHtml(proposal.signal_group_name)}</div>
                    <div class="proposal-detail__region">${escapeHtml(proposal.region_name)}</div>
                    <span class="badge badge--${statusClass}">${capitalizeFirst(proposal.status)}</span>
                </div>

                <div class="proposal-detail__section">
                    <label>Proposed by</label>
                    <p>${escapeHtml(proposal.proposed_by?.username || 'Unknown')}</p>
                </div>

                ${proposal.reason ? `
                    <div class="proposal-detail__section">
                        <label>Reason</label>
                        <p>${escapeHtml(proposal.reason)}</p>
                    </div>
                ` : ''}

                ${proposal.new_invite_link ? `
                    <div class="proposal-detail__section">
                        <label>New Invite Link</label>
                        <div class="invite-link-display">
                            <code class="invite-link-code">${escapeHtml(proposal.new_invite_link)}</code>
                        </div>
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
            title: 'Proposal Details',
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
        const result = await voteOnInviteLinkProposal(proposalId, approve);
        modal.closeModal();

        if (result.invite_link_updated) {
            toast.success('Vote recorded! Proposal approved - invite link has been updated.');
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
        await expireInviteLinkProposal(proposalId);
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
}

export default { render, cleanup };
