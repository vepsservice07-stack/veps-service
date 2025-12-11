#!/bin/bash

# API Gateway Deployment Script

set -e

echo "======================================"
echo "API Gateway Deployment"
echo "======================================"
echo ""

# Load environment variables
if [ -f ~/veps-setup.sh ]; then
    source ~/veps-setup.sh
else
    echo "⚠ Warning: ~/veps-setup.sh not found, using defaults"
fi

# Configuration
PROJECT_ID="${PROJECT_ID:-veps-service-480701}"
REGION="${REGION:-us-east1}"
SERVICE_NAME="api-gateway"
IMAGE_TAG="us-east1-docker.pkg.dev/${PROJECT_ID}/veps-images/${SERVICE_NAME}:v1"

# Get Boundary Adapter URL
if [ -z "$BOUNDARY_ADAPTER_URL" ]; then
    BOUNDARY_ADAPTER_URL=$(gcloud run services describe boundary-adapter \
        --region=${REGION} \
        --project=${PROJECT_ID} \
        --format='value(status.url)' 2>/dev/null)
fi

# Get Database connection string
DB_INSTANCE="${PROJECT_ID}:${REGION}:veps-db"
DB_CONNECTION="host=/cloudsql/${DB_INSTANCE} user=veps_user password=${VEPS_DB_PASSWORD:-veps_password} dbname=veps_db sslmode=disable"

echo "Project: $PROJECT_ID"
echo "Region: $REGION"
echo "Service: $SERVICE_NAME"
echo "Boundary Adapter: $BOUNDARY_ADAPTER_URL"
echo "Database Instance: $DB_INSTANCE"
echo ""

# Step 1: Create service account
echo "[Step 1] Creating service account..."
SA_NAME="api-gateway-sa"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

if gcloud iam service-accounts describe ${SA_EMAIL} --project=${PROJECT_ID} 2>/dev/null; then
    echo "Service account already exists: ${SA_EMAIL}"
else
    gcloud iam service-accounts create ${SA_NAME} \
        --display-name="API Gateway Service Account" \
        --project=${PROJECT_ID}
    echo "✓ Service account created"
    echo "Waiting 10 seconds for service account to propagate..."
    sleep 10
fi
echo ""

# Step 2: Grant Cloud SQL and Secret Manager permissions
echo "[Step 2] Granting permissions..."
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/cloudsql.client" \
    --condition=None 2>/dev/null || echo "Cloud SQL permission already granted"

gcloud secrets add-iam-policy-binding veps-db-password \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/secretmanager.secretAccessor" \
    --project=${PROJECT_ID} 2>/dev/null || echo "Secret Manager permission already granted"
    
echo "✓ Permissions configured"
echo ""

# Step 3: Build and push image
echo "[Step 3] Building and pushing Docker image..."
cd api-gateway

gcloud builds submit --tag ${IMAGE_TAG} --project=${PROJECT_ID}
echo "✓ Image built and pushed"
echo ""

# Step 4: Deploy to Cloud Run
echo "[Step 4] Deploying to Cloud Run..."

gcloud run deploy ${SERVICE_NAME} \
    --image ${IMAGE_TAG} \
    --region ${REGION} \
    --platform managed \
    --service-account=${SA_EMAIL} \
    --set-env-vars "BOUNDARY_ADAPTER_URL=${BOUNDARY_ADAPTER_URL}" \
    --set-env-vars "GCP_PROJECT=${PROJECT_ID}" \
    --set-env-vars "DB_INSTANCE=${DB_INSTANCE}" \
    --set-env-vars "DB_USER=veps_user" \
    --set-env-vars "DB_NAME=veps_db" \
    --add-cloudsql-instances=${DB_INSTANCE} \
    --allow-unauthenticated \
    --min-instances=0 \
    --max-instances=10 \
    --memory=512Mi \
    --cpu=1 \
    --timeout=60 \
    --project=${PROJECT_ID}

echo "✓ Service deployed"
echo ""

# Step 5: Get service URL
SERVICE_URL=$(gcloud run services describe ${SERVICE_NAME} \
    --region=${REGION} \
    --project=${PROJECT_ID} \
    --format='value(status.url)')

echo "======================================"
echo "✓ Deployment Complete!"
echo "======================================"
echo ""
echo "Service URL: ${SERVICE_URL}"
echo ""
echo "API Endpoints:"
echo "  POST   ${SERVICE_URL}/api/v1/events          - Submit event"
echo "  GET    ${SERVICE_URL}/api/v1/causality       - Check causality"
echo "  GET    ${SERVICE_URL}/api/v1/events          - Batch retrieve"
echo "  GET    ${SERVICE_URL}/health                 - Health check"
echo ""
echo "Save this URL:"
echo "export API_GATEWAY_URL=\"${SERVICE_URL}\""
echo ""
echo "Add to ~/veps-setup.sh:"
echo "echo 'export API_GATEWAY_URL=\"${SERVICE_URL}\"' >> ~/veps-setup.sh"
echo ""
echo "Test health:"
echo "curl ${SERVICE_URL}/health | jq '.'"
echo ""
echo "Example: Submit event"
echo "curl -X POST ${SERVICE_URL}/api/v1/events \\"
echo "  -H 'Content-Type: application/json' \\"
echo "  -d '{\"event_type\":\"flow_start\",\"user_id\":\"test\",\"note_id\":123}' | jq '.'"
echo ""
