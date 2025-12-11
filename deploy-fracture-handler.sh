#!/bin/bash

# Data Fracture Handler Deployment Script

set -e

echo "======================================"
echo "Data Fracture Handler Deployment"
echo "======================================"
echo ""

# Load environment variables
source veps-setup.sh

# Configuration
PROJECT_ID="veps-service-480701"
REGION="us-east1"
SERVICE_NAME="data-fracture-handler"
BUCKET_NAME="veps-fractures-${PROJECT_ID}"
IMAGE_TAG="us-east1-docker.pkg.dev/${PROJECT_ID}/veps-images/${SERVICE_NAME}:v1"

echo "Project: $PROJECT_ID"
echo "Region: $REGION"
echo "Service: $SERVICE_NAME"
echo "Bucket: $BUCKET_NAME"
echo ""

# Step 1: Create GCS bucket if it doesn't exist
echo "[Step 1] Creating Cloud Storage bucket..."
if gsutil ls -b gs://${BUCKET_NAME} 2>/dev/null; then
    echo "Bucket already exists: gs://${BUCKET_NAME}"
else
    gsutil mb -p ${PROJECT_ID} -l ${REGION} -b on gs://${BUCKET_NAME}
    echo "✓ Bucket created: gs://${BUCKET_NAME}"
fi

# Set lifecycle policy (move to Coldline after 30 days for cost savings)
echo "[Step 1b] Setting lifecycle policy..."
cat > /tmp/lifecycle.json << 'EOF'
{
  "lifecycle": {
    "rule": [
      {
        "action": {"type": "SetStorageClass", "storageClass": "COLDLINE"},
        "condition": {"age": 30}
      }
    ]
  }
}
EOF

gsutil lifecycle set /tmp/lifecycle.json gs://${BUCKET_NAME}
rm /tmp/lifecycle.json
echo "✓ Lifecycle policy set (move to Coldline after 30 days)"
echo ""

# Step 2: Create service account
echo "[Step 2] Creating service account..."
SA_NAME="data-fracture-handler-sa"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

if gcloud iam service-accounts describe ${SA_EMAIL} --project=${PROJECT_ID} 2>/dev/null; then
    echo "Service account already exists: ${SA_EMAIL}"
else
    gcloud iam service-accounts create ${SA_NAME} \
        --display-name="Data Fracture Handler Service Account" \
        --project=${PROJECT_ID}
    echo "✓ Service account created"
    echo "Waiting 10 seconds for service account to propagate..."
    sleep 10
fi
echo ""

# Step 3: Grant permissions
echo "[Step 3] Granting Cloud Storage permissions..."

# Remove --condition=None flag (it's causing issues)
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/storage.objectCreator"

gcloud projects add-iam-policy-binding ${PROJECT_ID} \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/storage.objectViewer"

echo "✓ Permissions granted"
echo ""

# Step 4: Build and push image
echo "[Step 4] Building and pushing Docker image..."
cd data-fracture-handler

gcloud builds submit --tag ${IMAGE_TAG} --project=${PROJECT_ID}
echo "✓ Image built and pushed"
echo ""

# Step 5: Deploy to Cloud Run
echo "[Step 5] Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
    --image ${IMAGE_TAG} \
    --region ${REGION} \
    --platform managed \
    --service-account=${SA_EMAIL} \
    --set-env-vars "GCS_BUCKET_NAME=${BUCKET_NAME},FRACTURE_NODE_ID=${SERVICE_NAME}-${REGION}-001" \
    --allow-unauthenticated \
    --min-instances=0 \
    --max-instances=10 \
    --memory=256Mi \
    --cpu=1 \
    --timeout=60 \
    --project=${PROJECT_ID}

echo "✓ Service deployed"
echo ""

# Step 6: Get service URL
SERVICE_URL=$(gcloud run services describe ${SERVICE_NAME} \
    --region=${REGION} \
    --project=${PROJECT_ID} \
    --format='value(status.url)')

echo "======================================"
echo "✓ Deployment Complete!"
echo "======================================"
echo ""
echo "Service URL: ${SERVICE_URL}"
echo "Bucket: gs://${BUCKET_NAME}"
echo ""
echo "Save this URL for Veto Service configuration:"
echo "export DATA_FRACTURE_HANDLER_URL=\"${SERVICE_URL}\""
echo ""
echo "Test health:"
echo "curl ${SERVICE_URL}/health"
echo ""