/**
 * Email verification page
 * Handles both the email verification link from the verification email
 * and the pending email verification state after login
 */

import { get, post } from '../api/client.js';
import { navigate } from '../app.js';
import toast from '../components/toast.js';

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
 * Render the email verification page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    // Get token from URL query parameter
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get('token');

    if (!token) {
        // Message/email are passed as navigation state from login.js when available
        // (e.g. a stale-session redirect from the API client has no such state).
        const state = window.history.state || {};
        renderPendingVerification(container, state.message, state.email);
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
 * Render pending email verification state
 * @param {HTMLElement} container - Container element
 * @param {string} [message] - Message returned by the login response, if available
 * @param {string} [email] - Email address the verification link was sent to, if available
 */
function renderPendingVerification(container, message, email) {
    const description = message
        ? escapeHtml(message)
        : 'Please verify your email address to continue. Check your inbox for a verification link.';
    const emailLine = email
        ? `<p class="empty-state__description" style="margin-top: 1rem; font-size: 0.9rem; color: var(--color-text-secondary);">Sent to <strong>${escapeHtml(email)}</strong></p>`
        : '';

    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Verify Your Email</h1>
                    </div>
                    <div class="card__body">
                        <div class="empty-state">
                            <div class="empty-state__icon" style="color: var(--color-info);">✉️</div>
                            <h3 class="empty-state__title">Check Your Email</h3>
                            <p class="empty-state__description">${description}</p>
                            ${emailLine}
                            <div class="btn-group" style="margin-top: 1.5rem;">
                                <button id="resend-btn" class="btn btn--primary">Resend verification email</button>
                            </div>
                            <div id="resend-status" class="form-error hidden" style="margin-top: 1rem;"></div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;

    const resendBtn = document.getElementById('resend-btn');
    const resendStatus = document.getElementById('resend-status');

    resendBtn.addEventListener('click', async () => {
        resendBtn.disabled = true;
        resendBtn.classList.add('btn--loading');
        resendStatus.classList.add('hidden');
        resendStatus.textContent = '';

        try {
            await post('/auth/resend-verification');
            toast.success('Verification email sent!');
            resendStatus.textContent = 'Verification email sent! Check your inbox.';
            resendStatus.classList.remove('hidden');
            resendStatus.classList.remove('form-error');
            resendStatus.classList.add('form-success');

            resendBtn.disabled = true;
            resendBtn.textContent = 'Email sent';

            setTimeout(() => {
                resendBtn.disabled = false;
                resendBtn.textContent = 'Resend verification email';
                resendStatus.classList.add('hidden');
            }, 5000);
        } catch (error) {
            let errorMessage = 'Failed to resend verification email. Please try again.';

            if (error.status === 429) {
                errorMessage = 'Too many requests. Please wait before trying again.';
            } else if (error.message) {
                errorMessage = error.message;
            }

            resendStatus.textContent = errorMessage;
            resendStatus.classList.remove('hidden');
            resendStatus.classList.add('form-error');
            resendBtn.disabled = false;
            resendBtn.classList.remove('btn--loading');
        } finally {
            resendBtn.classList.remove('btn--loading');
        }
    });
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
