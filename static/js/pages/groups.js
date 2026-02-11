/**
 * Signal groups discovery page
 * Shows all Signal groups the user has access to.
 */

import { getGroups } from '../api/signalGroups.js?v=c49aed9';
import { hasReadAccess, isAdmin } from '../utils/store.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';

/**
 * Get a scope key for grouping (region:id, school:id, or district:id)
 * @param {Object} group - Group data
 * @returns {string} Scope key
 */
function getScopeKey(group) {
    if (group.region_id) return `region:${group.region_id}`;
    if (group.school_id) return `school:${group.school_id}`;
    if (group.district_id) return `district:${group.district_id}`;
    return 'unknown:';
}

/**
 * Get the display name for a group's scope
 * @param {Object} group - Group data
 * @returns {string} Scope name
 */
function getScopeName(group) {
    if (group.region_id) return group.region_name || 'Unknown Community';
    if (group.school_id) return group.school_name || 'Unknown School';
    if (group.district_id) return group.district_name || 'Unknown District';
    return 'Unknown';
}

/**
 * Get the link for a group's scope
 * @param {Object} group - Group data
 * @returns {string} Scope link path
 */
function getScopeLink(group) {
    if (group.region_id) return `/communities/${group.region_id}`;
    if (group.school_id) return `/schools/${group.school_id}`;
    if (group.district_id) return `/school-districts/${group.district_id}`;
    return '#';
}

/**
 * Get the scope type for sorting (regions first, schools second, districts third)
 * @param {string} scopeKey - Scope key
 * @returns {number} Sort order
 */
function getScopeOrder(scopeKey) {
    if (scopeKey.startsWith('region:')) return 0;
    if (scopeKey.startsWith('school:')) return 1;
    if (scopeKey.startsWith('district:')) return 2;
    return 3;
}

/**
 * Render the groups page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const userHasReadAccess = hasReadAccess();
    const userIsAdmin = isAdmin();

    if (!userHasReadAccess) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Verification Required</h3>
                    <p class="empty-state__description">
                        You need to verify your address or get vouched to access Signal groups.
                    </p>
                    <a href="/verify" class="btn btn--primary" data-link>Verify Your Address</a>
                </div>
            </div>
        `;
        return;
    }

    // Show loading state
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">Signal Groups</h1>
                    <p class="page__subtitle">
                        Connect with your neighbors through local Signal group chats.
                    </p>
                </div>

                <div class="loading">
                    <div class="spinner spinner--lg"></div>
                </div>
            </div>
        </div>
    `;

    try {
        const groups = await getGroups();
        renderGroups(container, groups);
    } catch (error) {
        console.error('Failed to load groups:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">Error Loading Groups</h3>
                    <p class="empty-state__description">
                        Failed to load Signal groups. Please try again later.
                    </p>
                    <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
                </div>
            </div>
        `;
    }
}

/**
 * Render the groups list
 * @param {HTMLElement} container - Container element
 * @param {Object[]} groups - Array of Signal groups
 */
