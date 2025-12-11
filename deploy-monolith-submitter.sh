#!/bin/bash

# Monolith Submitter Deployment Script

set -e

echo "======================================"
echo "Monolith Submitter Deployment"
echo "======================================"
echo ""

# Load environment variables
source veps-setup.sh

# Configuration
PROJECT_ID=$(gcloud config get-value project)
REGION="us-east1"
SERVICE_NAME="monolith-submitter"
IMAGE_TAG="us-east1-docker.pkg.dev/${PROJECT_ID}/veps-images/${SERVICE_NAME}:v1"
LEDGER_ADDRESS="ledger-service.immutable-ledger.svc.cluster.local:50051"

echo "Project: $PROJECT_ID"
echo "Region: $REGION"
echo "Service: $SERVICE_NAME"
echo "Ledger Address: $LEDGER_ADDRESS"
echo ""

# Step 1: Create service account
echo "[Step 1] Creating service account..."
SA_NAME="monolith-submitter-sa"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

if gcloud iam service-accounts describe ${SA_EMAIL} --project=${PROJECT_ID} 2>/dev/null; then
    echo "Service account already exists: ${SA_EMAIL}"
else
    gcloud iam service-accounts create ${SA_NAME} \
        --display-name="Monolith Submitter Service Account" \
        --project=${PROJECT_ID}
    echo "✓ Service account created"
    echo "Waiting 10 seconds for service account to propagate..."
    sleep 10
fi
echo ""

# Step 2: Build and push image
echo "[Step 2] Building and pushing Docker image..."
cd monolith-submitter

gcloud builds submit --tag ${IMAGE_TAG} --project=${PROJECT_ID}
echo "✓ Image built and pushed"
echo ""

# Step 3: Deploy to Cloud Run
echo "[Step 3] Deploying to Cloud Run..."

# Generate a random secret key if not set
if [ -z "$VEPS_SECRET_KEY" ]; then
    VEPS_SECRET_KEY=$(openssl rand -hex 32)
    echo "Generated new VEPS_SECRET_KEY: ${VEPS_SECRET_KEY}"
    echo "export VEPS_SECRET_KEY=\"${VEPS_SECRET_KEY}\"" >> ~/veps-setup.sh
fi

gcloud run deploy ${SERVICE_NAME} \
    --image ${IMAGE_TAG} \
    --region ${REGION} \
    --platform managed \
    --service-account=${SA_EMAIL} \
    --set-env-vars "LEDGER_ADDRESS=${LEDGER_ADDRESS},VEPS_SECRET_KEY=${VEPS_SECRET_KEY},MONOLITH_NODE_ID=${SERVICE_NAME}-${REGION}-001" \
    --vpc-connector=veps-connector \
    --vpc-egress=all-traffic \
    --allow-unauthenticated \
    --min-instances=0 \
    --max-instances=10 \
    --memory=512Mi \
    --cpu=1 \
    --timeout=60 \
    --project=${PROJECT_ID}

echo "✓ Service deployed"
echo ""

# Step 4: Get service URL
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
echo "Save this URL for integration:"
echo "export MONOLITH_SUBMITTER_URL=\"${SERVICE_URL}\""
echo ""
echo "Add to ~/veps-setup.sh:"
echo "echo 'export MONOLITH_SUBMITTER_URL=\"${SERVICE_URL}\"' >> ~/veps-setup.sh"
echo ""
echo "Test health:"
echo "curl ${SERVICE_URL}/health"
echo ""
echo "Test connectivity to Ledger:"
echo "curl ${SERVICE_URL}/health | jq '.data.ledger_healthy'"
echo ""
