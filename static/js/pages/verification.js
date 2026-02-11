/**
 * Verification flow page
 * Handles postcard address verification and code entry.
 */

import {
    requestPostcardVerification,
    verifyPostcardCode,
    getVerificationStatus,
    cancelVerificationRequest,
    resendPostcard,
} from '../api/verification.js';
import { getRegions } from '../api/regions.js';
import { ApiError } from '../api/client.js';
import { getCurrentUser } from '../api/auth.js';
import toast from '../components/toast.js';
import modal from '../components/modal.js';
import { navigate } from '../app.js';

let availableRegions = [];

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

/**
 * Render the verification page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    // Check verification status first
    let status = null;
    try {
        status = await getVerificationStatus();
    } catch (error) {
        console.error('Failed to get verification status:', error);
    }

    // Fetch available regions
    try {
        availableRegions = await getRegions();
    } catch (error) {
        console.error('Failed to fetch regions:', error);
        availableRegions = [];
    }

    const hasPendingRequest = status?.pending_request;
    const currentStep = hasPendingRequest ? 2 : 1;

    container.innerHTML = `
        <div class="page">
            <div class="page__container" style="max-width: 600px;">
                <div class="page__header text-center">
                    <h1 class="page__title">Verify Your Address</h1>
                    <p class="page__subtitle">
                        Prove your residency to access local Signal groups. Combined with vouch verification, you can become a community admin.
                    </p>
                </div>

                <!-- Progress Steps -->
                <div class="verification-steps">
                    <div class="verification-step ${currentStep >= 1 ? 'verification-step--active' : ''} ${currentStep > 1 ? 'verification-step--completed' : ''}">
                        <div class="verification-step__number">${currentStep > 1 ? '&#10003;' : '1'}</div>
                        <div class="verification-step__label">Enter Address</div>
                    </div>
                    <div class="verification-step ${currentStep >= 2 ? 'verification-step--active' : ''}">
                        <div class="verification-step__number">2</div>
                        <div class="verification-step__label">Enter Code</div>
                    </div>
                </div>

                <div class="card">
                    <div class="card__body">
                        ${hasPendingRequest ? renderCodeEntryForm(status?.postcard_ref) : renderAddressForm()}
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind form handlers
    if (hasPendingRequest) {
        bindCodeFormHandlers();
    } else {
        bindAddressFormHandlers();
    }
}

/**
 * Render the address entry form
 * @returns {string} HTML string
 */
function renderAddressForm() {
    const stateOptions = US_STATES.map(s =>
        `<option value="${s.code}">${s.name}</option>`
    ).join('');

    const regionOptions = availableRegions.map(r =>
        `<option value="${r.id}">${r.name} (${r.type})</option>`
    ).join('');

    return `
        <div class="privacy-notice">
            <div class="privacy-notice__icon">&#x1F512;</div>
            <div class="privacy-notice__text">
                <strong>Your address is never stored</strong>
                Your address is used only to send the verification postcard and is immediately discarded.
                We never store your street address in our database.
            </div>
        </div>

        <form id="address-form">
            <div class="form-group">
                <label for="region_id" class="form-label">Community (optional)</label>
                <select id="region_id" name="region_id" class="form-input">
                    <option value="">Auto-detect from address</option>
                    ${regionOptions}
                </select>
                <p class="form-hint">Leave blank to auto-detect, or select an existing community.</p>
            </div>

            <div class="form-group">
                <label for="street_address" class="form-label form-label--required">Street Address</label>
                <input
                    type="text"
                    id="street_address"
                    name="street_address"
                    class="form-input"
                    required
                    placeholder="123 Main St"
                    autocomplete="street-address"
                >
            </div>

            <div class="form-group">
                <label for="unit" class="form-label">Unit/Apt (optional)</label>
                <input
                    type="text"
                    id="unit"
                    name="unit"
                    class="form-input"
                    placeholder="Apt 4B"
                >
            </div>

            <div class="form-group">
                <label for="city" class="form-label form-label--required">City</label>
                <input
                    type="text"
                    id="city"
                    name="city"
                    class="form-input"
                    required
                    autocomplete="address-level2"
                >
            </div>

            <div style="display: grid; grid-template-columns: 1fr 1fr; gap: var(--space-4);">
                <div class="form-group">
                    <label for="state" class="form-label form-label--required">State</label>
                    <select id="state" name="state" class="form-input" required>
                        <option value="">Select state</option>
                        ${stateOptions}
                    </select>
                </div>

                <div class="form-group">
                    <label for="zip_code" class="form-label form-label--required">ZIP Code</label>
                    <input
                        type="text"
                        id="zip_code"
                        name="zip_code"
                        class="form-input"
                        required
                        pattern="[0-9]{5}(-[0-9]{4})?"
                        placeholder="12345"
                        autocomplete="postal-code"
                    >
                </div>
            </div>

            <div id="address-error" class="form-error hidden"></div>

            <button type="submit" class="btn btn--primary btn--block" id="submit-btn">
                Send Verification Postcard
            </button>
        </form>

        <p style="margin-top: var(--space-4); font-size: var(--font-size-sm); color: var(--color-gray-500); text-align: center;">
            PO Boxes and commercial mail receiving agencies (CMRA) are not accepted.
        </p>
    `;
}

/**
 * Render the code entry form
 * @param {string|null} postcardRef - Reference code printed on the postcard exterior
 * @returns {string} HTML string
 */
