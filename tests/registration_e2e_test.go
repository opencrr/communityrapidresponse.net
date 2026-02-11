package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/opencrr/communityrapidresponse.net/internal/models"
)

// TestE2E_Registration_Success tests successful user registration
func TestE2E_Registration_Success(t *testing.T) {
	suite := SetupE2ETest(t)

	t.Run("registers user with valid data", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "validuser",
			"email":    "valid@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", resp.StatusCode)
		}

		var body models.RegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if body.UserID == "" {
			t.Error("Expected user_id in response")
		}
		if body.Username != "validuser" {
			t.Errorf("Expected username 'validuser', got '%s'", body.Username)
		}
		if body.Email != "valid@example.com" {
			t.Errorf("Expected email 'valid@example.com', got '%s'", body.Email)
		}

		// Cleanup
		suite.cleanup(body.UserID)
	})

	t.Run("registers user with minimum password length", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "minpassuser",
			"email":    "minpass@example.com",
			"password": "exactly12chr", // exactly 12 characters
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 for 12-char password, got %d", resp.StatusCode)
		}

		var body models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		suite.cleanup(body.UserID)
	})

	t.Run("user created with correct initial state", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "initialstate",
			"email":    "initial@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		var body models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		defer suite.cleanup(body.UserID)

		// Verify user state in database
		ctx := context.Background()
		user, err := suite.userRepo.GetByID(ctx, body.UserID)
		if err != nil {
			t.Fatalf("Failed to get user from DB: %v", err)
		}

		if user.VerificationTier != models.TierUnverified {
			t.Errorf("Expected verification tier %d, got %d", models.TierUnverified, user.VerificationTier)
		}
		if user.PostcardVerified {
			t.Error("Expected postcard_verified to be false")
		}
		if user.VouchVerified {
			t.Error("Expected vouch_verified to be false")
		}
		if user.IsSuperuser {
			t.Error("Expected is_superuser to be false")
		}
		if user.LastLogin != nil {
			t.Error("Expected last_login to be nil before login")
		}
	})

	t.Run("registration does not set auth cookie", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "nocookie",
			"email":    "nocookie@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		var body models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		defer suite.cleanup(body.UserID)

		// Check that no token cookie is set
		cookies := resp.Cookies()
		for _, cookie := range cookies {
			if cookie.Name == "token" {
				t.Error("Registration should not set token cookie")
			}
		}
	})
}

// TestE2E_Registration_MissingFields tests validation for missing required fields
func TestE2E_Registration_MissingFields(t *testing.T) {
	suite := SetupE2ETest(t)

	testCases := []struct {
		name    string
		payload map[string]string
	}{
		{
			name:    "missing username",
			payload: map[string]string{"email": "test@example.com", "password": "securepassword123"},
		},
		{
			name:    "missing email",
			payload: map[string]string{"username": "testuser", "password": "securepassword123"},
		},
		{
			name:    "missing password",
			payload: map[string]string{"username": "testuser", "email": "test@example.com"},
		},
		{
			name:    "missing username and email",
			payload: map[string]string{"password": "securepassword123"},
		},
		{
			name:    "missing username and password",
			payload: map[string]string{"email": "test@example.com"},
		},
		{
			name:    "missing email and password",
			payload: map[string]string{"username": "testuser"},
		},
		{
			name:    "all fields missing",
			payload: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := suite.request("POST", "/api/v1/auth/register", tc.payload, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", resp.StatusCode)
			}

			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)

			if body["error"] == nil {
				t.Error("Expected error field in response")
			}
		})
	}
}

// TestE2E_Registration_EmptyFields tests validation for empty string fields
func TestE2E_Registration_EmptyFields(t *testing.T) {
	suite := SetupE2ETest(t)

	testCases := []struct {
		name    string
		payload map[string]string
	}{
		{
			name:    "empty username",
			payload: map[string]string{"username": "", "email": "test@example.com", "password": "securepassword123"},
		},
		{
			name:    "empty email",
			payload: map[string]string{"username": "testuser", "email": "", "password": "securepassword123"},
		},
		{
			name:    "empty password",
			payload: map[string]string{"username": "testuser", "email": "test@example.com", "password": ""},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := suite.request("POST", "/api/v1/auth/register", tc.payload, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", resp.StatusCode)
			}
		})
	}
}

