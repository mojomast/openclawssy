#!/bin/sh
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

PROVIDER_OVERRIDE="$(printf '%s' "${OPENCLAWSSY_MODEL_PROVIDER:-}" | tr '[:upper:]' '[:lower:]')"
MODEL_OVERRIDE="$(printf '%s' "${OPENCLAWSSY_MODEL_NAME:-}")"
OPENAI_COMPAT_BASE_OVERRIDE="$(printf '%s' "${OPENAI_COMPAT_BASE_URL:-}" | sed 's:/*$::')"
SANDBOX_IMAGE_OVERRIDE="$(printf '%s' "${OPENCLAWSSY_SANDBOX_DOCKER_IMAGE:-}" | sed 's/^ *//;s/ *$//')"

default_model_for_provider() {
    case "$1" in
        openai_compat)
            printf 'gpt-5.4'
            ;;
        *)
            printf 'GLM-4.7'
            ;;
    esac
}

apply_provider_overrides() {
    if [ -z "$PROVIDER_OVERRIDE" ] && [ -z "$MODEL_OVERRIDE" ] && [ -z "$OPENAI_COMPAT_BASE_OVERRIDE" ]; then
        return
    fi

    config_file="/app/.openclawssy/config.json"
    if [ ! -s "$config_file" ]; then
        return
    fi

    current_provider="$(jq -r '.model.provider // "zai"' "$config_file")"
    current_model="$(jq -r '.model.name // ""' "$config_file")"

    target_provider="$current_provider"
    target_model="$current_model"

    if [ -n "$PROVIDER_OVERRIDE" ]; then
        target_provider="$PROVIDER_OVERRIDE"
    fi
    if [ -n "$MODEL_OVERRIDE" ]; then
        target_model="$MODEL_OVERRIDE"
    fi
    if [ -z "$target_model" ]; then
        target_model="$(default_model_for_provider "$target_provider")"
    fi

    tmp_config="/app/.openclawssy/config.json.tmp.$$"
    if [ "$target_provider" = "openai_compat" ]; then
        target_base="$(jq -r '.providers.openai_compat.base_url // ""' "$config_file")"
        if [ -n "$OPENAI_COMPAT_BASE_OVERRIDE" ]; then
            target_base="$OPENAI_COMPAT_BASE_OVERRIDE"
        fi
        if [ -z "$target_base" ]; then
            echo -e "${RED}ERROR: openai_compat provider requires providers.openai_compat.base_url (set OPENAI_COMPAT_BASE_URL)${NC}"
            exit 1
        fi
        jq --arg provider "$target_provider" --arg model "$target_model" --arg base "$target_base" \
            '.model.provider = $provider | .model.name = $model | .providers.openai_compat.base_url = $base' \
            "$config_file" > "$tmp_config"
    else
        jq --arg provider "$target_provider" --arg model "$target_model" \
            '.model.provider = $provider | .model.name = $model' \
            "$config_file" > "$tmp_config"
    fi
    mv "$tmp_config" "$config_file"
    echo -e "${GREEN}✓ Applied provider override: ${target_provider}/${target_model}${NC}"
}