function renderCodeEntryForm(postcardRef) {
    const refMessage = postcardRef
        ? `<p style="margin-top: var(--space-2); color: var(--color-gray-700); font-size: var(--font-size-sm);">
               Look for <strong>REF: ${escapeHtml(postcardRef)}</strong> on your postcard to identify it as yours.
           </p>`
        : '';

    return `
        <div style="text-align: center; margin-bottom: var(--space-6);">
            <div style="font-size: 48px; margin-bottom: var(--space-4);">&#x1F4EC;</div>
            <h2 style="font-size: var(--font-size-xl); font-weight: 600; margin-bottom: var(--space-2);">
                Postcard On Its Way!
            </h2>
            <p style="color: var(--color-gray-600);">
                Your verification postcard has been mailed. It should arrive within 5-7 business days.
                Enter the 8-character code when it arrives.
            </p>
            ${refMessage}
        </div>

        <form id="code-form">
            <div class="form-group">
                <label for="code" class="form-label form-label--required">Verification Code</label>
                <input
                    type="text"
                    id="code"
                    name="code"
                    class="form-input"
                    required
                    pattern="[0-9a-fA-F]{8}"
                    maxlength="8"
                    placeholder="abcd1234"
                    style="text-align: center; font-size: var(--font-size-2xl); letter-spacing: 0.3em;"
                    autocomplete="one-time-code"
                >
            </div>

            <div id="code-error" class="form-error hidden"></div>

            <button type="submit" class="btn btn--primary btn--block" id="verify-btn">
                Verify Code
            </button>
        </form>

        <div style="margin-top: var(--space-6); text-align: center;">
            <p style="font-size: var(--font-size-sm); color: var(--color-gray-500); margin-bottom: var(--space-2);">
                Didn't receive the postcard?
            </p>
            <div style="display: flex; gap: var(--space-2); justify-content: center;">
                <button class="btn btn--ghost btn--sm" id="resend-btn">Resend Postcard</button>
                <button class="btn btn--ghost btn--sm" id="cancel-btn">Start Over</button>
            </div>
        </div>
    `;
}

/**
 * Bind event handlers for address form
 */
function bindAddressFormHandlers() {
    const form = document.getElementById('address-form');
    if (!form) return;

    form.addEventListener('submit', handleAddressSubmit);
}

/**
 * Bind event handlers for code entry form
 */
function bindCodeFormHandlers() {
    const form = document.getElementById('code-form');
    const resendButton = document.getElementById('resend-btn');
    const cancelButton = document.getElementById('cancel-btn');

    if (form) {
        form.addEventListener('submit', handleCodeSubmit);
    }

    if (resendButton) {
        resendButton.addEventListener('click', handleResend);
    }

    if (cancelButton) {
        cancelButton.addEventListener('click', handleCancel);
    }
}

/**
 * Handle address form submission
 * @param {Event} event - Submit event
 */
async function handleAddressSubmit(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('submit-btn');
    const errorElement = document.getElementById('address-error');

    const data = {
        address: {
            line1: form.street_address.value.trim(),
            line2: form.unit.value.trim() || undefined,
            city: form.city.value.trim(),
            state: form.state.value,
            postal_code: form.zip_code.value.trim(),
        },
    };

    // Only include region_id if user selected one
    if (form.region_id.value) {
        data.region_id = form.region_id.value;
    }

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        await requestPostcardVerification(data);
        toast.success('Verification postcard has been sent!');

        // Refresh the page to show code entry form
        const container = document.getElementById('main');
        if (container) {
            render(container);
        }
    } catch (error) {
        let errorMessage = 'Failed to send verification postcard. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 400) {
                errorMessage = error.message || 'Invalid address. Please check your information.';
            } else if (error.status === 429) {
                errorMessage = 'Too many verification requests. Please wait before trying again.';
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
 * Handle verification code submission
 * @param {Event} event - Submit event
 */
async function handleCodeSubmit(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('verify-btn');
    const errorElement = document.getElementById('code-error');

    const code = form.code.value.trim();

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        await verifyPostcardCode(code);

        // Refresh user data
        await getCurrentUser();

        toast.success('Your address has been verified! Welcome to the community.');
        navigate('/dashboard');
    } catch (error) {
        let errorMessage = 'Failed to verify code. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 400) {
                errorMessage = 'Invalid or expired verification code.';
            } else if (error.message) {
                errorMessage = error.message;
            }
        }

        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
        form.code.focus();
        form.code.select();
    } finally {
        submitButton.disabled = false;
        submitButton.classList.remove('btn--loading');
    }
}

/**
 * Handle resend postcard button click
 */
async function handleResend() {
    const confirmed = await modal.confirm({
        title: 'Resend Postcard',
        message: 'Are you sure you want to resend the verification postcard? This counts against your verification limit.',
        confirmLabel: 'Resend',
        cancelLabel: 'Cancel',
    });

    if (!confirmed) return;

    try {
        await resendPostcard();
        toast.success('A new postcard has been sent!');
    } catch (error) {
        let errorMessage = 'Failed to resend postcard.';

        if (error instanceof ApiError) {
            if (error.status === 429) {
                errorMessage = 'You have reached the limit for verification requests. Please wait 30 days.';
            } else if (error.message) {
                errorMessage = error.message;
            }
        }

        toast.error(errorMessage);
    }
}

/**
 * Handle cancel verification button click
 */
async function handleCancel() {
    const confirmed = await modal.confirm({
        title: 'Cancel Verification',
        message: 'Are you sure you want to cancel this verification request and start over?',
        confirmLabel: 'Cancel Request',
        confirmType: 'danger',
        cancelLabel: 'Keep Waiting',
    });

    if (!confirmed) return;

    try {
        await cancelVerificationRequest();
        toast.info('Verification request cancelled.');

        // Refresh the page to show address form
        const container = document.getElementById('main');
        if (container) {
            render(container);
        }
    } catch (error) {
        toast.error('Failed to cancel verification request.');
    }
}

/**
 * @param {string} text - Text to escape
 * @returns {string} Escaped text
 */
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
