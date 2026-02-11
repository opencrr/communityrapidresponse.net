/**
 * School detail page
 * Shows school info, members, vouch UI, and signal groups.
 * Layout mirrors regionDetail.js.
 */

import { getSchool, joinSchool, leaveSchool, vouchForSchoolMember, getPendingSchoolVouches, getSchoolMembers, getSchoolSignalGroups, createSchoolSignalGroup, getDistrict, districtToFeature } from '../api/schools.js?v=c49aed9';
import { createSchoolReport } from '../api/reports.js?v=c49aed9';
import { initMap, addRegionsLayer, fitToBounds, destroyMap, showPopup } from '../components/map.js?v=c49aed9';
import { isAuthenticated, getUser } from '../utils/store.js?v=c49aed9';
import { navigate } from '../app.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';
import modal from '../components/modal.js?v=c49aed9';

let schoolMapInstance = null;

/**
 * Render the school detail page
 * @param {HTMLElement} container - Container element
 * @param {Object} params - Route params with id
 */
export async function render(container, params) {
    const schoolId = params.id;

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="loading">
                    <div class="spinner spinner--lg"></div>
                </div>
            </div>
        </div>
    `;

    try {
        const school = await getSchool(schoolId);
        renderSchoolDetail(container, school);
    } catch (error) {
        console.error('Failed to load school:', error);
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x26A0;</div>
                    <h3 class="empty-state__title">School Not Found</h3>
                    <p class="empty-state__description">
                        The school you're looking for doesn't exist or could not be loaded.
                    </p>
                    <a href="/schools" class="btn btn--primary" data-link>Browse Schools</a>
                </div>
            </div>
        `;
    }
}

/**
 * Render school detail content
 */
