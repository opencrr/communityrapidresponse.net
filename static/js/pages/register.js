/**
 * Registration page
 */

import { register } from '../api/auth.js';
import { ApiError } from '../api/client.js';
import toast from '../components/toast.js';
import { navigate } from '../app.js';

const MIN_PASSWORD_LENGTH = 12;

/**
 * Render the registration page
 * @param {HTMLElement} container - Container element to render into
 */
export function render(container) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Create your account</h1>
                    </div>
                    <div class="card__body">
                        <form id="register-form">
                            <div class="form-group">
                                <label for="username" class="form-label form-label--required">Username</label>
                                <input
                                    type="text"
                                    id="username"
                                    name="username"
                                    class="form-input"
                                    required
                                    autocomplete="username"
                                    autofocus
                                >
                                <p class="form-hint">This will be visible to other verified users.</p>
                            </div>
                            <div class="form-group">
                                <label for="email" class="form-label form-label--required">Email</label>
                                <input
                                    type="email"
                                    id="email"
                                    name="email"
                                    class="form-input"
                                    required
                                    autocomplete="email"
                                >
                            </div>
                            <div class="form-group">
                                <label for="password" class="form-label form-label--required">Password</label>
                                <input
                                    type="password"
                                    id="password"
                                    name="password"
                                    class="form-input"
                                    required
                                    minlength="${MIN_PASSWORD_LENGTH}"
                                    autocomplete="new-password"
                                >
                                <p class="form-hint">At least ${MIN_PASSWORD_LENGTH} characters.</p>
                            </div>
                            <div class="form-group">
                                <label for="password-confirm" class="form-label form-label--required">Confirm Password</label>
                                <input
                                    type="password"
                                    id="password-confirm"
                                    name="passwordConfirm"
                                    class="form-input"
                                    required
                                    autocomplete="new-password"
                                >
                            </div>
                            <div id="register-error" class="form-error hidden"></div>
                            <button type="submit" class="btn btn--primary btn--block" id="submit-btn">
                                Create account
                            </button>
                        </form>
                    </div>
                </div>
                <p class="auth-page__footer">
                    Already have an account? <a href="/login" data-link>Log in</a>
                </p>
            </div>
        </div>
    `;

    // Bind form handler
    const form = document.getElementById('register-form');
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
    const errorElement = document.getElementById('register-error');

    // Get form values
    const username = form.username.value.trim();
    const email = form.email.value.trim();
    const password = form.password.value;
    const passwordConfirm = form.passwordConfirm.value;

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Client-side validation
    if (password !== passwordConfirm) {
        errorElement.textContent = 'Passwords do not match.';
        errorElement.classList.remove('hidden');
        form.passwordConfirm.focus();
        return;
    }

    if (password.length < MIN_PASSWORD_LENGTH) {
        errorElement.textContent = `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`;
        errorElement.classList.remove('hidden');
        form.password.focus();
        return;
    }

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        const response = await register({ username, email, password });

        // Check if email verification is required
        if (response.email_verification_sent) {
            // Show verification required message instead of navigating
            showVerificationMessage(form, email);
            return;
        }

        toast.success('Account created successfully!');
        navigate('/login');
    } catch (error) {
        let errorMessage = 'An error occurred. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 409) {
                // Could be duplicate username or email
                errorMessage = error.message || 'Username or email already taken.';
            } else if (error.status === 400) {
                errorMessage = error.message || 'Invalid registration data.';
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
 * Show verification email sent message
 * @param {HTMLFormElement} form - The registration form
 * @param {string} email - The email address
 */
function showVerificationMessage(form, email) {
    const container = form.closest('.card');
    container.innerHTML = `
        <div class="card__header">
            <h1 class="card__title">Check your email</h1>
        </div>
        <div class="card__body">
            <div class="empty-state">
                <div class="empty-state__icon">&#x2709;</div>
                <p class="empty-state__description">
                    We've sent a verification link to <strong>${escapeHtml(email)}</strong>.
                    Please check your inbox and click the link to verify your account.
                </p>
                <p class="empty-state__description text-muted">
                    The link will expire in 24 hours.
                </p>
                <a href="/login" class="btn btn--primary" data-link>Go to login</a>
            </div>
        </div>
    `;
}

export function cleanup() {
    // No cleanup needed
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export default { render, cleanup };
