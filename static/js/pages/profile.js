/**
 * User profile page
 * Shows account info, verification status, verified regions, and account settings.
 */

import { getUser, getVerificationStatus, isPostcardVerified, isVouchVerified } from '../utils/store.js';
import { getCurrentUser, changePassword, getDeletionWarnings, deleteAccount } from '../api/auth.js';
import { getVerificationStatus as getVerificationStatusAPI } from '../api/verification.js';
import { getMyRegions } from '../api/regions.js';
import { ApiError } from '../api/client.js';
import toast from '../components/toast.js';
import * as modal from '../components/modal.js';
import { navigate } from '../app.js';
import { rewrapBackup } from '../crypto/index.js';

/**
 * Render the profile page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    // Fetch fresh user data so we get created_at and last_login
    await getCurrentUser();
    const user = getUser();
    const postcardVerified = isPostcardVerified();

    // Fetch detailed verification status from API (for pending postcard info)
    let verificationDetail = null;
    try {
        verificationDetail = await getVerificationStatusAPI();
    } catch (error) {
        console.error('Failed to fetch verification status:', error);
    }

    // Fetch user's regions
    let regions = [];
    try {
        regions = await getMyRegions();
    } catch (error) {
        console.error('Failed to fetch regions:', error);
    }

    const formatDate = (dateString) => {
        if (!dateString) return 'Never';
        const date = new Date(dateString);
        return date.toLocaleDateString(undefined, {
            year: 'numeric',
            month: 'long',
            day: 'numeric',
        });
    };

    container.innerHTML = `
        <div class="page">
            <div class="page__container" style="max-width: 640px;">
                <div class="page__header">
                    <h1 class="page__title">Profile</h1>
                </div>

                <section class="profile-section profile-section--first">
                    <h2>Account</h2>
                    <dl class="profile-info-grid">
                        <dt>Username</dt>
                        <dd>${escapeHtml(user?.username || '')}</dd>

                        <dt>Email</dt>
                        <dd>${escapeHtml(user?.email || '')}</dd>

                        <dt>Member Since</dt>
                        <dd>${formatDate(user?.created_at)}</dd>

                        <dt>Last Login</dt>
                        <dd>${formatDate(user?.last_login)}</dd>
                    </dl>
                </section>

                <section class="profile-section">
                    <h2>Address Verification</h2>
                    ${renderVerificationSection(postcardVerified, verificationDetail)}
                </section>

                <section class="profile-section">
                    <h2>Verified Regions</h2>
                    ${renderRegionsSection(regions)}
                </section>

                <section class="profile-section">
                    <h2>Account Settings</h2>
                    ${renderSettingsSection()}
                </section>
            </div>
        </div>
    `;

    bindEventHandlers();
}

/**
 * Render the verification status section
 * @param {boolean} postcardVerified - Whether user is postcard verified
 * @param {Object|null} verificationDetail - Detailed verification status from API
 * @returns {string} HTML string
 */
function renderVerificationSection(postcardVerified, verificationDetail) {
    const hasPendingPostcard = verificationDetail?.pending_request;

    if (postcardVerified) {
        return `
            <div class="verification-badge verification-badge--verified">
                &#10003; Address Verified
            </div>
        `;
    }

    if (hasPendingPostcard) {
        const postcardRef = verificationDetail?.postcard_ref;
        const refHtml = postcardRef
            ? `<p style="margin-top: var(--space-2); font-size: var(--font-size-sm); color: var(--color-gray-600);">
                   Look for <strong>REF: ${escapeHtml(postcardRef)}</strong> on your postcard.
               </p>`
            : '';

        return `
            <div class="verification-badge verification-badge--pending">
                &#9993; Postcard Sent
            </div>
            <p style="margin-top: var(--space-3); color: var(--color-gray-700);">
                Your verification postcard has been mailed. Enter your verification code when it arrives.
            </p>
            ${refHtml}
            <div style="margin-top: var(--space-4);">
                <a href="/verify" class="btn btn--primary btn--sm" data-link>Enter Verification Code</a>
            </div>
        `;
    }

    // Not verified, no pending request
    return `
        <div class="verification-badge verification-badge--unverified">
            Not Verified
        </div>
        <p style="margin-top: var(--space-3); color: var(--color-gray-700);">
            Verify your address to create groups and become a group admin.
        </p>
        <div style="margin-top: var(--space-4);">
            <a href="/verify" class="btn btn--primary btn--sm" data-link>Verify Your Address</a>
        </div>
    `;
}

/**
 * Render the verified regions section
 * @param {Array} regions - User's regions
 * @returns {string} HTML string
 */
