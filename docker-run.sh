#!/bin/bash
# Docker helper script for running consolidation CLI commands

set -e

# Default values
IMAGE="consolidation"
NETWORK="consolidation-network"
DATABASE_URL="mysql://consolidation:consolidation@consolidation-db:3306/consolidation"

# Function to show usage
usage() {
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  import       Import events from all sources"
    echo "  import-source <source-id>  Import from specific source"
    echo "  process      Process unprocessed events"
    echo "  rebuild      Rebuild consolidated view"
    echo "  stats        Show statistics"
    echo "  history      Show import history"
    echo "  serve        Start web server"
    echo "  build        Build Docker image"
    echo "  shell        Open shell in container"
    echo ""
    echo "Examples:"
    echo "  $0 import"
    echo "  $0 import-source 550e8400-e29b-41d4-a716-446655440000"
    echo "  $0 process"
    echo "  $0 stats"
    echo ""
    echo "Environment:"
    echo "  DATABASE_URL    Database connection string (default: $DATABASE_URL)"
    exit 1
}

# Build the Docker image
build_image() {
    echo "Building Docker image..."
    docker build -t "$IMAGE" .
    echo "Build complete: $IMAGE"
}

# Run a CLI command
run_cli() {
    local cmd="$1"
    shift
    
    docker run --rm \
        --network "$NETWORK" \
        -e DATABASE_URL="$DATABASE_URL" \
        -e GO_ENV=production \
        "$IMAGE" \
        ./consolidation-cli "$cmd" "$@"
}

# Main logic
case "${1:-}" in
    build)
        build_image
        ;;
    import)
        run_cli import
        ;;
    import-source)
        if [ -z "${2:-}" ]; then
            echo "Error: Source ID required"
            usage
        fi
        run_cli import -source "$2"
        ;;
    process)
        run_cli process
        ;;
    rebuild)
        run_cli rebuild
        ;;
    stats)
        run_cli stats
        ;;
    history)
        run_cli history
        ;;
    serve)
        docker run --rm \
            --network "$NETWORK" \
            -e DATABASE_URL="$DATABASE_URL" \
            -e GO_ENV=production \
            -p 3001:3000 \
            "$IMAGE"
        ;;
    shell)
        docker run --rm -it \
            --network "$NETWORK" \
            -e DATABASE_URL="$DATABASE_URL" \
            "$IMAGE" \
            sh
        ;;
    *)
        usage
        ;;
esac
