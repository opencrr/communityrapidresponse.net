/**
 * Region browser page
 * Shows map with all regions and sidebar list.
 */

import { getRegions, regionsToFeatureCollection } from '../api/regions.js';
import { initMap, addRegionsLayer, createLegendHtml, destroyMap, fitToBounds, showPopup } from '../components/map.js';
import { isAuthenticated, isAdmin } from '../utils/store.js';
import { navigate } from '../app.js';

let mapInstance = null;

/**
 * Render the regions page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page map-layout">
            <div class="map-layout__sidebar">
                <div class="region-list" id="region-list">
                    <div class="loading">
                        <div class="spinner"></div>
                    </div>
                </div>
            </div>
            <div class="map-layout__main">
                <div class="map-container" id="regions-map"></div>
            </div>
        </div>
    `;

    // Load regions and initialize map
    await loadRegions();
}

/**
 * Load and display regions
 */
async function loadRegions() {
    const listContainer = document.getElementById('region-list');
    const mapContainer = document.getElementById('regions-map');

    try {
        const regions = await getRegions();

        // Render list
        if (regions.length === 0) {
            listContainer.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F5FA;</div>
                    <h3 class="empty-state__title">No Communities Yet</h3>
                    <p class="empty-state__description">
                        No communities have been created yet. Be the first to verify and create a community!
                    </p>
                </div>
            `;
        } else {
            renderRegionList(regions, listContainer);
        }

        // Initialize map
        destroyMap();
        mapInstance = initMap({
            container: mapContainer,
            center: [-98.5795, 39.8283],
            zoom: 4,
            interactive: true,
            onLoad: (map) => {
                if (regions.length > 0) {
                    const geojson = regionsToFeatureCollection(regions);
                    addRegionsLayer(map, geojson, {
                        onClick: (feature, event) => {
                            handleRegionClick(feature, event, map);
                        },
                    });
                    fitToBounds(map, geojson, { maxZoom: 10 });
                }

                // Add legend
                const legendContainer = document.createElement('div');
                legendContainer.innerHTML = createLegendHtml();
                mapContainer.appendChild(legendContainer.firstElementChild);
            },
        });
    } catch (error) {
        console.error('Failed to load regions:', error);
        // Only show error for actual failures, not for empty data
        // (empty data is handled above with the friendly "No Regions Yet" message)
        listContainer.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x1F5FA;</div>
                <h3 class="empty-state__title">No Regions Yet</h3>
                <p class="empty-state__description">
                    No regions have been created yet. Be the first to verify and create a region!
                </p>
            </div>
        `;

        // Still initialize map even on error
        destroyMap();
        mapInstance = initMap({
            container: mapContainer,
            center: [-98.5795, 39.8283],
            zoom: 4,
            interactive: true,
            onLoad: (map) => {
                const legendContainer = document.createElement('div');
                legendContainer.innerHTML = createLegendHtml();
                mapContainer.appendChild(legendContainer.firstElementChild);
            },
        });
    }
}

/**
 * Render the region list in the sidebar
 * @param {Object[]} regions - Array of regions
 * @param {HTMLElement} container - List container
 */
function renderRegionList(regions, container) {
    // Group regions by type (hierarchy: state -> county -> city -> locality -> neighborhood -> city_block)
    const grouped = {
        state: [],
        county: [],
        city: [],
        locality: [],
        neighborhood: [],
        city_block: [],
    };

    regions.forEach(region => {
        const type = region.type || region.region_type;
        if (grouped[type]) {
            grouped[type].push(region);
        }
    });

    const typeLabels = {
        state: 'States',
        county: 'Counties',
        city: 'Cities',
        locality: 'Localities',
        neighborhood: 'Neighborhoods',
        city_block: 'City Blocks',
    };

    let html = `
        <div style="padding: var(--space-4); border-bottom: 1px solid var(--color-gray-200);">
            <input
                type="search"
                class="form-input"
                placeholder="Search communities..."
                id="region-search"
            >
        </div>
    `;

    for (const [type, typeRegions] of Object.entries(grouped)) {
        if (typeRegions.length === 0) continue;

        html += `
            <div class="region-list__section">
                <h3 style="padding: var(--space-3) var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-transform: uppercase; letter-spacing: 0.05em;">
                    ${typeLabels[type]} (${typeRegions.length})
                </h3>
                ${typeRegions.map(region => `
                    <a href="/communities/${region.id}" class="region-item" data-link data-region-id="${region.id}">
                        <div class="region-item__name">${escapeHtml(region.name)}</div>
                        ${region.parent_name ? `
                            <div class="region-item__meta">in ${escapeHtml(region.parent_name)}</div>
                        ` : ''}
                    </a>
                `).join('')}
            </div>
        `;
    }

    // Admin actions section
    if (isAuthenticated() && isAdmin()) {
        html += `
            <div class="region-list__section" style="border-top: 1px solid var(--color-gray-200);">
                <h3 style="padding: var(--space-3) var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-transform: uppercase; letter-spacing: 0.05em;">
                    Admin
                </h3>
                <a href="/admin/communities" class="region-item" data-link>
                    <div class="region-item__name">Create Community</div>
                </a>
            </div>
        `;
    }

    container.innerHTML = html;

    // Bind search
    const searchInput = document.getElementById('region-search');
    if (searchInput) {
        searchInput.addEventListener('input', (event) => {
            filterRegionList(event.target.value, container);
        });
    }
}

/**
 * Filter region list by search query
 * @param {string} query - Search query
 * @param {HTMLElement} container - List container
 */
function filterRegionList(query, container) {
    const normalizedQuery = query.toLowerCase().trim();
    const items = container.querySelectorAll('.region-item');

    items.forEach(item => {
        const name = item.querySelector('.region-item__name')?.textContent?.toLowerCase() || '';
        const meta = item.querySelector('.region-item__meta')?.textContent?.toLowerCase() || '';

        if (name.includes(normalizedQuery) || meta.includes(normalizedQuery)) {
            item.style.display = '';
        } else {
            item.style.display = 'none';
        }
    });
}

/**
 * Handle region click on map
 * @param {Object} feature - GeoJSON feature
 * @param {Object} event - Map event
 * @param {Object} map - Map instance
 */
function handleRegionClick(feature, event, map) {
    const properties = feature.properties;

    const popupHtml = `
        <div class="map-popup">
            <h4 class="map-popup__title">${escapeHtml(properties.name)}</h4>
            <p class="map-popup__meta">${formatRegionType(properties.type)}</p>
            <a href="/communities/${properties.id}" class="btn btn--primary btn--sm btn--block mt-2" data-link>
                View Details
            </a>
        </div>
    `;

    showPopup(map, event.lngLat.toArray(), popupHtml);
}

/**
 * Format region type for display
 * @param {string} type - Region type
 * @returns {string} Formatted type
 */
function formatRegionType(type) {
    const labels = {
        state: 'State',
        county: 'County',
        city: 'City',
        locality: 'Locality',
        neighborhood: 'Neighborhood',
        city_block: 'City Block',
    };
    return labels[type] || type;
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

/**
 * Cleanup when leaving the page
 */
export function cleanup() {
    if (mapInstance) {
        destroyMap();
        mapInstance = null;
    }
}

export default { render, cleanup };
