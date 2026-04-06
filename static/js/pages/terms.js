/**
 * Terms of Service page
 * Defines the rules and conditions for using the platform.
 */

/**
 * Render the terms of service page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="help-content">
                <div class="page__header">
                    <h1 class="page__title">Terms of Service</h1>
                    <p class="page__effective-date">Effective: February 10, 2026</p>
                </div>

                <section class="help-section">
                    <h2>1. Acceptance of Terms</h2>
                    <p>
                        By accessing or using Community Rapid Response ("the Service"), you agree to be bound by
                        these Terms of Service. If you do not agree to these terms, do not use the Service. You
                        must be at least 13 years of age to use the Service.
                    </p>
                </section>

                <section class="help-section">
                    <h2>2. Description of Service</h2>
                    <p>
                        Community Rapid Response is a platform that connects verified neighbors and school
                        communities through Signal group chats. The Service facilitates geographic and
                        school-based community organization by verifying that members live where they say they
                        do or belong to the schools they claim. The Service is not affiliated with, endorsed by,
                        or connected to Signal Messenger LLC.
                    </p>
                </section>

                <section class="help-section">
                    <h2>3. Account Responsibilities</h2>
                    <p>When you create an account, you agree to:</p>
                    <ul class="help-list">
                        <li>Provide accurate and truthful information</li>
                        <li>Keep your login credentials secure and confidential</li>
                        <li>Maintain only one account per person</li>
                        <li>Not use automated tools, bots, or scripts to interact with the Service</li>
                        <li>Notify us promptly if you believe your account has been compromised</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>4. Verification and Membership</h2>
                    <p>
                        The Service uses verification to ensure community integrity:
                    </p>
                    <ul class="help-list">
                        <li><strong>Postcard verification</strong> requires a valid residential or business street address (PO Boxes and commercial mail receiving agencies are not accepted)</li>
                        <li><strong>Vouch verification</strong> requires vouches from members with verified addresses in the same community</li>
                        <li><strong>Admin status</strong> requires completion of both postcard and vouch verification</li>
                        <li><strong>School membership</strong> is verified through vouch-based verification by existing school members</li>
                    </ul>
                    <p>
                        Fraudulent verification &mdash; including providing false addresses, impersonating
                        residents, or colluding to vouch for people who are not genuine community members &mdash;
                        is a violation of these terms.
                    </p>
                </section>

                <section class="help-section">
                    <h2>5. Acceptable Use</h2>
                    <p>You agree not to use the Service to:</p>
                    <ul class="help-list">
                        <li>Harass, threaten, intimidate, or bully other users</li>
                        <li>Impersonate another person or misrepresent your identity</li>
                        <li>Send spam or unsolicited messages through the platform</li>
                        <li>Circumvent or attempt to circumvent the verification process</li>
                        <li>Share Signal group invite links with people outside your community's membership</li>
                        <li>Engage in any illegal activity or encourage others to do so</li>
                        <li>Attempt to gain unauthorized access to other accounts or platform systems</li>
                        <li>Interfere with or disrupt the Service or its infrastructure</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>6. Community Governance</h2>
                    <p>
                        The Service uses consensus-based governance to protect communities. Significant actions
                        &mdash; including blocklisting users, deleting community resources, and updating Signal
                        group invite links &mdash; require approval from three or more admins. This threshold
                        exists to prevent abuse of power and ensure community decisions reflect collective
                        agreement. Superusers maintain overall platform integrity and can act when consensus
                        mechanisms are insufficient.
                    </p>
                </section>

                <section class="help-section">
                    <h2>7. Account Termination</h2>
                    <ul class="help-list">
                        <li><strong>Community blocklisting</strong> &mdash; community admins may blocklist users through the consensus process described above</li>
                        <li><strong>Platform suspension</strong> &mdash; we reserve the right to suspend or terminate accounts that violate these terms</li>
                        <li><strong>Voluntary deletion</strong> &mdash; you may delete your account at any time from your Profile page</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>8. Signal Groups</h2>
                    <p>
                        The Service facilitates access to Signal group chats but is not responsible for the
                        content shared within those groups. Signal groups are governed by
                        <a href="https://signal.org/legal/" target="_blank" rel="noopener noreferrer">Signal's own Terms of Service</a>.
                        We do not monitor, store, or have access to messages sent in Signal groups. Users are
                        responsible for their conduct within Signal groups.
                    </p>
                </section>

                <section class="help-section">
                    <h2>9. Intellectual Property</h2>
                    <ul class="help-list">
                        <li>The Community Rapid Response platform, including its design, code, and branding, is our property</li>
                        <li>Content you create (such as your username and profile information) belongs to you</li>
                        <li>School and school district data is sourced from the National Center for Education Statistics (NCES) and is in the public domain</li>
                    </ul>
                </section>

                <section class="help-section">
                    <h2>10. Privacy</h2>
                    <p>
                        Your use of the Service is also governed by our
                        <a href="/privacy" data-link>Privacy Policy</a>, which describes how we collect, use,
                        and protect your information. By using the Service, you consent to the practices
                        described in the Privacy Policy.
                    </p>
                </section>

                <section class="help-section">
                    <h2>11. Disclaimers</h2>
                    <p>
                        The Service is provided "as is" and "as available" without warranties of any kind,
                        either express or implied. We do not guarantee that the Service will be uninterrupted,
                        error-free, or secure. We are not responsible for the conduct of any user, whether on
                        the platform or in Signal groups. We do not guarantee the accuracy of verification
                        processes or the trustworthiness of any community member.
                    </p>
                </section>

                <section class="help-section">
                    <h2>12. Limitation of Liability</h2>
                    <p>
                        Community Rapid Response is a self-funded community project. To the fullest extent
                        permitted by applicable law, we shall not be liable for any indirect, incidental,
                        special, consequential, or punitive damages, or any loss of data, use, or goodwill,
                        arising out of or related to your use of the Service.
                    </p>
                </section>

                <section class="help-section">
                    <h2>13. Modifications</h2>
                    <p>
                        We may update these Terms of Service from time to time. Changes will be posted on this
                        page with an updated effective date. Your continued use of the Service after changes
                        are posted constitutes acceptance of the updated terms.
                    </p>
                </section>

                <section class="help-section">
                    <h2>14. Governing Law</h2>
                    <p>
                        These Terms of Service are governed by and construed in accordance with the laws of the
                        State of Washington, USA, without regard to its conflict of law provisions.
                    </p>
                </section>

                <section class="help-section">
                    <h2>15. Contact</h2>
                    <p>
                        If you have questions about these Terms of Service, contact us at
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
