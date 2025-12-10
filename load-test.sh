#!/bin/bash

# VEPS Industry-Standard Performance Test Suite
# Based on financial services benchmarks and distributed systems testing standards
# 
# Standards Reference:
# - Payment Card Industry (PCI) DSS: <2s response time for 95% of transactions
# - High-frequency trading: P99 <50ms
# - REST API best practices: P95 <100ms, P99 <200ms
# - Google SRE Book: 99.9% availability, measure four golden signals

set -e

# Configuration
BOUNDARY_ADAPTER_URL="${BOUNDARY_ADAPTER_URL:-https://boundary-adapter-846963717514.us-east1.run.app}"

# Industry-standard test parameters
WARMUP_REQUESTS=50              # Industry standard: 50-100 warmup requests
SUSTAINED_LOAD_REQUESTS=5000    # 5K requests for statistical significance
BURST_LOAD_REQUESTS=1000        # Burst testing
CONCURRENT_USERS=50             # Simulates realistic concurrent load
SPIKE_USERS=200                 # Spike testing (4x normal)

# Financial services workload distribution (based on industry patterns)
# 70% payments, 20% withdrawals, 10% high-value (potential veto)
PAYMENT_RATIO=70
WITHDRAWAL_RATIO=20
HIGH_VALUE_RATIO=10

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo "========================================"
echo "  VEPS Industry-Standard Load Test"
echo "========================================"
echo ""
echo "Standard: Financial Services / Payment Processing"
echo "Target: $BOUNDARY_ADAPTER_URL"
echo ""
echo "Test Profile:"
echo "  - Warmup: $WARMUP_REQUESTS requests"
echo "  - Sustained Load: $SUSTAINED_LOAD_REQUESTS requests"
echo "  - Concurrent Users: $CONCURRENT_USERS"
echo "  - Spike Load: $SPIKE_USERS users"
echo "  - Workload: ${PAYMENT_RATIO}% payments, ${WITHDRAWAL_RATIO}% withdrawals, ${HIGH_VALUE_RATIO}% high-value"
echo ""

# Create results directory
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
RESULTS_DIR="./veps-test-results-$TIMESTAMP"
mkdir -p "$RESULTS_DIR"

# Create summary report
REPORT_FILE="$RESULTS_DIR/test-report.txt"

log_report() {
    echo "$1" | tee -a "$REPORT_FILE"
}

log_report "========================================"
log_report "VEPS Performance Test Report"
log_report "Timestamp: $(date)"
log_report "========================================"
log_report ""

# Function to make a request and measure latency
make_request() {
    local type=$1
    local amount=$2
    local actor_id=$3
    local test_phase=$4
    
    START=$(date +%s%N)
    
    RESPONSE=$(curl -s -w "\n%{http_code}\n%{time_total}" -X POST "$BOUNDARY_ADAPTER_URL/ingest" \
        -H "Content-Type: application/json" \
        -d "{
            \"source\": \"load-test-$test_phase\",
            \"data\": {
                \"type\": \"$type\",
                \"actor\": {
                    \"id\": \"$actor_id\",
                    \"name\": \"TestUser$actor_id\",
                    \"type\": \"user\"
                },
                \"amount\": $amount,
                \"currency\": \"USD\",
                \"payment_method\": \"credit_card\",
                \"test_metadata\": {
                    \"phase\": \"$test_phase\",
                    \"timestamp\": \"$(date -Iseconds)\"
                }
            }
        }")
    
    HTTP_CODE=$(echo "$RESPONSE" | tail -n 2 | head -n 1)
    TIME_TOTAL=$(echo "$RESPONSE" | tail -n 1)
    BODY=$(echo "$RESPONSE" | head -n -2)
    
    END=$(date +%s%N)
    DURATION_MS=$(( (END - START) / 1000000 ))
    
    # Extract routing duration if available
    ROUTING_MS=$(echo "$BODY" | grep -o '"routing_duration":"[^"]*"' | cut -d'"' -f4 | sed 's/[^0-9.]//g')
    
    # Extract success status
    SUCCESS=$(echo "$BODY" | grep -o '"success":[^,]*' | cut -d':' -f2)
    
    echo "$HTTP_CODE,$DURATION_MS,$ROUTING_MS,$type,$amount,$test_phase,$SUCCESS"
}

