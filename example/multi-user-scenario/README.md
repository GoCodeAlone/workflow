# Multi-User Access Control Scenario

This example demonstrates the platform's hierarchical access control model
with three users that have different permission levels across the resource
hierarchy.

## Resource Hierarchy

```
Acme Corp (company)
  |
  +-- Engineering (organization)
        |
        +-- E-Commerce (project)
              |
              +-- Workflow A: Order Ingestion
              +-- Workflow B: Fulfillment Processing
              +-- Workflow C: Notification Hub
```

## Users

### Alice (admin@acme.com) -- Company Owner

- **Role**: Owner of "Acme Corp" company
- **Access**: Full access to all resources (company, organizations, projects, workflows)
- **Can do**: Create/delete orgs, projects, workflows; manage members; deploy; view everything

### Bob (bob@acme.com) -- Project Editor

- **Role**: Editor on the "E-Commerce" project
- **Access**: Can edit workflows A and B within the E-Commerce project
- **Can do**: Edit workflow configs, trigger executions, view logs and dashboards
- **Cannot do**: Delete workflows, manage project members, access other projects

### Carol (carol@external.com) -- Workflow Viewer

- **Role**: Viewer on Workflow C only (via project membership scoped to viewer)
- **Access**: Read-only access to Workflow C (Notification Hub)
- **Can do**: View workflow status, read logs, view dashboard for Workflow C
- **Cannot do**: Edit configs, trigger executions, cancel runs, see Workflows A or B

## Permission Cascade

The platform resolves permissions by cascading through the hierarchy:

1. **Workflow access** checks project membership, then falls back to company membership
2. **Project access** checks project-level membership, then company-level
3. **Company access** checks company-level membership directly

Alice's owner role on "Acme Corp" cascades down to all organizations, projects,
and workflows. Bob's editor role on the "E-Commerce" project gives him editor
access to all workflows in that project. Carol has explicit viewer access scoped
to the project with restricted visibility.

## Setup

Run `setup.sh` to create the full hierarchy via API calls:

```bash
chmod +x example/multi-user-scenario/setup.sh
./example/multi-user-scenario/setup.sh
```

The script expects the server to be running on `http://localhost:8080`.

## API Endpoints Used

| Step | Method | Endpoint | Actor |
|------|--------|----------|-------|
| Register users | POST | `/api/v1/auth/register` | Public |
| Create company | POST | `/api/v1/companies` | Alice |
| Create organization | POST | `/api/v1/companies/{cid}/organizations` | Alice |
| Create project | POST | `/api/v1/organizations/{oid}/projects` | Alice |
| Add project member | POST | `/api/v1/projects/{id}/members` | Alice |
| Create workflow | POST | `/api/v1/projects/{pid}/workflows` | Alice |
| List workflows | GET | `/api/v1/projects/{pid}/workflows` | Bob |
| Get workflow | GET | `/api/v1/workflows/{id}` | Carol |
