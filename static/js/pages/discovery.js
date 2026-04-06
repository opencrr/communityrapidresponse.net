/**
 * Discovery page — consolidated browse groups + topic board
 *
 * Section 1: Disclaimer (always visible)
 * Section 2: Browse Groups (all authenticated users)
 * Section 3: Topic Board (admin of at least one group only)
 */

import { browseGroups, browseTopicBoard, listMyGroups } from '../api/groups.js';
import { proposeConnection } from '../api/connections.js';
import { isVouchVerified } from '../utils/store.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';

let allBrowseGroups = [];
let adminGroups = [];

/**
 * Render the discovery page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header" style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: var(--space-3);">
                    <div>
                        <h1 class="page__title">Discover</h1>
                    </div>
                    <a href="/groups/create" class="btn btn--primary" data-link id="create-group-btn" style="display:none">Create Group</a>
                </div>

                <div class="disclaimer" id="disclaimer"></div>

                <section class="discovery-section" id="browse-section">
                    <h2 class="discovery-section__title">Browse Groups</h2>
                    <div class="discovery-section__filters">
                        <select id="region-filter" class="form-input">
                            <option value="">All discoverable groups</option>
                        </select>
                        <input type="search" id="name-search" class="form-input" placeholder="Search by name..." />
                    </div>
                    <div class="loading" id="browse-loading"><div class="spinner"></div></div>
                    <div id="browse-groups-list" class="group-browse__list" style="display:none"></div>
                    <div class="empty-state" id="browse-empty" style="display:none">
                        <div class="empty-state__icon">&#x1F465;</div>
                        <h3 class="empty-state__title">No Groups Found</h3>
                        <p class="empty-state__description">No groups match your search. Try different keywords.</p>
                    </div>
                </section>

                <section class="discovery-section" id="topic-board-section" style="display:none">
                    <hr />
                    <h2 class="discovery-section__title">Topic Board (Admin Only)</h2>
                    <p class="discovery-section__subtitle">Browse by topic to discover groups in other regions.</p>
                    <div class="discovery-section__filters">
                        <select id="topic-group-select" class="form-input">
                            <option value="">Select your group...</option>
                        </select>
                        <input type="text" id="topic-tag-search" class="form-input" placeholder="Search by topic (e.g., mutual-aid)" />
                        <button class="btn btn--primary" id="topic-search-btn" disabled>Search</button>
                    </div>
                    <div class="loading" id="topic-loading" style="display:none"><div class="spinner"></div></div>
                    <div id="topic-results" class="discovery__results"></div>
                    <div class="empty-state" id="topic-empty" style="display:none">
                        <div class="empty-state__icon">&#x1F50D;</div>
                        <h3 class="empty-state__title">No Groups Found</h3>
                        <p class="empty-state__description">No groups found for this topic. Try a different tag.</p>
                    </div>
                </section>
            </div>
        </div>
    `;

    // Show create button for vouch-verified users
    if (isVouchVerified()) {
        const createBtn = document.getElementById('create-group-btn');
        if (createBtn) createBtn.style.display = '';
    }

    // Load browse groups and admin groups in parallel
    await Promise.all([
        loadBrowseGroups(),
        loadAdminGroups(),
    ]);

    bindBrowseEvents();
    bindTopicBoardEvents();
}

// ---------------------------------------------------------------------------
// Section 2: Browse Groups
// ---------------------------------------------------------------------------

/**
 * Load groups from the browse API and render them
 */
async function loadBrowseGroups() {
    const loadingEl = document.getElementById('browse-loading');
    const listEl = document.getElementById('browse-groups-list');
    const emptyEl = document.getElementById('browse-empty');
    const disclaimerEl = document.getElementById('disclaimer');

    try {
        const response = await browseGroups();
        const groups = response.groups || [];
        allBrowseGroups = groups;

        // Render disclaimer from API or fall back to hardcoded text
        const disclaimerText = response.disclaimer;
        if (disclaimerText && Array.isArray(disclaimerText)) {
            disclaimerEl.innerHTML = disclaimerText.map(text =>
                `<p class="disclaimer__text">${escapeHtml(text)}</p>`
            ).join('');
        } else {
            disclaimerEl.innerHTML = `
                <p class="disclaimer__text">This platform verifies member residency. It does not verify, endorse, or guarantee the quality, safety, or leadership of any group.</p>
                <p class="disclaimer__text">Group names, descriptions, and claims are provided by the group's organizers, not by this platform.</p>
            `;
        }

        // Populate region filter from the groups' region data
        populateRegionFilter(groups);

        loadingEl.style.display = 'none';

        if (groups.length === 0) {
            emptyEl.style.display = '';
            return;
        }

        listEl.style.display = '';
        renderGroupCards(groups, listEl);
    } catch (error) {
        console.error('Failed to browse groups:', error);
        loadingEl.style.display = 'none';
        emptyEl.querySelector('.empty-state__title').textContent = 'Error Loading Groups';
        emptyEl.querySelector('.empty-state__description').textContent =
            'Failed to load groups. Please try again later.';
        emptyEl.style.display = '';
    }
}