// TestE2E_Registration_PasswordValidation tests password length requirements
func TestE2E_Registration_PasswordValidation(t *testing.T) {
	suite := SetupE2ETest(t)

	testCases := []struct {
		name           string
		password       string
		expectedStatus int
	}{
		{
			name:           "password too short - 1 char",
			password:       "a",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "password too short - 5 chars",
			password:       "short",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "password too short - 11 chars",
			password:       "almostthere", // 11 characters
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "password minimum length - 12 chars",
			password:       "justbarely12", // 12 characters
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "password longer than minimum",
			password:       "thisisaverylongpassword123",
			expectedStatus: http.StatusCreated,
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
				"username": "passtest" + string(rune('a'+i)),
				"email":    "passtest" + string(rune('a'+i)) + "@example.com",
				"password": tc.password,
			}, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			// Cleanup successful registrations
			if tc.expectedStatus == http.StatusCreated {
				var body models.RegisterResponse
				_ = json.NewDecoder(resp.Body).Decode(&body)
				suite.cleanup(body.UserID)
			}
		})
	}
}

// TestE2E_Registration_InvalidJSON tests handling of malformed JSON
func TestE2E_Registration_InvalidJSON(t *testing.T) {
	suite := SetupE2ETest(t)

	testCases := []struct {
		name    string
		payload string
	}{
		{
			name:    "completely invalid JSON",
			payload: "not json at all",
		},
		{
			name:    "malformed JSON - missing closing brace",
			payload: `{"username": "test"`,
		},
		{
			name:    "malformed JSON - invalid syntax",
			payload: `{"username": "test",}`,
		},
		{
			name:    "empty body",
			payload: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Make raw request with invalid JSON
			req, _ := http.NewRequest("POST", suite.server.URL+"/api/v1/auth/register", strings.NewReader(tc.payload))
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", resp.StatusCode)
			}
		})
	}
}

// TestE2E_Registration_DuplicateUsername tests duplicate username rejection
func TestE2E_Registration_DuplicateUsername(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register first user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "duplicateuser",
		"email":    "original@example.com",
		"password": "securepassword123",
	}, "")
	var firstUser models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&firstUser)
	_ = resp.Body.Close()
	defer suite.cleanup(firstUser.UserID)

	t.Run("rejects duplicate username", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "duplicateuser",
			"email":    "different@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["error"] != "user_exists" {
			t.Errorf("Expected error 'user_exists', got '%v'", body["error"])
		}
	})
}

// TestE2E_Registration_DuplicateEmail tests duplicate email rejection
func TestE2E_Registration_DuplicateEmail(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register first user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "firstuser",
		"email":    "duplicate@example.com",
		"password": "securepassword123",
	}, "")
	var firstUser models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&firstUser)
	_ = resp.Body.Close()
	defer suite.cleanup(firstUser.UserID)

	t.Run("rejects duplicate email", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "seconduser",
			"email":    "duplicate@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["error"] != "email_exists" {
			t.Errorf("Expected error 'email_exists', got '%v'", body["error"])
		}
	})
}

// TestE2E_Registration_UniqueConstraints tests that username and email uniqueness are independent
func TestE2E_Registration_UniqueConstraints(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register first user
	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "uniquetest",
		"email":    "unique@example.com",
		"password": "securepassword123",
	}, "")
	var firstUser models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&firstUser)
	_ = resp.Body.Close()
	defer suite.cleanup(firstUser.UserID)

	t.Run("allows different username with different email", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
			"username": "differentuser",
			"email":    "different@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", resp.StatusCode)
		}

		var body models.RegisterResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		suite.cleanup(body.UserID)
	})
}

