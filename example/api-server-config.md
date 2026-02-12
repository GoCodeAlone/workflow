# API Server Architecture

This diagram visualizes the API server workflow with multiple endpoints.

## API Server Workflow

```mermaid
graph TD
    subgraph APIServerWorkflow["API Server Workflow"]
        HS["HTTP Server (:8080)"] --> AR["API Router"]
        AR --> UH["Users Handler"]
        AR --> PH["Products Handler"]
        AR --> HH["Health Handler"]
    end
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/users` | Retrieve users |
| `POST` | `/api/users` | Create user |
| `GET` | `/api/products` | Retrieve products |
| `POST` | `/api/products` | Create product |
| `GET` | `/health` | Health check |

## Request Flow

This shows how a request flows through the system.

```mermaid
graph LR
    CR["Client Request"] --> HS["HTTP Server (:8080)"]
    HS --> AR["API Router"]
    AR --> H["Handler"]
    H --> JR["JSON Response"]
```
