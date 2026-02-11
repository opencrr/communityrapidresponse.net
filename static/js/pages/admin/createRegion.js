/**
 * Admin: Create Region page
 * Allows admins to create new regions using Mapbox Draw for polygon creation
 * or by searching for a city/state name.
 */

import {
    createRegion,
    getAdminBoundary,
    validatePolygon,
    getRegion,
    getRegions,
} from '../../api/regions.js';
import { ApiError } from '../../api/client.js';
import {
    initMap,
    initDraw,
    addAdminBoundary,
    getDrawnPolygon,
    clearDrawn,
    destroyMap,
    fitToBounds,
    isGeometryWithinUS,
    searchPlaces,
    getPlaceBoundary,
    addPreviewBoundary,
    removePreviewBoundary,
    addParentBoundary,
    removeParentBoundary,
    setDrawnPolygon,
} from '../../components/map.js';
import { isAdmin, isSuperuser } from '../../utils/store.js';
import toast from '../../components/toast.js';
import { navigate } from '../../app.js';

let mapInstance = null;
let drawInstance = null;
let adminBoundary = null;
let selectedBoundary = null;
let boundaryMode = 'draw'; // 'draw' or 'search'
let searchTimeout = null;

/**
 * Render the create region page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const userIsAdmin = isAdmin();

    if (!userIsAdmin) {
        container.innerHTML = `
            <div class="page page--centered">
                <div class="empty-state">
                    <div class="empty-state__icon">&#x1F512;</div>
                    <h3 class="empty-state__title">Admin Access Required</h3>
                    <p class="empty-state__description">
                        You need both postcard and vouch verification to create communities.
                    </p>
                    <a href="/dashboard" class="btn btn--primary" data-link>View Verification Status</a>
                </div>
            </div>
        `;
        return;
    }

    // Get parent ID from URL if present
    const urlParams = new URLSearchParams(window.location.search);
    const parentId = urlParams.get('parent');

    container.innerHTML = `
        <div class="page map-layout">
            <div class="map-layout__sidebar">
                <div class="create-region-form">
                    <h1 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-4);">
                        Create Community
                    </h1>

                    <form id="create-region-form">
                        <div class="form-group">
                            <label for="name" class="form-label form-label--required">Community Name</label>
                            <input
                                type="text"
                                id="name"
                                name="name"
                                class="form-input"
                                required
                                placeholder="e.g., Downtown, Oak Park, Austin"
                            >
                        </div>

                        <div class="form-group">
                            <label for="type" class="form-label form-label--required">Community Type</label>
                            <select id="type" name="type" class="form-input" required>
                                <option value="">Select type</option>
                                <option value="state">State</option>
                                <option value="county">County</option>
                                <option value="city">City</option>
                                <option value="neighborhood">Neighborhood</option>
                            </select>
                            <p class="form-hint" id="type-hint">
                                State &rarr; County &rarr; City &rarr; Neighborhood
                            </p>
                        </div>

                        <div class="form-group">
                            <label for="parent_id" class="form-label">Parent Community</label>
                            <select id="parent_id" name="parent_id" class="form-input">
                                <option value="">Auto-detect from location</option>
                            </select>
                            <p class="form-hint" id="parent-hint">
                                Leave empty to auto-detect and create parent communities based on the boundary location.
                            </p>
                        </div>

                        <div class="form-group">
                            <label class="form-label form-label--required">Boundary</label>
                            <div class="boundary-mode-toggle">
                                <button type="button" class="boundary-mode-btn boundary-mode-btn--active" data-mode="draw">
                                    Draw on Map
                                </button>
                                <button type="button" class="boundary-mode-btn" data-mode="search">
                                    Search Location
                                </button>
                            </div>
                        </div>

                        <!-- Draw Mode Instructions -->
                        <div id="draw-mode-content" class="boundary-mode-content">
                            <div class="drawing-instructions">
                                <h4>How to Draw</h4>
                                <ol>
                                    <li>Click the polygon tool on the map</li>
                                    <li>Click to place points around your region</li>
                                    <li>Double-click to complete</li>
                                </ol>
                            </div>
                        </div>

                        <!-- Search Mode Content -->
                        <div id="search-mode-content" class="boundary-mode-content hidden">
                            <div class="form-group">
                                <label for="location-search" class="form-label">Search City/Town</label>
                                <input
                                    type="text"
                                    id="location-search"
                                    class="form-input"
                                    placeholder="e.g., Austin, Texas"
                                    autocomplete="off"
                                >
                                <div id="search-results" class="search-results hidden"></div>
                            </div>
                            <div id="selected-location" class="selected-location hidden">
                                <div class="selected-location__info">
                                    <strong id="selected-location-name"></strong>
                                    <button type="button" id="clear-location" class="btn btn--sm btn--ghost">&times;</button>
                                </div>
                            </div>
                        </div>

                        <div class="form-group" id="polygon-status">
                            <div id="polygon-feedback" class="form-hint">
                                Choose a method above to define your region boundary.
                            </div>
                        </div>

                        <div id="form-error" class="form-error hidden"></div>

                        <button type="submit" class="btn btn--primary btn--block" id="submit-btn" disabled>
                            Create Community
                        </button>
                    </form>
                </div>
            </div>
            <div class="map-layout__main">
                <div class="map-container" id="create-region-map"></div>
            </div>
        </div>
    `;

    // Load data and initialize
    await Promise.all([
        loadAdminBoundary(),
        loadParentRegions(parentId),
    ]);

    initCreateRegionMap();
    bindFormHandlers();
    bindModeToggle();
    bindLocationSearch();
    bindParentRegionHandler();
    bindTypeParentValidation();

    // If a parent ID was provided, load and show its boundary
    if (parentId) {
        await showParentRegionBoundary(parentId);
        // Also update the type based on the parent
        updateTypeOptionsForParent();
    }
}

/**
 * Load the admin boundary for the current user
 */
