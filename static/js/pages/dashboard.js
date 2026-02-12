/**
 * User dashboard page
 * Shows user status, verification status, regions, and groups.
 */

import { store, getUser, isPostcardVerified, isVouchVerified, hasReadAccess, isAdmin, isSuperuser, getVerificationStatus as getVerificationStatusFromStore } from '../utils/store.js';
import { getMyRegions } from '../api/regions.js';
import { getGroups } from '../api/signalGroups.js';
import { getVerificationStatus as getVerificationStatusAPI, getPendingVouchRequests, vouchForUser } from '../api/verification.js';
import { listMyRequests, listMyInvitations } from '../api/membership.js';
import { getMySchools } from '../api/schools.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';
import { navigate } from '../app.js';

// Module-level state for detailed verification status
let detailedVerificationStatus = null;

/**
 * Render the dashboard page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const user = getUser();
    const verificationStatus = getVerificationStatusFromStore();
    const postcardVerified = isPostcardVerified();
    const vouchVerified = isVouchVerified();
    const userHasReadAccess = hasReadAccess();
    const userIsAdmin = isAdmin();
    const userIsSuperuser = isSuperuser();

    // Fetch detailed verification status from API for vouch progress info
    try {
        detailedVerificationStatus = await getVerificationStatusAPI();
    } catch (error) {
        console.error('Failed to fetch detailed verification status:', error);
        detailedVerificationStatus = {};
    }

    // Get vouch progress info
    const vouchCount = detailedVerificationStatus?.vouch_count || 0;
    const pendingVouchRegion = detailedVerificationStatus?.pending_vouch_region;
    const hasPendingVouchRequest = !!pendingVouchRegion;

    // Bootstrap mode info - region may have fewer than 3 full admins
    // In bootstrap mode: postcard-only users can vouch, and 3 vouches are required instead of 2
    const bootstrapMode = detailedVerificationStatus?.bootstrap_mode || false;
    const vouchesRequired = detailedVerificationStatus?.vouches_required || (bootstrapMode ? 3 : 2);

    // In bootstrap mode, any member can vouch (not just full admins)
    const canVouch = userIsAdmin || bootstrapMode;

    // Check if user is blocked
    const isBlocked = user?.is_blocked || false;
    const blockReason = user?.block_reason || '';

    // Initial render with loading state
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">Dashboard</h1>
                </div>

                ${isBlocked ? `
                <div class="alert alert--danger" style="margin-bottom: var(--space-6);">
                    <div class="alert__icon">&#x26D4;</div>
                    <div class="alert__content">
                        <strong>Your account has been restricted</strong>
                        <p style="margin-top: var(--space-2);">
                            Your access has been revoked by moderators.
                            ${blockReason ? `<br><strong>Reason:</strong> ${escapeHtml(blockReason)}` : ''}
                        </p>
                        <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                            If you believe this is an error, please contact the community administrators.
                        </p>
                    </div>
                </div>
                ` : ''}

                <div class="dashboard-grid">
                    <!-- User Status Card -->
                    <div class="card">
                        <div class="card__body">
                            <div class="user-status">
                                <div class="user-status__avatar">
                                    ${getInitials(user?.username || user?.email || '')}
                                </div>
                                <div class="user-status__info">
                                    <h3>${escapeHtml(user?.username || 'User')}</h3>
                                    <p class="user-status__email">${escapeHtml(user?.email || '')}</p>
                                    <span class="header__tier-badge header__tier-badge--${verificationStatus.level}">
                                        ${verificationStatus.label}
                                    </span>
                                </div>
                            </div>
                        </div>
                        <div class="card__footer">
                            <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                ${verificationStatus.description}
                            </p>
                        </div>
                    </div>

                    <!-- Verification Status Card -->
                    <div class="card" id="verification-card">
                        <div class="card__header">
                            <h2 class="card__title">Verification Status</h2>
                        </div>
                        <div class="card__body">
                            <!-- Vouch Verification (Step 1) -->
                            <div class="verification-status ${vouchVerified ? 'verification-status--verified' : hasPendingVouchRequest ? 'verification-status--pending' : ''}" style="margin-bottom: var(--space-4);">
                                <div class="verification-status__icon">${vouchVerified ? '&#x2713;' : hasPendingVouchRequest ? '&#x23F3;' : '&#x1F91D;'}</div>
                                <div style="flex: 1;">
                                    <strong>Step 1: Vouch Verification</strong>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600); margin-top: var(--space-1);">
                                        ${vouchVerified
                                            ? 'You have been vouched for by community members. You can view Signal groups.'
                                            : hasPendingVouchRequest
                                                ? `Waiting for vouches in <strong>${escapeHtml(pendingVouchRegion.name)}</strong> (${vouchCount}/${vouchesRequired} received)${bootstrapMode ? ' <em>(bootstrap mode)</em>' : ''}`
                                                : `Enter your address and get vouched by ${vouchesRequired} community members.${bootstrapMode ? ' (bootstrap mode - community building initial admin team)' : ''}`}
                                    </p>
                                    ${hasPendingVouchRequest && !vouchVerified ? `
                                        <div class="vouch-progress-mini" style="margin-top: var(--space-2);">
                                            <div class="vouch-progress-mini__bar" style="height: 4px; background: var(--color-gray-200); border-radius: 2px; overflow: hidden;">
                                                <div style="width: ${(vouchCount / vouchesRequired) * 100}%; height: 100%; background: var(--color-primary-500);"></div>
                                            </div>
                                        </div>
                                    ` : ''}
                                </div>
                                ${!vouchVerified ? `
                                    <a href="/vouch" class="btn btn--sm btn--primary" data-link>${hasPendingVouchRequest ? 'View' : 'Start'}</a>
                                ` : ''}
                            </div>

                            <!-- Postcard Verification (Step 2) -->
                            <div class="verification-status ${postcardVerified ? 'verification-status--verified' : ''}">
                                <div class="verification-status__icon">${postcardVerified ? '&#x2713;' : '&#x1F4EC;'}</div>
                                <div style="flex: 1;">
                                    <strong>Step 2: Address Verification</strong>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600); margin-top: var(--space-1);">
                                        ${postcardVerified
                                            ? 'Your address has been verified via postcard.'
                                            : vouchVerified
                                                ? 'Verify your address via postcard to become a community admin.'
                                                : 'Complete vouch verification first, then verify your address to become admin.'}
                                    </p>
                                </div>
                                ${!postcardVerified && vouchVerified ? `
                                    <a href="/verify" class="btn btn--sm btn--primary" data-link>Verify</a>
                                ` : ''}
                            </div>

                            ${!userIsAdmin && vouchVerified && !postcardVerified ? `
                                <div class="privacy-notice" style="margin-top: var(--space-4); background: #fef3c7;">
                                    <div class="privacy-notice__icon">&#x1F511;</div>
                                    <div class="privacy-notice__text">
                                        <strong>Want admin access?</strong>
                                        Verify your address via postcard to become a community admin and manage Signal groups.
                                    </div>
                                </div>
                            ` : ''}

                            ${bootstrapMode && !userIsAdmin ? `
                                <div class="privacy-notice" style="margin-top: var(--space-4); background: #dbeafe;">
                                    <div class="privacy-notice__icon">&#x1F680;</div>
                                    <div class="privacy-notice__text">
                                        <strong>Bootstrap Mode Active</strong>
                                        This community is building its initial admin team. Any community member can <a href="/vouch" data-link>vouch for others</a> to help establish the community.
                                    </div>
                                </div>
                            ` : ''}

                            ${userIsAdmin ? `
                                <div class="privacy-notice" style="margin-top: var(--space-4); background: #dcfce7;">
                                    <div class="privacy-notice__icon">&#x2713;</div>
                                    <div class="privacy-notice__text">
                                        <strong>Full Admin Access</strong>
                                        You can create communities, manage Signal groups, and <a href="/vouch" data-link>vouch for new members</a>.
                                    </div>
                                </div>
                            ` : ''}
                        </div>
                    </div>
                </div>

                ${canVouch ? `
                    <!-- Pending Vouch Requests Section (Admin or postcard-verified in bootstrap mode) -->
                    <div class="dashboard-section mt-6" id="pending-vouches-section">
                        <h2 class="dashboard-section__title">Pending Vouch Requests</h2>
                        <p class="text-muted" style="margin-bottom: var(--space-4);">
                            Users waiting for vouches in your communities
                            ${bootstrapMode && !userIsAdmin ? ' (bootstrap mode - any member can vouch)' : ''}
                        </p>
                        <div id="pending-vouches-content" data-bootstrap-mode="${bootstrapMode}" data-vouches-required="${vouchesRequired}">
                            <div class="loading">
                                <div class="spinner"></div>
                            </div>
                        </div>
                    </div>
                ` : ''}

                ${userHasReadAccess ? `
                    <!-- My Signal Groups Section -->
                    <div class="dashboard-section mt-6">
                        <h2 class="dashboard-section__title">My Signal Groups</h2>
                        <div id="my-groups">
                            <div class="loading">
                                <div class="spinner"></div>
                            </div>
                        </div>
                    </div>

                    <!-- Sub-Region Membership Section -->
                    <div class="dashboard-section mt-6">
                        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--space-4);">
                            <h2 class="dashboard-section__title" style="margin: 0;">Membership</h2>
                            <a href="/membership" class="btn btn--sm btn--secondary" data-link>View All</a>
                        </div>
                        <div id="my-membership-requests">
                            <div class="loading">
                                <div class="spinner"></div>
                            </div>
                        </div>
                    </div>
                ` : ''}

                <!-- My Schools Section -->
                <div class="dashboard-section mt-6">
                    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--space-4);">
                        <h2 class="dashboard-section__title" style="margin: 0;">My Schools</h2>
                        <a href="/schools" class="btn btn--sm btn--secondary" data-link>Browse Schools</a>
                    </div>
                    <div id="my-schools">
                        <div class="loading">
                            <div class="spinner"></div>
                        </div>
                    </div>
                </div>

                ${userIsAdmin ? `
                    <!-- My Regions Section (Admin only) -->
                    <div class="dashboard-section mt-6">
                        <h2 class="dashboard-section__title">My Communities</h2>
                        <div id="my-regions" class="dashboard-grid">
                            <div class="loading">
                                <div class="spinner"></div>
                            </div>
                        </div>
                    </div>

                    <!-- Admin Tools Section -->
                    <div class="dashboard-section mt-6">
                        <h2 class="dashboard-section__title">Admin Tools</h2>
                        <div class="dashboard-grid">
                            <a href="/admin/communities" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F5FA;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Create Community</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Draw boundaries for new neighborhoods and city blocks.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/groups" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F4AC;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Manage Groups</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Create and manage Signal groups for your communities.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/proposals" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F4CB;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Proposals</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Review and vote on invite link update proposals.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/membership-requests" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F465;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Membership Requests</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Review and approve membership requests.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/blocklist-proposals" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F6AB;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Blocklist Proposals</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Propose and vote on blocking users from your communities.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/deletion-proposals" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F5D1;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Deletion Proposals</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Propose and vote on deleting Signal groups and sub-regions.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/reports" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F6A9;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">User Reports</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Review reports filed by community members.
                                    </p>
                                </div>
                            </a>
                        </div>
                    </div>
                ` : ''}

                ${userIsSuperuser ? `
                    <!-- Superuser Tools Section -->
                    <div class="dashboard-section mt-6">
                        <h2 class="dashboard-section__title">Superuser Tools</h2>
                        <div class="dashboard-grid">
                            <a href="/admin/users" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F465;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Manage Users</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Search for users and grant or revoke vouch verification.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/audit-logs" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F4DD;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Audit Logs</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        View system activity and security events.
                                    </p>
                                </div>
                            </a>
                            <a href="/admin/blocked-addresses" class="card card--interactive" data-link>
                                <div class="card__body">
                                    <div style="font-size: 32px; margin-bottom: var(--space-2);">&#x1F4CD;</div>
                                    <h3 style="font-weight: 600; margin-bottom: var(--space-1);">Blocked Locations</h3>
                                    <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                        Manage location fingerprints for blocked users.
                                    </p>
                                </div>
                            </a>
                        </div>
                    </div>
                ` : ''}
            </div>
        </div>
    `;

    // Load data based on access level
    const loadPromises = [];
    loadPromises.push(loadMySchools());
    if (userHasReadAccess) {
        loadPromises.push(loadMyGroups());
        loadPromises.push(loadMyMembershipRequests());
    }
    if (userIsAdmin) {
        loadPromises.push(loadMyRegions());
    }
    if (canVouch) {
        loadPromises.push(loadPendingVouches());
    }
    await Promise.all(loadPromises);
}

/**
 * Load pending vouch requests for admin or postcard-verified user in bootstrap mode
 */
