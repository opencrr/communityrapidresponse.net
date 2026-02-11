/**
 * Vouch Management Page
 * Handles requesting vouch verification and vouching for others.
 */

import {
    requestVouchVerification,
    vouchForUser,
    getVerificationStatus,
    getVouchStatus,
    getPendingVouchRequests,
} from '../api/verification.js?v=c49aed9';
import { getUser, isPostcardVerified, isVouchVerified, isAdmin } from '../utils/store.js?v=c49aed9';
import { ApiError } from '../api/client.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';
import modal from '../components/modal.js?v=c49aed9';
import { navigate } from '../app.js?v=c49aed9';

const US_STATES = [
    { code: 'AL', name: 'Alabama' }, { code: 'AK', name: 'Alaska' },
    { code: 'AZ', name: 'Arizona' }, { code: 'AR', name: 'Arkansas' },
    { code: 'CA', name: 'California' }, { code: 'CO', name: 'Colorado' },
    { code: 'CT', name: 'Connecticut' }, { code: 'DE', name: 'Delaware' },
    { code: 'FL', name: 'Florida' }, { code: 'GA', name: 'Georgia' },
    { code: 'HI', name: 'Hawaii' }, { code: 'ID', name: 'Idaho' },
    { code: 'IL', name: 'Illinois' }, { code: 'IN', name: 'Indiana' },
    { code: 'IA', name: 'Iowa' }, { code: 'KS', name: 'Kansas' },
    { code: 'KY', name: 'Kentucky' }, { code: 'LA', name: 'Louisiana' },
    { code: 'ME', name: 'Maine' }, { code: 'MD', name: 'Maryland' },
    { code: 'MA', name: 'Massachusetts' }, { code: 'MI', name: 'Michigan' },
    { code: 'MN', name: 'Minnesota' }, { code: 'MS', name: 'Mississippi' },
    { code: 'MO', name: 'Missouri' }, { code: 'MT', name: 'Montana' },
    { code: 'NE', name: 'Nebraska' }, { code: 'NV', name: 'Nevada' },
    { code: 'NH', name: 'New Hampshire' }, { code: 'NJ', name: 'New Jersey' },
    { code: 'NM', name: 'New Mexico' }, { code: 'NY', name: 'New York' },
    { code: 'NC', name: 'North Carolina' }, { code: 'ND', name: 'North Dakota' },
    { code: 'OH', name: 'Ohio' }, { code: 'OK', name: 'Oklahoma' },
    { code: 'OR', name: 'Oregon' }, { code: 'PA', name: 'Pennsylvania' },
    { code: 'RI', name: 'Rhode Island' }, { code: 'SC', name: 'South Carolina' },
    { code: 'SD', name: 'South Dakota' }, { code: 'TN', name: 'Tennessee' },
    { code: 'TX', name: 'Texas' }, { code: 'UT', name: 'Utah' },
    { code: 'VT', name: 'Vermont' }, { code: 'VA', name: 'Virginia' },
    { code: 'WA', name: 'Washington' }, { code: 'WV', name: 'West Virginia' },
    { code: 'WI', name: 'Wisconsin' }, { code: 'WY', name: 'Wyoming' },
    { code: 'DC', name: 'District of Columbia' },
];

let verificationStatus = null;

