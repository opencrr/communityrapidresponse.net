/**
 * School search/browse page
 * Sidebar + map layout mirroring regions.js.
 * Shows user's schools/districts on load, with search below.
 */

import { searchSchools, getMySchools } from '../api/schools.js';
import { initMap, destroyMap, showPopup } from '../components/map.js';
import { isAuthenticated } from '../utils/store.js';
import { navigate } from '../app.js';

const US_STATES = [
    'AL','AK','AZ','AR','CA','CO','CT','DE','FL','GA','HI','ID','IL','IN','IA',
    'KS','KY','LA','ME','MD','MA','MI','MN','MS','MO','MT','NE','NV','NH','NJ',
    'NM','NY','NC','ND','OH','OK','OR','PA','RI','SC','SD','TN','TX','UT','VT',
    'VA','WA','WV','WI','WY','DC'
];

let mapInstance = null;
let markers = [];
let currentPage = 1;
let currentQuery = '';
let currentState = '';

/**
 * Render the schools page
 * @param {HTMLElement} container - Container element
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page map-layout">
            <div class="map-layout__sidebar">
                <div id="school-list">
                    <div style="padding: var(--space-4); border-bottom: 1px solid var(--color-gray-200);">
                        <input
                            type="search"
                            class="form-input"
                            placeholder="Search schools..."
                            id="school-search"
                            style="margin-bottom: var(--space-2);"
                        >
                        <select class="form-input" id="state-filter" style="width: 100%;">
                            <option value="">All States</option>
                            ${US_STATES.map(state => `<option value="${state}">${state}</option>`).join('')}
                        </select>
                    </div>
                    <div id="my-schools-section"></div>
                    <div id="school-results"></div>
                </div>
            </div>
            <div class="map-layout__main">
                <div class="map-container" id="schools-map"></div>
            </div>
        </div>
    `;

    // Initialize map then load user's schools
    initSchoolsMap();
    bindEvents();

    if (isAuthenticated()) {
        await loadMySchools();
    } else {
        showSearchPrompt();
    }
}

/**
 * Show the default search prompt
 */
function showSearchPrompt() {
    const resultsContainer = document.getElementById('school-results');
    if (!resultsContainer) return;
    resultsContainer.innerHTML = `
        <div class="empty-state" style="padding: var(--space-6) var(--space-4);">
            <div class="empty-state__icon">&#x1F3EB;</div>
            <h3 class="empty-state__title">Search for a School</h3>
            <p class="empty-state__description">
                Enter a school name or select a state to browse schools.
            </p>
        </div>
    `;
}

/**
 * Load the authenticated user's schools and districts
 */
async function loadMySchools() {
    const mySection = document.getElementById('my-schools-section');
    if (!mySection) return;

    try {
        const response = await getMySchools();
        const schools = response.schools || [];

        if (schools.length === 0) {
            mySection.innerHTML = '';
            showSearchPrompt();
            return;
        }

        // Extract unique districts from user's schools
        const districtMap = {};
        schools.forEach(school => {
            if (school.district_name && school.district_id) {
                districtMap[school.district_id] = school.district_name;
            }
        });
        const districts = Object.entries(districtMap);

        let html = '';

        // Districts section
        if (districts.length > 0) {
            html += `
                <div class="region-list__section">
                    <h3 style="padding: var(--space-3) var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-transform: uppercase; letter-spacing: 0.05em;">
                        My Districts (${districts.length})
                    </h3>
                    ${districts.map(([id, name]) => `
                        <a href="/school-districts/${id}" class="region-item" data-link>
                            <div class="region-item__name">${escapeHtml(name)}</div>
                        </a>
                    `).join('')}
                </div>
            `;
        }

        // Schools section
        html += `
            <div class="region-list__section">
                <h3 style="padding: var(--space-3) var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-transform: uppercase; letter-spacing: 0.05em;">
                    My Schools (${schools.length})
                </h3>
                ${schools.map(school => `
                    <a href="/schools/${school.id}" class="region-item" data-link>
                        <div class="region-item__name">${escapeHtml(school.name)}</div>
                        <div class="region-item__meta">
                            ${escapeHtml(school.city || '')}${school.city ? ', ' : ''}${escapeHtml(school.state)}
                            ${school.district_name ? ` &middot; ${escapeHtml(school.district_name)}` : ''}
                        </div>
                    </a>
                `).join('')}
            </div>
        `;

        mySection.innerHTML = html;

        // Plot user's schools on map
        plotSchoolsOnMap(schools);

    } catch (error) {
        console.error('Failed to load my schools:', error);
        mySection.innerHTML = '';
        showSearchPrompt();
    }
}

