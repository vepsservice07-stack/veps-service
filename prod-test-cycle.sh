#!/bin/bash

# VEPS Production Test Cycle
# Upgrades to prod config, runs tests, then downgrades back to dev

set -e

PROJECT_ID="veps-service-480701"
REGION="us-east1"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "========================================"
echo "  VEPS Production Test Cycle"
echo "========================================"
echo ""
echo "${YELLOW}This script will:${NC}"
echo "1. Upgrade to production configuration (db-custom-2-8192, min-instances=1)"
echo "2. Wait for services to stabilize and warm up"
echo "3. Run industry-standard load tests (5000+ requests)"
echo "4. Generate comprehensive performance report"
echo "5. Downgrade back to dev configuration"
echo ""
echo "${RED}WARNING:${NC}"
echo "  - Testing will take ~15-20 minutes"
echo "  - Temporary cost increase during test (~$0.50-1.00)"
echo "  - Production configuration: ~$200/month if not downgraded"
echo ""
read -p "Continue? (yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    echo "Aborted."
    exit 0
fi

# Function to check service status
check_service_ready() {
    local service=$1
    local url=$2
    
    echo -n "  Checking $service... "
    
    for i in {1..30}; do
        if curl -s -f "$url/health" > /dev/null 2>&1; then
            echo "${GREEN}ready${NC}"
            return 0
        fi
        sleep 2
    done
    
    echo "${RED}timeout${NC}"
    return 1
}

# Phase 1: Upgrade to Production Config
echo ""
echo "${BLUE}=== Phase 1: Upgrading to Production Configuration ===${NC}"
echo ""

echo "Upgrading Cloud SQL to production tier..."
gcloud sql instances patch veps-db \
    --tier=db-custom-2-8192 \
    --quiet

echo "Setting min-instances=1 on all services..."
gcloud run services update boundary-adapter --region $REGION --min-instances 1 --quiet
gcloud run services update veto-service --region $REGION --min-instances 1 --quiet
gcloud run services update rdb-updater --region $REGION --min-instances 1 --quiet

echo "${GREEN}Upgrade complete!${NC}"
echo ""

# Phase 2: Warmup Period
echo "${BLUE}=== Phase 2: Warming Up Services ===${NC}"
echo ""
echo "Waiting 60 seconds for instances to stabilize..."
sleep 60

echo "Checking service health..."
check_service_ready "Boundary Adapter" "https://boundary-adapter-846963717514.us-east1.run.app"
check_service_ready "Veto Service" "https://veto-service-846963717514.us-east1.run.app"
check_service_ready "RDB Updater" "https://rdb-updater-846963717514.us-east1.run.app"

echo ""
echo "Running warmup requests..."
for i in {1..20}; do
    curl -s -X POST https://boundary-adapter-846963717514.us-east1.run.app/ingest \
        -H "Content-Type: application/json" \
        -d "{
            \"source\": \"warmup\",
            \"data\": {
                \"type\": \"payment_processed\",
                \"actor\": {\"id\": \"warmup-$i\", \"name\": \"Warmup\", \"type\": \"user\"},
                \"amount\": 100.00,
                \"currency\": \"USD\"
            }
        }" > /dev/null
    echo -n "."
done
echo ""
echo "${GREEN}Services warmed up!${NC}"
echo ""

# Phase 3: Run Load Tests
echo "${BLUE}=== Phase 3: Running Load Tests ===${NC}"
echo ""

# Check if load test script exists
if [ ! -f "./load-test.sh" ]; then
    echo "${RED}Error: load-test.sh not found${NC}"
    echo "Please ensure load-test.sh is in the current directory"
    exit 1
fi

# Run the load test
bash ./load-test.sh

echo ""
echo "${GREEN}Load tests complete!${NC}"
echo ""

# Phase 4: Downgrade Back to Dev
echo "${BLUE}=== Phase 4: Downgrading to Dev Configuration ===${NC}"
echo ""

read -p "Downgrade back to dev config? (yes/no): " downgrade

if [ "$downgrade" == "yes" ]; then
    echo "Setting min-instances=0 on all services..."
    gcloud run services update boundary-adapter --region $REGION --min-instances 0 --quiet
    gcloud run services update veto-service --region $REGION --min-instances 0 --quiet
    gcloud run services update rdb-updater --region $REGION --min-instances 0 --quiet
    
    echo "Downgrading Cloud SQL to dev tier..."
    gcloud sql instances patch veps-db \
        --tier=db-f1-micro \
        --quiet
    
    echo "${GREEN}Downgrade complete!${NC}"
else
    echo "${YELLOW}Keeping production configuration${NC}"
fi

echo ""
echo "========================================"
echo "${GREEN}Test Cycle Complete!${NC}"
echo "========================================"
echo ""
echo "Next steps:"
echo "1. Review test results in test-results-* directory"
echo "2. Check latency percentiles"
echo "3. Verify P99 < 50ms target"
echo ""