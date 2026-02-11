/**
 * Mapbox GL JS wrapper component
 * Provides map initialization, region display, and polygon drawing functionality.
 */

import { store } from '../utils/store.js?v=c49aed9';

// Mapbox access token - should be configured in environment
const MAPBOX_TOKEN = window.MAPBOX_ACCESS_TOKEN || '';

// Default map settings
const DEFAULT_CENTER = [-98.5795, 39.8283]; // Center of US
const DEFAULT_ZOOM = 4;

// US bounds (includes continental US, Alaska, Hawaii, and territories)
// Using a generous bounding box that includes all US territories
const US_BOUNDS = [
    [-180, -15], // Southwest (includes American Samoa)
    [180, 72],    // Northeast (includes Alaska)
];

// Region type colors (hierarchy: state -> county -> city -> locality -> neighborhood -> city_block)
const REGION_COLORS = {
    state: '#6366f1',          // Indigo
    county: '#8b5cf6',         // Violet
    city: '#3b82f6',           // Blue
    city_town: '#3b82f6',      // Blue (legacy alias)
    locality: '#0ea5e9',       // Sky blue
    neighborhood: '#10b981',   // Green
    city_block: '#f59e0b',     // Amber
    school_district: '#ec4899', // Pink
};

const REGION_OPACITY = 0.3;
const REGION_BORDER_WIDTH = 2;

let mapInstance = null;
let drawInstance = null;

/**
 * Initialize a Mapbox map
 * @param {Object} options - Map options
 * @param {string|HTMLElement} options.container - Container element or ID
 * @param {number[]} [options.center] - Initial center [lng, lat]
 * @param {number} [options.zoom] - Initial zoom level
 * @param {boolean} [options.interactive=true] - Enable map interactions
 * @param {boolean} [options.restrictToUS=true] - Restrict panning to US bounds
 * @param {Function} [options.onLoad] - Callback when map loads
 * @param {Function} [options.onClick] - Callback for map clicks
 * @returns {Object} Mapbox map instance
 */
