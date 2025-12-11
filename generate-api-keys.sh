#!/bin/bash

# API Key Generation Script

set -e

PROJECT_ID="${PROJECT_ID:-veps-service-480701}"

echo "======================================"
echo "VEPS API Key Management"
echo "======================================"
echo ""

# Generate a new API key
generate_key() {
    openssl rand -hex 32
}

# Create or update API keys secret
echo "Creating API keys..."
echo ""

# Generate keys for clients
CLIENT1_KEY=$(generate_key)
CLIENT2_KEY=$(generate_key)

# Format: key:clientID:name:rateLimit
API_KEYS="${CLIENT1_KEY}:second-brain:Second Brain App:1000,${CLIENT2_KEY}:test-client:Test Client:100"

# Store in Secret Manager
if gcloud secrets describe veps-api-keys --project=${PROJECT_ID} 2>/dev/null; then
    echo "Updating existing API keys secret..."
    echo -n "$API_KEYS" | gcloud secrets versions add veps-api-keys \
        --data-file=- \
        --project=${PROJECT_ID}
else
    echo "Creating new API keys secret..."
    echo -n "$API_KEYS" | gcloud secrets create veps-api-keys \
        --data-file=- \
        --project=${PROJECT_ID} \
        --replication-policy="automatic"
fi

# Grant API Gateway access to the secret
echo "Granting API Gateway access to secret..."
gcloud secrets add-iam-policy-binding veps-api-keys \
    --member="serviceAccount:api-gateway-sa@${PROJECT_ID}.iam.gserviceaccount.com" \
    --role="roles/secretmanager.secretAccessor" \
    --project=${PROJECT_ID} 2>/dev/null || echo "Permission already granted"

echo ""
echo "======================================"
echo "✓ API Keys Created!"
echo "======================================"
echo ""
echo "Second Brain App API Key:"
echo "  ${CLIENT1_KEY}"
echo ""
echo "Test Client API Key:"
echo "  ${CLIENT2_KEY}"
echo ""
echo "Save these keys securely!"
echo ""
echo "Usage:"
echo "  curl -H 'Authorization: Bearer ${CLIENT1_KEY}' \\"
echo "    https://api-gateway-846963717514.us-east1.run.app/api/v1/events"
echo ""
echo "Rate Limits:"
echo "  - Second Brain: 1000 requests/minute"
echo "  - Test Client: 100 requests/minute"
echo ""
echo "Add to your client's .env file:"
echo "  VEPS_API_KEY=${CLIENT1_KEY}"
echo ""
