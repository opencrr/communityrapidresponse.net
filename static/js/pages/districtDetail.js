/**
 * School district detail page
 * Shows district info, schools list, and signal groups.
 */

import { getDistrict, getDistrictMembers, getDistrictSignalGroups, createDistrictSignalGroup, districtToFeature } from '../api/schools.js?v=c49aed9';
import { createDistrictReport } from '../api/reports.js?v=c49aed9';
import { initMap, addRegionsLayer, fitToBounds, destroyMap } from '../components/map.js?v=c49aed9';
import { isAuthenticated, getUser } from '../utils/store.js?v=c49aed9';
import { navigate } from '../app.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';
import modal from '../components/modal.js?v=c49aed9';

let districtId = null;
let districtData = null;
let mapInstance = null;

/**
 * Render the district detail page
 * @param {HTMLElement} container - Container element
 * @param {Object} params - Route params
 */
export async function render(container, params) {
    districtId = params.id;

    container.innerHTML = `
        <div class="page">
            <div class="loading">
                <div class="spinner"></div>
            </div>
        </div>
    `;

    try {
        districtData = await getDistrict(districtId);
        renderDistrictPage(container);
    } catch (error) {
        console.error('Failed to load district:', error);
        container.innerHTML = `
            <div class="page">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">District Not Found</h3>
                    <p class="empty-state__description">
                        This district could not be found or may have been removed.
                    </p>
                    <a href="/schools" class="btn btn--primary" data-link>Browse Schools</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render the full district page
 */
async function renderDistrictPage(container) {
    const district = districtData;
    const schools = district.schools || [];

    let html = `
        <div class="page">
            <div class="page__container">
            <div class="page__header">
                <a href="/schools" class="btn btn--ghost btn--sm" data-link style="margin-bottom: var(--space-3);">
                    &larr; Back to Schools
                </a>
                <h1 class="page__title">${escapeHtml(district.name)}</h1>
                <p class="page__description">
                    ${escapeHtml(district.state)}
                    ${district.district_type ? ` &middot; ${escapeHtml(formatDistrictType(district.district_type))}` : ''}
                </p>
            </div>

            ${district.geometry ? `
            <section style="margin-bottom: var(--space-8);">
                <div id="district-map" style="height: 400px; border-radius: var(--radius-md); overflow: hidden;"></div>
            </section>
            ` : ''}

            <section style="margin-bottom: var(--space-8);">
                <h2 style="margin-bottom: var(--space-4);">Schools in this District</h2>
    `;

    if (schools.length === 0) {
        html += `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F3EB;</div>
                <h3 class="empty-state__title">No Schools Found</h3>
                <p class="empty-state__description">
                    No schools have been added to this district yet.
                </p>
            </div>
        `;
    } else {
        html += `
            <p style="color: var(--color-gray-500); margin-bottom: var(--space-3);">
                ${schools.length} school${schools.length !== 1 ? 's' : ''}
            </p>
            <div class="card-list">
        `;

        for (const school of schools) {
            const bootstrapBadge = school.bootstrap_mode
                ? '<span class="badge badge--warning" style="margin-left: var(--space-2);">Bootstrap</span>'
                : '';

            html += `
                <a href="/schools/${school.id}" class="card card--clickable" data-link>
                    <div class="card__body">
                        <div style="display: flex; justify-content: space-between; align-items: start;">
                            <div>
                                <h3 class="card__title">${escapeHtml(school.name)}${bootstrapBadge}</h3>
                                <p class="card__meta">
                                    ${escapeHtml(school.city || '')}${school.city ? ', ' : ''}${escapeHtml(school.state)}
                                </p>
                            </div>
                            <div style="text-align: right; font-size: var(--font-size-sm); color: var(--color-gray-500);">
                                <div>${school.member_count || 0} member${(school.member_count || 0) !== 1 ? 's' : ''}</div>
                                <div>${school.verified_count || 0} verified</div>
                            </div>
                        </div>
                    </div>
                </a>
            `;
        }

        html += '</div>';
    }

    html += '</section>';

    // Members section (populated async for authenticated users)
    html += '<div id="members-section"></div>';

    // Signal groups section
    html += `
        <section style="margin-bottom: var(--space-8);">
            <h2 style="margin-bottom: var(--space-4);">District Signal Groups</h2>
            <div id="district-signal-groups">
                <div class="loading"><div class="spinner"></div></div>
            </div>
        </section>
    `;

    html += '</div></div>';
    container.innerHTML = html;

    // Initialize map if geometry is available
    if (district.geometry) {
        initDistrictMap(district);
    }

    // Load members and signal groups for authenticated users
    if (isAuthenticated()) {
        loadMembers(districtId);
    }
    await loadSignalGroups();
}

/**
 * Initialize the district boundary map
 */
function initDistrictMap(district) {
    const mapContainer = document.getElementById('district-map');
    if (!mapContainer) return;

    destroyMap();

    const feature = districtToFeature(district);
    const geojson = {
        type: 'FeatureCollection',
        features: [feature],
    };

    mapInstance = initMap({
        container: mapContainer,
        interactive: true,
        onLoad: (map) => {
            addRegionsLayer(map, geojson);
            fitToBounds(map, geojson, { maxZoom: 12 });
        },
    });
}

/**
 * Load and render district members
 */
async function loadMembers(districtId) {
    const section = document.getElementById('members-section');
    if (!section) return;

    const currentUser = getUser();
    const showReportButtons = isAuthenticated() && currentUser;

    try {
        const response = await getDistrictMembers(districtId);
        const members = response.members || [];

        if (members.length === 0) {
            section.innerHTML = '';
            return;
        }

        section.innerHTML = `
            <section style="margin-bottom: var(--space-8);">
                <h2 style="margin-bottom: var(--space-4);">Members (${members.length})</h2>
                <div class="card">
                    <div class="card__body">
                        <table style="width: 100%; border-collapse: collapse; text-align: left;">
                            <thead>
                                <tr style="border-bottom: 2px solid var(--color-gray-200);">
                                    <th style="padding: var(--space-2) var(--space-3);">Username</th>
                                    <th style="padding: var(--space-2) var(--space-3);">School</th>
                                    <th style="padding: var(--space-2) var(--space-3);">Status</th>
                                    <th style="padding: var(--space-2) var(--space-3);">Role</th>
                                    ${showReportButtons ? '<th style="padding: var(--space-2) var(--space-3);">Actions</th>' : ''}
                                </tr>
                            </thead>
                            <tbody>
                                ${members.map(member => `
                                    <tr style="border-bottom: 1px solid var(--color-gray-100);">
                                        <td style="padding: var(--space-2) var(--space-3);">${escapeHtml(member.username)}</td>
                                        <td style="padding: var(--space-2) var(--space-3);">${escapeHtml(member.school_name)}</td>
                                        <td style="padding: var(--space-2) var(--space-3);">
                                            ${member.verification_status === 'verified'
                                                ? '<span class="badge badge--success">Verified</span>'
                                                : '<span class="badge badge--warning">Pending</span>'}
                                        </td>
                                        <td style="padding: var(--space-2) var(--space-3);">
                                            ${member.is_admin ? '<span class="badge badge--info">Admin</span>' : 'Member'}
                                        </td>
                                        ${showReportButtons ? `
                                            <td style="padding: var(--space-2) var(--space-3);">
                                                ${member.user_id !== currentUser?.id ? `
                                                    <button class="btn btn--sm btn--outline report-member-btn"
                                                        data-user-id="${member.user_id}"
                                                        data-username="${escapeHtml(member.username)}">
                                                        Report
                                                    </button>
                                                ` : ''}
                                            </td>
                                        ` : ''}
                                    </tr>
                                `).join('')}
                            </tbody>
                        </table>
                    </div>
                </div>
            </section>
        `;

        // Bind report buttons
        if (showReportButtons) {
            section.querySelectorAll('.report-member-btn').forEach(btn => {
                btn.addEventListener('click', () => {
                    showReportModal(districtId, btn.dataset.userId, btn.dataset.username);
                });
            });
        }
    } catch (error) {
        console.error('Failed to load district members:', error);
        if (error.status === 403) {
            section.innerHTML = `
                <section style="margin-bottom: var(--space-8);">
                    <h2 style="margin-bottom: var(--space-4);">Members</h2>
                    <div class="empty-state">
                        <div class="empty-state__icon">&#x1F512;</div>
                        <p class="empty-state__description">
                            You must be a verified member of a school in this district to view members.
                        </p>
                    </div>
                </section>
            `;
        }
    }
}

/**
 * Show report modal for a district member
 */
function showReportModal(districtId, userId, username) {
    const reasons = [
        { value: 'harassment', label: 'Harassment' },
        { value: 'spam', label: 'Spam' },
        { value: 'impersonation', label: 'Impersonation' },
        { value: 'fraudulent_verification', label: 'Fraudulent Verification' },
        { value: 'other', label: 'Other' },
    ];

    modal.showModal({
        title: `Report ${escapeHtml(username)}`,
        content: `
            <div class="form">
                <div class="form__group">
                    <label for="report-reason" class="form__label">Reason</label>
                    <select id="report-reason" class="form__input">
                        ${reasons.map(r => `<option value="${r.value}">${r.label}</option>`).join('')}
                    </select>
                </div>
                <div class="form__group">
                    <label for="report-details" class="form__label">Details (optional)</label>
                    <textarea id="report-details" class="form__input form__textarea" rows="3" placeholder="Provide additional details about this report..."></textarea>
                </div>
            </div>
        `,
        actions: [
            { label: 'Cancel', type: 'secondary' },
            {
                label: 'Submit Report',
                type: 'danger',
                closeOnClick: false,
                onClick: async () => {
                    const reason = document.getElementById('report-reason')?.value;
                    const details = document.getElementById('report-details')?.value?.trim() || null;

                    if (!reason) {
                        toast.error('Please select a reason.');
                        return;
                    }

                    try {
                        await createDistrictReport(districtId, userId, reason, details);
                        modal.closeModal();
                        toast.success('Report submitted successfully.');
                    } catch (error) {
                        let errorMessage = 'Failed to submit report.';
                        if (error.data?.message) {
                            errorMessage = error.data.message;
                        } else if (error.message) {
                            errorMessage = error.message;
                        }
                        toast.error(errorMessage);
                    }
                },
            },
        ],
    });
}

/**
 * Load and render signal groups
 */
async function loadSignalGroups() {
    const groupsContainer = document.getElementById('district-signal-groups');
    if (!groupsContainer) return;

    if (!isAuthenticated()) {
        groupsContainer.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F4E1;</div>
                <p class="empty-state__description">
                    <a href="/login" data-link>Log in</a> and become a verified member of a school in this district to see signal groups.
                </p>
            </div>
        `;
        return;
    }

    try {
        const response = await getDistrictSignalGroups(districtId);
        const groups = response.groups || [];

        if (groups.length === 0) {
            groupsContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F4E1;</div>
                    <h3 class="empty-state__title">No Signal Groups Yet</h3>
                    <p class="empty-state__description">
                        No district-level signal groups have been created yet.
                    </p>
                </div>
            `;
        } else {
            let groupsHtml = '<div class="card-list">';
            for (const group of groups) {
                groupsHtml += `
                    <div class="card">
                        <div class="card__body">
                            <h3 class="card__title">${escapeHtml(group.name)}${group.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</h3>
                            ${group.description ? `<p class="card__meta">${escapeHtml(group.description)}</p>` : ''}
                            ${group.invite_link ? `
                                <a href="${escapeHtml(group.invite_link)}" target="_blank" rel="noopener noreferrer" class="btn btn--primary btn--sm" style="margin-top: var(--space-2);">
                                    Join Signal Group
                                </a>
                            ` : ''}
                        </div>
                    </div>
                `;
            }
            groupsHtml += '</div>';
            groupsContainer.innerHTML = groupsHtml;
        }

        // Add create form for admins (if the API returned can_create flag or if user is admin of a school in district)
        if (response.can_create) {
            const formHtml = `
                <div style="margin-top: var(--space-6); padding: var(--space-4); background: var(--color-gray-50); border-radius: var(--radius-md);">
                    <h3 style="margin-bottom: var(--space-3);">Create District Signal Group</h3>
                    <form id="create-district-group-form">
                        <div class="form-group">
                            <label class="form-label" for="district-group-name">Group Name</label>
                            <input type="text" class="form-input" id="district-group-name" required maxlength="255" placeholder="e.g., District Parents Group">
                        </div>
                        <div class="form-group">
                            <label class="form-label" for="district-group-link">Signal Invite Link</label>
                            <input type="url" class="form-input" id="district-group-link" required placeholder="https://signal.group/...">
                        </div>
                        <div class="form-group">
                            <label class="form-label" for="district-group-description">Description (optional)</label>
                            <textarea class="form-input" id="district-group-description" rows="2" placeholder="What is this group for?"></textarea>
                        </div>
                        <button type="submit" class="btn btn--primary">Create Group</button>
                    </form>
                </div>
            `;
            groupsContainer.insertAdjacentHTML('beforeend', formHtml);

            const form = document.getElementById('create-district-group-form');
            if (form) {
                form.addEventListener('submit', handleCreateDistrictGroup);
            }
        }
    } catch (error) {
        console.error('Failed to load district signal groups:', error);
        if (error.status === 403) {
            groupsContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <p class="empty-state__description">
                        You must be a verified member of a school in this district to view signal groups.
                    </p>
                </div>
            `;
        } else {
            groupsContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <p class="empty-state__description">
                        Failed to load signal groups. Please try again.
                    </p>
                </div>
            `;
        }
    }
}

/**
 * Handle create district signal group form submission
 */
async function handleCreateDistrictGroup(event) {
    event.preventDefault();

    const name = document.getElementById('district-group-name')?.value?.trim();
    const inviteLink = document.getElementById('district-group-link')?.value?.trim();
    const description = document.getElementById('district-group-description')?.value?.trim();

    if (!name || !inviteLink) {
        toast.error('Group name and invite link are required.');
        return;
    }

    const submitBtn = event.target.querySelector('button[type="submit"]');
    if (submitBtn) {
        submitBtn.disabled = true;
        submitBtn.textContent = 'Creating...';
    }

    try {
        const data = { name, invite_link: inviteLink };
        if (description) data.description = description;

        await createDistrictSignalGroup(districtId, data);
        toast.success('District signal group created!');
        await loadSignalGroups();
    } catch (error) {
        console.error('Failed to create district signal group:', error);
        toast.error(error.message || 'Failed to create signal group.');
        if (submitBtn) {
            submitBtn.disabled = false;
            submitBtn.textContent = 'Create Group';
        }
    }
}

/**
 * Format district type for display
 */
function formatDistrictType(type) {
    const types = {
        'unified': 'Unified School District',
        'elementary': 'Elementary School District',
        'secondary': 'Secondary School District',
    };
    return types[type] || type;
}

/**
 * Escape HTML to prevent XSS
 */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function cleanup() {
    if (mapInstance) {
        destroyMap();
        mapInstance = null;
    }
    districtId = null;
    districtData = null;
}

export default { render, cleanup };
