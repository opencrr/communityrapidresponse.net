# Community Rapid Response

A secure platform that enables communities to connect within specific geographic regions through verified Signal group chats. The system proves residency before granting access, maintaining privacy through ephemeral verification data.

## Core Principles

- **Privacy First**: Addresses are NEVER stored - they exist only in memory during verification and are sent directly to our mailing partner
- **Dual Verification**: Complete both postcard and vouch verification to become an admin; either one grants read-only access to Signal groups
- **Community Governance**: Consensus-based moderation requiring 3-admin approval for critical actions
- **Geographic Hierarchy**: State > County > City > Locality > Neighborhood > City Block

## Features

### User Verification

- **Postcard Verification**: Physical postcard with one-time code sent to your address (address is never stored)
  - Grants read-only access to Signal groups in your region
  - Combined with vouch verification, grants full admin rights

- **Vouch Verification**: Endorsed by 2+ fully verified users from a shared geographic region (same neighborhood, city, or county)
  - Grants read-only access to Signal groups in your region
  - Combined with postcard verification, grants full admin rights

- **Admin (Both verifications)**: Can create regions, manage Signal groups, vouch for others

### Geographic Regions

```
State
    └── County
        └── City/Town
            └── Locality (optional, e.g., Brooklyn in NYC)
                └── Neighborhood
                    └── City Block
```

### Schools & School Districts

- Browse NCES-sourced schools and districts by name or state
- Join schools and get verified through a vouch-based system (similar to regions)
- Schools support their own Signal groups, independent from region groups
- School districts can also have their own Signal groups
- Bootstrap mode for new schools (3 vouches required, relaxed voucher requirements)

### Signal Group Integration

- Admins can create Signal groups for regions, schools, or school districts
- Invite link updates require 3-admin consensus
- Email notifications (without sensitive links) sent when groups are updated

## Technology Stack

- **Backend**: Go (net/http standard library)
- **Database**: MariaDB with spatial extensions
- **Mapping**: Mapbox GL JS (geocoding, polygon drawing)
- **Address Validation & Mail**: Postgrid API
- **Authentication**: JWT tokens
- **Containerization**: Docker & Docker Compose