/**
 * Render the vouch management page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    const user = getUser();
    const postcardVerified = isPostcardVerified();
    const vouchVerified = isVouchVerified();
    const userIsAdmin = isAdmin();

    // Fetch current verification status
    try {
        verificationStatus = await getVerificationStatus();
    } catch (error) {
        console.error('Failed to get verification status:', error);
        verificationStatus = {};
    }

    // Bootstrap mode info - region may have fewer than 3 full admins
    // In bootstrap mode: postcard-only users can vouch, and 3 vouches are required instead of 2
    const bootstrapMode = verificationStatus?.bootstrap_mode || false;

    // Calculate progress for progress indicator
    // Use dynamic vouch requirement based on bootstrap mode
    const vouchCount = verificationStatus?.vouch_count || 0;
    const vouchesNeeded = bootstrapMode ? 3 : 2;
    const vouchProgress = Math.min((vouchCount / vouchesNeeded) * 100, 100);

    // In bootstrap mode, postcard-only users can vouch (not just full admins)
    const canVouch = userIsAdmin || (bootstrapMode && postcardVerified);

    container.innerHTML = `
        <div class="page">
            <div class="page__container" style="max-width: 800px;">
                <div class="page__header text-center">
                    <h1 class="page__title">Vouch Verification</h1>
                    <p class="page__subtitle">
                        Get verified by trusted community members or vouch for others.
                    </p>
                </div>

                <!-- Verification Status Overview -->
                <div class="card" style="margin-bottom: var(--space-6);">
                    <div class="card__header">
                        <h2 class="card__title">Your Verification Status</h2>
                    </div>
                    <div class="card__body">
                        <div class="verification-overview">
                            <div class="verification-item ${postcardVerified ? 'verification-item--complete' : ''}">
                                <div class="verification-item__icon">${postcardVerified ? '&#x2713;' : '&#x1F4EC;'}</div>
                                <div class="verification-item__content">
                                    <strong>Postcard Verification</strong>
                                    <p>${postcardVerified ? 'Complete' : 'Not verified'}</p>
                                </div>
                                ${!postcardVerified ? `<a href="/verify" class="btn btn--sm btn--secondary" data-link>Verify</a>` : ''}
                            </div>
                            <div class="verification-item ${vouchVerified ? 'verification-item--complete' : ''}">
                                <div class="verification-item__icon">${vouchVerified ? '&#x2713;' : '&#x1F91D;'}</div>
                                <div class="verification-item__content">
                                    <strong>Vouch Verification</strong>
                                    <p>${vouchVerified ? 'Complete' : `${vouchCount} of ${vouchesNeeded} vouches received`}</p>
                                </div>
                            </div>
                        </div>

                        ${!vouchVerified ? `
                            <!-- Progress Bar -->
                            <div class="vouch-progress" style="margin-top: var(--space-4);">
                                <div class="vouch-progress__label">
                                    <span>Vouch Progress</span>
                                    <span>${vouchCount} / ${vouchesNeeded}</span>
                                </div>
                                <div class="vouch-progress__bar">
                                    <div class="vouch-progress__fill" style="width: ${vouchProgress}%"></div>
                                </div>
                                ${vouchCount > 0 ? `
                                    <p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-primary-600);">
                                        ${vouchCount === 1 ? "You're halfway there! Just 1 more vouch needed." : "Almost there!"}
                                    </p>
                                ` : ''}
                            </div>
                        ` : ''}

                        ${userIsAdmin ? `
                            <div class="privacy-notice" style="margin-top: var(--space-4); background: #dcfce7;">
                                <div class="privacy-notice__icon">&#x2713;</div>
                                <div class="privacy-notice__text">
                                    <strong>Full Admin Access</strong>
                                    You have both postcard and vouch verification. You can create communities, manage Signal groups, and vouch for others.
                                </div>
                            </div>
                        ` : postcardVerified && bootstrapMode ? `
                            <div class="privacy-notice" style="margin-top: var(--space-4); background: #dbeafe;">
                                <div class="privacy-notice__icon">&#x1F680;</div>
                                <div class="privacy-notice__text">
                                    <strong>Bootstrap Mode Active</strong>
                                    This community has fewer than 3 full admins. As a postcard-verified user, you can help bootstrap the community by vouching for others.
                                    Requires 3 vouches (instead of 2) and a 1-hour cooldown between vouches.
                                </div>
                            </div>
                        ` : `
                            <div class="privacy-notice" style="margin-top: var(--space-4); background: #fef3c7;">
                                <div class="privacy-notice__icon">&#x1F511;</div>
                                <div class="privacy-notice__text">
                                    <strong>Want admin access?</strong>
                                    Complete both postcard and vouch verification to become a community admin.
                                </div>
                            </div>
                        `}
                    </div>
                </div>

                <div class="vouch-grid">
                    ${!vouchVerified ? renderRequestVouchSection(bootstrapMode, vouchesNeeded, postcardVerified) : ''}
                    ${canVouch ? renderGiveVouchSection(bootstrapMode) : ''}
                </div>

                ${!canVouch ? renderCoordinationInstructions(bootstrapMode, vouchesNeeded) : ''}
            </div>
        </div>
    `;

    // Bind form handlers
    if (!vouchVerified) {
        bindRequestVouchHandlers();
    }
    if (canVouch) {
        bindGiveVouchHandlers(bootstrapMode);
    }
}

/**
 * Render the request vouch section
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 * @param {number} vouchesNeeded - Number of vouches required
 * @param {boolean} postcardVerified - Whether the user is postcard verified
 * @returns {string} HTML string
 */