// TestE2E_Registration_SequentialUsers tests that multiple users can register
func TestE2E_Registration_SequentialUsers(t *testing.T) {
	suite := SetupE2ETest(t)

	var userIDs []string

	for i := 0; i < 3; i++ {
		t.Run("register user "+string(rune('A'+i)), func(t *testing.T) {
			resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
				"username": "sequser" + string(rune('a'+i)),
				"email":    "sequser" + string(rune('a'+i)) + "@example.com",
				"password": "securepassword123",
			}, "")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusCreated {
				t.Errorf("Expected status 201, got %d", resp.StatusCode)
			}

			var body models.RegisterResponse
			_ = json.NewDecoder(resp.Body).Decode(&body)
			userIDs = append(userIDs, body.UserID)
		})
	}

	// Cleanup all users
	suite.cleanup(userIDs...)
}

// TestE2E_Registration_ResponseFormat tests the exact response format
func TestE2E_Registration_ResponseFormat(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "formattest",
		"email":    "format@example.com",
		"password": "securepassword123",
	}, "")
	defer func() { _ = resp.Body.Close() }()

	// Decode as raw map to verify field names
	var rawBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify expected fields exist
	expectedFields := []string{"user_id", "username", "email"}
	for _, field := range expectedFields {
		if _, exists := rawBody[field]; !exists {
			t.Errorf("Expected field '%s' in response", field)
		}
	}

	// Verify no extra fields (like password_hash which should never be exposed)
	unexpectedFields := []string{"password", "password_hash", "verification_tier", "is_superuser"}
	for _, field := range unexpectedFields {
		if _, exists := rawBody[field]; exists {
			t.Errorf("Unexpected field '%s' in registration response", field)
		}
	}

	// Cleanup
	if userID, ok := rawBody["user_id"].(string); ok {
		suite.cleanup(userID)
	}
}

// TestE2E_Registration_ContentType tests Content-Type header in response
func TestE2E_Registration_ContentType(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "contenttypetest",
		"email":    "contenttype@example.com",
		"password": "securepassword123",
	}, "")
	defer func() { _ = resp.Body.Close() }()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	var body models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	suite.cleanup(body.UserID)
}

// TestE2E_Registration_ErrorResponseFormat tests error response structure
func TestE2E_Registration_ErrorResponseFormat(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "errorformat",
		// missing email and password
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	// Verify error response has expected structure
	if body["error"] == nil {
		t.Error("Expected 'error' field in error response")
	}
	if body["message"] == nil {
		t.Error("Expected 'message' field in error response")
	}
}

// TestE2E_Registration_LoginAfterRegister tests that user can login immediately after registration
func TestE2E_Registration_LoginAfterRegister(t *testing.T) {
	suite := SetupE2ETest(t)

	// Register user
	registerResp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "loginafter",
		"email":    "loginafter@example.com",
		"password": "securepassword123",
	}, "")
	var regBody models.RegisterResponse
	_ = json.NewDecoder(registerResp.Body).Decode(&regBody)
	_ = registerResp.Body.Close()
	defer suite.cleanup(regBody.UserID)

	t.Run("can login with registered credentials", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "loginafter@example.com",
			"password": "securepassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body models.LoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body.Token == "" {
			t.Error("Expected token in login response")
		}
		if body.User == nil {
			t.Error("Expected user in login response")
		}
	})

	t.Run("cannot login with wrong password", func(t *testing.T) {
		resp := suite.request("POST", "/api/v1/auth/login", map[string]string{
			"email":    "loginafter@example.com",
			"password": "wrongpassword123",
		}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})
}

// TestE2E_Registration_UUIDFormat tests that user_id is a valid UUID
func TestE2E_Registration_UUIDFormat(t *testing.T) {
	suite := SetupE2ETest(t)

	resp := suite.request("POST", "/api/v1/auth/register", map[string]string{
		"username": "uuidtest",
		"email":    "uuid@example.com",
		"password": "securepassword123",
	}, "")
	defer func() { _ = resp.Body.Close() }()

	var body models.RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	defer suite.cleanup(body.UserID)

	// UUID format: 8-4-4-4-12 hex characters
	if len(body.UserID) != 36 {
		t.Errorf("Expected UUID length 36, got %d", len(body.UserID))
	}

	// Check for dashes in correct positions
	if body.UserID[8] != '-' || body.UserID[13] != '-' || body.UserID[18] != '-' || body.UserID[23] != '-' {
		t.Errorf("User ID does not match UUID format: %s", body.UserID)
	}
}
