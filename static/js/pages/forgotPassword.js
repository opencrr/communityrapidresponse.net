/**
 * Forgot password page
 * Allows users to request a password reset email.
 */

import { requestPasswordReset } from '../api/auth.js?v=c49aed9';
import { ApiError } from '../api/client.js?v=c49aed9';

/**
 * Render the forgot password page
 * @param {HTMLElement} container - Container element to render into
 */
export function render(container) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Reset your password</h1>
                    </div>
                    <div class="card__body">
                        <p style="margin-bottom: 1rem; color: var(--color-text-secondary);">
                            Enter your email address and we'll send you a link to reset your password.
                        </p>
                        <form id="forgot-password-form">
                            <div class="form-group">
                                <label for="email" class="form-label form-label--required">Email</label>
                                <input
                                    type="email"
                                    id="email"
                                    name="email"
                                    class="form-input"
                                    required
                                    autocomplete="email"
                                    autofocus
                                >
                            </div>
                            <div id="forgot-error" class="form-error hidden"></div>
                            <button type="submit" class="btn btn--primary btn--block" id="submit-btn">
                                Send Reset Link
                            </button>
                        </form>
                        <div id="success-message" class="hidden" style="text-align: center; padding: 1rem 0;">
                            <p style="color: var(--color-success); font-weight: 500; margin-bottom: 0.5rem;">
                                Check your email
                            </p>
                            <p style="color: var(--color-text-secondary);">
                                If an account with that email exists, we've sent a password reset link.
                                The link will expire in 1 hour.
                            </p>
                        </div>
                    </div>
                </div>
                <p class="auth-page__footer">
                    <a href="/login" data-link>Back to login</a>
                </p>
            </div>
        </div>
    `;

    // Bind form handler
    const form = document.getElementById('forgot-password-form');
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
    const errorElement = document.getElementById('forgot-error');
    const successMessage = document.getElementById('success-message');

    const email = form.email.value.trim();

    // Clear previous messages
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        await requestPasswordReset(email);

        // Hide form, show success
        form.classList.add('hidden');
        successMessage.classList.remove('hidden');
    } catch (error) {
        let errorMessage = 'An error occurred. Please try again.';

        if (error instanceof ApiError && error.message) {
            errorMessage = error.message;
        }

        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
    } finally {
        submitButton.disabled = false;
        submitButton.classList.remove('btn--loading');
    }
}

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