# Determine request type based on workload distribution
get_request_type() {
    local rand=$((RANDOM % 100))
    
    if [ $rand -lt $PAYMENT_RATIO ]; then
        echo "payment_processed"
    elif [ $rand -lt $((PAYMENT_RATIO + WITHDRAWAL_RATIO)) ]; then
        echo "withdrawal"
    else
        echo "payment_processed" # High value payment (will be vetoed)
    fi
}

# Get amount based on request type
get_amount() {
    local type=$1
    local rand=$((RANDOM % 100))
    
    if [ $rand -lt $HIGH_VALUE_RATIO ]; then
        # High value (should trigger veto)
        echo "5000000.00"
    elif [ "$type" == "withdrawal" ]; then
        # Withdrawal: $10-$5000
        echo "$((RANDOM % 4990 + 10)).00"
    else
        # Normal payment: $10-$10000
        echo "$((RANDOM % 9990 + 10)).00"
    fi
}

# Calculate percentiles using awk
calculate_percentiles() {
    local file=$1
    local column=$2
    local phase=$3
    
    awk -F',' -v col=$column -v phase="$phase" '
    BEGIN {
        printf "\n"
        printf "=== %s ===\n", phase
    }
    NR > 1 && $col ~ /^[0-9.]+$/ && $col > 0 {
        values[NR] = $col
        sum += $col
        if (min == "" || $col < min) min = $col
        if ($col > max) max = $col
        count++
    }
    END {
        if (count == 0) {
            print "No valid data"
            exit
        }
        
        # Sort values
        n = asort(values)
        
        # Calculate percentiles
        p50_idx = int(n * 0.50) > 0 ? int(n * 0.50) : 1
        p90_idx = int(n * 0.90) > 0 ? int(n * 0.90) : 1
        p95_idx = int(n * 0.95) > 0 ? int(n * 0.95) : 1
        p99_idx = int(n * 0.99) > 0 ? int(n * 0.99) : 1
        p999_idx = int(n * 0.999) > 0 ? int(n * 0.999) : 1
        
        avg = sum / count
        
        printf "Sample Size: %d requests\n", count
        printf "Average: %.2f ms\n", avg
        printf "Median (P50): %.2f ms\n", values[p50_idx]
        printf "P90: %.2f ms\n", values[p90_idx]
        printf "P95: %.2f ms\n", values[p95_idx]
        printf "P99: %.2f ms\n", values[p99_idx]
        printf "P99.9: %.2f ms\n", values[p999_idx]
        printf "Min: %.2f ms\n", min
        printf "Max: %.2f ms\n", max
        printf "\n"
    }' "$file"
}

# Phase 0: Pre-flight checks
echo "${CYAN}=== Phase 0: Pre-flight Checks ===${NC}"
echo ""
echo -n "Checking Boundary Adapter health... "
if curl -s -f "$BOUNDARY_ADAPTER_URL/health" > /dev/null; then
    echo "${GREEN}OK${NC}"
else
    echo "${RED}FAILED${NC}"
    echo "Cannot reach Boundary Adapter. Exiting."
    exit 1
fi
echo ""

# Phase 1: Warmup
echo "${YELLOW}=== Phase 1: Warmup ($WARMUP_REQUESTS requests) ===${NC}"
log_report "Phase 1: Warmup"
echo ""

for i in $(seq 1 $WARMUP_REQUESTS); do
    TYPE=$(get_request_type)
    AMOUNT=$(get_amount $TYPE)
    ACTOR_ID="warmup-$i"
    
    make_request "$TYPE" "$AMOUNT" "$ACTOR_ID" "warmup" > /dev/null 2>&1
    
    if [ $((i % 10)) -eq 0 ]; then
        echo -n "."
    fi
done
echo ""
echo "${GREEN}Warmup complete${NC}"
log_report "Warmup: $WARMUP_REQUESTS requests completed"
echo ""
sleep 2

# Phase 2: Baseline Performance Test
echo "${YELLOW}=== Phase 2: Baseline Performance ($SUSTAINED_LOAD_REQUESTS requests) ===${NC}"
log_report ""
log_report "Phase 2: Baseline Performance Test"
echo ""

