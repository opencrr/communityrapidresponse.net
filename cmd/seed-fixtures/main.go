package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
)

// Deterministic IDs for cross-referencing between fixtures.
const (
	// Users
	userAlice  = "user-alice"
	userBob    = "user-bob"
	userCarol  = "user-carol"
	userDave   = "user-dave"
	userEve    = "user-eve"
	userFrank  = "user-frank"
	userGrace  = "user-grace"
	userHeidi  = "user-heidi"
	userIvan   = "user-ivan"
	userJudy   = "user-judy"
	userKarl   = "user-karl"
	userLiam   = "user-liam"
	userMaya   = "user-maya"
	userNoah   = "user-noah"

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

	// Safety guard: this tool inserts known fixture accounts (including a
	// superuser with a hard-coded password) and connects to whatever the
	// resolved environment points at. Refuse to run outside development unless
	// explicitly confirmed, so it can never silently seed a staging/prod DB.
	if cfg.Env != config.EnvDevelopment && os.Getenv("SEED_FIXTURES_CONFIRM") != "1" {
		log.Fatalf("refusing to seed fixtures: ENVIRONMENT=%q is not development; "+
			"set SEED_FIXTURES_CONFIRM=1 to override (this inserts a known superuser "+
			"with a default password and must never touch staging/production)", cfg.Env)
	}

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
		{userLiam, "liam", "liam@test.com", 0, false, false, false},
		{userMaya, "maya", "maya@test.com", 0, false, false, false},
		{userNoah, "noah", "noah@test.com", 0, false, false, false},
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
		// Unverified users in open/discoverable groups
		{groupOpenHub, userLiam, false, false},     // Liam joined Open Community Hub
		{groupOpenHub, userMaya, false, false},      // Maya joined Open Community Hub
		{groupSeattleMA, userLiam, false, false},    // Liam also joined Seattle MA
		{groupPortlandMA, userNoah, false, false},   // Noah joined Portland MA
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
		description  string
		accessTier   string
	}

	signalGroups := []signalGroupFixture{
		// Seattle Mutual Aid (3 chats spanning all access levels)
		{"sg-seattle-ma-general", groupSeattleMA, "General Chat", "Open chat for anyone interested in mutual aid in Seattle. Introduce yourself!", "open"},
		{"sg-seattle-ma-ops", groupSeattleMA, "Operations", "Coordination for active mutual aid operations, supply runs, and volunteer scheduling.", "member"},
		{"sg-seattle-ma-lead", groupSeattleMA, "Leadership", "Admin coordination, budget discussions, and strategic planning.", "admin_only"},
		// Seattle Tenants Union (2 chats)
		{"sg-seattle-tu-main", groupSeattleTenants, "Main Chat", "General discussion for Seattle tenants. Share experiences, ask questions, support each other.", "member"},
		{"sg-seattle-tu-legal", groupSeattleTenants, "Legal Resources", "Sensitive legal discussions and case coordination. Trusted members only.", "trusted"},
		// Portland Mutual Aid (3 chats)
		{"sg-portland-ma-welcome", groupPortlandMA, "Welcome", "Open chat for newcomers. Learn about Portland mutual aid and how to get involved.", "open"},
		{"sg-portland-ma-planning", groupPortlandMA, "Planning", "Weekly planning and coordination for active projects.", "member"},
		{"sg-portland-ma-trusted", groupPortlandMA, "Trusted Circle", "Sensitive operational discussions for trusted members.", "trusted"},
		// Chicago Disaster Prep (3 chats)
		{"sg-chicago-dp-general", groupChicagoDP, "General", "General discussion about disaster preparedness in Chicago.", "member"},
		{"sg-chicago-dp-emergency", groupChicagoDP, "Emergency Response", "Active emergency coordination. Trusted members with training only.", "trusted"},
		{"sg-chicago-dp-public", groupChicagoDP, "Public Info", "Open channel for sharing public safety info and preparedness tips.", "open"},
		// The Secret Society (2 chats)
		{"sg-secret-inner", groupSecretSociety, "Inner Circle", "Main discussion space.", "member"},
		{"sg-secret-ops", groupSecretSociety, "Operations", "Operational planning for trusted members.", "trusted"},
		// Open Community Hub (3 chats)
		{"sg-open-hub-public", groupOpenHub, "Public Chat", "Open to everyone! General community discussion, events, and announcements.", "open"},
		{"sg-open-hub-members", groupOpenHub, "Members Only", "Member discussions, project planning, and coordination.", "member"},
		{"sg-open-hub-admin", groupOpenHub, "Admin Channel", "Admin coordination and moderation discussion.", "admin_only"},
	}

	for _, sg := range signalGroups {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO signal_groups
				(id, owner_group_id, group_name, description, access_tier, is_active, created_at)
			VALUES (?, ?, ?, ?, ?, TRUE, ?)
		`, sg.id, sg.ownerGroupID, sg.groupName, sg.description, sg.accessTier, now)
		if err != nil {
			log.Fatalf("failed to insert signal group %s: %v", sg.groupName, err)
		}
	}
	slog.Info("signal groups seeded", "count", len(signalGroups))

	// =========================================================================
	// Meshtastic Channels (within groups)
	// =========================================================================
	slog.Info("seeding meshtastic channels...")

	type meshtasticFixture struct {
		id           string
		ownerGroupID string
		channelName  string
		description  string
		accessTier   string
		createdBy    string
	}

	meshtasticChannels := []meshtasticFixture{
		// Seattle Mutual Aid
		{"mesh-seattle-ma-emergency", groupSeattleMA, "Emergency Mesh Network", "Emergency mesh communications for mutual aid operations.", "member", userBob},
		{"mesh-seattle-ma-public", groupSeattleMA, "Public Info Channel", "Open mesh channel for public information and alerts.", "open", userBob},
		// Portland Mutual Aid
		{"mesh-portland-ma-net", groupPortlandMA, "PDX Mesh Net", "Portland area mesh networking for mutual aid coordination.", "member", userFrank},
		// Chicago Disaster Prep
		{"mesh-chicago-dp-emergency", groupChicagoDP, "Emergency Comms", "Emergency communications for trained responders.", "trusted", userAlice},
		{"mesh-chicago-dp-weather", groupChicagoDP, "Weather Alerts", "Open weather alert channel for the Chicago area.", "open", userAlice},
		// Open Community Hub
		{"mesh-open-hub-community", groupOpenHub, "Community Mesh", "Open mesh channel for community communication.", "open", userAlice},
	}

	for _, mc := range meshtasticChannels {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO meshtastic_channels
				(id, owner_group_id, channel_name, description, access_tier, is_active, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, TRUE, ?, ?)
		`, mc.id, mc.ownerGroupID, mc.channelName, mc.description, mc.accessTier, mc.createdBy, now)
		if err != nil {
			log.Fatalf("failed to insert meshtastic channel %s: %v", mc.channelName, err)
		}
	}
	slog.Info("meshtastic channels seeded", "count", len(meshtasticChannels))

	// =========================================================================
	// Group Resources
	// =========================================================================
	slog.Info("seeding group resources...")

	type resourceFixtureWithDesc struct {
		id          string
		groupID     string
		title       string
		url         string
		description string
		accessTier  string
		createdBy   string
	}

	resourcesWithDesc := []resourceFixtureWithDesc{
		// Seattle Mutual Aid (5 resources)
		{"res-ma-handbook", groupSeattleMA, "Mutual Aid Handbook", "https://docs.google.com/document/d/example-ma-handbook", "Comprehensive guide to running mutual aid operations. Covers logistics, communication, and volunteer management.", "open", userBob},
		{"res-volunteer-schedule", groupSeattleMA, "Volunteer Schedule", "https://docs.google.com/spreadsheets/d/example-volunteer-schedule", "Weekly volunteer sign-up sheet. Update your availability here.", "member", userBob},
		{"res-ma-budget", groupSeattleMA, "Budget Tracker", "https://docs.google.com/spreadsheets/d/example-budget", "Donation tracking and expense reporting. Admin eyes only.", "admin_only", userAlice},
		{"res-ma-onboarding", groupSeattleMA, "New Volunteer Onboarding", "https://docs.google.com/document/d/example-onboarding", "Step-by-step guide for new volunteers joining the team.", "member", userBob},
		{"res-ma-safety", groupSeattleMA, "Safety Protocols", "https://docs.google.com/document/d/example-safety", "Safety guidelines for supply runs and in-person events.", "trusted", userAlice},

		// Seattle Tenants Union (4 resources)
		{"res-tenant-rights", groupSeattleTenants, "WA Tenant Rights Guide", "https://www.washingtonlawhelp.org/resource/tenants-rights", "Washington state tenant rights overview from WA Law Help.", "member", userCarol},
		{"res-tenant-hotline", groupSeattleTenants, "Tenant Hotline Numbers", "https://docs.google.com/document/d/example-hotlines", "Emergency hotline numbers for tenant emergencies, legal aid, and housing assistance.", "open", userCarol},
		{"res-tenant-legal-templates", groupSeattleTenants, "Legal Letter Templates", "https://docs.google.com/document/d/example-legal-templates", "Template letters for landlord disputes, repair requests, and lease issues. Trusted members only.", "trusted", userCarol},
		{"res-tenant-meeting-notes", groupSeattleTenants, "Meeting Notes Archive", "https://docs.google.com/document/d/example-meeting-notes", "Running notes from weekly tenant union meetings.", "member", userCarol},

		// Portland Mutual Aid (4 resources)
		{"res-logistics", groupPortlandMA, "Supply Chain Logistics", "https://docs.google.com/spreadsheets/d/example-logistics", "Tracking sheet for supply donations, storage locations, and distribution routes.", "member", userFrank},
		{"res-portland-guide", groupPortlandMA, "Portland MA Getting Started", "https://docs.google.com/document/d/example-portland-start", "How to get involved with Portland mutual aid. Open to everyone.", "open", userFrank},
		{"res-portland-contacts", groupPortlandMA, "Partner Organizations", "https://docs.google.com/document/d/example-partners", "Contact info for partner orgs, food banks, shelters, and community centers.", "member", userFrank},
		{"res-portland-ops-manual", groupPortlandMA, "Operations Manual", "https://docs.google.com/document/d/example-ops-manual", "Detailed procedures for supply runs, volunteer coordination, and emergency response.", "trusted", userFrank},

		// Chicago Disaster Prep (5 resources)
		{"res-contacts", groupChicagoDP, "Emergency Contact Sheet", "https://docs.google.com/spreadsheets/d/example-emergency-contacts", "Emergency contacts for first responders, hospitals, shelters, and utility companies.", "trusted", userHeidi},
		{"res-chicago-go-bag", groupChicagoDP, "Go-Bag Checklist", "https://docs.google.com/document/d/example-go-bag", "What to pack in your emergency go-bag. Printable checklist.", "open", userHeidi},
		{"res-chicago-comms", groupChicagoDP, "Emergency Comms Plan", "https://docs.google.com/document/d/example-comms-plan", "Communication protocols during emergencies. Radio frequencies, check-in procedures, rally points.", "member", userHeidi},
		{"res-chicago-training", groupChicagoDP, "Training Materials", "https://docs.google.com/drive/folders/example-training", "First aid, CPR, and disaster response training materials and videos.", "member", userHeidi},
		{"res-chicago-shelter", groupChicagoDP, "Shelter Location Map", "https://www.google.com/maps/d/example-shelters", "Map of emergency shelters across Chicago with capacity and contact info.", "open", userHeidi},

		// The Secret Society (2 resources)
		{"res-secret-manifesto", groupSecretSociety, "The Manifesto", "https://docs.google.com/document/d/example-manifesto", "Our founding principles and operating guidelines.", "member", userKarl},
		{"res-secret-contacts", groupSecretSociety, "Member Directory", "https://docs.google.com/spreadsheets/d/example-directory", "Contact information for all members. Handle with care.", "trusted", userKarl},

		// Open Community Hub (4 resources)
		{"res-calendar", groupOpenHub, "Community Calendar", "https://calendar.google.com/calendar/example-community", "Upcoming community events, meetings, and social gatherings. Open to all.", "open", userBob},
		{"res-hub-local-biz", groupOpenHub, "Local Business Directory", "https://docs.google.com/spreadsheets/d/example-local-biz", "Community-maintained list of local businesses, services, and resources.", "open", userBob},
		{"res-hub-projects", groupOpenHub, "Active Projects Board", "https://docs.google.com/document/d/example-projects", "Current community projects and how to get involved.", "member", userBob},
		{"res-hub-minutes", groupOpenHub, "Meeting Minutes", "https://docs.google.com/document/d/example-hub-minutes", "Notes from community meetings and decisions made.", "member", userBob},
	}

	for _, r := range resourcesWithDesc {
		_, err := db.ExecContext(ctx, `
			INSERT IGNORE INTO group_resources (id, group_id, title, url, description, access_tier, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, r.id, r.groupID, r.title, r.url, r.description, r.accessTier, r.createdBy, now)
		if err != nil {
			log.Fatalf("failed to insert resource %s: %v", r.title, err)
		}
	}
	slog.Info("group resources seeded", "count", len(resourcesWithDesc))

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
	fmt.Println("  judy     (tier 0)            - unverified, no groups")
	fmt.Println("  karl     (tier 2)            - admin of The Secret Society")
	fmt.Println("  liam     (tier 0)            - unverified, member of Open Hub + Seattle MA")
	fmt.Println("  maya     (tier 0)            - unverified, member of Open Hub")
	fmt.Println("  noah     (tier 0)            - unverified, member of Portland MA")
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
	fmt.Println("Meshtastic channels:")
	fmt.Println("  Seattle Mutual Aid:    Emergency Mesh Network (member), Public Info Channel (open)")
	fmt.Println("  Portland Mutual Aid:   PDX Mesh Net (member)")
	fmt.Println("  Chicago Disaster Prep: Emergency Comms (trusted), Weather Alerts (open)")
	fmt.Println("  Open Community Hub:    Community Mesh (open)")
	fmt.Println()
	fmt.Println("Trust vouches: Dave is trusted in Seattle MA (vouched by alice + bob)")
}

