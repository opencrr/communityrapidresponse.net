/**
 * Discovery page — topic board browsing
 * Allows group admins to discover other groups across regions via topic tags.
 * Intentionally omits group names, member counts, and specific locations.
 */

import { browseTopicBoard, listMyGroups } from '../api/groups.js';
import { proposeConnection } from '../api/connections.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';

let adminGroups = [];

/**
 * Render the discovery page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="page__header">
                    <h1 class="page__title">Discover Groups</h1>
                    <p class="page__subtitle">Find groups working on similar issues across regions. Only group admins can browse.</p>
                </div>
                <div class="discovery__controls">
                    <select id="group-select" class="form-input">
                        <option value="">Select your group...</option>
                    </select>
                    <input type="text" id="tag-search" class="form-input" placeholder="Search by topic (e.g., mutual-aid)" />
                    <button class="btn btn--primary" id="search-btn" disabled>Search</button>
                </div>
                <div class="loading" id="loading" style="display:none"><div class="spinner"></div></div>
                <div id="results" class="discovery__results"></div>
                <div class="empty-state" id="empty-state" style="display:none">
                    <div class="empty-state__icon">&#x1F50D;</div>
                    <h3 class="empty-state__title">No Groups Found</h3>
                    <p class="empty-state__description">No groups found for this topic. Try a different tag.</p>
                </div>
            </div>
        </div>
    `;

    await loadAdminGroups();
    bindEvents();
}

/**
 * Load the user's groups where they are admin, to populate the group selector
 */
async function loadAdminGroups() {
    try {
        const response = await listMyGroups();
        const groups = response.groups || [];
        adminGroups = groups.filter(group => group.is_admin);

        const selectEl = document.getElementById('group-select');
        if (!selectEl) return;

        if (adminGroups.length === 0) {
            selectEl.innerHTML = '<option value="">You are not an admin of any groups</option>';
            return;
        }

        selectEl.innerHTML = '<option value="">Select your group...</option>' +
            adminGroups.map(group =>
                `<option value="${escapeHtml(group.id)}">${escapeHtml(group.name)}</option>`
            ).join('');
    } catch (error) {
        console.error('Failed to load groups:', error);
        toast.error('Failed to load your groups');
    }
}

/**
 * Bind event handlers
 */
function bindEvents() {
    const searchBtn = document.getElementById('search-btn');
    const groupSelect = document.getElementById('group-select');
    const tagSearch = document.getElementById('tag-search');

    if (groupSelect) {
        groupSelect.addEventListener('change', () => {
            if (searchBtn) searchBtn.disabled = !groupSelect.value;
        });
    }

    if (searchBtn) {
        searchBtn.addEventListener('click', performSearch);
    }

    if (tagSearch) {
        tagSearch.addEventListener('keydown', (event) => {
            if (event.key === 'Enter' && groupSelect?.value) {
                performSearch();
            }
        });
    }
}

/**
 * Execute the topic board search
 */
async function performSearch() {
    const groupSelect = document.getElementById('group-select');
    const tagSearch = document.getElementById('tag-search');
    const resultsEl = document.getElementById('results');
    const emptyEl = document.getElementById('empty-state');
    const loadingEl = document.getElementById('loading');

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
    adminGroups = [];
}

export default { render, cleanup };
