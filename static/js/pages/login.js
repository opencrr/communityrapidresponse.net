/**
 * Login page
 */

import { login } from '../api/auth.js?v=c49aed9';
import { ApiError } from '../api/client.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';
import { navigate } from '../app.js?v=c49aed9';

/**
 * Render the login page
 * @param {HTMLElement} container - Container element to render into
 */
export function render(container) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="auth-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Log in to your account</h1>
                    </div>
                    <div class="card__body">
                        <form id="login-form">
                            <div class="form-group">
                                <label for="email" class="form-label form-label--required">Email or Username</label>
                                <input
                                    type="text"
                                    id="email"
                                    name="email"
                                    class="form-input"
                                    required
                                    autocomplete="username"
                                    autofocus
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
                                    autocomplete="current-password"
                                >
                            </div>
                            <div id="login-error" class="form-error hidden"></div>
                            <div style="text-align: right; margin-bottom: 1rem;">
                                <a href="/forgot-password" data-link style="font-size: 0.875rem;">Forgot password?</a>
                            </div>
                            <button type="submit" class="btn btn--primary btn--block" id="submit-btn">
                                Log in
                            </button>
                        </form>
                    </div>
                </div>
                <p class="auth-page__footer">
                    Don't have an account? <a href="/register" data-link>Sign up</a>
                </p>
            </div>
        </div>
    `;

    // Bind form handler
    const form = document.getElementById('login-form');
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
    const errorElement = document.getElementById('login-error');

    // Get form values
    const email = form.email.value.trim();
    const password = form.password.value;

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        const response = await login({ email, password });

        // Check if MFA action is required
        if (response.mfa_action) {
            if (response.mfa_action === 'setup') {
                navigate('/mfa/setup');
            } else if (response.mfa_action === 'verify') {
                navigate('/mfa/verify');
            }
            return;
        }

        toast.success('Welcome back!');
        navigate('/dashboard');
    } catch (error) {
        let errorMessage = 'An error occurred. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 401) {
                errorMessage = 'Invalid credentials.';
            } else if (error.message) {
                errorMessage = error.message;
            }
        }

        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');

        // Focus email field on error
        form.email.focus();
    } finally {
        submitButton.disabled = false;
        submitButton.classList.remove('btn--loading');
    }
}

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
