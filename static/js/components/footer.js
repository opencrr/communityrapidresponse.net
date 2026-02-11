/**
 * Footer component
 * Renders the site footer with links to legal pages, about, and contact.
 */

/**
 * Render the footer component
 */
export function renderFooter() {
    const footerElement = document.getElementById('footer');
    if (!footerElement) return;

    const currentYear = new Date().getFullYear();

    footerElement.className = 'footer';
    footerElement.innerHTML = `
        <div class="footer__container">
            <div class="footer__links">
                <a href="/privacy" class="footer__link" data-link>Privacy Policy</a>
                <span class="footer__separator">|</span>
                <a href="/terms" class="footer__link" data-link>Terms of Service</a>
                <span class="footer__separator">|</span>
                <a href="/about" class="footer__link" data-link>About</a>
                <span class="footer__separator">|</span>
                <a href="mailto:help@communityrapidresponse.net" class="footer__link">Contact</a>
                <span class="footer__separator">|</span>
                <a href="https://github.com/opencrr/communityrapidresponse.net" class="footer__link" target="_blank" rel="noopener noreferrer">Source Code</a>
            </div>
            <p class="footer__copyright">&copy; ${currentYear} Community Rapid Response</p>
        </div>
    `;
}

export default { renderFooter };