function renderRequestVouchSection(bootstrapMode = false, vouchesNeeded = 2, postcardVerified = false) {
    const stateOptions = US_STATES.map(s =>
        `<option value="${s.name}">${s.name}</option>`
    ).join('');

    const hasPendingRequest = verificationStatus?.pending_vouch_region;
    const autoEligible = verificationStatus?.auto_eligible || false;
    const pendingBootstrapMode = verificationStatus?.bootstrap_mode || bootstrapMode;
    const pendingVouchesRequired = verificationStatus?.vouches_required || vouchesNeeded;

    // Postcard-verified users are automatically eligible for vouches in their region
    // No need to fill out the city/state form
    if (hasPendingRequest) {
        const isAutoEligible = autoEligible || postcardVerified;
        return `
            <div class="card">
                <div class="card__header">
                    <h2 class="card__title">Waiting for Vouches</h2>
                </div>
                <div class="card__body">
                    <div style="text-align: center; padding: var(--space-4);">
                        <div style="font-size: 48px; margin-bottom: var(--space-4);">&#x23F3;</div>
                        <h3 style="font-size: var(--font-size-lg); margin-bottom: var(--space-2);">
                            ${isAutoEligible ? 'Ready for Vouches' : 'Request Submitted'}
                        </h3>
                        <p style="color: var(--color-gray-600); margin-bottom: var(--space-4);">
                            ${isAutoEligible
                                ? `Since you're postcard verified in <strong>${escapeHtml(verificationStatus.pending_vouch_region.name)}</strong>, you're automatically eligible for vouches.`
                                : `You've requested vouch verification for <strong>${escapeHtml(verificationStatus.pending_vouch_region.name)}</strong>.`}
                            Connect with verified community members to receive vouches.
                        </p>
                        <p style="font-size: var(--font-size-sm); color: var(--color-gray-500);">
                            ${verificationStatus.vouch_count || 0} of ${pendingVouchesRequired} vouches received
                        </p>
                        ${pendingBootstrapMode ? `
                            <div class="privacy-notice" style="margin-top: var(--space-4); background: #dbeafe; text-align: left;">
                                <div class="privacy-notice__icon">&#x1F680;</div>
                                <div class="privacy-notice__text">
                                    <strong>Bootstrap Mode</strong>
                                    This community is building its initial admin team. ${pendingVouchesRequired} vouches are required (instead of the usual 2).
                                </div>
                            </div>
                        ` : ''}
                    </div>
                </div>
            </div>
        `;
    }

    // Only show the request form for non-postcard-verified users
    return `
        <div class="card">
            <div class="card__header">
                <h2 class="card__title">Request Vouch Verification</h2>
            </div>
            <div class="card__body">
                ${!postcardVerified ? `
                    <div class="privacy-notice" style="margin-bottom: var(--space-4); background: #fef3c7;">
                        <div class="privacy-notice__icon">&#x26A0;</div>
                        <div class="privacy-notice__text">
                            <strong>Recommended: Postcard verify first</strong>
                            New communities in bootstrap mode require postcard verification before you can receive vouches.
                            <a href="/verify" data-link style="color: var(--color-primary-600);">Start postcard verification</a> for the fastest path to full access.
                        </div>
                    </div>
                ` : ''}

                <p style="margin-bottom: var(--space-4); color: var(--color-gray-600);">
                    Enter the city where you want to be vouched. You'll need verified community members from that area to vouch for you.
                </p>

                <form id="request-vouch-form">
                    <div class="form-group">
                        <label for="vouch-city" class="form-label form-label--required">City</label>
                        <input
                            type="text"
                            id="vouch-city"
                            name="city"
                            class="form-input"
                            required
                            placeholder="e.g., Portland"
                            autocomplete="address-level2"
                        >
                    </div>

                    <div class="form-group">
                        <label for="vouch-state" class="form-label form-label--required">State</label>
                        <select id="vouch-state" name="state" class="form-input" required>
                            <option value="">Select state</option>
                            ${stateOptions}
                        </select>
                    </div>

                    <div id="request-vouch-error" class="form-error hidden"></div>

                    <button type="submit" class="btn btn--primary btn--block" id="request-vouch-btn">
                        Request Vouch Verification
                    </button>
                </form>

                <p style="margin-top: var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-align: center;">
                    The city must already exist in our system. If it doesn't, someone needs to verify via postcard first.
                </p>
            </div>
        </div>
    `;
}

/**
 * Render the give vouch section (admin or postcard-verified user in bootstrap mode)
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 * @returns {string} HTML string
 */
function renderGiveVouchSection(bootstrapMode = false) {
    return `
        <div class="card">
            <div class="card__header">
                <h2 class="card__title">Vouch for Someone</h2>
            </div>
            <div class="card__body">
                <p style="margin-bottom: var(--space-4); color: var(--color-gray-600);">
                    Users in your communities waiting for verification
                </p>

                ${bootstrapMode ? `
                    <div class="privacy-notice" style="margin-bottom: var(--space-4); background: #dbeafe;">
                        <div class="privacy-notice__icon">&#x1F680;</div>
                        <div class="privacy-notice__text">
                            <strong>Bootstrap Mode</strong>
                            You can vouch because this community is building its initial admin team.
                            There's a 1-hour cooldown between vouches to prevent abuse.
                        </div>
                    </div>
                ` : ''}

                <div id="vouch-pending-list">
                    <div class="loading">
                        <div class="spinner"></div>
                    </div>
                </div>

                <!-- Manual entry fallback -->
                <details class="manual-vouch-details">
                    <summary>Can't find someone? Enter email manually</summary>
                    <form id="give-vouch-form" class="form" style="margin-top: var(--space-4);">
                        <div class="form-group">
                            <label for="vouchee-email" class="form-label form-label--required">Email or Username</label>
                            <input
                                type="text"
                                id="vouchee-email"
                                name="email"
                                class="form-input"
                                required
                                placeholder="Enter the person's email or username"
                            >
                            <p class="form-hint">Enter the email or username of the person you want to vouch for.</p>
                        </div>

                        <div id="give-vouch-error" class="form-error hidden"></div>

                        <button type="submit" class="btn btn--primary btn--block" id="give-vouch-btn">
                            Vouch for This Person
                        </button>
                    </form>
                </details>

                <div class="vouch-requirements" style="margin-top: var(--space-4); padding: var(--space-3); background: var(--color-gray-50); border-radius: var(--radius-md);">
                    <h4 style="font-size: var(--font-size-sm); font-weight: 600; margin-bottom: var(--space-2);">Requirements</h4>
                    <ul style="font-size: var(--font-size-sm); color: var(--color-gray-600); margin: 0; padding-left: var(--space-4);">
                        <li>The person must be in your community (postcard verified or have a pending vouch request)</li>
                        <li>You can vouch for up to 10 people per month</li>
                        <li>Circular vouching patterns are not allowed</li>
                        ${bootstrapMode ? '<li><strong>Bootstrap mode:</strong> 1-hour cooldown between vouches</li>' : ''}
                    </ul>
                </div>
            </div>
        </div>
    `;
}

/**
 * Render out-of-band coordination instructions
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 * @param {number} vouchesNeeded - Number of vouches required
 * @returns {string} HTML string
 */
function renderCoordinationInstructions(bootstrapMode = false, vouchesNeeded = 2) {
    return `
        <div class="card" style="margin-top: var(--space-6);">
            <div class="card__header">
                <h2 class="card__title">How Vouch Verification Works</h2>
            </div>
            <div class="card__body">
                <div class="instructions-grid">
                    <div class="instruction-step">
                        <div class="instruction-step__number">1</div>
                        <div class="instruction-step__content">
                            <h4>Request Verification</h4>
                            <p>Enter your city above to request vouch verification. This lets verified members know you're looking to join.</p>
                        </div>
                    </div>

                    <div class="instruction-step">
                        <div class="instruction-step__number">2</div>
                        <div class="instruction-step__content">
                            <h4>Connect with Neighbors</h4>
                            <p>Meet verified community members in person, through local events, or via existing Signal groups. Building real connections is key.</p>
                        </div>
                    </div>

                    <div class="instruction-step">
                        <div class="instruction-step__number">3</div>
                        <div class="instruction-step__content">
                            <h4>Get Vouched</h4>
                            <p>Once ${vouchesNeeded} verified members vouch for you, you'll gain access to view Signal group invite links.${bootstrapMode ? ' (Bootstrap mode requires 3 vouches)' : ''}</p>
                        </div>
                    </div>

                    <div class="instruction-step">
                        <div class="instruction-step__number">4</div>
                        <div class="instruction-step__content">
                            <h4>Become an Admin</h4>
                            <p>Complete both postcard and vouch verification to become a full admin and help build your community.</p>
                        </div>
                    </div>
                </div>

                ${bootstrapMode ? `
                    <div class="privacy-notice" style="margin-top: var(--space-4); background: #dbeafe;">
                        <div class="privacy-notice__icon">&#x1F680;</div>
                        <div class="privacy-notice__text">
                            <strong>Bootstrap Mode Active</strong>
                            This community is still building its initial admin team. During bootstrap mode, postcard-verified users can vouch for others,
                            but 3 vouches are required (instead of 2) and there's a 1-hour cooldown between vouches.
                        </div>
                    </div>
                ` : ''}

                <div class="privacy-notice" style="margin-top: var(--space-4);">
                    <div class="privacy-notice__icon">&#x1F6E1;</div>
                    <div class="privacy-notice__text">
                        <strong>Why in-person connections?</strong>
                        Vouch verification helps ensure that community members are real neighbors who know each other. This creates a web of trust that protects everyone.
                    </div>
                </div>
            </div>
        </div>
    `;
}

/**
 * Bind event handlers for request vouch form
 */
function bindRequestVouchHandlers() {
    const form = document.getElementById('request-vouch-form');
    if (!form) return;

    form.addEventListener('submit', handleRequestVouchSubmit);
}

/**
 * Bind event handlers for give vouch form
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 */
function bindGiveVouchHandlers(bootstrapMode = false) {
    const form = document.getElementById('give-vouch-form');
    if (form) {
        form.addEventListener('submit', (e) => handleGiveVouchSubmit(e, bootstrapMode));
    }

    // Load pending vouch requests
    loadVouchPendingList(bootstrapMode);
}

/**
 * Handle request vouch form submission
 * @param {Event} event - Submit event
 */
async function handleRequestVouchSubmit(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('request-vouch-btn');
    const errorElement = document.getElementById('request-vouch-error');

    const city = form.city.value.trim();
    const state = form.state.value;

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        const result = await requestVouchVerification(city, state);
        toast.success(`Vouch request submitted for ${result.region_name || city}!`);

        // Refresh the page to show pending status
        const container = document.getElementById('main');
        if (container) {
            render(container);
        }
    } catch (error) {
        let errorMessage = 'Failed to submit vouch request. Please try again.';
        let showPostcardLink = false;

        if (error instanceof ApiError) {
            if (error.status === 404) {
                errorMessage = 'City not found. The city must already exist in our system. Try a different city or verify via postcard first.';
            } else if (error.status === 403 && error.data?.error === 'bootstrap_requires_postcard') {
                errorMessage = 'This community is in bootstrap mode (building its initial admin team). Please complete postcard verification first - you\'ll then be automatically eligible for vouches.';
                showPostcardLink = true;
            } else if (error.status === 409) {
                errorMessage = error.message || 'You already have a pending or verified membership in this region.';
            } else if (error.message) {
                errorMessage = error.message;
            }
        }

        errorElement.innerHTML = showPostcardLink
            ? `${errorMessage} <a href="/verify" data-link style="color: var(--color-primary-600); text-decoration: underline;">Start postcard verification</a>`
            : errorMessage;
        errorElement.classList.remove('hidden');
    } finally {
        submitButton.disabled = false;
        submitButton.classList.remove('btn--loading');
    }
}

/**
 * Handle give vouch form submission
 * @param {Event} event - Submit event
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 */
async function handleGiveVouchSubmit(event, bootstrapMode = false) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('give-vouch-btn');
    const errorElement = document.getElementById('give-vouch-error');

    const emailOrUsername = form.email.value.trim();

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        // The backend accepts email or username in the user_id field
        // and will look up the actual user
        await vouchForUser(emailOrUsername);
        toast.success('Successfully vouched for this person!');

        // Clear the form
        form.reset();
    } catch (error) {
        let errorMessage = 'Failed to vouch for this person. Please try again.';

        if (error instanceof ApiError) {
            switch (error.status) {
                case 400:
                    // Parse specific validation errors
                    if (error.message?.includes('pending')) {
                        errorMessage = 'This person hasn\'t requested vouch verification in your community yet.';
                    } else if (error.message?.includes('already vouched')) {
                        errorMessage = 'You have already vouched for this person.';
                    } else if (error.message?.includes('yourself')) {
                        errorMessage = 'You cannot vouch for yourself.';
                    } else if (error.message?.includes('circular')) {
                        errorMessage = 'Circular vouching patterns are not allowed.';
                    } else {
                        errorMessage = error.message || 'Invalid vouch request.';
                    }
                    break;
                case 403:
                    if (error.message?.includes('30 days')) {
                        errorMessage = 'Your account must be at least 30 days old to vouch for others.';
                    } else if (error.message?.includes('limit')) {
                        errorMessage = 'You have reached your monthly vouch limit (10 per month).';
                    } else if (error.message?.includes('verified')) {
                        errorMessage = 'You must be fully verified (both postcard and vouch) to vouch for others.';
                    } else {
                        errorMessage = error.message || 'You are not allowed to vouch for others.';
                    }
                    break;
                case 404:
                    errorMessage = 'User not found. Please check the email or username.';
                    break;
                case 429:
                    // Handle cooldown error from bootstrap mode
                    if (error.data?.cooldown_end) {
                        const cooldownEnd = new Date(error.data.cooldown_end);
                        const minutesLeft = Math.ceil((cooldownEnd - new Date()) / 60000);
                        errorMessage = `Bootstrap mode cooldown: please wait ${minutesLeft} minutes before vouching again.`;
                    } else if (error.message?.includes('cooldown')) {
                        errorMessage = error.message;
                    } else {
                        errorMessage = 'You have reached a rate limit. Please wait before trying again.';
                    }
                    break;
                default:
                    if (error.message) {
                        errorMessage = error.message;
                    }
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
 * Load pending vouch requests list for vouch page
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 */
async function loadVouchPendingList(bootstrapMode = false) {
    const content = document.getElementById('vouch-pending-list');
    if (!content) return;

    try {
        const pending = await getPendingVouchRequests();
        if (pending.length === 0) {
            content.innerHTML = `
                <div class="empty-state empty-state--compact">
                    <p>No one is waiting for vouches in your communities right now.</p>
                </div>
            `;
            return;
        }
        content.innerHTML = renderVouchPendingCards(pending, bootstrapMode);
        bindVouchCardButtons(bootstrapMode);
    } catch (error) {
        console.error('Failed to load pending vouch requests:', error);
        content.innerHTML = `<p class="form-error">Failed to load pending requests</p>`;
    }
}

/**
 * Render pending vouch cards
 * @param {Array} pending - Array of pending vouch requests
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 * @returns {string} HTML string
 */
function renderVouchPendingCards(pending, bootstrapMode = false) {
    // In bootstrap mode, 3 vouches are required; normally 2
    const vouchesRequired = bootstrapMode ? 3 : 2;

    return `
        <div class="vouch-pending-cards">
            ${pending.map(req => `
                <div class="vouch-card">
                    <div class="vouch-card__header">
                        <strong>${escapeHtml(req.username)}</strong>
                        <span class="badge badge--region">${escapeHtml(req.region_name)}</span>
                    </div>
                    <div class="vouch-card__meta">
                        <span>${escapeHtml(req.email)}</span>
                        <span class="vouch-progress-text">${req.vouch_count}/${vouchesRequired} vouches</span>
                    </div>
                    <div class="vouch-card__actions">
                        <button class="btn btn--primary vouch-card-btn"
                                data-user-id="${req.user_id}"
                                data-username="${escapeHtml(req.username)}">
                            Vouch for ${escapeHtml(req.username)}
                        </button>
                    </div>
                </div>
            `).join('')}
        </div>
    `;
}

/**
 * Bind click handlers to vouch card buttons
 * @param {boolean} bootstrapMode - Whether the region is in bootstrap mode
 */
function bindVouchCardButtons(bootstrapMode = false) {
    document.querySelectorAll('.vouch-card-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
            const userId = btn.dataset.userId;
            const username = btn.dataset.username;

            let confirmMessage = 'This confirms you know this person and they live in your community.';
            if (bootstrapMode) {
                confirmMessage += ' Note: There is a 1-hour cooldown between vouches in bootstrap mode.';
            }

            const confirmed = await modal.confirm({
                title: `Vouch for ${username}?`,
                message: confirmMessage,
            });

            if (confirmed) {
                try {
                    btn.disabled = true;
                    btn.classList.add('btn--loading');
                    await vouchForUser(userId);
                    toast.success(`Vouched for ${username}!`);
                    await loadVouchPendingList(bootstrapMode); // Refresh list
                } catch (error) {
                    // Handle cooldown error specifically
                    let errorMessage = error.message || 'Failed to vouch';
                    if (error.status === 429 && error.data?.cooldown_end) {
                        const cooldownEnd = new Date(error.data.cooldown_end);
                        const minutesLeft = Math.ceil((cooldownEnd - new Date()) / 60000);
                        errorMessage = `Bootstrap cooldown: wait ${minutesLeft} minutes`;
                    }
                    toast.error(errorMessage);
                    btn.disabled = false;
                    btn.classList.remove('btn--loading');
                }
            }
        });
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

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
