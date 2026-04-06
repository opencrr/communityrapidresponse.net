/**
 * Group creation page
 * Form for creating a new group. Only accessible to vouch-verified users.
 */

import { createGroup } from '../api/groups.js';
import { getRegions } from '../api/regions.js';
import { isVouchVerified } from '../utils/store.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';

/**
 * Render the group creation page
 * @param {HTMLElement} container - Container element
 */
export async function render(container) {
    if (!isVouchVerified()) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Verification Required</h3>
                    <p class="empty-state__description">
                        You need to be vouch-verified to create groups.
                    </p>
                    <a href="/vouch" class="btn btn--primary" data-link>Get Vouched</a>
                </div>
            </div>
        `;
        return;
    }

    container.innerHTML = `
        <div class="page">
            <div class="page__container" style="max-width: 640px;">
                <nav class="region-detail__breadcrumb">
                    <a href="/groups/browse" data-link>Browse Groups</a>
                    &rsaquo; Create Group
                </nav>

                <h1 class="page__title" style="margin-top: var(--space-4);">Create a Group</h1>
                <p class="page__subtitle">Groups let you organize around shared interests within your verified communities.</p>

                <form id="create-group-form" class="mt-6">
                    <div class="form-group">
                        <label class="form-label" for="group-name">Group Name <span style="color: var(--color-error);">*</span></label>
                        <input type="text" class="form-input" id="group-name" required minlength="3" maxlength="100" placeholder="e.g. Block Watch, Community Garden Club">
                    </div>

                    <div class="form-group">
                        <label class="form-label" for="group-description">Description</label>
                        <textarea class="form-input" id="group-description" rows="4" maxlength="2000" placeholder="What is this group about?"></textarea>
                    </div>

                    <div class="form-group">
                        <label class="form-label">Visibility</label>
                        <div style="display: flex; gap: var(--space-4); margin-top: var(--space-2);">
                            <label style="display: flex; align-items: center; gap: var(--space-2); cursor: pointer;">
                                <input type="radio" name="visibility" value="listed" checked>
                                <span>Listed</span>
                            </label>
                            <label style="display: flex; align-items: center; gap: var(--space-2); cursor: pointer;">
                                <input type="radio" name="visibility" value="unlisted">
                                <span>Unlisted</span>
                            </label>
                        </div>
                        <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">
                            Listed groups appear in browse results. Unlisted groups can only be found via invite link.
                        </p>
                    </div>

                    <div class="form-group">
                        <label class="form-label" for="group-tags">Topic Tags</label>
                        <input type="text" class="form-input" id="group-tags" placeholder="e.g. safety, gardening, mutual-aid (comma-separated)">
                        <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-top: var(--space-1);">
                            Separate tags with commas. Helps others find your group.
                        </p>
                    </div>

                    <div class="form-group" id="regions-group">
                        <label class="form-label">Communities</label>
                        <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-bottom: var(--space-2);">
                            Select which of your verified communities this group is associated with.
                        </p>
                        <div id="region-checkboxes">
                            <div class="loading"><div class="spinner"></div></div>
                        </div>
                    </div>

                    <div class="form-actions" style="margin-top: var(--space-6); display: flex; gap: var(--space-3); justify-content: flex-end;">
                        <a href="/groups/browse" class="btn btn--secondary" data-link>Cancel</a>
                        <button type="submit" class="btn btn--primary" id="submit-btn">Create Group</button>
                    </div>
                </form>
            </div>
        </div>
    `;

    await loadRegionCheckboxes();
    bindFormSubmit();
}

/**
 * Load user's regions and render checkboxes
 */
async function loadRegionCheckboxes() {
    const checkboxContainer = document.getElementById('region-checkboxes');
    if (!checkboxContainer) return;

    try {
        const regions = await getRegions();

        if (regions.length === 0) {
            checkboxContainer.innerHTML = `
                <p style="color: var(--color-gray-500); font-size: var(--font-size-sm);">
                    You are not a member of any communities yet. <a href="/verify" data-link>Verify your address</a> first.
                </p>
            `;
            return;
        }

        checkboxContainer.innerHTML = regions.map(region => `
            <label style="display: flex; align-items: center; gap: var(--space-2); padding: var(--space-2) 0; cursor: pointer;">
                <input type="checkbox" name="region_ids" value="${region.id}">
                <span>${escapeHtml(region.name)}</span>
                <span style="font-size: var(--font-size-xs); color: var(--color-gray-400);">${escapeHtml(region.type || region.region_type || '')}</span>
            </label>
        `).join('');
    } catch (error) {
        console.error('Failed to load regions:', error);
        checkboxContainer.innerHTML = `
            <p style="color: var(--color-error); font-size: var(--font-size-sm);">Failed to load communities.</p>
        `;
    }
}

/**
 * Bind form submit handler
 */
function bindFormSubmit() {
    const form = document.getElementById('create-group-form');
    if (!form) return;

    form.addEventListener('submit', async (event) => {
        event.preventDefault();

        const submitBtn = document.getElementById('submit-btn');
        const nameInput = document.getElementById('group-name');
        const descriptionInput = document.getElementById('group-description');
        const tagsInput = document.getElementById('group-tags');
        const visibilityInput = form.querySelector('input[name="visibility"]:checked');
        const regionCheckboxes = form.querySelectorAll('input[name="region_ids"]:checked');

        const name = nameInput?.value?.trim();
        if (!name) {
            toast.error('Group name is required');
            nameInput?.focus();
            return;
        }

        const description = descriptionInput?.value?.trim() || undefined;
        const visibility = visibilityInput?.value || 'listed';
        const topicTags = tagsInput?.value
            ? tagsInput.value.split(',').map(t => t.trim()).filter(Boolean)
            : undefined;
        const regionIds = Array.from(regionCheckboxes).map(cb => cb.value);

        submitBtn.disabled = true;
        submitBtn.textContent = 'Creating...';

        try {
            const groupData = { name, visibility };
            if (description) groupData.description = description;
            if (topicTags && topicTags.length > 0) groupData.topic_tags = topicTags;
            if (regionIds.length > 0) groupData.region_ids = regionIds;

            const result = await createGroup(groupData);
            const groupId = result.id || result.group?.id;

            toast.success('Group created successfully!');

            if (groupId) {
                navigate(`/groups/${groupId}`);
            } else {
                navigate('/groups/browse');
            }
        } catch (error) {
            console.error('Failed to create group:', error);
            toast.error(error.data?.message || error.message || 'Failed to create group');
            submitBtn.disabled = false;
            submitBtn.textContent = 'Create Group';
        }
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

export function cleanup() {}

export default { render, cleanup };
