#!/usr/bin/env bash
#
# Multi-User Scenario Setup Script
#
# This script demonstrates the full API flow for setting up a multi-user
# e-commerce scenario with hierarchical access control.
#
# Prerequisites:
#   - Server running at http://localhost:8080
#   - curl and jq installed
#
# Usage:
#   chmod +x setup.sh
#   ./setup.sh

set -euo pipefail

BASE_URL="http://localhost:8080/api/v1"

# Color output helpers
info()  { printf "\033[1;34m==> %s\033[0m\n" "$*"; }
ok()    { printf "\033[1;32m    OK: %s\033[0m\n" "$*"; }
error() { printf "\033[1;31m    ERROR: %s\033[0m\n" "$*"; }

# -------------------------------------------------------------------------
# Step 1: Register Users
# -------------------------------------------------------------------------
info "Registering Alice (admin@acme.com)..."
ALICE_RESP=$(curl -s -X POST "$BASE_URL/auth/register" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@acme.com",
    "password": "AlicePass123!",
    "display_name": "Alice Admin"
  }')
ALICE_TOKEN=$(echo "$ALICE_RESP" | jq -r '.data.access_token')
if [ "$ALICE_TOKEN" = "null" ] || [ -z "$ALICE_TOKEN" ]; then
  error "Failed to register Alice: $ALICE_RESP"
  exit 1
fi
ok "Alice registered, token obtained"

info "Registering Bob (bob@acme.com)..."
BOB_RESP=$(curl -s -X POST "$BASE_URL/auth/register" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "bob@acme.com",
    "password": "BobPass123!",
    "display_name": "Bob Builder"
  }')
BOB_TOKEN=$(echo "$BOB_RESP" | jq -r '.data.access_token')
BOB_ID=$(echo "$BOB_RESP" | jq -r '.data.user.id // empty')
if [ "$BOB_TOKEN" = "null" ] || [ -z "$BOB_TOKEN" ]; then
  error "Failed to register Bob: $BOB_RESP"
  exit 1
fi
ok "Bob registered (ID: $BOB_ID)"

info "Registering Carol (carol@external.com)..."
CAROL_RESP=$(curl -s -X POST "$BASE_URL/auth/register" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "carol@external.com",
    "password": "CarolPass123!",
    "display_name": "Carol Viewer"
  }')
CAROL_TOKEN=$(echo "$CAROL_RESP" | jq -r '.data.access_token')
CAROL_ID=$(echo "$CAROL_RESP" | jq -r '.data.user.id // empty')
if [ "$CAROL_TOKEN" = "null" ] || [ -z "$CAROL_TOKEN" ]; then
  error "Failed to register Carol: $CAROL_RESP"
  exit 1
fi
ok "Carol registered (ID: $CAROL_ID)"

# If user IDs weren't in register response, fetch them via /me
if [ -z "$BOB_ID" ]; then
  BOB_ID=$(curl -s -H "Authorization: Bearer $BOB_TOKEN" "$BASE_URL/auth/me" | jq -r '.data.id')
fi
if [ -z "$CAROL_ID" ]; then
  CAROL_ID=$(curl -s -H "Authorization: Bearer $CAROL_TOKEN" "$BASE_URL/auth/me" | jq -r '.data.id')
fi

# -------------------------------------------------------------------------
# Step 2: Alice creates company "Acme Corp"
# -------------------------------------------------------------------------
info "Alice creates company 'Acme Corp'..."
COMPANY_RESP=$(curl -s -X POST "$BASE_URL/companies" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Acme Corp",
    "slug": "acme-corp"
  }')
COMPANY_ID=$(echo "$COMPANY_RESP" | jq -r '.data.id')
if [ "$COMPANY_ID" = "null" ] || [ -z "$COMPANY_ID" ]; then
  error "Failed to create company: $COMPANY_RESP"
  exit 1
fi
ok "Company created (ID: $COMPANY_ID)"

