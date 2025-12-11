# VEPS API Gateway

## Overview

The **API Gateway** is the customer-facing REST API for VEPS. It provides a clean, simple interface for clients to submit events, check causality, and retrieve event history.

### What It Does:
- Accepts client events in a simple JSON format
- Translates them to VEPS internal format
- Submits to Boundary Adapter for processing
- Returns sequence numbers and proofs
- Queries events from RDB for causality checks and batch retrieval

### Architecture Position:
```
Client (Second Brain) → API Gateway → Boundary Adapter → VEPS Pipeline
                            ↓
                    Queries RDB for retrieval
```

---

## 🚀 Quick Start

### Deploy:
```bash
cd ~/veps-services
tar -xzf api-gateway.tar.gz
chmod +x deploy-api-gateway.sh
./deploy-api-gateway.sh
```

---

## 📡 API Endpoints

### 1. POST /api/v1/events - Submit Event

**Request:**
```json
{
  "event_type": "flow_start",
  "note_id": 123,
  "user_id": "abc",
  "bpm": 73,
  "duration_ms": 5000,
  "timestamp_client": 1702401234567,
  "metadata": {
    "session_id": "xyz"
  }
}
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Event submitted successfully",
  "timestamp": "2025-12-10T21:45:00Z",
  "data": {
    "sequence_number": 1234567890,
    "vector_clock": {
      "boundary-adapter-us-east1-001": 1765373774648014271
    },
    "proof_hash": "a3f9e2d1b8c4...",
    "timestamp_veps": 1702401234589,
    "event_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

**Fields:**
- `event_type` (required): Type of event (e.g., "flow_start", "pause", "burst")
- `user_id` (required): User identifier
- `note_id` (optional): Note/document identifier
- `bpm` (optional): Beats per minute (for rhythm tracking)
- `duration_ms` (optional): Event duration in milliseconds
- `timestamp_client` (required): Client timestamp (ms since epoch)
- `metadata` (optional): Additional custom data

---

### 2. GET /api/v1/causality - Check Causality

**Request:**
```
GET /api/v1/causality?event_a=1234567890&event_b=1234567900
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Causality check complete",
  "timestamp": "2025-12-10T21:46:00Z",
  "data": {
    "relationship": "happened-before",
    "time_delta_ms": 3420,
    "confidence": 1.0
  }
}
```

**Query Parameters:**
- `event_a` (required): Sequence number of first event
- `event_b` (required): Sequence number of second event

**Relationship Values:**
- `"happened-before"`: Event A occurred before Event B
- `"happened-after"`: Event A occurred after Event B
- `"concurrent"`: Events are concurrent (rare with total ordering)

**Confidence:** Always 1.0 with ImmutableLedger's total ordering

---

### 3. GET /api/v1/events - Batch Retrieve

**Request:**
```
GET /api/v1/events?note_id=123&start_time=1702400000000&end_time=1702500000000&limit=50
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Retrieved 25 events",
  "timestamp": "2025-12-10T21:47:00Z",
  "data": {
    "events": [
      {
        "sequence_number": 1234567890,
        "event_type": "flow_start",
        "timestamp_veps": 1702401234589,
        "note_id": 123,
        "user_id": "abc",
        "metadata": {}
      }
    ],
    "total_count": 25
  }
}
```

**Query Parameters:**
- `note_id` (optional): Filter by note ID
- `user_id` (optional): Filter by user ID
- `start_seq` (optional): Start sequence number (inclusive)
- `end_seq` (optional): End sequence number (inclusive)
- `start_time` (optional): Start timestamp in ms since epoch
- `end_time` (optional): End timestamp in ms since epoch
- `limit` (optional): Maximum events to return (default: 100, max: 1000)

---

### 4. GET /health - Health Check

**Request:**
```
GET /health
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "API Gateway is healthy",
  "timestamp": "2025-12-10T21:48:00Z",
  "data": {
    "gateway_ready": true,
    "database_healthy": true,
    "boundary_url": "https://boundary-adapter-..."
  }
}
```

---

## 🧪 Testing

### Test 1: Submit Event
```bash
curl -X POST $API_GATEWAY_URL/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "flow_start",
    "user_id": "alice",
    "note_id": 123,
    "bpm": 73,
    "timestamp_client": '$(date +%s000)'
  }' | jq '.'
```

**Expected:**
- `success: true`
- `sequence_number: <number>`
- `event_id: <uuid>`

---

### Test 2: Submit Multiple Events
```bash
# Submit first event
RESP1=$(curl -s -X POST $API_GATEWAY_URL/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "flow_start",
    "user_id": "alice",
    "note_id": 123,
    "timestamp_client": '$(date +%s000)'
  }')

SEQ1=$(echo $RESP1 | jq -r '.data.sequence_number')
echo "Event 1 sequence: $SEQ1"

# Submit second event
RESP2=$(curl -s -X POST $API_GATEWAY_URL/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "pause",
    "user_id": "alice",
    "note_id": 123,
    "timestamp_client": '$(date +%s000)'
  }')

