# API Gateway Architecture

This diagram visualizes the API gateway workflow with proxy routing to backend services.

## API Gateway Workflow

```mermaid
graph TD
    subgraph APIGateway["API Gateway Workflow"]
        HS["HTTP Server (:8090)"] --> GR["Gateway Router"]

        subgraph Middleware["Middleware Stack"]
            CORS["CORS Middleware"] --> LOG["Logging Middleware"]
            LOG --> RL["Rate Limit Middleware"]
            RL --> AUTH["Auth Middleware"]
        end

        GR --> Middleware

        Middleware --> SP["Service Proxies"]
        Middleware --> DH["Direct Handlers"]

        SP --> US["Users Service"]
        SP --> PS["Products Service"]
        SP --> OS["Orders Service"]

        DH --> HC["Health Check"]
        DH --> ME["Metrics"]
    end
```

## Gateway Request Flow

This shows how a request flows through the gateway to backend services.

```mermaid
graph LR
    CR["Client Request"] --> GR["Gateway Router"]
    GR --> MS["Middleware Stack"]
    MS --> SP["Service Proxy"]
    SP --> BS["Backend Service"]
```

## Available Routes

### Public Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Metrics (auth protected) |

### Protected Service Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET/POST` | `/api/users` | User service |
| `GET/POST` | `/api/products` | Product service |
| `GET/POST` | `/api/orders` | Order service |
