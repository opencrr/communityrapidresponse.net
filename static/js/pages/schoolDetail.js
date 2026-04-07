/**
 * School detail page
 * Shows school info and links to the associated group (if any).
 * Membership, vouching, signal groups, and members are all on the group page.
 */

import { getSchool, joinSchool, getDistrict, districtToFeature } from '../api/schools.js';
import { initMap, addRegionsLayer, destroyMap } from '../components/map.js';
import { isAuthenticated } from '../utils/store.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';

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
    const hasGroup = !!school.group_id;

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
                        <h1 class="page__title">${escapeHtml(school.name)}</h1>
                        ${locationLine ? `
                            <span class="region-item__type" style="margin-top: var(--space-2); display: inline-block;">
                                ${locationLine}
                            </span>
                        ` : ''}
                    </div>

                    <!-- Group Link or Join -->
                    ${authenticated ? renderGroupSection(school, hasGroup) : `
                        <div class="card" style="margin-top: var(--space-4);">
                            <div class="card__body text-center">
                                <p style="color: var(--color-gray-600);">
                                    Log in to join this school's community group.
                                </p>
                            </div>
                        </div>
                    `}

                    <!-- Map -->
                    ${school.latitude && school.longitude ? `
                        <div class="card mt-6">
                            <div class="card__body" style="padding: 0; height: 400px;">
                                <div class="map-container" id="school-map"></div>
                            </div>
                        </div>
                    ` : ''}
                </div>
            </div>
        </div>
    `;

    bindEvents(school);

    if (school.latitude && school.longitude) {
        initSchoolMap(school);
    }
}

/**
 * Render the group section (link to existing group or join button)
 */
function renderGroupSection(school, hasGroup) {
    if (hasGroup) {
        return `
            <div class="card" style="margin-top: var(--space-4);">
                <div class="card__body text-center">
                    <p style="color: var(--color-gray-600); margin-bottom: var(--space-3);">
                        This school has a community group. View it for members, signal groups, and vouching.
                    </p>
                    <a href="/groups/${school.group_id}" class="btn btn--primary" data-link>View Group</a>
                </div>
            </div>
        `;
    }

    return `
        <div class="card" style="margin-top: var(--space-4);">
            <div class="card__body text-center">
                <p style="color: var(--color-gray-600); margin-bottom: var(--space-3);">
                    No community group exists for this school yet. Join to create one and start building your school community.
                </p>
                <button class="btn btn--primary" id="join-btn">Join School</button>
            </div>
        </div>
    `;
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
            new mapboxgl.Marker({ color: '#ef4444' })
                .setLngLat(schoolCenter)
                .setPopup(new mapboxgl.Popup().setHTML(`<strong>${escapeHtml(school.name)}</strong>`))
                .addTo(map);

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
                const result = await joinSchool(school.id);
                toast.success('School group created! Redirecting...');
                // Navigate to the new group page
                if (result.group_id) {
                    navigate(`/groups/${result.group_id}`);
                } else {
                    // Reload school detail to show the group link
                    const updatedSchool = await getSchool(school.id);
                    const container = document.getElementById('main');
                    if (container) renderSchoolDetail(container, updatedSchool);
                }
            } catch (error) {
                toast.error(error.data?.message || error.message || 'Failed to join school');
                joinBtn.disabled = false;
                joinBtn.textContent = 'Join School';
            }
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
