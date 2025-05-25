# Workflow UI

The Workflow UI provides a web-based interface for managing workflows within the Workflow Engine.

## Features

- View a list of all defined workflows
- View details of individual workflows
- Create new workflows with a YAML/JSON editor
- Edit existing workflows
- Delete workflows
- Visual representation of workflow status

## Setup

To enable the UI server, include a UI server module in your workflow configuration:

```yaml
modules:
  - name: ui-server
    type: ui.server
    config:
      address: ":8080"  # The address on which the UI server will listen
```

## Usage

1. Start your application with the UI server configured
2. Navigate to `http://localhost:8080` (or the configured address)
3. Use the workflow management interface to:
   - View existing workflows
   - Create new workflows
   - Edit workflows
   - Delete workflows

## API Endpoints

The UI server exposes the following API endpoints:

### GET /api/workflows

Returns a list of all workflows.

**Response:**
```json
{
  "workflows": [
    {
      "name": "workflow-name",
      "status": "active|configured",
      "description": "Workflow description"
    }
  ]
}
```

### GET /api/workflows/{name}

Returns details for a specific workflow.

**Response:**
```json
{
  "name": "workflow-name",
  "status": "active|configured",
  "config": {
    // Workflow configuration object
  }
}
```

### POST /api/workflows

Creates a new workflow.

**Request:**
```json
{
  "name": "new-workflow",
  "config": {
    // Workflow configuration object
  }
}
```

**Response:**
```json
{
  "status": "created",
  "name": "new-workflow"
}
```

### PUT /api/workflows/{name}

Updates an existing workflow.

**Request:**
```json
{
  "config": {
    // Updated workflow configuration
  }
}
```

**Response:**
```json
{
  "status": "updated",
  "name": "workflow-name"
}
```

### DELETE /api/workflows/{name}

Deletes a workflow.

**Response:**
```json
{
  "status": "deleted",
  "name": "workflow-name"
}
```

## Testing

The UI is tested using Playwright, a browser automation library. Tests are located in the `tests/ui` directory.

To run the UI tests:

```bash
npm run ui-test
```