CSV_FILE="$RESULTS_DIR/baseline-latencies.csv"
echo "http_code,total_duration_ms,routing_duration_ms,event_type,amount,phase,success" > "$CSV_FILE"

START_TIME=$(date +%s)

for i in $(seq 1 $SUSTAINED_LOAD_REQUESTS); do
    TYPE=$(get_request_type)
    AMOUNT=$(get_amount $TYPE)
    ACTOR_ID=$((RANDOM % 1000 + 1))
    
    make_request "$TYPE" "$AMOUNT" "user-$ACTOR_ID" "baseline" >> "$CSV_FILE"
    
    if [ $((i % 500)) -eq 0 ]; then
        echo "  Progress: $i/$SUSTAINED_LOAD_REQUESTS requests..."
    fi
done

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))
THROUGHPUT=$(echo "scale=2; $SUSTAINED_LOAD_REQUESTS / $DURATION" | bc)

echo ""
echo "${GREEN}Baseline test complete${NC}"
log_report "Completed $SUSTAINED_LOAD_REQUESTS requests in ${DURATION}s"
log_report "Throughput: ${THROUGHPUT} req/s"
echo ""

# Phase 3: Concurrent Load Test
echo "${YELLOW}=== Phase 3: Concurrent Load ($CONCURRENT_USERS users) ===${NC}"
log_report ""
log_report "Phase 3: Concurrent Load Test"
echo ""

CSV_FILE="$RESULTS_DIR/concurrent-latencies.csv"
echo "http_code,total_duration_ms,routing_duration_ms,event_type,amount,phase,success" > "$CSV_FILE"

BATCHES=100
REQUESTS_PER_BATCH=$CONCURRENT_USERS

for batch in $(seq 1 $BATCHES); do
    for user in $(seq 1 $REQUESTS_PER_BATCH); do
        (
            TYPE=$(get_request_type)
            AMOUNT=$(get_amount $TYPE)
            ACTOR_ID=$((user + batch * REQUESTS_PER_BATCH))
            make_request "$TYPE" "$AMOUNT" "concurrent-user-$ACTOR_ID" "concurrent" >> "$CSV_FILE"
        ) &
    done
    
    wait
    
    if [ $((batch % 10)) -eq 0 ]; then
        echo "  Completed $((batch * REQUESTS_PER_BATCH)) requests..."
    fi
done

echo "${GREEN}Concurrent test complete${NC}"
log_report "Completed $((BATCHES * REQUESTS_PER_BATCH)) concurrent requests"
echo ""

# Phase 4: Spike Test
echo "${YELLOW}=== Phase 4: Spike Test ($SPIKE_USERS concurrent users) ===${NC}"
log_report ""
log_report "Phase 4: Spike Test"
echo ""

CSV_FILE="$RESULTS_DIR/spike-latencies.csv"
echo "http_code,total_duration_ms,routing_duration_ms,event_type,amount,phase,success" > "$CSV_FILE"

echo "Generating spike load..."

for user in $(seq 1 $SPIKE_USERS); do
    (
        TYPE=$(get_request_type)
        AMOUNT=$(get_amount $TYPE)
        make_request "$TYPE" "$AMOUNT" "spike-user-$user" "spike" >> "$CSV_FILE"
    ) &
done

wait

echo "${GREEN}Spike test complete${NC}"
log_report "Completed $SPIKE_USERS simultaneous requests"
echo ""

# Phase 5: Analysis
echo "${BLUE}=== Phase 5: Results Analysis ===${NC}"
echo ""

# Baseline results
echo "${CYAN}BASELINE PERFORMANCE (Sequential Load)${NC}" | tee -a "$REPORT_FILE"
calculate_percentiles "$RESULTS_DIR/baseline-latencies.csv" 2 "Total End-to-End Latency" | tee -a "$REPORT_FILE"
calculate_percentiles "$RESULTS_DIR/baseline-latencies.csv" 3 "Internal Routing Latency" | tee -a "$REPORT_FILE"

# Concurrent results
echo "${CYAN}CONCURRENT PERFORMANCE ($CONCURRENT_USERS users)${NC}" | tee -a "$REPORT_FILE"
calculate_percentiles "$RESULTS_DIR/concurrent-latencies.csv" 2 "Total Latency Under Load" | tee -a "$REPORT_FILE"