/**
 * Populate the region filter dropdown from the loaded groups' region data
 * @param {Object[]} groups - Array of group objects
 */
function populateRegionFilter(groups) {
    const regionSelect = document.getElementById('region-filter');
    if (!regionSelect) return;

    // Collect unique regions from the groups
    const regionMap = new Map();
    for (const group of groups) {
        if (group.region_id && group.region_name) {
            regionMap.set(group.region_id, group.region_name);
        }
    }

    if (regionMap.size === 0) return;

    const options = Array.from(regionMap.entries())
        .sort((a, b) => a[1].localeCompare(b[1]))
        .map(([id, name]) => `<option value="${escapeHtml(id)}">${escapeHtml(name)}</option>`)
        .join('');

    regionSelect.innerHTML = `<option value="">All groups</option>${options}`;
}

/**
 * Render group cards into the list container
 * @param {Object[]} groups - Array of group objects
 * @param {HTMLElement} listEl - Container to render into
 */
function renderGroupCards(groups, listEl) {
    listEl.innerHTML = groups.map(group => {
        const description = group.description || '';
        const truncatedDescription = description.length > 200
            ? description.substring(0, 200) + '...'
            : description;
        const tags = group.topic_tags || [];
        const memberCount = group.member_count || 0;
        const createdAt = group.created_at ? formatDate(group.created_at) : '';

        return `
            <a href="/groups/${group.id}" class="group-card" data-link data-group-id="${group.id}">
                <div class="group-card__name">${escapeHtml(group.name)}</div>
                ${truncatedDescription ? `
                    <div class="group-card__description">${escapeHtml(truncatedDescription)}</div>
                ` : ''}
                ${tags.length > 0 ? `
                    <div class="group-card__tags">
                        ${tags.map(tag => `<span class="tag-badge">${escapeHtml(tag)}</span>`).join('')}
                    </div>
                ` : ''}
                <div class="group-card__meta">
                    <span>${memberCount} member${memberCount !== 1 ? 's' : ''}</span>
                    ${createdAt ? `<span>Created ${createdAt}</span>` : ''}
                    ${group.visibility === 'unlisted' ? '<span>Unlisted</span>' : ''}
                </div>
            </a>
        `;
    }).join('');
}

/**
 * Bind browse section event handlers (region filter + name search)
 */
function bindBrowseEvents() {
    const regionFilter = document.getElementById('region-filter');
    const nameSearch = document.getElementById('name-search');

    if (regionFilter) {
        regionFilter.addEventListener('change', () => filterBrowseGroups());
    }
    if (nameSearch) {
        nameSearch.addEventListener('input', () => filterBrowseGroups());
    }
}

/**
 * Filter displayed browse groups by region and name
 */
function filterBrowseGroups() {
    const regionFilter = document.getElementById('region-filter');
    const nameSearch = document.getElementById('name-search');
    const listEl = document.getElementById('browse-groups-list');
    const emptyEl = document.getElementById('browse-empty');

    const selectedRegion = regionFilter?.value || '';
    const nameQuery = (nameSearch?.value || '').toLowerCase().trim();

    let filtered = allBrowseGroups;

    if (selectedRegion) {
        filtered = filtered.filter(group => group.region_id === selectedRegion);
    }

    if (nameQuery) {
        filtered = filtered.filter(group => {
            const name = (group.name || '').toLowerCase();
            const description = (group.description || '').toLowerCase();
            const tags = (group.topic_tags || []).map(t => t.toLowerCase());

            return name.includes(nameQuery) ||
                description.includes(nameQuery) ||
                tags.some(tag => tag.includes(nameQuery));
        });
    }

    if (filtered.length === 0) {
        listEl.style.display = 'none';
        emptyEl.querySelector('.empty-state__title').textContent = 'No Groups Found';
        emptyEl.querySelector('.empty-state__description').textContent =
            'No groups match your filters. Try different keywords or region.';
        emptyEl.style.display = '';
    } else {
        emptyEl.style.display = 'none';
        listEl.style.display = '';
        renderGroupCards(filtered, listEl);
    }
}

// ---------------------------------------------------------------------------
// Section 3: Topic Board (admin only)
// ---------------------------------------------------------------------------

/**
 * Load the user's groups where they are admin, to populate the topic board selector.
 * If the user has no admin groups, the topic board section stays hidden.
 */
