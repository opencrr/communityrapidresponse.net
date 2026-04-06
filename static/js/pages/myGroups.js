export async function render(container) {
    container.innerHTML = `
        <div class="page">
            <h1>My Groups</h1>
            <p>Coming soon — this will show your groups, invitations, and connections.</p>
        </div>
    `;
}
export function cleanup() {}
export default { render, cleanup };