async function loadPendingVouches() {
    const content = document.getElementById('pending-vouches-content');
    if (!content) return;

    // Get bootstrap mode and vouches required from data attributes
    const bootstrapMode = content.dataset.bootstrapMode === 'true';
    const vouchesRequired = parseInt(content.dataset.vouchesRequired, 10) || 2;

    try {
        const pending = await getPendingVouchRequests();
        if (pending.length === 0) {
            content.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <div class="empty-state__icon">&#x2705;</div>
                    <p>No pending vouch requests in your communities</p>
                </div>
            `;
            return;
        }
        content.innerHTML = renderPendingVouchList(pending, vouchesRequired);
        bindVouchButtons(bootstrapMode);
    } catch (error) {
        console.error('Failed to load pending vouch requests:', error);
        content.innerHTML = `<p class="form-error">Failed to load pending requests</p>`;
    }
}

/**
 * Render pending vouch list
 * @param {Array} pending - Array of pending vouch requests
 * @param {number} vouchesRequired - Number of vouches required (2 normally, 3 in bootstrap mode)
 * @returns {string} HTML string
 */
function renderPendingVouchList(pending, vouchesRequired = 2) {
    return `
        <div class="pending-vouch-list">
            ${pending.map(req => `
                <div class="pending-vouch-item" data-user-id="${req.user_id}">
                    <div class="pending-vouch-info">
                        <strong>${escapeHtml(req.username)}</strong>
                        <span class="text-muted">${escapeHtml(req.email)}</span>
                        <span class="badge">${escapeHtml(req.region_name)}</span>
                        <span class="text-muted">${req.vouch_count}/${vouchesRequired} vouches</span>
                    </div>
                    <button class="btn btn--primary btn--sm vouch-btn"
                            data-user-id="${req.user_id}"
                            data-username="${escapeHtml(req.username)}">
                        Vouch
                    </button>
                </div>
            `).join('')}
        </div>
    `;
}

/**
 * Bind click handlers to vouch buttons
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 */
function bindVouchButtons(bootstrapMode = false) {
    document.querySelectorAll('.vouch-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
            const userId = btn.dataset.userId;
            const username = btn.dataset.username;

            let confirmMessage = 'This confirms you know this person and they live in your community.';
            if (bootstrapMode) {
                confirmMessage += ' Note: There is a 1-hour cooldown between vouches in bootstrap mode.';
            }

            const confirmed = await modal.confirm({
                title: `Vouch for ${username}?`,
                message: confirmMessage,
            });

            if (confirmed) {
                try {
                    btn.disabled = true;
                    btn.classList.add('btn--loading');
                    await vouchForUser(userId);
                    toast.success(`Vouched for ${username}!`);
                    await loadPendingVouches(); // Refresh list
                } catch (error) {
                    // Handle cooldown error specifically
                    let errorMessage = error.message || 'Failed to vouch';
                    if (error.status === 429 && error.data?.cooldown_end) {
                        const cooldownEnd = new Date(error.data.cooldown_end);
                        const minutesLeft = Math.ceil((cooldownEnd - new Date()) / 60000);
                        errorMessage = `Bootstrap cooldown: wait ${minutesLeft} minutes`;
                    }
                    toast.error(errorMessage);
                    btn.disabled = false;
                    btn.classList.remove('btn--loading');
                }
            }
        });
    });
}

/**
 * Load user's regions
 */
async function loadMyRegions() {
    const regionContainer = document.getElementById('my-regions');
    if (!regionContainer) return;

    try {
        const regions = await getMyRegions();

        if (regions.length === 0) {
            regionContainer.innerHTML = `
                <div class="card">
                    <div class="card__body">
                        <div class="empty-state">
                            <div class="empty-state__icon">&#x1F5FA;</div>
                            <h3 class="empty-state__title">No Communities Yet</h3>
                            <p class="empty-state__description">
                                You haven't created or joined any communities yet.
                            </p>
                            <a href="/admin/communities" class="btn btn--primary" data-link>Create a Community</a>
                        </div>
                    </div>
                </div>
            `;
            return;
        }

        regionContainer.innerHTML = regions.map(region => `
            <a href="/communities/${region.id}" class="region-item" data-link>
                <span class="region-item__type">${formatRegionType(region.type)}</span>
                <div class="region-item__name">${escapeHtml(region.name)}</div>
                ${region.member_count !== undefined ? `
                    <div class="region-item__meta">${region.member_count} members</div>
                ` : ''}
            </a>
        `).join('');
    } catch (error) {
        console.error('Failed to load regions:', error);
        regionContainer.innerHTML = `
            <p class="form-error">Failed to load communities.</p>
        `;
    }
}

/**
 * Load user's membership requests and invitations
 */
async function loadMyMembershipRequests() {
    const container = document.getElementById('my-membership-requests');
    if (!container) return;

    try {
        const [requestsResponse, invitationsResponse] = await Promise.all([
            listMyRequests({ status: 'pending' }),
            listMyInvitations(),
        ]);

        const pendingRequests = (requestsResponse.requests || []).filter(r => r.request_type === 'request' && r.status === 'pending');
        const pendingInvitations = invitationsResponse.requests || [];

        if (pendingRequests.length === 0 && pendingInvitations.length === 0) {
            container.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <p>No pending requests or invitations. <a href="/communities" data-link>Browse communities</a> to find communities to join.</p>
                </div>
            `;
            return;
        }

        container.innerHTML = `
            <div class="dashboard-grid">
                ${pendingInvitations.map(inv => `
                    <a href="/membership" class="card card--interactive" data-link>
                        <div class="card__body">
                            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--space-2);">
                                <span class="badge" style="background: #e0e7ff; color: #3730a3;">Invitation</span>
                            </div>
                            <h3 style="font-weight: 600; margin-bottom: var(--space-1);">${escapeHtml(inv.region_name || 'Unknown Community')}</h3>
                            <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                Invited by ${escapeHtml(inv.initiated_by_name || 'Admin')}
                            </p>
                        </div>
                    </a>
                `).join('')}
                ${pendingRequests.map(req => `
                    <a href="/membership" class="card card--interactive" data-link>
                        <div class="card__body">
                            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--space-2);">
                                <span class="badge badge--pending">Pending</span>
                                <span style="font-size: var(--font-size-xs); color: var(--color-gray-500);">
                                    ${req.current_votes || 0}/${req.votes_needed || 2} approvals
                                </span>
                            </div>
                            <h3 style="font-weight: 600; margin-bottom: var(--space-1);">${escapeHtml(req.region_name || 'Unknown Community')}</h3>
                            <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                Request pending admin approval
                            </p>
                        </div>
                    </a>
                `).join('')}
            </div>
        `;
    } catch (error) {
        console.error('Failed to load membership requests:', error);
        container.innerHTML = `
            <p class="form-error">Failed to load membership requests.</p>
        `;
    }
}

