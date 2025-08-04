# Workflow UI Documentation

This document describes the Workflow UI feature that provides a web-based interface for managing workflows with multi-tenancy support.

## Overview

The Workflow UI is a complete web application that allows users to:

- **Authenticate** with username/password and JWT-based sessions
- **Manage workflows** (create, read, update, delete, execute)
- **Monitor executions** with real-time status and log viewing
- **Multi-tenant support** for isolated workflow management
- **Role-based access control** (admin, user, viewer roles)

## Architecture

### Components

1. **UI Module** (`ui/module.go`): Main module that integrates with the workflow engine
2. **Database Service** (`ui/database.go`): Handles data persistence with SQLite
3. **Authentication Service** (`ui/auth.go`): JWT-based authentication and authorization
4. **API Handlers** (`ui/handlers.go`): REST API endpoints for the frontend
5. **Models** (`ui/models.go`): Data structures for users, tenants, workflows, and executions
6. **Frontend** (`ui/static/`): Vue.js-based web interface

### Database Schema

The UI uses SQLite with the following tables:

- **tenants**: Multi-tenant organization support
- **users**: User accounts with role-based access
- **workflows**: Stored workflow configurations
- **workflow_executions**: Execution history and logs

## Configuration

Add the UI module to your workflow configuration:

```yaml
modules:
  - name: ui-web
    type: ui.web
    config:
      address: ":8080"              # Server address
      staticDir: "./ui/static"      # Static files directory
      secretKey: "your-secret-key"  # JWT signing key
      database: "./workflow_ui.db"  # SQLite database path

# Optional: Add database module for advanced database configuration
  - name: ui-database
    type: database.modular
    config:
      type: sqlite
      dsn: "./workflow_ui.db"
```

## Getting Started

### 1. Run the UI

```bash
# Using the example configuration
go run example/main.go -config example/ui-workflow-config.yaml

# Or with custom configuration
go run example/main.go -config your-ui-config.yaml
```

### 2. Access the Web Interface

Open your browser to `http://localhost:8080`

### 3. Default Login

- **Username**: `admin`
- **Password**: `admin`
- **Tenant**: `default`

## API Endpoints

### Authentication

- `POST /api/login` - User authentication
  ```json
  {
    "username": "admin",
    "password": "admin"
  }
  ```

### Workflow Management

- `GET /api/workflows` - List workflows (paginated)
- `POST /api/workflows` - Create new workflow
- `GET /api/workflows/{id}` - Get workflow details
- `PUT /api/workflows/{id}` - Update workflow
- `DELETE /api/workflows/{id}` - Delete workflow (soft delete)
- `POST /api/workflows/{id}/execute` - Execute workflow

### Execution Management

- `GET /api/workflows/{id}/executions` - List workflow executions
- Executions include status, logs, input/output data, and timing information

### User Management (Admin only)

- `POST /api/users` - Create new user

## Frontend Features

### Dashboard

- Overview of workflow metrics
- Recent workflows and executions
- Status distribution charts

### Workflow Management

- **Create/Edit Workflows**: YAML configuration editor with syntax highlighting
- **Execute Workflows**: Run workflows with custom input parameters
- **Status Monitoring**: Real-time workflow status updates
- **Configuration Validation**: YAML syntax checking before save

### Execution Monitoring

- **Execution History**: Complete list of workflow runs
- **Log Viewing**: Real-time log streaming during execution
- **Status Tracking**: Running, completed, failed, stopped states
- **Performance Metrics**: Execution duration and timestamps

### User Interface

- **Responsive Design**: Works on desktop, tablet, and mobile
- **Vue.js Frontend**: Modern reactive user interface
- **Bootstrap Styling**: Professional and consistent design
- **Real-time Updates**: Live status and log updates

## Multi-Tenancy

The UI supports full multi-tenancy:

- **Tenant Isolation**: Users can only access workflows within their tenant
- **Shared Resources**: Templates and examples can be shared across tenants
- **Admin Access**: Admin users can manage multiple tenants
- **Tenant-specific Configuration**: Each tenant can have custom settings

## Security Features

### Authentication
- **JWT Tokens**: Secure session management
- **Password Hashing**: bcrypt for secure password storage
- **Session Expiry**: Configurable token expiration

### Authorization
- **Role-based Access**: Admin, user, and viewer roles
- **API Protection**: All endpoints require valid authentication
- **Tenant Validation**: Users can only access their tenant's data

### CORS Support
- **Development-friendly**: CORS headers for frontend development
- **Production-ready**: Configurable origins for security