async function loadAdminBoundary() {
    try {
        adminBoundary = await getAdminBoundary();
    } catch (error) {
        // Admin boundary endpoint may not be implemented yet - this is expected
        // Users can still create regions, they just won't see their authorized area
        adminBoundary = null;
    }
}

/**
 * Load parent regions for the dropdown
 * @param {string} selectedId - ID to pre-select
 */
async function loadParentRegions(selectedId) {
    const select = document.getElementById('parent_id');
    if (!select) return;

    try {
        const regions = await getRegions();

        regions.forEach(region => {
            const regionType = region.type || region.region_type;
            const option = document.createElement('option');
            option.value = region.id;
            option.dataset.regionType = regionType;
            option.textContent = `${region.name} (${formatRegionType(regionType)})`;
            if (region.id === selectedId) {
                option.selected = true;
            }
            select.appendChild(option);
        });
    } catch (error) {
        console.error('Failed to load regions:', error);
    }
}

/**
 * Initialize the map with drawing tools
 */
function initCreateRegionMap() {
    const mapContainer = document.getElementById('create-region-map');
    if (!mapContainer) return;

    destroyMap();

    const userIsSuperuser = isSuperuser();

    mapInstance = initMap({
        container: mapContainer,
        center: [-98.5795, 39.8283],
        zoom: 4,
        interactive: true,
        restrictToUS: !userIsSuperuser,
        onLoad: (map) => {
            if (adminBoundary) {
                addAdminBoundary(map, adminBoundary);
                fitToBounds(map, { type: 'Feature', geometry: adminBoundary }, { maxZoom: 12 });
            }

            // Initialize drawing tools (visible in draw mode)
            drawInstance = initDraw(map, {
                onCreate: handlePolygonCreate,
                onUpdate: handlePolygonUpdate,
                onDelete: handlePolygonDelete,
            });
        },
    });
}

/**
 * Bind mode toggle buttons
 */
function bindModeToggle() {
    const buttons = document.querySelectorAll('.boundary-mode-btn');
    const drawContent = document.getElementById('draw-mode-content');
    const searchContent = document.getElementById('search-mode-content');

    buttons.forEach(btn => {
        btn.addEventListener('click', () => {
            const mode = btn.dataset.mode;
            if (mode === boundaryMode) return;

            // Update active button
            buttons.forEach(b => b.classList.remove('boundary-mode-btn--active'));
            btn.classList.add('boundary-mode-btn--active');

            // Show/hide content
            if (mode === 'draw') {
                drawContent.classList.remove('hidden');
                searchContent.classList.add('hidden');
                // Show draw controls
                if (drawInstance && mapInstance) {
                    // Clear any preview boundary
                    removePreviewBoundary(mapInstance);
                }
            } else {
                drawContent.classList.add('hidden');
                searchContent.classList.remove('hidden');
                // Clear drawn polygon when switching to search
                if (drawInstance) {
                    clearDrawn(drawInstance);
                }
            }

            boundaryMode = mode;
            selectedBoundary = null;
            updatePolygonFeedback(false, mode === 'draw'
                ? 'Use the map tools to draw your region boundary.'
                : 'Search for a city or town above.');
            document.getElementById('submit-btn').disabled = true;
        });
    });
}

