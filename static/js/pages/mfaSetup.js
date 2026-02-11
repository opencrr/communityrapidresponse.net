/**
 * MFA Setup page
 */

import { initSetup, completeSetup } from '../api/mfa.js';
import { ApiError } from '../api/client.js';
import toast from '../components/toast.js';
import { navigate } from '../app.js';

let setupData = null;
let backupCodes = null;

/**
 * Render the MFA setup page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page page--centered">
            <div class="mfa-page">
                <div class="card">
                    <div class="card__header">
                        <h1 class="card__title">Set Up Two-Factor Authentication</h1>
                    </div>
                    <div class="card__body">
                        <div id="mfa-loading" class="loading">
                            <div class="spinner spinner--lg"></div>
                        </div>
                        <div id="mfa-content" class="hidden"></div>
                    </div>
                </div>
            </div>
        </div>
    `;

    await loadSetupData();
}

/**
 * Load MFA setup data (QR code and secret)
 */
async function loadSetupData() {
    const loadingElement = document.getElementById('mfa-loading');
    const contentElement = document.getElementById('mfa-content');

    try {
        setupData = await initSetup();

        loadingElement.classList.add('hidden');
        contentElement.classList.remove('hidden');

        renderSetupStep(contentElement);
    } catch (error) {
        loadingElement.classList.add('hidden');
        contentElement.classList.remove('hidden');

        let errorMessage = 'Failed to initialize MFA setup. Please try again.';
        if (error instanceof ApiError) {
            errorMessage = error.message || errorMessage;
        }

        contentElement.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">&#x26A0;</div>
                <h3 class="empty-state__title">Setup Error</h3>
                <p class="empty-state__description">${errorMessage}</p>
                <button class="btn btn--primary" onclick="location.reload()">Try Again</button>
            </div>
        `;
    }
}

/**
 * Render the setup step (QR code and verification)
 */
function renderSetupStep(container) {
    container.innerHTML = `
        <div class="mfa-setup">
            <div class="mfa-setup__instructions">
                <p class="mfa-setup__intro">
                    Two-factor authentication adds an extra layer of security to your account.
                </p>
                <ol class="mfa-setup__steps">
                    <li>Install an authenticator app like Google Authenticator, Authy, or 1Password</li>
                    <li>Scan the QR code below with your authenticator app</li>
                    <li>Enter the 6-digit code from your app to verify setup</li>
                </ol>
            </div>

            <div class="mfa-setup__qr">
                <img src="${setupData.qr_code}" alt="MFA QR Code" class="mfa-qr-code" />
            </div>

            <div class="mfa-setup__manual">
                <p class="text-muted">Can't scan the QR code? Enter this code manually:</p>
                <div class="mfa-secret-display">
                    <code id="mfa-secret">${formatSecret(setupData.secret)}</code>
                    <button type="button" class="btn btn--sm btn--ghost" id="copy-secret-btn" title="Copy to clipboard">
                        Copy
                    </button>
                </div>
            </div>

            <form id="mfa-verify-form" class="mfa-setup__form">
                <div class="form-group">
                    <label for="mfa-code" class="form-label form-label--required">
                        Enter the 6-digit code from your app
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
                    >
                </div>
                <div id="verify-error" class="form-error hidden"></div>
                <button type="submit" class="btn btn--primary btn--block" id="verify-btn">
                    Verify and Enable
                </button>
            </form>
        </div>
    `;

    // Bind event handlers
    document.getElementById('mfa-verify-form').addEventListener('submit', handleVerify);
    document.getElementById('copy-secret-btn').addEventListener('click', copySecret);

    // Auto-focus the code input
    document.getElementById('mfa-code').focus();
}

/**
 * Format secret for display (groups of 4)
 */
function formatSecret(secret) {
    return secret.match(/.{1,4}/g)?.join(' ') || secret;
}

/**
 * Copy secret to clipboard
 */
async function copySecret() {
    try {
        await navigator.clipboard.writeText(setupData.secret);
        toast.success('Secret copied to clipboard');
    } catch (error) {
        // Fallback for older browsers
        const secretElement = document.getElementById('mfa-secret');
        const range = document.createRange();
        range.selectNode(secretElement);
        window.getSelection().removeAllRanges();
        window.getSelection().addRange(range);
        document.execCommand('copy');
        window.getSelection().removeAllRanges();
        toast.success('Secret copied to clipboard');
    }
}

/**
 * Handle verification form submission
 */
async function handleVerify(event) {
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
        const response = await completeSetup(code);
        backupCodes = response.backup_codes;

        // Show backup codes
        renderBackupCodes();
    } catch (error) {
        let errorMessage = 'Failed to verify code. Please try again.';

        if (error instanceof ApiError) {
            if (error.status === 400 && error.data?.error === 'invalid_code') {
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
 * Render backup codes after successful setup
 */
function renderBackupCodes() {
    const container = document.getElementById('mfa-content');

    container.innerHTML = `
        <div class="mfa-backup-codes">
            <div class="mfa-success-icon">&#x2705;</div>
            <h2>Two-Factor Authentication Enabled</h2>

            <div class="mfa-backup-warning">
                <strong>Save your backup codes!</strong>
                <p>
                    These codes can be used to access your account if you lose your authenticator device.
                    Each code can only be used once. Store them somewhere safe.
                </p>
            </div>

            <div class="mfa-backup-codes__grid">
                ${backupCodes.map((code, index) => `
                    <div class="mfa-backup-code">
                        <span class="mfa-backup-code__number">${index + 1}.</span>
                        <code>${code}</code>
                    </div>
                `).join('')}
            </div>

            <div class="mfa-backup-actions">
                <button type="button" class="btn btn--secondary" id="copy-codes-btn">
                    Copy All Codes
                </button>
                <button type="button" class="btn btn--secondary" id="download-codes-btn">
                    Download Codes
                </button>
            </div>

            <div class="mfa-backup-confirm">
                <label class="form-checkbox">
                    <input type="checkbox" id="codes-saved-checkbox">
                    <span>I have saved my backup codes in a safe place</span>
                </label>
            </div>

            <button type="button" class="btn btn--primary btn--block" id="continue-btn" disabled>
                Continue to Dashboard
            </button>
        </div>
    `;

    // Bind event handlers
    document.getElementById('copy-codes-btn').addEventListener('click', copyAllCodes);
    document.getElementById('download-codes-btn').addEventListener('click', downloadCodes);
    document.getElementById('codes-saved-checkbox').addEventListener('change', handleCheckboxChange);
    document.getElementById('continue-btn').addEventListener('click', () => {
        toast.success('Two-factor authentication is now enabled!');
        navigate('/dashboard');
    });
}

/**
 * Copy all backup codes to clipboard
 */
async function copyAllCodes() {
    const codesText = backupCodes.join('\n');
    try {
        await navigator.clipboard.writeText(codesText);
        toast.success('Backup codes copied to clipboard');
    } catch (error) {
        toast.error('Failed to copy codes');
    }
}

/**
 * Download backup codes as a text file
 */
function downloadCodes() {
    const codesText = `Community Rapid Response - Backup Codes
Generated: ${new Date().toISOString()}

These codes can be used to access your account if you lose your authenticator device.
Each code can only be used once.

${backupCodes.map((code, i) => `${i + 1}. ${code}`).join('\n')}

Keep these codes safe and do not share them with anyone.
`;

    const blob = new Blob([codesText], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'community-rapid-response-backup-codes.txt';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    toast.success('Backup codes downloaded');
}

/**
 * Handle checkbox change for enabling continue button
 */
function handleCheckboxChange(event) {
    const continueBtn = document.getElementById('continue-btn');
    continueBtn.disabled = !event.target.checked;
}

export function cleanup() {
    setupData = null;
    backupCodes = null;
}

export default { render, cleanup };
