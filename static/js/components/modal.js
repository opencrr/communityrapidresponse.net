/**
 * Modal dialog component
 * Provides a reusable modal system with customizable content.
 */

const MODAL_CONTAINER_ID = 'modal-container';

let currentModal = null;

/**
 * Show a modal dialog
 * @param {Object} options - Modal options
 * @param {string} options.title - Modal title
 * @param {string|HTMLElement} options.content - Modal body content (HTML string or element)
 * @param {Object[]} [options.actions] - Array of action button configs
 * @param {string} options.actions[].label - Button label
 * @param {string} [options.actions[].type='secondary'] - Button type (primary, secondary, danger)
 * @param {Function} options.actions[].onClick - Click handler
 * @param {boolean} [options.actions[].closeOnClick=true] - Close modal on click
 * @param {boolean} [options.closeOnBackdrop=true] - Close when clicking backdrop
 * @param {boolean} [options.showClose=true] - Show close button
 * @param {Function} [options.onClose] - Callback when modal closes
 * @returns {Object} Modal control object with close() method
 */
export function showModal(options) {
    const {
        title,
        content,
        actions = [],
        closeOnBackdrop = true,
        showClose = true,
        onClose,
    } = options;

    // Close existing modal
    if (currentModal) {
        closeModal();
    }

    const container = document.getElementById(MODAL_CONTAINER_ID);
    if (!container) {
        console.error('Modal container not found');
        return null;
    }

    const backdrop = document.createElement('div');
    backdrop.className = 'modal-backdrop';

    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.setAttribute('role', 'dialog');
    modal.setAttribute('aria-modal', 'true');
    modal.setAttribute('aria-labelledby', 'modal-title');

    // Build modal HTML
    let modalHtml = `
        <div class="modal__header">
            <h2 class="modal__title" id="modal-title">${escapeHtml(title)}</h2>
            ${showClose ? '<button class="modal__close" aria-label="Close">&times;</button>' : ''}
        </div>
        <div class="modal__body">
            ${typeof content === 'string' ? content : ''}
        </div>
    `;

    if (actions.length > 0) {
        modalHtml += `<div class="modal__footer"></div>`;
    }

    modal.innerHTML = modalHtml;

    // If content is an element, append it
    if (content instanceof HTMLElement) {
        modal.querySelector('.modal__body').appendChild(content);
    }

    // Add action buttons
    if (actions.length > 0) {
        const footer = modal.querySelector('.modal__footer');
        actions.forEach(action => {
            const button = document.createElement('button');
            button.className = `btn btn--${action.type || 'secondary'}`;
            button.textContent = action.label;
            button.addEventListener('click', () => {
                if (action.onClick) {
                    action.onClick();
                }
                if (action.closeOnClick !== false) {
                    closeModal();
                }
            });
            footer.appendChild(button);
        });
    }

    backdrop.appendChild(modal);

    // Close button handler
    const closeButton = modal.querySelector('.modal__close');
    if (closeButton) {
        closeButton.addEventListener('click', closeModal);
    }

    // Backdrop click handler
    if (closeOnBackdrop) {
        backdrop.addEventListener('click', (event) => {
            if (event.target === backdrop) {
                closeModal();
            }
        });
    }

    // Escape key handler
    const handleEscape = (event) => {
        if (event.key === 'Escape') {
            closeModal();
        }
    };
    document.addEventListener('keydown', handleEscape);

    container.appendChild(backdrop);

    // Store current modal state
    currentModal = {
        backdrop,
        onClose,
        handleEscape,
    };

    // Focus the modal
    modal.focus();

    // Prevent body scroll
    document.body.style.overflow = 'hidden';

    return {
        close: closeModal,
        getElement: () => modal,
    };
}

/**
 * Close the current modal
 */
export function closeModal() {
    if (!currentModal) return;

    const { backdrop, onClose, handleEscape } = currentModal;

    // Remove escape handler
    document.removeEventListener('keydown', handleEscape);

    // Animate out
    backdrop.style.animation = 'modal-fade-out 0.15s ease forwards';
    const modal = backdrop.querySelector('.modal');
    if (modal) {
        modal.style.animation = 'modal-slide-out 0.15s ease forwards';
    }

    setTimeout(() => {
        backdrop.remove();
        document.body.style.overflow = '';
        if (onClose) {
            onClose();
        }
    }, 150);

    currentModal = null;
}

