#!/bin/bash
# Docker helper script for running creaves-console tasks inside the container

set -e

# Default values
IMAGE="muaddib/creaves-console"
NETWORK="consolidation-network"
DATABASE_URL="mysql://consolidation:consolidation@(consolidation-db:3306)/consolidation?parseTime=true&multiStatements=true&readTimeout=3s"

# Function to show usage
usage() {
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  process      Process unprocessed events"
    echo "  rebuild      Rebuild consolidated view"
    echo "  stats        Show statistics"
    echo "  serve        Start web server (port 3001)"
    echo "  build        Build Docker image (local, single platform)"
    echo "  push         Build & push multi-arch image (same as build.sh)"
    echo "  shell        Open shell in container"
    echo ""
    echo "Examples:"
    echo "  $0 process"
    echo "  $0 rebuild"
    echo "  $0 stats"
    echo ""
    echo "Environment:"
    echo "  DATABASE_URL    Database connection string (default: $DATABASE_URL)"
    exit 1
}

# Build the Docker image locally
build_image() {
    echo "Building Docker image..."
    docker build -t "$IMAGE" .
    echo "Build complete: $IMAGE"
}

# Build & push multi-arch image (delegates to build.sh)
push_image() {
    ./build.sh
}

# Run a buffalo task inside the container
run_task() {
    local task="$1"
    shift

    docker run --rm \
        --network "$NETWORK" \
        -e DATABASE_URL="$DATABASE_URL" \
        -e GO_ENV=production \
        --entrypoint /bin/app \
        "$IMAGE" \
        task "$task" "$@"
}

# Main logic
case "${1:-}" in
    build)
        build_image
        ;;
    push)
        push_image
        ;;
    process)
        run_task consolidation:process
        ;;
    rebuild)
        run_task consolidation:rebuild
        ;;
    stats)
        run_task consolidation:stats
        ;;
    serve)
        docker run --rm \
            --network "$NETWORK" \
            -e DATABASE_URL="$DATABASE_URL" \
            -e GO_ENV=production \
            -e ADDR=0.0.0.0 \
            -e PORT=3001 \
            -p 3001:3001 \
            "$IMAGE"
        ;;
    shell)
        docker run --rm -it \
            --network "$NETWORK" \
            -e DATABASE_URL="$DATABASE_URL" \
            --entrypoint sh \
            "$IMAGE"
        ;;
    *)
        usage
        ;;
esac
