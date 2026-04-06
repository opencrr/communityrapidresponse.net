/**
 * Group browse/discover page
 * Shows discoverable groups with search filtering, topic tags, and member counts.
 */

import { browseGroups } from '../api/groups.js';
import { isVouchVerified } from '../utils/store.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';

let allGroups = [];

/**
 * Render the group browse page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header" style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: var(--space-3);">
                    <div>
                        <h1 class="page__title">Browse Groups</h1>
                        <p class="page__subtitle">Discover groups in your verified communities.</p>
                    </div>
                    <a href="/groups/create" class="btn btn--primary" data-link id="create-group-btn" style="display:none">Create Group</a>
                </div>
                <div class="disclaimer" id="disclaimer"></div>
                <div class="group-browse__filters">
                    <input type="search" id="search-input" class="form-input" placeholder="Filter by name or tag..." />
                </div>
                <div class="loading" id="loading"><div class="spinner"></div></div>
                <div id="groups-list" class="group-browse__list" style="display:none"></div>
                <div class="empty-state" id="empty-state" style="display:none">
                    <div class="empty-state__icon">&#x1F465;</div>
                    <h3 class="empty-state__title">No Groups Found</h3>
                    <p class="empty-state__description">
                        No groups found in your area. Try a different search or create one.
                    </p>
                </div>
            </div>
        </div>
    `;

    // Show create button for vouch-verified users
    if (isVouchVerified()) {
        const createBtn = document.getElementById('create-group-btn');
        if (createBtn) createBtn.style.display = '';
    }

    await loadGroups();
    bindEvents();
}

/**
 * Load groups from the API and render them
 */
async function loadGroups() {
    const loadingEl = document.getElementById('loading');
    const listEl = document.getElementById('groups-list');
    const emptyEl = document.getElementById('empty-state');
    const disclaimerEl = document.getElementById('disclaimer');

    try {
        const response = await browseGroups();
        const groups = response.groups || [];
        allGroups = groups;

        // Render disclaimer
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
 * Bind search filter events
 */
function bindEvents() {
    const searchInput = document.getElementById('search-input');
    if (searchInput) {
        searchInput.addEventListener('input', () => {
            filterGroups(searchInput.value);
        });
    }
}

/**
 * Filter displayed groups by search query
 * @param {string} query - Search text
 */
function filterGroups(query) {
    const normalizedQuery = query.toLowerCase().trim();
    const listEl = document.getElementById('groups-list');
    const emptyEl = document.getElementById('empty-state');

    if (!normalizedQuery) {
        listEl.style.display = '';
        emptyEl.style.display = allGroups.length === 0 ? '' : 'none';
        renderGroupCards(allGroups, listEl);
        return;
    }

    const filtered = allGroups.filter(group => {
        const name = (group.name || '').toLowerCase();
        const description = (group.description || '').toLowerCase();
        const tags = (group.topic_tags || []).map(t => t.toLowerCase());

        return name.includes(normalizedQuery) ||
            description.includes(normalizedQuery) ||
            tags.some(tag => tag.includes(normalizedQuery));
    });

    if (filtered.length === 0) {
        listEl.style.display = 'none';
        emptyEl.querySelector('.empty-state__title').textContent = 'No Results';
        emptyEl.querySelector('.empty-state__description').textContent =
            'No groups match your search. Try different keywords.';
        emptyEl.style.display = '';
    } else {
        emptyEl.style.display = 'none';
        listEl.style.display = '';
        renderGroupCards(filtered, listEl);
    }
}

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
    allGroups = [];
}

export default { render, cleanup };
