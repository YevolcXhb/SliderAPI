# Sub2API Docker Image

Sub2API is an AI API Gateway Platform for distributing and managing AI product subscription API quotas.

## Quick Start

```bash
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_DIALECT=mysql \
  -e DATABASE_HOST=host \
  -e DATABASE_PORT=3306 \
  -e DATABASE_USER=sub2api \
  -e DATABASE_PASSWORD=pass \
  -e DATABASE_DBNAME=sub2api \
  -e REDIS_URL="redis://host:6379" \
  weishaw/sub2api:latest
```

## Docker Compose

```yaml
version: '3.8'

services:
  sub2api:
    image: weishaw/sub2api:latest
    ports:
      - "8080:8080"
    environment:
      - DATABASE_DIALECT=mysql
      - DATABASE_HOST=db
      - DATABASE_PORT=3306
      - DATABASE_USER=sub2api
      - DATABASE_PASSWORD=sub2api
      - DATABASE_DBNAME=sub2api
      - REDIS_URL=redis://redis:6379
    depends_on:
      - db
      - redis

  db:
    image: mariadb:10.11.14
    environment:
      - MARIADB_USER=sub2api
      - MARIADB_PASSWORD=sub2api
      - MARIADB_DATABASE=sub2api
      - MARIADB_RANDOM_ROOT_PASSWORD=yes
    volumes:
      - mariadb_data:/var/lib/mysql

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  mariadb_data:
  redis_data:
```

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_*` | MariaDB connection variables (dialect/host/port/user/password/dbname) | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `PORT` | Server port | No | `8080` |
| `GIN_MODE` | Gin framework mode (`debug`/`release`) | No | `release` |

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `x.y.z` - Specific version
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

## Links

- [GitHub Repository](https://github.com/weishaw/sub2api)
- [Documentation](https://github.com/weishaw/sub2api#readme)
