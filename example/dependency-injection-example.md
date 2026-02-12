# Dependency Injection Architecture

This diagram visualizes the dependency relationships between services.

## Dependency Injection Example

```mermaid
graph TD
    subgraph Core["Core Services"]
        CP["Config Provider"]
        LS["Logger Service"]
        MS["Metrics Service"]
    end

    subgraph Data["Data Services"]
        CS["Cache Service"]
        DS["Database Service"]
    end

    subgraph Business["Business Services"]
        US["User Service"]
        PS["Product Service"]
        OS["Order Service"]
    end

    subgraph API["API Layer"]
        UHH["User HTTP Handler"]
        PHH["Product HTTP Handler"]
        OHH["Order HTTP Handler"]
    end

    subgraph Interface["External Interface Layer"]
        HTTP["HTTP Server (:8080)"]
        GRPC["gRPC Server (:9090)"]
        HR["HTTP Router"]
    end

    CP --> DS
    LS --> DS
    CP --> CS
    LS --> CS
    MS --> DS

    CS --> US
    DS --> US
    CS --> PS
    DS --> PS
    CS --> OS
    DS --> OS

    US --> UHH
    PS --> PHH
    OS --> OHH

    UHH --> HR
    PHH --> HR
    OHH --> HR

    HR --> HTTP
    HR --> GRPC
```

## Service Dependency Hierarchy

Shows the hierarchical organization of service dependencies.

```mermaid
graph TD
    CS["Core Services"] --> DS["Data Services"]
    DS --> BS["Business Services"]
    BS --> AL["API Layer"]
    AL --> IL["Interface Layer"]
```
