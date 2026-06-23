/**
 * Help/FAQ page
 * Provides information about the platform and answers common questions.
 * Uses a tabbed layout: Getting Started, Groups, Connections, Verification, Safety & Privacy, Meshtastic.
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
                    <button class="tab tab--active" data-tab="getting-started">Getting Started</button>
                    <button class="tab" data-tab="groups">Groups</button>
                    <button class="tab" data-tab="connections">Connections</button>
                    <button class="tab" data-tab="verification">Verification</button>
                    <button class="tab" data-tab="safety">Safety & Privacy</button>
                    <button class="tab" data-tab="meshtastic">Meshtastic</button>
                </div>

                <!-- Getting Started Tab -->
                <div class="help-tab-content" id="tab-getting-started">
                    <section class="help-section">
                        <h3>What is Community Rapid Response?</h3>
                        <p>
                            A platform that helps people organize within their communities through
                            verified Signal group chats. The platform verifies where people live,
                            but groups are independently organized by their members. Think of it
                            as infrastructure for organizers, not a network of communities.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>How do I sign up?</h3>
                        <p>
                            Create an account with your email and password. Once registered, you
                            can browse discoverable groups and join open chats immediately.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>How do I find groups?</h3>
                        <p>
                            Use the <strong>Discover</strong> page to browse groups in your area.
                            If a group is set to discoverable, you can see it without address
                            verification.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>How do I join a group?</h3>
                        <p>
                            There are three ways to join a group:
                        </p>
                        <ul>
                            <li>Through an <strong>invite link</strong> shared by an existing member</li>
                            <li>Through a <strong>direct invitation</strong> from a group admin or member</li>
                            <li>By <strong>requesting to join</strong> through the group's page</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>What is Signal?</h3>
                        <p>
                            <strong>Signal</strong> is a free, end-to-end encrypted messaging app.
                            We use it because messages are private and can't be read by anyone
                            except participants &mdash; not even Signal's servers. Download it at
                            <strong>signal.org</strong>. You'll need it to join group chats.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Why Signal?</h3>
                        <p>
                            Unlike regular text messages or many other apps, Signal encrypts all
                            messages end-to-end. Nobody can read your messages except the people
                            in the conversation. Group chats, voice calls, and video calls are
                            all encrypted.
                        </p>
                    </section>
                </div>

                <!-- Groups Tab -->
                <div class="help-tab-content" id="tab-groups" style="display: none;">
                    <section class="help-section">
                        <h3>What is a group?</h3>
                        <p>
                            An independent organization created by community members. A group has
                            its own admins, members, signal chats, and resources. Multiple groups
                            can operate in the same area with no relationship to each other.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>How do I create a group?</h3>
                        <p>
                            You need <strong>address verification</strong> first (see the
                            Verification tab). Then use the "Create Group" button. Your group
                            starts in a <strong>provisional state</strong> until enough members
                            join (default: 3). All founding members become co-admins.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>What is a provisional group?</h3>
                        <p>
                            A new group that hasn't reached its founding threshold yet.
                            Provisional groups are unlisted and can't create signal chats.
                            Share invite links to recruit your first members. Once the threshold
                            is met, the group graduates to active status.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Group roles</h3>
                        <ul>
                            <li>
                                <strong>Admin</strong> &mdash; Can manage the group, create signal
                                chats, create invite links, and manage settings. Requires address
                                verification.
                            </li>
                            <li>
                                <strong>Trusted</strong> &mdash; Earned through vouches from other
                                trusted members and admins within the group. Can access trusted-tier
                                content and vouch for other members.
                            </li>
                            <li>
                                <strong>Member</strong> &mdash; Basic membership. Can view
                                member-tier content, propose resources, invite others, and propose
                                connections.
                            </li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>Access tiers</h3>
                        <p>
                            Each signal chat and resource in a group has an access tier that
                            controls who can see it:
                        </p>
                        <ul>
                            <li><strong>Open</strong> &mdash; Any registered user (if the group is discoverable)</li>
                            <li><strong>Resident</strong> &mdash; Users with a verified address in the group's area</li>
                            <li><strong>Member</strong> &mdash; Group members</li>
                            <li><strong>Trusted</strong> &mdash; Trusted members and admins</li>
                            <li><strong>Admin only</strong> &mdash; Just the admins</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>Listed vs Unlisted</h3>
                        <p>
                            <strong>Listed</strong> groups appear in browse results.
                            <strong>Unlisted</strong> groups can only be found through invite links
                            or direct invitations. Groups choose their own visibility.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Resources</h3>
                        <p>
                            Groups can share links to external <strong>resources</strong>
                            (documents, guides, spreadsheets). Each resource has an access tier.
                            Any member can add resources; admins can edit and delete them.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Trust vouching</h3>
                        <p>
                            Within a group, trusted members and admins can vouch for other members.
                            When a member receives enough vouches (set by the group), they're
                            promoted to <strong>trusted</strong> status. This is separate from
                            address verification &mdash; it's about community trust within the
                            group.
                        </p>
                    </section>
                </div>

                <!-- Connections Tab -->
                <div class="help-tab-content" id="tab-connections" style="display: none;">
                    <section class="help-section">
                        <h3>What are connections?</h3>
                        <p>
                            <strong>Connections</strong> link groups together for cross-region
                            collaboration. A connection is a group of groups &mdash; it can have
                            two groups or twenty.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>How do connections form?</h3>
                        <p>
                            Any group member can propose a connection through the
                            <strong>topic board</strong> on the Connections page. All target groups
                            must accept for the connection to form.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Topic board</h3>
                        <p>
                            Group admins can post a presence on the topic board: topic tags, a
                            vague region label (like "Pacific Northwest"), and a short description.
                            Other admins can browse by topic to find groups working on similar
                            issues. No specific location or group name is revealed until both
                            sides agree to connect.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>What can connected groups do?</h3>
                        <ul>
                            <li>See each other's group names, descriptions, and regions</li>
                            <li>Create shared signal chats (requires consensus from all member groups)</li>
                            <li>Share resource links with each other</li>
                            <li>All group members can see that a connection exists</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>Expanding connections</h3>
                        <p>
                            Any member group can invite another group to join an existing
                            connection. All current member groups must approve.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Leaving a connection</h3>
                        <p>
                            Any group can leave a connection at any time. Their shared resources
                            are removed.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Blocking</h3>
                        <p>
                            Groups can block other groups. Blocking prevents new connections and
                            hides topic board postings, but doesn't remove either group from
                            existing shared connections.
                        </p>
                    </section>
                </div>

                <!-- Verification Tab -->
                <div class="help-tab-content" id="tab-verification" style="display: none;">
                    <section class="help-section">
                        <h3>What is address verification?</h3>
                        <p>
                            The platform can verify that you live at a specific address by mailing
                            a postcard with a verification code. This proves residency, nothing
                            more.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Who needs it?</h3>
                        <p>
                            Only people who want to <strong>create groups</strong> or become
                            <strong>group admins</strong>. Regular members and trusted members
                            don't need address verification.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>What does verification prove?</h3>
                        <p>
                            That you live where you say you live. Period. It does <strong>not</strong>
                            verify:
                        </p>
                        <ul>
                            <li>The quality, safety, or leadership of any group</li>
                            <li>Your character or trustworthiness</li>
                            <li>That any group you create is "official" or "endorsed"</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>What about group-level trust?</h3>
                        <p>
                            Trust within a group is handled by the group's own vouch system, not
                            by address verification. A group's admins decide who to trust.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Your address is never stored</h3>
                        <p>
                            When you verify, your address is used to mail the postcard and then
                            immediately discarded. It is never written to our database. We only
                            record which geographic region you verified in.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Moving?</h3>
                        <p>
                            Verify at your new address anytime. You can remove your old verified
                            region from your Profile page.
                        </p>
                    </section>
                </div>

                <!-- Safety & Privacy Tab -->
                <div class="help-tab-content" id="tab-safety" style="display: none;">
                    <section class="help-section">
                        <h3>Your privacy</h3>
                        <ul>
                            <li>Addresses are never stored &mdash; used only for postcard mailing, then discarded</li>
                            <li>For neighborhood, school, and district chats, Signal invite links are stored encrypted so only verified members can decrypt them. Group- and connection-owned chats are currently listed for coordination only &mdash; their invite links are shared out-of-band, not stored on the platform.</li>
                            <li>The platform doesn't track which Signal groups you join</li>
                            <li>Group membership is visible to other group members, not to the public</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>Signal safety tips</h3>
                        <ul>
                            <li>Set disappearing messages in sensitive chats</li>
                            <li>Use a registration lock PIN in Signal settings</li>
                            <li>Be cautious about sharing personal information even in encrypted chats</li>
                            <li>Remember that anyone in a Signal group can see all messages and member phone numbers</li>
                            <li>Consider using a secondary phone number for activism (Google Voice, etc.)</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>What we don't endorse</h3>
                        <p>
                            This platform verifies member residency. It does not verify, endorse,
                            or guarantee the quality, safety, or leadership of any group. Group
                            names, descriptions, and claims are provided by the group's organizers,
                            not by this platform.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Encryption</h3>
                        <p>
                            For neighborhood, school, and district Signal chats, invite links and
                            sensitive data are stored using envelope encryption (RSA + AES-256-GCM),
                            so only authorized, verified members can decrypt them.
                        </p>
                        <p>
                            Signal chats owned by groups and connections are currently listed for
                            coordination only: their invite links are shared out-of-band rather than
                            stored encrypted on the platform. Encrypted storage for these chats is a
                            planned addition.
                        </p>
                    </section>
                </div>

                <!-- Meshtastic Tab -->
                <div class="help-tab-content" id="tab-meshtastic" style="display: none;">
                    <section class="help-section">
                        <h3>What is Meshtastic?</h3>
                        <p>
                            <strong>Meshtastic</strong> is an open-source project that turns
                            inexpensive radios into mesh communication devices. Messages hop
                            between radios to extend range, and the network works without
                            internet or cell service.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Why Meshtastic?</h3>
                        <p>
                            In emergencies, cell towers and internet can go down. Meshtastic
                            provides a backup communication channel that works when nothing else
                            does. It's useful for disaster prep, search and rescue, and community
                            resilience.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>How does it work with groups?</h3>
                        <p>
                            Groups can create Meshtastic channels alongside their Signal chats.
                            Channel URLs are shared with the same access tier system &mdash; some
                            channels might be open to all, others restricted to trusted members.
                        </p>
                    </section>

                    <section class="help-section">
                        <h3>Getting started</h3>
                        <ul>
                            <li>You need a Meshtastic-compatible radio (starting around $25-35)</li>
                            <li>Download the Meshtastic app for your phone</li>
                            <li>Flash the firmware onto the radio</li>
                            <li>Join channels shared by your group</li>
                            <li>See <strong>meshtastic.org</strong> for full setup guides</li>
                        </ul>
                    </section>

                    <section class="help-section">
                        <h3>Limitations</h3>
                        <ul>
                            <li>Meshtastic is for short text messages, not voice or video</li>
                            <li>Range depends on terrain and radio power (typically 1-5 miles per hop, more with line of sight)</li>
                            <li>Messages are not end-to-end encrypted by default (use encrypted channels for sensitive comms)</li>
                        </ul>
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