/**
 * Bind location search functionality
 */
function bindLocationSearch() {
    const searchInput = document.getElementById('location-search');
    const resultsContainer = document.getElementById('search-results');
    const selectedContainer = document.getElementById('selected-location');
    const selectedName = document.getElementById('selected-location-name');
    const clearBtn = document.getElementById('clear-location');

    if (!searchInput) return;

    // Debounced search
    searchInput.addEventListener('input', (e) => {
        const query = e.target.value.trim();

        if (searchTimeout) {
            clearTimeout(searchTimeout);
        }

        if (query.length < 2) {
            resultsContainer.classList.add('hidden');
            return;
        }

        searchTimeout = setTimeout(async () => {
            const results = await searchPlaces(query);
            displaySearchResults(results);
        }, 300);
    });

    // Clear location
    clearBtn?.addEventListener('click', () => {
        selectedBoundary = null;
        searchInput.value = '';
        selectedContainer.classList.add('hidden');
        resultsContainer.classList.add('hidden');

        if (mapInstance) {
            removePreviewBoundary(mapInstance);
        }

        updatePolygonFeedback(false, 'Search for a city or town above.');
        document.getElementById('submit-btn').disabled = true;
    });

    // Close results when clicking outside
    document.addEventListener('click', (e) => {
        if (!searchInput.contains(e.target) && !resultsContainer.contains(e.target)) {
            resultsContainer.classList.add('hidden');
        }
    });
}

/**
 * Bind parent region dropdown change handler
 */
function bindParentRegionHandler() {
    const parentSelect = document.getElementById('parent_id');
    if (!parentSelect) return;

    parentSelect.addEventListener('change', async (e) => {
        const parentId = e.target.value;

        if (!parentId) {
            // No parent selected, remove parent boundary
            if (mapInstance) {
                removeParentBoundary(mapInstance);
            }
            return;
        }

        await showParentRegionBoundary(parentId);
    });
}

/**
 * Fetch and display parent region boundary on map
 * @param {string} parentId - Parent region ID
 */
async function showParentRegionBoundary(parentId) {
    if (!mapInstance) return;

    try {
        const parentRegion = await getRegion(parentId);

        if (parentRegion && parentRegion.geometry) {
            addParentBoundary(mapInstance, parentRegion.geometry);
            fitToBounds(mapInstance, { type: 'Feature', geometry: parentRegion.geometry }, { maxZoom: 14 });
        }
    } catch (error) {
        console.error('Failed to load parent region:', error);
    }
}

/**
 * Bind type and parent validation handlers
 */
function bindTypeParentValidation() {
    const typeSelect = document.getElementById('type');
    const parentSelect = document.getElementById('parent_id');

    if (!typeSelect || !parentSelect) return;

    // When type changes, update parent options and validation
    typeSelect.addEventListener('change', () => {
        updateParentOptionsForType();
        validateTypeParentCombination();
    });

    // When parent changes, update type options and validation
    parentSelect.addEventListener('change', () => {
        updateTypeOptionsForParent();
        validateTypeParentCombination();
    });
}

/**
 * Update parent dropdown options based on selected type
 */