async function loadAdminGroups() {
    try {
        const response = await listMyGroups();
        const groups = response.groups || [];
        adminGroups = groups.filter(group => group.is_admin);

        if (adminGroups.length === 0) return;

        // Show topic board section
        const sectionEl = document.getElementById('topic-board-section');
        if (sectionEl) sectionEl.style.display = '';

        const selectEl = document.getElementById('topic-group-select');
        if (!selectEl) return;

        selectEl.innerHTML = '<option value="">Select your group...</option>' +
            adminGroups.map(group =>
                `<option value="${escapeHtml(group.id)}">${escapeHtml(group.name)}</option>`
            ).join('');
    } catch (error) {
        console.error('Failed to load admin groups for topic board:', error);
        // Silently fail — topic board section stays hidden
    }
}

/**
 * Bind topic board event handlers
 */
function bindTopicBoardEvents() {
    const searchBtn = document.getElementById('topic-search-btn');
    const groupSelect = document.getElementById('topic-group-select');
    const tagSearch = document.getElementById('topic-tag-search');

    if (groupSelect) {
        groupSelect.addEventListener('change', () => {
            if (searchBtn) searchBtn.disabled = !groupSelect.value;
        });
    }

    if (searchBtn) {
        searchBtn.addEventListener('click', performTopicSearch);
    }

    if (tagSearch) {
        tagSearch.addEventListener('keydown', (event) => {
            if (event.key === 'Enter' && groupSelect?.value) {
                performTopicSearch();
            }
        });
    }
}

/**
 * Execute the topic board search
 */
async function performTopicSearch() {
    const groupSelect = document.getElementById('topic-group-select');
    const tagSearch = document.getElementById('topic-tag-search');
    const resultsEl = document.getElementById('topic-results');
    const emptyEl = document.getElementById('topic-empty');
    const loadingEl = document.getElementById('topic-loading');

    const selectedGroupId = groupSelect?.value;
    const tagQuery = tagSearch?.value?.trim();

    if (!selectedGroupId) {
        toast.error('Please select one of your groups first');
        return;
    }

    loadingEl.style.display = '';
    resultsEl.innerHTML = '';
    emptyEl.style.display = 'none';

    try {
        const params = { group_id: selectedGroupId, limit: 20 };
        if (tagQuery) {
            params.tag = tagQuery;
        }

        const response = await browseTopicBoard(params);
        const postings = response.postings || [];

        loadingEl.style.display = 'none';

        if (postings.length === 0) {
            emptyEl.style.display = '';
            return;
        }

        renderPostingCards(postings, resultsEl, selectedGroupId);
    } catch (error) {
        console.error('Topic board search failed:', error);
        loadingEl.style.display = 'none';
        toast.error(error.data?.message || 'Search failed. You may need to be a group admin.');
    }
}

/**
 * Render posting cards into the results container
 * @param {Object[]} postings - Array of topic board postings
 * @param {HTMLElement} resultsEl - Container element
 * @param {string} fromGroupId - The group requesting on behalf of
 */
function renderPostingCards(postings, resultsEl, fromGroupId) {
    resultsEl.innerHTML = postings.map(posting => {
        const tags = posting.tags || [];
        const regionLabel = posting.region_label || '';
        const description = posting.description || '';

        return `
            <div class="posting-card">
                ${regionLabel ? `<div class="posting-card__region">${escapeHtml(regionLabel)}</div>` : ''}
                ${tags.length > 0 ? `
                    <div class="group-card__tags">
                        ${tags.map(tag => `<span class="tag-badge">${escapeHtml(tag)}</span>`).join('')}
                    </div>
                ` : ''}
                ${description ? `<div class="posting-card__description">${escapeHtml(description)}</div>` : ''}
                <div class="posting-card__actions">
                    <button class="btn btn--primary btn--sm request-connection-btn"
                        data-posting-group-id="${escapeHtml(posting.group_id)}"
                        data-from-group-id="${escapeHtml(fromGroupId)}">
                        Request Connection
                    </button>
                </div>
            </div>
        `;
    }).join('');

    // Bind request connection buttons
    resultsEl.querySelectorAll('.request-connection-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
            const targetGroupId = btn.dataset.postingGroupId;
            const initiatorGroupId = btn.dataset.fromGroupId;

            const confirmed = await modal.confirm({
                title: 'Request Connection?',
                message: 'This will send a connection proposal to the other group\'s admins. They will see your group name and region.',
            });
            if (!confirmed) return;

            btn.disabled = true;
            btn.textContent = 'Sending...';

            try {
                await proposeConnection({
                    initiator_group_id: initiatorGroupId,
                    target_group_id: targetGroupId,
                });
                toast.success('Connection proposal sent');
                btn.textContent = 'Sent';
            } catch (error) {
                toast.error(error.data?.message || 'Failed to send connection proposal');
                btn.disabled = false;
                btn.textContent = 'Request Connection';
            }
        });
    });
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

/**
 * Format an ISO date string for display
 * @param {string} dateStr - ISO date string
 * @returns {string} Formatted date
 */
function formatDate(dateStr) {
    try {
        const date = new Date(dateStr);
        return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
    } catch {
        return '';
    }
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
    allBrowseGroups = [];
    adminGroups = [];
}

export default { render, cleanup };