# Spike results
echo "${CYAN}SPIKE PERFORMANCE ($SPIKE_USERS simultaneous users)${NC}" | tee -a "$REPORT_FILE"
calculate_percentiles "$RESULTS_DIR/spike-latencies.csv" 2 "Total Latency Under Spike" | tee -a "$REPORT_FILE"

# Success rates
echo "${CYAN}SUCCESS METRICS${NC}" | tee -a "$REPORT_FILE"
echo "" | tee -a "$REPORT_FILE"

TOTAL=$(awk -F',' 'NR > 1' "$RESULTS_DIR/baseline-latencies.csv" | wc -l)
SUCCESS=$(awk -F',' 'NR > 1 && $1 == 200' "$RESULTS_DIR/baseline-latencies.csv" | wc -l)
VETOED=$(awk -F',' 'NR > 1 && $1 != 200' "$RESULTS_DIR/baseline-latencies.csv" | wc -l)

SUCCESS_RATE=$(echo "scale=2; ($SUCCESS / $TOTAL) * 100" | bc)
VETO_RATE=$(echo "scale=2; ($VETOED / $TOTAL) * 100" | bc)

echo "Total Requests: $TOTAL" | tee -a "$REPORT_FILE"
echo "Successful: $SUCCESS (${SUCCESS_RATE}%)" | tee -a "$REPORT_FILE"
echo "Vetoed: $VETOED (${VETO_RATE}%)" | tee -a "$REPORT_FILE"
echo "" | tee -a "$REPORT_FILE"

# SLA Compliance Check
echo "${CYAN}SLA COMPLIANCE CHECK${NC}" | tee -a "$REPORT_FILE"
echo "" | tee -a "$REPORT_FILE"

# Extract P99 from baseline
P99=$(awk -F',' 'NR > 1 && $2 ~ /^[0-9.]+$/ && $2 > 0 {values[NR] = $2; count++} END {n = asort(values); p99_idx = int(n * 0.99); print values[p99_idx]}' "$RESULTS_DIR/baseline-latencies.csv")
P999=$(awk -F',' 'NR > 1 && $2 ~ /^[0-9.]+$/ && $2 > 0 {values[NR] = $2; count++} END {n = asort(values); p999_idx = int(n * 0.999); print values[p999_idx]}' "$RESULTS_DIR/baseline-latencies.csv")

# Financial Services Benchmarks
echo "Target: Financial Services / Payment Processing" | tee -a "$REPORT_FILE"
echo "" | tee -a "$REPORT_FILE"

# P99 < 50ms check
if (( $(echo "$P99 < 50" | bc -l) )); then
    echo "${GREEN}✓ P99 < 50ms: PASS (${P99}ms)${NC}" | tee -a "$REPORT_FILE"
else
    echo "${RED}✗ P99 < 50ms: FAIL (${P99}ms)${NC}" | tee -a "$REPORT_FILE"
fi

# P99.9 < 100ms check
if (( $(echo "$P999 < 100" | bc -l) )); then
    echo "${GREEN}✓ P99.9 < 100ms: PASS (${P999}ms)${NC}" | tee -a "$REPORT_FILE"
else
    echo "${RED}✗ P99.9 < 100ms: FAIL (${P999}ms)${NC}" | tee -a "$REPORT_FILE"
fi

# Success rate > 90% (accounting for intentional vetos)
if (( $(echo "$SUCCESS_RATE > 90" | bc -l) )); then
    echo "${GREEN}✓ Success Rate > 90%: PASS (${SUCCESS_RATE}%)${NC}" | tee -a "$REPORT_FILE"
else
    echo "${RED}✗ Success Rate > 90%: FAIL (${SUCCESS_RATE}%)${NC}" | tee -a "$REPORT_FILE"
fi

echo "" | tee -a "$REPORT_FILE"
echo "========================================"
echo "${GREEN}Test Complete!${NC}"
echo "========================================"
echo ""
echo "Results saved to: $RESULTS_DIR"
echo "Full report: $REPORT_FILE"
echo ""
echo "CSV files for detailed analysis:"
echo "  - baseline-latencies.csv (sustained load)"
echo "  - concurrent-latencies.csv (concurrent users)"
echo "  - spike-latencies.csv (spike test)"
echo ""