function renderSchoolDetail(container, school) {
    const authenticated = isAuthenticated();
    const user = getUser();
    const isMember = school.is_member || false;
    const isVerified = school.is_verified || false;
    const isSchoolAdmin = school.is_admin || false;

    let locationParts = [];
    if (school.city) locationParts.push(escapeHtml(school.city));
    if (school.state) locationParts.push(escapeHtml(school.state));
    const locationLine = locationParts.join(', ');

    container.innerHTML = `
        <div class="page">
            <div class="page__container">
                <div class="region-detail">
                    <!-- Breadcrumb -->
                    <nav class="region-detail__breadcrumb">
                        <a href="/schools" data-link>All Schools</a>
                        ${school.district_id && school.district_name ? `
                            &rsaquo; <a href="/school-districts/${school.district_id}" data-link>${escapeHtml(school.district_name)}</a>
                        ` : ''}
                        &rsaquo; ${escapeHtml(school.name)}
                    </nav>

                    <!-- Header -->
                    <div class="region-detail__header">
                        <div style="display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap;">
                            <h1 class="page__title">${escapeHtml(school.name)}</h1>
                            ${isMember ? '<span class="badge badge--success">Member</span>' : ''}
                            ${isVerified ? '<span class="badge badge--primary">Verified</span>' : ''}
                            ${isSchoolAdmin ? '<span class="badge badge--info">Admin</span>' : ''}
                        </div>
                        ${locationLine ? `
                            <span class="region-item__type" style="margin-top: var(--space-2); display: inline-block;">
                                ${locationLine}
                            </span>
                        ` : ''}
                    </div>

                    <!-- Stats -->
                    <div class="region-detail__stats">
                        <div class="region-stat">
                            <div class="region-stat__value">${school.member_count || 0}</div>
                            <div class="region-stat__label">Members</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${school.verified_count || 0}</div>
                            <div class="region-stat__label">Verified</div>
                        </div>
                        <div class="region-stat">
                            <div class="region-stat__value">${school.admin_count || 0}</div>
                            <div class="region-stat__label">Admins</div>
                        </div>
                    </div>

                    ${school.bootstrap_mode ? `
                        <div class="privacy-notice" style="margin-top: var(--space-4); background: #dbeafe;">
                            <div class="privacy-notice__icon">&#x1F680;</div>
                            <div class="privacy-notice__text">
                                <strong>Bootstrap Mode Active</strong>
                                This school has fewer than 3 verified admins.
                                Any member can vouch during bootstrap. 3 vouches required for verification.
                                The first 3 verified users will become admins.
                                (${school.admin_count || 0}/3 admins)
                            </div>
                        </div>
                    ` : ''}

                    ${renderJoinSection(school, authenticated, isMember, isVerified)}

                    <!-- Map -->
                    ${school.latitude && school.longitude ? `
                        <div class="card mt-6">
                            <div class="card__body" style="padding: 0; height: 400px;">
                                <div class="map-container" id="school-map"></div>
                            </div>
                        </div>
                    ` : ''}

                    <!-- Members -->
                    ${authenticated && isMember ? `
                        <div id="members-section" class="mt-6"></div>
                    ` : ''}

                    <!-- Vouch Section -->
                    ${authenticated && isMember && (isVerified || school.bootstrap_mode) ? `
                        <div id="vouch-section" class="mt-6"></div>
                    ` : ''}

                    <!-- Signal Groups -->
                    ${isVerified ? `
                        <div id="signal-groups-section" class="mt-6"></div>
                    ` : ''}

                    <!-- Admin Actions -->
                    ${isSchoolAdmin ? `
                        <div id="admin-section" class="mt-6"></div>
                    ` : ''}

                    <!-- Leave -->
                    ${authenticated && isMember ? `
                        <div class="mt-6" style="padding-top: var(--space-4); border-top: 1px solid var(--color-gray-200);">
                            <button class="btn btn--ghost btn--sm" id="leave-btn" style="color: var(--color-red-600);">Leave School</button>
                        </div>
                    ` : ''}
                </div>
            </div>
        </div>
    `;

    bindEvents(school);

    // Initialize map if school has coordinates
    if (school.latitude && school.longitude) {
        initSchoolMap(school);
    }

    // Load dynamic sections
    if (authenticated && isMember) {
        loadMembers(school.id);
        if (isVerified || school.bootstrap_mode) {
            loadVouchSection(school);
        }
    }
    if (isVerified) {
        loadSignalGroups(school);
    }
    if (isSchoolAdmin) {
        loadAdminSection(school);
    }
}

/**
 * Render join / membership status section
 */
function renderJoinSection(school, authenticated, isMember, isVerified) {
    if (!authenticated) {
        return '';
    }

    if (isMember && !isVerified) {
        return `
            <div class="card" style="margin-top: var(--space-4); border-left: 4px solid var(--color-warning);">
                <div class="card__body text-center">
                    <p style="color: var(--color-warning); font-weight: 500;">
                        &#x23F3; Awaiting Verification &mdash; ask existing verified members to vouch for you.
                        ${school.bootstrap_mode ? '3 vouches required in bootstrap mode.' : '2 vouches required.'}
                    </p>
                </div>
            </div>
        `;
    }

    if (!isMember) {
        return `
            <div class="card" style="margin-top: var(--space-4);">
                <div class="card__body text-center">
                    <p style="color: var(--color-gray-600); margin-bottom: var(--space-3);">
                        Join to connect with your school community. You'll need vouches from existing members to become verified.
                    </p>
                    <button class="btn btn--primary" id="join-btn">Join School</button>
                </div>
            </div>
        `;
    }

    return '';
}

/**
 * Initialize school location map with optional district boundary
 */
