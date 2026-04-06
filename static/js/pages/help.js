/**
 * Help/FAQ page
 * Provides information about the platform and answers common questions.
 * Uses a tabbed layout: General, Communities, Schools, Meshtastic, Encryption, Signal Safety.
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
                    <button class="tab" data-tab="meshtastic">Meshtastic</button>
                    <button class="tab" data-tab="encryption">Encryption</button>
                    <button class="tab" data-tab="signal-safety">Signal Safety</button>
                </div>

                <!-- General Tab -->
                <div class="help-tab-content" id="tab-general">
                    <section class="help-section">
                        <h2>What is Community Rapid Response?</h2>
                        <p>
                            Community Rapid Response is a platform that connects neighbors and
                            school communities through secure Signal group chats. We help communities
                            organize by creating geographic-based groups where members have verified
                            addresses in the area, and school-based groups where members are vouched
                            for by their peers.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>What We Verify (and What We Don't)</h2>
                        <div class="help-highlight">
                            <p>
                                This platform verifies <strong>member residency</strong> (that a person can
                                receive mail at an address in a geographic area) and <strong>community vouches</strong>
                                (that existing members recognize a person). It does not verify, endorse, or
                                guarantee the quality, safety, or leadership of any group.
                            </p>
                            <p>
                                Groups are registered on this platform by their organizers. Group names,
                                descriptions, and claims are provided by those organizers, not by this platform.
                                A group appearing here means its members have verified addresses in the area
                                &mdash; nothing more.
                            </p>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>Privacy & Security</h2>
                        <div class="help-highlight">
                            <h3>Your address is not stored in our database</h3>
                            <p>
                                When you enter your address &mdash; whether for vouch verification (to
                                identify your community) or postcard verification (to prove residency)
                                &mdash; it exists only in our server's memory long enough to process
                                the request. For vouch requests, your address is geocoded then immediately
                                discarded. For postcard requests, it is sent to our mailing partner then
                                discarded. Your address is never written to our database. Even our
                                administrators cannot retrieve it.
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
                            <li>Signal group invite links and Meshtastic channel URLs are end-to-end encrypted &mdash; the server never sees the plaintext</li>
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
                                <strong>For communities:</strong> First, get vouched by community members
                                to gain read-only access. Then, request postcard verification to prove your
                                address. Once you have both vouch and postcard verification, you automatically
                                gain admin rights for your community.
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
                            We use a two-step verification system to ensure community members are genuine
                            neighbors. Vouching comes first to establish community trust, then address
                            verification proves residency for admin access.
                        </p>
                        <div class="help-cards">
                            <div class="help-card">
                                <h3>Step 1: Vouch Verification</h3>
                                <p>
                                    Enter your address to identify your community (your address is used only
                                    for geocoding and is never stored). Then get vouched for by existing members
                                    who know you live in the area &mdash; vouchers can be from anywhere in your
                                    city or county, not just your exact neighborhood. Once vouched, you gain
                                    <strong>read-only access</strong> to your community's Signal groups.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Step 2: Postcard Verification (optional)</h3>
                                <p>
                                    After being vouched, you can request address verification. We mail a postcard
                                    with a unique code to your address. Enter the code to become a <strong>full
                                    admin</strong> who can create communities and manage groups.
                                    <strong>Your address is never stored.</strong>
                                </p>
                            </div>
                        </div>
                        <p>
                            <strong>Why vouch first?</strong> Requiring community trust before granting any
                            Signal group access prevents bad actors from seeing group invite links before
                            anyone in the community has vetted them. A postcard alone only proves a mailing
                            address &mdash; a vouch proves a real community connection.
                        </p>
                        <div class="help-highlight" style="margin-top: var(--space-4);">
                            <h3>Signal Group Invite Links Are Encrypted</h3>
                            <p>
                                Signal group invite links are protected with end-to-end encryption. The server
                                never sees the plaintext link &mdash; it is encrypted in your browser before being
                                sent, and only decrypted in the browsers of members with verified addresses. See the
                                <strong>Encryption</strong> tab for details.
                            </p>
                        </div>
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
                            <li><strong>Neighborhood</strong> - Most specific level</li>
                        </ul>
                        <p>
                            When you submit your address for vouch verification, it is geocoded to identify
                            your community and then immediately discarded. You are assigned to the most
                            specific community that contains your location.
                        </p>
                        <p>
                            Vouching works across the hierarchy &mdash; a verified member from one neighborhood
                            can vouch for someone in a different neighborhood within the same city or county.
                            This makes it easier to get started, since you don't need to find verified members
                            in your exact neighborhood. State-level vouching is not allowed.
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
                                <li><strong>Any community member can vouch</strong> (normally only fully verified admins can vouch)</li>
                                <li><strong>1-hour cooldown</strong> between vouches from the same person</li>
                                <li><strong>Exact-region vouching only</strong> &mdash; cross-neighborhood (ancestor-level) vouching is not available</li>
                            </ul>
                            <p style="margin-top: var(--space-2); margin-bottom: 0;">
                                Once your community has 3 or more full admins, normal verification rules apply,
                                including the ability for vouchers from anywhere in your city or county to vouch.
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
                                Connect with neighbors through community events, local social media,
                                or mutual friends. You need 2-3 different members to vouch for you
                                (3 in bootstrap mode, 2 otherwise). Vouchers can be from anywhere in
                                your city or county &mdash; they don't need to be in your exact neighborhood.
                                In bootstrap mode, any community member can vouch (but only within the
                                exact same community); once the community has 3+ admins, only fully
                                verified admins can vouch.
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
                                any community member can vouch for others, but 3 vouches are required
                                instead of 2, there's a 1-hour cooldown between vouches from the same person,
                                and vouching must be within the exact same community (no cross-neighborhood vouching).
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
                        <p>
                            District-level Signal groups use the same end-to-end encryption as school and
                            community groups &mdash; invite links are never stored in plaintext on the server.
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
                            <p>
                                Invite links are end-to-end encrypted, just like community groups &mdash;
                                the server never sees the plaintext. See the <strong>Encryption</strong>
                                tab for details.
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

                <!-- Meshtastic Tab -->
                <div class="help-tab-content" id="tab-meshtastic" style="display: none;">
                    <section class="help-section">
                        <h2>What is Meshtastic?</h2>
                        <p>
                            <a href="https://meshtastic.org" target="_blank" rel="noopener">Meshtastic</a> is
                            an open-source mesh networking project that uses affordable LoRa radios to create
                            long-range, off-grid communication networks. Unlike cellular or Wi-Fi, Meshtastic
                            works without any internet infrastructure &mdash; messages hop between radios to
                            reach their destination.
                        </p>
                        <p>
                            Meshtastic complements Signal by providing a communication fallback when internet
                            and cellular networks are unavailable. This is especially valuable during natural
                            disasters, power outages, or in remote areas with no cell coverage.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>How Meshtastic Channels Work</h2>
                        <div class="help-cards">
                            <div class="help-card">
                                <h3>Channel URLs</h3>
                                <p>
                                    Each Meshtastic channel is configured via a URL that encodes the channel
                                    name, encryption key, radio settings, and region. Community admins share
                                    these URLs so members can join the same mesh channel.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>QR Codes</h3>
                                <p>
                                    Channel URLs are displayed as scannable QR codes. Open the Meshtastic app
                                    on your phone, scan the QR code, and your radio will be configured
                                    automatically.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Channel Details</h3>
                                <p>
                                    Each channel displays its parsed settings: encryption type and strength,
                                    modem preset (range vs speed), radio region, and hop limit. This helps
                                    you understand the channel's configuration at a glance.
                                </p>
                            </div>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>End-to-End Encryption</h2>
                        <div class="help-highlight">
                            <h3>Channel URLs Are Encrypted</h3>
                            <p>
                                Meshtastic channel URLs (like Signal group invite links) are stored using
                                end-to-end encryption. The server never sees the plaintext URL &mdash;
                                it is encrypted in your browser before being sent, and only decrypted
                                in the browsers of members with verified addresses.
                            </p>
                        </div>
                        <ul class="help-list">
                            <li>Each user has a unique encryption keypair generated in their browser</li>
                            <li>Secrets are encrypted with a random key (DEK), and the DEK is wrapped for each member's public key</li>
                            <li>Even server administrators and superusers cannot decrypt channel URLs</li>
                            <li>If you reset your password, community members automatically re-encrypt secrets for your new key when they log in</li>
                        </ul>
                        <p style="margin-top: var(--space-3);">
                            For a detailed explanation of how encryption works, see the <strong>Encryption</strong> tab.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>Meshtastic FAQ</h2>

                        <details class="faq-item">
                            <summary>What hardware do I need?</summary>
                            <p>
                                You need a Meshtastic-compatible LoRa radio (such as a Heltec V3 or LilyGo
                                T-Beam) and a phone with the
                                <a href="https://meshtastic.org/docs/software" target="_blank" rel="noopener">Meshtastic app</a>.
                                Radios typically cost $20&ndash;$50 and communicate over license-free radio
                                frequencies.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How far can Meshtastic radios reach?</summary>
                            <p>
                                Range depends on terrain and antenna placement. Line-of-sight range can be
                                several miles (or tens of miles with elevated antennas). In urban areas,
                                expect 1&ndash;3 miles between nodes. The mesh network extends range by
                                hopping messages through intermediate radios.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Is Meshtastic encrypted?</summary>
                            <p>
                                Yes. Meshtastic channels use AES-256 encryption by default. Each channel
                                has its own encryption key (PSK). The platform shows you the encryption
                                type and warns if a channel uses weak or default keys.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>How do I scan a channel QR code?</summary>
                            <p>
                                Open the Meshtastic app on your phone, go to the channel settings, and
                                use the "Scan QR Code" option. The app will configure your radio with
                                the correct channel name, encryption key, and radio settings automatically.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Can I be in multiple Meshtastic channels?</summary>
                            <p>
                                Yes. Most Meshtastic radios support up to 8 channels simultaneously. Your
                                community may have separate channels for different purposes (e.g., general
                                chat, emergency alerts).
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What are security warnings about?</summary>
                            <p>
                                The platform analyzes each channel's encryption key and warns about insecure
                                configurations: channels using the default key (which anyone can read),
                                channels with no encryption, or channels using weaker AES-128 keys. Admins
                                should use strong, unique AES-256 keys for community channels.
                            </p>
                        </details>
                    </section>
                </div>

                <!-- Encryption Tab -->
                <div class="help-tab-content" id="tab-encryption" style="display: none;">
                    <section class="help-section">
                        <h2>How Your Data Is Protected</h2>
                        <div class="help-highlight">
                            <h3>Secrets Never Stored in Plaintext</h3>
                            <p>
                                Signal group invite links and Meshtastic channel URLs are never stored in
                                plaintext on the server. They are encrypted in your browser before being sent,
                                and only decrypted in the browsers of verified members. Server administrators
                                and superusers cannot decrypt these secrets.
                            </p>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>How It Works</h2>
                        <div class="help-cards">
                            <div class="help-card">
                                <h3>Your Keypair</h3>
                                <p>
                                    When you register, an RSA-OAEP 2048-bit keypair is generated in your
                                    browser. Your private key never leaves your device unencrypted. Your
                                    public key is shared with the server so other members can encrypt secrets
                                    for you.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Envelope Encryption</h3>
                                <p>
                                    Each secret (such as a Signal invite link) is encrypted with a random
                                    AES-256-GCM key called a DEK (data encryption key). The DEK is then
                                    wrapped separately for each verified member using their public key.
                                    Only your private key can unwrap your copy of the DEK and decrypt the
                                    secret.
                                </p>
                            </div>
                            <div class="help-card">
                                <h3>Key Backup</h3>
                                <p>
                                    Your private key is wrapped with a key derived from your password
                                    (PBKDF2, 600,000 iterations) and stored on the server as a backup.
                                    When you log in on a new device, your key backup is restored
                                    automatically &mdash; no manual key transfer needed.
                                </p>
                            </div>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>What Happens When You Reset Your Password</h2>
                        <div class="help-highlight">
                            <ul class="help-list">
                                <li>Your old keypair is destroyed and a new one is generated</li>
                                <li>Existing secrets are temporarily inaccessible until other members re-encrypt them for your new key</li>
                                <li>Re-encryption happens automatically when other members log in &mdash; no action is required from anyone</li>
                                <li>Just log in and secrets will become available as community members come online</li>
                            </ul>
                        </div>
                    </section>

                    <section class="help-section">
                        <h2>Updating Secrets</h2>
                        <p>
                            Updating a Signal group invite link or Meshtastic channel URL requires approval
                            from 3 admins (a consensus proposal). Once approved, the proposer encrypts
                            the new secret for all current verified members. Members are notified to log
                            in and view the updated link.
                        </p>
                    </section>

                    <section class="help-section">
                        <h2>Encryption FAQ</h2>

                        <details class="faq-item">
                            <summary>Can the server read my Signal invite links?</summary>
                            <p>
                                No. Invite links are encrypted in your browser before being sent to the
                                server. The server only stores ciphertext that it cannot decrypt.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What if I lose access to my device?</summary>
                            <p>
                                Log in on a new device with your password. Your key backup is stored on
                                the server (encrypted with a key derived from your password) and is restored
                                automatically when you log in.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Why can't superusers see invite links?</summary>
                            <p>
                                Superusers are intentionally excluded from encryption keys. This prevents
                                centralized access to secrets and ensures that only verified members of a
                                community or school can decrypt invite links.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>What happens if a member is removed?</summary>
                            <p>
                                A removed member loses access to future secrets. However, they may still
                                have decrypted copies of secrets they accessed before removal. Admins should
                                rotate existing secrets via an update proposal after removing a member.
                            </p>
                        </details>

                        <details class="faq-item">
                            <summary>Do I need to do anything for encryption to work?</summary>
                            <p>
                                No. Encryption is fully automatic. Your keypair is generated at registration
                                and your key backup is maintained whenever you log in. Secrets are encrypted
                                and decrypted transparently in your browser.
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
