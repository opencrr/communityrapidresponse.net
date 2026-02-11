/**
 * Toast notification component
 * Shows non-blocking notifications for success, error, warning, and info messages.
 */

const TOAST_DURATION = 5000; // 5 seconds
const TOAST_CONTAINER_ID = 'toast-container';

let toastIdCounter = 0;

/**
 * Create and show a toast notification
 * @param {Object} options - Toast options
 * @param {string} options.message - Toast message
 * @param {string} [options.title] - Optional toast title
 * @param {string} [options.type='info'] - Toast type: success, error, warning, info
 * @param {number} [options.duration=5000] - Duration in ms (0 for no auto-dismiss)
 * @returns {number} Toast ID for manual dismissal
 */
export function showToast({ message, title, type = 'info', duration = TOAST_DURATION }) {
    const container = document.getElementById(TOAST_CONTAINER_ID);
    if (!container) {
        console.error('Toast container not found');
        return -1;
    }

    const toastId = ++toastIdCounter;
    const toast = document.createElement('div');
    toast.className = `toast toast--${type}`;
    toast.dataset.toastId = toastId;

    const iconMap = {
        success: '\u2713', // checkmark
        error: '\u2717',   // x mark
        warning: '\u26A0', // warning triangle
        info: '\u2139',    // info circle
    };

    toast.innerHTML = `
        <div class="toast__icon">${iconMap[type] || iconMap.info}</div>
        <div class="toast__content">
            ${title ? `<div class="toast__title">${escapeHtml(title)}</div>` : ''}
            <div class="toast__message">${escapeHtml(message)}</div>
        </div>
        <button class="toast__close" aria-label="Dismiss">&times;</button>
    `;

    // Add close handler
    const closeButton = toast.querySelector('.toast__close');
    closeButton.addEventListener('click', () => dismissToast(toastId));

    container.appendChild(toast);

    // Auto-dismiss
    if (duration > 0) {
        setTimeout(() => dismissToast(toastId), duration);
    }

    return toastId;
}

/**
 * Dismiss a toast by ID
 * @param {number} toastId - Toast ID to dismiss
 */
export function dismissToast(toastId) {
    const container = document.getElementById(TOAST_CONTAINER_ID);
    if (!container) return;

    const toast = container.querySelector(`[data-toast-id="${toastId}"]`);
    if (toast) {
        toast.style.animation = 'toast-slide-out 0.2s ease forwards';
        setTimeout(() => toast.remove(), 200);
    }
}

/**
 * Dismiss all toasts
 */
export function dismissAllToasts() {
    const container = document.getElementById(TOAST_CONTAINER_ID);
    if (!container) return;

    container.innerHTML = '';
}

// Convenience methods
export function success(message, title = null) {
    return showToast({ message, title, type: 'success' });
}

export function error(message, title = 'Error') {
    return showToast({ message, title, type: 'error', duration: 8000 });
}

export function warning(message, title = null) {
    return showToast({ message, title, type: 'warning' });
}

export function info(message, title = null) {
    return showToast({ message, title, type: 'info' });
}

/**
 * Escape HTML to prevent XSS
 * @param {string} text - Text to escape
 * @returns {string} Escaped text
 */
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Add slide-out animation to stylesheet
const style = document.createElement('style');
style.textContent = `
    @keyframes toast-slide-out {
        from {
            transform: translateX(0);
            opacity: 1;
        }
        to {
            transform: translateX(100%);
            opacity: 0;
        }
    }
`;
document.head.appendChild(style);

export default {
    showToast,
    dismissToast,
    dismissAllToasts,
    success,
    error,
    warning,
    info,
};