/**
 * Initialize the map
 */
function initSchoolsMap() {
    const mapContainer = document.getElementById('schools-map');
    if (!mapContainer) return;

    destroyMap();
    mapInstance = initMap({
        container: mapContainer,
        center: [-98.5795, 39.8283],
        zoom: 4,
        interactive: true,
        onLoad: () => {},
    });
}

/**
 * Clear all markers from the map
 */
function clearMarkers() {
    markers.forEach(m => m.remove());
    markers = [];
}

/**
 * Add school markers to the map and fit bounds
 */
function plotSchoolsOnMap(schools) {
    clearMarkers();
    if (!mapInstance) return;

    const bounds = new mapboxgl.LngLatBounds();
    let hasPoints = false;

    for (const school of schools) {
        if (school.latitude && school.longitude) {
            hasPoints = true;
            const marker = new mapboxgl.Marker({ color: '#6366f1', scale: 0.7 })
                .setLngLat([school.longitude, school.latitude])
                .addTo(mapInstance);

            marker.getElement().addEventListener('click', () => {
                const popupHtml = `
                    <div class="map-popup">
                        <h4 class="map-popup__title">${escapeHtml(school.name)}</h4>
                        <p class="map-popup__meta">${escapeHtml(school.city || '')}${school.city ? ', ' : ''}${escapeHtml(school.state)}</p>
                        <a href="/schools/${school.id}" class="btn btn--primary btn--sm btn--block mt-2" data-link>
                            View Details
                        </a>
                    </div>
                `;
                showPopup(mapInstance, [school.longitude, school.latitude], popupHtml);
            });

            markers.push(marker);
            bounds.extend([school.longitude, school.latitude]);
        }
    }

    if (hasPoints) {
        mapInstance.fitBounds(bounds, { padding: 50, maxZoom: 14 });
    }
}

/**
 * Bind search events
 */
function bindEvents() {
    const searchInput = document.getElementById('school-search');
    const stateFilter = document.getElementById('state-filter');

    if (searchInput) {
        searchInput.addEventListener('keypress', (event) => {
            if (event.key === 'Enter') {
                performSearch();
            }
        });
    }

    if (stateFilter) {
        stateFilter.addEventListener('change', () => performSearch());
    }
}

/**
 * Perform school search
 */
async function performSearch() {
    const searchInput = document.getElementById('school-search');
    const stateFilter = document.getElementById('state-filter');
    const resultsContainer = document.getElementById('school-results');

    currentQuery = searchInput?.value?.trim() || '';
    currentState = stateFilter?.value || '';
    currentPage = 1;

    if (!currentQuery && !currentState) {
        resultsContainer.innerHTML = '';
        return;
    }

    resultsContainer.innerHTML = `
        <div class="loading" style="padding: var(--space-6);">
            <div class="spinner"></div>
        </div>
    `;

    try {
        const params = { page: currentPage, limit: 20 };
        if (currentQuery) params.query = currentQuery;
        if (currentState) params.state = currentState;

        const response = await searchSchools(params);
        renderResults(response, resultsContainer);
    } catch (error) {
        console.error('School search failed:', error);
        resultsContainer.innerHTML = `
            <div class="empty-state" style="padding: var(--space-6) var(--space-4);">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Search Failed</h3>
                <p class="empty-state__description">
                    An error occurred while searching. Please try again.
                </p>
            </div>
        `;
    }
}

/**
 * Render search results in sidebar
 */
