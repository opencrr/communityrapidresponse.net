package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
)

// Deterministic IDs for cross-referencing between fixtures.
const (
	// Users
	userAlice = "user-alice"
	userBob   = "user-bob"
	userCarol = "user-carol"
	userDave  = "user-dave"
	userEve   = "user-eve"
	userFrank = "user-frank"
	userGrace = "user-grace"
	userHeidi = "user-heidi"
	userIvan  = "user-ivan"
	userJudy  = "user-judy"
	userKarl  = "user-karl"

	// Regions
	regionWashington     = "region-washington"
	regionKingCounty     = "region-king-county"
	regionSeattle        = "region-seattle"
	regionOregon         = "region-oregon"
	regionMultnomahCounty = "region-multnomah-county"
	regionPortland       = "region-portland"
	regionIllinois       = "region-illinois"
	regionCookCounty     = "region-cook-county"
	regionChicago        = "region-chicago"

	// Groups
	groupSeattleMA       = "group-seattle-ma"
	groupSeattleTenants  = "group-seattle-tenants"
	groupPortlandMA      = "group-portland-ma"
	groupChicagoDP       = "group-chicago-dp"
	groupSecretSociety   = "group-secret-society"
	groupProvisional     = "group-provisional"
	groupOpenHub         = "group-open-hub"

	// Connection
	connectionPNW = "conn-pnw-mutual-aid"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()

	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()

	slog.Info("connected to database, seeding fixtures...")

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), 12)
	if err != nil {
		log.Fatalf("failed to hash password: %v", err)
	}

	now := time.Now().UTC()
	pastGraduation := now.Add(-30 * 24 * time.Hour)

	// =========================================================================
	// Users
	// =========================================================================
	slog.Info("seeding users...")

	type userFixture struct {
		id               string
		username         string
		email            string
		verificationTier int
		postcardVerified bool
		vouchVerified    bool
		isSuperuser      bool
	}

	users := []userFixture{
		{userAlice, "alice", "alice@test.com", 2, true, true, true},
		{userBob, "bob", "bob@test.com", 2, true, true, false},
		{userCarol, "carol", "carol@test.com", 2, true, true, false},
		{userDave, "dave", "dave@test.com", 1, false, true, false},
		{userEve, "eve", "eve@test.com", 1, false, true, false},
		{userFrank, "frank", "frank@test.com", 2, true, true, false},
		{userGrace, "grace", "grace@test.com", 1, false, true, false},
		{userHeidi, "heidi", "heidi@test.com", 2, true, true, false},
		{userIvan, "ivan", "ivan@test.com", 1, false, true, false},
		{userJudy, "judy", "judy@test.com", 0, false, false, false},
		{userKarl, "karl", "karl@test.com", 2, true, true, false},
	}

	for _, u := range users {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO users
				(id, username, email, password_hash, verification_tier,
				 postcard_verified, vouch_verified, is_superuser,
				 mfa_enabled, mfa_setup_required, email_verified,
				 created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, FALSE, FALSE, TRUE, ?)
		`, u.id, u.username, u.email, string(passwordHash),
			u.verificationTier, u.postcardVerified, u.vouchVerified,
			u.isSuperuser, now)
		if err != nil {
			log.Fatalf("failed to insert user %s: %v", u.username, err)
		}
	}
	slog.Info("users seeded", "count", len(users))

	// =========================================================================
	// Regions
	// =========================================================================
	slog.Info("seeding regions...")

	type regionFixture struct {
		id         string
		name       string
		regionType string
		parentID   *string
	}

	regions := []regionFixture{
		{regionWashington, "Washington", "state", nil},
		{regionKingCounty, "King County", "county", strPtr(regionWashington)},
		{regionSeattle, "Seattle", "city", strPtr(regionKingCounty)},
		{regionOregon, "Oregon", "state", nil},
		{regionMultnomahCounty, "Multnomah County", "county", strPtr(regionOregon)},
		{regionPortland, "Portland", "city", strPtr(regionMultnomahCounty)},
		{regionIllinois, "Illinois", "state", nil},
		{regionCookCounty, "Cook County", "county", strPtr(regionIllinois)},
		{regionChicago, "Chicago", "city", strPtr(regionCookCounty)},
	}

	for _, r := range regions {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO geographic_regions (id, name, region_type, parent_region_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, r.id, r.name, r.regionType, r.parentID, now)
		if err != nil {
			log.Fatalf("failed to insert region %s: %v", r.name, err)
		}
	}
	slog.Info("regions seeded", "count", len(regions))

	// =========================================================================
	// User Regions (verified address associations)
	// =========================================================================
	slog.Info("seeding user regions...")

	type userRegionFixture struct {
		userID   string
		regionID string
		isAdmin  bool
	}

	// Address-verified users get user_regions entries for their city + parent hierarchy
	userRegions := []userRegionFixture{
		// Alice (superuser) - verified in Seattle
		{userAlice, regionSeattle, true},
		{userAlice, regionKingCounty, false},
		{userAlice, regionWashington, false},
		// Bob - verified in Seattle
		{userBob, regionSeattle, true},
		{userBob, regionKingCounty, false},
		{userBob, regionWashington, false},
		// Carol - verified in Seattle
		{userCarol, regionSeattle, true},
		{userCarol, regionKingCounty, false},
		{userCarol, regionWashington, false},
		// Dave - vouch-only, verified in Seattle (pending postcard)
		{userDave, regionSeattle, false},
		{userDave, regionKingCounty, false},
		{userDave, regionWashington, false},
		// Eve - vouch-only, verified in Seattle
		{userEve, regionSeattle, false},
		{userEve, regionKingCounty, false},
		{userEve, regionWashington, false},
		// Frank - verified in Portland
		{userFrank, regionPortland, true},
		{userFrank, regionMultnomahCounty, false},
		{userFrank, regionOregon, false},
		// Grace - vouch-only, verified in Portland
		{userGrace, regionPortland, false},
		{userGrace, regionMultnomahCounty, false},
		{userGrace, regionOregon, false},
		// Heidi - verified in Chicago
		{userHeidi, regionChicago, true},
		{userHeidi, regionCookCounty, false},
		{userHeidi, regionIllinois, false},
		// Ivan - vouch-only, verified in Chicago
		{userIvan, regionChicago, false},
		{userIvan, regionCookCounty, false},
		{userIvan, regionIllinois, false},
		// Karl - verified in Seattle
		{userKarl, regionSeattle, true},
		{userKarl, regionKingCounty, false},
		{userKarl, regionWashington, false},
		// Judy - unverified, no regions
	}

	for _, ur := range userRegions {
		urID := fmt.Sprintf("ur-%s-%s", ur.userID, ur.regionID)
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO user_regions (id, user_id, region_id, is_admin, verification_status, verified_at)
			VALUES (?, ?, ?, ?, 'verified', ?)
		`, urID, ur.userID, ur.regionID, ur.isAdmin, now)
		if err != nil {
			log.Fatalf("failed to insert user_region %s->%s: %v", ur.userID, ur.regionID, err)
		}
	}
	slog.Info("user regions seeded", "count", len(userRegions))

	// =========================================================================
	// Groups
	// =========================================================================
	slog.Info("seeding groups...")

	type groupFixture struct {
		id                        string
		name                      string
		description               string
		status                    string
		visibility                string
		discoverableByUnverified  bool
		showAddressVerification   bool
		createdBy                 string
		regionIDs                 []string
		tags                      []string
		graduated                 bool
	}

	groups := []groupFixture{
		{
			groupSeattleMA, "Seattle Mutual Aid",
			"Grassroots mutual aid network serving the Seattle area",
			"active", "listed", true, true, userBob,
			[]string{regionSeattle},
			[]string{"mutual-aid", "community"},
			true,
		},
		{
			groupSeattleTenants, "Seattle Tenants Union",
			"Tenant organizing and rights advocacy in Seattle",
			"active", "listed", false, true, userCarol,
			[]string{regionSeattle},
			[]string{"tenant-rights", "housing"},
			true,
		},
		{
			groupPortlandMA, "Portland Mutual Aid",
			"Community-driven mutual aid in Portland",
			"active", "listed", true, true, userFrank,
			[]string{regionPortland},
			[]string{"mutual-aid", "community"},
			true,
		},
		{
			groupChicagoDP, "Chicago Disaster Prep",
			"Urban disaster preparedness for the Chicago metro area",
			"active", "listed", false, true, userHeidi,
			[]string{regionChicago},
			[]string{"disaster-prep", "emergency"},
			true,
		},
		{
			groupSecretSociety, "The Secret Society",
			"An unlisted group for testing unlisted visibility",
			"active", "unlisted", false, false, userKarl,
			[]string{regionSeattle},
			nil,
			true,
		},
		{
			groupProvisional, "Provisional Group",
			"A provisional group still in founding phase",
			"provisional", "unlisted", false, true, userAlice,
			[]string{regionSeattle},
			[]string{"testing"},
			false,
		},
		{
			groupOpenHub, "Open Community Hub",
			"An open-access community hub for general discussion",
			"active", "listed", true, true, userBob,
			[]string{regionSeattle},
			[]string{"open-access", "general"},
			true,
		},
	}

	for _, g := range groups {
		var graduatedAt *time.Time
		if g.graduated {
			graduatedAt = &pastGraduation
		}

		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO `+"`groups`"+` (id, name, description, status, visibility,
				discoverable_by_unverified, show_address_verification,
				created_by, graduated_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, g.id, g.name, g.description, g.status, g.visibility,
			g.discoverableByUnverified, g.showAddressVerification,
			g.createdBy, graduatedAt, now)
		if err != nil {
			log.Fatalf("failed to insert group %s: %v", g.name, err)
		}

		// Group-region associations
		for _, regionID := range g.regionIDs {
			groupRegionID := fmt.Sprintf("gr-%s-%s", g.id, regionID)
			_, err := db.ExecContext(ctx, `
				INSERT IGNORE INTO group_regions (id, group_id, region_id)
				VALUES (?, ?, ?)
			`, groupRegionID, g.id, regionID)
			if err != nil {
				log.Fatalf("failed to insert group_region %s->%s: %v", g.id, regionID, err)
			}
		}

		// Topic tags
		for _, tag := range g.tags {
			tagID := fmt.Sprintf("tag-%s-%s", g.id, tag)
			_, err := db.ExecContext(ctx, `
				INSERT IGNORE INTO group_topic_tags (id, group_id, tag)
				VALUES (?, ?, ?)
			`, tagID, g.id, tag)
			if err != nil {
				log.Fatalf("failed to insert tag %s for group %s: %v", tag, g.id, err)
			}
		}
	}
	slog.Info("groups seeded", "count", len(groups))

	// =========================================================================
	// Group Members
	// =========================================================================
	slog.Info("seeding group members...")

	type memberFixture struct {
		groupID         string
		userID          string
		isAdmin         bool
		isFoundingMember bool
	}

	members := []memberFixture{
		// Seattle Mutual Aid
		{groupSeattleMA, userBob, true, true},
		{groupSeattleMA, userAlice, true, true},
		{groupSeattleMA, userDave, false, false},
		{groupSeattleMA, userEve, false, false},
		// Seattle Tenants Union
		{groupSeattleTenants, userCarol, true, true},
		{groupSeattleTenants, userDave, false, false},
		// Portland Mutual Aid
		{groupPortlandMA, userFrank, true, true},
		{groupPortlandMA, userEve, false, false},
		{groupPortlandMA, userGrace, false, false},
		// Chicago Disaster Prep
		{groupChicagoDP, userHeidi, true, true},
		{groupChicagoDP, userIvan, false, false},
		// The Secret Society
		{groupSecretSociety, userKarl, true, true},
		{groupSecretSociety, userDave, false, false},
		// Provisional Group
		{groupProvisional, userAlice, true, true},
		// Open Community Hub
		{groupOpenHub, userBob, true, true},
		{groupOpenHub, userDave, false, false},
	}

	for _, m := range members {
		memberID := fmt.Sprintf("gm-%s-%s", m.groupID, m.userID)
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO group_members (id, group_id, user_id, is_admin, is_founding_member, joined_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, memberID, m.groupID, m.userID, m.isAdmin, m.isFoundingMember, now)
		if err != nil {
			log.Fatalf("failed to insert member %s in group %s: %v", m.userID, m.groupID, err)
		}
	}
	slog.Info("group members seeded", "count", len(members))

	// =========================================================================
	// Signal Groups (chats within groups)
	// =========================================================================
	slog.Info("seeding signal groups...")

	type signalGroupFixture struct {
		id           string
		ownerGroupID string
		groupName    string
		accessTier   string
	}

	signalGroups := []signalGroupFixture{
		// Seattle Mutual Aid
		{"sg-seattle-ma-general", groupSeattleMA, "General Chat", "open"},
		{"sg-seattle-ma-ops", groupSeattleMA, "Operations", "member"},
		{"sg-seattle-ma-lead", groupSeattleMA, "Leadership", "admin_only"},
		// Seattle Tenants Union
		{"sg-seattle-tu-main", groupSeattleTenants, "Main Chat", "member"},
		{"sg-seattle-tu-legal", groupSeattleTenants, "Legal Resources", "trusted"},
		// Portland Mutual Aid
		{"sg-portland-ma-welcome", groupPortlandMA, "Welcome", "open"},
		{"sg-portland-ma-planning", groupPortlandMA, "Planning", "member"},
		// Chicago Disaster Prep
		{"sg-chicago-dp-general", groupChicagoDP, "General", "member"},
		{"sg-chicago-dp-emergency", groupChicagoDP, "Emergency Response", "trusted"},
		// The Secret Society
		{"sg-secret-inner", groupSecretSociety, "Inner Circle", "member"},
		// Open Community Hub
		{"sg-open-hub-public", groupOpenHub, "Public Chat", "open"},
		{"sg-open-hub-members", groupOpenHub, "Members Only", "member"},
	}

	for _, sg := range signalGroups {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO signal_groups
				(id, owner_group_id, group_name, access_tier, is_active, created_at)
			VALUES (?, ?, ?, ?, TRUE, ?)
		`, sg.id, sg.ownerGroupID, sg.groupName, sg.accessTier, now)
		if err != nil {
			log.Fatalf("failed to insert signal group %s: %v", sg.groupName, err)
		}
	}
	slog.Info("signal groups seeded", "count", len(signalGroups))

	// =========================================================================
	// Group Resources
	// =========================================================================
	slog.Info("seeding group resources...")

	type resourceFixture struct {
		id         string
		groupID    string
		title      string
		url        string
		accessTier string
		createdBy  string
	}

	resources := []resourceFixture{
		{"res-ma-handbook", groupSeattleMA, "Mutual Aid Handbook", "https://example.com/ma-handbook", "open", userBob},
		{"res-volunteer-schedule", groupSeattleMA, "Volunteer Schedule", "https://example.com/schedule", "member", userBob},
		{"res-tenant-rights", groupSeattleTenants, "Tenant Rights Guide", "https://example.com/rights", "member", userCarol},
		{"res-logistics", groupPortlandMA, "Supply Chain Logistics", "https://example.com/logistics", "member", userFrank},
		{"res-contacts", groupChicagoDP, "Emergency Contact Sheet", "https://example.com/contacts", "trusted", userHeidi},
		{"res-calendar", groupOpenHub, "Community Calendar", "https://example.com/calendar", "open", userBob},
	}

	for _, r := range resources {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO group_resources (id, group_id, title, url, access_tier, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, r.id, r.groupID, r.title, r.url, r.accessTier, r.createdBy, now)
		if err != nil {
			log.Fatalf("failed to insert resource %s: %v", r.title, err)
		}
	}
	slog.Info("group resources seeded", "count", len(resources))

	// =========================================================================
	// Trust Vouches
	// =========================================================================
	slog.Info("seeding trust vouches...")

	type trustVouchFixture struct {
		groupID   string
		voucherID string
		vouchedID string
	}

	trustVouches := []trustVouchFixture{
		// Alice and Bob vouch for Dave in Seattle MA -> Dave becomes trusted
		{groupSeattleMA, userAlice, userDave},
		{groupSeattleMA, userBob, userDave},
		// Frank vouches for Eve in Portland MA -> 1 vouch, not enough for trusted
		{groupPortlandMA, userFrank, userEve},
	}

	for i, tv := range trustVouches {
		vouchID := fmt.Sprintf("tv-%s-%s-%s", tv.groupID, tv.voucherID, tv.vouchedID)
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO group_trust_vouches (id, group_id, voucher_user_id, vouched_user_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, vouchID, tv.groupID, tv.voucherID, tv.vouchedID, now)
		if err != nil {
			log.Fatalf("failed to insert trust vouch %d: %v", i, err)
		}
	}

	// Update Dave's trust_level to 'trusted' in Seattle MA (he has 2 vouches, threshold is 2)
	_, err = db.ExecContext(ctx, `
		UPDATE group_members SET trust_level = 'trusted'
		WHERE group_id = ? AND user_id = ?
	`, groupSeattleMA, userDave)
	if err != nil {
		log.Fatalf("failed to update Dave's trust level: %v", err)
	}

	slog.Info("trust vouches seeded", "count", len(trustVouches))

	// =========================================================================
	// Topic Board Postings
	// =========================================================================
	slog.Info("seeding topic board postings...")

	type topicPostingFixture struct {
		id          string
		groupID     string
		regionLabel string
		description string
		tags        []string
	}

	topicPostings := []topicPostingFixture{
		{
			"tp-seattle-ma", groupSeattleMA, "Pacific Northwest",
			"Established mutual aid network sharing resources and best practices",
			[]string{"mutual-aid"},
		},
		{
			"tp-portland-ma", groupPortlandMA, "Pacific Northwest",
			"Community-driven mutual aid, open to connecting with similar groups",
			[]string{"mutual-aid"},
		},
		{
			"tp-chicago-dp", groupChicagoDP, "Great Lakes",
			"Urban disaster preparedness group, happy to share our playbook",
			[]string{"disaster-prep"},
		},
	}

	for _, tp := range topicPostings {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO topic_board_postings (id, group_id, region_label, description, is_active, created_at)
			VALUES (?, ?, ?, ?, TRUE, ?)
		`, tp.id, tp.groupID, tp.regionLabel, tp.description, now)
		if err != nil {
			log.Fatalf("failed to insert topic posting %s: %v", tp.id, err)
		}

		for _, tag := range tp.tags {
			_, err := db.ExecContext(ctx, `
				INSERT IGNORE INTO topic_board_tags (posting_id, tag)
				VALUES (?, ?)
			`, tp.id, tag)
			if err != nil {
				log.Fatalf("failed to insert topic tag %s for posting %s: %v", tag, tp.id, err)
			}
		}
	}
	slog.Info("topic board postings seeded", "count", len(topicPostings))

	// =========================================================================
	// Connection: PNW Mutual Aid Network
	// =========================================================================
	slog.Info("seeding connection...")

	connectionName := "PNW Mutual Aid Network"
	_, err = db.ExecContext(ctx, `
		INSERT IGNORE INTO connections (id, name, created_at)
		VALUES (?, ?, ?)
	`, connectionPNW, connectionName, now)
	if err != nil {
		log.Fatalf("failed to insert connection: %v", err)
	}

	// Add member groups
	connectionMembers := []struct {
		groupID string
	}{
		{groupSeattleMA},
		{groupPortlandMA},
	}
	for _, cm := range connectionMembers {
		memberID := fmt.Sprintf("cm-%s-%s", connectionPNW, cm.groupID)
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO connection_members (id, connection_id, group_id, joined_at)
			VALUES (?, ?, ?, ?)
		`, memberID, connectionPNW, cm.groupID, now)
		if err != nil {
			log.Fatalf("failed to insert connection member %s: %v", cm.groupID, err)
		}
	}

	// Connection signal chat: PNW Admin Chat
	connectionChatID := "sg-pnw-admin-chat"
	_, err = db.ExecContext(ctx, `
		INSERT IGNORE INTO signal_groups
			(id, connection_id, group_name, access_tier, is_active, created_at)
		VALUES (?, ?, ?, ?, TRUE, ?)
	`, connectionChatID, connectionPNW, "PNW Admin Chat", "admin_only", now)
	if err != nil {
		log.Fatalf("failed to insert connection signal chat: %v", err)
	}

	// Share Seattle MA's "Mutual Aid Handbook" into the connection
	sharedResourceID := "csr-ma-handbook-pnw"
	_, err = db.ExecContext(ctx, `
		INSERT IGNORE INTO connection_shared_resources
			(id, resource_id, connection_id, shared_by_group_id, visibility, shared_at)
		VALUES (?, ?, ?, ?, 'all_members', ?)
	`, sharedResourceID, "res-ma-handbook", connectionPNW, groupSeattleMA, now)
	if err != nil {
		log.Fatalf("failed to insert shared resource: %v", err)
	}

	slog.Info("connection seeded", "name", connectionName)

	// =========================================================================
	// Group Blocks
	// =========================================================================
	slog.Info("seeding group blocks...")

	blockID := "gb-tenants-blocks-secret"
	_, err = db.ExecContext(ctx, `
		INSERT IGNORE INTO group_blocks (id, blocker_group_id, blocked_group_id, created_at)
		VALUES (?, ?, ?, ?)
	`, blockID, groupSeattleTenants, groupSecretSociety, now)
	if err != nil {
		log.Fatalf("failed to insert group block: %v", err)
	}
	slog.Info("group blocks seeded", "count", 1)

	// =========================================================================
	// Done
	// =========================================================================
	slog.Info("fixtures seeded successfully!")
	printSummary()
}

func strPtr(s string) *string {
	return &s
}

func printSummary() {
	fmt.Println()
	fmt.Println("=== Fixture Summary ===")
	fmt.Println()
	fmt.Println("Users (password: \"password\" for all):")
	fmt.Println("  alice    (superuser, tier 2) - admin of Seattle MA, Provisional Group")
	fmt.Println("  bob      (tier 2)            - admin of Seattle MA, Open Community Hub")
	fmt.Println("  carol    (tier 2)            - admin of Seattle Tenants Union")
	fmt.Println("  dave     (tier 1)            - member of Seattle MA, Tenants, Secret Society, Open Hub")
	fmt.Println("  eve      (tier 1)            - member of Seattle MA, Portland MA")
	fmt.Println("  frank    (tier 2)            - admin of Portland MA")
	fmt.Println("  grace    (tier 1)            - member of Portland MA")
	fmt.Println("  heidi    (tier 2)            - admin of Chicago Disaster Prep")
	fmt.Println("  ivan     (tier 1)            - member of Chicago Disaster Prep")
	fmt.Println("  judy     (tier 0)            - unverified user")
	fmt.Println("  karl     (tier 2)            - admin of The Secret Society")
	fmt.Println()
	fmt.Println("Groups (show_address_verification: T=address verification enabled):")
	fmt.Println("  Seattle Mutual Aid       (active, listed, discoverable, T)")
	fmt.Println("  Seattle Tenants Union    (active, listed, T)")
	fmt.Println("  Portland Mutual Aid      (active, listed, discoverable, T)")
	fmt.Println("  Chicago Disaster Prep    (active, listed, T)")
	fmt.Println("  The Secret Society       (active, unlisted, address verification OFF)")
	fmt.Println("  Provisional Group        (provisional, unlisted, T)")
	fmt.Println("  Open Community Hub       (active, listed, discoverable, T)")
	fmt.Println()
	fmt.Println("Connection: PNW Mutual Aid Network (Seattle MA + Portland MA)")
	fmt.Println("  Signal chat: PNW Admin Chat (admin_only)")
	fmt.Println("  Shared resource: Mutual Aid Handbook (all_members)")
	fmt.Println()
	fmt.Println("Group block: Seattle Tenants Union blocks The Secret Society")
	fmt.Println()
	fmt.Println("Trust vouches: Dave is trusted in Seattle MA (vouched by alice + bob)")
}

