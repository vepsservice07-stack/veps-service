#!/bin/bash

# VEPS Service Setup Script
# Run this script to configure your environment for VEPS development and deployment
# Usage: source veps-setup.sh

echo "========================================"
echo "  VEPS Service Environment Setup"
echo "========================================"
echo ""

# Project Configuration
export PROJECT_ID="veps-service-480701"
export REGION="us-east1"
export EXPECTED_ACCOUNT="vepsservice07@gmail.com"

# Check current account
CURRENT_ACCOUNT=$(gcloud config get-value account 2>/dev/null)

echo "Current account: $CURRENT_ACCOUNT"
echo "Expected account: $EXPECTED_ACCOUNT"
echo ""

if [ "$CURRENT_ACCOUNT" != "$EXPECTED_ACCOUNT" ]; then
    echo "⚠️  Account mismatch detected!"
    echo "Switching to $EXPECTED_ACCOUNT..."
    echo ""
    
    # Check if the expected account is already authenticated
    if gcloud auth list --filter="account:$EXPECTED_ACCOUNT" --format="value(account)" 2>/dev/null | grep -q "$EXPECTED_ACCOUNT"; then
        echo "Account $EXPECTED_ACCOUNT is already authenticated. Switching..."
        gcloud config set account $EXPECTED_ACCOUNT
    else
        echo "Account $EXPECTED_ACCOUNT is not authenticated. Please login..."
        gcloud auth login $EXPECTED_ACCOUNT
    fi
    echo ""
fi

# Service URLs
export BOUNDARY_ADAPTER_URL="https://boundary-adapter-846963717514.us-east1.run.app"
export RDB_UPDATER_URL="https://rdb-updater-846963717514.us-east1.run.app"
export VETO_SERVICE_URL="https://veto-service-846963717514.us-east1.run.app"

# Database Configuration
export DB_INSTANCE="veps-db"
export DB_CONNECTION_NAME="veps-service-480701:us-east1:veps-db"
export DB_NAME="veps"
export DB_USER="veps_app"

# VPC Configuration
export VPC_NETWORK="veps-network"
export VPC_SUBNET="veps-subnet"
export VPC_CONNECTOR="veps-connector"

# Artifact Registry
export ARTIFACT_REGISTRY_LOCATION="us-east1"
export ARTIFACT_REGISTRY_REPO="veps-images"

# Set gcloud project
echo "Setting gcloud project to: $PROJECT_ID"
gcloud config set project $PROJECT_ID

# Verify configuration
echo ""
echo "Environment variables set:"
echo "  PROJECT_ID: $PROJECT_ID"
echo "  REGION: $REGION"
echo ""
echo "Service URLs:"
echo "  Boundary Adapter: $BOUNDARY_ADAPTER_URL"
echo "  RDB Updater: $RDB_UPDATER_URL"
echo "  Veto Service: $VETO_SERVICE_URL"
echo ""
echo "Database:"
echo "  Instance: $DB_INSTANCE"
echo "  Connection: $DB_CONNECTION_NAME"
echo ""
echo "VPC:"
echo "  Network: $VPC_NETWORK"
echo "  Connector: $VPC_CONNECTOR"
echo ""
echo "========================================"
echo "  Setup Complete!"
echo "========================================"
echo ""
echo "You can now run gcloud commands without specifying --project or --region"
echo "Example: gcloud run services list --region \$REGION"
echo ""