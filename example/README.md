# Workflow Engine Examples: Custom vs Modular Modules

This directory demonstrates the two approaches for building applications with the Workflow Engine:

## 🔧 **Custom Module Approach** (Simple & Fast)
Perfect for prototyping, learning, and simple applications.

**Examples:**
- `simple-workflow-config.yaml` - Basic HTTP server with routing
- `api-server-config.yaml` - REST API with custom handlers  
- `event-processor-config.yaml` - Simple event processing

**Benefits:**
- Easy YAML-based configuration
- Built-in workflow routing system
- Great for rapid prototyping
- Simple JSON response handlers

## 🚀 **Modular v1.3.9 Approach** (Production-Ready)
Enterprise-grade modules with advanced features.

**Examples:**
- `api-gateway-modular-config.yaml` - Production API gateway with reverse proxy
- `scheduled-jobs-modular-config.yaml` - Advanced job scheduling with cron

**Benefits:**
- Production-ready with TLS, metrics, circuit breakers
- Advanced features (load balancing, caching, pub/sub)
- Better performance and reliability
- Tenant-aware multi-tenancy support

## 🎯 **Hybrid Approach** (Best of Both)
Combine custom and Modular modules as needed.

**Examples:**
- `api-gateway-config.yaml` - Uses both custom middleware and reverse proxy

## 📊 **Feature Comparison**

| Feature | Custom Modules | Modular Modules |
|---------|----------------|-----------------|
| **Learning Curve** | ⭐⭐⭐⭐⭐ Easy | ⭐⭐⭐ Moderate |
| **Configuration** | YAML workflows | Module configs |
| **Performance** | ⭐⭐⭐ Good | ⭐⭐⭐⭐⭐ Excellent |
| **Features** | ⭐⭐ Basic | ⭐⭐⭐⭐⭐ Advanced |
| **Production Ready** | ⭐⭐ Limited | ⭐⭐⭐⭐⭐ Full |
| **Extensibility** | ⭐⭐⭐ Good | ⭐⭐⭐⭐⭐ Excellent |

## 🚀 **Getting Started**

### Test Custom Modules
```bash
go run main.go -config simple-workflow-config.yaml
curl http://localhost:8080/health
```

### Test Modular Modules  
```bash
go run main.go -config api-gateway-modular-config.yaml
# Full API gateway with reverse proxy running on port 8080
```

### Test Advanced Scheduling
```bash
go run main.go -config scheduled-jobs-modular-config.yaml
# Production scheduler with eventbus integration
```

## 📚 **Module Reference**

### Available Custom Module Types
- `http.server` - Basic HTTP server
- `http.router` - Request routing  
- `http.handler` - Simple JSON handlers
- `http.middleware.auth` - Basic authentication
- `http.middleware.logging` - Request logging
- `http.middleware.ratelimit` - Rate limiting
- `http.middleware.cors` - CORS headers
- `messaging.broker` - In-memory message broker
- `messaging.handler` - Message handlers

### Available Modular Module Types
- `reverseproxy` - Production reverse proxy with load balancing
- `httpserver.modular` - Enterprise HTTP server with TLS
- `scheduler.modular` - Advanced cron job scheduling
- `auth.modular` - JWT/OAuth authentication
- `eventbus.modular` - Pub/sub messaging system
- `cache.modular` - Multi-backend caching
- `chimux.router` - Chi router with middleware

Choose the approach that best fits your needs!

---

## Legacy Examples

The following examples demonstrate various workflow patterns:

### State Machine & Event Processing
- `state-machine-workflow.yaml` - E-commerce order processing states
- `event-driven-workflow.yaml` - Complex event pattern detection
- `event-processor-config.yaml` - Basic event processing

### Integration & APIs  
- `integration-workflow.yaml` - Third-party service integration
- `api-gateway-config.yaml` - API gateway with authentication
- `sms-chat-config.yaml` - SMS-based messaging workflow

### Scheduling & Jobs
- `advanced-scheduler-workflow.yaml` - Complex scheduling scenarios
- `scheduled-jobs-config.yaml` - Recurring task management
- `data-pipeline-config.yaml` - Data processing workflows

### Patterns & Examples
- `multi-workflow-config.yaml` - Multiple parallel workflows
- `dependency-injection-example.yaml` - Service injection patterns
- `trigger-workflow-example.yaml` - Event trigger demonstrations

### Running Legacy Examples

Option 1: Specify configuration file
```bash
go run main.go -config <configuration-file>.yaml
```

Option 2: Interactive selection menu
```bash
go run main.go
```

This displays a numbered list of available configurations.