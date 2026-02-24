#!/bin/sh
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║     Openclawssy - ZAI Setup            ║${NC}"
echo -e "${GREEN}║     Powered by GLM-4.7 Coding Plan     ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
echo ""

# Check Docker socket — required for spawning sandbox containers.
# The socket is a Unix domain socket mount, not HTTP/TCP.
if [ ! -S "/var/run/docker.sock" ]; then
    echo -e "${RED}ERROR: /var/run/docker.sock not found.${NC}"
    echo "The Docker sandbox requires the host Docker socket to spawn workspace containers."
    echo ""
    echo "Add this volume mount to your docker run / docker-compose:"
    echo "  -v /var/run/docker.sock:/var/run/docker.sock"
    echo ""
    echo -e "${YELLOW}Falling back to sandbox.provider=local (no container isolation)${NC}"
    SANDBOX_FALLBACK="local"
fi

# Check if already configured
if [ -f "/app/.openclawssy/config.json" ] && [ -s "/app/.openclawssy/config.json" ]; then
    echo -e "${GREEN}✓ Configuration found. Starting server...${NC}"
else
    echo -e "${YELLOW}⚠ First-time setup required${NC}"
    echo ""
    
    # Check if API key is provided via environment variable
    if [ -z "$ZAI_API_KEY" ]; then
        echo -e "${RED}ERROR: ZAI_API_KEY environment variable is required${NC}"
        echo ""
        echo "Please provide your Z.AI API key from https://z.ai/subscribe"
        echo ""
        echo "Usage examples:"
        echo "  docker run -e ZAI_API_KEY=your-key-here ..."
        echo "  docker-compose up (with ZAI_API_KEY in .env file)"
        echo ""
        exit 1
    fi
    
    echo -e "${GREEN}✓ ZAI_API_KEY found in environment${NC}"
    echo ""
    
    # Initialize the configuration
    echo "Initializing Openclawssy with ZAI provider..."
    openclawssy init -agent default || true
    
    # Store the API key in the secret store
    echo "Storing API key securely..."
    # Generate master key if needed
    if [ ! -f "/app/.openclawssy/master.key" ]; then
        mkdir -p /app/.openclawssy
        openssl rand -hex 32 > /app/.openclawssy/master.key
        chmod 600 /app/.openclawssy/master.key
    fi
    
    echo ""
    echo -e "${GREEN}✓ Setup complete!${NC}"
    echo ""
fi

# Verify API key is available
if [ -n "$ZAI_API_KEY" ]; then
    echo -e "${GREEN}✓ Using ZAI API key from environment${NC}"
else
    echo -e "${YELLOW}⚠ ZAI_API_KEY not set in environment${NC}"
    echo "Make sure it's stored in the secret store or the container will fail."
fi

# Show sandbox status
if [ -n "$SANDBOX_FALLBACK" ]; then
    echo -e "${YELLOW}⚠ Sandbox: local (no Docker socket — no container isolation)${NC}"
else
    echo -e "${GREEN}✓ Docker sandbox enabled (workspace runs in separate containers)${NC}"
fi

echo ""
echo -e "${GREEN}Starting Openclawssy server...${NC}"
echo -e "${GREEN}Dashboard available at: http://localhost:8080/dashboard${NC}"
echo ""

# Run the server with the provided arguments or defaults.
if [ $# -eq 0 ]; then
    PROVIDER="${SANDBOX_FALLBACK:-docker}"
    exec openclawssy serve --token "${OPENCLAWSSY_TOKEN:-change-me}" --addr "0.0.0.0:8080" --sandbox-active --sandbox-provider "$PROVIDER"
else
    exec openclawssy "$@"
fi
