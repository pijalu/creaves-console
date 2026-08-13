# Creaves Console

Standalone application for consolidating animal care data from multiple Creaves instances.

## Overview

This application provides a unified view of animal care data across multiple Creaves centers. 
It imports events from source databases and builds a consolidated view for authority oversight.

## Features

- **Multi-source import**: Import events from multiple Creaves instances
- **Consolidated view**: Unified view of all animals across all instances
- **Drill-down**: Detailed view of individual animals with full event history
- **User management**: Dedicated user system for authority personnel
- **Source management**: Configure and manage source database connections

## Architecture

- **Independent application**: Separate from the main Creaves app
- **Own database**: Dedicated database for consolidated data
- **Own models**: No shared data structures with main app
- **Docker support**: Containerized deployment

## Setup

### Prerequisites

- Go 1.18+
- MySQL/MariaDB
- Docker (optional)

### Database Setup

```bash
# Run migrations
buffalo pop migrate up

# Create admin user
buffalo task db:seed
```

### Running

```bash
# Development
buffalo dev

# Production
buffalo build --environment production -o bin/creaves-console
./bin/creaves-console
```

### Docker

```bash
# Build and run with docker-compose
docker-compose up -d

# Run migrations
docker-compose exec consolidation-app ./consolidation migrate
```

## Usage

### Configure Sources

1. Login as admin
2. Navigate to Source Instances
3. Add new source with database connection details

### Import Events

```bash
# Import from all sources
buffalo task import:all

# Import from specific source
buffalo task import:source SOURCE_ID

# Process unprocessed events
buffalo task consolidation:process
```

### View Consolidated Data

- Dashboard: Overview statistics
- Consolidated Animals: List view with filters
- Drill-down: Detailed animal view with event history

## API Endpoints

- `GET /` - Dashboard
- `GET /auth/new` - Login
- `GET /users` - User management (admin)
- `GET /source_instances` - Source management (admin)
- `GET /consolidated_animals` - Consolidated animal list
- `GET /consolidated_animals/:id` - Animal detail
- `GET /consolidated_animals/:id/drill_down` - Drill-down view
- `GET /import` - Import from all sources

## Configuration

Environment variables:
- `DATABASE_URL` - Database connection string
- `GO_ENV` - Environment (development/production)
- `ADDR` - Bind address (default: 0.0.0.0)
- `PORT` - Port (default: 3000)

## License

Same as Creaves project