function renderResults(response, container) {
    const schools = response.schools || [];
    const total = response.total || 0;

    if (schools.length === 0) {
        container.innerHTML = `
            <div class="empty-state" style="padding: var(--space-6) var(--space-4);">
                <div class="empty-state__icon">&#x1F50D;</div>
                <h3 class="empty-state__title">No Schools Found</h3>
                <p class="empty-state__description">
                    No schools matched your search. Try different search terms.
                </p>
            </div>
        `;
        clearMarkers();
        return;
    }

    // Group schools by state
    const grouped = {};
    schools.forEach(school => {
        const state = school.state || 'Unknown';
        if (!grouped[state]) grouped[state] = [];
        grouped[state].push(school);
    });

    let html = `
        <div class="region-list__section">
            <h3 style="padding: var(--space-3) var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-transform: uppercase; letter-spacing: 0.05em;">
                Search Results (${total})
            </h3>
        </div>
    `;

    for (const [state, stateSchools] of Object.entries(grouped)) {
        html += `
            <div class="region-list__section">
                <h3 style="padding: var(--space-3) var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-transform: uppercase; letter-spacing: 0.05em;">
                    ${escapeHtml(state)} (${stateSchools.length})
                </h3>
                ${stateSchools.map(school => `
                    <a href="/schools/${school.id}" class="region-item" data-link data-school-id="${school.id}">
                        <div class="region-item__name">${escapeHtml(school.name)}</div>
                        <div class="region-item__meta">
                            ${escapeHtml(school.city || '')}${school.city && school.district_name ? ' &middot; ' : ''}${escapeHtml(school.district_name || '')}
                            ${school.member_count ? ` &middot; ${school.member_count} member${school.member_count !== 1 ? 's' : ''}` : ''}
                        </div>
                    </a>
                `).join('')}
            </div>
        `;
    }

    // Pagination
    if (response.has_more) {
        html += `
            <div style="padding: var(--space-4); text-align: center;">
                <button class="btn btn--ghost btn--sm" id="load-more-btn">Load More</button>
            </div>
        `;
    }

    container.innerHTML = html;

    // Plot on map
    plotSchoolsOnMap(schools);

    // Bind load more
    const loadMoreBtn = document.getElementById('load-more-btn');
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener('click', loadMore);
    }
}

/**
 * Load more results
 */
async function loadMore() {
    currentPage++;
    const loadMoreBtn = document.getElementById('load-more-btn');
    if (loadMoreBtn) {
        loadMoreBtn.textContent = 'Loading...';
        loadMoreBtn.disabled = true;
    }

    try {
        const params = { page: currentPage, limit: 20 };
        if (currentQuery) params.query = currentQuery;
        if (currentState) params.state = currentState;

        const response = await searchSchools(params);
        const schools = response.schools || [];
        const container = document.getElementById('school-results');

        // Remove load more button before appending
        if (loadMoreBtn) loadMoreBtn.parentElement.remove();

        // Append new school items
        for (const school of schools) {
            const link = document.createElement('a');
            link.href = `/schools/${school.id}`;
            link.className = 'region-item';
            link.setAttribute('data-link', '');
            link.setAttribute('data-school-id', school.id);
            link.innerHTML = `
                <div class="region-item__name">${escapeHtml(school.name)}</div>
                <div class="region-item__meta">
                    ${escapeHtml(school.city || '')}${school.city && school.district_name ? ' &middot; ' : ''}${escapeHtml(school.district_name || '')}
                    ${school.member_count ? ` &middot; ${school.member_count} member${school.member_count !== 1 ? 's' : ''}` : ''}
                </div>
            `;
            container.appendChild(link);
        }

        // Add new markers
        for (const school of schools) {
            if (school.latitude && school.longitude) {
                const marker = new mapboxgl.Marker({ color: '#6366f1', scale: 0.7 })
                    .setLngLat([school.longitude, school.latitude])
                    .addTo(mapInstance);

                marker.getElement().addEventListener('click', () => {
                    const popupHtml = `
                        <div class="map-popup">
                            <h4 class="map-popup__title">${escapeHtml(school.name)}</h4>
                            <p class="map-popup__meta">${escapeHtml(school.city || '')}${school.city ? ', ' : ''}${escapeHtml(school.state)}</p>
                            <a href="/schools/${school.id}" class="btn btn--primary btn--sm btn--block mt-2" data-link>
                                View Details
                            </a>
                        </div>
                    `;
                    showPopup(mapInstance, [school.longitude, school.latitude], popupHtml);
                });

                markers.push(marker);
            }
        }

        // Add load more if needed
        if (response.has_more) {
            const loadMoreDiv = document.createElement('div');
            loadMoreDiv.style.cssText = 'padding: var(--space-4); text-align: center;';
            loadMoreDiv.innerHTML = '<button class="btn btn--ghost btn--sm" id="load-more-btn">Load More</button>';
            container.appendChild(loadMoreDiv);
            document.getElementById('load-more-btn').addEventListener('click', loadMore);
        }
    } catch (error) {
        console.error('Failed to load more:', error);
        if (loadMoreBtn) {
            loadMoreBtn.textContent = 'Load More';
            loadMoreBtn.disabled = false;
        }
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
    clearMarkers();
    if (mapInstance) {
        destroyMap();
        mapInstance = null;
    }
}

export default { render, cleanup };