/**
 * Show a confirmation dialog
 * @param {Object} options - Confirmation options
 * @param {string} options.title - Dialog title
 * @param {string} options.message - Confirmation message
 * @param {string} [options.confirmLabel='Confirm'] - Confirm button label
 * @param {string} [options.cancelLabel='Cancel'] - Cancel button label
 * @param {string} [options.confirmType='primary'] - Confirm button type
 * @returns {Promise<boolean>} Resolves to true if confirmed, false if cancelled
 */
export function confirm({ title, message, confirmLabel = 'Confirm', cancelLabel = 'Cancel', confirmType = 'primary' }) {
    return new Promise((resolve) => {
        showModal({
            title,
            content: `<p>${escapeHtml(message)}</p>`,
            closeOnBackdrop: false,
            actions: [
                {
                    label: cancelLabel,
                    type: 'secondary',
                    onClick: () => resolve(false),
                },
                {
                    label: confirmLabel,
                    type: confirmType,
                    onClick: () => resolve(true),
                },
            ],
            onClose: () => resolve(false),
        });
    });
}

/**
 * Show an alert dialog
 * @param {Object} options - Alert options
 * @param {string} options.title - Dialog title
 * @param {string} options.message - Alert message
 * @param {string} [options.buttonLabel='OK'] - Button label
 * @returns {Promise<void>} Resolves when closed
 */
export function alert({ title, message, buttonLabel = 'OK' }) {
    return new Promise((resolve) => {
        showModal({
            title,
            content: `<p>${escapeHtml(message)}</p>`,
            actions: [
                {
                    label: buttonLabel,
                    type: 'primary',
                    onClick: () => resolve(),
                },
            ],
            onClose: () => resolve(),
        });
    });
}

/**
 * Show a prompt dialog with text input
 * @param {Object} options - Prompt options
 * @param {string} options.title - Dialog title
 * @param {string} options.message - Prompt message
 * @param {string} [options.inputLabel] - Label for the input field
 * @param {string} [options.inputPlaceholder=''] - Placeholder for the input
 * @param {string} [options.defaultValue=''] - Default input value
 * @param {string} [options.confirmLabel='Submit'] - Confirm button label
 * @param {string} [options.cancelLabel='Cancel'] - Cancel button label
 * @param {string} [options.confirmType='primary'] - Confirm button type
 * @returns {Promise<string|null>} Resolves to input value if confirmed, null if cancelled
 */
export function prompt({
    title,
    message,
    inputLabel = '',
    inputPlaceholder = '',
    defaultValue = '',
    confirmLabel = 'Submit',
    cancelLabel = 'Cancel',
    confirmType = 'primary',
}) {
    return new Promise((resolve) => {
        let inputValue = defaultValue;

        const contentHtml = `
            <p>${escapeHtml(message)}</p>
            <div class="form-group" style="margin-top: 1rem;">
                ${inputLabel ? `<label class="form-label">${escapeHtml(inputLabel)}</label>` : ''}
                <input
                    type="text"
                    class="form-input modal-prompt-input"
                    placeholder="${escapeHtml(inputPlaceholder)}"
                    value="${escapeHtml(defaultValue)}"
                >
            </div>
        `;

        const modalControl = showModal({
            title,
            content: contentHtml,
            closeOnBackdrop: false,
            actions: [
                {
                    label: cancelLabel,
                    type: 'secondary',
                    onClick: () => resolve(null),
                },
                {
                    label: confirmLabel,
                    type: confirmType,
                    onClick: () => resolve(inputValue),
                },
            ],
            onClose: () => resolve(null),
        });

        // Set up input handler
        const input = modalControl.getElement().querySelector('.modal-prompt-input');
        if (input) {
            input.addEventListener('input', (event) => {
                inputValue = event.target.value;
            });
            // Focus the input
            setTimeout(() => input.focus(), 100);
            // Submit on Enter
            input.addEventListener('keydown', (event) => {
                if (event.key === 'Enter') {
                    event.preventDefault();
                    modalControl.close();
                    resolve(inputValue);
                }
            });
        }
    });
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

// Add animations to stylesheet
const style = document.createElement('style');
style.textContent = `
    @keyframes modal-fade-out {
        from { opacity: 1; }
        to { opacity: 0; }
    }
    @keyframes modal-slide-out {
        from {
            transform: translateY(0);
            opacity: 1;
        }
        to {
            transform: translateY(-20px);
            opacity: 0;
        }
    }
`;
document.head.appendChild(style);

export default {
    showModal,
    closeModal,
    confirm,
    alert,
    prompt,
};