function renderGroups(container, groups) {
    const userIsAdmin = isAdmin();

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header flex justify-between items-center">
                    <div>
                        <h1 class="page__title">Signal Groups</h1>
                        <p class="page__subtitle">
                            Connect with your neighbors through local Signal group chats.
                        </p>
                    </div>
                    ${userIsAdmin ? `
                        <a href="/admin/groups" class="btn btn--primary" data-link>
                            Create Group
                        </a>
                    ` : ''}
                </div>

                <!-- Search and Filter -->
                <div class="card mb-6">
                    <div class="card__body">
                        <input
                            type="search"
                            class="form-input"
                            placeholder="Search groups..."
                            id="group-search"
                        >
                    </div>
                </div>

                ${groups.length === 0 ? `
                    <div class="empty-state">
                        <div class="empty-state__icon">&#x1F4AC;</div>
                        <h3 class="empty-state__title">No Signal Groups Yet</h3>
                        <p class="empty-state__description">
                            No Signal groups are available in your verified communities or schools yet.
                            ${userIsAdmin ? 'Create the first one!' : ''}
                        </p>
                        ${userIsAdmin ? `
                            <a href="/admin/groups" class="btn btn--primary" data-link>Create a Group</a>
                        ` : ''}
                    </div>
                ` : `
                    <div id="groups-list">
                        ${renderGroupsList(groups)}
                    </div>
                `}
            </div>
        </div>
    `;

    // Bind search
    const searchInput = document.getElementById('group-search');
    if (searchInput) {
        searchInput.addEventListener('input', (event) => {
            filterGroups(event.target.value, groups);
        });
    }

    // Bind copy buttons
    bindCopyButtons();
}

/**
 * Render groups list HTML grouped by scope
 * @param {Object[]} groups - Groups to render
 * @returns {string} HTML string
 */
function renderGroupsList(groups) {
    // Group by scope key
    const groupsByScope = new Map();
    groups.forEach(group => {
        const scopeKey = getScopeKey(group);
        if (!groupsByScope.has(scopeKey)) {
            groupsByScope.set(scopeKey, []);
        }
        groupsByScope.get(scopeKey).push(group);
    });

    // Sort scope keys: regions first, then schools, then districts; alphabetical within each type
    const sortedKeys = [...groupsByScope.keys()].sort((a, b) => {
        const orderDiff = getScopeOrder(a) - getScopeOrder(b);
        if (orderDiff !== 0) return orderDiff;
        const nameA = getScopeName(groupsByScope.get(a)[0]).toLowerCase();
        const nameB = getScopeName(groupsByScope.get(b)[0]).toLowerCase();
        return nameA.localeCompare(nameB);
    });

    let html = '';

    for (const scopeKey of sortedKeys) {
        const scopeGroups = groupsByScope.get(scopeKey);
        const representativeGroup = scopeGroups[0];
        const scopeName = getScopeName(representativeGroup);
        const scopeLink = getScopeLink(representativeGroup);

        html += `
            <div class="dashboard-section" data-scope-key="${scopeKey}">
                <h2 class="dashboard-section__title">
                    <a href="${scopeLink}" data-link style="color: inherit; text-decoration: none;">
                        ${escapeHtml(scopeName)}
                    </a>
                </h2>
                <div class="dashboard-grid">
                    ${scopeGroups.map(group => renderGroupCard(group)).join('')}
                </div>
            </div>
        `;
    }

    return html;
}

/**
 * Render a single group card
 * @param {Object} group - Group data
 * @returns {string} HTML string
 */
function renderGroupCard(group) {
    return `
        <div class="card group-card" data-group-id="${group.id}" data-group-name="${escapeHtml(group.name).toLowerCase()}">
            <div class="card__body">
                <div class="group-card__header">
                    <div class="group-card__name">${escapeHtml(group.name)}${group.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
                </div>
                ${group.description ? `
                    <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                        ${escapeHtml(group.description)}
                    </p>
                ` : ''}
                <div class="group-invite">
                    <span class="group-invite__link">${escapeHtml(group.invite_link || 'Invite link not available')}</span>
                    ${group.invite_link ? `
                        <button class="btn btn--sm btn--secondary copy-link-btn" data-link="${escapeHtml(group.invite_link)}">
                            Copy
                        </button>
                    ` : ''}
                </div>
                ${group.invite_link ? `
                    <a href="${escapeHtml(group.invite_link)}" target="_blank" rel="noopener noreferrer" class="btn btn--primary btn--block mt-4">
                        Join on Signal
                    </a>
                ` : ''}
            </div>
        </div>
    `;
}

/**
 * Filter groups by search query
 * @param {string} query - Search query
 * @param {Object[]} groups - All groups
 */
function filterGroups(query, groups) {
    const normalizedQuery = query.toLowerCase().trim();
    const container = document.getElementById('groups-list');

    if (!normalizedQuery) {
        container.innerHTML = renderGroupsList(groups);
        bindCopyButtons();
        return;
    }

    const filtered = groups.filter(group => {
        const name = group.name?.toLowerCase() || '';
        const description = group.description?.toLowerCase() || '';
        const regionName = group.region_name?.toLowerCase() || '';
        const schoolName = group.school_name?.toLowerCase() || '';
        const districtName = group.district_name?.toLowerCase() || '';

        return name.includes(normalizedQuery) ||
               description.includes(normalizedQuery) ||
               regionName.includes(normalizedQuery) ||
               schoolName.includes(normalizedQuery) ||
               districtName.includes(normalizedQuery);
    });

    if (filtered.length === 0) {
        container.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F50D;</div>
                <h3 class="empty-state__title">No Results</h3>
                <p class="empty-state__description">
                    No groups match your search. Try different keywords.
                </p>
            </div>
        `;
    } else {
        container.innerHTML = renderGroupsList(filtered);
        bindCopyButtons();
    }
}

/**
 * Bind click handlers to copy buttons
 */
function bindCopyButtons() {
    const copyButtons = document.querySelectorAll('.copy-link-btn');
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
