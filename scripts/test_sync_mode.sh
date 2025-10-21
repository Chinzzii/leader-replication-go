#!/bin/bash

# A script to test the synchronous replication mode of the key-value store.
# It follows the demo steps outlined in the README.md.

# Exit immediately if a command exits with a non-zero status.
set -e

# --- Configuration ---
LEADER_URL="http://localhost:8080"
FOLLOWER1_URL="http://localhost:8081"
FOLLOWER2_URL="http://localhost:8082"

# --- Helper Functions ---
# Color codes for readable output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${YELLOW}[INFO] $1${NC}"
}

success() {
    echo -e "${GREEN}[SUCCESS] $1${NC}"
}

fail() {
    echo -e "${RED}[FAIL] $1${NC}"
    # In a real test suite, you might want to capture logs before exiting.
    # echo "Dumping container logs..."
    # docker-compose logs
    exit 1
}

# Function to check if a required command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check the value of a key on a specific node
check_value() {
    local node_url=$1
    local key=$2
    local expected_value=$3
    local node_name=$4

    info "Checking for key '$key' on $node_name ($node_url)..."
    # Use curl's --fail flag to exit with an error on non-200 responses
    # Use -s for silent mode to keep the output clean
    local response=$(curl --fail -s "$node_url/get?key=$key")
    # Use sed to parse the JSON and extract the value for the "value" key
    local actual_value=$(echo "$response" | sed -n 's/.*"value":"\([^"]*\)".*/\1/p')

    if [ "$actual_value" == "$expected_value" ]; then
        success "$node_name has correct value: '$actual_value'"
    else
        fail "$node_name has incorrect value. Expected: '$expected_value', Got: '$actual_value'"
    fi
}

# Function to verify that a key is NOT present on a node
check_not_found() {
    local node_url=$1
    local key=$2
    local node_name=$3

    info "Verifying key '$key' is NOT FOUND on $node_name ($node_url)..."
    # We expect a 404, so we can't use --fail. Instead, check the status code.
    local http_status=$(curl -s -o /dev/null -w "%{http_code}" "$node_url/get?key=$key")

    if [ "$http_status" -eq 404 ]; then
        success "$node_name correctly returned 404 Not Found for key '$key'."
    else
        fail "$node_name returned status $http_status instead of 404 for key '$key'."
    fi
}


# --- Main Script ---

# 1. Prerequisites Check
info "Checking for prerequisites (docker, docker-compose)..."
if ! command_exists docker || ! command_exists docker-compose; then
    fail "docker and docker-compose are required. Please install them."
fi
success "All prerequisites are met."

# 2. Setup: Start a clean cluster
info "Setting up a clean 3-node cluster..."
docker-compose down > /dev/null 2>&1
docker-compose up -d
# Give the containers a moment to fully initialize
sleep 5
success "Cluster is up and running."

# 3. Test Case 1: Basic Synchronous Write and Verification
info "\n--- Running Test Case 1: Basic Synchronous Write ---"
KEY1="city"
VALUE1="Raleigh"
info "Writing key '$KEY1' with value '$VALUE1' to leader..."
curl -s -X POST -H "Content-Type: application/json" \
  -d "{\"key\":\"$KEY1\", \"value\":\"$VALUE1\"}" \
  "$LEADER_URL/put"
echo "" # for formatting

# Verify the write propagated to all nodes
check_value "$LEADER_URL" "$KEY1" "$VALUE1" "Leader"
check_value "$FOLLOWER1_URL" "$KEY1" "$VALUE1" "Follower 1"
check_value "$FOLLOWER2_URL" "$KEY1" "$VALUE1" "Follower 2"
success "Test Case 1 Passed!"


# 4. Test Case 2: Write during a Network Partition
info "\n--- Running Test Case 2: Write During Network Partition ---"
KEY2="weather"
VALUE2="clear"
PARTITIONED_PEER="http://follower1:8081"

info "Simulating network partition: Blocking replication to Follower 1..."
curl -s -X POST "$LEADER_URL/partition?block=$PARTITIONED_PEER" > /dev/null
success "Partition created."

info "Writing key '$KEY2' with value '$VALUE2'. Expecting a ~5-second delay due to sync mode..."
start_time=$(date +%s)
curl -s -X POST -H "Content-Type: application/json" \
  -d "{\"key\":\"$KEY2\", \"value\":\"$VALUE2\"}" \
  "$LEADER_URL/put"
echo "" # for formatting
end_time=$(date +%s)
duration=$((end_time - start_time))
info "Write command took $duration seconds."

if [ "$duration" -lt 5 ]; then
    fail "Write completed too quickly ($duration s). The sync replication did not wait for the timeout as expected."
else
    success "Write command correctly delayed due to partition."
fi


# Verify the data state across the partitioned cluster
check_value "$LEADER_URL" "$KEY2" "$VALUE2" "Leader"
check_value "$FOLLOWER2_URL" "$KEY2" "$VALUE2" "Follower 2 (Healthy)"
check_not_found "$FOLLOWER1_URL" "$KEY2" "Follower 1 (Partitioned)"
success "Test Case 2 Passed!"


# 5. Test Case 3: Heal Partition and Verify Consistency Catch-up
info "\n--- Running Test Case 3: Heal Partition and Verify Write Propagation ---"
VALUE3="rainy"

info "Healing the network partition..."
curl -s -X POST "$LEADER_URL/partition?unblock=$PARTITIONED_PEER" > /dev/null
success "Partition healed."

info "Updating key '$KEY2' with new value '$VALUE3'..."
curl -s -X POST -H "Content-Type: application/json" \
  -d "{\"key\":\"$KEY2\", \"value\":\"$VALUE3\"}" \
  "$LEADER_URL/put"
echo "" # for formatting

# Verify the new write propagated to all nodes, including the previously partitioned one
check_value "$LEADER_URL" "$KEY2" "$VALUE3" "Leader"
check_value "$FOLLOWER1_URL" "$KEY2" "$VALUE3" "Follower 1"
check_value "$FOLLOWER2_URL" "$KEY2" "$VALUE3" "Follower 2"
success "Test Case 3 Passed!"


# --- Cleanup ---
info "\nAll tests passed successfully. Cleaning up Docker containers..."
docker-compose down > /dev/null 2>&1
success "Cleanup complete."
