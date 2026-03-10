# Admin API Management - Complete Testing Guide

## Table of Contents

- [Overview](#overview)
- [Base Configuration](#base-configuration)
- [API Endpoints](#api-endpoints)
  - [1. Create API](#1-create-api)
  - [2. Update API](#2-update-api)
  - [3. Get API](#3-get-api)
  - [4. List APIs](#4-list-apis)
  - [5. Delete API](#5-delete-api)
  - [6. List Endpoints](#6-list-endpoints)
  - [7. Add Endpoint](#7-add-endpoint)
  - [8. Get Endpoint](#8-get-endpoint)
  - [9. Update Endpoint](#9-update-endpoint)
  - [10. Delete Endpoint](#10-delete-endpoint)
  - [11. Bulk Import](#11-bulk-import)
  - [12. Export All](#12-export-all)
  - [13. List Templates](#13-list-templates)
  - [14. Create from Template](#14-create-from-template)
  - [15. Test Rule](#15-test-rule)
  - [16. Resolve Path](#16-resolve-path)
  - [17. Reload Rules](#17-reload-rules)
  - [18. Subscribe to Events](#18-subscribe-to-events)
- [Metrics API](#metrics-api)
- [Configuration Reference](#configuration-reference)
- [Best Practices](#best-practices)

---

## Overview

This guide provides comprehensive documentation for testing the Admin API Management endpoints. The API allows you to dynamically register, configure, and manage APIs with rate limiting, routing, and reverse proxy capabilities.

**Key Features:**

- Reverse proxy with upstream URL configuration
- Sliding window rate limiting (always enabled)
- Bearer token authentication for admin endpoints
- Bot detection and IP blocking
- Real-time metrics and event streaming
- Strict HTTP method validation
- No path stripping (full paths forwarded to upstream)

---

## Base Configuration

### Base URL

<http://localhost:8080>

### Common Headers

```http
Content-Type: application/json
Authorization: Bearer YOUR_ADMIN_TOKEN
```

### Response Codes

| Code | Description |
| ------ | ------------- |
| `200 OK` | Successful GET/PUT request |
| `201 Created` | Successful POST request |
| `204 No Content` | Successful DELETE request |
| `400 Bad Request` | Invalid request body or parameters |
| `401 Unauthorized` | Missing or invalid admin token |
| `404 Not Found` | Resource not found |
| `405 Method Not Allowed` | HTTP method doesn't match endpoint |
| `409 Conflict` | Resource already exists |
| `500 Internal Server Error` | Server error |

## API Endpoints

### 1. Create API

**Endpoint:** `POST /admin/apis`

Creates a new API registration with routing, rate limiting, and upstream configuration.

#### Request Body

```json
{
  "id": "user-service",
  "service_id": "user-service-v1",
  "name": "User Service API",
  "description": "Handles user operations",
  "upstream_url": "http://user-backend:8080",
  "status": "active",
  "default_limits": {
    "requests_per_second": 100,
    "burst_size": 200,
    "block_duration": 300000000000
  },
  "endpoints": [
    {
      "id": "get-users",
      "path": "/api/users",
      "method": "GET",
      "limits": {
        "requests_per_second": 50,
        "burst_size": 100,
        "block_duration": 300000000000
      },
      "priority": 10
    },
    {
      "id": "create-user",
      "path": "/api/users",
      "method": "POST",
      "limits": {
        "requests_per_second": 20,
        "burst_size": 40,
        "block_duration": 600000000000
      },
      "priority": 20
    },
    {
      "id": "get-user",
      "path": "/api/users/{id}",
      "method": "GET",
      "limits": {
        "requests_per_second": 60,
        "burst_size": 120
      },
      "priority": 15
    }
  ]
}
```

**Field Details:**

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `id` | string | Yes | - | Unique API identifier |
| `service_id` | string | Yes | - | Service grouping identifier |
| `name` | string | No | - | Human-readable name |
| `description` | string | No | - | API description |
| `upstream_url` | string | Yes | - | Backend server URL |
| `status` | string | No | "active" | active, maintenance, deprecated, disabled |
| `default_limits` | object | No | - | Default rate limits for all endpoints |
| `endpoints` | array | Yes | - | Endpoint definitions (at least one required) |

**Rate Limits Object:**

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `requests_per_second` | number | Yes | - | Maximum requests per second |
| `burst_size` | number | Yes | - | Burst capacity |
| `block_duration` | number (nanoseconds) | No | 300000000000 | Block duration (default: 5 minutes) |
| `use_sliding_window` | boolean | Auto | true | Always true (enforced by system) |

**Endpoint Object:**

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `id` | string | Yes | - | Unique endpoint identifier within API |
| `path` | string | Yes | - | Full URL path (e.g., `/api/users`) |
| `method` | string | Yes | - | HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS) |
| `limits` | object | No | API defaults | Endpoint-specific rate limits |
| `priority` | number | No | 100 | Matching priority (lower = higher priority) |
| `enabled` | boolean | Auto | true | Always true (enforced by system) |

**Important Notes:**

- ✅ **No `base_path` required** - paths are used as-is
- ✅ **Full paths required** - use `/api/users`, not `/users`
- ✅ **Methods validated** - must be valid HTTP methods, auto-uppercase
- ✅ **At least one endpoint required** - empty endpoints array rejected
- ❌ **No `auth_type` field** - authentication handled at middleware layer, not per-endpoint

#### Response (201 Created)

```json
{
  "id": "user-service",
  "service_id": "user-service-v1",
  "name": "User Service API",
  "description": "Handles user operations",
  "upstream_url": "http://user-backend:8080",
  "status": "active",
  "default_limits": {
    "requests_per_second": 100,
    "burst_size": 200,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "endpoints": [
    {
      "id": "get-users",
      "path": "/api/users",
      "method": "GET",
      "limits": {
        "requests_per_second": 50,
        "burst_size": 100,
        "block_duration": 300000000000,
        "use_sliding_window": true
      },
      "priority": 10,
      "enabled": true
    },
    {
      "id": "create-user",
      "path": "/api/users",
      "method": "POST",
      "limits": {
        "requests_per_second": 20,
        "burst_size": 40,
        "block_duration": 600000000000,
        "use_sliding_window": true
      },
      "priority": 20,
      "enabled": true
    }
  ],
  "created_at": "2026-03-10T10:00:00.000000000+05:00",
  "updated_at": "2026-03-10T10:00:00.000000000+05:00"
}
```

#### cURL Examples

**Create Full API:**

```bash
curl -X POST http://localhost:8080/admin/apis \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "id": "order-service",
    "service_id": "commerce-v1",
    "name": "Order Service",
    "description": "Handles order processing",
    "upstream_url": "http://order-backend:8080",
    "status": "active",
    "default_limits": {
      "requests_per_second": 100,
      "burst_size": 200,
      "block_duration": 300000000000
    },
    "endpoints": [
      {
        "id": "list-orders",
        "path": "/api/orders",
        "method": "GET",
        "limits": {
          "requests_per_second": 50,
          "burst_size": 100
        },
        "priority": 10
      },
      {
        "id": "create-order",
        "path": "/api/orders",
        "method": "POST",
        "limits": {
          "requests_per_second": 20,
          "burst_size": 40,
          "block_duration": 600000000000
        },
        "priority": 20
      }
    ]
  }'
```

**Create Minimal API (Required Fields Only):**

```bash
curl -X POST http://localhost:8080/admin/apis \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "id": "minimal-api",
    "service_id": "minimal-service",
    "upstream_url": "http://minimal-backend:8080",
    "endpoints": [
      {
        "id": "health",
        "path": "/health",
        "method": "GET"
      }
    ]
  }'
```

---

### 2. Update API

**Endpoint:** `PUT /admin/apis/{apiID}`

Updates an existing API. Supports partial updates - only send fields you want to change.

#### Request Body - Full Update

```json
{
  "name": "Updated Order Service",
  "description": "Updated description for order processing",
  "upstream_url": "http://new-order-backend:8080",
  "status": "active",
  "default_limits": {
    "requests_per_second": 150,
    "burst_size": 300,
    "block_duration": 300000000000
  }
}
```

#### Request Body - Partial Updates

**Update Name Only:**

```json
{
  "name": "Updated API Name"
}
```

**Update Rate Limits Only:**

```json
{
  "default_limits": {
    "requests_per_second": 200,
    "burst_size": 400,
    "block_duration": 300000000000
  }
}
```

**Update Status to Maintenance:**

```json
{
  "status": "maintenance"
}
```

**Update Upstream URL:**

```json
{
  "upstream_url": "http://new-backend:9000"
}
```

#### Response (200 OK)

Returns the complete updated API object.

```json
{
  "id": "order-service",
  "service_id": "commerce-v1",
  "name": "Updated Order Service",
  "description": "Updated description for order processing",
  "upstream_url": "http://new-order-backend:8080",
  "status": "active",
  "default_limits": {
    "requests_per_second": 150,
    "burst_size": 300,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "endpoints": [...],
  "created_at": "2026-03-10T10:00:00.000000000+05:00",
  "updated_at": "2026-03-10T10:30:00.000000000+05:00"
}
```

#### cURL Examples

```bash
# Full update
curl -X PUT http://localhost:8080/admin/apis/order-service \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "name": "Updated Order Service",
    "upstream_url": "http://new-backend:9000",
    "default_limits": {
      "requests_per_second": 150,
      "burst_size": 300
    }
  }'

# Partial update - status only
curl -X PUT http://localhost:8080/admin/apis/order-service \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{"status": "maintenance"}'
```

---

### 3. Get API

**Endpoint:** `GET /admin/apis/{apiID}`

Retrieves a single API by ID.

#### Response (200 OK)

```json
{
  "id": "order-service",
  "service_id": "commerce-v1",
  "name": "Order Service",
  "description": "Handles order processing",
  "upstream_url": "http://order-backend:8080",
  "status": "active",
  "default_limits": {
    "requests_per_second": 100,
    "burst_size": 200,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "endpoints": [
    {
      "id": "list-orders",
      "path": "/api/orders",
      "method": "GET",
      "limits": {
        "requests_per_second": 50,
        "burst_size": 100,
        "block_duration": 300000000000,
        "use_sliding_window": true
      },
      "priority": 10,
      "enabled": true
    }
  ],
  "created_at": "2026-03-10T10:00:00.000000000+05:00",
  "updated_at": "2026-03-10T10:00:00.000000000+05:00"
}
```

#### cURL Example

```bash
curl http://localhost:8080/admin/apis/order-service \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 4. List APIs

**Endpoint:** `GET /admin/apis`

Lists all registered APIs with optional filtering and pagination.

#### Query Parameters

| Parameter | Type | Description | Example |
| ----------- | ------ | ------------- | --------- |
| `service_id` | string | Filter by service ID | `commerce-v1` |
| `status` | string | Filter by status | `active`, `maintenance` |
| `search` | string | Search in name/description | `order` |
| `limit` | integer | Limit results | `10` |
| `offset` | integer | Pagination offset | `0` |

#### Response (200 OK)

```json
{
  "apis": [
    {
      "id": "order-service",
      "service_id": "commerce-v1",
      "name": "Order Service",
      "description": "Handles order processing",
      "upstream_url": "http://order-backend:8080",
      "status": "active",
      "created_at": "2026-03-10T10:00:00.000000000+05:00",
      "updated_at": "2026-03-10T10:00:00.000000000+05:00"
    },
    {
      "id": "user-service",
      "service_id": "user-service-v1",
      "name": "User Service API",
      "upstream_url": "http://user-backend:8080",
      "status": "active",
      "created_at": "2026-03-10T10:30:00.000000000+05:00",
      "updated_at": "2026-03-10T10:30:00.000000000+05:00"
    }
  ],
  "count": 2
}
```

#### cURL Examples

```bash
# List all APIs
curl http://localhost:8080/admin/apis \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Filter by status
curl "http://localhost:8080/admin/apis?status=active" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Filter by service_id with pagination
curl "http://localhost:8080/admin/apis?service_id=commerce-v1&limit=10&offset=0" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 5. Delete API

**Endpoint:** `DELETE /admin/apis/{apiID}`

Deletes an API and all its endpoints.

#### Response

`204 No Content` - No response body

#### cURL Example

```bash
curl -X DELETE http://localhost:8080/admin/apis/order-service \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 6. List Endpoints

**Endpoint:** `GET /admin/apis/{apiID}/endpoints`

Lists all endpoints for a specific API.

#### Response (200 OK)

```json
{
  "endpoints": [
    {
      "id": "list-orders",
      "path": "/api/orders",
      "method": "GET",
      "limits": {
        "requests_per_second": 50,
        "burst_size": 100,
        "block_duration": 300000000000,
        "use_sliding_window": true
      },
      "priority": 10,
      "enabled": true
    },
    {
      "id": "create-order",
      "path": "/api/orders",
      "method": "POST",
      "limits": {
        "requests_per_second": 20,
        "burst_size": 40,
        "block_duration": 600000000000,
        "use_sliding_window": true
      },
      "priority": 20,
      "enabled": true
    }
  ],
  "count": 2
}
```

#### cURL Example

```bash
curl http://localhost:8080/admin/apis/order-service/endpoints \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 7. Add Endpoint

**Endpoint:** `POST /admin/apis/{apiID}/endpoints`

Adds a new endpoint to an existing API.

#### Request Body

```json
{
  "id": "update-order",
  "path": "/api/orders/{id}",
  "method": "PUT",
  "limits": {
    "requests_per_second": 15,
    "burst_size": 30,
    "block_duration": 300000000000
  },
  "priority": 25
}
```

**Field Details:**

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `id` | string | Yes | - | Unique endpoint identifier within API |
| `path` | string | Yes | - | Full URL path (e.g., `/api/orders/{id}`) |
| `method` | string | Yes | - | HTTP method (auto-uppercase) |
| `limits` | object | No | API defaults | Endpoint-specific rate limits |
| `priority` | number | No | 100 | Matching priority |

#### Response (201 Created)

```json
{
  "id": "update-order",
  "path": "/api/orders/{id}",
  "method": "PUT",
  "limits": {
    "requests_per_second": 15,
    "burst_size": 30,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "priority": 25,
  "enabled": true
}
```

#### cURL Example

```bash
curl -X POST http://localhost:8080/admin/apis/order-service/endpoints \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "id": "update-order",
    "path": "/api/orders/{id}",
    "method": "PUT",
    "limits": {
      "requests_per_second": 15,
      "burst_size": 30
    },
    "priority": 25
  }'
```

---

### 8. Get Endpoint

**Endpoint:** `GET /admin/apis/{apiID}/endpoints/{endpointID}`

Retrieves a specific endpoint.

#### Response (200 OK)

```json
{
  "id": "list-orders",
  "path": "/api/orders",
  "method": "GET",
  "limits": {
    "requests_per_second": 50,
    "burst_size": 100,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "priority": 10,
  "enabled": true
}
```

#### cURL Example

```bash
curl http://localhost:8080/admin/apis/order-service/endpoints/list-orders \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 9. Update Endpoint

**Endpoint:** `PUT /admin/apis/{apiID}/endpoints/{endpointID}`

Updates an existing endpoint. Supports partial updates.

#### Request Body - Full Update

```json
{
  "path": "/api/orders/v2/{id}",
  "method": "PATCH",
  "limits": {
    "requests_per_second": 25,
    "burst_size": 50,
    "block_duration": 300000000000
  },
  "priority": 15
}
```

#### Request Body - Partial Update

**Update Limits Only:**

```json
{
  "limits": {
    "requests_per_second": 100,
    "burst_size": 200
  }
}
```

**Update Priority Only:**

```json
{
  "priority": 5
}
```

#### Response (200 OK)

```json
{
  "id": "list-orders",
  "path": "/api/orders",
  "method": "GET",
  "limits": {
    "requests_per_second": 100,
    "burst_size": 200,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "priority": 5,
  "enabled": true
}
```

#### cURL Examples

```bash
# Update limits
curl -X PUT http://localhost:8080/admin/apis/order-service/endpoints/list-orders \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "limits": {
      "requests_per_second": 100,
      "burst_size": 200
    }
  }'

# Update priority
curl -X PUT http://localhost:8080/admin/apis/order-service/endpoints/list-orders \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{"priority": 5}'
```

---

### 10. Delete Endpoint

**Endpoint:** `DELETE /admin/apis/{apiID}/endpoints/{endpointID}`

Deletes a specific endpoint.

#### Response

`204 No Content` - No response body

#### cURL Example

```bash
curl -X DELETE http://localhost:8080/admin/apis/order-service/endpoints/update-order \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 11. Bulk Import

**Endpoint:** `POST /admin/apis/import`

Imports multiple APIs at once.

#### Request Body

```json
{
  "apis": [
    {
      "id": "bulk-api-1",
      "service_id": "bulk-service",
      "name": "Bulk API 1",
      "upstream_url": "http://localhost:8082",
      "default_limits": {
        "requests_per_second": 50,
        "burst_size": 100,
        "block_duration": 300000000000
      },
      "endpoints": [
        {
          "id": "get-data",
          "path": "/api/data",
          "method": "GET",
          "limits": {
            "requests_per_second": 50,
            "burst_size": 100
          },
          "priority": 10
        }
      ]
    },
    {
      "id": "bulk-api-2",
      "service_id": "bulk-service",
      "name": "Bulk API 2",
      "upstream_url": "http://localhost:8083",
      "default_limits": {
        "requests_per_second": 30,
        "burst_size": 60,
        "block_duration": 300000000000
      },
      "endpoints": [
        {
          "id": "health",
          "path": "/health",
          "method": "GET",
          "priority": 1
        }
      ]
    }
  ],
  "overwrite_existing": false
}
```

#### Response (200 OK)

```json
{
  "imported": 2,
  "updated": 0,
  "failed": 0,
  "errors": []
}
```

**With Errors:**

```json
{
  "imported": 1,
  "updated": 0,
  "failed": 1,
  "errors": [
    "Failed to register bulk-api-2: API with ID bulk-api-2 already exists"
  ]
}
```

#### cURL Example

```bash
curl -X POST http://localhost:8080/admin/apis/import \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "apis": [
      {
        "id": "imported-api",
        "service_id": "imported-service",
        "name": "Imported API",
        "upstream_url": "http://imported-backend:8080",
        "default_limits": {
          "requests_per_second": 100,
          "burst_size": 200
        },
        "endpoints": [
          {
            "id": "health",
            "path": "/health",
            "method": "GET",
            "priority": 1
          }
        ]
      }
    ],
    "overwrite_existing": false
  }'
```

---

### 12. Export All

**Endpoint:** `GET /admin/apis/export`

Exports all APIs in JSON format for backup or migration.

#### Response (200 OK)

```json
{
  "exported_at": "2026-03-10T10:30:00Z",
  "count": 2,
  "apis": [
    {
      "id": "order-service",
      "service_id": "commerce-v1",
      "name": "Order Service",
      "upstream_url": "http://order-backend:8080",
      "status": "active",
      "default_limits": {
        "requests_per_second": 100,
        "burst_size": 200,
        "block_duration": 300000000000,
        "use_sliding_window": true
      },
      "endpoints": [
        {
          "id": "list-orders",
          "path": "/api/orders",
          "method": "GET",
          "limits": {
            "requests_per_second": 50,
            "burst_size": 100,
            "block_duration": 300000000000,
            "use_sliding_window": true
          },
          "priority": 10,
          "enabled": true
        }
      ],
      "created_at": "2026-03-10T10:00:00.000000000+05:00",
      "updated_at": "2026-03-10T10:00:00.000000000+05:00"
    }
  ]
}
```

#### cURL Example

```bash
# Export to file
curl http://localhost:8080/admin/apis/export \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  > apis-backup-$(date +%Y%m%d).json
```

---

### 13. List Templates

**Endpoint:** `GET /admin/apis/templates`

Lists available service templates with predefined rate limits.

#### Response (200 OK)

```json
{
  "templates": {
    "standard": {
      "requests_per_second": 100,
      "burst_size": 200,
      "window_size": 60000000000,
      "block_duration": 300000000000
    },
    "premium": {
      "requests_per_second": 500,
      "burst_size": 1000,
      "window_size": 60000000000,
      "block_duration": 300000000000
    },
    "public": {
      "requests_per_second": 1000,
      "burst_size": 2000,
      "window_size": 10000000000,
      "block_duration": 60000000000
    },
    "secure": {
      "requests_per_second": 10,
      "burst_size": 15,
      "window_size": 60000000000,
      "block_duration": 900000000000
    }
  },
  "count": 4
}
```

#### cURL Example

```bash
curl http://localhost:8080/admin/apis/templates \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 14. Create from Template

**Endpoint:** `POST /admin/apis/from-template`

Creates an API from a predefined template with automatic rate limit defaults.

#### Request Body

```json
{
  "api_id": "premium-api",
  "service_id": "premium-service",
  "name": "Premium API from Template",
  "upstream_url": "http://premium-backend:8080",
  "template": "premium",
  "endpoints": [
    {
      "id": "get-data",
      "path": "/api/data",
      "method": "GET",
      "priority": 10
    },
    {
      "id": "post-data",
      "path": "/api/data",
      "method": "POST",
      "priority": 20
    }
  ]
}
```

**Template Values Applied:**

- `standard`: 100 RPS, 200 burst, 5min block
- `premium`: 500 RPS, 1000 burst, 5min block
- `public`: 1000 RPS, 2000 burst, 1min block
- `secure`: 10 RPS, 15 burst, 15min block

#### Response (201 Created)

```json
{
  "id": "premium-api",
  "service_id": "premium-service",
  "name": "Premium API from Template",
  "upstream_url": "http://premium-backend:8080",
  "status": "active",
  "default_limits": {
    "requests_per_second": 500,
    "burst_size": 1000,
    "window_size": 60000000000,
    "block_duration": 300000000000,
    "use_sliding_window": true
  },
  "endpoints": [
    {
      "id": "get-data",
      "path": "/api/data",
      "method": "GET",
      "limits": {
        "requests_per_second": 500,
        "burst_size": 1000,
        "window_size": 60000000000,
        "block_duration": 300000000000,
        "use_sliding_window": true
      },
      "priority": 10,
      "enabled": true
    }
  ],
  "created_at": "2026-03-10T10:00:00.000000000+05:00",
  "updated_at": "2026-03-10T10:00:00.000000000+05:00"
}
```

#### cURL Example

```bash
curl -X POST http://localhost:8080/admin/apis/from-template \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "api_id": "my-premium-api",
    "service_id": "my-service",
    "name": "My Premium API",
    "upstream_url": "http://backend:8080",
    "template": "premium",
    "endpoints": [
      {
        "id": "health",
        "path": "/health",
        "method": "GET",
        "priority": 1
      }
    ]
  }'
```

---

### 15. Test Rule

**Endpoint:** `POST /admin/apis/test`

Tests a rate limit rule against a path without applying it.

#### Request Body

```json
{
  "path": "/api/orders/orders",
  "method": "GET",
  "rule": {
    "requests_per_second": 100,
    "burst_size": 200,
    "block_duration": 300000000000
  }
}
```

#### Response (200 OK)

```json
{
  "path": "/api/orders/orders",
  "method": "GET",
  "matched_rule": {
    "api_id": "order-service",
    "endpoint_id": "list-orders",
    "service_id": "commerce-v1",
    "path": "/api/orders/orders",
    "method": "GET",
    "requests_per_second": 50,
    "burst_size": 100,
    "window_size": 60000000000,
    "block_duration": 300000000000,
    "use_sliding_window": true,
    "priority": 10,
    "enabled": true,
    "upstream_url": "http://order-backend:8080",
    "is_excluded": false
  },
  "would_apply": true,
  "test_limits": {
    "requests_per_second": 100,
    "burst_size": 200,
    "block_duration": 300000000000
  }
}
```

#### cURL Example

```bash
curl -X POST http://localhost:8080/admin/apis/test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -d '{
    "path": "/api/orders/orders",
    "method": "GET",
    "rule": {
      "requests_per_second": 100,
      "burst_size": 200
    }
  }'
```

---

### 16. Resolve Path

**Endpoint:** `GET /admin/apis/resolve`

Shows which rule would apply to a specific path.

#### Query Parameters

| Parameter | Type | Required | Description |
| ------- | ------ | ---------- | ------------- |
| `path` | string | Yes | Path to resolve |

#### Response (200 OK)

```json
{
  "path": "/api/orders/orders",
  "resolved_rule": {
    "api_id": "order-service",
    "endpoint_id": "list-orders",
    "service_id": "commerce-v1",
    "path": "/api/orders/orders",
    "method": "GET",
    "requests_per_second": 50,
    "burst_size": 100,
    "window_size": 60000000000,
    "block_duration": 300000000000,
    "use_sliding_window": true,
    "priority": 10,
    "enabled": true,
    "upstream_url": "http://order-backend:8080"
  },
  "excluded": false
}
```

#### cURL Example

```bash
curl "http://localhost:8080/admin/apis/resolve?path=/api/orders/orders" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 17. Reload Rules

**Endpoint:** `POST /admin/apis/reload`

Triggers a reload of all rate limiting rules from the registry file.

#### Response (200 OK)

```json
{
  "status": "reloaded",
  "message": "Rules reloaded successfully"
}
```

#### cURL Example

```bash
curl -X POST http://localhost:8080/admin/apis/reload \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

### 18. Subscribe to Events

**Endpoint:** `GET /admin/apis/events`

Subscribes to Server-Sent Events (SSE) for real-time API changes.

#### Response (Server-Sent Events Stream)

```text
data: {"type": "connected", "timestamp": "2026-03-10T10:00:00Z"}

data: {"type": "api_created", "api_id": "new-api", "timestamp": "2026-03-10T10:05:00Z", "data": {"id": "new-api", ...}}

data: {"type": "api_updated", "api_id": "order-service", "timestamp": "2026-03-10T10:10:00Z"}

data: {"type": "api_deleted", "api_id": "old-api", "timestamp": "2026-03-10T10:15:00Z"}
```

#### cURL Example

```bash
# Stream events (requires -N for no buffering)
curl -N http://localhost:8080/admin/apis/events \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

---

## Metrics API

The Metrics API provides real-time and historical metrics for monitoring API usage and performance.

### Base URL

```code
http://localhost:8080/admin/metrics
```

### Endpoints

#### 1. List All APIs with Metrics

**Endpoint:** `GET /admin/metrics`

Returns all APIs with their current metrics summary.

#### Response (200 OK)

```json
{
  "apis": [
    {
      "id": "order-service",
      "total_requests": 1500,
      "allowed_requests": 1450,
      "blocked_requests": 50,
      "rate_limited_requests": 30,
      "avg_response_time_ms": 45.5,
      "status_codes": {
        "200": 1400,
        "201": 50,
        "429": 30,
        "500": 20
      }
    }
  ],
  "count": 1,
  "generated_at": "2026-03-10T10:00:00Z"
}
```

#### cURL Example

```bash
curl http://localhost:8080/admin/metrics \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

#### 2. Get API Metrics (Time Range)

**Endpoint:** `GET /admin/metrics/{apiID}`

Query parameters for time range:

- `from` - Start time (RFC3339)
- `to` - End time (RFC3339)
- `range` - Relative range: `1h`, `6h`, `12h`, `24h`, `3d`, `7d`, `30d`, `90d`, `1y`

#### Predefined Time Ranges

| Endpoint | Description |
| ---------- | ------------- |
| `GET /admin/metrics/{apiID}/current` | Current hour |
| `GET /admin/metrics/{apiID}/today` | Today (00:00 to now) |
| `GET /admin/metrics/{apiID}/yesterday` | Yesterday full day |
| `GET /admin/metrics/{apiID}/week` | Last 7 days |
| `GET /admin/metrics/{apiID}/month` | Last 30 days |
| `GET /admin/metrics/{apiID}/year` | Last 365 days |

#### Custom Range

**Endpoint:** `GET /admin/metrics/{apiID}/range`

Query parameters:

- `days` - Number of days back
- `hours` - Number of hours back

#### Response (200 OK)

```json
{
  "api_id": "order-service",
  "query_range": "last_7_days",
  "from": "2026-03-03T10:00:00Z",
  "to": "2026-03-10T10:00:00Z",
  "total_requests": 10500,
  "allowed_requests": 10000,
  "blocked_requests": 300,
  "rate_limited_requests": 200,
  "avg_response_time_ms": 42.3,
  "requests_per_hour": [
    {"hour": "2026-03-03T10:00:00Z", "count": 150},
    {"hour": "2026-03-03T11:00:00Z", "count": 180}
  ],
  "top_ips": [
    {"ip": "192.168.1.1", "requests": 500},
    {"ip": "192.168.1.2", "requests": 300}
  ],
  "status_codes": {
    "200": 9500,
    "201": 500,
    "429": 200,
    "500": 100
  },
  "endpoints": [
    {
      "endpoint_id": "list-orders",
      "path": "/api/orders",
      "method": "GET",
      "requests": 8000,
      "avg_response_time_ms": 35.0
    }
  ]
}
```

#### cURL Examples

```bash
# Last 24 hours (default)
curl http://localhost:8080/admin/metrics/order-service \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Today only
curl http://localhost:8080/admin/metrics/order-service/today \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Last 7 days
curl http://localhost:8080/admin/metrics/order-service/week \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Custom range - last 3 days
curl http://localhost:8080/admin/metrics/order-service/range?days=3 \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Custom range - last 6 hours
curl http://localhost:8080/admin/metrics/order-service/range?hours=6 \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"

# Specific date range
curl "http://localhost:8080/admin/metrics/order-service?from=2026-03-01T00:00:00Z&to=2026-03-10T00:00:00Z" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

#### 3. Stream Real-Time Metrics

**Endpoint:** `GET /admin/metrics/{apiID}/stream`

Server-Sent Events stream with real-time metrics updates every 5 seconds.

#### Response (SSE Stream)

```text
data: {"type": "connected", "timestamp": "2026-03-10T10:00:00Z", "metrics": {...}}

data: {"type": "update", "timestamp": "2026-03-10T10:00:05Z", "metrics": {...}}
```

#### cURL Example

```bash
curl -N http://localhost:8080/admin/metrics/order-service/stream \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

#### 4. Export Metrics

**Endpoint:** `GET /admin/metrics/{apiID}/export`

Exports raw metrics data as JSON file.

Query parameters:

- `format` - Export format (default: `json`)

#### Response

File download with `Content-Disposition: attachment`

#### cURL Example

```bash
curl http://localhost:8080/admin/metrics/order-service/export \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  > order-service-metrics.json
```

---

## Configuration Reference

### API Object Structure

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `id` | string | Yes | - | Unique API identifier |
| `service_id` | string | Yes | - | Service grouping identifier |
| `name` | string | No | - | Human-readable name |
| `description` | string | No | - | Detailed description |
| `upstream_url` | string | Yes | - | Backend server URL |
| `status` | string | No | "active" | active, maintenance, deprecated, disabled |
| `default_limits` | object | No | `{}` | Default rate limits |
| `endpoints` | array | Yes | - | Endpoint definitions (at least one) |
| `created_at` | string | Auto | current time | ISO 8601 timestamp |
| `updated_at` | string | Auto | current time | ISO 8601 timestamp |

### Rate Limits Object

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `requests_per_second` | number | Yes | - | Maximum requests per second |
| `burst_size` | number | Yes | - | Maximum burst size |
| `block_duration` | number (nanoseconds) | No | 300000000000 | Block duration (5 minutes) |
| `use_sliding_window` | boolean | Auto | `true` | Always true (enforced) |

### Endpoint Object Structure

| Field | Type | Required | Default | Description |
| ------- | ------ | ---------- | --------- | ------------- |
| `id` | string | Yes | - | Unique endpoint identifier |
| `path` | string | Yes | - | Full URL path (e.g., `/api/users`) |
| `method` | string | Yes | - | HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS) |
| `limits` | object | No | API defaults | Endpoint-specific rate limits |
| `priority` | number | No | 100 | Matching priority (lower = higher) |
| `enabled` | boolean | Auto | `true` | Always true (enforced) |

### Status Values

| Status | Description |
| -------- | ------------- |
| `active` | Operational and accepting requests |
| `maintenance` | Maintenance mode, requests may be rejected |
| `deprecated` | Deprecated but still operational |
| `disabled` | Disabled, not accepting requests |

### Time Values (Nanoseconds)

| Duration | Nanoseconds | Use Case |
| ---------- | ------------- | ---------- |
| 1 minute | 60000000000 | Short blocks |
| 5 minutes | 300000000000 | Standard block duration (default) |
| 10 minutes | 600000000000 | Medium blocks |
| 15 minutes | 900000000000 | Secure API blocks |
| 30 minutes | 1800000000000 | Long blocks |

### Compiled Rule Object

Returned by Test Rule and Resolve Path endpoints:

| Field | Type | Description |
| ------- | ------ | ------------- |
| `api_id` | string | Parent API identifier |
| `endpoint_id` | string | Matched endpoint identifier |
| `service_id` | string | Service grouping |
| `path` | string | Full request path |
| `method` | string | HTTP method |
| `requests_per_second` | number | Applied RPS limit |
| `burst_size` | number | Applied burst limit |
| `window_size` | number | Time window in nanoseconds |
| `block_duration` | number | Block duration in nanoseconds |
| `use_sliding_window` | boolean | Always true |
| `priority` | number | Endpoint priority |
| `enabled` | boolean | Always true |
| `upstream_url` | string | Backend URL |
| `is_excluded` | boolean | Whether path is excluded |

---

## Reverse Proxy Behavior

### Key Characteristics

| Feature | Behavior |
| --------- | ---------- |
| **Path Forwarding** | Full paths forwarded as-is (no stripping) |
| **Method Validation** | Strict matching required (405 if mismatch) |
| **Authentication** | Handled by middleware layer, not per-endpoint |
| **Rate Limiting** | Applied before proxying |
| **Request Body** | Preserved and forwarded correctly |

### Request Flow

1. **Request arrives**: `POST http://localhost:8080/api/users`
2. **Registry lookup**: Finds endpoint with path `/api/users` and method `POST`
3. **Method validation**: Checks if `POST == POST` ✓
4. **Rate limiting**: Checks limits for the endpoint
5. **Proxy request**: Forwards to `http://backend:8080/api/users` (unchanged path)

### Method Mismatch Example

```
Request:  GET /api/users
Endpoint: POST /api/users
Result:   405 Method Not Allowed
Error:    {"error": "method_not_allowed", "message": "Method GET not allowed for this endpoint. Expected: POST"}
```

---

## Best Practices

### Rate Limit Configuration

**Public/High-Traffic APIs:**

```json
{
  "requests_per_second": 1000,
  "burst_size": 2000,
  "block_duration": 60000000000
}
```

**Standard APIs:**

```json
{
  "requests_per_second": 100,
  "burst_size": 200,
  "block_duration": 300000000000
}
```

**Secure/Sensitive APIs:**

```json
{
  "requests_per_second": 10,
  "burst_size": 15,
  "block_duration": 900000000000
}
```

### Endpoint Priority Guidelines

| Priority | Use Case | Example |
| ---------- | ---------- | --------- |
| 1-5 | Health checks, critical endpoints | `/health`, `/ready` |
| 10-20 | Standard read operations | `GET /api/users` |
| 20-30 | Standard write operations | `POST /api/users` |
| 30-50 | Destructive operations | `DELETE /api/users/{id}` |
| 50-100 | Administrative operations | `POST /admin/reset` |

### Path Guidelines

✅ **DO:**

- Use full paths: `/api/users`, `/api/orders/{id}`
- Include version in path: `/api/v1/users`
- Use consistent naming conventions

❌ **DON'T:**

- Use relative paths: `/users` (unless backend expects this)
- Omit leading slash: `api/users`
- Use `base_path` field (deprecated, ignored by proxy)

### Complete Workflow Example

```bash
# 1. Create API with multiple endpoints
curl -X POST http://localhost:8080/admin/apis \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer admin-token" \
  -d '{
    "id": "payment-service",
    "service_id": "payments-v1",
    "name": "Payment Service",
    "upstream_url": "http://payment-backend:8080",
    "default_limits": {
      "requests_per_second": 50,
      "burst_size": 100,
      "block_duration": 300000000000
    },
    "endpoints": [
      {
        "id": "health",
        "path": "/health",
        "method": "GET",
        "limits": {"requests_per_second": 1000, "burst_size": 2000},
        "priority": 1
      },
      {
        "id": "list-payments",
        "path": "/api/v1/payments",
        "method": "GET",
        "limits": {"requests_per_second": 50, "burst_size": 100},
        "priority": 10
      },
      {
        "id": "create-payment",
        "path": "/api/v1/payments",
        "method": "POST",
        "limits": {"requests_per_second": 20, "burst_size": 40, "block_duration": 600000000000},
        "priority": 20
      }
    ]
  }'

# 2. Verify creation
curl http://localhost:8080/admin/apis/payment-service \
  -H "Authorization: Bearer admin-token"

# 3. Test routing
curl "http://localhost:8080/admin/apis/resolve?path=/api/v1/payments" \
  -H "Authorization: Bearer admin-token"

# 4. Test the proxy
curl http://localhost:8080/api/v1/payments \
  -H "Authorization: Bearer user-token"

# 5. Monitor metrics
curl http://localhost:8080/admin/metrics/payment-service \
  -H "Authorization: Bearer admin-token"

# 6. Stream real-time events
curl -N http://localhost:8080/admin/apis/events \
  -H "Authorization: Bearer admin-token"

# 7. Export for backup
curl http://localhost:8080/admin/apis/export \
  -H "Authorization: Bearer admin-token" > backup.json
```

### Error Handling

**400 Bad Request - Invalid JSON:**

```json
{"error": "invalid request body"}
```

**400 Bad Request - Missing Required Fields:**

```json
{"error": "id, service_id, and upstream_url are required"}
```

**400 Bad Request - Empty Endpoints:**

```json
{"error": "at least one endpoint is required"}
```

**400 Bad Request - Invalid Method:**

```json
{"error": "invalid HTTP method: INVALID"}
```

**401 Unauthorized:**

```json
{"error": "unauthorized"}
```

**404 Not Found - API:**

```json
{"error": "API not found"}
```

**404 Not Found - Endpoint:**

```json
{"error": "Endpoint not found"}
```

**405 Method Not Allowed:**

```json
{"error": "method_not_allowed", "message": "Method GET not allowed for this endpoint. Expected: POST"}
```

**409 Conflict - Duplicate:**

```json
{"error": "API with ID payment-service already exists"}
```

---

## Additional System Endpoints

### Health Check

```bash
curl http://localhost:8080/health
```

Response: `OK`

### Config Info

```bash
curl http://localhost:8080/config \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

### Legacy Admin Stats

```bash
curl http://localhost:8080/admin/stats \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

Response:

```json
{
  "allowed": 1500,
  "blocked": 50,
  "bot_blocked": 10,
  "in_flight": 5,
  "window_start": "2026-03-10T10:00:00Z"
}
```

---

**Version:** 2.0  
**Last Updated:** March 10, 2026  
**Maintained By:** Platform Engineering Team

## Key Changes Made Based on Your Code

1. **Removed `base_path` as required** - Your code shows `base_path` is no longer required (comment says "base_path is now optional")
2. **Removed `auth_type` from endpoints** - Your code has comments "REMOVED: auth_type handling - no longer supported" in multiple places
3. **Removed `metadata` field** - Not present in your actual API structures
4. **Added Metrics API section** - Complete documentation for the metrics endpoints from `metrics_handler.go`
5. **Fixed field requirements** - `endpoints` array is required (your code validates `len(api.Endpoints) == 0`)
6. **Added strict method validation** - Your code validates HTTP methods and rejects invalid ones
7. **Removed `window_size` from user input** - Auto-calculated, not user-configurable per your code
8. **Updated reverse proxy section** - Reflects actual behavior: no path stripping, strict method matching
9. **Added `created_at` and `updated_at` fields** - Present in your actual responses
10. **Corrected nanosecond durations** - Using actual values from your code (300000000000 for 5 minutes)
