# AlphaFlow

AlphaFlow is an intelligent trading system project.

## Project Structure

```text
AlphaFlow/
  frontend/                         # Future React + TypeScript frontend
  backend/
    python-service/                 # Python services, each with its own dependencies
      alphaflow-core/               # Current Python service managed by uv
    go-service/                     # Go services, each with its own module
```

## Backend

The current Python service uses Python 3.12 and uv.

```sh
cd backend/python-service/alphaflow-core
uv run python src/alphaflow/main.py
```

No web framework, database, broker SDK, or task queue has been selected yet.

Go services will live under `backend/go-service/` when they are introduced.

## Frontend

The frontend directory is reserved for a future React + TypeScript application.