apply_sandbox_overrides() {
    if [ -z "$SANDBOX_IMAGE_OVERRIDE" ]; then
        return
    fi

    config_file="/app/.openclawssy/config.json"
    if [ ! -s "$config_file" ]; then
        return
    fi

    tmp_config="/app/.openclawssy/config.json.tmp.$$"
    jq --arg image "$SANDBOX_IMAGE_OVERRIDE" '
        (.sandbox //= {}) |
        (.sandbox.docker //= {}) |
        .sandbox.docker.image = $image |
        if ((.sandbox.docker.allowed_images // []) | length) > 0 then
            .sandbox.docker.allowed_images = (((.sandbox.docker.allowed_images // []) + [$image]) | unique)
        else
            .
        end
    ' "$config_file" > "$tmp_config"
    mv "$tmp_config" "$config_file"
    echo -e "${GREEN}✓ Applied sandbox image override: ${SANDBOX_IMAGE_OVERRIDE}${NC}"
}

echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║     Openclawssy - Docker Setup         ║${NC}"
echo -e "${GREEN}║     OpenAI-compatible runtime          ║${NC}"
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

    BOOTSTRAP_PROVIDER="zai"
    if [ -n "$PROVIDER_OVERRIDE" ]; then
        BOOTSTRAP_PROVIDER="$PROVIDER_OVERRIDE"
    fi

    case "$BOOTSTRAP_PROVIDER" in
        openai_compat)
            if [ -z "$OPENAI_COMPAT_API_KEY" ]; then
                echo -e "${RED}ERROR: OPENAI_COMPAT_API_KEY environment variable is required for openai_compat provider${NC}"
                echo ""
                echo "Usage examples:"
                echo "  docker run -e OPENAI_COMPAT_API_KEY=your-key-here -e OPENAI_COMPAT_BASE_URL=https://api.example.com/v1 ..."
                echo "  docker-compose up (with OPENAI_COMPAT_API_KEY and OPENAI_COMPAT_BASE_URL in .env file)"
                echo ""
                exit 1
            fi
            if [ -z "$OPENAI_COMPAT_BASE_OVERRIDE" ]; then
                echo -e "${RED}ERROR: OPENAI_COMPAT_BASE_URL environment variable is required for openai_compat provider bootstrap${NC}"
                echo ""
                echo "Example: OPENAI_COMPAT_BASE_URL=https://api.example.com/v1"
                echo ""
                exit 1
            fi
            echo -e "${GREEN}✓ OPENAI_COMPAT_API_KEY found in environment${NC}"
            ;;
        zai)
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
            ;;
        *)
            echo -e "${YELLOW}⚠ No bootstrap API key validation configured for provider '$BOOTSTRAP_PROVIDER'${NC}"
            ;;
    esac
    echo ""

    # Initialize the configuration
    echo "Initializing Openclawssy configuration..."
    if ! openclawssy init -agent default; then
        echo -e "${RED}ERROR: failed to initialize /app/.openclawssy/config.json${NC}"
        exit 1
    fi

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

apply_provider_overrides
apply_sandbox_overrides

ACTIVE_PROVIDER="$(jq -r '.model.provider // "zai"' /app/.openclawssy/config.json 2>/dev/null || printf 'zai')"

# Verify API key is available
case "$ACTIVE_PROVIDER" in
    openai_compat)
        if [ -n "$OPENAI_COMPAT_API_KEY" ]; then
            echo -e "${GREEN}✓ Using OPENAI_COMPAT_API_KEY from environment${NC}"
        else
            echo -e "${YELLOW}⚠ OPENAI_COMPAT_API_KEY not set in environment${NC}"
            echo "Make sure it's stored in the secret store or the container will fail."
        fi
        ;;
    zai)
        if [ -n "$ZAI_API_KEY" ]; then
            echo -e "${GREEN}✓ Using ZAI API key from environment${NC}"
        else
            echo -e "${YELLOW}⚠ ZAI_API_KEY not set in environment${NC}"
            echo "Make sure it's stored in the secret store or the container will fail."
        fi
        ;;
    *)
        echo -e "${GREEN}✓ Using provider from config: ${ACTIVE_PROVIDER}${NC}"
        ;;
esac

# Show sandbox status
if [ -n "$SANDBOX_FALLBACK" ]; then
    echo -e "${YELLOW}⚠ Sandbox: local (no Docker socket — no container isolation)${NC}"
else
    echo -e "${GREEN}✓ Docker sandbox enabled (workspace runs in separate containers)${NC}"
fi

echo ""
echo -e "${GREEN}Starting Openclawssy server...${NC}"
echo -e "${GREEN}Dashboard available at: http://localhost:8080/dashboard${NC}"
echo "Onboarding docs: https://github.com/mojomast/openclawssy/blob/main/docs/GETTING_STARTED.md"
echo "Docker docs: https://github.com/mojomast/openclawssy/blob/main/DOCKER.md"
echo ""

# Run the server with the provided arguments or defaults.
if [ $# -eq 0 ]; then
    PROVIDER="${SANDBOX_FALLBACK:-docker}"
    exec openclawssy serve --token "${OPENCLAWSSY_TOKEN:-change-me}" --addr "0.0.0.0:8080" --sandbox-active --sandbox-provider "$PROVIDER"
else
    exec openclawssy "$@"
fi