function updateParentOptionsForType() {
    const typeSelect = document.getElementById('type');
    const parentSelect = document.getElementById('parent_id');
    const parentHint = document.getElementById('parent-hint');

    if (!typeSelect || !parentSelect) return;

    const selectedType = typeSelect.value;
    const options = parentSelect.querySelectorAll('option');

    // Reset all options to visible
    options.forEach(opt => {
        opt.hidden = false;
        opt.disabled = false;
    });

    if (selectedType === 'state') {
        // State cannot have a parent
        parentSelect.value = '';
        parentSelect.disabled = true;
        parentHint.textContent = 'State regions cannot have a parent.';
    } else if (selectedType === 'county') {
        // County needs a State parent - can select existing or auto-detect
        parentSelect.disabled = false;
        parentHint.textContent = 'Select a State parent, or leave empty to auto-detect from location.';

        // Filter to show only state options
        options.forEach(opt => {
            if (opt.value && opt.dataset.regionType !== 'state') {
                opt.hidden = true;
                if (opt.selected) {
                    parentSelect.value = '';
                }
            }
        });
        // Show "Auto-detect" option
        options[0].hidden = false;
        options[0].textContent = 'Auto-detect State from location';
    } else if (selectedType === 'city') {
        // City needs a County parent - can select existing or auto-detect
        parentSelect.disabled = false;
        parentHint.textContent = 'Select a County parent, or leave empty to auto-detect from location.';

        // Filter to show only county options
        options.forEach(opt => {
            if (opt.value && opt.dataset.regionType !== 'county') {
                opt.hidden = true;
                if (opt.selected) {
                    parentSelect.value = '';
                }
            }
        });
        // Show "Auto-detect" option
        options[0].hidden = false;
        options[0].textContent = 'Auto-detect County & State from location';
    } else if (selectedType === 'neighborhood') {
        // Neighborhood must have an existing City or Locality parent (no auto-detect)
        parentSelect.disabled = false;
        parentHint.textContent = 'Select a City or Locality as the parent region (required).';

        // Filter to show only city and locality options
        options.forEach(opt => {
            if (opt.value && opt.dataset.regionType !== 'city' && opt.dataset.regionType !== 'locality') {
                opt.hidden = true;
                if (opt.selected) {
                    parentSelect.value = '';
                }
            }
        });
        // Hide "Auto-detect" option - neighborhoods require existing parent
        options[0].hidden = true;
    } else {
        // No type selected
        parentSelect.disabled = false;
        parentHint.textContent = 'Select a region type first.';
        options[0].textContent = 'Auto-detect from location';
    }
}

/**
 * Update type dropdown options based on selected parent
 */
function updateTypeOptionsForParent() {
    const typeSelect = document.getElementById('type');
    const parentSelect = document.getElementById('parent_id');
    const typeHint = document.getElementById('type-hint');

    if (!typeSelect || !parentSelect) return;

    const selectedParentOption = parentSelect.options[parentSelect.selectedIndex];
    const parentType = selectedParentOption?.dataset?.regionType;

    // Reset all type options to enabled
    const typeOptions = typeSelect.querySelectorAll('option');
    typeOptions.forEach(opt => {
        opt.disabled = false;
    });

    if (!parentSelect.value) {
        // No parent selected - only State is allowed
        typeOptions.forEach(opt => {
            if (opt.value === 'county' || opt.value === 'city' || opt.value === 'neighborhood') {
                opt.disabled = true;
            }
        });
        typeHint.textContent = 'Select a parent region to create County, City, or Neighborhood regions.';

        // If current type is invalid, reset it
        if (typeSelect.value === 'county' || typeSelect.value === 'city' || typeSelect.value === 'neighborhood') {
            typeSelect.value = '';
        }
    } else if (parentType === 'state') {
        // Parent is State - only County is allowed
        typeOptions.forEach(opt => {
            if (opt.value === 'state' || opt.value === 'city' || opt.value === 'neighborhood') {
                opt.disabled = true;
            }
        });
        typeHint.textContent = 'Creating a County within a State.';

        // Auto-select County if nothing selected
        if (!typeSelect.value || typeSelect.value === 'state' || typeSelect.value === 'city' || typeSelect.value === 'neighborhood') {
            typeSelect.value = 'county';
        }
    } else if (parentType === 'county') {
        // Parent is County - only City is allowed
        typeOptions.forEach(opt => {
            if (opt.value === 'state' || opt.value === 'county' || opt.value === 'neighborhood') {
                opt.disabled = true;
            }
        });
        typeHint.textContent = 'Creating a City within a County.';

        // Auto-select City if nothing selected
        if (!typeSelect.value || typeSelect.value === 'state' || typeSelect.value === 'county' || typeSelect.value === 'neighborhood') {
            typeSelect.value = 'city';
        }
    } else if (parentType === 'city') {
        // Parent is City - only Neighborhood is allowed
        typeOptions.forEach(opt => {
            if (opt.value === 'state' || opt.value === 'county' || opt.value === 'city') {
                opt.disabled = true;
            }
        });
        typeHint.textContent = 'Creating a Neighborhood within a City.';

        // Auto-select Neighborhood if nothing selected
        if (!typeSelect.value || typeSelect.value === 'state' || typeSelect.value === 'county' || typeSelect.value === 'city') {
            typeSelect.value = 'neighborhood';
        }
    } else if (parentType === 'locality') {
        // Parent is Locality - only Neighborhood is allowed
        typeOptions.forEach(opt => {
            if (opt.value === 'state' || opt.value === 'county' || opt.value === 'city') {
                opt.disabled = true;
            }
        });
        typeHint.textContent = 'Creating a Neighborhood within a Locality.';

        // Auto-select Neighborhood if nothing selected
        if (!typeSelect.value || typeSelect.value === 'state' || typeSelect.value === 'county' || typeSelect.value === 'city') {
            typeSelect.value = 'neighborhood';
        }
    }
}

