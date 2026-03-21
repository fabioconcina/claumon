#!/usr/bin/env bash
#
# Checks pricing.json against models found in Claude session files and the
# Anthropic pricing page, then prompts for updates.
#
# Usage: ./scripts/update-pricing.sh
#
# What it does:
# 1. Scans ~/.claude/projects/ for model IDs actually used in sessions
# 2. Reports any models missing from pricing.json
# 3. Opens the Anthropic pricing page for manual reference
# 4. If you edited pricing.json, copies it to the embedded fallback
#
# This is intentionally semi-manual. Anthropic's pricing page is JS-rendered
# and changes layout over time, so scraping is fragile. Model pricing changes
# are infrequent (only on new model releases), so a quick manual check is
# more reliable than a brittle parser.

set -euo pipefail
cd "$(dirname "$0")/.."

PRICING_FILE="pricing.json"
EMBEDDED="internal/pricing/embedded.json"
CLAUDE_DIR="${CLAUDE_DIR:-$HOME/.claude}"
PRICING_URL="https://docs.anthropic.com/en/docs/about-claude/pricing"

echo "=== Pricing Update Check ==="
echo ""

# 1. Find all model IDs used in sessions
echo "Scanning sessions in $CLAUDE_DIR/projects/ ..."
SESSION_MODELS=$(grep -roh '"model":"[^"]*"' "$CLAUDE_DIR/projects/" 2>/dev/null \
    | sed 's/"model":"//;s/"//' \
    | sort -u \
    || true)

if [ -z "$SESSION_MODELS" ]; then
    echo "  No session files found."
    exit 0
fi

echo "  Models found in sessions:"
echo "$SESSION_MODELS" | sed 's/^/    /'
echo ""

# 2. Check which models are missing from pricing.json (after normalization)
echo "Checking against $PRICING_FILE ..."
MISSING=""
while IFS= read -r model; do
    # Normalize: strip date suffixes (e.g., claude-sonnet-4-6-20250514 -> claude-sonnet-4-6)
    NORMALIZED=$(echo "$model" | sed -E 's/-[0-9]{8}$//')
    if ! grep -q "\"$NORMALIZED\"" "$PRICING_FILE" 2>/dev/null; then
        MISSING="$MISSING  $model (normalized: $NORMALIZED)\n"
    fi
done <<< "$SESSION_MODELS"

if [ -z "$MISSING" ]; then
    echo "  All models have pricing entries."
else
    echo "  MISSING pricing for:"
    echo -e "$MISSING"
    echo "  Add these to $PRICING_FILE with values from:"
    echo "  $PRICING_URL"
fi

# 3. Show age of current pricing file
if [ -f "$PRICING_FILE" ]; then
    UPDATED=$(python3 -c "import json; print(json.load(open('$PRICING_FILE'))['updated'])" 2>/dev/null || echo "unknown")
    echo ""
    echo "Current pricing.json dated: $UPDATED"
fi

# 4. Offer to open pricing page
echo ""
read -p "Open Anthropic pricing page in browser? [y/N] " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    if command -v open &>/dev/null; then
        open "$PRICING_URL"
    elif command -v xdg-open &>/dev/null; then
        xdg-open "$PRICING_URL"
    else
        echo "Open manually: $PRICING_URL"
    fi
fi

# 5. Sync embedded copy
echo ""
read -p "Copy pricing.json to embedded fallback? [y/N] " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    cp "$PRICING_FILE" "$EMBEDDED"
    echo "Copied to $EMBEDDED"
    echo ""
    echo "Don't forget to commit both files:"
    echo "  git add $PRICING_FILE $EMBEDDED"
fi
