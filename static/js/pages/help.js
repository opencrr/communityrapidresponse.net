/**
 * Help/FAQ page
 * Provides information about the platform and answers common questions.
 * Uses a tabbed layout: General, Communities, Schools, Signal Safety.
 */

/**
 * Render the help page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="help-content">
                <div class="page__header">
                    <h1 class="page__title">Help & FAQ</h1>
                    <p class="page__description">
                        Learn how Community Rapid Response works and get answers to common questions.
                    </p>
                </div>

                <div class="tabs" id="help-tabs">
                    <button class="tab tab--active" data-tab="general">General</button>
                    <button class="tab" data-tab="regions">Communities</button>
                    <button class="tab" data-tab="schools">Schools</button>
                    <button class="tab" data-tab="signal-safety">Signal Safety</button>
                </div>

                <!-- General Tab -->
                <div class="help-tab-content" id="tab-general">
                    <section class="help-section">
                        <h2>What is Community Rapid Response?</h2>
                        <p>
                            Community Rapid Response is a platform that connects verified neighbors and
                            school communities through secure Signal group chats. We help communities
                            organize by creating geographic-based groups where members have proven they
                            live in the area, and school-based groups where members are verified by
                            their peers.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>Privacy & Security</h2>
                        <div class="help-highlight">
                            <h3>Your address is not stored in our database</h3>
                            <p>
                                When you request postcard verification, your address exists only in our
                                server's memory long enough to send it to our mailing partner. It is
                                immediately discarded and never written to our database. Even our
                                administrators cannot retrieve your address from our system.
                            </p>
                        </div>
                        <div class="help-card" style="margin: var(--space-4) 0;">
                            <h3>About Our Mailing Partner</h3>
                            <p>
                                We use <a href="https://www.lob.com" target="_blank" rel="noopener">Lob</a>
                                to deliver verification postcards. When you submit your address, it is sent to
                                Lob for mail delivery. Lob automatically deletes address data after 90 days,
                                aligning with our commitment to minimize data retention.
                            </p>
                            <p style="margin-top: var(--space-2); margin-bottom: 0;">
                                <a href="https://www.lob.com/privacy" target="_blank" rel="noopener">
                                    View Lob's Privacy Policy
                                </a>
                            </p>
                        </div>
                        <ul class="help-list">
                            <li>We only store your community membership (e.g., "member of Downtown neighborhood")</li>
                            <li>All accounts require multi-factor authentication (MFA)</li>
                            <li>Signal group invite links are only visible to verified members</li>
                            <li>Sensitive data is never sent via email</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h2>Frequently Asked Questions</h2>

                        <details class="faq-item">
                            <summary>What is Signal and why do you use it?</summary>
                            <p>
                                <a href="https://signal.org" target="_blank" rel="noopener">Signal</a> is a
                                secure, end-to-end encrypted messaging app. We use Signal groups because they
                                provide strong privacy protections and are independent of our platform - your
                                conversations stay private even from us.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Who can see my information?</summary>
                            <p>
                                Other verified members in your community or school can see your username.
                                Community admins can see your email for coordination purposes. No one can
                                see your physical address because we don't store it.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How do I become an admin?</summary>
                            <p>
                                <strong>For communities:</strong> Complete both postcard verification AND vouch
                                verification. Once you have both, you automatically gain admin rights for
                                your verified community.
                            </p>
                            <p>
                                <strong>For schools:</strong> Join a school and receive enough vouches from
                                verified school members. In bootstrap mode (fewer than 3 admins), you need
                                3 vouches; otherwise, 2 vouches are sufficient.
                            </p>
                        </details>
                    </section>

                    <section class="help-section">
                        <h2>Need More Help?</h2>
                        <p>
                            If you have questions not answered here, you can:
                        </p>
                        <ul class="help-list">
                            <li>Reach out to a local admin in your community or school</li>
                            <li>Contact us at <a href="mailto:help@communityrapidresponse.net">help@communityrapidresponse.net</a></li>
                            <li>Browse the <a href="https://github.com/opencrr/communityrapidresponse.net" target="_blank" rel="noopener noreferrer">source code on GitHub</a> &mdash; this project is open source under the <a href="https://opensource.org/licenses/MIT" target="_blank" rel="noopener noreferrer">MIT License</a></li>
                        </ul>
                    </section>
                </div>

                <!-- Communities Tab -->
                <div class="help-tab-content" id="tab-regions" style="display: none;">
                    <section class="help-section">
                        <h2>How Community Verification Works</h2>
                        <p>
                            We use a dual verification system to ensure community members are genuine neighbors:
                        </p>
                        <div class="help-cards">
                            <div class="help-card">
                                <h3>Postcard Verification</h3>
                                <p>
                                    We mail a postcard with a unique code to your address. Enter the code
                                    to prove you can receive mail there. <strong>Your address is never stored</strong> -
                                    it's only used to send the postcard.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Vouch Verification</h3>
                                <p>
                                    Get vouched for by existing verified members who know you live in the area.
                                    This builds trust through community connections. The number of vouches required
                                    depends on whether your community is in bootstrap mode (see below).
                                </p>
                            </div>
                        </div>
                        <p>
                            Complete <strong>both</strong> verifications to become a full admin who can create
                            communities and manage groups. Complete <strong>either one</strong> to access Signal groups.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>Geographic Hierarchy</h2>
                        <p>
                            Communities are organized in a hierarchy from broad to specific:
                        </p>
                        <ul class="help-list">
                            <li><strong>State</strong> - Top-level boundary</li>
                            <li><strong>County</strong> - County/district within a state</li>
                            <li><strong>City/Town</strong> - Municipal boundary</li>
                            <li><strong>Locality</strong> - Borough/sub-city (optional, e.g., Brooklyn in NYC)</li>
                            <li><strong>Neighborhood</strong> - Named neighborhood area</li>
                            <li><strong>City Block</strong> - Most specific level</li>
                        </ul>
                        <p>
                            When you verify your address, you are automatically assigned to the most specific
                            community that contains your location.
                        </p>
                    </section>

                    <section class="help-section">
                        <div class="help-highlight">
                            <h3>Bootstrap Mode for New Communities</h3>
                            <p>
                                New communities with fewer than 3 full admins operate in "bootstrap mode" with special rules
                                to help establish the initial community:
                            </p>
                            <ul class="help-list" style="margin-top: var(--space-2);">
                                <li><strong>3 vouches required</strong> instead of the normal 2</li>
                                <li><strong>Postcard-verified users can vouch</strong> (normally requires both verifications)</li>
                                <li><strong>1-hour cooldown</strong> between vouches from the same person</li>
                                <li><strong>Auto-eligibility:</strong> Postcard-verified users are automatically eligible for vouches without requesting</li>
                            </ul>
                            <p style="margin-top: var(--space-2); margin-bottom: 0;">
                                Once your community has 3 or more full admins, normal verification rules apply.
                            </p>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>Community FAQ</h2>

                        <details class="faq-item">
                            <summary>Why can't I use a PO Box for verification?</summary>
                            <p>
                                PO Boxes don't prove residential location - they can be obtained without
                                living in the area. We need to verify you actually reside in the community
                                you're joining.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How long does the postcard take to arrive?</summary>
                            <p>
                                Postcards typically arrive within 3-5 business days via USPS. The verification
                                code is valid for 30 days.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>I didn't receive my postcard. What should I do?</summary>
                            <p>
                                You can request a new postcard after waiting at least 7 days. Note that you're
                                limited to 3 verification requests per 30 days to prevent abuse.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How do I find someone to vouch for me?</summary>
                            <p>
                                Connect with verified neighbors through community events, local social media,
                                or mutual friends. You need 2-3 different verified members from your community to
                                vouch for you (3 in bootstrap mode, 2 otherwise). In bootstrap mode, postcard-verified
                                users can vouch; otherwise, vouchers must have both verifications.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What is circular vouching and why isn't it allowed?</summary>
                            <p>
                                Circular vouching occurs when two people vouch for each other (A vouches for B,
                                then B tries to vouch for A). This is not allowed because it could enable two
                                bad actors to verify each other without legitimate community connections. Each
                                vouch must come from someone who hasn't received a vouch from you.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How are decisions made about the community?</summary>
                            <p>
                                Important actions like updating Signal group links or removing members require
                                consensus from at least 3 admins. This prevents any single person from making
                                unilateral decisions.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What is bootstrap mode?</summary>
                            <p>
                                Bootstrap mode helps new communities establish their first admins. When a community
                                has fewer than 3 full admins, the verification requirements are adjusted:
                                postcard-only verified users can vouch for others, but 3 vouches are required
                                instead of 2, and there's a 1-hour cooldown between vouches from the same person.
                                This prevents a single bad actor from quickly verifying fake accounts while still
                                allowing new communities to grow.
                            </p>
                        </details>
                    </section>
                </div>

                <!-- Schools Tab -->
                <div class="help-tab-content" id="tab-schools" style="display: none;">
                    <section class="help-section">
                        <h2>What are Schools?</h2>
                        <p>
                            Schools are communities organized around educational institutions. School data
                            comes from the National Center for Education Statistics (NCES), which provides
                            comprehensive information about schools across the United States. Schools operate
                            independently from geographic communities - you can be part of both local and school
                            communities simultaneously.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>How to Join a School</h2>
                        <ol class="help-list">
                            <li>Search for your school by name or state on the Schools page</li>
                            <li>Click "Join" on your school's page</li>
                            <li>Your membership starts as "pending" until you are verified</li>
                            <li>Connect with existing verified members who can vouch for you</li>
                            <li>Once you receive enough vouches, you become a verified member</li>
                        </ol>
                    </section>

                    <section class="help-section">
                        <h2>School Verification</h2>
                        <p>
                            School verification is vouch-based (no postcard required). Verified members
                            of a school vouch for new members to confirm their connection to the school.
                        </p>
                        <div class="help-highlight">
                            <h3>Bootstrap Mode vs Normal Mode</h3>
                            <ul class="help-list" style="margin-top: var(--space-2);">
                                <li><strong>Bootstrap mode</strong> (school has fewer than 3 admins): <strong>3 vouches</strong> required, and any verified member can vouch</li>
                                <li><strong>Normal mode</strong> (school has 3+ admins): <strong>2 vouches</strong> required from admin members</li>
                            </ul>
                            <p style="margin-top: var(--space-2); margin-bottom: 0;">
                                Bootstrap mode helps new school communities grow by lowering the barrier
                                for vouching while still requiring more vouches for safety.
                            </p>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>School Districts</h2>
                        <p>
                            School districts group multiple schools together by geographic area. Districts
                            can have their own Signal groups for district-wide communication, separate from
                            individual school groups. If your school belongs to a district, you may also
                            have access to district-level groups.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>School FAQ</h2>

                        <details class="faq-item">
                            <summary>How is school data sourced?</summary>
                            <p>
                                School and district data comes from the National Center for Education Statistics
                                (NCES), a part of the U.S. Department of Education. This ensures accurate and
                                comprehensive coverage of schools across the country.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Can I join multiple schools?</summary>
                            <p>
                                Yes, you can join as many schools as you like. Each school has its own
                                independent membership and verification process.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What is a school district?</summary>
                            <p>
                                A school district is an administrative grouping of schools, typically by
                                geographic area. Districts can be unified (all grade levels), elementary,
                                or secondary. District-level Signal groups enable communication across
                                all schools in the district.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How do school Signal groups work?</summary>
                            <p>
                                School admins can create Signal groups for their school community. Only
                                verified school members can see the invite links. Schools and districts
                                can each have up to 5 Signal groups. These are completely independent
                                from community-based Signal groups.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Do I need postcard verification for schools?</summary>
                            <p>
                                No. School verification is entirely vouch-based. You just need other verified
                                school members to vouch for you. Postcard verification is only used for
                                geographic community verification.
                            </p>
                        </details>
                    </section>
                </div>

                <!-- Signal Safety Tab -->
                <div class="help-tab-content" id="tab-signal-safety" style="display: none;">
                    <section class="help-section">
                        <h2>Understanding Signal's Security</h2>
                        <p>
                            Signal uses <strong>end-to-end encryption</strong>, which means messages are
                            encrypted on your device and can only be decrypted by the intended recipients.
                            Signal (the company) cannot read your messages, and neither can we.
                        </p>
                        <p>
                            However, there is an important limitation: <strong>anyone already in a group can
                            read, screenshot, or forward messages</strong>. Encryption protects data in transit
                            and at rest on Signal's servers, but it does not prevent someone with access to a
                            device in the group from seeing the conversation.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>Protect Your Account</h2>
                        <div class="help-cards">
                            <div class="help-card">
                                <h3>Set a Signal PIN</h3>
                                <p>
                                    Protects your account registration and prevents someone from re-registering
                                    your phone number. Go to Settings > Account > Signal PIN.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Enable Registration Lock</h3>
                                <p>
                                    Prevents someone from registering your phone number on a new device without
                                    your PIN. Go to Settings > Account > Registration Lock.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Use App Lock</h3>
                                <p>
                                    Enable biometric or passcode lock so a lost or stolen phone can't be used
                                    to access your chats. Go to Settings > Privacy > App Lock.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Hide Lock Screen Previews</h3>
                                <p>
                                    Configure Signal to hide message content in lock screen notifications.
                                    Go to Settings > Notifications > Show > "No name or message."
                                </p>
                            </div>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>Protect Your Identity</h2>
                        <div class="help-highlight">
                            <h3>Group Members May See Your Phone Number</h3>
                            <p>
                                Depending on your privacy settings, other group members may be able to see
                                your phone number. Review and adjust your settings to control this.
                            </p>
                        </div>
                        <ul class="help-list">
                            <li>
                                Use a <strong>Signal username</strong> for discovery instead of sharing your
                                phone number directly
                            </li>
                            <li>
                                <strong>Phone number privacy:</strong> Go to Settings > Privacy > Phone Number
                                and set "Who can find me by my number" to "Nobody"
                            </li>
                            <li>
                                <strong>Profile name:</strong> Choose carefully — your profile name is visible
                                to everyone in groups you join. Avoid using your full legal name if privacy is
                                a concern
                            </li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h2>Safe Practices in Group Chats</h2>
                        <ul class="help-list">
                            <li>Enable <strong>disappearing messages</strong> — we recommend 1 week as a default timer</li>
                            <li>Don't share sensitive personal info (home address, daily routine, financial details)</li>
                            <li>Be cautious of links — verify with the sender through another channel if something looks suspicious</li>
                            <li>Don't forward group invite links outside the platform</li>
                            <li>Remember: anyone in the group can screenshot or forward messages</li>
                            <li>If you see something concerning, report it to a community or school admin</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h2>Recognizing Threats</h2>
                        <div class="help-cards">
                            <div class="help-card">
                                <h3>Social Engineering</h3>
                                <p>
                                    People may pose as trusted figures (admins, neighbors, school staff) to extract
                                    personal information. Verify identities through known channels before sharing
                                    anything sensitive.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Phishing Links</h3>
                                <p>
                                    Suspicious links designed to steal credentials or install malware. Long-press
                                    or hover to preview URLs before clicking. Legitimate services won't ask for
                                    your password via group chat.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Impersonation</h3>
                                <p>
                                    Someone using a similar name or photo as a known community member. If a message
                                    seems out of character, verify through the platform or a separate trusted channel.
                                </p>
                            </div>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>If Something Goes Wrong</h2>
                        <div class="help-highlight">
                            <ul class="help-list">
                                <li>
                                    <strong>Suspect a bad actor in a group:</strong> Report to community or school
                                    admins immediately
                                </li>
                                <li>
                                    <strong>Admins can help:</strong> The platform's consensus-based removal process
                                    can blacklist malicious users from groups
                                </li>
                                <li>
                                    <strong>Account may be compromised:</strong> Change your Signal PIN, enable
                                    Registration Lock, and notify your community admins
                                </li>
                                <li>
                                    <strong>Unwanted messages from a group member:</strong> Use Signal's block
                                    feature and report to admins
                                </li>
                            </ul>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>Signal Safety FAQ</h2>

                        <details class="faq-item">
                            <summary>Can group admins read my private messages?</summary>
                            <p>
                                No. Group messages and 1:1 messages are completely separate. No one on this
                                platform can see your private conversations.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Should I enable disappearing messages?</summary>
                            <p>
                                Yes, it's strongly recommended. Disappearing messages limit exposure if a device
                                is compromised. You can set timers from 30 seconds to 4 weeks. We recommend
                                1 week as a good default.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Can someone who was removed from a group still see old messages?</summary>
                            <p>
                                Messages they already received remain on their device, but they won't receive
                                new messages. This is why disappearing messages are important.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What if I change my phone number?</summary>
                            <p>
                                Use Signal's built-in "Change Number" feature (Settings > Account > Change Phone
                                Number) rather than creating a new account. This preserves your group memberships
                                and contacts.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How do I verify I'm talking to the right person?</summary>
                            <p>
                                Signal has a "Safety Number" feature for 1:1 conversations. Tap a contact's name
                                > "View Safety Number" and compare with them in person or via a trusted channel.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What's the difference between blocking and reporting?</summary>
                            <p>
                                Blocking prevents a user from contacting you on Signal. Reporting to community
                                admins through this platform triggers the consensus-based review process that
                                can remove them from groups.
                            </p>
                        </details>
                    </section>
                </div>
            </div>
        </div>
    `;

    setupTabs(container);
}

/**
 * Set up tab switching behavior
 * @param {HTMLElement} container - Container element
 */
function setupTabs(container) {
    const tabButtons = container.querySelectorAll('#help-tabs .tab');
    const tabContents = container.querySelectorAll('.help-tab-content');

    tabButtons.forEach(button => {
        button.addEventListener('click', () => {
            const targetTab = button.dataset.tab;

            tabButtons.forEach(btn => btn.classList.remove('tab--active'));
            button.classList.add('tab--active');

            tabContents.forEach(content => {
                content.style.display = content.id === `tab-${targetTab}` ? '' : 'none';
            });
        });
    });
}

/**
 * Cleanup when leaving the page
 */
export function cleanup() {
    // No cleanup needed for static content
}

export default { render, cleanup };