/**
 * Validate the type and parent combination
 * @returns {boolean} Whether the combination is valid
 */
function validateTypeParentCombination() {
    const typeSelect = document.getElementById('type');
    const parentSelect = document.getElementById('parent_id');
    const errorElement = document.getElementById('form-error');

    if (!typeSelect || !parentSelect) return true;

    const selectedType = typeSelect.value;
    const selectedParentOption = parentSelect.options[parentSelect.selectedIndex];
    const parentType = selectedParentOption?.dataset?.regionType;

    let error = null;

    if (selectedType === 'state' && parentSelect.value) {
        error = 'State regions cannot have a parent region.';
    } else if (selectedType === 'county') {
        // County can auto-detect state from location, so no parent is OK
        if (parentSelect.value && parentType !== 'state') {
            error = 'County regions must have a State parent.';
        }
    } else if (selectedType === 'city') {
        // City can auto-detect county/state from location, so no parent is OK
        if (parentSelect.value && parentType !== 'county') {
            error = 'City regions must have a County parent.';
        }
    } else if (selectedType === 'neighborhood') {
        if (!parentSelect.value) {
            error = 'Neighborhood regions must have a City or Locality parent.';
        } else if (parentType !== 'city' && parentType !== 'locality') {
            error = 'Neighborhood regions must have a City or Locality parent.';
        }
    }

    if (error) {
        errorElement.textContent = error;
        errorElement.classList.remove('hidden');
        return false;
    } else {
        errorElement.classList.add('hidden');
        return true;
    }
}

/**
 * Display search results dropdown
 * @param {Array} results - Mapbox geocoding results
 */
function displaySearchResults(results) {
    const resultsContainer = document.getElementById('search-results');

    if (!results || results.length === 0) {
        resultsContainer.innerHTML = '<div class="search-result search-result--empty">No results found</div>';
        resultsContainer.classList.remove('hidden');
        return;
    }

    resultsContainer.innerHTML = results.map((place, index) => `
        <div class="search-result" data-index="${index}">
            <strong>${place.text}</strong>
            <span class="search-result__context">${place.place_name.replace(place.text + ', ', '')}</span>
        </div>
    `).join('');

    // Bind click handlers
    resultsContainer.querySelectorAll('.search-result').forEach((el, index) => {
        el.addEventListener('click', () => selectPlace(results[index]));
    });

    resultsContainer.classList.remove('hidden');
}

/**
 * Select a place from search results
 * @param {Object} place - Mapbox geocoding result
 */
async function selectPlace(place) {
    const searchInput = document.getElementById('location-search');
    const resultsContainer = document.getElementById('search-results');
    const selectedContainer = document.getElementById('selected-location');
    const selectedName = document.getElementById('selected-location-name');
    const nameInput = document.getElementById('name');
    const form = document.getElementById('create-region-form');

    // Hide search results and show selected city name in input
    searchInput.value = place.text;
    resultsContainer.classList.add('hidden');

    // Disable UI while loading
    setFormDisabled(form, true);

    // Show loading state in selected location area
    selectedName.innerHTML = `<span class="spinner spinner--sm"></span> Loading boundary for ${escapeHtml(place.text)}...`;
    selectedContainer.classList.remove('hidden');
    updatePolygonFeedback(null, 'Fetching city boundary from OpenStreetMap...');

    // Get boundary from place (fetches from OSM if needed)
    let boundary = null;
    try {
        boundary = await getPlaceBoundary(place);
    } catch (error) {
        console.error('Error fetching boundary:', error);
    }

    // Re-enable UI
    setFormDisabled(form, false);

    if (!boundary) {
        toast.error('Could not get boundary for this location. Try drawing manually.');
        updatePolygonFeedback(false, 'Could not get boundary. Try drawing manually.');
        selectedContainer.classList.add('hidden');
        return;
    }

    selectedBoundary = boundary;

    // Update UI with selected place
    selectedName.textContent = place.place_name;
    selectedContainer.classList.remove('hidden');

    // Auto-fill name if empty
    if (!nameInput.value.trim()) {
        nameInput.value = place.text;
    }

    // Show boundary on map
    if (mapInstance) {
        // Clear any drawn polygons
        if (drawInstance) {
            clearDrawn(drawInstance);
        }

        addPreviewBoundary(mapInstance, boundary);
        fitToBounds(mapInstance, { type: 'Feature', geometry: boundary }, { maxZoom: 12 });
    }

    // Validate and update UI
    await validateSearchedBoundary();
}

