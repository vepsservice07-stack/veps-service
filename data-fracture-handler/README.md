# Data Fracture Handler - Deployment Guide

## 🎯 Overview

The **Data Fracture Handler** is the audit trail component for VEPS that logs all vetoed (rejected) events for compliance, forensics, and pattern detection.

### What It Does:
- Captures all events that fail validation checks
- Stores them in Cloud Storage for permanent audit trail
- Provides query capabilities for investigation
- Fire-and-forget pattern (doesn't slow down rejection)

### Cost-Optimized Design:
- **Cloud Storage only** (no PostgreSQL for now)
- **~$0.02/GB/month** (Coldline after 30 days)
- **Min instances = 0** (scales to zero when idle)
- **256Mi memory** (minimal footprint)

---

## 📁 What You Have

Extract the tarball:
```bash
cd ~/veps-services
tar -xzf data-fracture-handler.tar.gz
```

**Structure:**
```
data-fracture-handler/
├── cmd/server/main.go              # Server entry point
├── internal/
│   ├── handler/handler.go          # HTTP handlers
│   └── storage/cloudstorage.go     # GCS client
├── pkg/models/fracture.go          # Data models
├── go.mod                          # Dependencies
└── Dockerfile                      # Container build
```

---

## 🚀 Deployment Steps

### Step 1: Deploy Data Fracture Handler

```bash
chmod +x deploy-fracture-handler.sh
./deploy-fracture-handler.sh
```

This script will:
1. ✅ Create GCS bucket: `veps-fractures-veps-service-480701`
2. ✅ Set lifecycle policy (move to Coldline after 30 days)
3. ✅ Create service account with storage permissions
4. ✅ Build and push Docker image
5. ✅ Deploy to Cloud Run (min-instances=0, 256Mi memory)
6. ✅ Output the service URL

**Save the URL** displayed at the end!

---

### Step 2: Update Veto Service

Replace your existing Veto Service handler:

```bash
cd ~/veps-services/veto-service/internal/handler
cp ~/veps-services/data-fracture-handler/veto-service-handler-updated.go handler.go
```

**What changed:**
- Added Data Fracture Handler client
- Calls `/fracture` endpoint when validation fails (non-blocking)
- Fire-and-forget pattern (doesn't wait for response)

---

### Step 3: Redeploy Veto Service with Fracture Handler URL

```bash
cd ~/veps-services/veto-service

# Set the Data Fracture Handler URL
export DATA_FRACTURE_HANDLER_URL="<URL from Step 1>"

# Rebuild
go mod tidy
gcloud builds submit --tag us-east1-docker.pkg.dev/veps-service-480701/veps-images/veto-service:v3

# Redeploy with new environment variable
gcloud run deploy veto-service \
  --image us-east1-docker.pkg.dev/veps-service-480701/veps-images/veto-service:v3 \
  --region us-east1 \
  --service-account=veto-service-sa@veps-service-480701.iam.gserviceaccount.com \
  --set-env-vars "RDB_UPDATER_URL=$RDB_UPDATER_URL,DATA_FRACTURE_HANDLER_URL=$DATA_FRACTURE_HANDLER_URL,VETO_SERVICE_NODE_ID=veto-service-us-east1-001"
```

---

## 🧪 Testing

### Test 1: Health Check

```bash
export FRACTURE_HANDLER_URL="<your-service-url>"
curl $FRACTURE_HANDLER_URL/health
```

**Expected:**
```json
{
  "success": true,
  "message": "Data Fracture Handler is healthy",
  "timestamp": "2025-12-10T14:30:00Z"
}
```

---

### Test 2: Trigger a Veto (Invalid Payment)

```bash
curl -X POST $BOUNDARY_ADAPTER_URL/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "source": "test",
    "data": {
      "type": "payment_processed",
      "actor": {"id": "test-user", "name": "Test", "type": "user"},
      "amount": 5000000.00,
      "currency": "USD",
      "payment_method": "wire"
    }
  }'
```

**Expected:** Event is rejected (HTTP 412) and fracture is logged

---

### Test 3: Query Fractured Events

```bash
# Query today's fractures
TODAY=$(date +%Y-%m-%d)
curl "$FRACTURE_HANDLER_URL/fractures?date=$TODAY" | jq '.'
```

**Expected:**
```json
{
  "success": true,
  "message": "Found 1 fractures for 2025-12-10",
  "data": {
    "date": "2025-12-10",
    "count": 1,
    "fractures": [
      {
        "fracture_id": "...",
        "timestamp": "2025-12-10T14:30:00Z",
        "event": { ... },
        "rejection": {
          "failed_checks": ["business_rules"],
          "reasons": ["business_rules: payment amount exceeds limit: 5000000.00"],
          "veto_service_node": "veto-service-us-east1-001"
        }
      }
    ]
  }
}
```

---

### Test 4: Check Cloud Storage

```bash
# List fractures in GCS
gsutil ls -r gs://veps-fractures-veps-service-480701/

# View today's fractures
gsutil cat gs://veps-fractures-veps-service-480701/2025/12/10/*.jsonl | jq '.'
```

---

## 📊 Data Structure

Each fractured event contains:

```json
{
  "fracture_id": "uuid",              // Unique fracture ID
  "timestamp": "2025-12-10T14:30:00Z", // When vetoed
  "event": {                          // Full normalized event
    "id": "uuid",
    "type": "payment_processed",
    "actor": {...},
    "evidence": {...},
    "vector_clock": {...}
  },
  "rejection": {
    "failed_checks": ["business_rules", "temporal"],
    "reasons": [
      "business_rules: payment amount exceeds limit",
      "temporal: event timestamp too old"
    ],
    "veto_service_node": "veto-service-us-east1-001",
    "validation_duration": "5.2ms"
  },
  "context": {
    "veps_node_id": "boundary-adapter-us-east1-001",
    "correlation_id": "abc-123",
    "vector_clock": {...},
    "original_source": "payment-gateway",
    "received_at": "2025-12-10T14:30:00Z"
  }
}
```

---

## 💰 Cost Analysis

**Storage:**
- 1,000 vetoed events/day ≈ 1MB/day ≈ 30MB/month
- First 30 days: Standard storage ($0.02/GB) = **~$0.00**
- After 30 days: Coldline storage ($0.004/GB) = **~$0.00**

**Cloud Run:**
- Min instances = 0 (scales to zero)
- Only billed when processing requests
- Estimated: **<$1/month** for typical load

**Total:** **~$1/month or less**

---

## 🔍 Use Cases

### 1. Compliance Audit
```bash
# Get all fractures for a specific date
curl "$FRACTURE_HANDLER_URL/fractures?date=2025-12-10"
```

### 2. Fraud Pattern Detection
```bash
# Download all fractures and analyze
gsutil -m cp -r gs://veps-fractures-veps-service-480701/2025/12/ ./fractures/
# Analyze with your tools
```

### 3. Debugging Failed Transactions
```bash
# Search for specific event
gsutil cat gs://veps-fractures-veps-service-480701/2025/12/10/*.jsonl | \
  jq 'select(.event.actor.id == "user-123")'
```

---

## 🔐 Security

- **Authentication:** Service-to-service with OAuth2
- **Authorization:** Service account with minimal GCS permissions
- **Data Protection:** Bucket versioning enabled
- **Access Control:** IAM policies restrict bucket access

---

## 📈 Future Enhancements

When you have paying customers and need faster queries:

### Add PostgreSQL Table
```sql
CREATE TABLE fractured_events (
  fracture_id UUID PRIMARY KEY,
  timestamp TIMESTAMPTZ NOT NULL,
  event_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  failed_checks TEXT[] NOT NULL,
  reasons TEXT[] NOT NULL,
  data JSONB NOT NULL,
  INDEX idx_timestamp ON fractured_events(timestamp),
  INDEX idx_actor ON fractured_events(actor_id),
  INDEX idx_event_type ON fractured_events(event_type)
);
```

Update `cloudstorage.go` to also write to PostgreSQL (dual-write pattern).

---

## 🎉 You're Done!

The Data Fracture Handler is now:
- ✅ Deployed and running
- ✅ Integrated with Veto Service
- ✅ Logging all vetoed events
- ✅ Costing nearly $0/month
- ✅ Ready for compliance audits

**Next Component:** Monolith Submitter (queues certified events for ImmutableLedger)
