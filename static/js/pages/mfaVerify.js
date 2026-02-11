/**
 * MFA Verification page
 */

import { verify, verifyWithBackupCode } from '../api/mfa.js?v=c49aed9';
import { ApiError } from '../api/client.js?v=c49aed9';
import toast from '../components/toast.js?v=c49aed9';
import { navigate } from '../app.js?v=c49aed9';

/**
 * Render the MFA verification page
 * @param {HTMLElement} container - Container element to render into
 */
export function render(container) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="mfa-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Two-Factor Authentication</h1>
                    </div>
                    <div class="card__body">
                        <div id="mfa-totp-section">
                            <p class="mfa-verify__intro">
                                Enter the 6-digit code from your authenticator app.
                            </p>

                            <form id="mfa-verify-form">
                                <div class="form-group">
                                    <label for="mfa-code" class="form-label form-label--required">
                                        Authentication Code
                                    </label>
                                    <input
                                        type="text"
                                        id="mfa-code"
                                        name="code"
                                        class="form-input mfa-code-input"
                                        required
                                        autocomplete="one-time-code"
                                        inputmode="numeric"
                                        pattern="[0-9]{6}"
                                        maxlength="6"
                                        placeholder="000000"
                                        autofocus
                                    >
                                </div>
                                <div id="verify-error" class="form-error hidden"></div>
                                <button type="submit" class="btn btn--primary btn--block" id="verify-btn">
                                    Verify
                                </button>
                            </form>

                            <div class="mfa-verify__backup-link">
                                <button type="button" class="btn btn--ghost" id="use-backup-btn">
                                    Use a backup code instead
                                </button>
                            </div>
                        </div>

                        <div id="mfa-backup-section" class="hidden">
                            <p class="mfa-verify__intro">
                                Enter one of your backup codes.
                            </p>

                            <form id="mfa-backup-form">
                                <div class="form-group">
                                    <label for="backup-code" class="form-label form-label--required">
                                        Backup Code
                                    </label>
                                    <input
                                        type="text"
                                        id="backup-code"
                                        name="backup_code"
                                        class="form-input"
                                        required
                                        autocomplete="off"
                                        placeholder="XXXX-XXXX"
                                    >
                                    <p class="form-hint">
                                        Each backup code can only be used once.
                                    </p>
                                </div>
                                <div id="backup-error" class="form-error hidden"></div>
                                <button type="submit" class="btn btn--primary btn--block" id="backup-verify-btn">
                                    Verify Backup Code
                                </button>
                            </form>

                            <div class="mfa-verify__backup-link">
                                <button type="button" class="btn btn--ghost" id="use-totp-btn">
                                    Use authenticator app instead
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;

    // Bind event handlers
    document.getElementById('mfa-verify-form').addEventListener('submit', handleTOTPVerify);
    document.getElementById('mfa-backup-form').addEventListener('submit', handleBackupVerify);
    document.getElementById('use-backup-btn').addEventListener('click', showBackupSection);
    document.getElementById('use-totp-btn').addEventListener('click', showTOTPSection);
}

/**
 * Show backup code section
 */
function showBackupSection() {
    document.getElementById('mfa-totp-section').classList.add('hidden');
    document.getElementById('mfa-backup-section').classList.remove('hidden');
    document.getElementById('backup-code').focus();
}

/**
 * Show TOTP section
 */
function showTOTPSection() {
    document.getElementById('mfa-backup-section').classList.add('hidden');
    document.getElementById('mfa-totp-section').classList.remove('hidden');
    document.getElementById('mfa-code').focus();
}

/**
 * Handle TOTP verification form submission
 */
async function handleTOTPVerify(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('verify-btn');
    const errorElement = document.getElementById('verify-error');
    const code = form.code.value.trim();

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    // Validate code format
    if (!/^\d{6}$/.test(code)) {
        errorElement.textContent = 'Please enter a 6-digit code';
        errorElement.classList.remove('hidden');
        return;
    }

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        const response = await verify(code);

        // Check if backup codes are running low
        if (response.backup_codes_count !== undefined && response.backup_codes_count <= 3) {
            toast.warning(`You have ${response.backup_codes_count} backup codes remaining.`);
        }

        toast.success('Welcome back!');
        navigate('/dashboard');
    } catch (error) {
        let errorMessage = 'Failed to verify code. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 401) {
                errorMessage = 'Invalid code. Please check your authenticator app and try again.';
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
 * Handle backup code verification form submission
 */
async function handleBackupVerify(event) {
    event.preventDefault();

    const form = event.target;
    const submitButton = document.getElementById('backup-verify-btn');
    const errorElement = document.getElementById('backup-error');
    const backupCode = form.backup_code.value.trim();

    // Clear previous error
    errorElement.classList.add('hidden');
    errorElement.textContent = '';

    if (!backupCode) {
        errorElement.textContent = 'Please enter a backup code';
        errorElement.classList.remove('hidden');
        return;
    }

    // Disable form
    submitButton.disabled = true;
    submitButton.classList.add('btn--loading');

    try {
        const response = await verifyWithBackupCode(backupCode);

        // Warn about remaining backup codes
        if (response.backup_codes_count !== undefined) {
            if (response.backup_codes_count === 0) {
                toast.warning('That was your last backup code. Consider regenerating backup codes.');
            } else if (response.backup_codes_count <= 3) {
                toast.warning(`You have ${response.backup_codes_count} backup codes remaining.`);
            }
        }

        toast.success('Welcome back!');
        navigate('/dashboard');
    } catch (error) {
        let errorMessage = 'Failed to verify backup code. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 401) {
                errorMessage = 'Invalid backup code. Please check and try again.';
            } else if (error.data?.error === 'no_backup_codes') {
                errorMessage = 'No backup codes remaining. Please contact support.';
            } else if (error.message) {
                errorMessage = error.message;
            }
        }

        errorElement.textContent = errorMessage;
        errorElement.classList.remove('hidden');
        form.backup_code.focus();
        form.backup_code.select();
    } finally {
        submitButton.disabled = false;
        submitButton.classList.remove('btn--loading');
    }
}

export function cleanup() {
    // No cleanup needed
}

export default { render, cleanup };
