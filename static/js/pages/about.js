/**
 * About page
 * Explains who built the platform, the privacy model, and design philosophy.
 */

/**
 * Render the about page
 * @param {HTMLElement} container - Container element to render into
 */
export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <div class="help-content">
                <div class="page__header">
                    <h1 class="page__title">About Community Rapid Response</h1>
                </div>

                <section class="help-section">
                    <h2>About</h2>
                    <p>
                        My name is Brian. I'm a software engineer living on Whidbey Island,
                        Washington. I spend my days
                        building and operating production infrastructure at scale. I built
                        Community Rapid Response because I believe strong, connected communities
                        are critically important for our future &mdash; and the tools to build
                        them shouldn't require giving up your privacy.
                    </p>
                    <p>
                        The importance of community is something I've been slowly growing to
                        understand, living on a small island where knowing your neighbors isn't
                        optional &mdash; it's how things get done. When the power goes out, when
                        a storm hits, when someone needs help, it's the people nearby who show
                        up first. My town was actually founded in 1900 as a free-land colony,
                        built on the idea that community works best when it's accessible to
                        everyone. That spirit stuck around.
                    </p>
                    <p>
                        More immediately, I've been inspired by the rapid response groups that
                        have organized across Minnesota and other places over the past year
                        &mdash; neighbors physically showing up to protect neighbors, communities
                        stepping in where institutions weren't enough or weren't trusted. That
                        kind of organizing depends on people being able to find each other
                        quickly and trust that the people in the group are who they say they are.
                    </p>
                    <p>
                        Too many companies capitalize on our private lives in exchange for
                        things that may or may not actually benefit us. I'm funding this out
                        of my own pocket because I think this work matters, and because I don't
                        want this platform to be one more thing that profits from your data.
                        Every design decision here
                        &mdash; the postcards, the vouching, the consensus rules &mdash; exists
                        to protect the people doing the hard work of building community.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Your Privacy</h2>
                    <div class="help-highlight">
                        <h3>We never store your address</h3>
                        <p>
                            When you enter your address, it exists in our server's memory only
                            long enough to identify your community or mail you a postcard. It is
                            never written to our database. For vouch verification, your address is
                            geocoded to find your community and then discarded. For postcard
                            verification, it's sent to our mailing partner and then discarded.
                            Even our administrators cannot retrieve it.
                        </p>
                    </div>

                    <div class="help-highlight">
                        <h3>Our mailing provider retains your address for up to 90 days</h3>
                        <p>
                            We use <a href="https://www.lob.com/" target="_blank" rel="noopener noreferrer">Lob</a>
                            to send verification postcards. While we discard your address immediately, Lob retains
                            it for up to 90 days as part of their operations. Lob had the strongest privacy policy
                            I could find among physical mailing services, but I want to be upfront about this
                            limitation. If you know of a mailing provider with a better privacy policy, please
                            let me know at <a href="mailto:admin@communityrapidresponse.net">admin@communityrapidresponse.net</a>.
                        </p>
                    </div>

                    <h3>What we do store</h3>
                    <ul class="help-list">
                        <li>Your username and email (for login and notifications)</li>
                        <li>Which communities and schools you belong to (e.g., "member of Downtown neighborhood")</li>
                        <li>Your verification status (whether you've completed postcard and/or vouch verification)</li>
                    </ul>

                    <h3>What we never store</h3>
                    <ul class="help-list">
                        <li>Your street address, apartment number, or specific coordinates</li>
                        <li>Your Signal phone number or messages</li>
                        <li>Any content from your Signal group conversations</li>
                    </ul>

                    <h3>If you delete your account</h3>
                    <p>
                        You can delete your account at any time from your profile. When you do,
                        your personal information is removed and your community memberships are
                        erased. There is no data to linger because we collected as little as
                        possible in the first place.
                    </p>
                </section>

                <section class="help-section">
                    <h2>Open Source</h2>
                    <p>
                        Community Rapid Response is open source under the
                        <a href="https://opensource.org/licenses/MIT" target="_blank" rel="noopener noreferrer">MIT License</a>.
                        You can view, audit, and contribute to the source code on
                        <a href="https://github.com/opencrr/communityrapidresponse.net" target="_blank" rel="noopener noreferrer">GitHub</a>.
                        Transparency is a core value of this project &mdash; you shouldn't have to
                        trust our word about how your data is handled when you can read the code yourself.
                    </p>
                </section>

                <section class="help-section">
                    <h2>How We Protect You</h2>
                    <p>
                        Every verification and governance mechanism on this platform was chosen
                        to protect the people using it. Here's why we made the choices we did.
                    </p>

                    <div class="help-cards">
                        <div class="help-card">
                            <h3>Why vouching comes first</h3>
                            <p>
                                Vouching is the first step to joining a community because it puts
                                a human gate on access. Anyone can send themselves a postcard, but
                                a vouch means an actual person in the community recognizes you.
                                By requiring community trust before granting any access to Signal
                                groups, we prevent bad actors from seeing group invite links before
                                anyone in the community has vetted them.
                            </p>
                        </div>
                        <div class="help-card">
                            <h3>Why postcards?</h3>
                            <p>
                                A physical postcard is one of the simplest ways to prove someone
                                can receive mail at an address, without us needing to store that
                                address. Digital verification methods (IP geolocation, phone GPS)
                                are easy to fake and invasive to collect. A postcard leaves no
                                digital trail in our system. Postcard verification is available
                                after being vouched and upgrades you to admin.
                            </p>
                        </div>
                        <div class="help-card">
                            <h3>Why multi-admin consensus?</h3>
                            <p>
                                No single person should be able to remove a member, change a group
                                link, or delete community resources unilaterally. Every significant
                                action requires at least three admins to agree, and that threshold
                                scales up as a community grows. This prevents abuse of power and
                                ensures decisions reflect the community, not one individual.
                            </p>
                        </div>
                        <div class="help-card">
                            <h3>Why Signal?</h3>
                            <p>
                                Signal provides end-to-end encryption, meaning your conversations
                                are private even from us. By connecting communities through Signal
                                rather than building our own chat, your messages stay on
                                infrastructure designed for privacy from the ground up.
                            </p>
                        </div>
                    </div>
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