export function initMap(options = {}) {
    const {
        container,
        center = DEFAULT_CENTER,
        zoom = DEFAULT_ZOOM,
        interactive = true,
        restrictToUS = true,
        onLoad,
        onClick,
    } = options;

    if (!MAPBOX_TOKEN) {
        console.error('Mapbox access token not configured');
        const containerElement = typeof container === 'string'
            ? document.getElementById(container)
            : container;
        if (containerElement) {
            containerElement.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F5FA;</div>
                    <h3 class="empty-state__title">Map Unavailable</h3>
                    <p class="empty-state__description">
                        Mapbox access token not configured. Please set MAPBOX_ACCESS_TOKEN.
                    </p>
                </div>
            `;
        }
        return null;
    }

    mapboxgl.accessToken = MAPBOX_TOKEN;

    const map = new mapboxgl.Map({
        container,
        style: 'mapbox://styles/mapbox/light-v11',
        center,
        zoom,
        interactive,
        ...(restrictToUS && { maxBounds: US_BOUNDS }),
    });

    if (interactive) {
        map.addControl(new mapboxgl.NavigationControl(), 'top-right');
        map.addControl(new mapboxgl.GeolocateControl({
            positionOptions: { enableHighAccuracy: true },
            trackUserLocation: false,
        }), 'top-right');
    }

    map.on('load', () => {
        map.resize();
        if (onLoad) {
            onLoad(map);
        }
    });

    // Safety net: resize again after browser has had time to finalize layout.
    // Fixes sporadic rendering in flex containers where dimensions aren't
    // computed before the map style finishes loading.
    requestAnimationFrame(() => {
        requestAnimationFrame(() => {
            if (mapInstance === map) {
                map.resize();
            }
        });
    });

    if (onClick) {
        map.on('click', onClick);
    }

    mapInstance = map;
    return map;
}

/**
 * Add regions layer to the map
 * @param {Object} map - Mapbox map instance
 * @param {Object} geojson - GeoJSON FeatureCollection of regions
 * @param {Object} [options] - Layer options
 * @param {Function} [options.onClick] - Click handler for regions
 * @param {Function} [options.onHover] - Hover handler for regions
 */
export function addRegionsLayer(map, geojson, options = {}) {
    const { onClick, onHover } = options;
    const sourceId = 'regions';
    const layerId = 'regions-fill';
    const borderLayerId = 'regions-border';

    // Remove existing layers/source if present
    if (map.getLayer(borderLayerId)) map.removeLayer(borderLayerId);
    if (map.getLayer(layerId)) map.removeLayer(layerId);
    if (map.getSource(sourceId)) map.removeSource(sourceId);

    // Add source with generateId for feature state support
    map.addSource(sourceId, {
        type: 'geojson',
        data: geojson,
        generateId: true, // Auto-generate numeric IDs for setFeatureState
    });

    // Add fill layer
    map.addLayer({
        id: layerId,
        type: 'fill',
        source: sourceId,
        paint: {
            'fill-color': [
                'match',
                ['get', 'type'],
                'state', REGION_COLORS.state,
                'county', REGION_COLORS.county,
                'city', REGION_COLORS.city,
                'city_town', REGION_COLORS.city_town,
                'locality', REGION_COLORS.locality,
                'neighborhood', REGION_COLORS.neighborhood,
                'city_block', REGION_COLORS.city_block,
                'school_district', REGION_COLORS.school_district,
                '#888888', // default
            ],
            'fill-opacity': [
                'case',
                ['boolean', ['feature-state', 'hover'], false],
                REGION_OPACITY + 0.2,
                REGION_OPACITY,
            ],
        },
    });

    // Add border layer
    map.addLayer({
        id: borderLayerId,
        type: 'line',
        source: sourceId,
        paint: {
            'line-color': [
                'match',
                ['get', 'type'],
                'state', REGION_COLORS.state,
                'county', REGION_COLORS.county,
                'city', REGION_COLORS.city,
                'city_town', REGION_COLORS.city_town,
                'locality', REGION_COLORS.locality,
                'neighborhood', REGION_COLORS.neighborhood,
                'city_block', REGION_COLORS.city_block,
                'school_district', REGION_COLORS.school_district,
                '#888888',
            ],
            'line-width': REGION_BORDER_WIDTH,
        },
    });

    // Hover state
    let hoveredFeatureId = null;

    map.on('mousemove', layerId, (event) => {
        if (event.features.length > 0) {
            const featureId = event.features[0].id;

            // Only use setFeatureState if feature has an id
            if (featureId !== undefined && featureId !== null) {
                // Clear previous hover
                if (hoveredFeatureId !== null) {
                    map.setFeatureState(
                        { source: sourceId, id: hoveredFeatureId },
                        { hover: false }
                    );
                }

                hoveredFeatureId = featureId;
                map.setFeatureState(
                    { source: sourceId, id: hoveredFeatureId },
                    { hover: true }
                );
            }

            map.getCanvas().style.cursor = 'pointer';

            if (onHover) {
                onHover(event.features[0], event);
            }
        }
    });

    map.on('mouseleave', layerId, () => {
        if (hoveredFeatureId !== null) {
            try {
                map.setFeatureState(
                    { source: sourceId, id: hoveredFeatureId },
                    { hover: false }
                );
            } catch (e) {
                // Ignore errors if feature state can't be cleared
            }
        }
        hoveredFeatureId = null;
        map.getCanvas().style.cursor = '';
    });

    // Click handler
    if (onClick) {
        map.on('click', layerId, (event) => {
            if (event.features.length > 0) {
                onClick(event.features[0], event);
            }
        });
    }
}

/**
 * Update regions data on existing layer
 * @param {Object} map - Mapbox map instance
 * @param {Object} geojson - Updated GeoJSON FeatureCollection
 */
export function updateRegionsData(map, geojson) {
    const source = map.getSource('regions');
    if (source) {
        source.setData(geojson);
    }
}

/**
 * Add admin boundary overlay (for region creation)
 * @param {Object} map - Mapbox map instance
 * @param {Object} boundary - GeoJSON Polygon of admin boundary
 */
export function addAdminBoundary(map, boundary) {
    const sourceId = 'admin-boundary';
    const layerId = 'admin-boundary-fill';
    const borderLayerId = 'admin-boundary-border';

    // Remove existing
    if (map.getLayer(borderLayerId)) map.removeLayer(borderLayerId);
    if (map.getLayer(layerId)) map.removeLayer(layerId);
    if (map.getSource(sourceId)) map.removeSource(sourceId);

    map.addSource(sourceId, {
        type: 'geojson',
        data: {
            type: 'Feature',
            geometry: boundary,
        },
    });

    // Semi-transparent fill
    map.addLayer({
        id: layerId,
        type: 'fill',
        source: sourceId,
        paint: {
            'fill-color': '#6366f1',
            'fill-opacity': 0.1,
        },
    });

    // Dashed border
    map.addLayer({
        id: borderLayerId,
        type: 'line',
        source: sourceId,
        paint: {
            'line-color': '#6366f1',
            'line-width': 2,
            'line-dasharray': [3, 3],
        },
    });
}

/**
 * Initialize Mapbox Draw for polygon creation
 * @param {Object} map - Mapbox map instance
 * @param {Object} [options] - Draw options
 * @param {Function} [options.onCreate] - Callback when polygon created
 * @param {Function} [options.onUpdate] - Callback when polygon updated
 * @param {Function} [options.onDelete] - Callback when polygon deleted
 * @returns {Object} Mapbox Draw instance
 */
export function initDraw(map, options = {}) {
    const { onCreate, onUpdate, onDelete } = options;

    if (!window.MapboxDraw) {
        console.error('Mapbox Draw plugin not loaded');
        return null;
    }

    const draw = new MapboxDraw({
        displayControlsDefault: false,
        controls: {
            polygon: true,
            trash: true,
        },
        defaultMode: 'simple_select',
    });

    map.addControl(draw, 'top-left');

    // Event handlers
    if (onCreate) {
        map.on('draw.create', (event) => {
            const feature = event.features[0];
            if (feature) {
                onCreate(feature.geometry);
            }
        });
    }

    if (onUpdate) {
        map.on('draw.update', (event) => {
            const feature = event.features[0];
            if (feature) {
                onUpdate(feature.geometry);
            }
        });
    }

    if (onDelete) {
        map.on('draw.delete', () => {
            onDelete();
        });
    }

    drawInstance = draw;
    return draw;
}

/**
 * Set the drawn polygon programmatically
 * @param {Object} draw - Mapbox Draw instance
 * @param {Object} geometry - GeoJSON Polygon geometry
 */
export function setDrawnPolygon(draw, geometry) {
    draw.deleteAll();
    draw.add({
        type: 'Feature',
        geometry,
    });
}

/**
 * Get the current drawn polygon
 * @param {Object} draw - Mapbox Draw instance
 * @returns {Object|null} GeoJSON Polygon geometry or null
 */
export function getDrawnPolygon(draw) {
    const data = draw.getAll();
    if (data.features.length > 0) {
        return data.features[0].geometry;
    }
    return null;
}

/**
 * Clear all drawn polygons
 * @param {Object} draw - Mapbox Draw instance
 */
export function clearDrawn(draw) {
    draw.deleteAll();
}

/**
 * Show a popup at a location
 * @param {Object} map - Mapbox map instance
 * @param {number[]} coordinates - [lng, lat]
 * @param {string} html - Popup HTML content
 * @returns {Object} Mapbox Popup instance
 */
export function showPopup(map, coordinates, html) {
    const popup = new mapboxgl.Popup({
        closeButton: true,
        closeOnClick: false,
        maxWidth: '300px',
    })
        .setLngLat(coordinates)
        .setHTML(html)
        .addTo(map);

    return popup;
}

/**
 * Fit map to bounds of a GeoJSON feature
 * @param {Object} map - Mapbox map instance
 * @param {Object} geojson - GeoJSON Feature or FeatureCollection
 * @param {Object} [options] - Fit bounds options
 */
export function fitToBounds(map, geojson, options = {}) {
    const bounds = new mapboxgl.LngLatBounds();

    const addCoordinates = (coords) => {
        if (typeof coords[0] === 'number') {
            bounds.extend(coords);
        } else {
            coords.forEach(addCoordinates);
        }
    };

    if (geojson.type === 'FeatureCollection') {
        geojson.features.forEach(feature => {
            addCoordinates(feature.geometry.coordinates);
        });
    } else if (geojson.type === 'Feature') {
        addCoordinates(geojson.geometry.coordinates);
    } else {
        addCoordinates(geojson.coordinates);
    }

    if (!bounds.isEmpty()) {
        map.fitBounds(bounds, {
            padding: 50,
            maxZoom: 15,
            ...options,
        });
    }
}

/**
 * Get the current map instance
 * @returns {Object|null} Mapbox map instance
 */
export function getMap() {
    return mapInstance;
}

/**
 * Get the current draw instance
 * @returns {Object|null} Mapbox Draw instance
 */
export function getDraw() {
    return drawInstance;
}

/**
 * Destroy map instance
 */
export function destroyMap() {
    if (mapInstance) {
        mapInstance.remove();
        mapInstance = null;
        drawInstance = null;
    }
}

/**
 * Create map legend HTML
 * @returns {string} Legend HTML
 */
export function createLegendHtml() {
    return `
        <div class="map-legend">
            <div class="map-legend__item">
                <div class="map-legend__color" style="background: ${REGION_COLORS.state}"></div>
                <span>State</span>
            </div>
            <div class="map-legend__item">
                <div class="map-legend__color" style="background: ${REGION_COLORS.county}"></div>
                <span>County</span>
            </div>
            <div class="map-legend__item">
                <div class="map-legend__color" style="background: ${REGION_COLORS.city}"></div>
                <span>City</span>
            </div>
            <div class="map-legend__item">
                <div class="map-legend__color" style="background: ${REGION_COLORS.locality}"></div>
                <span>Locality</span>
            </div>
            <div class="map-legend__item">
                <div class="map-legend__color" style="background: ${REGION_COLORS.neighborhood}"></div>
                <span>Neighborhood</span>
            </div>
        </div>
    `;
}

/**
 * Check if a polygon's centroid is within US bounds
 * This is a client-side validation before submitting to the server
 * @param {Object} geometry - GeoJSON Polygon geometry
 * @returns {boolean} True if within US bounds
 */
export function isGeometryWithinUS(geometry) {
    if (!geometry || !geometry.coordinates) return false;

    // Calculate centroid of the polygon
    const coords = geometry.coordinates[0]; // Outer ring
    let sumLng = 0;
    let sumLat = 0;
    const numPoints = coords.length - 1; // Last point equals first

    for (let i = 0; i < numPoints; i++) {
        sumLng += coords[i][0];
        sumLat += coords[i][1];
    }

    const centroidLng = sumLng / numPoints;
    const centroidLat = sumLat / numPoints;

    // Check if centroid is within any US territory bounds
    return (
        // Continental US
        (centroidLat >= 24 && centroidLat <= 50 && centroidLng >= -125 && centroidLng <= -66) ||
        // Alaska
        (centroidLat >= 51 && centroidLat <= 72 && centroidLng >= -180 && centroidLng <= -130) ||
        // Hawaii
        (centroidLat >= 18 && centroidLat <= 29 && centroidLng >= -180 && centroidLng <= -154) ||
        // Puerto Rico and US Virgin Islands
        (centroidLat >= 17 && centroidLat <= 19 && centroidLng >= -68 && centroidLng <= -64) ||
        // Guam and Northern Mariana Islands
        (centroidLat >= 13 && centroidLat <= 21 && centroidLng >= 144 && centroidLng <= 146) ||
        // American Samoa
        (centroidLat >= -15 && centroidLat <= -11 && centroidLng >= -171 && centroidLng <= -168)
    );
}

/**
 * Search for a place using Mapbox Geocoding API
 * @param {string} query - Search query (e.g., "Austin, Texas")
 * @param {Object} [options] - Search options
 * @param {string} [options.types] - Place types to search (default: 'place,locality')
 * @param {string} [options.country] - Country code to limit results (default: 'us')
 * @returns {Promise<Array>} Array of place results
 */
export async function searchPlaces(query, options = {}) {
    const {
        types = 'place,locality',
        country = 'us',
    } = options;

    if (!MAPBOX_TOKEN || !query.trim()) {
        return [];
    }

    const url = `https://api.mapbox.com/geocoding/v5/mapbox.places/${encodeURIComponent(query)}.json?` +
        `access_token=${MAPBOX_TOKEN}&types=${types}&country=${country}&limit=5`;

    try {
        const response = await fetch(url);
        const data = await response.json();
        return data.features || [];
    } catch (error) {
        console.error('Geocoding error:', error);
        return [];
    }
}

/**
 * Get boundary polygon for a place
 * First tries to get actual boundary from OSM Nominatim, falls back to bounding box
 * @param {Object} place - Mapbox geocoding result feature
 * @returns {Promise<Object|null>} GeoJSON Polygon geometry
 */
export async function getPlaceBoundary(place) {
    if (!place) return null;

    // If the place has a polygon geometry, use it
    if (place.geometry && place.geometry.type === 'Polygon') {
        return place.geometry;
    }

    // Try to get actual boundary from OpenStreetMap Nominatim
    if (place.text && place.place_name) {
        try {
            const boundary = await fetchOSMBoundary(place.place_name);
            if (boundary) {
                return boundary;
            }
        } catch (error) {
            console.warn('Could not fetch OSM boundary:', error);
        }
    }

    // Fall back to bounding box if no polygon available
    if (place.bbox && place.bbox.length === 4) {
        const [minLng, minLat, maxLng, maxLat] = place.bbox;
        return {
            type: 'Polygon',
            coordinates: [[
                [minLng, minLat],
                [maxLng, minLat],
                [maxLng, maxLat],
                [minLng, maxLat],
                [minLng, minLat], // Close the polygon
            ]],
        };
    }

    return null;
}

/**
 * Fetch actual city/town boundary from OpenStreetMap Nominatim
 * @param {string} placeName - Full place name (e.g., "Austin, Texas, United States")
 * @returns {Promise<Object|null>} GeoJSON Polygon geometry
 */
async function fetchOSMBoundary(placeName) {
    // Search by name with polygon output
    const url = `https://nominatim.openstreetmap.org/search?` +
        `format=geojson&q=${encodeURIComponent(placeName)}&polygon_geojson=1&limit=1`;

    const response = await fetch(url, {
        headers: {
            'User-Agent': 'CommunityRapidResponse/1.0',
        },
    });

    if (!response.ok) {
        return null;
    }

    const data = await response.json();

    if (data.features && data.features.length > 0) {
        const feature = data.features[0];
        if (feature.geometry) {
            if (feature.geometry.type === 'Polygon') {
                return feature.geometry;
            }
            if (feature.geometry.type === 'MultiPolygon') {
                return convertMultiPolygonToPolygon(feature.geometry);
            }
        }
    }

    return null;
}

/**
 * Convert MultiPolygon to single Polygon (uses the largest polygon by point count)
 * @param {Object} multiPolygon - GeoJSON MultiPolygon geometry
 * @returns {Object} GeoJSON Polygon geometry
 */
function convertMultiPolygonToPolygon(multiPolygon) {
    if (!multiPolygon.coordinates || multiPolygon.coordinates.length === 0) {
        return null;
    }

    // Find the polygon with the most points (rough approximation of largest)
    let largestPolygon = multiPolygon.coordinates[0];
    let maxPoints = countPolygonPoints(largestPolygon);

    for (let i = 1; i < multiPolygon.coordinates.length; i++) {
        const points = countPolygonPoints(multiPolygon.coordinates[i]);
        if (points > maxPoints) {
            maxPoints = points;
            largestPolygon = multiPolygon.coordinates[i];
        }
    }

    return {
        type: 'Polygon',
        coordinates: largestPolygon,
    };
}

/**
 * Count total points in a polygon (including holes)
 * @param {Array} polygonCoords - Polygon coordinates array
 * @returns {number} Total point count
 */
function countPolygonPoints(polygonCoords) {
    return polygonCoords.reduce((sum, ring) => sum + ring.length, 0);
}

/**
 * Add a preview boundary layer to the map
 * @param {Object} map - Mapbox map instance
 * @param {Object} geometry - GeoJSON Polygon geometry
 */
export function addPreviewBoundary(map, geometry) {
    const sourceId = 'preview-boundary';
    const fillLayerId = 'preview-boundary-fill';
    const borderLayerId = 'preview-boundary-border';

    // Remove existing preview layers
    removePreviewBoundary(map);

    map.addSource(sourceId, {
        type: 'geojson',
        data: {
            type: 'Feature',
            geometry,
        },
    });

    map.addLayer({
        id: fillLayerId,
        type: 'fill',
        source: sourceId,
        paint: {
            'fill-color': '#10b981',
            'fill-opacity': 0.2,
        },
    });

    map.addLayer({
        id: borderLayerId,
        type: 'line',
        source: sourceId,
        paint: {
            'line-color': '#10b981',
            'line-width': 3,
        },
    });
}

/**
 * Remove preview boundary layer from the map
 * @param {Object} map - Mapbox map instance
 */
export function removePreviewBoundary(map) {
    const sourceId = 'preview-boundary';
    const fillLayerId = 'preview-boundary-fill';
    const borderLayerId = 'preview-boundary-border';

    if (map.getLayer(borderLayerId)) map.removeLayer(borderLayerId);
    if (map.getLayer(fillLayerId)) map.removeLayer(fillLayerId);
    if (map.getSource(sourceId)) map.removeSource(sourceId);
}

/**
 * Add a parent region boundary layer to the map
 * @param {Object} map - Mapbox map instance
 * @param {Object} geometry - GeoJSON Polygon geometry
 */
export function addParentBoundary(map, geometry) {
    const sourceId = 'parent-boundary';
    const fillLayerId = 'parent-boundary-fill';
    const borderLayerId = 'parent-boundary-border';

    // Remove existing parent boundary layers
    removeParentBoundary(map);

    map.addSource(sourceId, {
        type: 'geojson',
        data: {
            type: 'Feature',
            geometry: geometry,
        },
    });

    // Add fill layer (light blue, semi-transparent)
    map.addLayer({
        id: fillLayerId,
        type: 'fill',
        source: sourceId,
        paint: {
            'fill-color': '#3b82f6',
            'fill-opacity': 0.1,
        },
    });

    // Add border layer (blue dashed line)
    map.addLayer({
        id: borderLayerId,
        type: 'line',
        source: sourceId,
        paint: {
            'line-color': '#3b82f6',
            'line-width': 3,
            'line-dasharray': [3, 2],
        },
    });
}

/**
 * Remove parent boundary layer from the map
 * @param {Object} map - Mapbox map instance
 */
export function removeParentBoundary(map) {
    const sourceId = 'parent-boundary';
    const fillLayerId = 'parent-boundary-fill';
    const borderLayerId = 'parent-boundary-border';

    if (map.getLayer(borderLayerId)) map.removeLayer(borderLayerId);
    if (map.getLayer(fillLayerId)) map.removeLayer(fillLayerId);
    if (map.getSource(sourceId)) map.removeSource(sourceId);
}

export default {
    initMap,
    addRegionsLayer,
    updateRegionsData,
    addAdminBoundary,
    initDraw,
    setDrawnPolygon,
    getDrawnPolygon,
    clearDrawn,
    showPopup,
    fitToBounds,
    getMap,
    getDraw,
    destroyMap,
    createLegendHtml,
    isGeometryWithinUS,
    searchPlaces,
    getPlaceBoundary,
    addPreviewBoundary,
    removePreviewBoundary,
    addParentBoundary,
    removeParentBoundary,
    REGION_COLORS,
    US_BOUNDS,
};
