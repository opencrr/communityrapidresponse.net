/**
 * User profile page
 * Shows account information and allows password change.
 */

import { getUser, getVerificationStatus } from '../utils/store.js';
import { changePassword, getDeletionWarnings, deleteAccount } from '../api/auth.js';
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
    const user = getUser();
    const verificationStatus = getVerificationStatus();

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
                    <h1 class="page__title">Your Profile</h1>
                </div>

                <div class="card" style="margin-bottom: 1.5rem;">
                    <div class="card__header">
                        <h2 class="card__title">Account Information</h2>
                    </div>
                    <div class="card__body">
                        <dl style="display: grid; grid-template-columns: auto 1fr; gap: 0.75rem 1.5rem; margin: 0;">
                            <dt style="font-weight: 600;">Username</dt>
                            <dd style="margin: 0; text-align: right;">${escapeHtml(user?.username || '')}</dd>

                            <dt style="font-weight: 600;">Email</dt>
                            <dd style="margin: 0; text-align: right;">${escapeHtml(user?.email || '')}</dd>

                            <dt style="font-weight: 600;">Verification Status</dt>
                            <dd style="margin: 0; text-align: right;">
                                <span class="header__tier-badge header__tier-badge--${verificationStatus.level}">
                                    ${verificationStatus.label}
                                </span>
                            </dd>

                            <dt style="font-weight: 600;">Email Verified</dt>
                            <dd style="margin: 0; text-align: right;">${user?.email_verified ? 'Yes' : 'No'}</dd>

                            <dt style="font-weight: 600;">Postcard Verified</dt>
                            <dd style="margin: 0; text-align: right;">${user?.postcard_verified ? 'Yes' : 'No'}</dd>

                            <dt style="font-weight: 600;">Vouch Verified</dt>
                            <dd style="margin: 0; text-align: right;">${user?.vouch_verified ? 'Yes' : 'No'}</dd>

                            <dt style="font-weight: 600;">Member Since</dt>
                            <dd style="margin: 0; text-align: right;">${formatDate(user?.created_at)}</dd>

                            <dt style="font-weight: 600;">Last Login</dt>
                            <dd style="margin: 0; text-align: right;">${formatDate(user?.last_login)}</dd>
                        </dl>
                    </div>
                </div>

                <div class="card">
                    <div class="card__header">
                        <h2 class="card__title">Change Password</h2>
                    </div>
                    <div class="card__body">
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
                            <button type="submit" class="btn btn--primary" id="change-password-btn">
                                Change Password
                            </button>
                        </form>
                    </div>
                </div>

                <div class="card" style="margin-top: 1.5rem; border-color: var(--color-danger, #dc3545);">
                    <div class="card__header">
                        <h2 class="card__title" style="color: var(--color-danger, #dc3545);">Delete Account</h2>
                    </div>
                    <div class="card__body">
                        <p style="margin-bottom: 1rem; color: var(--color-danger, #dc3545); font-weight: 600;">
                            This action is permanent and cannot be undone.
                        </p>
                        <p style="margin-bottom: 1rem;">
                            Deleting your account will remove all your data, including community memberships,
                            verification status, and school memberships.
                        </p>
                        <button type="button" class="btn btn--danger" id="delete-account-btn">
                            Delete My Account
                        </button>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind form handler
    const form = document.getElementById('change-password-form');
    form.addEventListener('submit', handleChangePassword);

    // Bind delete account button
    const deleteButton = document.getElementById('delete-account-btn');
    deleteButton.addEventListener('click', handleDeleteAccount);
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

    // Validate passwords match
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

    // Disable form
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

        // Clear the form
        form.reset();
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
    // Fetch preflight warnings
    let warnings = [];
    try {
        const preflightData = await getDeletionWarnings();
        warnings = preflightData.warnings || [];
    } catch (error) {
        toast.error('Failed to load deletion warnings. Please try again.');
        return;
    }

    // Build modal content
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

                    // Find and disable the delete button in the modal
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