async function initSchoolMap(school) {
    const mapContainer = document.getElementById('school-map');
    if (!mapContainer) return;

    destroyMap();

    const schoolCenter = [school.longitude, school.latitude];

    schoolMapInstance = initMap({
        container: mapContainer,
        center: schoolCenter,
        zoom: 13,
        interactive: true,
        onLoad: async (map) => {
            // Add school marker
            new mapboxgl.Marker({ color: '#ef4444' })
                .setLngLat(schoolCenter)
                .setPopup(new mapboxgl.Popup().setHTML(`<strong>${escapeHtml(school.name)}</strong>`))
                .addTo(map);

            // Load district boundary if school has a district
            if (school.district_id) {
                try {
                    const districtData = await getDistrict(school.district_id);
                    if (districtData && districtData.geometry) {
                        const feature = districtToFeature(districtData);
                        const geojson = {
                            type: 'FeatureCollection',
                            features: [feature],
                        };
                        addRegionsLayer(map, geojson);
                    }
                } catch (districtError) {
                    console.warn('Could not load district boundary:', districtError);
                }
            }
        },
    });
}

/**
 * Bind event handlers
 */
function bindEvents(school) {
    const joinBtn = document.getElementById('join-btn');
    if (joinBtn) {
        joinBtn.addEventListener('click', async () => {
            joinBtn.disabled = true;
            joinBtn.textContent = 'Joining...';
            try {
                await joinSchool(school.id);
                toast.success('You have joined this school!');
                const updatedSchool = await getSchool(school.id);
                const container = document.getElementById('main');
                if (container) renderSchoolDetail(container, updatedSchool);
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to join school');
                joinBtn.disabled = false;
                joinBtn.textContent = 'Join School';
            }
        });
    }

    const leaveBtn = document.getElementById('leave-btn');
    if (leaveBtn) {
        leaveBtn.addEventListener('click', async () => {
            if (!confirm('Are you sure you want to leave this school?')) return;
            try {
                await leaveSchool(school.id);
                toast.success('You have left the school');
                const updatedSchool = await getSchool(school.id);
                const container = document.getElementById('main');
                if (container) renderSchoolDetail(container, updatedSchool);
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to leave school');
            }
        });
    }
}

/**
 * Load and display members
 */