function renderRegionsSection(regions) {
    if (!regions || regions.length === 0) {
        return `
            <p style="color: var(--color-gray-600);">
                No verified regions. Verify an address to get started.
            </p>
            <div style="margin-top: var(--space-3);">
                <a href="/verify" class="btn btn--outline btn--sm" data-link>Verify New Address</a>
            </div>
        `;
    }

    const regionItems = regions.map(region => `
        <div class="region-item">
            <div>
                <span class="region-item__name">${escapeHtml(region.name)}</span>
                <span class="region-item__type">${escapeHtml(formatRegionType(region.type || region.region_type))}</span>
            </div>
        </div>
    `).join('');

    return `
        <div class="region-list">
            ${regionItems}
        </div>
        <div style="margin-top: var(--space-4);">
            <a href="/verify" class="btn btn--outline btn--sm" data-link>Verify New Address</a>
        </div>
    `;
}

/**
 * Render the account settings section
 * @returns {string} HTML string
 */
function renderSettingsSection() {
    return `
        <div class="profile-settings-list">
            <div class="profile-settings-item">
                <div>
                    <strong>Change Password</strong>
                    <p class="profile-settings-desc">Update your account password.</p>
                </div>
                <button type="button" class="btn btn--outline btn--sm" id="show-password-form-btn">Change</button>
            </div>
            <div id="change-password-section" class="hidden" style="margin-top: var(--space-4);">
                <form id="change-password-form">
                    <div class="form-group">
                        <label for="current-password" class="form-label form-label--required">Current Password</label>
                        <input
                            type="password"
                            id="current-password"
                            name="current_password"
                            class="form-input"
                            required
                            autocomplete="current-password"
                        >
                    </div>
                    <div class="form-group">
                        <label for="new-password" class="form-label form-label--required">New Password</label>
                        <input
                            type="password"
                            id="new-password"
                            name="new_password"
                            class="form-input"
                            required
                            minlength="12"
                            autocomplete="new-password"
                        >
                        <p class="form-hint">Minimum 12 characters</p>
                    </div>
                    <div class="form-group">
                        <label for="confirm-password" class="form-label form-label--required">Confirm New Password</label>
                        <input
                            type="password"
                            id="confirm-password"
                            name="confirm_password"
                            class="form-input"
                            required
                            minlength="12"
                            autocomplete="new-password"
                        >
                    </div>
                    <div id="password-error" class="form-error hidden"></div>
                    <div style="display: flex; gap: var(--space-2);">
                        <button type="submit" class="btn btn--primary btn--sm" id="change-password-btn">
                            Change Password
                        </button>
                        <button type="button" class="btn btn--ghost btn--sm" id="cancel-password-btn">
                            Cancel
                        </button>
                    </div>
                </form>
            </div>

            <div class="profile-settings-item">
                <div>
                    <strong>Multi-Factor Authentication</strong>
                    <p class="profile-settings-desc">Manage your MFA settings.</p>
                </div>
                <a href="/mfa/setup" class="btn btn--outline btn--sm" data-link>Setup</a>
            </div>
        </div>

        <div class="danger-zone">
            <h3>Delete Account</h3>
            <p>
                Deleting your account will permanently remove all your data.
                This cannot be undone. You will lose access to all groups,
                connections, and verification status.
            </p>
            <button type="button" class="btn btn--danger btn--sm" id="delete-account-btn">
                Delete My Account
            </button>
        </div>
    `;
}

/**
 * Format region type for display
 * @param {string} regionType - Raw region type
 * @returns {string} Formatted type
 */
function formatRegionType(regionType) {
    if (!regionType) return '';
    const typeMap = {
        state: 'State',
        county: 'County',
        city: 'City',
        locality: 'Locality',
        neighborhood: 'Neighborhood',
        city_block: 'City Block',
    };
    return typeMap[regionType] || regionType;
}

/**
 * Bind all event handlers for the profile page
 */
function bindEventHandlers() {
    // Show/hide password form
    const showPasswordBtn = document.getElementById('show-password-form-btn');
    const cancelPasswordBtn = document.getElementById('cancel-password-btn');
    const passwordSection = document.getElementById('change-password-section');

    if (showPasswordBtn && passwordSection) {
        showPasswordBtn.addEventListener('click', () => {
            passwordSection.classList.remove('hidden');
            showPasswordBtn.classList.add('hidden');
            document.getElementById('current-password')?.focus();
        });
    }

    if (cancelPasswordBtn && passwordSection && showPasswordBtn) {
        cancelPasswordBtn.addEventListener('click', () => {
            passwordSection.classList.add('hidden');
            showPasswordBtn.classList.remove('hidden');
            document.getElementById('change-password-form')?.reset();
            const errorEl = document.getElementById('password-error');
            if (errorEl) {
                errorEl.classList.add('hidden');
                errorEl.textContent = '';
            }
        });
    }

    // Change password form
    const passwordForm = document.getElementById('change-password-form');
    if (passwordForm) {
        passwordForm.addEventListener('submit', handleChangePassword);
    }

    // Delete account button
    const deleteButton = document.getElementById('delete-account-btn');
    if (deleteButton) {
        deleteButton.addEventListener('click', handleDeleteAccount);
    }
}

