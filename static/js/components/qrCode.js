/**
 * QR Code component
 * Generates QR codes for Meshtastic channel URLs
 */

let qrCodeLibLoaded = false;
let qrCodeLibLoading = false;
let qrCodeLibCallbacks = [];

/**
 * Ensure QR code library is loaded
 */
function ensureQRLibrary() {
    return new Promise((resolve, reject) => {
        if (qrCodeLibLoaded) {
            resolve();
            return;
        }

        qrCodeLibCallbacks.push({ resolve, reject });

        if (qrCodeLibLoading) return;
        qrCodeLibLoading = true;

        const script = document.createElement('script');
        script.src = '/static/vendor/qrcode.min.js';
        script.onload = () => {
            qrCodeLibLoaded = true;
            qrCodeLibCallbacks.forEach(cb => cb.resolve());
            qrCodeLibCallbacks = [];
        };
        script.onerror = () => {
            qrCodeLibCallbacks.forEach(cb => cb.reject(new Error('Failed to load QR code library')));
            qrCodeLibCallbacks = [];
            qrCodeLibLoading = false;
        };
        document.head.appendChild(script);
    });
}

/**
 * Render a QR code into a container element
 * @param {HTMLElement} container - Container to render into
 * @param {string} text - Text to encode in QR code
 * @param {number} [size=200] - Size in pixels
 */
export async function renderQRCode(container, text, size = 200) {
    try {
        await ensureQRLibrary();

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
