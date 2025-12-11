#!/bin/bash

# Deploy Monolith Submitter to GKE
set -e

echo "======================================"
echo "Monolith Submitter GKE Deployment"
echo "======================================"
echo ""

# Configuration
PROJECT_ID="immutable-ledger"
REGION="us-east1"
CLUSTER_NAME="immutable-ledger-autopilot"
IMAGE_PROJECT="veps-service-480701"
IMAGE_TAG="us-east1-docker.pkg.dev/${IMAGE_PROJECT}/veps-images/monolith-submitter:v1"

echo "Project: $PROJECT_ID"
echo "Cluster: $CLUSTER_NAME"
echo "Image: $IMAGE_TAG"
echo ""

# Step 1: Ensure kubectl is configured
echo "[Step 1] Configuring kubectl..."
gcloud container clusters get-credentials ${CLUSTER_NAME} \
    --region=${REGION} \
    --project=${PROJECT_ID}
echo "✓ kubectl configured"
echo ""

# Step 2: Generate secret key
echo "[Step 2] Generating VEPS secret key..."
VEPS_SECRET_KEY=$(openssl rand -hex 32)
echo "✓ Secret key generated: ${VEPS_SECRET_KEY}"
echo ""

# Step 3: Update manifest with secret
echo "[Step 3] Updating Kubernetes manifest..."
sed "s/REPLACE_WITH_SECRET_KEY/${VEPS_SECRET_KEY}/g" monolith-submitter-k8s.yaml > monolith-submitter-k8s-final.yaml
echo "✓ Manifest updated"
echo ""

# Step 4: Grant GKE access to pull images from veps-service-480701
echo "[Step 4] Configuring cross-project image access..."

# Get the GKE service account
GKE_SA=$(gcloud container clusters describe ${CLUSTER_NAME} \
    --region=${REGION} \
    --project=${PROJECT_ID} \
    --format='value(nodeConfig.serviceAccount)')

if [ -z "$GKE_SA" ]; then
    GKE_SA="${PROJECT_ID}.svc.id.goog[ksa/default]"
fi

echo "GKE Service Account: ${GKE_SA}"

# Grant Artifact Registry Reader role
gcloud artifacts repositories add-iam-policy-binding veps-images \
    --location=us-east1 \
    --project=${IMAGE_PROJECT} \
    --member="serviceAccount:${GKE_SA}" \
    --role="roles/artifactregistry.reader" 2>/dev/null || echo "Permission already granted or need manual setup"

echo "✓ Image access configured"
echo ""

# Step 5: Deploy to Kubernetes
echo "[Step 5] Deploying to Kubernetes..."
kubectl apply -f monolith-submitter-k8s-final.yaml

echo "✓ Deployment created"
echo ""

# Step 6: Wait for LoadBalancer IP
echo "[Step 6] Waiting for LoadBalancer IP (this may take 2-3 minutes)..."
kubectl wait --for=condition=available --timeout=180s deployment/monolith-submitter -n veps || echo "Deployment may still be starting..."

echo "Waiting for external IP..."
for i in {1..60}; do
    EXTERNAL_IP=$(kubectl get svc monolith-submitter -n veps -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null)
    if [ -n "$EXTERNAL_IP" ] && [ "$EXTERNAL_IP" != "<pending>" ]; then
        break
    fi
    echo "  Attempt $i/60: Still waiting..."
    sleep 5
done

if [ -z "$EXTERNAL_IP" ] || [ "$EXTERNAL_IP" == "<pending>" ]; then
    echo "⚠ External IP still pending. Check with: kubectl get svc monolith-submitter -n veps"
    EXTERNAL_IP="PENDING"
else
    echo "✓ External IP assigned: $EXTERNAL_IP"
fi
echo ""

# Step 7: Create firewall rule
echo "[Step 7] Creating firewall rule..."
gcloud compute firewall-rules create allow-monolith-submitter \
    --project=${PROJECT_ID} \
    --allow=tcp:80 \
    --source-ranges=0.0.0.0/0 \
    --description="Allow HTTP traffic to Monolith Submitter" \
    2>/dev/null || echo "Firewall rule already exists"
echo "✓ Firewall rule configured"
echo ""

# Step 8: Check pod status
echo "[Step 8] Checking pod status..."
kubectl get pods -n veps -l app=monolith-submitter
echo ""

# Step 9: Display service info
echo "======================================"
echo "✓ Deployment Complete!"
echo "======================================"
echo ""
echo "Namespace: veps"
echo "Service: monolith-submitter"
echo ""
if [ "$EXTERNAL_IP" != "PENDING" ]; then
    echo "External URL: http://${EXTERNAL_IP}"
    echo "Test health: curl http://${EXTERNAL_IP}/health | jq '.'"
    echo ""
    echo "Save this URL:"
    echo "export MONOLITH_SUBMITTER_URL=\"http://${EXTERNAL_IP}\""
else
    echo "Get external IP with:"
    echo "kubectl get svc monolith-submitter -n veps"
fi
echo ""
echo "Internal URL (from other pods in cluster):"
echo "  http://monolith-submitter-internal.veps.svc.cluster.local:8080"
echo ""
echo "View logs:"
echo "  kubectl logs -n veps -l app=monolith-submitter --tail=100 -f"
echo ""
echo "View pods:"
echo "  kubectl get pods -n veps"
echo ""
echo "VEPS Secret Key (save this): ${VEPS_SECRET_KEY}"
echo ""

# Cleanup temp file
rm -f monolith-submitter-k8s-final.yaml
