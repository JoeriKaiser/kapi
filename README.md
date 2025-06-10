# Kapi (Kchat API) - Cloneathon Backend

This repository contains the Go backend for our LLM chat application, created for the T3 Cloneathon. It provides backend API for the frontend application.

## Tech Stack

-   **Language:** [Go](https://golang.org/)
-   **Web Framework:** [Gin](https://gin-gonic.com/)
-   **Database:** [PostgreSQL](https://www.postgresql.org/)
-   **ORM:** [GORM](https://gorm.io/)
-   **Authentication:** [JWT](https://jwt.io/)
-   **Containerization:** [Docker](https://www.docker.com/) & [Docker Compose](https://docs.docker.com/compose/)

## Quick Start

### 1. Configure Environment

Create a `.env` file in the project root

-   `DB_HOST` - Database host (e.g., `db` for Docker)
-   `DB_PORT` - Database port (e.g., `5432`)
-   `DB_USER` - Database username
-   `DB_PASSWORD` - Database password
-   `DB_NAME` - Database name
-   `DB_SSLMODE` - disable SSL verification (e.g., `disable`)
-   `JWT_SECRET` - A strong secret for signing tokens
-   `PORT` - Server port (e.g., `8080`)
-   `GIN_MODE` - Gin mode (e.g., `debug` or `release`)
-   `OPENROUTER_KEY` - OpenRouter API key

### 2. Run with Docker (Recommended)

This method starts the Go server and a PostgreSQL database in containers.

```bash
# Build and start the services in detached mode
docker-compose up --build -d
