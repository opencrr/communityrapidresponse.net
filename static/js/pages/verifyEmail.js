/**
 * Email verification page
 * Handles the email verification link from the verification email
 */

import { get } from '../api/client.js';
import { navigate } from '../app.js';

/**
 * Render the email verification page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    // Get token from URL query parameter
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get('token');

    if (!token) {
        renderError(container, 'Invalid verification link', 'No verification token found in the URL.');
        return;
    }

    // Show loading state
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Verifying your email...</h1>
                    </div>
                    <div class="card__body">
                        <div class="loading">
                            <div class="spinner spinner--lg"></div>
                            <p class="loading__text">Please wait while we verify your email address.</p>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Call the verification API
    try {
        const response = await get(`/auth/verify-email?token=${encodeURIComponent(token)}`);
        renderSuccess(container, response.message || 'Your email has been verified successfully!');
    } catch (error) {
        let errorMessage = 'An error occurred while verifying your email.';
        let errorDescription = 'Please try again or request a new verification email.';

        if (error.status === 400) {
            if (error.message?.includes('expired')) {
                errorMessage = 'Verification link expired';
                errorDescription = 'This verification link has expired. Please log in and request a new verification email.';
            } else if (error.message?.includes('invalid')) {
                errorMessage = 'Invalid verification link';
                errorDescription = 'This verification link is invalid. Please check your email for the correct link.';
            } else {
                errorMessage = error.message || errorMessage;
            }
        } else if (error.message) {
            errorMessage = error.message;
        }

        renderError(container, errorMessage, errorDescription);
    }
}

/**
 * Render success state
 * @param {HTMLElement} container - Container element
 * @param {string} message - Success message
 */
function renderSuccess(container, message) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Email Verified!</h1>
                    </div>
                    <div class="card__body">
                        <div class="empty-state">
                            <div class="empty-state__icon" style="color: var(--color-success);">&#x2713;</div>
                            <p class="empty-state__description">${message}</p>
                            <a href="/login" class="btn btn--primary" data-link>Log in</a>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;
}

/**
 * Render error state
 * @param {HTMLElement} container - Container element
 * @param {string} title - Error title
 * @param {string} description - Error description
 */
function renderError(container, title, description) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Verification Failed</h1>
                    </div>
                    <div class="card__body">
                        <div class="empty-state">
                            <div class="empty-state__icon" style="color: var(--color-error);">&#x2717;</div>
                            <h3 class="empty-state__title">${title}</h3>
                            <p class="empty-state__description">${description}</p>
                            <div class="btn-group">
                                <a href="/login" class="btn btn--primary" data-link>Log in</a>
                                <a href="/register" class="btn btn--secondary" data-link>Register</a>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;
}

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