/**
 * Load user's schools
 */
async function loadMySchools() {
    const schoolsContainer = document.getElementById('my-schools');
    if (!schoolsContainer) return;

    try {
        const response = await getMySchools();
        const schools = response.schools || [];

        if (schools.length === 0) {
            schoolsContainer.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <p>You haven't joined any schools yet. <a href="/schools" data-link>Browse Schools</a></p>
                </div>
            `;
            return;
        }

        schoolsContainer.innerHTML = `
            <div class="dashboard-grid">
                ${schools.map(school => {
                    const isVerified = school.verification_status === 'verified';
                    const isPending = school.verification_status === 'pending';
                    const vouchesRequired = school.vouch_count + school.vouches_needed;
                    const vouchProgress = vouchesRequired > 0 ? (school.vouch_count / vouchesRequired) * 100 : 0;

                    return `
                        <a href="/schools/${school.id}" class="card card--interactive" data-link>
                            <div class="card__body">
                                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--space-2);">
                                    <div style="display: flex; gap: var(--space-2); align-items: center; flex-wrap: wrap;">
                                        ${isVerified ? '<span class="badge badge--success">Verified</span>' : ''}
                                        ${isPending ? '<span class="badge badge--pending">Pending</span>' : ''}
                                        ${school.is_admin ? '<span class="badge badge--info">Admin</span>' : ''}
                                        ${school.bootstrap_mode ? '<span class="badge badge--warning">Bootstrap</span>' : ''}
                                    </div>
                                </div>
                                <h3 style="font-weight: 600; margin-bottom: var(--space-1);">${escapeHtml(school.name)}</h3>
                                <p style="font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                    ${escapeHtml(school.city || '')}${school.city ? ', ' : ''}${escapeHtml(school.state)}
                                    ${school.district_name ? ` &middot; ${escapeHtml(school.district_name)}` : ''}
                                </p>
                                ${isPending && vouchesRequired > 0 ? `
                                    <div style="margin-top: var(--space-3);">
                                        <div style="display: flex; justify-content: space-between; font-size: var(--font-size-xs); color: var(--color-gray-500); margin-bottom: var(--space-1);">
                                            <span>Vouch progress</span>
                                            <span>${school.vouch_count}/${vouchesRequired} vouches</span>
                                        </div>
                                        <div style="height: 4px; background: var(--color-gray-200); border-radius: 2px; overflow: hidden;">
                                            <div style="width: ${vouchProgress}%; height: 100%; background: var(--color-primary-500); transition: width 0.3s;"></div>
                                        </div>
                                    </div>
                                ` : ''}
                            </div>
                        </a>
                    `;
                }).join('')}
            </div>
        `;
    } catch (error) {
        console.error('Failed to load schools:', error);
        schoolsContainer.innerHTML = `
            <p class="form-error">Failed to load schools.</p>
        `;
    }
}

/**
 * Load user's Signal groups
 */
async function loadMyGroups() {
    const groupsContainer = document.getElementById('my-groups');
    if (!groupsContainer) return;

    try {
        const groups = await getGroups();

        if (groups.length === 0) {
            groupsContainer.innerHTML = `
                <div class="card">
                    <div class="card__body">
                        <div class="empty-state">
                            <div class="empty-state__icon">&#x1F4AC;</div>
                            <h3 class="empty-state__title">No Signal Groups</h3>
                            <p class="empty-state__description">
                                You don't have access to any Signal groups yet.
                            </p>
                            <a href="/groups" class="btn btn--primary" data-link>Browse Groups</a>
                        </div>
                    </div>
                </div>
            `;
            return;
        }

        groupsContainer.innerHTML = `
            <div class="dashboard-grid">
                ${groups.map(group => `
                    <div class="card group-card">
                        <div class="card__body">
                            <div class="group-card__header">
                                <div>
                                    <div class="group-card__name">${escapeHtml(group.name)}${group.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
                                    <div class="group-card__region">${escapeHtml(group.region_name || group.school_name || group.district_name || '')}</div>
                                </div>
                            </div>
                            ${group.description ? `
                                <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                                    ${escapeHtml(group.description)}
                                </p>
                            ` : ''}
                            <div class="group-invite">
                                <span class="group-invite__link">${escapeHtml(group.invite_link || 'Invite link hidden')}</span>
                                ${group.invite_link ? `
                                    <button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(group.invite_link)}">
                                        Copy
                                    </button>
                                ` : ''}
                            </div>
                        </div>
                    </div>
                `).join('')}
            </div>
        `;

        // Bind copy buttons
        const copyButtons = groupsContainer.querySelectorAll('.copy-link-btn');
        copyButtons.forEach(btn => {
            btn.addEventListener('click', async () => {
                const link = btn.dataset.link;
                try {
                    await navigator.clipboard.writeText(link);
                    toast.success('Invite link copied to clipboard');
                } catch (error) {
                    toast.error('Failed to copy link');
                }
            });
        });
    } catch (error) {
        console.error('Failed to load groups:', error);
        groupsContainer.innerHTML = `
            <p class="form-error">Failed to load Signal groups.</p>
        `;
    }
}

/**
 * Get initials from name
 * @param {string} name - User name
 * @returns {string} Initials
 */
function getInitials(name) {
    if (!name) return '?';
    const parts = name.trim().split(/\s+/);
    if (parts.length === 1) {
        return parts[0].charAt(0).toUpperCase();
    }
    return (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase();
}

/**
 * Format region type for display
 * @param {string} type - Region type
 * @returns {string} Formatted type
 */
function formatRegionType(type) {
    const labels = {
        state: 'State',
        county: 'County',
        city: 'City',
        locality: 'Locality',
        neighborhood: 'Neighborhood',
        city_block: 'Block',
    };
    return labels[type] || type;
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
    // No cleanup needed
}

export default { render, cleanup };