# -------------------------------------------------------------------------
# Step 3: Alice creates organization "Engineering" under Acme Corp
# -------------------------------------------------------------------------
info "Alice creates organization 'Engineering'..."
ORG_RESP=$(curl -s -X POST "$BASE_URL/companies/$COMPANY_ID/organizations" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Engineering",
    "slug": "engineering"
  }')
ORG_ID=$(echo "$ORG_RESP" | jq -r '.data.id')
if [ "$ORG_ID" = "null" ] || [ -z "$ORG_ID" ]; then
  error "Failed to create organization: $ORG_RESP"
  exit 1
fi
ok "Organization created (ID: $ORG_ID)"

# -------------------------------------------------------------------------
# Step 4: Alice creates project "E-Commerce" under Engineering
# -------------------------------------------------------------------------
info "Alice creates project 'E-Commerce'..."
PROJECT_RESP=$(curl -s -X POST "$BASE_URL/organizations/$ORG_ID/projects" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "E-Commerce",
    "slug": "e-commerce",
    "description": "E-commerce order processing platform"
  }')
PROJECT_ID=$(echo "$PROJECT_RESP" | jq -r '.data.id')
if [ "$PROJECT_ID" = "null" ] || [ -z "$PROJECT_ID" ]; then
  error "Failed to create project: $PROJECT_RESP"
  exit 1
fi
ok "Project created (ID: $PROJECT_ID)"

# -------------------------------------------------------------------------
# Step 5: Alice adds Bob as editor on the E-Commerce project
# -------------------------------------------------------------------------
info "Alice adds Bob as editor on E-Commerce project..."
ADD_BOB_RESP=$(curl -s -X POST "$BASE_URL/projects/$PROJECT_ID/members" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"$BOB_ID\",
    \"role\": \"editor\"
  }")
BOB_MEMBER_ID=$(echo "$ADD_BOB_RESP" | jq -r '.data.id')
if [ "$BOB_MEMBER_ID" = "null" ] || [ -z "$BOB_MEMBER_ID" ]; then
  error "Failed to add Bob: $ADD_BOB_RESP"
  exit 1
fi
ok "Bob added as editor (membership: $BOB_MEMBER_ID)"

# -------------------------------------------------------------------------
# Step 6: Alice creates Workflows A, B, C
# -------------------------------------------------------------------------
info "Alice creates Workflow A: Order Ingestion..."
WF_A_CONFIG=$(cat example/multi-workflow-ecommerce/workflow-a-orders.yaml)
WF_A_RESP=$(curl -s -X POST "$BASE_URL/projects/$PROJECT_ID/workflows" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Order Ingestion\",
    \"description\": \"Receives and validates incoming orders\",
    \"config_yaml\": $(echo "$WF_A_CONFIG" | jq -Rs .)
  }")
WF_A_ID=$(echo "$WF_A_RESP" | jq -r '.data.id')
if [ "$WF_A_ID" = "null" ] || [ -z "$WF_A_ID" ]; then
  error "Failed to create Workflow A: $WF_A_RESP"
  exit 1
fi
ok "Workflow A created (ID: $WF_A_ID)"

info "Alice creates Workflow B: Fulfillment Processing..."
WF_B_CONFIG=$(cat example/multi-workflow-ecommerce/workflow-b-fulfillment.yaml)
WF_B_RESP=$(curl -s -X POST "$BASE_URL/projects/$PROJECT_ID/workflows" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Fulfillment Processing\",
    \"description\": \"Processes order fulfillment lifecycle\",
    \"config_yaml\": $(echo "$WF_B_CONFIG" | jq -Rs .)
  }")
WF_B_ID=$(echo "$WF_B_RESP" | jq -r '.data.id')
if [ "$WF_B_ID" = "null" ] || [ -z "$WF_B_ID" ]; then
  error "Failed to create Workflow B: $WF_B_RESP"
  exit 1
fi
ok "Workflow B created (ID: $WF_B_ID)"