## Getting Started

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Go 1.24+](https://golang.org/dl/)
- [just](https://github.com/casey/just) command runner (recommended)

### Quick Start

```bash
# Clone the repository
git clone https://github.com/opencrr/communityrapidresponse.net.git
cd communityrapidresponse

# Initialize the development environment
just init

# Start the development server
just dev
```

The API will be available at `http://localhost:8080`.

### Environment Variables

Copy `.env.example` to `.env` and configure. Key variables:

```bash
# Server
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
SECURE_COOKIES=false  # Set to true in production (requires HTTPS)

# Database
DB_HOST=db
DB_PORT=3306
DB_USER=communityrapidresponse
DB_PASSWORD=devpassword
DB_NAME=communityrapidresponse

# JWT Authentication
JWT_SECRET=your_secret_key_at_least_32_characters
JWT_EXPIRATION_HOURS=24
JWT_ISSUER=communityrapidresponse

# MFA (Multi-Factor Authentication)
MFA_REQUIRED=false  # Set to true for production
MFA_ENCRYPTION_KEY=your_32_character_encryption_key  # Required if MFA_REQUIRED=true
MFA_ISSUER=Community Rapid Response

# Email Verification
EMAIL_ENABLED=false  # Set to true for production
EMAIL_BACKEND=mock   # mock, sendgrid, or smtp
EMAIL_VERIFICATION_URL=http://localhost:3000/verify-email
EMAIL_FROM_ADDRESS=noreply@communityrapidresponse.net
SENDGRID_API_KEY=    # Required if EMAIL_BACKEND=sendgrid

# Mapbox (geocoding and maps)
MAPBOX_PUBLIC_TOKEN=your_mapbox_public_token
MAPBOX_SECRET_TOKEN=your_mapbox_secret_token

# Address Verification & Postcard Mailing (Lob recommended)
MAIL_PROVIDER=lob
LOB_API_KEY=your_lob_api_key
LOB_BASE_URL=https://api.lob.com/v1
LOB_RETURN_NAME=Community Rapid Response
LOB_RETURN_ADDRESS=123 Main St
LOB_RETURN_CITY=Your City
LOB_RETURN_STATE=CA
LOB_RETURN_ZIP=12345

# Rate Limiting
RATE_LIMIT_ENABLED=true
RATE_LIMIT_IP_LIMIT=100
RATE_LIMIT_IP_WINDOW_SECS=60

# CORS
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:8080
```

See `.env.example` for complete configuration options including SMTP settings and Postgrid (legacy) support.

## Development

### Available Commands

Run `just` or `just help` to see all available commands:

| Command | Description |
|---------|-------------|
| `just init` | First-time setup |
| `just dev` | Start development server with hot reload (Docker) |
| `just dev-local` | Start development server locally |
| `just test` | Run all tests in Docker |
| `just test-unit` | Run unit tests only (no database) |
| `just test-coverage` | Run tests with coverage report |
| `just lint` | Run linters |
| `just fmt` | Format code |
| `just db-cli` | Connect to database CLI |
| `just db-migrate` | Run database migrations |
| `just db-reset` | Reset database (local only) |

#### Superuser Management

| Command | Description |
|---------|-------------|
| `just create-superuser` | Create a superuser account (interactive) |
| `just promote-superuser EMAIL` | Promote existing user to superuser |
| `just demote-superuser EMAIL` | Demote superuser to regular user |
| `just list-superusers` | List all superuser accounts |
| `just list-users` | List all user accounts |

### Project Structure

```
.
├── cmd/
│   ├── server/          # Application entrypoint
│   └── seed-schools/    # NCES school data seeder
├── internal/
│   ├── config/          # Configuration management
│   ├── database/        # Database layer & repositories
│   ├── handlers/        # HTTP request handlers
│   ├── middleware/      # HTTP middleware (auth, logging)
│   ├── models/          # Data models
│   ├── mocks/           # Mock implementations for testing
│   ├── services/        # External service integrations (email, rate limiting, etc.)
│   └── testutil/        # Test utilities
├── migrations/          # Database migrations
├── tests/               # Integration & E2E tests
├── docker-compose.yml   # Docker services configuration
├── Dockerfile           # Multi-stage build configuration
├── justfile             # Development commands (run `just` to see all)
└── static/              # Frontend assets (CSS, JS, images)
```

### Testing

Tests are designed to run inside Docker for consistent database access:

```bash
# Run all tests (recommended)
just test

# Run specific test categories
just test-unit         # Unit tests (no database)
just test-integration  # Integration tests
just test-e2e          # End-to-end tests
just test-handlers     # Handler tests
just test-db           # Database tests

# Run a specific test by name
just test-run TestAuthHandler_Login

# Generate coverage report
just test-coverage
```

## API Documentation

### Authentication

All authenticated endpoints require a JWT token in the `Authorization` header:

```
Authorization: Bearer <token>
```

### Endpoints

#### Auth

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/register` | Register a new user |
| POST | `/api/v1/auth/login` | Login (returns MFA token if MFA required) |
| POST | `/api/v1/auth/logout` | Logout (clears cookie) |
| GET | `/api/v1/users/me` | Get current user info |

#### MFA (Multi-Factor Authentication)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/mfa/setup` | Initialize MFA setup (returns QR code) |
| POST | `/api/v1/mfa/setup/complete` | Complete MFA setup with TOTP code |
| POST | `/api/v1/mfa/verify` | Verify TOTP or backup code during login |

**MFA Flow:**
1. All users must set up MFA on first login
2. Login returns `mfa_action: "setup"` or `mfa_action: "verify"` with a limited token
3. Complete MFA setup/verification to receive full access token

#### Verification

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/verification/postcard/request` | Request postcard verification |
| POST | `/api/v1/verification/postcard/verify` | Verify with postcard code |
| POST | `/api/v1/verification/vouch` | Vouch for another user |
| GET | `/api/v1/verification/vouch/status/:user_id` | Get vouch status |

#### Regions

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/regions` | List regions (authenticated, scoped by membership) |
| POST | `/api/v1/regions` | Create a region (admin only) |
| GET | `/api/v1/regions/:id` | Get region details with sub-regions |
| PUT | `/api/v1/regions/:id` | Update region name (admin/superuser) |
| DELETE | `/api/v1/regions/:id` | Delete region (superuser only) |

**Access Control:**
- All region endpoints require authentication
- Regular users see only regions they belong to (directly or via parent hierarchy)
- Superusers can see and filter all regions

**Sub-Regions:**
- `GET /api/v1/regions/:id` returns `sub_regions` array using geographic containment
- Sub-regions are determined using MariaDB ST_Contains (geometry-based)
- This allows cities to show as sub-regions of states even without explicit parent_id

**Region Type Hierarchy:**
- State regions cannot have a parent region
- County regions must have a State parent
- City/Town regions must have a County parent
- Locality regions must have a City parent
- Neighborhood regions must have a Locality OR City parent
- City Block regions must have a Neighborhood parent

#### Signal Groups

Signal groups can belong to a region, a school, or a school district (exactly one; three-way XOR constraint).

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/signal-groups` | List Signal groups |
| POST | `/api/v1/signal-groups` | Create a Signal group (admin only) |
| PUT | `/api/v1/signal-groups/:id` | Update Signal group |

#### Schools

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/schools` | Search schools by name/state |
| GET | `/api/v1/schools/my` | Get user's schools (auth) |
| GET | `/api/v1/schools/:id` | Get school details |
| POST | `/api/v1/schools/:id/join` | Join a school (auth) |
| POST | `/api/v1/schools/:id/leave` | Leave a school (auth) |
| POST | `/api/v1/schools/:id/vouch` | Vouch for a school member (auth) |
| GET | `/api/v1/schools/:id/vouch/pending` | List pending vouch requests (auth) |
| GET | `/api/v1/schools/:id/vouch-status/:user_id` | Get vouch status for user (auth) |
| GET | `/api/v1/schools/:id/members` | List school members (auth) |
| GET | `/api/v1/schools/:id/signal-groups` | List school Signal groups (auth) |
| POST | `/api/v1/schools/:id/signal-groups` | Create school Signal group (admin) |

#### School Districts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/school-districts` | Search districts by name/state |
| GET | `/api/v1/school-districts/:id` | Get district details |
| GET | `/api/v1/school-districts/:id/signal-groups` | List district Signal groups (auth) |
| POST | `/api/v1/school-districts/:id/signal-groups` | Create district Signal group (admin) |

### Example: Register and Login with MFA

```bash
# Register
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser","email":"test@example.com","password":"securepassword123"}'

# Login (first time - requires MFA setup)
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"securepassword123"}'
# Returns: {"token":"<mfa_setup_token>","mfa_action":"setup",...}

# Initialize MFA setup (get QR code)
curl -X POST http://localhost:8080/api/v1/mfa/setup \
  -H "Authorization: Bearer <mfa_setup_token>"
# Returns: {"qr_code":"data:image/png;base64,...","secret":"..."}

# Complete MFA setup with code from authenticator app
curl -X POST http://localhost:8080/api/v1/mfa/setup/complete \
  -H "Authorization: Bearer <mfa_setup_token>" \
  -H "Content-Type: application/json" \
  -d '{"code":"123456"}'
# Returns: {"token":"<full_token>","backup_codes":["XXXX-YYYY",...],...}

# Subsequent logins - verify with TOTP
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"securepassword123"}'
# Returns: {"token":"<pending_mfa_token>","mfa_action":"verify",...}

curl -X POST http://localhost:8080/api/v1/mfa/verify \
  -H "Authorization: Bearer <pending_mfa_token>" \
  -H "Content-Type: application/json" \
  -d '{"code":"123456"}'
# Returns: {"token":"<full_token>",...}
```

## Architecture

```
┌─────────────────────────────────┐
│         Web Frontend            │
│   (HTML/CSS/JS + Mapbox GL JS)  │
└────────────────┬────────────────┘
                 │
         ┌───────┴───────┐
         ▼               ▼
┌─────────────────┐  ┌──────────┐
│   Go Backend    │  │  Mapbox  │
│   (HTTP API)    │  │   API    │
└────────┬────────┘  └──────────┘
         │
    ┌────┴────┬──────────────┐
    ▼         ▼              ▼
┌─────────┐ ┌──────────┐ ┌──────────┐
│ MariaDB │ │Postgrid  │ │  Signal  │
│(Spatial)│ │   API    │ │ (Groups) │
└─────────┘ └──────────┘ └──────────┘
```

## Privacy & Security

### What We Store vs. What We Don't

| Stored | NOT Stored |
|--------|------------|
| City/county name | Street address |
| Region membership | Apartment/unit number |
| Postgrid tracking ID | GPS coordinates |
| Verification code (temporary) | Specific location |

### Security Features

- **MFA**: TOTP-based two-factor authentication (mandatory in production, optional in development via `MFA_REQUIRED=false`)
- **Backup codes**: 10 single-use backup codes for account recovery
- Passwords hashed with bcrypt
- JWT tokens with 24-hour expiration (MFA tokens: 10 minutes)
- HttpOnly cookies for token storage
- MFA secrets encrypted with AES-256
- Rate limiting on verification requests
- 3-admin consensus for critical actions
- PO Box and CMRA addresses rejected

## Deployment

Deployment infrastructure is maintained in a separate repository. See the deployment repo for provisioning, configuration management, and CI/CD workflows. For application-level deploy steps, env vars, and the production checklist, see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Documentation

Supplementary documentation lives at the repo root and under [`docs/`](docs/):

- [CONTRIBUTING.md](CONTRIBUTING.md) — local setup, testing, code style, PR workflow
- [SECURITY.md](SECURITY.md) — responsible disclosure policy and security posture
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — system architecture overview
- [docs/API.md](docs/API.md) — REST API reference
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) — deployment and operations guide
- [DESIGN.md](DESIGN.md) — authoritative design specification
- [CLAUDE.md](CLAUDE.md) — quick reference for AI assistants

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow (prerequisites, local setup, testing, code style, branching, and PR conventions). The short version:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests (`just test`)
5. Run linter (`just lint`)
6. Commit your changes (`git commit -m 'Add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [Mapbox](https://www.mapbox.com/) for geocoding and mapping services
- [Postgrid](https://www.postgrid.com/) for address validation and mail services
- [Signal](https://signal.org/) for secure messaging

---

**Document Version**: 1.4
**Last Updated**: 2026-02-07
