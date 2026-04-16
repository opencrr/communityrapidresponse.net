package services

import (
	"strings"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

func TestEmailTemplates_Build(t *testing.T) {
	templates := NewEmailTemplates("Community Rapid Response", "http://localhost:3000/login")

	tests := []struct {
		name             string
		notificationType models.NotificationType
		data             *TemplateData
		wantSubject      string
		wantToContain    []string
		wantNotToContain []string
	}{
		{
			name:             "InviteLinkUpdated",
			notificationType: models.NotificationTypeInviteLinkUpdated,
			data:             &TemplateData{UserEmail: "test@example.com"},
			wantSubject:      "Signal Group Invite Link Updated",
			wantToContain:    []string{"log in", "invite link", "updated"},
			wantNotToContain: []string{"signal.group.link"}, // Should never contain actual invite links
		},
		{
			name:             "VerificationComplete",
			notificationType: models.NotificationTypeVerificationComplete,
			data: &TemplateData{
				UserEmail:  "test@example.com",
				RegionName: "Brooklyn Heights",
			},
			wantSubject:   "Your Address Has Been Verified",
			wantToContain: []string{"verified", "Brooklyn Heights", "vouch"},
		},
		{
			name:             "VouchReceived",
			notificationType: models.NotificationTypeVouchReceived,
			data: &TemplateData{
				UserEmail:   "test@example.com",
				VoucherName: "JohnDoe",
			},
			wantSubject:   "You Received a Vouch",
			wantToContain: []string{"vouch", "JohnDoe"},
		},
		{
			name:             "VouchComplete",
			notificationType: models.NotificationTypeVouchComplete,
			data: &TemplateData{
				UserEmail:  "test@example.com",
				RegionName: "Downtown",
			},
			wantSubject:   "You're Now Vouch-Verified",
			wantToContain: []string{"vouch-verified", "Downtown"},
		},
		{
			name:             "RekeyingNeeded",
			notificationType: models.NotificationTypeRekeyingNeeded,
			data:             &TemplateData{UserEmail: "test@example.com"},
			wantSubject:      "Encryption Re-keying Needed",
			wantToContain:    []string{"re-keying", "log in", "automatically"},
			wantNotToContain: []string{}, // No sensitive data
		},
		{
			name:             "SubRegionInvitation",
			notificationType: models.NotificationTypeSubRegionInvitation,
			data: &TemplateData{
				UserEmail:   "test@example.com",
				InviterName: "AdminUser",
			},
			wantSubject:   "You've Been Invited",
			wantToContain: []string{"invited", "AdminUser", "7 days"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notification := &models.EmailNotification{
				NotificationType: tt.notificationType,
			}

			msg := templates.Build(notification, tt.data)

			// Check recipient
			if msg.To != tt.data.UserEmail {
				t.Errorf("Expected To '%s', got '%s'", tt.data.UserEmail, msg.To)
			}

			// Check subject contains expected string
			if !strings.Contains(msg.Subject, tt.wantSubject) {
				t.Errorf("Expected subject to contain '%s', got '%s'", tt.wantSubject, msg.Subject)
			}

			// Check content contains expected strings
			content := msg.TextContent + msg.HTMLContent
			for _, want := range tt.wantToContain {
				if !strings.Contains(strings.ToLower(content), strings.ToLower(want)) {
					t.Errorf("Expected content to contain '%s'", want)
				}
			}

			// Check content does NOT contain forbidden strings
			for _, notWant := range tt.wantNotToContain {
				if strings.Contains(strings.ToLower(content), strings.ToLower(notWant)) {
					t.Errorf("Content should NOT contain '%s'", notWant)
				}
			}

			// Both text and HTML content should be non-empty
			if msg.TextContent == "" {
				t.Error("Expected non-empty TextContent")
			}
			if msg.HTMLContent == "" {
				t.Error("Expected non-empty HTMLContent")
			}
		})
	}
}

func TestEmailTemplates_SecurityRequirements(t *testing.T) {
	templates := NewEmailTemplates("Test App", "http://localhost/login")

	// Test that sensitive data is never included in emails
	sensitiveStrings := []string{
		"https://signal.group/", // Invite links
		"verification_code=",    // Verification codes
		"123 Main Street",       // Addresses
		"password",              // Passwords
	}

	notificationTypes := []models.NotificationType{
		models.NotificationTypeInviteLinkUpdated,
		models.NotificationTypeVerificationComplete,
		models.NotificationTypeVouchReceived,
		models.NotificationTypeVouchComplete,
	}

	data := &TemplateData{
		UserEmail:   "test@example.com",
		RegionName:  "Test Region",
		GroupName:   "Test Group",
		VoucherName: "TestVoucher",
	}

	for _, notifType := range notificationTypes {
		notification := &models.EmailNotification{
			NotificationType: notifType,
		}

		msg := templates.Build(notification, data)
		content := msg.Subject + msg.TextContent + msg.HTMLContent

		for _, sensitive := range sensitiveStrings {
			if strings.Contains(strings.ToLower(content), strings.ToLower(sensitive)) {
				t.Errorf("Notification type %s contains sensitive string '%s'", notifType, sensitive)
			}
		}
	}
}

func TestEmailTemplates_LoginURL(t *testing.T) {
	loginURL := "https://example.com/login"
	templates := NewEmailTemplates("Test App", loginURL)

	notificationTypes := []models.NotificationType{
		models.NotificationTypeInviteLinkUpdated,
		models.NotificationTypeVerificationComplete,
		models.NotificationTypeVouchReceived,
		models.NotificationTypeVouchComplete,
	}

	data := &TemplateData{UserEmail: "test@example.com"}

	for _, notifType := range notificationTypes {
		notification := &models.EmailNotification{
			NotificationType: notifType,
		}

		msg := templates.Build(notification, data)

		// All emails should include the login URL
		if !strings.Contains(msg.TextContent, loginURL) {
			t.Errorf("Notification type %s TextContent should contain login URL", notifType)
		}
		if !strings.Contains(msg.HTMLContent, loginURL) {
			t.Errorf("Notification type %s HTMLContent should contain login URL", notifType)
		}
	}
}

func TestEmailTemplates_HTMLEscaping(t *testing.T) {
	templates := NewEmailTemplates("Test App", "http://localhost/login")

	// Test that user-provided data is properly escaped in HTML
	data := &TemplateData{
		UserEmail:   "test@example.com",
		RegionName:  "<script>alert('xss')</script>",
		VoucherName: "<img src=x onerror=alert('xss')>",
	}

	notification := &models.EmailNotification{
		NotificationType: models.NotificationTypeVerificationComplete,
	}

	msg := templates.Build(notification, data)

	// HTML content should not contain unescaped script tags
	if strings.Contains(msg.HTMLContent, "<script>") {
		t.Error("HTML content contains unescaped script tag")
	}

	// The html/template package should escape < to &lt;
	// If the escaped version appears, that's correct behavior
	// If the region name doesn't appear at all (stripped), that's also acceptable
	if strings.Contains(msg.HTMLContent, "<script>") {
		t.Error("HTML content should have escaped or stripped script tags")
	}
}