/**
 * Validate the searched boundary
 */
async function validateSearchedBoundary() {
    if (!selectedBoundary) {
        updatePolygonFeedback(false, 'Search for a city or town above.');
        document.getElementById('submit-btn').disabled = true;
        return;
    }

    // Check US bounds (superusers can create anywhere)
    if (!isSuperuser() && !isGeometryWithinUS(selectedBoundary)) {
        updatePolygonFeedback(false, 'Community must be within the United States or its territories.');
        document.getElementById('submit-btn').disabled = true;
        return;
    }

    updatePolygonFeedback(null, 'Validating boundary...');

    try {
        const result = await validatePolygon(selectedBoundary);

        if (result.valid) {
            updatePolygonFeedback(true, 'Location boundary is valid.');
            document.getElementById('submit-btn').disabled = false;
        } else {
            updatePolygonFeedback(false, result.message || 'Boundary is outside your authorized area.');
            document.getElementById('submit-btn').disabled = true;
        }
    } catch (error) {
        // Allow submission if validation fails (backend will validate)
        updatePolygonFeedback(true, 'Location selected. It will be validated on submission.');
        document.getElementById('submit-btn').disabled = false;
    }
}

/**
 * Handle polygon creation (draw mode)
 * @param {Object} geometry - GeoJSON Polygon geometry
 */
async function handlePolygonCreate(geometry) {
    if (boundaryMode !== 'draw') return;
    selectedBoundary = geometry;
    await validateAndUpdateUI();
}

/**
 * Handle polygon update (draw mode)
 * @param {Object} geometry - GeoJSON Polygon geometry
 */
async function handlePolygonUpdate(geometry) {
    if (boundaryMode !== 'draw') return;
    selectedBoundary = geometry;
    await validateAndUpdateUI();
}

/**
 * Handle polygon deletion (draw mode)
 */
function handlePolygonDelete() {
    if (boundaryMode !== 'draw') return;
    selectedBoundary = null;
    updatePolygonFeedback(false, 'Use the map tools to draw your region boundary.');
    document.getElementById('submit-btn').disabled = true;
}

/**
 * Validate polygon and update UI (draw mode)
 */
async function validateAndUpdateUI() {
    if (!selectedBoundary) {
        updatePolygonFeedback(false, 'Use the map tools to draw your region boundary.');
        document.getElementById('submit-btn').disabled = true;
        return;
    }

    // Check US bounds (superusers can create anywhere)
    if (!isSuperuser() && !isGeometryWithinUS(selectedBoundary)) {
        updatePolygonFeedback(false, 'Community must be within the United States or its territories.');
        document.getElementById('submit-btn').disabled = true;
        return;
    }

    updatePolygonFeedback(null, 'Validating boundary...');

    try {
        const result = await validatePolygon(selectedBoundary);

        if (result.valid) {
            updatePolygonFeedback(true, 'Boundary is valid and within your authorized area.');
            document.getElementById('submit-btn').disabled = false;
        } else {
            updatePolygonFeedback(false, result.message || 'Boundary is outside your authorized area.');
            document.getElementById('submit-btn').disabled = true;
        }
    } catch (error) {
        updatePolygonFeedback(true, 'Boundary drawn. It will be validated on submission.');
        document.getElementById('submit-btn').disabled = false;
    }
}

/**
 * Update polygon feedback UI
 * @param {boolean|null} valid - Validation state (null = pending)
 * @param {string} message - Feedback message
 */
