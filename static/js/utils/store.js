/**
 * Simple state management store for the application.
 * Provides reactive state with subscriber notifications.
 */

class Store {
    constructor(initialState = {}) {
        this.state = initialState;
        this.subscribers = new Map();
        this.subscriberIdCounter = 0;
    }

    /**
     * Get current state or a specific key from state
     * @param {string} [key] - Optional key to get specific value
     * @returns {*} The state or state value
     */
    get(key) {
        if (key) {
            return this.state[key];
        }
        return { ...this.state };
    }

    /**
     * Update state with partial state object
     * @param {Object} partialState - Partial state to merge
     */
    set(partialState) {
        const previousState = { ...this.state };
        this.state = { ...this.state, ...partialState };
        this.notifySubscribers(previousState);
    }

    /**
     * Subscribe to state changes
     * @param {Function} callback - Function to call on state change
     * @param {string[]} [keys] - Optional array of keys to watch
     * @returns {Function} Unsubscribe function
     */
    subscribe(callback, keys = null) {
        const subscriberId = ++this.subscriberIdCounter;
        this.subscribers.set(subscriberId, { callback, keys });

        return () => {
            this.subscribers.delete(subscriberId);
        };
    }

    /**
     * Notify subscribers of state changes
     * @param {Object} previousState - Previous state before update
     */
    notifySubscribers(previousState) {
        this.subscribers.forEach(({ callback, keys }) => {
            if (keys) {
                // Only notify if watched keys changed
                const hasChanges = keys.some(
                    key => this.state[key] !== previousState[key]
                );
                if (hasChanges) {
                    callback(this.state, previousState);
                }
            } else {
                callback(this.state, previousState);
            }
        });
    }

    /**
     * Reset state to initial values
     * @param {Object} [newInitialState] - Optional new initial state
     */
    reset(newInitialState = {}) {
        const previousState = { ...this.state };
        this.state = newInitialState;
        this.notifySubscribers(previousState);
    }
}

// Application store singleton with initial state
export const store = new Store({
    // Auth state
    user: null,
    isAuthenticated: false,
    authLoading: true,

    // UI state
    currentRoute: null,
    sidebarOpen: false,

    // Data cache
    regions: [],
    currentRegion: null,
    signalGroups: [],

    // Map state
    mapCenter: [-98.5795, 39.8283], // Center of US
    mapZoom: 4,
    selectedRegionId: null,
});

// Auth-specific helpers
export function setUser(user) {
    store.set({
        user,
        isAuthenticated: !!user,
        authLoading: false,
    });
}

export function clearUser() {
    store.set({
        user: null,
        isAuthenticated: false,
        authLoading: false,
    });
}

export function isAuthenticated() {
    return store.get('isAuthenticated');
}

export function getUser() {
    return store.get('user');
}

/**
 * Check if user is a superuser (global admin, created directly in database)
 * Superusers have access to all regions and can grant vouch verification
 * @returns {boolean}
 */
export function isSuperuser() {
    const user = store.get('user');
    return user?.is_superuser ?? false;
}

/**
 * Check if user is postcard verified
 * @returns {boolean}
 */
export function isPostcardVerified() {
    const user = store.get('user');
    return user?.postcard_verified ?? false;
}

/**
 * Check if user is vouch verified (has 2+ vouches)
 * @returns {boolean}
 */
export function isVouchVerified() {
    const user = store.get('user');
    return user?.vouch_verified ?? false;
}

/**
 * Check if user has any verification (read-only access to groups)
 * Either postcard OR vouch verification grants read-only access
 * Superusers always have read access
 * @returns {boolean}
 */
export function hasReadAccess() {
    return isSuperuser() || isPostcardVerified() || isVouchVerified();
}

/**
 * Check if user is a full admin
 * Requires BOTH postcard AND vouch verification, OR superuser status
 * @returns {boolean}
 */
export function isAdmin() {
    return isSuperuser() || (isPostcardVerified() && isVouchVerified());
}

/**
 * Check if user is blocked
 * @returns {boolean}
 */
export function isBlocked() {
    const user = store.get('user');
    return user?.is_blocked ?? false;
}

/**
 * Get user verification status for display
 * @returns {Object} Status object with label, description, and level
 */
export function getVerificationStatus() {
    const blocked = isBlocked();
    const superuser = isSuperuser();
    const postcardVerified = isPostcardVerified();
    const vouchVerified = isVouchVerified();

    // Blocked status takes precedence over all others
    if (blocked) {
        return {
            level: 'blocked',
            label: 'Restricted',
            description: 'Your account access has been restricted by moderators',
            canAdmin: false,
            canRead: false,
            isSuperuser: false,
        };
    }

    if (superuser) {
        return {
            level: 'superuser',
            label: 'Superuser',
            description: 'Global admin access to all regions',
            canAdmin: true,
            canRead: true,
            isSuperuser: true,
        };
    } else if (postcardVerified && vouchVerified) {
        return {
            level: 'admin',
            label: 'Admin',
            description: 'Full admin access - postcard and vouch verified',
            canAdmin: true,
            canRead: true,
            isSuperuser: false,
        };
    } else if (postcardVerified) {
        return {
            level: 'postcard',
            label: 'Postcard Verified',
            description: 'Read-only access - needs vouch verification for admin rights',
            canAdmin: false,
            canRead: true,
            isSuperuser: false,
        };
    } else if (vouchVerified) {
        return {
            level: 'vouched',
            label: 'Vouch Verified',
            description: 'Read-only access - needs postcard verification for admin rights',
            canAdmin: false,
            canRead: true,
            isSuperuser: false,
        };
    } else {
        return {
            level: 'unverified',
            label: 'Unverified',
            description: 'Verify your address or get vouched to access groups',
            canAdmin: false,
            canRead: false,
            isSuperuser: false,
        };
    }
}

/**
 * @deprecated Use isAdmin() or hasReadAccess() instead
 * Kept for backwards compatibility
 */
export function getUserTier() {
    const user = store.get('user');
    return user?.tier ?? 0;
}

// Region helpers
export function setRegions(regions) {
    store.set({ regions });
}

export function setCurrentRegion(region) {
    store.set({ currentRegion: region });
}

// Signal group helpers
export function setSignalGroups(groups) {
    store.set({ signalGroups: groups });
}

export default store;
