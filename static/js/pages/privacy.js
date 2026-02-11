/**
 * Privacy Policy page
 * Describes how the platform collects, uses, and protects user data.
 */

/**
 * Render the privacy policy page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="help-content">
                <div class="page__header">
                    <h1 class="page__title">Privacy Policy</h1>
                    <p class="page__effective-date">Effective: February 10, 2026</p>
                </div>

                <section class="help-section">
                    <h2>Introduction</h2>
                    <p>
                        Community Rapid Response ("we," "us," or "our") operates the communityrapidresponse.net
                        platform. This Privacy Policy explains how we collect, use, and protect your information
                        when you use our service. We are committed to minimizing data collection and maximizing
                        your privacy.
                    </p>
                    <p>
                        If you have questions about this policy, contact us at
                        <a href="mailto:help@communityrapidresponse.net">help@communityrapidresponse.net</a>.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Information We Collect</h2>
                    <p>We collect only the minimum information necessary to operate the platform:</p>
                    <ul class="help-list">
                        <li><strong>Username and email address</strong> &mdash; for account login and notifications</li>
                        <li><strong>Hashed password</strong> &mdash; securely hashed with bcrypt (cost factor 12); we never store your plaintext password</li>
                        <li><strong>Verification status</strong> &mdash; whether you have completed postcard and/or vouch verification</li>
                        <li><strong>Encrypted MFA secrets</strong> &mdash; if you enable two-factor authentication, your TOTP secret is encrypted with AES-256-GCM before storage</li>
                        <li><strong>Community and school memberships</strong> &mdash; which regions, schools, and districts you belong to</li>
                        <li><strong>Audit logs</strong> &mdash; records of administrative actions, retained for 90 days</li>
                    </ul>

                    <div class="help-highlight">
                        <h3>Zero Address Storage</h3>
                        <p>
                            When you verify your address, it exists in our server's memory only long enough to
                            validate it and send you a postcard. Your address is <strong>never written to our
                            database</strong>. Once the postcard mailing request is sent, your address is gone from
                            our system entirely. Even our administrators cannot retrieve it.
                        </p>
                    </div>
                </section>

                <section class="help-section">
                    <h2>Information We Do NOT Collect</h2>
                    <p>We deliberately avoid collecting:</p>
                    <ul class="help-list">
                        <li><strong>Street addresses, apartment numbers, or specific coordinates</strong> &mdash; never stored in our database</li>
                        <li><strong>Signal phone numbers or messages</strong> &mdash; we facilitate access to Signal groups but have no access to your conversations</li>
                        <li><strong>Browsing analytics or tracking data</strong> &mdash; we do not use analytics cookies, tracking pixels, or fingerprinting</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>How We Use Your Information</h2>
                    <p>We use the information we collect to:</p>
                    <ul class="help-list">
                        <li><strong>Authenticate your account</strong> &mdash; verify your identity when you log in</li>
                        <li><strong>Process verifications</strong> &mdash; handle postcard and vouch verification workflows</li>
                        <li><strong>Send email notifications</strong> &mdash; notify you of governance actions, invite link changes, and account events (sensitive information like invite links is never included in emails)</li>
                        <li><strong>Enforce governance</strong> &mdash; support consensus-based admin actions such as blocklisting and deletion proposals</li>
                        <li><strong>Rate limiting</strong> &mdash; prevent abuse of verification, vouching, and reporting features</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>Cookies</h2>
                    <p>We use only essential cookies required for the platform to function:</p>
                    <ul class="help-list">
                        <li><strong>Authentication cookie</strong> &mdash; an httpOnly JWT cookie (24-hour expiration) that keeps you logged in</li>
                        <li><strong>CSRF cookie</strong> &mdash; protects against cross-site request forgery attacks</li>
                    </ul>
                    <p>
                        We do <strong>not</strong> use tracking cookies, advertising cookies, or analytics cookies of any kind.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Third-Party Services</h2>
                    <p>We use a limited number of third-party services to operate the platform:</p>
                    <ul class="help-list">
                        <li><strong>Lob</strong> &mdash; sends verification postcards on our behalf. Lob retains address data for up to 90 days per their retention policy. We do not control Lob's retention.</li>
                        <li><strong>Mapbox</strong> &mdash; provides geocoding (address-to-coordinates conversion) and map display. Mapbox processes addresses during geocoding requests.</li>
                        <li><strong>Sentry</strong> &mdash; captures application errors to help us fix bugs. We configure Sentry to minimize personally identifiable information in error reports.</li>
                    </ul>
                    <p>
                        We do <strong>not</strong> sell, rent, or share your data with advertisers, data brokers, or any other third parties.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Data Retention</h2>
                    <ul class="help-list">
                        <li><strong>Account data</strong> &mdash; retained while your account is active</li>
                        <li><strong>Audit logs</strong> &mdash; retained for 90 days, then automatically deleted</li>
                        <li><strong>Lob address data</strong> &mdash; retained by Lob for up to 90 days per their policy</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>Account Deletion</h2>
                    <p>
                        You can delete your account at any time from your <a href="/profile" data-link>Profile</a> page.
                        When you delete your account:
                    </p>
                    <ul class="help-list">
                        <li>If you are not blocked, your account and all associated data are permanently deleted</li>
                        <li>If you have been blocked by a community, your personal information (username, email, password, MFA secrets) is scrubbed, but a minimal record is retained to enforce the block</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>Data Security</h2>
                    <p>We employ multiple layers of security to protect your data:</p>
                    <ul class="help-list">
                        <li><strong>Password hashing</strong> &mdash; bcrypt with cost factor 12</li>
                        <li><strong>MFA encryption</strong> &mdash; AES-256-GCM encryption for TOTP secrets</li>
                        <li><strong>HTTPS</strong> &mdash; all connections are encrypted in transit</li>
                        <li><strong>Rate limiting</strong> &mdash; protects against brute-force and abuse</li>
                        <li><strong>httpOnly cookies</strong> &mdash; prevents JavaScript access to authentication tokens</li>
                        <li><strong>CSRF protection</strong> &mdash; guards against cross-site request forgery</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>Children's Privacy</h2>
                    <p>
                        Community Rapid Response is not intended for use by anyone under the age of 13. We do not
                        knowingly collect personal information from children under 13. If we discover that we have
                        collected information from a child under 13, we will delete that information promptly. If
                        you believe a child under 13 has provided us with personal information, please contact us
                        at <a href="mailto:help@communityrapidresponse.net">help@communityrapidresponse.net</a>.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Changes to This Policy</h2>
                    <p>
                        We may update this Privacy Policy from time to time. Changes will be posted on this page
                        with an updated effective date. Your continued use of the platform after changes are posted
                        constitutes acceptance of the updated policy.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Contact</h2>
                    <p>
                        If you have questions or concerns about this Privacy Policy, contact us at
                        <a href="mailto:help@communityrapidresponse.net">help@communityrapidresponse.net</a>.
                    </p>
                </section>
            </div>
        </div>
    `;
}

/**
 * Cleanup when leaving the page
 */
export function cleanup() {
    // No cleanup needed for static content
}

export default { render, cleanup };
