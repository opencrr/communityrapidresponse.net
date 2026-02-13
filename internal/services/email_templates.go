package services

import (
	"fmt"
	"html"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// EmailTemplates generates email content for notification types
type EmailTemplates struct {
	appName  string
	loginURL string
}

// NewEmailTemplates creates a new EmailTemplates instance
func NewEmailTemplates(appName, loginURL string) *EmailTemplates {
	return &EmailTemplates{
		appName:  appName,
		loginURL: loginURL,
	}
}

// TemplateData contains data for rendering email templates
type TemplateData struct {
	UserEmail   string
	RegionName  string
	GroupName   string
	VoucherName string
	InviterName string
}

// Build generates an EmailMessage for a notification
func (t *EmailTemplates) Build(n *models.EmailNotification, data *TemplateData) *EmailMessage {
	switch n.NotificationType {
	case models.NotificationTypeInviteLinkUpdated:
		return t.buildInviteLinkUpdated(data)
	case models.NotificationTypeVerificationComplete:
		return t.buildVerificationComplete(data)
	case models.NotificationTypeVouchReceived:
		return t.buildVouchReceived(data)
	case models.NotificationTypeVouchComplete:
		return t.buildVouchComplete(data)
	case models.NotificationTypeSubRegionInvitation:
		return t.buildSubRegionInvitation(data)
	case models.NotificationTypeRekeyingNeeded:
		return t.buildRekeyingNeeded(data)
	default:
		return t.buildGeneric(n, data)
	}
}

// buildInviteLinkUpdated generates email for invite link updates
// SECURITY: Never include the actual invite link in email
func (t *EmailTemplates) buildInviteLinkUpdated(data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("Signal Group Invite Link Updated - %s", t.appName)

	textContent := fmt.Sprintf(`A Signal group invite link has been updated for a group in your region.

To view the new invite link, please log in to your %s account:
%s

For security reasons, invite links are never included in emails.

- The %s Team`,
		t.appName, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<p>A Signal group invite link has been updated for a group in your region.</p>
<p>To view the new invite link, please log in to your %s account:</p>
<p><a href="%s" style="color: #0066cc;">%s</a></p>
<p style="color: #666; font-size: 14px;"><em>For security reasons, invite links are never included in emails.</em></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(t.appName),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// buildVerificationComplete generates email for postcard verification completion
func (t *EmailTemplates) buildVerificationComplete(data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("Your Address Has Been Verified - %s", t.appName)

	regionInfo := ""
	if data.RegionName != "" {
		regionInfo = fmt.Sprintf(" You now have read-only access to Signal groups in %s.", data.RegionName)
	}

	textContent := fmt.Sprintf(`Congratulations! Your address has been verified.%s

To become a fully verified member who can vouch for others and manage groups, you'll need to be vouched for by two verified neighbors.

Log in to continue:
%s

- The %s Team`,
		regionInfo, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<h2 style="color: #2e7d32;">Congratulations!</h2>
<p>Your address has been verified.%s</p>
<p>To become a fully verified member who can vouch for others and manage groups, you'll need to be vouched for by two verified neighbors.</p>
<p><a href="%s" style="display: inline-block; background: #1976d2; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">Log In to Continue</a></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(regionInfo),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// buildVouchReceived generates email when user receives a vouch
func (t *EmailTemplates) buildVouchReceived(data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("You Received a Vouch - %s", t.appName)

	voucherInfo := "A verified neighbor"
	if data.VoucherName != "" {
		voucherInfo = data.VoucherName
	}

	textContent := fmt.Sprintf(`%s has vouched for you!

You need two vouches from verified neighbors to become fully verified. Once fully verified, you'll be able to create regions, manage Signal groups, and vouch for others.

Log in to check your verification status:
%s

- The %s Team`,
		voucherInfo, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<h2 style="color: #1976d2;">You Received a Vouch!</h2>
<p><strong>%s</strong> has vouched for you.</p>
<p>You need two vouches from verified neighbors to become fully verified. Once fully verified, you'll be able to create regions, manage Signal groups, and vouch for others.</p>
<p><a href="%s" style="display: inline-block; background: #1976d2; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">Check Your Status</a></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(voucherInfo),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// buildVouchComplete generates email when user achieves vouch verification
func (t *EmailTemplates) buildVouchComplete(data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("You're Now Vouch-Verified - %s", t.appName)

	regionInfo := ""
	if data.RegionName != "" {
		regionInfo = fmt.Sprintf(" in %s", data.RegionName)
	}

	textContent := fmt.Sprintf(`Congratulations! You're now vouch-verified%s.

You now have access to view Signal groups in your region. If you're also postcard-verified, you can now:
- Create new regions within your area
- Manage Signal groups
- Vouch for other neighbors

Log in to get started:
%s

- The %s Team`,
		regionInfo, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<h2 style="color: #2e7d32;">Congratulations!</h2>
<p>You're now vouch-verified%s.</p>
<p>You now have access to view Signal groups in your region. If you're also postcard-verified, you can now:</p>
<ul>
<li>Create new regions within your area</li>
<li>Manage Signal groups</li>
<li>Vouch for other neighbors</li>
</ul>
<p><a href="%s" style="display: inline-block; background: #2e7d32; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">Get Started</a></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(regionInfo),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// buildSubRegionInvitation generates email when user is invited to a sub-region
func (t *EmailTemplates) buildSubRegionInvitation(data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("You've Been Invited to a Region - %s", t.appName)

	inviterInfo := "An admin"
	if data.InviterName != "" {
		inviterInfo = data.InviterName
	}

	textContent := fmt.Sprintf(`%s has invited you to join a sub-region in your community.

Log in to view and respond to your invitation:
%s

Invitations expire after 7 days if not accepted.

- The %s Team`,
		inviterInfo, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<h2 style="color: #1976d2;">You've Been Invited!</h2>
<p><strong>%s</strong> has invited you to join a sub-region in your community.</p>
<p><a href="%s" style="display: inline-block; background: #1976d2; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">View Invitation</a></p>
<p style="color: #666; font-size: 14px;"><em>Invitations expire after 7 days if not accepted.</em></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(inviterInfo),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// buildRekeyingNeeded generates email when a community member needs encryption re-keying
func (t *EmailTemplates) buildRekeyingNeeded(data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("Encryption Re-keying Needed - %s", t.appName)

	textContent := fmt.Sprintf(`A member of your community has rotated their encryption keys and needs re-keying assistance.

To help restore their access to shared encrypted data, please log in to your %s account:
%s

Re-keying happens automatically when you log in - no manual action is required.

- The %s Team`,
		t.appName, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<h2 style="color: #1976d2;">Encryption Re-keying Needed</h2>
<p>A member of your community has rotated their encryption keys and needs re-keying assistance.</p>
<p>To help restore their access to shared encrypted data, please log in:</p>
<p><a href="%s" style="display: inline-block; background: #1976d2; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">Log In</a></p>
<p style="color: #666; font-size: 14px;"><em>Re-keying happens automatically when you log in — no manual action is required.</em></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// BuildPasswordReset generates a password reset email with the reset URL.
// This is sent immediately (not queued via notification worker).
func (t *EmailTemplates) BuildPasswordReset(toEmail, resetURL string) *EmailMessage {
	subject := fmt.Sprintf("Password Reset Request - %s", t.appName)

	textContent := fmt.Sprintf(`You requested a password reset for your %s account.

Click the link below to reset your password. This link will expire in 1 hour.

%s

If you didn't request this password reset, you can safely ignore this email. Your password will not be changed.

- The %s Team`,
		t.appName, resetURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<h2 style="color: #1976d2;">Password Reset Request</h2>
<p>You requested a password reset for your %s account.</p>
<p>Click the button below to reset your password. This link will expire in 1 hour.</p>
<p><a href="%s" style="display: inline-block; background: #1976d2; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">Reset Password</a></p>
<p style="color: #666; font-size: 14px;"><em>If you didn't request this password reset, you can safely ignore this email. Your password will not be changed.</em></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(t.appName),
		html.EscapeString(resetURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          toEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}

// buildGeneric generates a generic notification email
func (t *EmailTemplates) buildGeneric(n *models.EmailNotification, data *TemplateData) *EmailMessage {
	subject := fmt.Sprintf("Notification - %s", t.appName)

	textContent := fmt.Sprintf(`You have a new notification from %s.

Log in to view details:
%s

- The %s Team`,
		t.appName, t.loginURL, t.appName)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
<p>You have a new notification from %s.</p>
<p><a href="%s" style="display: inline-block; background: #1976d2; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px;">Log In to View Details</a></p>
<p>- The %s Team</p>
</body>
</html>`,
		html.EscapeString(t.appName),
		html.EscapeString(t.loginURL),
		html.EscapeString(t.appName))

	return &EmailMessage{
		To:          data.UserEmail,
		Subject:     subject,
		TextContent: textContent,
		HTMLContent: htmlContent,
	}
}