## Testing

The UI includes comprehensive testing:

### BDD Tests (Godog)

Located in `tests/bdd/`:

```bash
# Run BDD tests
npm run test:bdd
# or
cd tests/bdd && go test -v
```

Features tested:
- Authentication flows
- Workflow CRUD operations
- Multi-tenant isolation
- API security

### E2E Tests (Playwright)

Located in `tests/e2e/`:

```bash
# Run E2E tests
npm run test:e2e

# Run with UI for debugging
npm run test:e2e:ui

# Run in headed mode to see browser
npm run test:e2e:headed
```

Features tested:
- Complete user journeys
- UI responsiveness
- Cross-browser compatibility
- Screenshot validation

## Development

### Frontend Development

The frontend is a Vue.js SPA that communicates with the Go backend via REST APIs.

**Key files:**
- `ui/static/index.html` - Main HTML template
- `ui/static/app.js` - Vue.js application logic

**Development workflow:**
1. Start the backend: `npm run start:ui`
2. Make frontend changes in `ui/static/`
3. Refresh browser to see changes

### Backend Development

The backend follows the modular architecture pattern:

**Key files:**
- `ui/module.go` - Main module implementation
- `ui/handlers.go` - HTTP request handlers
- `ui/database.go` - Data access layer
- `ui/auth.go` - Authentication logic

### Adding Features

1. **New API Endpoints**: Add to `ui/handlers.go`
2. **Database Changes**: Update `ui/database.go` and schema
3. **Frontend Features**: Modify `ui/static/app.js` and `ui/static/index.html`
4. **Add Tests**: Update both BDD and E2E test suites

## Troubleshooting

### Common Issues

1. **Database Connection Errors**
   - Ensure SQLite file permissions are correct
   - Check if database directory exists

2. **Authentication Failures**
   - Verify JWT secret key configuration
   - Check user credentials in database

3. **Frontend Loading Issues**
   - Confirm static files are in correct directory
   - Check CORS configuration for development

4. **API Errors**
   - Verify all required modules are loaded
   - Check application logs for detailed error messages

### Logging

Enable debug logging in your configuration:

```yaml
# Add to your workflow config
logging:
  level: debug
```

### Performance

For production deployments:

1. **Database**: Consider PostgreSQL for multi-user environments
2. **Static Files**: Use a CDN or reverse proxy for static assets
3. **Sessions**: Configure appropriate JWT expiry times
4. **Monitoring**: Add metrics collection for workflow executions

## Configuration Examples

### Development Configuration

```yaml
modules:
  - name: ui-web
    type: ui.web
    config:
      address: ":8080"
      staticDir: "./ui/static"
      secretKey: "dev-secret-key"
      database: "./dev_workflow_ui.db"
```

### Production Configuration

```yaml
modules:
  - name: ui-database
    type: database.modular
    config:
      type: postgres
      dsn: "postgres://user:pass@localhost/workflow_ui?sslmode=disable"
  
  - name: ui-web
    type: ui.web
    config:
      address: ":8080"
      staticDir: "/app/static"
      secretKey: "${JWT_SECRET_KEY}"  # Environment variable
```

## API Reference

### Authentication Response

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "uuid",
    "username": "admin",
    "email": "admin@example.com",
    "role": "admin",
    "tenant_id": "uuid"
  },
  "tenant": {
    "id": "uuid",
    "name": "default",
    "description": "Default tenant"
  },
  "expires_at": "2024-08-05T06:13:47Z"
}
```

### Workflow Object

```json
{
  "id": "uuid",
  "tenant_id": "uuid",
  "user_id": "uuid",
  "name": "My Workflow",
  "description": "A sample workflow",
  "config": "modules:\n  - name: server\n    type: http.server",
  "status": "stopped",
  "active": true,
  "created_at": "2024-08-04T06:13:47Z",
  "updated_at": "2024-08-04T06:13:47Z"
}
```

### Execution Object

```json
{
  "id": "uuid",
  "workflow_id": "uuid",
  "tenant_id": "uuid", 
  "user_id": "uuid",
  "status": "completed",
  "input": {"param": "value"},
  "output": {"result": "success"},
  "logs": ["Starting execution", "Completed successfully"],
  "started_at": "2024-08-04T06:13:47Z",
  "ended_at": "2024-08-04T06:13:50Z",
  "created_at": "2024-08-04T06:13:47Z"
}
```

This comprehensive UI system provides a complete workflow management solution with enterprise-grade features including security, multi-tenancy, and thorough testing coverage.