async function loadMembers(schoolId) {
    const section = document.getElementById('members-section');
    if (!section) return;

    const currentUser = getUser();
    const showReportButtons = isAuthenticated() && currentUser;

    try {
        const response = await getSchoolMembers(schoolId);
        const members = response.members || [];

        if (members.length === 0) {
            section.innerHTML = '';
            return;
        }

        section.innerHTML = `
            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                Members (${members.length})
            </h2>
            <div class="card">
                <div class="card__body">
                    <table style="width: 100%; border-collapse: collapse; text-align: left;">
                        <thead>
                            <tr style="border-bottom: 2px solid var(--color-gray-200);">
                                <th style="padding: var(--space-2) var(--space-3);">Username</th>
                                <th style="padding: var(--space-2) var(--space-3);">Status</th>
                                <th style="padding: var(--space-2) var(--space-3);">Role</th>
                                <th style="padding: var(--space-2) var(--space-3);">Vouches</th>
                                ${showReportButtons ? '<th style="padding: var(--space-2) var(--space-3);">Actions</th>' : ''}
                            </tr>
                        </thead>
                        <tbody>
                            ${members.map(member => `
                                <tr style="border-bottom: 1px solid var(--color-gray-100);">
                                    <td style="padding: var(--space-2) var(--space-3);">${escapeHtml(member.username)}</td>
                                    <td style="padding: var(--space-2) var(--space-3);">
                                        <span class="badge badge--${member.verification_status === 'verified' ? 'success' : 'default'}">
                                            ${member.verification_status}
                                        </span>
                                    </td>
                                    <td style="padding: var(--space-2) var(--space-3);">${member.is_admin ? '<span class="badge badge--info">Admin</span>' : 'Member'}</td>
                                    <td style="padding: var(--space-2) var(--space-3);">${member.vouch_count || 0}</td>
                                    ${showReportButtons ? `
                                        <td style="padding: var(--space-2) var(--space-3);">
                                            ${member.user_id !== currentUser?.id ? `
                                                <button class="btn btn--sm btn--outline report-member-btn"
                                                    data-user-id="${member.user_id}"
                                                    data-username="${escapeHtml(member.username)}"
                                                    data-school-id="${schoolId}">
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
        `;

        // Bind report buttons
        if (showReportButtons) {
            section.querySelectorAll('.report-member-btn').forEach(btn => {
                btn.addEventListener('click', () => {
                    showReportModal(btn.dataset.schoolId, btn.dataset.userId, btn.dataset.username);
                });
            });
        }
    } catch (error) {
        console.error('Failed to load members:', error);
        if (error.status === 403) {
            section.innerHTML = `
                <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">Members</h2>
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <p class="empty-state__description">
                        You must be a verified member of this school to view the member list.
                    </p>
                </div>
            `;
        }
    }
}

/**
 * Show the report user modal
 */
function showReportModal(schoolId, userId, username) {
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
                        await createSchoolReport(schoolId, userId, reason, details);
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
 * Load vouch section
 */
async function loadVouchSection(school) {
    const section = document.getElementById('vouch-section');
    if (!section) return;

    try {
        const response = await getPendingSchoolVouches(school.id);
        const currentUser = getUser();
        const pendingUsers = (response.requests || []).filter(u => u.user_id !== currentUser?.id);

        section.innerHTML = `
            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                Vouch for Members
            </h2>
            <div class="card">
                <div class="card__body">
                    ${pendingUsers.length === 0 ? `
                        <p style="color: var(--color-gray-500);">No members are currently awaiting vouches.</p>
                    ` : `
                        <p style="margin-bottom: var(--space-4);">These members are awaiting vouches:</p>
                        ${pendingUsers.map(pendingUser => `
                            <div style="display: flex; justify-content: space-between; align-items: center; padding: var(--space-3) 0; border-bottom: 1px solid var(--color-gray-100);">
                                <div>
                                    <strong>${escapeHtml(pendingUser.username)}</strong>
                                    <span style="color: var(--color-gray-500); margin-left: var(--space-2);">${pendingUser.vouch_count || 0} vouches</span>
                                </div>
                                <button class="btn btn--primary btn--sm vouch-user-btn" data-user-id="${pendingUser.user_id}" data-username="${escapeHtml(pendingUser.username)}">
                                    Vouch
                                </button>
                            </div>
                        `).join('')}
                    `}
                    <div style="margin-top: var(--space-4);">
                        <h4>Vouch by username or email</h4>
                        <div style="display: flex; gap: var(--space-2); margin-top: var(--space-2);">
                            <input type="text" class="form-input" placeholder="Username or email" id="vouch-input" style="flex: 1;">
                            <button class="btn btn--primary" id="vouch-btn">Vouch</button>
                        </div>
                    </div>
                </div>
            </div>
        `;

        // Bind vouch buttons
        const vouchUserBtns = section.querySelectorAll('.vouch-user-btn');
        vouchUserBtns.forEach(btn => {
            btn.addEventListener('click', async () => {
                const userId = btn.dataset.userId;
                btn.disabled = true;
                btn.textContent = 'Vouching...';
                try {
                    const result = await vouchForSchoolMember(school.id, { user_identifier: userId });
                    toast.success(`Vouched successfully! ${result.vouches_needed > 0 ? `${result.vouches_needed} more vouch(es) needed.` : 'User is now verified!'}`);
                    loadVouchSection(school);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to vouch');
                    btn.disabled = false;
                    btn.textContent = 'Vouch';
                }
            });
        });

        const vouchBtn = document.getElementById('vouch-btn');
        const vouchInput = document.getElementById('vouch-input');
        if (vouchBtn && vouchInput) {
            vouchBtn.addEventListener('click', async () => {
                const identifier = vouchInput.value.trim();
                if (!identifier) {
                    toast.error('Please enter a username or email');
                    return;
                }
                vouchBtn.disabled = true;
                vouchBtn.textContent = 'Vouching...';
                try {
                    const result = await vouchForSchoolMember(school.id, { user_identifier: identifier });
                    toast.success(`Vouched successfully! ${result.vouches_needed > 0 ? `${result.vouches_needed} more vouch(es) needed.` : 'User is now verified!'}`);
                    vouchInput.value = '';
                    loadVouchSection(school);
                } catch (error) {
                    toast.error(error.data?.message || error.message || 'Failed to vouch');
                }
                vouchBtn.disabled = false;
                vouchBtn.textContent = 'Vouch';
            });
        }
    } catch (error) {
        console.error('Failed to load vouch section:', error);
    }
}

/**
 * Load signal groups using dashboard-grid / group-card layout
 */
async function loadSignalGroups(school) {
    const section = document.getElementById('signal-groups-section');
    if (!section) return;

    try {
        const response = await getSchoolSignalGroups(school.id);
        const groups = response.groups || [];

        section.innerHTML = `
            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                Signal Groups
            </h2>
            ${groups.length > 0 ? `
                <div class="dashboard-grid">
                    ${groups.map(group => `
                        <div class="card group-card">
                            <div class="card__body">
                                <div class="group-card__name">${escapeHtml(group.name)}${group.has_pending_deletion ? '<span class="badge badge--danger" style="margin-left: var(--space-2);">Delete Proposed</span>' : ''}</div>
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
            ` : `
                <div class="card">
                    <div class="card__body">
                        <p class="text-center" style="color: var(--color-gray-600);">
                            No Signal groups have been created for this school yet.
                        </p>
                    </div>
                </div>
            `}
        `;

        // Bind copy buttons
        const copyButtons = section.querySelectorAll('.copy-link-btn');
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
        console.error('Failed to load signal groups:', error);
    }
}

/**
 * Load admin section
 */
async function loadAdminSection(school) {
    const section = document.getElementById('admin-section');
    if (!section) return;

    section.innerHTML = `
        <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
            Admin
        </h2>
        <div class="card">
            <div class="card__body">
                <h3>Create Signal Group</h3>
                <div style="margin-top: var(--space-3);">
                    <div class="form-group">
                        <label class="form-label">Group Name</label>
                        <input type="text" class="form-input" id="new-group-name" placeholder="e.g. School Announcements">
                    </div>
                    <div class="form-group">
                        <label class="form-label">Signal Invite Link</label>
                        <input type="url" class="form-input" id="new-group-link" placeholder="https://signal.group/...">
                    </div>
                    <div class="form-group">
                        <label class="form-label">Description (optional)</label>
                        <textarea class="form-input" id="new-group-desc" rows="2" placeholder="What is this group for?"></textarea>
                    </div>
                    <button class="btn btn--primary" id="create-group-btn">Create Signal Group</button>
                </div>
            </div>
        </div>
    `;

    const createBtn = document.getElementById('create-group-btn');
    if (createBtn) {
        createBtn.addEventListener('click', async () => {
            const name = document.getElementById('new-group-name')?.value?.trim();
            const inviteLink = document.getElementById('new-group-link')?.value?.trim();
            const description = document.getElementById('new-group-desc')?.value?.trim();

            if (!name || !inviteLink) {
                toast.error('Group name and invite link are required');
                return;
            }

            createBtn.disabled = true;
            createBtn.textContent = 'Creating...';
            try {
                await createSchoolSignalGroup(school.id, {
                    name,
                    invite_link: inviteLink,
                    description: description || undefined,
                });
                toast.success('Signal group created!');
                loadSignalGroups(school);
                loadAdminSection(school);
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to create signal group');
            }
            createBtn.disabled = false;
            createBtn.textContent = 'Create Signal Group';
        });
    }
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
    if (schoolMapInstance) {
        destroyMap();
        schoolMapInstance = null;
    }
}

export default { render, cleanup };