info "Alice creates Workflow C: Notification Hub..."
WF_C_CONFIG=$(cat example/multi-workflow-ecommerce/workflow-c-notifications.yaml)
WF_C_RESP=$(curl -s -X POST "$BASE_URL/projects/$PROJECT_ID/workflows" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Notification Hub\",
    \"description\": \"Sends notifications for order and fulfillment events\",
    \"config_yaml\": $(echo "$WF_C_CONFIG" | jq -Rs .)
  }")
WF_C_ID=$(echo "$WF_C_RESP" | jq -r '.data.id')
if [ "$WF_C_ID" = "null" ] || [ -z "$WF_C_ID" ]; then
  error "Failed to create Workflow C: $WF_C_RESP"
  exit 1
fi
ok "Workflow C created (ID: $WF_C_ID)"

# -------------------------------------------------------------------------
# Step 7: Alice adds Carol as viewer on the project (viewer access)
# -------------------------------------------------------------------------
info "Alice adds Carol as viewer on E-Commerce project..."
ADD_CAROL_RESP=$(curl -s -X POST "$BASE_URL/projects/$PROJECT_ID/members" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"$CAROL_ID\",
    \"role\": \"viewer\"
  }")
CAROL_MEMBER_ID=$(echo "$ADD_CAROL_RESP" | jq -r '.data.id')
if [ "$CAROL_MEMBER_ID" = "null" ] || [ -z "$CAROL_MEMBER_ID" ]; then
  error "Failed to add Carol: $ADD_CAROL_RESP"
  exit 1
fi
ok "Carol added as viewer (membership: $CAROL_MEMBER_ID)"

# -------------------------------------------------------------------------
# Step 8: Verification - demonstrate access levels
# -------------------------------------------------------------------------
info "Verifying access levels..."

echo ""
info "Bob lists workflows in E-Commerce project (should see all 3)..."
BOB_WF_LIST=$(curl -s -H "Authorization: Bearer $BOB_TOKEN" \
  "$BASE_URL/projects/$PROJECT_ID/workflows")
BOB_WF_COUNT=$(echo "$BOB_WF_LIST" | jq '.data | length')
ok "Bob sees $BOB_WF_COUNT workflows"

info "Bob gets Workflow A details (should succeed as editor)..."
BOB_WF_A=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $BOB_TOKEN" \
  "$BASE_URL/workflows/$WF_A_ID")
ok "Bob GET Workflow A: HTTP $BOB_WF_A"

info "Carol gets Workflow C details (should succeed as viewer)..."
CAROL_WF_C=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $CAROL_TOKEN" \
  "$BASE_URL/workflows/$WF_C_ID")
ok "Carol GET Workflow C: HTTP $CAROL_WF_C"

info "Carol tries to get Workflow A (viewer can still GET via project membership)..."
CAROL_WF_A=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $CAROL_TOKEN" \
  "$BASE_URL/workflows/$WF_A_ID")
ok "Carol GET Workflow A: HTTP $CAROL_WF_A"

# -------------------------------------------------------------------------
# Summary
# -------------------------------------------------------------------------
echo ""
info "Setup complete!"
echo ""
echo "  Company:      Acme Corp ($COMPANY_ID)"
echo "  Organization: Engineering ($ORG_ID)"
echo "  Project:      E-Commerce ($PROJECT_ID)"
echo ""
echo "  Workflow A:   Order Ingestion ($WF_A_ID)"
echo "  Workflow B:   Fulfillment Processing ($WF_B_ID)"
echo "  Workflow C:   Notification Hub ($WF_C_ID)"
echo ""
echo "  Alice (owner): token=$ALICE_TOKEN"
echo "  Bob (editor):  token=$BOB_TOKEN"
echo "  Carol (viewer): token=$CAROL_TOKEN"
echo ""
echo "  Try:"
echo "    curl -H \"Authorization: Bearer $BOB_TOKEN\" $BASE_URL/workflows/$WF_A_ID"
echo "    curl -H \"Authorization: Bearer $CAROL_TOKEN\" $BASE_URL/workflows/$WF_C_ID"