function updatePolygonFeedback(valid, message) {
    const feedback = document.getElementById('polygon-feedback');
    if (!feedback) return;

    feedback.textContent = message;

    if (valid === true) {
        feedback.style.color = 'var(--color-success)';
    } else if (valid === false) {
        feedback.style.color = 'var(--color-error)';
    } else {
        feedback.style.color = 'var(--color-gray-500)';
    }
}

/**
 * Bind form event handlers
 */
function bindFormHandlers() {
    const form = document.getElementById('create-region-form');
    if (!form) return;

    form.addEventListener('submit', handleSubmit);
}

/**
 * Handle form submission
 * @param {Event} event - Submit event
 */
async function handleSubmit(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('submit-btn');
    const errorElement = document.getElementById('form-error');

    // Get boundary based on mode
    let boundary = null;
    if (boundaryMode === 'draw' && drawInstance) {
        boundary = getDrawnPolygon(drawInstance);
    } else if (boundaryMode === 'search') {
        boundary = selectedBoundary;
    }

    if (!boundary) {
        errorElement.textContent = boundaryMode === 'draw'
            ? 'Please draw the region boundary on the map.'
            : 'Please search and select a location.';
        errorElement.classList.remove('hidden');
        return;
    }

    // Validate US bounds (superusers can create anywhere)
    if (!isSuperuser() && !isGeometryWithinUS(boundary)) {
        errorElement.textContent = 'Community must be within the United States or its territories.';
        errorElement.classList.remove('hidden');
        return;
    }

    // Validate type/parent combination
    if (!validateTypeParentCombination()) {
        return;
    }

    const data = {
        name: form.name.value.trim(),
        type: form.type.value,
        parent_region_id: form.parent_id.value || undefined,
        geometry: boundary,
    };

    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        const region = await createRegion(data);
        toast.success(`Community "${region.name}" created successfully!`);
        navigate(`/communities/${region.id}`);
    } catch (error) {
        let errorMessage = 'Failed to create community. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 400) {
                errorMessage = error.message || 'Invalid community data.';
            } else if (error.status === 403) {
                errorMessage = 'You are not authorized to create communities in this area.';
            } else if (error.message) {
                errorMessage = error.message;
            }
        }

        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
    } finally {
        submitButton.disabled = false;
        submitButton.classList.remove('btn--loading');
    }
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
 * Enable or disable all form inputs
 * @param {HTMLFormElement} form - Form element
 * @param {boolean} disabled - Whether to disable
 */
function setFormDisabled(form, disabled) {
    if (!form) return;

    const elements = form.querySelectorAll('input, select, button, textarea');
    elements.forEach(el => {
        el.disabled = disabled;
    });

    // Also disable mode toggle buttons
    const modeButtons = document.querySelectorAll('.boundary-mode-btn');
    modeButtons.forEach(btn => {
        btn.disabled = disabled;
    });

    // Add/remove loading class on form for styling
    if (disabled) {
        form.classList.add('form--loading');
        showLoadingOverlay('Loading city boundary...');
    } else {
        form.classList.remove('form--loading');
        hideLoadingOverlay();
    }
}

/**
 * Show loading overlay on the page
 * @param {string} message - Loading message to display
 */
function showLoadingOverlay(message) {
    // Remove existing overlay if any
    hideLoadingOverlay();

    const overlay = document.createElement('div');
    overlay.id = 'loading-overlay';
    overlay.className = 'loading-overlay';
    overlay.innerHTML = `
        <div class="loading-overlay__content">
            <div class="spinner spinner--lg"></div>
            <p class="loading-overlay__message">${escapeHtml(message)}</p>
        </div>
    `;

    document.body.appendChild(overlay);
}

/**
 * Hide the loading overlay
 */
function hideLoadingOverlay() {
    const overlay = document.getElementById('loading-overlay');
    if (overlay) {
        overlay.remove();
    }
}

/**
 * Cleanup when leaving the page
 */
export function cleanup() {
    if (mapInstance) {
        destroyMap();
        mapInstance = null;
        drawInstance = null;
    }
    adminBoundary = null;
    selectedBoundary = null;
    boundaryMode = 'draw';
    if (searchTimeout) {
        clearTimeout(searchTimeout);
        searchTimeout = null;
    }
}

export default { render, cleanup };
