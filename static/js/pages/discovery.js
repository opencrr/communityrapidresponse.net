/**
 * Discovery page — browse groups
 *
 * Section 1: Disclaimer (always visible)
 * Section 2: Browse Groups (all authenticated users)
 *
 * Topic board has moved to the Connections page.
 */

import { browseGroups } from '../api/groups.js';
import { isVouchVerified } from '../utils/store.js';
import toast from '../components/toast.js';

let allBrowseGroups = [];

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

                <!-- Topic board moved to Connections page -->
            </div>
        </div>
    `;

    // Show create button for vouch-verified users
    if (isVouchVerified()) {
        const createBtn = document.getElementById('create-group-btn');
        if (createBtn) createBtn.style.display = '';
    }

    await loadBrowseGroups();
    bindBrowseEvents();
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

// Topic board moved to Connections page (/connections)

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
}

export default { render, cleanup };
