/**
 * Home/landing page
 * Shows hero section with map overview and call-to-action.
 */

import { isAuthenticated, hasReadAccess } from '../utils/store.js';
import { getRegions, regionsToFeatureCollection } from '../api/regions.js';
import { initMap, addRegionsLayer, createLegendHtml, destroyMap, fitToBounds } from '../components/map.js';
import { navigate } from '../app.js';

let mapInstance = null;

/**
 * Render the home page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const authenticated = isAuthenticated();
    const userHasReadAccess = hasReadAccess();

    container.innerHTML = `
        <div class="hero">
            <div class="hero__map" id="home-map"></div>
            <div class="hero__content">
                <h1 class="hero__title">Connect with your neighbors through verified Signal groups</h1>
                <p class="hero__description">
                    Community Rapid Response helps communities organize by connecting neighbors through
                    geographic-based Signal group chats. Verify your address to join your local community.
                </p>
                <div class="hero__actions">
                    ${authenticated ? `
                        ${!userHasReadAccess ? `
                            <a href="/verify" class="btn btn--primary btn--lg" data-link>Verify Your Address</a>
                            <a href="/communities" class="btn btn--secondary btn--lg" data-link>Explore Communities</a>
                        ` : `
                            <a href="/dashboard" class="btn btn--primary btn--lg" data-link>Go to Dashboard</a>
                            <a href="/groups" class="btn btn--secondary btn--lg" data-link>View Groups</a>
                        `}
                    ` : `
                        <a href="/register" class="btn btn--primary btn--lg" data-link>Get Started</a>
                        <a href="/communities" class="btn btn--secondary btn--lg" data-link>Explore Communities</a>
                    `}
                </div>
                <p class="hero__trust-link">
                    <a href="/about" data-link>Learn how we protect your privacy</a>
                </p>
            </div>
        </div>
    `;

    // Initialize map
    await initHomeMap();
}

/**
 * Initialize the home page map
 */
async function initHomeMap() {
    const mapContainer = document.getElementById('home-map');
    if (!mapContainer) return;

    // Destroy any existing map
    destroyMap();

    mapInstance = initMap({
        container: mapContainer,
        center: [-98.5795, 39.8283],
        zoom: 4,
        interactive: true,
        onLoad: async (map) => {
            // Load regions (empty result is fine - just show empty map)
            const regions = await getRegions().catch(() => []);
            if (regions.length > 0) {
                const geojson = regionsToFeatureCollection(regions);
                addRegionsLayer(map, geojson, {
                    onClick: (feature) => {
                        navigate(`/communities/${feature.properties.id}`);
                    },
                });

                // Fit to regions
                fitToBounds(map, geojson, { maxZoom: 10 });
            }

            // Add legend
            const legendContainer = document.createElement('div');
            legendContainer.innerHTML = createLegendHtml();
            mapContainer.appendChild(legendContainer.firstElementChild);
        },
    });
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