SEQ2=$(echo $RESP2 | jq -r '.data.sequence_number')
echo "Event 2 sequence: $SEQ2"

# Check causality
curl "$API_GATEWAY_URL/api/v1/causality?event_a=$SEQ1&event_b=$SEQ2" | jq '.'
```

**Expected:**
- `relationship: "happened-before"`
- `time_delta_ms: <positive number>`

---

### Test 3: Batch Retrieve
```bash
curl "$API_GATEWAY_URL/api/v1/events?note_id=123&limit=10" | jq '.'
```

**Expected:**
- `total_count: <number>`
- `events: [...]`

---

## 🏗️ Architecture

### Components:

1. **HTTP Server** (port 8080)
   - Receives client requests
   - Validates input
   - Returns responses

2. **Boundary Adapter Client**
   - Translates client format → VEPS format
   - Submits events to Boundary Adapter
   - Returns sequence numbers

3. **Database Client**
   - Queries RDB for event retrieval
   - Performs causality checks
   - Batch queries with filters

### Data Flow:

#### Submit Event:
```
1. Client sends event (POST /api/v1/events)
   ↓
2. API Gateway validates & transforms to VEPS format
   ↓
3. Calls Boundary Adapter /ingest
   ↓
4. Boundary Adapter → Veto → RDB/Monolith → Ledger
   ↓
5. Returns sequence number to client
```

#### Query Events:
```
1. Client requests events (GET /api/v1/events)
   ↓
2. API Gateway builds SQL query
   ↓
3. Queries RDB (PostgreSQL)
   ↓
4. Returns events to client
```

---

## 🔧 Configuration

### Environment Variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | No | `8080` | HTTP server port |
| `BOUNDARY_ADAPTER_URL` | Yes | - | URL of Boundary Adapter |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |

### Database Connection:

The API Gateway connects to the same PostgreSQL database (veps_db) as RDB Updater:
- Queries the `events` table
- Read-only access
- Uses Cloud SQL Proxy via Unix socket

---

## 📊 Performance

**Target:** Sub-100ms response time for event submission

**Breakdown:**
- API Gateway validation: <5ms
- Boundary Adapter call: 20-30ms
- Veto Service validation: 10-20ms
- RDB/Monolith Submitter: 10-20ms
- **Total: ~50-80ms** ✅

**Query Performance:**
- Single event lookup: <10ms
- Causality check: <20ms (2 lookups)
- Batch query (100 events): <50ms

---

## 🚨 Troubleshooting

### Issue: "BOUNDARY_ADAPTER_URL environment variable is required"

**Fix:**
```bash
export BOUNDARY_ADAPTER_URL="https://boundary-adapter-..."
```

Or set in deployment script.

---

### Issue: "Failed to initialize database client"

**Check database connection:**
```bash
gcloud sql instances describe veps-db --project=veps-service-480701
```

**Verify service account has cloudsql.client role:**
```bash
gcloud projects get-iam-policy veps-service-480701 \
  --flatten="bindings[].members" \
  --filter="bindings.members:api-gateway-sa@*"
```

---

### Issue: "database_healthy: false"

**Check RDB Updater logs:**
```bash
gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=rdb-updater" \
  --limit=20 \
  --project=veps-service-480701
```

The `events` table must exist for queries to work.

---

## 📁 Project Structure

```
api-gateway/
├── cmd/server/main.go              # Server entry point
├── internal/
│   ├── database/client.go          # Database queries
│   └── handler/handler.go          # HTTP handlers
├── pkg/models/models.go            # Data models
├── go.mod                          # Go dependencies
├── Dockerfile                      # Container build
└── README.md                       # This file
```

---

## 🎉 Integration Example (Second Brain Client)

### JavaScript/TypeScript:

```typescript
const VEPS_API = "https://api-gateway-...";

// Submit event
async function trackFlowStart(noteId: number, userId: string, bpm: number) {
  const response = await fetch(`${VEPS_API}/api/v1/events`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      event_type: "flow_start",
      note_id: noteId,
      user_id: userId,
      bpm: bpm,
      timestamp_client: Date.now()
    })
  });
  
  const data = await response.json();
  return data.data.sequence_number;
}

// Check causality
async function checkCausality(seqA: number, seqB: number) {
  const response = await fetch(
    `${VEPS_API}/api/v1/causality?event_a=${seqA}&event_b=${seqB}`
  );
  
  const data = await response.json();
  return data.data.relationship; // "happened-before", etc.
}

// Get note history
async function getNoteHistory(noteId: number) {
  const response = await fetch(
    `${VEPS_API}/api/v1/events?note_id=${noteId}&limit=100`
  );
  
  const data = await response.json();
  return data.data.events;
}
```

---

## 🎯 Next Steps

After deployment:

1. **Test all endpoints** with the examples above
2. **Share API URL** with your Second Brain client
3. **Monitor logs** for any errors
4. **Set up monitoring** (optional: Cloud Monitoring dashboards)

---

**Your client can now integrate!** 🚀
