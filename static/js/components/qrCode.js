/**
 * QR Code component
 * Generates QR codes for Meshtastic channel URLs
 *
 * The QR library uses `this` in a non-strict IIFE expecting window,
 * which breaks in ESM strict mode. Load it as a classic script instead.
 */

let qrLibLoaded = false;
let qrLibPromise = null;

function loadQRLibrary() {
    if (qrLibLoaded) return Promise.resolve();
    if (qrLibPromise) return qrLibPromise;

    qrLibPromise = new Promise((resolve, reject) => {
        const script = document.createElement('script');
        script.src = '/static/js/vendor/qrcode.min.js';
        script.onload = () => { qrLibLoaded = true; resolve(); };
        script.onerror = () => { qrLibPromise = null; reject(new Error('Failed to load QR code library')); };
        document.head.appendChild(script);
    });
    return qrLibPromise;
}

/**
 * Render a QR code into a container element
 * @param {HTMLElement} container - Container to render into
 * @param {string} text - Text to encode in QR code
 * @param {number} [size=200] - Size in pixels
 */
export async function renderQRCode(container, text, size = 200) {
    try {
        await loadQRLibrary();

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