/**
 * Handle change password form submission
 * @param {Event} event - Submit event
 */
async function handleChangePassword(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('change-password-btn');
    const errorElement = document.getElementById('password-error');

    const currentPassword = form.current_password.value;
    const newPassword = form.new_password.value;
    const confirmPassword = form.confirm_password.value;

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    if (newPassword !== confirmPassword) {
        errorElement.textContent = 'New passwords do not match.';
        errorElement.classList.remove('hidden');
        return;
    }

    if (newPassword.length < 12) {
        errorElement.textContent = 'New password must be at least 12 characters.';
        errorElement.classList.remove('hidden');
        return;
    }

    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        await changePassword({
            current_password: currentPassword,
            new_password: newPassword,
        });

        // Re-wrap encryption key backup with new password
        await rewrapBackup(newPassword);

        toast.success('Password changed successfully');
        form.reset();

        // Collapse the form back
        const passwordSection = document.getElementById('change-password-section');
        const showPasswordBtn = document.getElementById('show-password-form-btn');
        if (passwordSection) passwordSection.classList.add('hidden');
        if (showPasswordBtn) showPasswordBtn.classList.remove('hidden');
    } catch (error) {
        let errorMessage = 'An error occurred. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 401) {
                errorMessage = 'Current password is incorrect.';
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
 * Handle delete account button click - shows modal with preflight warnings and password confirmation
 */
async function handleDeleteAccount() {
    let warnings = [];
    try {
        const preflightData = await getDeletionWarnings();
        warnings = preflightData.warnings || [];
    } catch (error) {
        toast.error('Failed to load deletion warnings. Please try again.');
        return;
    }

    let warningsHtml = '';
    if (warnings.length > 0) {
        const warningItems = warnings.map(w =>
            `<li style="margin-bottom: 0.5rem;">${escapeHtml(w.message)}</li>`
        ).join('');
        warningsHtml = `
            <div style="background: var(--color-warning-bg, #fff3cd); border: 1px solid var(--color-warning-border, #ffc107); border-radius: 6px; padding: 1rem; margin-bottom: 1rem;">
                <strong>Warning:</strong>
                <ul style="margin: 0.5rem 0 0 1.25rem; padding: 0;">${warningItems}</ul>
            </div>
        `;
    }

    const contentHtml = `
        ${warningsHtml}
        <p style="margin-bottom: 1rem;">
            This action is <strong>permanent and cannot be undone</strong>. All your data will be removed.
        </p>
        <p style="margin-bottom: 1rem;">Enter your password to confirm:</p>
        <div class="form-group">
            <input
                type="password"
                id="delete-confirm-password"
                class="form-input"
                placeholder="Enter your password"
                autocomplete="current-password"
            >
        </div>
        <div id="delete-error" class="form-error hidden"></div>
    `;

    const modalControl = modal.showModal({
        title: 'Delete Account',
        content: contentHtml,
        closeOnBackdrop: false,
        actions: [
            {
                label: 'Cancel',
                type: 'secondary',
            },
            {
                label: 'Delete My Account',
                type: 'danger',
                closeOnClick: false,
                onClick: async () => {
                    const passwordInput = document.getElementById('delete-confirm-password');
                    const errorElement = document.getElementById('delete-error');
                    const password = passwordInput ? passwordInput.value : '';

                    if (!password) {
                        errorElement.textContent = 'Password is required.';
                        errorElement.classList.remove('hidden');
                        return;
                    }

                    const modalElement = modalControl.getElement();
                    const deleteBtn = modalElement.querySelector('.btn--danger');
                    if (deleteBtn) {
                        deleteBtn.disabled = true;
                        deleteBtn.classList.add('btn--loading');
                    }

                    try {
                        await deleteAccount(password);
                        modal.closeModal();
                        navigate('/');
                        toast.success('Your account has been deleted.');
                    } catch (error) {
                        if (deleteBtn) {
                            deleteBtn.disabled = false;
                            deleteBtn.classList.remove('btn--loading');
                        }

                        let errorMessage = 'An error occurred. Please try again.';
                        if (error instanceof ApiError) {
                            if (error.status === 401) {
                                errorMessage = 'Password is incorrect.';
                            } else if (error.status === 403) {
                                errorMessage = error.message || 'Account deletion is not allowed.';
                            } else if (error.message) {
                                errorMessage = error.message;
                            }
                        }

                        errorElement.textContent = errorMessage;
                        errorElement.classList.remove('hidden');
                    }
                },
            },
        ],
    });

    // Focus password input
    setTimeout(() => {
        const passwordInput = document.getElementById('delete-confirm-password');
        if (passwordInput) {
            passwordInput.focus();
        }
    }, 100);
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
