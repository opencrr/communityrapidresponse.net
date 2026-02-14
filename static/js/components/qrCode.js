/**
 * QR Code component
 * Generates QR codes for Meshtastic channel URLs
 */

// Side-effect import: sets the global QRCode constructor
import '../vendor/qrcode.min.js';

/**
 * Render a QR code into a container element
 * @param {HTMLElement} container - Container to render into
 * @param {string} text - Text to encode in QR code
 * @param {number} [size=200] - Size in pixels
 */
export async function renderQRCode(container, text, size = 200) {
    try {
        // Clear container
        container.innerHTML = '';

        // QRCode library creates a canvas or img element
        if (typeof QRCode !== 'undefined') {
            new QRCode(container, {
                text: text,
                width: size,
                height: size,
                colorDark: '#000000',
                colorLight: '#ffffff',
                correctLevel: QRCode.CorrectLevel.M,
            });
        } else {
            container.innerHTML = '<p class="text-muted">QR code generation unavailable</p>';
        }
    } catch (e) {
        console.error('Failed to render QR code:', e);
        container.innerHTML = '<p class="text-muted">QR code generation unavailable</p>';
    }
}
