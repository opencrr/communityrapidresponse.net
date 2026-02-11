/**
 * Reset password page
 * Validates token from URL and allows setting a new password.
 */

import { validateResetToken, resetPassword } from '../api/auth.js';
import { ApiError } from '../api/client.js';

/**
 * Render the reset password page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    // Extract token from URL
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get('token');

    if (!token) {
        renderError(container, 'No reset token provided.', true);
        return;
    }

    // Show loading state
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__body" style="text-align: center; padding: 2rem;">
                        <div class="spinner spinner--lg"></div>
                        <p style="margin-top: 1rem; color: var(--color-text-secondary);">Validating reset link...</p>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Validate the token
    try {
        await validateResetToken(token);
        renderResetForm(container, token);
    } catch (error) {
        let errorMessage = 'This reset link is invalid or has expired.';
        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }
        renderError(container, errorMessage, true);
    }
}

/**
 * Render the password reset form
 * @param {HTMLElement} container - Container element
 * @param {string} token - Valid reset token
 */
function renderResetForm(container, token) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Set a new password</h1>
                    </div>
                    <div class="card__body">
                        <form id="reset-password-form">
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
                                    autofocus
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
                            <div id="reset-error" class="form-error hidden"></div>
                            <button type="submit" class="btn btn--primary btn--block" id="submit-btn">
                                Reset Password
                            </button>
                        </form>
                        <div id="success-message" class="hidden" style="text-align: center; padding: 1rem 0;">
                            <p style="color: var(--color-success); font-weight: 500; margin-bottom: 0.5rem;">
                                Password reset successful
                            </p>
                            <p style="color: var(--color-text-secondary); margin-bottom: 1rem;">
                                Your password has been updated. You can now log in with your new password.
                            </p>
                            <a href="/login" class="btn btn--primary" data-link>Go to Login</a>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind form handler
    const form = document.getElementById('reset-password-form');
    form.addEventListener('submit', (event) => handleSubmit(event, token));
}

/**
 * Render an error state
 * @param {HTMLElement} container - Container element
 * @param {string} message - Error message
 * @param {boolean} showForgotLink - Whether to show link to forgot password page
 */
function renderError(container, message, showForgotLink) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__body" style="text-align: center; padding: 2rem;">
                        <p style="color: var(--color-danger); font-weight: 500; margin-bottom: 1rem;">
                            ${escapeHtml(message)}
                        </p>
                        ${showForgotLink ? `
                            <a href="/forgot-password" class="btn btn--primary" data-link>Request a New Reset Link</a>
                        ` : ''}
                    </div>
                </div>
                <p class="auth-page__footer">
                    <a href="/login" data-link>Back to login</a>
                </p>
            </div>
        </div>
    `;
}

/**
 * Handle form submission
 * @param {Event} event - Submit event
 * @param {string} token - Reset token
 */
async function handleSubmit(event, token) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('submit-btn');
    const errorElement = document.getElementById('reset-error');
    const successMessage = document.getElementById('success-message');

    const newPassword = form.new_password.value;
    const confirmPassword = form.confirm_password.value;

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Validate passwords match
    if (newPassword !== confirmPassword) {
        errorElement.textContent = 'Passwords do not match.';
        errorElement.classList.remove('hidden');
        return;
    }

    if (newPassword.length < 12) {
        errorElement.textContent = 'Password must be at least 12 characters.';
        errorElement.classList.remove('hidden');
        return;
    }

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        await resetPassword({ token, password: newPassword });

        // Hide form, show success
        form.classList.add('hidden');
        successMessage.classList.remove('hidden');
    } catch (error) {
        let errorMessage = 'An error occurred. Please try again.';

        if (error instanceof ApiError) {
            if (error.data?.error === 'expired_token') {
                errorMessage = 'This reset link has expired. Please request a new one.';
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
